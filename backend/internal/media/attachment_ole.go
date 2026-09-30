package media

import (
	"encoding/binary"
	"unicode/utf16"
)

// The legacy office formats - .doc, .xls, .ppt - are streams inside an OLE
// compound file, a small filesystem of its own. The same container carries
// Windows installers and other things that are not documents, so a file is
// taken for a document only when it holds the stream that kind of document
// keeps its text in, and refused when it holds a macro project, a packaged
// file, an Excel 4.0 macro sheet or a PowerPoint VBA project.
//
// Only what telling them apart needs is read: the directory, and the one
// stream of a workbook or a presentation that says whether it carries code.

// oleSignature starts every OLE compound file.
var oleSignature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// Sector numbers that end a chain or mark sectors that hold no data.
const (
	oleMaxRegular uint32 = 0xFFFFFFFA
	oleEndOfChain uint32 = 0xFFFFFFFE
)

// oleMiniStreamCutoff is the size below which a stream lives in the mini
// stream rather than in sectors of its own.
const oleMiniStreamCutoff = 4096

// Directory entry types.
const (
	oleStorage byte = 1
	oleStream  byte = 2
	oleRoot    byte = 5
)

// oleForbidden are the names of storages and streams that carry code or a
// packaged file: Word's and Excel's VBA projects and the packager's native
// data, in which any file at all may be embedded.
var oleForbidden = map[string]bool{
	"Macros":           true,
	"_VBA_PROJECT_CUR": true,
	"_VBA_PROJECT":     true,
	"VBA":              true,
	"\x01Ole10Native":  true,
}

// oleEntry is one entry of the directory.
type oleEntry struct {
	name  string
	kind  byte
	start uint32
	size  uint64
}

// oleFile is an opened compound file.
type oleFile struct {
	data       []byte
	sectorSize int
	fat        []uint32
	miniFAT    []uint32
	miniStream []byte
	entries    []oleEntry
}

// detectOLEDocument names the legacy office document an OLE file is, or ""
// when it is none, cannot be read, or carries code.
func detectOLEDocument(data []byte) string {
	file, ok := openOLE(data)
	if !ok {
		return ""
	}
	streams := map[string]oleEntry{}
	for _, entry := range file.entries {
		if oleForbidden[entry.name] {
			return ""
		}
		if entry.kind == oleStream {
			streams[entry.name] = entry
		}
	}
	if _, ok := streams["WordDocument"]; ok {
		return "doc"
	}
	for _, name := range []string{"Workbook", "Book"} {
		if entry, ok := streams[name]; ok {
			stream, ok := file.stream(entry)
			if !ok || workbookCarriesMacros(stream) {
				return ""
			}
			return "xls"
		}
	}
	if entry, ok := streams["PowerPoint Document"]; ok {
		stream, ok := file.stream(entry)
		if !ok || presentationCarriesObjects(stream, 0) {
			return ""
		}
		return "ppt"
	}
	return ""
}

// openOLE reads the header, the allocation tables and the directory of a
// compound file. Every chain is bounded by the number of sectors in the file,
// so a crafted loop ends rather than spins.
func openOLE(data []byte) (*oleFile, bool) {
	if len(data) < 512 {
		return nil, false
	}
	le := binary.LittleEndian
	shift := le.Uint16(data[30:])
	if shift != 9 && shift != 12 || le.Uint16(data[28:]) != 0xFFFE {
		return nil, false
	}
	file := &oleFile{data: data, sectorSize: 1 << shift}
	perSector := file.sectorSize / 4

	// The sectors of the allocation table are listed by the header's first
	// 109 entries and then by a chain of DIFAT sectors.
	fatCount := int(le.Uint32(data[44:]))
	if fatCount > file.sectorCount() {
		return nil, false
	}
	fatSectors := make([]uint32, 0, fatCount)
	for index := 0; index < 109 && len(fatSectors) < fatCount; index++ {
		fatSectors = append(fatSectors, le.Uint32(data[76+index*4:]))
	}
	next := le.Uint32(data[68:])
	for steps := 0; len(fatSectors) < fatCount && next <= oleMaxRegular; steps++ {
		sector, ok := file.sector(next)
		if !ok || steps > file.sectorCount() {
			return nil, false
		}
		for index := 0; index < perSector-1 && len(fatSectors) < fatCount; index++ {
			fatSectors = append(fatSectors, le.Uint32(sector[index*4:]))
		}
		next = le.Uint32(sector[(perSector-1)*4:])
	}
	if len(fatSectors) < fatCount {
		return nil, false
	}
	for _, number := range fatSectors {
		sector, ok := file.sector(number)
		if !ok {
			return nil, false
		}
		for index := 0; index < perSector; index++ {
			file.fat = append(file.fat, le.Uint32(sector[index*4:]))
		}
	}

	directory, ok := file.chain(le.Uint32(data[48:]), -1)
	if !ok {
		return nil, false
	}
	for offset := 0; offset+128 <= len(directory); offset += 128 {
		raw := directory[offset : offset+128]
		kind := raw[66]
		if kind != oleStorage && kind != oleStream && kind != oleRoot {
			continue
		}
		length := int(le.Uint16(raw[64:]))
		if length < 2 || length > 64 || length%2 != 0 {
			return nil, false
		}
		units := make([]uint16, length/2-1)
		for index := range units {
			units[index] = le.Uint16(raw[index*2:])
		}
		size := le.Uint64(raw[120:])
		if file.sectorSize == 512 {
			// Version 3 files leave the upper half undefined.
			size &= 0xFFFFFFFF
		}
		file.entries = append(file.entries, oleEntry{
			name: string(utf16.Decode(units)), kind: kind, start: le.Uint32(raw[116:]), size: size,
		})
	}

	// Small streams live in the mini stream, which is the root's own data,
	// allocated in 64-byte pieces by the mini allocation table.
	if miniStart := le.Uint32(data[60:]); miniStart <= oleMaxRegular {
		table, ok := file.chain(miniStart, -1)
		if !ok {
			return nil, false
		}
		for index := 0; index+4 <= len(table); index += 4 {
			file.miniFAT = append(file.miniFAT, le.Uint32(table[index:]))
		}
	}
	for _, entry := range file.entries {
		if entry.kind == oleRoot && entry.start <= oleMaxRegular {
			file.miniStream, ok = file.chain(entry.start, int64(entry.size))
			if !ok {
				return nil, false
			}
		}
	}
	return file, true
}

