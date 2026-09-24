package media

import (
	"encoding/binary"
	"time"
)

// What a photograph knows about itself is read here rather than with a library:
// three tags are needed - which way up it is, when it was taken and where - and
// each one is a few bytes at a known place in the EXIF block a camera writes
// into a JPEG. Anything unreadable is simply absent, never an error: a
// photograph with a damaged header is still a photograph.

// Metadata is what a file says about itself.
type Metadata struct {
	// Orientation is the EXIF value from 1 to 8; 1 means "as stored".
	Orientation int
	// TakenAt is when the camera took the picture, in UTC unless the file
	// carries the offset its clock was set to.
	TakenAt *time.Time
	// Lat and Lng are where it was taken; both are set or neither is.
	Lat *float64
	Lng *float64
}

// EXIF tags read here. The first three live in the main directory, the rest in
// the sub-directories those two pointers lead to.
const (
	tagOrientation        = 0x0112
	tagExifIFD            = 0x8769
	tagGPSIFD             = 0x8825
	tagDateTimeOriginal   = 0x9003
	tagOffsetTimeOriginal = 0x9011
	tagGPSLatitudeRef     = 0x0001
	tagGPSLatitude        = 0x0002
	tagGPSLongitudeRef    = 0x0003
	tagGPSLongitude       = 0x0004
)

// ReadMetadata - reads orientation, capture time and coordinates from a file.
//
// Only JPEG carries the block this reads; any other kind, or a file without
// EXIF, comes back with orientation 1 and nothing else.
//
// Arguments:
//   - data: the whole file, or at least its leading kilobytes, where the block
//     a camera writes always sits.
//
// Returns:
//   - what could be read, with an orientation of 1 when there is nothing.
func ReadMetadata(data []byte) Metadata {
	meta := Metadata{Orientation: 1}
	block, ok := exifBlock(data)
	if !ok {
		return meta
	}
	order, first, ok := tiffHeader(block)
	if !ok {
		return meta
	}

	entries := readIFD(block, order, first)
	if value, found := entries[tagOrientation]; found {
		if orientation := int(value.uint()); orientation >= 1 && orientation <= 8 {
			meta.Orientation = orientation
		}
	}

	if pointer, found := entries[tagExifIFD]; found {
		exif := readIFD(block, order, pointer.uint())
		meta.TakenAt = takenAt(exif)
	}
	if pointer, found := entries[tagGPSIFD]; found {
		gps := readIFD(block, order, pointer.uint())
		meta.Lat, meta.Lng = coordinates(gps)
	}
	return meta
}

// exifBlock finds the EXIF block of a JPEG: the APP1 segment whose payload
// starts with "Exif\0\0". The returned slice starts at the TIFF header.
func exifBlock(data []byte) ([]byte, bool) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil, false
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xFF {
			return nil, false
		}
		marker := data[offset+1]
		// Start of scan: the image data begins, so there is no header left.
		if marker == 0xDA || marker == 0xD9 {
			return nil, false
		}
		length := int(binary.BigEndian.Uint16(data[offset+2:]))
		if length < 2 || offset+2+length > len(data) {
			return nil, false
		}
		payload := data[offset+4 : offset+2+length]
		if marker == 0xE1 && len(payload) > 6 && string(payload[:6]) == "Exif\x00\x00" {
			return payload[6:], true
		}
		offset += 2 + length
	}
	return nil, false
}

