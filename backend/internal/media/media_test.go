package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pngBytes renders a picture of the given size, which is what the tests upload.
func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			picture.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, picture); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return out.Bytes()
}

// TestDetectContentTypeAcceptsPicturesOnly checks the allowlist: the three
// kinds the service stores are recognised from their bytes, and a file that
// only claims to be a picture is refused.
func TestDetectContentTypeAcceptsPicturesOnly(t *testing.T) {
	accepted := map[string]struct {
		data      []byte
		mime      string
		extension string
	}{
		"png":  {pngBytes(t, 4, 4), "image/png", ".png"},
		"jpeg": {jpegBytes(t, 4, 4), "image/jpeg", ".jpg"},
		"webp": {webpHeader(), "image/webp", ".webp"},
	}
	for name, want := range accepted {
		mime, extension, ok := DetectContentType(want.data)
		if !ok || mime != want.mime || extension != want.extension {
			t.Errorf("%s: got %q %q %v, want %q %q true", name, mime, extension, ok, want.mime, want.extension)
		}
	}

	refused := map[string][]byte{
		"pdf":   []byte("%PDF-1.7\n"),
		"svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"html":  []byte("<!DOCTYPE html><html><body>hello</body></html>"),
		"empty": {},
	}
	for name, data := range refused {
		if mime, _, ok := DetectContentType(data); ok {
			t.Errorf("%s was accepted as %q", name, mime)
		}
	}
}

// jpegBytes renders a JPEG of the given size.
func jpegBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	var out bytes.Buffer
	if err := jpeg.Encode(&out, picture, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return out.Bytes()
}

// webpHeader builds the leading bytes of a WebP file, which is all the sniffer
// looks at.
func webpHeader() []byte {
	header := make([]byte, 16)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], 8)
	copy(header[8:], "WEBPVP8 ")
	return header
}

// TestThumbnailFitsTheRequestedWidth checks a preview is never wider than
// asked for, and that a picture smaller than the request is left at its size
// rather than blown up.
func TestThumbnailFitsTheRequestedWidth(t *testing.T) {
	preview, err := Thumbnail(pngBytes(t, 800, 400), 320)
	if err != nil {
		t.Fatalf("render preview: %v", err)
	}
	width, height, err := Dimensions(preview)
	if err != nil {
		t.Fatalf("read preview size: %v", err)
	}
	if width != 320 || height != 160 {
		t.Errorf("preview is %dx%d, want 320x160", width, height)
	}

	small, err := Thumbnail(pngBytes(t, 100, 50), 320)
	if err != nil {
		t.Fatalf("render small preview: %v", err)
	}
	if width, _, _ := Dimensions(small); width != 100 {
		t.Errorf("a picture of 100px came back at %dpx", width)
	}

	if _, err := Thumbnail([]byte("not a picture"), 320); err != ErrNoThumbnail {
		t.Errorf("a file that is not a picture gave %v, want ErrNoThumbnail", err)
	}
}

