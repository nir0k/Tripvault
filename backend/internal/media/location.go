package media

import (
	"encoding/binary"
	"errors"
	"io"
)

// A picture sent to a reader from outside its trip must not say where it was
// taken when that place is somebody's home. The previews keep no metadata at
// all; an original is sent as it was stored, so its location is blanked on the
// way out instead. It is blanked in place - the bytes overwritten, none
// removed - so the file keeps its length and a download of it can still be
// resumed by range.

// LocationHeadSize is how much of a file ScrubLocation needs: the EXIF block
// sits among the segments before the image data, each at most 64 KiB.
const LocationHeadSize = 1 << 20

// xmpPrefix starts the payload of the APP1 segment holding XMP, which an editor
// may have written the position into as well.
const xmpPrefix = "http://ns.adobe.com/xap/1.0/\x00"

// ScrubLocation - blanks where a JPEG was taken, in place: the GPS directory of
// its EXIF block is emptied and its values zeroed, and an XMP packet is
// overwritten with spaces. The orientation and the time the picture was taken
// stay.
//
// Arguments:
//   - head: the leading bytes of the file, LocationHeadSize or the whole file;
//     changed in place.
//
// Returns:
//   - true when something was blanked.
func ScrubLocation(head []byte) bool {
	if len(head) < 4 || head[0] != 0xFF || head[1] != 0xD8 {
		return false
	}
	changed := false
	for offset := 2; offset+4 <= len(head); {
		if head[offset] != 0xFF {
			return changed
		}
		marker := head[offset+1]
		if marker == 0xDA || marker == 0xD9 {
			return changed
		}
		length := int(binary.BigEndian.Uint16(head[offset+2:]))
		if length < 2 || offset+2+length > len(head) {
			return changed
		}
		payload := head[offset+4 : offset+2+length]
		if marker == 0xE1 {
			switch {
			case len(payload) > 6 && string(payload[:6]) == "Exif\x00\x00":
				changed = scrubGPS(payload[6:]) || changed
			case len(payload) > len(xmpPrefix) && string(payload[:len(xmpPrefix)]) == xmpPrefix:
				for index := len(xmpPrefix); index < len(payload); index++ {
					payload[index] = ' '
				}
				changed = true
			}
		}
		offset += 2 + length
	}
	return changed
}

// typeSizes is the size in bytes of one value of each TIFF type.
var typeSizes = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8, 13: 4}

// scrubGPS empties the GPS directory of an EXIF block: every value kept
// outside its entry is zeroed, then the entries, and the directory is left
// holding none. It reports whether there was a directory to empty.
func scrubGPS(block []byte) bool {
	order, first, ok := tiffHeader(block)
	if !ok {
		return false
	}
	pointer, found := readIFD(block, order, first)[tagGPSIFD]
	if !found {
		return false
	}
	start := int(pointer.uint())
	if start+2 > len(block) {
		return false
	}
	count := int(order.Uint16(block[start:]))
	for index := range count {
		record := start + 2 + index*12
		if record+12 > len(block) {
			break
		}
		size := typeSizes[order.Uint16(block[record+2:])] * int(order.Uint32(block[record+4:]))
		if size > 4 {
			if at := int(order.Uint32(block[record+8:])); at >= 0 && at+size <= len(block) {
				clear(block[at : at+size])
			}
		}
		clear(block[record : record+12])
	}
	order.PutUint16(block[start:], 0)
	return true
}

// LocationFree is a stored file with its head blanked by ScrubLocation, read
// as the file it replaces: the same length, the head from memory and the rest
// from the store.
type LocationFree struct {
	head []byte
	file io.ReadCloser
	size int64
	// pos is where the next read starts; filePos is where the stored file
	// stands, which a seek past the head leaves behind.
	pos     int64
	filePos int64
}

// Errors a seek can fail with.
var (
	errNegativeOffset = errors.New("seek before the start of the file")
	errNotSeekable    = errors.New("the stored file cannot seek")
)

// NewLocationFree - reads the head of a stored JPEG and blanks where it was
// taken.
//
// Arguments:
//   - file: the stored file, positioned at its start; closed with the result.
//   - size: the file's length.
//
// Returns:
//   - the file as it is to be sent.
//   - an error when its head cannot be read.
func NewLocationFree(file io.ReadCloser, size int64) (*LocationFree, error) {
	head := make([]byte, min(size, LocationHeadSize))
	if _, err := io.ReadFull(file, head); err != nil {
		return nil, err
	}
	ScrubLocation(head)
	return &LocationFree{head: head, file: file, size: size, filePos: int64(len(head))}, nil
}

// Read - reads the blanked head, then the rest of the stored file.
//
// Arguments:
//   - buffer: where the bytes go.
//
// Returns:
//   - how many bytes were read.
//   - io.EOF at the end, or the store's error.
func (f *LocationFree) Read(buffer []byte) (int, error) {
	if f.pos >= f.size {
		return 0, io.EOF
	}
	if f.pos < int64(len(f.head)) {
		read := copy(buffer, f.head[f.pos:])
		f.pos += int64(read)
		return read, nil
	}
	if f.filePos != f.pos {
		seeker, ok := f.file.(io.Seeker)
		if !ok {
			return 0, errNotSeekable
		}
		if _, err := seeker.Seek(f.pos, io.SeekStart); err != nil {
			return 0, err
		}
		f.filePos = f.pos
	}
	read, err := f.file.Read(buffer)
	f.pos += int64(read)
	f.filePos = f.pos
	return read, err
}

// Seek - moves to an offset. A stored file that cannot seek itself fails the
// next read that would need it to.
//
// Arguments:
//   - offset, whence: as io.Seeker takes them.
//
// Returns:
//   - the new offset.
//   - an error for an offset before the start.
func (f *LocationFree) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += f.pos
	case io.SeekEnd:
		offset += f.size
	}
	if offset < 0 {
		return 0, errNegativeOffset
	}
	f.pos = offset
	return offset, nil
}

// Close - closes the stored file.
//
// Returns:
//   - the store's error.
func (f *LocationFree) Close() error {
	return f.file.Close()
}
