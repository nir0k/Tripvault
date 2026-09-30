package media

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"io"
	"net/http"
	"path"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Attachments are what a place carries besides its pictures: a ticket, a
// booking, a timetable. They are the kinds of file such a thing arrives as -
// text and office documents, and pictures of them - and nothing else: no
// archive, no program, no video, no sound. The list is an allowlist, and what a
// file is is decided from its own bytes; its name must then agree with that, so
// a script is not let through as a text file nor a workbook as a letter. Office
// documents that carry macros, or files packed inside them, are refused like a
// program would be.

// AttachmentKind is one kind of file a place may carry.
type AttachmentKind struct {
	// MIME is the media type the file is stored and downloaded as.
	MIME string
	// Extensions are the endings a file of this kind may be named with, the
	// usual one first.
	Extensions []string
}

// The kinds of attachment, keyed by what detectAttachment answers.
var attachmentKinds = map[string]AttachmentKind{
	"pdf":  {MIME: "application/pdf", Extensions: []string{".pdf"}},
	"txt":  {MIME: "text/plain; charset=utf-8", Extensions: []string{".txt"}},
	"md":   {MIME: "text/markdown; charset=utf-8", Extensions: []string{".md", ".markdown"}},
	"csv":  {MIME: "text/csv; charset=utf-8", Extensions: []string{".csv"}},
	"ics":  {MIME: "text/calendar; charset=utf-8", Extensions: []string{".ics"}},
	"eml":  {MIME: "message/rfc822", Extensions: []string{".eml"}},
	"html": {MIME: "text/html; charset=utf-8", Extensions: []string{".html", ".htm"}},
	"rtf":  {MIME: "application/rtf", Extensions: []string{".rtf"}},
	"docx": {MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Extensions: []string{".docx"}},
	"xlsx": {MIME: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		Extensions: []string{".xlsx"}},
	"pptx": {MIME: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		Extensions: []string{".pptx"}},
	"odt":  {MIME: "application/vnd.oasis.opendocument.text", Extensions: []string{".odt"}},
	"ods":  {MIME: "application/vnd.oasis.opendocument.spreadsheet", Extensions: []string{".ods"}},
	"odp":  {MIME: "application/vnd.oasis.opendocument.presentation", Extensions: []string{".odp"}},
	"doc":  {MIME: "application/msword", Extensions: []string{".doc"}},
	"xls":  {MIME: "application/vnd.ms-excel", Extensions: []string{".xls"}},
	"ppt":  {MIME: "application/vnd.ms-powerpoint", Extensions: []string{".ppt"}},
	"jpeg": {MIME: "image/jpeg", Extensions: []string{".jpg", ".jpeg"}},
	"png":  {MIME: "image/png", Extensions: []string{".png"}},
	"webp": {MIME: "image/webp", Extensions: []string{".webp"}},
	"avif": {MIME: "image/avif", Extensions: []string{".avif"}},
	"bmp":  {MIME: "image/bmp", Extensions: []string{".bmp"}},
}

// textKinds are the kinds of plain text told apart by the name alone: their
// bytes are all just text.
var textKinds = map[string]string{
	".txt": "txt", ".md": "md", ".markdown": "md", ".csv": "csv", ".ics": "ics", ".eml": "eml",
}

// DetectAttachment - identifies a file a place may carry.
//
// The type the upload declares is ignored: the bytes decide the kind, and the
// name has to end the way that kind is named. Plain text is the one family the
// bytes cannot split further, so there the name chooses among the text kinds,
// once the bytes are known to be text and nothing but text.
//
// Arguments:
//   - data: the whole file; containers are opened and text is read to the end.
//   - name: the name it was uploaded under.
//
// Returns:
//   - the kind of file.
//   - false when it is not a kind a place may carry, or the name disagrees.
func DetectAttachment(data []byte, name string) (AttachmentKind, bool) {
	extension := strings.ToLower(path.Ext(strings.TrimSpace(name)))
	if extension == "" || len(data) == 0 {
		return AttachmentKind{}, false
	}
	key := detectAttachment(data, extension)
	kind, ok := attachmentKinds[key]
	if !ok {
		return AttachmentKind{}, false
	}
	for _, allowed := range kind.Extensions {
		if allowed == extension {
			return kind, true
		}
	}
	return AttachmentKind{}, false
}