// TestPreviewsRenderEveryWidth checks a camera-sized JPEG comes
// back at every offered width with its proportions kept, and that scaling a
// flat picture down leaves its colour where it was.
func TestPreviewsRenderEveryWidth(t *testing.T) {
	picture := image.NewRGBA(image.Rect(0, 0, 3001, 2001))
	fill := color.RGBA{R: 200, G: 90, B: 40, A: 255}
	for y := range 2001 {
		for x := range 3001 {
			picture.SetRGBA(x, y, fill)
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	previews, err := Previews(encoded.Bytes())
	if err != nil {
		t.Fatalf("render previews: %v", err)
	}
	for _, size := range Sizes {
		preview, err := jpeg.Decode(bytes.NewReader(previews[size]))
		if err != nil {
			t.Fatalf("decode the %d preview: %v", size, err)
		}
		bounds := preview.Bounds()
		// The height is rounded, so it may land a pixel either side of the
		// exact proportion.
		if height := 2001 * size / 3001; bounds.Dx() != size || bounds.Dy() < height || bounds.Dy() > height+1 {
			t.Errorf("the %d preview is %dx%d", size, bounds.Dx(), bounds.Dy())
		}
		r, g, b, _ := preview.At(bounds.Dx()/2, bounds.Dy()/2).RGBA()
		got := [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
		want := [3]int{int(fill.R), int(fill.G), int(fill.B)}
		for c := range got {
			if got[c]-want[c] > 6 || want[c]-got[c] > 6 {
				t.Errorf("the %d preview is %v in the middle, want about %v", size, got, want)
				break
			}
		}
	}

	if _, err := Previews([]byte("not a picture")); err != ErrNoThumbnail {
		t.Errorf("a file that is not a picture gave %v, want ErrNoThumbnail", err)
	}
}

// TestCropCutsTheFrame checks that a cover's frame is cut out at the share of
// the preview it names, and that rubbish is refused rather than passed on.
func TestCropCutsTheFrame(t *testing.T) {
	cropped, err := Crop(jpegBytes(t, 400, 200), 0.25, 0.5, 0.5, 0.5)
	if err != nil {
		t.Fatalf("crop: %v", err)
	}
	width, height, err := Dimensions(cropped)
	if err != nil {
		t.Fatalf("read cropped size: %v", err)
	}
	if width != 200 || height != 100 {
		t.Errorf("frame is %dx%d, want 200x100", width, height)
	}

	if _, err := Crop([]byte("not a picture"), 0, 0, 1, 1); err != ErrNoThumbnail {
		t.Errorf("a file that is not a picture gave %v, want ErrNoThumbnail", err)
	}
}

// TestHasSizeAcceptsOfferedWidthsOnly keeps the preview sizes a closed set, so
// a request cannot ask the service to render an arbitrary size.
func TestHasSizeAcceptsOfferedWidthsOnly(t *testing.T) {
	for _, size := range Sizes {
		if !HasSize(size) {
			t.Errorf("%d is offered but not accepted", size)
		}
	}
	for _, size := range []int{0, -1, 100, 321, 4000} {
		if HasSize(size) {
			t.Errorf("%d is not offered but was accepted", size)
		}
	}
}

// exifJPEG builds a JPEG carrying an EXIF block with the orientation, the
// capture time and the position, which is what a camera writes.
func exifJPEG(t *testing.T, orientation int) []byte {
	t.Helper()
	var block bytes.Buffer
	block.WriteString("II")                                   // little-endian
	_ = binary.Write(&block, binary.LittleEndian, uint16(42)) // magic
	_ = binary.Write(&block, binary.LittleEndian, uint32(8))  // first directory

	// The directories are laid out after the entries: IFD0 with three entries,
	// then the Exif and GPS directories, then the values they point at.
	const (
		ifd0Offset = 8
		exifOffset = 8 + 2 + 3*12 + 4
		gpsOffset  = exifOffset + 2 + 2*12 + 4
		dateOffset = gpsOffset + 2 + 4*12 + 4
		offsetTime = dateOffset + 20
		latOffset  = offsetTime + 8
		lngOffset  = latOffset + 24
	)

	entry := func(buf *bytes.Buffer, tag, kind uint16, count, value uint32) {
		_ = binary.Write(buf, binary.LittleEndian, tag)
		_ = binary.Write(buf, binary.LittleEndian, kind)
		_ = binary.Write(buf, binary.LittleEndian, count)
		_ = binary.Write(buf, binary.LittleEndian, value)
	}

	_ = binary.Write(&block, binary.LittleEndian, uint16(3)) // IFD0 entries
	entry(&block, tagOrientation, typeShort, 1, uint32(orientation))
	entry(&block, tagExifIFD, typeLong, 1, exifOffset)
	entry(&block, tagGPSIFD, typeLong, 1, gpsOffset)
	_ = binary.Write(&block, binary.LittleEndian, uint32(0)) // no next directory

	_ = binary.Write(&block, binary.LittleEndian, uint16(2)) // Exif entries
	entry(&block, tagDateTimeOriginal, typeASCII, 20, dateOffset)
	entry(&block, tagOffsetTimeOriginal, typeASCII, 7, offsetTime)
	_ = binary.Write(&block, binary.LittleEndian, uint32(0))

	_ = binary.Write(&block, binary.LittleEndian, uint16(4)) // GPS entries
	entry(&block, tagGPSLatitudeRef, typeASCII, 2, uint32('N'))
	entry(&block, tagGPSLatitude, typeRational, 3, latOffset)
	entry(&block, tagGPSLongitudeRef, typeASCII, 2, uint32('W'))
	entry(&block, tagGPSLongitude, typeRational, 3, lngOffset)
	_ = binary.Write(&block, binary.LittleEndian, uint32(0))

	block.WriteString("2026:06:21 14:30:05\x00")
	block.WriteString("+02:00\x00\x00")
	for _, part := range []uint32{64, 1, 8, 1, 30, 1} { // 64° 8' 30" N
		_ = binary.Write(&block, binary.LittleEndian, part)
	}
	for _, part := range []uint32{21, 1, 56, 1, 0, 1} { // 21° 56' 0" W
		_ = binary.Write(&block, binary.LittleEndian, part)
	}

	payload := append([]byte("Exif\x00\x00"), block.Bytes()...)
	picture := jpegBytes(t, 8, 4)
	// The APP1 segment goes straight after the start-of-image marker. The
	// buffer starts empty on purpose: one built on picture[:2] would share that
	// slice's array and overwrite the picture as it grew.
	out := &bytes.Buffer{}
	out.Write(picture[:2])
	out.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(picture[2:])
	return out.Bytes()
}

// TestReadMetadataReadsWhatACameraWrites covers the three tags the service
// stores: which way up the picture is, when it was taken and where.
func TestReadMetadataReadsWhatACameraWrites(t *testing.T) {
	meta := ReadMetadata(exifJPEG(t, 6))
	if meta.Orientation != 6 {
		t.Errorf("orientation %d, want 6", meta.Orientation)
	}
	if meta.TakenAt == nil {
		t.Fatal("the capture time was not read")
	}
	want := time.Date(2026, 6, 21, 12, 30, 5, 0, time.UTC)
	if !meta.TakenAt.Equal(want) {
		t.Errorf("taken at %s, want %s", meta.TakenAt, want)
	}
	if meta.Lat == nil || meta.Lng == nil {
		t.Fatal("the position was not read")
	}
	if diff := *meta.Lat - 64.141666; diff > 0.001 || diff < -0.001 {
		t.Errorf("latitude %f, want about 64.1417", *meta.Lat)
	}
	if diff := *meta.Lng + 21.933333; diff > 0.001 || diff < -0.001 {
		t.Errorf("longitude %f, want about -21.9333", *meta.Lng)
	}
}

// TestReadMetadataSurvivesRubbish checks a file without EXIF, and a damaged
// one, come back as "nothing known" rather than as an error.
func TestReadMetadataSurvivesRubbish(t *testing.T) {
	for name, data := range map[string][]byte{
		"png":       pngBytes(t, 4, 4),
		"plain":     jpegBytes(t, 4, 4),
		"truncated": exifJPEG(t, 3)[:40],
		"garbage":   {0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x08, 'E', 'x', 'i', 'f'},
	} {
		meta := ReadMetadata(data)
		if meta.Orientation != 1 || meta.TakenAt != nil || meta.Lat != nil {
			t.Errorf("%s: got %+v, want an empty reading", name, meta)
		}
	}
}

// TestThumbnailTurnsThePictureUpright checks a frame stored on its side comes
// back the way round the camera says it should be seen.
func TestThumbnailTurnsThePictureUpright(t *testing.T) {
	preview, err := Thumbnail(exifJPEG(t, 6), 160)
	if err != nil {
		t.Fatalf("render preview: %v", err)
	}
	width, height, err := Dimensions(preview)
	if err != nil {
		t.Fatalf("read preview size: %v", err)
	}
	// The picture is 8x4 and the orientation turns it a quarter, so the preview
	// is taller than it is wide.
	if width >= height {
		t.Errorf("preview is %dx%d; a quarter-turned picture should be taller than wide", width, height)
	}
}

// TestLocalStoreKeepsWhatItIsGiven covers the round trip and the two rules the
// store adds: a key cannot climb out of the root, and removing a file that is
// not there is not a failure.
func TestLocalStoreKeepsWhatItIsGiven(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx := context.Background()
	data := pngBytes(t, 8, 8)

	written, err := store.Put(ctx, "trip/file.png", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("store file: %v", err)
	}
	if written != int64(len(data)) {
		t.Errorf("stored %d bytes, want %d", written, len(data))
	}

	reader, err := store.Open(ctx, "trip/file.png")
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	read := new(bytes.Buffer)
	_, _ = read.ReadFrom(reader)
	_ = reader.Close()
	if !bytes.Equal(read.Bytes(), data) {
		t.Error("the file came back different from what was stored")
	}

	if _, err := store.Open(ctx, "trip/missing.png"); err != ErrNotFound {
		t.Errorf("a missing file gave %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, "trip/missing.png"); err != nil {
		t.Errorf("removing a missing file failed: %v", err)
	}
	if err := store.Delete(ctx, "trip/file.png"); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "trip", "file.png")); !os.IsNotExist(err) {
		t.Error("the file is still on disk after being deleted")
	}

	// A key that tries to climb out of the root is written inside it instead.
	if _, err := store.Put(ctx, "../escape.png", bytes.NewReader(data)); err != nil {
		t.Fatalf("store an escaping key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.png")); err == nil {
		t.Error("a key climbed out of the store root")
	}
	if err := store.Check(ctx); err != nil {
		t.Errorf("check a usable store: %v", err)
	}
}

// TestRestoreStageCanRollBack checks both originals and generated previews
// return when database activation fails after the media cutover.
func TestRestoreStageCanRollBack(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx := context.Background()
	if _, err := store.Put(ctx, "old/photo.jpg", strings.NewReader("old")); err != nil {
		t.Fatalf("store old media: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(store.root, ".previews", "old"), 0o750); err != nil {
		t.Fatalf("make preview directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.root, ".previews", "old", "photo.jpg"), []byte("preview"), 0o640); err != nil {
		t.Fatalf("store old preview: %v", err)
	}

	stage, err := store.BeginRestore(ctx)
	if err != nil {
		t.Fatalf("begin restore: %v", err)
	}
	defer stage.Close(ctx)
	if _, err := stage.Put(ctx, "new/photo.jpg", strings.NewReader("new")); err != nil {
		t.Fatalf("stage new media: %v", err)
	}
	if err := stage.Activate(ctx); err != nil {
		t.Fatalf("activate stage: %v", err)
	}
	if _, err := store.Open(ctx, "old/photo.jpg"); err != ErrNotFound {
		t.Errorf("old media remained visible: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.root, ".previews", "old", "photo.jpg")); !os.IsNotExist(err) {
		t.Errorf("old preview remained visible: %v", err)
	}

	if err := stage.Rollback(ctx); err != nil {
		t.Fatalf("roll back stage: %v", err)
	}
	reader, err := store.Open(ctx, "old/photo.jpg")
	if err != nil {
		t.Fatalf("old media was not restored: %v", err)
	}
	body, _ := io.ReadAll(reader)
	_ = reader.Close()
	if string(body) != "old" {
		t.Errorf("old media is %q", body)
	}
	if _, err := os.Stat(filepath.Join(store.root, ".previews", "old", "photo.jpg")); err != nil {
		t.Errorf("old preview was not restored: %v", err)
	}
}
