package media

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// TestScrubLocationBlanksThePositionOnly checks the position is gone while the
// orientation and the capture time stay, and the file keeps its length.
func TestScrubLocationBlanksThePositionOnly(t *testing.T) {
	original := exifJPEG(t, 6)
	data := bytes.Clone(original)
	if !ScrubLocation(data) {
		t.Fatal("nothing was blanked")
	}
	if len(data) != len(original) {
		t.Fatalf("length %d, want %d", len(data), len(original))
	}
	meta := ReadMetadata(data)
	if meta.Lat != nil || meta.Lng != nil {
		t.Errorf("the position survived: %v, %v", *meta.Lat, *meta.Lng)
	}
	if meta.Orientation != 6 || meta.TakenAt == nil {
		t.Errorf("orientation %d, taken at %v; both should stay", meta.Orientation, meta.TakenAt)
	}
	// The rationals of the latitude, 64° 8' 30", must not be left in the bytes.
	var latitude bytes.Buffer
	for _, part := range []uint32{64, 1, 8, 1, 30, 1} {
		_ = binary.Write(&latitude, binary.LittleEndian, part)
	}
	if bytes.Contains(data, latitude.Bytes()) {
		t.Error("the latitude is still in the file")
	}
}

// TestScrubLocationBlanksXMP checks an XMP packet, where an editor may have
// written the position, is overwritten.
func TestScrubLocationBlanksXMP(t *testing.T) {
	picture := jpegBytes(t, 8, 4)
	payload := []byte(xmpPrefix + `<x:xmpmeta><exif:GPSLatitude>64,8.5N</exif:GPSLatitude></x:xmpmeta>`)
	var out bytes.Buffer
	out.Write(picture[:2])
	out.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(picture[2:])
	data := out.Bytes()

	if !ScrubLocation(data) {
		t.Fatal("nothing was blanked")
	}
	if bytes.Contains(data, []byte("GPSLatitude")) {
		t.Error("the XMP position survived")
	}
}

// TestScrubLocationLeavesOtherFilesAlone checks a picture without a position,
// and a file that is no JPEG, come back untouched.
func TestScrubLocationLeavesOtherFilesAlone(t *testing.T) {
	for name, data := range map[string][]byte{
		"jpeg without exif": jpegBytes(t, 8, 4),
		"png":               pngBytes(t, 8, 4),
	} {
		original := bytes.Clone(data)
		if ScrubLocation(data) || !bytes.Equal(data, original) {
			t.Errorf("%s: the file was changed", name)
		}
	}
}

// TestLocationFreeReadsAndSeeksLikeTheFile checks the blanked file reads whole
// and from any offset as the scrubbed bytes would, past the head included.
func TestLocationFreeReadsAndSeeksLikeTheFile(t *testing.T) {
	original := exifJPEG(t, 1)
	want := bytes.Clone(original)
	ScrubLocation(want)

	open := func() *LocationFree {
		file, err := NewLocationFree(nopCloser{bytes.NewReader(original)}, int64(len(original)))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return file
	}
	whole, err := io.ReadAll(open())
	if err != nil || !bytes.Equal(whole, want) {
		t.Fatalf("read whole: %v, equal %v", err, bytes.Equal(whole, want))
	}

	file := open()
	file.head = file.head[:20] // a short head, so the offset falls past it
	file.filePos = 20
	if _, err := file.Seek(30, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	rest, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(rest, original[30:]) {
		t.Errorf("read after seek: %v, equal %v", err, bytes.Equal(rest, original[30:]))
	}
}

// nopCloser lets a bytes.Reader stand in for a stored file that can seek.
type nopCloser struct{ *bytes.Reader }

// Close does nothing.
func (nopCloser) Close() error { return nil }

// TestLocationFreeScrubsExtendedXMPLateInTheHeader checks extended packets and
// marker padding after more than a megabyte of metadata cannot bypass privacy.
func TestLocationFreeScrubsExtendedXMPLateInTheHeader(t *testing.T) {
	picture := jpegBytes(t, 8, 4)
	var out bytes.Buffer
	out.Write(picture[:2])
	for range 20 {
		out.Write([]byte{0xff, 0xe2, 0xff, 0xff})
		out.Write(make([]byte, 65533))
	}
	payload := []byte(extendedXMPPrefix + "GPSLatitude=home")
	out.Write([]byte{0xff, 0xff, 0xe1})
	_ = binary.Write(&out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(picture[2:])
	original := out.Bytes()
	file, err := NewLocationFree(nopCloser{bytes.NewReader(original)}, int64(len(original)))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	if err != nil || len(data) != len(original) || bytes.Contains(data, []byte("GPSLatitude")) {
		t.Fatalf("extended metadata survived or file changed length: %v", err)
	}
}