// detectAttachment names the kind of the bytes, or answers "" for anything not
// allowed. The extension only chooses among plain text kinds.
func detectAttachment(data []byte, extension string) string {
	switch {
	case isPDF(data):
		return "pdf"
	case bytes.HasPrefix(data, []byte(`{\rtf`)):
		// An RTF may carry an embedded object, which is how a program is
		// packed into one.
		if bytes.Contains(data, []byte(`\objdata`)) {
			return ""
		}
		return "rtf"
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		return detectZipDocument(data)
	case bytes.HasPrefix(data, oleSignature):
		return detectOLEDocument(data)
	case isAVIF(data):
		return "avif"
	}
	switch sniffed, _, _ := strings.Cut(http.DetectContentType(data), ";"); sniffed {
	case "image/jpeg":
		return "jpeg"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "image/bmp":
		return "bmp"
	}
	text, ok := decodeText(data)
	if !ok {
		return ""
	}
	if extension == ".html" || extension == ".htm" {
		if isHTML(text) {
			return "html"
		}
		return ""
	}
	kind, ok := textKinds[extension]
	if !ok {
		return ""
	}
	switch kind {
	case "ics":
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(text)), "BEGIN:VCALENDAR") {
			return ""
		}
	case "eml":
		if !isMailHeader(text) {
			return ""
		}
	}
	return kind
}

// isPDF looks for the PDF header at the start of the file. Readers accept it
// anywhere in the first kilobyte, but a file with something else in front of it
// may be another kind of file with a PDF inside, so only leading whitespace is
// let through.
func isPDF(data []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("%PDF-"))
}

// isAVIF recognises an AVIF picture by its file type box. HEIC is written in
// the same container under brands of its own, which are not accepted.
func isAVIF(data []byte) bool {
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		return false
	}
	brand := string(data[8:12])
	return brand == "avif" || brand == "avis"
}

// decodeText reads a file as text: UTF-8, with or without a byte order mark, or
// UTF-16 behind one. Text is text only when every character is printable or a
// line's own whitespace; a control character is a binary file in disguise. A
// script that names its interpreter on the first line is refused as well, since
// a text file that asks to be run is a program.
func decodeText(data []byte) (string, bool) {
	var text string
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}), bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		body := data[2:]
		if len(body)%2 != 0 {
			return "", false
		}
		order := binary.ByteOrder(binary.LittleEndian)
		if data[0] == 0xFE {
			order = binary.BigEndian
		}
		units := make([]uint16, len(body)/2)
		for index := range units {
			units[index] = order.Uint16(body[index*2:])
		}
		text = string(utf16.Decode(units))
		if strings.ContainsRune(text, utf8.RuneError) {
			return "", false
		}
	default:
		body := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
		if !utf8.Valid(body) {
			return "", false
		}
		text = string(body)
	}
	for _, r := range text {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' && r != '\f' || r == 0x7F {
			return "", false
		}
	}
	if strings.HasPrefix(text, "#!") {
		return "", false
	}
	return text, true
}

// isHTML tells a saved web page from any other text, the way a browser's
// sniffer would.
func isHTML(text string) bool {
	sniffed, _, _ := strings.Cut(http.DetectContentType([]byte(text)), ";")
	return sniffed == "text/html"
}