// sectorCount is how many whole sectors follow the header.
func (f *oleFile) sectorCount() int {
	return len(f.data)/f.sectorSize - 1
}

// sector returns the bytes of one sector.
func (f *oleFile) sector(number uint32) ([]byte, bool) {
	if number > oleMaxRegular {
		return nil, false
	}
	start := (int64(number) + 1) * int64(f.sectorSize)
	end := start + int64(f.sectorSize)
	if end > int64(len(f.data)) {
		return nil, false
	}
	return f.data[start:end], true
}

// chain reads a run of sectors from the allocation table, cut to size when a
// size is given (not negative).
func (f *oleFile) chain(start uint32, size int64) ([]byte, bool) {
	var out []byte
	for number, steps := start, 0; number != oleEndOfChain; steps++ {
		if steps > f.sectorCount() || int(number) >= len(f.fat) {
			return nil, false
		}
		sector, ok := f.sector(number)
		if !ok {
			return nil, false
		}
		out = append(out, sector...)
		if size >= 0 && int64(len(out)) >= size {
			return out[:size], true
		}
		number = f.fat[number]
	}
	if size > int64(len(out)) {
		return nil, false
	}
	if size >= 0 {
		out = out[:size]
	}
	return out, true
}

// stream reads the bytes of one stream, from its own sectors or from the mini
// stream when it is small.
func (f *oleFile) stream(entry oleEntry) ([]byte, bool) {
	if entry.size > uint64(len(f.data)) {
		return nil, false
	}
	if entry.size >= oleMiniStreamCutoff {
		return f.chain(entry.start, int64(entry.size))
	}
	var out []byte
	for number, steps := entry.start, 0; uint64(len(out)) < entry.size; steps++ {
		if steps > len(f.miniFAT) || int(number) >= len(f.miniFAT) {
			return nil, false
		}
		start := int(number) * 64
		if start+64 > len(f.miniStream) {
			return nil, false
		}
		out = append(out, f.miniStream[start:start+64]...)
		number = f.miniFAT[number]
	}
	return out[:entry.size], true
}

// workbookCarriesMacros walks the records of an Excel workbook stream and
// reports a sheet that is an Excel 4.0 macro sheet or a VBA module, which keep
// their code in the stream itself rather than in a project of its own.
func workbookCarriesMacros(stream []byte) bool {
	const boundSheet = 0x0085
	le := binary.LittleEndian
	for offset := 0; offset+4 <= len(stream); {
		kind := le.Uint16(stream[offset:])
		length := int(le.Uint16(stream[offset+2:]))
		body := offset + 4
		if body+length > len(stream) {
			return false
		}
		if kind == boundSheet && length >= 6 {
			if sheet := stream[body+5]; sheet == 0x01 || sheet == 0x06 {
				return true
			}
		}
		offset = body + length
	}
	return false
}

// presentationCarriesObjects walks the records of a PowerPoint document stream
// and reports a stored OLE object or VBA project, which PowerPoint keeps inside
// the stream in records of one type. Only containers are walked into; depth
// bounds a crafted nesting.
func presentationCarriesObjects(stream []byte, depth int) bool {
	const objectStorage = 0x1011
	if depth > 32 {
		return true
	}
	le := binary.LittleEndian
	for offset := 0; offset+8 <= len(stream); {
		header := le.Uint16(stream[offset:])
		kind := le.Uint16(stream[offset+2:])
		length := int64(le.Uint32(stream[offset+4:]))
		body := offset + 8
		if int64(body)+length > int64(len(stream)) {
			return false
		}
		if kind == objectStorage {
			return true
		}
		if header&0x0F == 0x0F && presentationCarriesObjects(stream[body:body+int(length)], depth+1) {
			return true
		}
		offset = body + int(length)
	}
	return false
}