// tiffHeader reads the byte order and the offset of the first directory.
func tiffHeader(block []byte) (binary.ByteOrder, uint32, bool) {
	if len(block) < 8 {
		return nil, 0, false
	}
	var order binary.ByteOrder
	switch string(block[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, 0, false
	}
	if order.Uint16(block[2:]) != 0x002A {
		return nil, 0, false
	}
	return order, order.Uint32(block[4:]), true
}

// entry is one directory record: its type, how many values it holds and the
// four bytes that are either the value itself or where it lives.
type entry struct {
	block  []byte
	order  binary.ByteOrder
	kind   uint16
	count  uint32
	value  []byte
	offset uint32
}

// EXIF value types this reader understands.
const (
	typeASCII    = 2
	typeShort    = 3
	typeLong     = 4
	typeRational = 5
)

// uint reads a SHORT or LONG entry as a number, or zero for anything else.
func (e entry) uint() uint32 {
	switch e.kind {
	case typeShort:
		return uint32(e.order.Uint16(e.value))
	case typeLong:
		return e.order.Uint32(e.value)
	default:
		return 0
	}
}

// text reads an ASCII entry, without its terminating zero.
func (e entry) text() string {
	if e.kind != typeASCII || e.count == 0 {
		return ""
	}
	raw := e.value
	if e.count > 4 {
		if int(e.offset)+int(e.count) > len(e.block) {
			return ""
		}
		raw = e.block[e.offset : uint32(e.offset)+e.count]
	}
	for index, char := range raw {
		if char == 0 {
			return string(raw[:index])
		}
	}
	return string(raw)
}

// rationals reads an entry of count rational numbers, such as the three that
// make up a latitude.
func (e entry) rationals(count int) ([]float64, bool) {
	if e.kind != typeRational || int(e.count) < count {
		return nil, false
	}
	start := int(e.offset)
	if start+8*count > len(e.block) {
		return nil, false
	}
	values := make([]float64, 0, count)
	for index := range count {
		numerator := e.order.Uint32(e.block[start+8*index:])
		denominator := e.order.Uint32(e.block[start+8*index+4:])
		if denominator == 0 {
			return nil, false
		}
		values = append(values, float64(numerator)/float64(denominator))
	}
	return values, true
}

// readIFD reads one directory into a map by tag. A malformed or looping
// directory yields what could be read so far.
func readIFD(block []byte, order binary.ByteOrder, offset uint32) map[uint16]entry {
	entries := make(map[uint16]entry)
	if int(offset)+2 > len(block) {
		return entries
	}
	count := int(order.Uint16(block[offset:]))
	for index := range count {
		start := int(offset) + 2 + index*12
		if start+12 > len(block) {
			break
		}
		record := block[start : start+12]
		entries[order.Uint16(record)] = entry{
			block:  block,
			order:  order,
			kind:   order.Uint16(record[2:]),
			count:  order.Uint32(record[4:]),
			value:  record[8:12],
			offset: order.Uint32(record[8:]),
		}
	}
	return entries
}

// takenAt reads the moment the picture was taken. EXIF writes local time
// without a zone, so the offset tag is used when the camera wrote one and the
// time is read as UTC otherwise - the only reading that never invents a zone.
func takenAt(exif map[uint16]entry) *time.Time {
	raw := exif[tagDateTimeOriginal].text()
	if raw == "" {
		return nil
	}
	const layout = "2006:01:02 15:04:05"
	if offset := exif[tagOffsetTimeOriginal].text(); offset != "" {
		if moment, err := time.Parse(layout+" -07:00", raw+" "+offset); err == nil {
			moment = moment.UTC()
			return &moment
		}
	}
	moment, err := time.ParseInLocation(layout, raw, time.UTC)
	if err != nil {
		return nil
	}
	return &moment
}

// coordinates reads the GPS position, which is stored as degrees, minutes and
// seconds plus the hemisphere.
func coordinates(gps map[uint16]entry) (*float64, *float64) {
	lat, okLat := degrees(gps[tagGPSLatitude], gps[tagGPSLatitudeRef].text(), "S", 90)
	lng, okLng := degrees(gps[tagGPSLongitude], gps[tagGPSLongitudeRef].text(), "W", 180)
	if !okLat || !okLng {
		return nil, nil
	}
	return &lat, &lng
}

// degrees turns one coordinate into a signed decimal degree.
func degrees(value entry, reference, negative string, limit float64) (float64, bool) {
	parts, ok := value.rationals(3)
	if !ok {
		return 0, false
	}
	degree := parts[0] + parts[1]/60 + parts[2]/3600
	if degree > limit {
		return 0, false
	}
	if reference == negative {
		degree = -degree
	}
	return degree, true
}