// isMailHeader checks a message begins with a header field, a name followed by
// a colon, as every saved e-mail does.
func isMailHeader(text string) bool {
	line, _, _ := strings.Cut(strings.TrimLeft(text, "\r\n"), "\n")
	name, _, found := strings.Cut(line, ":")
	if !found || name == "" {
		return false
	}
	for _, r := range name {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	return true
}

// Limits on what is read out of an office document while telling what it is.
// They keep a crafted archive from making the check itself expensive.
const (
	maxZipEntries      = 10000
	maxContentTypesLen = 1 << 20
)

// OOXML main parts: the content type of a document's main part says which
// kind it is. The macro-enabled variants have types of their own and are not
// listed, so a .docm is not a document here.
var ooxmlMainParts = map[string]string{
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml":   "docx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml":         "xlsx",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml": "pptx",
}

// odfTypes are the media types an OpenDocument names in its mimetype entry.
var odfTypes = map[string]string{
	"application/vnd.oasis.opendocument.text":         "odt",
	"application/vnd.oasis.opendocument.spreadsheet":  "ods",
	"application/vnd.oasis.opendocument.presentation": "odp",
}

// embeddedDocument are the endings of the files an office document may carry
// inside it - the workbook behind a chart, a picture - without being refused.
// Anything else embedded, such as a packaged program or an ActiveX control, is
// a file hidden inside the document and is refused with it.
var embeddedDocument = map[string]bool{
	".xlsx": true, ".docx": true, ".pptx": true, ".png": true, ".jpg": true, ".jpeg": true,
	".gif": true, ".emf": true, ".wmf": true, ".svg": true,
}

// detectZipDocument tells an OOXML or OpenDocument file from any other ZIP,
// which is an archive and is refused.
func detectZipDocument(data []byte) string {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(archive.File) == 0 || len(archive.File) > maxZipEntries {
		return ""
	}
	for _, file := range archive.File {
		if carriesProgram(file.Name) {
			return ""
		}
	}
	if first := archive.File[0]; first.Name == "mimetype" {
		value, ok := readZipEntry(first, 256)
		if !ok {
			return ""
		}
		return odfTypes[strings.TrimSpace(string(value))]
	}
	for _, file := range archive.File {
		if file.Name != "[Content_Types].xml" {
			continue
		}
		value, ok := readZipEntry(file, maxContentTypesLen)
		if !ok {
			return ""
		}
		return ooxmlKind(value)
	}
	return ""
}

// carriesProgram reports an entry of an office document that holds code or a
// file packed inside it: VBA projects, OpenDocument Basic and scripts, ActiveX
// controls and embedded objects other than documents and pictures.
func carriesProgram(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "basic/") || strings.HasPrefix(lower, "scripts/") ||
		strings.Contains(lower, "/activex/") || strings.HasSuffix(lower, ".bin") {
		return true
	}
	if strings.Contains(lower, "/embeddings/") && !strings.HasSuffix(lower, "/") {
		return !embeddedDocument[path.Ext(lower)]
	}
	return false
}

// readZipEntry reads one small entry of an archive, refusing one larger than
// limit once uncompressed.
func readZipEntry(file *zip.File, limit int64) ([]byte, bool) {
	reader, err := file.Open()
	if err != nil {
		return nil, false
	}
	defer func() { _ = reader.Close() }()
	value, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(value)) > limit {
		return nil, false
	}
	return value, true
}

// ooxmlKind reads the content types of an OOXML package and names its main
// part. A package that declares a macro project anywhere is refused.
func ooxmlKind(contentTypes []byte) string {
	var types struct {
		Defaults []struct {
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Default"`
		Overrides []struct {
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Override"`
	}
	if err := xml.Unmarshal(contentTypes, &types); err != nil {
		return ""
	}
	kind := ""
	check := func(contentType string) bool {
		lower := strings.ToLower(contentType)
		if strings.Contains(lower, "macroenabled") || strings.Contains(lower, "vbaproject") ||
			strings.Contains(lower, "activex") {
			return false
		}
		if found, ok := ooxmlMainParts[contentType]; ok {
			if kind != "" && kind != found {
				return false
			}
			kind = found
		}
		return true
	}
	for _, entry := range types.Defaults {
		if !check(entry.ContentType) {
			return ""
		}
	}
	for _, entry := range types.Overrides {
		if !check(entry.ContentType) {
			return ""
		}
	}
	return kind
}
