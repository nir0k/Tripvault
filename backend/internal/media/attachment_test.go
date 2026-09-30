package media

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

// zipOf packs entries, in the order given, into a ZIP archive.
func zipOf(t *testing.T, entries ...[2]string) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, entry := range entries {
		file, err := writer.Create(entry[0])
		if err != nil {
			t.Fatalf("create %s: %v", entry[0], err)
		}
		if _, err := file.Write([]byte(entry[1])); err != nil {
			t.Fatalf("write %s: %v", entry[0], err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the archive: %v", err)
	}
	return out.Bytes()
}

// contentTypes is the [Content_Types].xml of an OOXML package whose main part
// has the given type.
func contentTypes(main string) [2]string {
	return [2]string{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="` + main + `"/>
</Types>`}
}

const (
	docxMain = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
	xlsxMain = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"
	pptxMain = "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"
	docmMain = "application/vnd.ms-word.document.macroEnabled.main+xml"
)

// oleStreamOf is one entry of a compound file built by oleOf.
type oleStreamOf struct {
	name    string
	storage bool
	data    []byte
}

// oleOf builds a version 3 compound file holding the given storages and
// streams: streams under the mini stream cut-off live in the mini stream, the
// rest in sectors of their own, as a real writer lays them out.
func oleOf(entries ...oleStreamOf) []byte {
	const sector = 512
	le := binary.LittleEndian
	var sectors [][]byte
	var fat []uint32
	// place appends data as a chain of sectors and returns its first.
	place := func(data []byte) uint32 {
		if len(data) == 0 {
			return oleEndOfChain
		}
		first := uint32(len(sectors) + 1) // sector 0 is the allocation table
		for offset := 0; offset < len(data); offset += sector {
			chunk := make([]byte, sector)
			copy(chunk, data[offset:])
			sectors = append(sectors, chunk)
			fat = append(fat, uint32(len(sectors)+1))
		}
		fat[len(fat)-1] = oleEndOfChain
		return first
	}

	var mini []byte
	var miniFAT []uint32
	starts := make([]uint32, len(entries))
	for index, entry := range entries {
		starts[index] = oleEndOfChain
		if entry.storage || len(entry.data) == 0 || len(entry.data) >= oleMiniStreamCutoff {
			continue
		}
		starts[index] = uint32(len(mini) / 64)
		for offset := 0; offset < len(entry.data); offset += 64 {
			chunk := make([]byte, 64)
			copy(chunk, entry.data[offset:])
			mini = append(mini, chunk...)
			miniFAT = append(miniFAT, uint32(len(mini)/64))
		}
		miniFAT[len(miniFAT)-1] = oleEndOfChain
	}
	for index, entry := range entries {
		if !entry.storage && len(entry.data) >= oleMiniStreamCutoff {
			starts[index] = place(entry.data)
		}
	}
	miniStart := place(mini)
	miniFATBytes := make([]byte, 0, len(miniFAT)*4)
	for _, next := range miniFAT {
		miniFATBytes = le.AppendUint32(miniFATBytes, next)
	}
	miniFATStart := place(miniFATBytes)

	directory := make([]byte, 128*(len(entries)+1))
	writeEntry := func(slot int, name string, kind byte, start uint32, size int) {
		raw := directory[slot*128:]
		units := utf16.Encode([]rune(name))
		for index, unit := range units {
			le.PutUint16(raw[index*2:], unit)
		}
		le.PutUint16(raw[64:], uint16((len(units)+1)*2))
		raw[66] = kind
		le.PutUint32(raw[68:], 0xFFFFFFFF)
		le.PutUint32(raw[72:], 0xFFFFFFFF)
		le.PutUint32(raw[76:], 0xFFFFFFFF)
		le.PutUint32(raw[116:], start)
		le.PutUint32(raw[120:], uint32(size))
	}
	writeEntry(0, "Root Entry", oleRoot, miniStart, len(mini))
	for index, entry := range entries {
		kind := oleStream
		if entry.storage {
			kind = oleStorage
		}
		writeEntry(index+1, entry.name, kind, starts[index], len(entry.data))
	}
	directoryStart := place(directory)

	table := make([]byte, sector)
	le.PutUint32(table, 0xFFFFFFFD)
	for index, next := range fat {
		le.PutUint32(table[(index+1)*4:], next)
	}
	for index := len(fat) + 1; index < sector/4; index++ {
		le.PutUint32(table[index*4:], 0xFFFFFFFF)
	}

	header := make([]byte, sector)
	copy(header, oleSignature)
	le.PutUint16(header[24:], 0x3E)
	le.PutUint16(header[26:], 3)
	le.PutUint16(header[28:], 0xFFFE)
	le.PutUint16(header[30:], 9)
	le.PutUint16(header[32:], 6)
	le.PutUint32(header[44:], 1)
	le.PutUint32(header[48:], directoryStart)
	le.PutUint32(header[56:], oleMiniStreamCutoff)
	le.PutUint32(header[60:], miniFATStart)
	le.PutUint32(header[64:], uint32(len(miniFATBytes)+sector-1)/sector)
	le.PutUint32(header[68:], oleEndOfChain)
	le.PutUint32(header[76:], 0)
	for index := 1; index < 109; index++ {
		le.PutUint32(header[76+index*4:], 0xFFFFFFFF)
	}

	out := append(header, table...)
	for _, chunk := range sectors {
		out = append(out, chunk...)
	}
	return out
}

// workbook is an Excel workbook stream with one sheet of the given type,
// padded to size.
func workbook(sheetType byte, size int) []byte {
	le := binary.LittleEndian
	var out []byte
	out = le.AppendUint16(out, 0x0809)
	out = le.AppendUint16(out, 16)
	out = append(out, make([]byte, 16)...)
	out = le.AppendUint16(out, 0x0085)
	out = le.AppendUint16(out, 8)
	out = append(out, 0, 0, 0, 0, 0, sheetType, 1, 0)
	out = le.AppendUint16(out, 0x000A)
	out = le.AppendUint16(out, 0)
	return append(out, make([]byte, size-len(out))...)
}

// presentation is a PowerPoint document stream whose document container holds
// one child of the given record type, padded to size.
func presentation(child uint16, size int) []byte {
	le := binary.LittleEndian
	var inner []byte
	inner = le.AppendUint16(inner, 0)
	inner = le.AppendUint16(inner, child)
	inner = le.AppendUint32(inner, 4)
	inner = append(inner, 1, 2, 3, 4)
	var out []byte
	out = le.AppendUint16(out, 0x000F)
	out = le.AppendUint16(out, 0x03E8)
	out = le.AppendUint32(out, uint32(len(inner)))
	out = append(out, inner...)
	return append(out, make([]byte, size-len(out))...)
}

// TestDetectAttachmentAcceptsDocumentsAndPictures checks every kind a place
// may carry is recognised under the names that kind goes by.
func TestDetectAttachmentAcceptsDocumentsAndPictures(t *testing.T) {
	utf16Text := []byte{0xFF, 0xFE}
	for _, unit := range utf16.Encode([]rune("Билет на паром\r\n")) {
		utf16Text = binary.LittleEndian.AppendUint16(utf16Text, unit)
	}
	cases := []struct {
		name string
		file string
		data []byte
		mime string
	}{
		{"pdf", "ticket.pdf", []byte("%PDF-1.7\n1 0 obj\n"), "application/pdf"},
		{"pdf after whitespace", "ticket.PDF", []byte("\r\n%PDF-1.4\n"), "application/pdf"},
		{"text", "notes.txt", []byte("Booking 42\nGate B\n"), "text/plain; charset=utf-8"},
		{"text with bom", "notes.txt", []byte("\xEF\xBB\xBFБилет\n"), "text/plain; charset=utf-8"},
		{"utf-16 text", "notes.txt", utf16Text, "text/plain; charset=utf-8"},
		{"markdown", "plan.md", []byte("# Day 1\n- ferry\n"), "text/markdown; charset=utf-8"},
		{"csv", "times.csv", []byte("from,to\nOslo,Bergen\n"), "text/csv; charset=utf-8"},
		{"calendar", "flight.ics", []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nEND:VCALENDAR\r\n"),
			"text/calendar; charset=utf-8"},
		{"mail", "confirmation.eml", []byte("From: hotel@example.com\r\nSubject: Booking\r\n\r\nHello"),
			"message/rfc822"},
		{"web page", "booking.html", []byte("<!DOCTYPE html><html><body>Booking</body></html>"),
			"text/html; charset=utf-8"},
		{"web page htm", "booking.htm", []byte("<html><body>Booking</body></html>"), "text/html; charset=utf-8"},
		{"rtf", "letter.rtf", []byte(`{\rtf1\ansi Booking}`), "application/rtf"},
		{"docx", "voucher.docx", zipOf(t, contentTypes(docxMain), [2]string{"word/document.xml", "<w/>"},
			[2]string{"word/media/image1.png", "png"}), attachmentKinds["docx"].MIME},
		{"docx with a chart", "voucher.docx", zipOf(t, contentTypes(docxMain),
			[2]string{"word/embeddings/Microsoft_Excel_Worksheet.xlsx", "PK"}), attachmentKinds["docx"].MIME},
		{"xlsx", "budget.xlsx", zipOf(t, contentTypes(xlsxMain)), attachmentKinds["xlsx"].MIME},
		{"pptx", "route.pptx", zipOf(t, contentTypes(pptxMain)), attachmentKinds["pptx"].MIME},
		{"odt", "voucher.odt", zipOf(t, [2]string{"mimetype", "application/vnd.oasis.opendocument.text"},
			[2]string{"content.xml", "<office/>"}), attachmentKinds["odt"].MIME},
		{"ods", "budget.ods", zipOf(t, [2]string{"mimetype", "application/vnd.oasis.opendocument.spreadsheet"}),
			attachmentKinds["ods"].MIME},
		{"odp", "route.odp", zipOf(t, [2]string{"mimetype", "application/vnd.oasis.opendocument.presentation"}),
			attachmentKinds["odp"].MIME},
		{"doc", "voucher.doc", oleOf(oleStreamOf{name: "WordDocument", data: make([]byte, 5000)},
			oleStreamOf{name: "ObjectPool", storage: true}), "application/msword"},
		{"xls", "budget.xls", oleOf(oleStreamOf{name: "Workbook", data: workbook(0x00, 6000)}),
			"application/vnd.ms-excel"},
		{"xls in the mini stream", "budget.xls", oleOf(oleStreamOf{name: "Book", data: workbook(0x00, 300)}),
			"application/vnd.ms-excel"},
		{"ppt", "route.ppt", oleOf(oleStreamOf{name: "PowerPoint Document", data: presentation(0x0FA0, 5000)}),
			"application/vnd.ms-powerpoint"},
		{"jpeg", "scan.jpg", []byte("\xFF\xD8\xFF\xE0\x00\x10JFIF\x00"), "image/jpeg"},
		{"jpeg long", "scan.jpeg", []byte("\xFF\xD8\xFF\xE0\x00\x10JFIF\x00"), "image/jpeg"},
		{"png", "screen.png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), "image/png"},
		{"webp", "screen.webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "image/webp"},
		{"avif", "screen.avif", []byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00"), "image/avif"},
		{"bmp", "scan.bmp", []byte("BM\x00\x00\x00\x00\x00\x00\x00\x00"), "image/bmp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, ok := DetectAttachment(tc.data, tc.file)
			if !ok {
				t.Fatalf("%s was refused", tc.file)
			}
			if kind.MIME != tc.mime {
				t.Errorf("%s is %q, want %q", tc.file, kind.MIME, tc.mime)
			}
		})
	}
}

// TestDetectAttachmentRefusesEverythingElse checks archives, programs, media
// and pictures of the kinds left out are refused, and that a file cannot pass
// under the name of another kind.
func TestDetectAttachmentRefusesEverythingElse(t *testing.T) {
	cases := []struct {
		name string
		file string
		data []byte
	}{
		{"no extension", "ticket", []byte("%PDF-1.7\n")},
		{"pdf named as a letter", "ticket.docx", []byte("%PDF-1.7\n")},
		{"program named as a pdf", "ticket.pdf", []byte("MZ\x90\x00\x03\x00\x00\x00 %PDF-1.7")},
		{"linux program", "ticket.pdf", []byte("\x7fELF\x02\x01\x01\x00")},
		{"script", "run.sh", []byte("echo hello\n")},
		{"script named as text", "notes.txt", []byte("#!/bin/sh\nrm -rf /\n")},
		{"batch file", "run.bat", []byte("echo hello\r\n")},
		{"binary named as text", "notes.txt", []byte("hello\x00world")},
		{"invalid utf-8", "notes.txt", []byte("hello \xFF\xFE\xFD")},
		{"text named as a web page", "booking.html", []byte("Just a note\n")},
		{"calendar without a calendar", "flight.ics", []byte("Flight at 10:00\n")},
		{"mail without headers", "mail.eml", []byte("Hello there\n")},
		{"svg", "map.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
		{"rtf with an object", "letter.rtf", []byte(`{\rtf1{\object\objemb{\objdata 0102}}}`)},
		{"zip archive", "photos.zip", zipOf(t, [2]string{"a.txt", "a"})},
		{"zip named as a letter", "voucher.docx", zipOf(t, [2]string{"a.txt", "a"})},
		{"docx named as a workbook", "budget.xlsx", zipOf(t, contentTypes(docxMain))},
		{"docm", "voucher.docx", zipOf(t, contentTypes(docmMain))},
		{"docx with a vba project", "voucher.docx", zipOf(t, contentTypes(docxMain),
			[2]string{"word/vbaProject.bin", "vba"})},
		{"docx with a packaged file", "voucher.docx", zipOf(t, contentTypes(docxMain),
			[2]string{"word/embeddings/oleObject1.bin", "MZ"})},
		{"docx with an embedded program", "voucher.docx", zipOf(t, contentTypes(docxMain),
			[2]string{"word/embeddings/setup.exe", "MZ"})},
		{"odt with basic macros", "voucher.odt", zipOf(t,
			[2]string{"mimetype", "application/vnd.oasis.opendocument.text"},
			[2]string{"Basic/Standard/Module1.xml", "<script/>"})},
		{"odt of another kind", "drawing.odt", zipOf(t,
			[2]string{"mimetype", "application/vnd.oasis.opendocument.graphics"})},
		{"doc with macros", "voucher.doc", oleOf(oleStreamOf{name: "WordDocument", data: make([]byte, 5000)},
			oleStreamOf{name: "Macros", storage: true})},
		{"doc with a packaged file", "voucher.doc", oleOf(oleStreamOf{name: "WordDocument", data: make([]byte, 5000)},
			oleStreamOf{name: "\x01Ole10Native", data: []byte("MZ")})},
		{"xls with a vba project", "budget.xls", oleOf(oleStreamOf{name: "Workbook", data: workbook(0, 6000)},
			oleStreamOf{name: "_VBA_PROJECT_CUR", storage: true})},
		{"xls with an excel 4 macro sheet", "budget.xls", oleOf(oleStreamOf{name: "Workbook",
			data: workbook(0x01, 6000)})},
		{"xls macro sheet in the mini stream", "budget.xls", oleOf(oleStreamOf{name: "Workbook",
			data: workbook(0x01, 300)})},
		{"ppt with a stored object", "route.ppt", oleOf(oleStreamOf{name: "PowerPoint Document",
			data: presentation(0x1011, 5000)})},
		{"installer named as a letter", "voucher.doc", oleOf(oleStreamOf{name: "䡀㼿䕷",
			data: make([]byte, 5000)})},
		{"doc named as a workbook", "budget.xls", oleOf(oleStreamOf{name: "WordDocument", data: make([]byte, 5000)})},
		{"truncated ole", "voucher.doc", oleSignature},
		{"gif", "scan.gif", []byte("GIF89a\x01\x00\x01\x00")},
		{"gif named as png", "scan.png", []byte("GIF89a\x01\x00\x01\x00")},
		{"heic", "photo.heic", []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00")},
		{"heic named as avif", "photo.avif", []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00")},
		{"tiff and raw", "photo.cr2", []byte("II*\x00\x10\x00\x00\x00CR\x02\x00")},
		{"tiff", "scan.tiff", []byte("MM\x00*\x00\x00\x00\x08")},
		{"video", "clip.mp4", []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2")},
		{"audio", "voice.mp3", []byte("ID3\x03\x00\x00\x00\x00\x00\x00")},
		{"gzip", "notes.txt", []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00")},
		{"empty", "notes.txt", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if kind, ok := DetectAttachment(tc.data, tc.file); ok {
				t.Errorf("%s was accepted as %q", tc.file, kind.MIME)
			}
		})
	}
}
