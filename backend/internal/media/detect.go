package media

import (
	"bytes"
	"net/http"
)

// SniffLength is how many leading bytes DetectContentType needs. The standard
// library reads 512, and every signature checked here sits well inside that.
const SniffLength = 512

// AllowedTypes are the kinds of file this service stores, with the extension
// each one is kept under.
//
// It is an allowlist rather than a blocklist, and SVG is deliberately absent:
// an SVG is a document that can carry script, and it would be served from the
// application's own origin. HEIC and video belong to a later stage, which adds
// the decoding they need.
var AllowedTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// DetectContentType - identifies a file from its leading bytes.
//
// The type declared by the upload is ignored on purpose: a client can claim
// anything, so only the content decides what is stored and served back.
//
// Arguments:
//   - header: the first bytes of the file, at least SniffLength for a reliable
//     answer.
//
// Returns:
//   - the detected media type.
//   - the filename extension to store it under.
//   - false when the type is not one this service accepts.
func DetectContentType(header []byte) (string, string, bool) {
	detected := http.DetectContentType(header)
	// The sniffer answers with parameters for some types, such as
	// "text/plain; charset=utf-8"; only the type itself matters here.
	if index := bytes.IndexByte([]byte(detected), ';'); index >= 0 {
		detected = detected[:index]
	}
	extension, ok := AllowedTypes[detected]
	if !ok {
		return "", "", false
	}
	return detected, extension, true
}
