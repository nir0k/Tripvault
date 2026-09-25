package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder
	_ "image/png"  // registers the PNG decoder
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/davidbyttow/govips/v2/vips"
	_ "golang.org/x/image/webp" // registers the WebP decoder
)

// ErrNoThumbnail reports a file no preview can be made of: a kind this service
// does not decode, or a picture whose declared canvas is too large to decode
// safely.
var ErrNoThumbnail = errors.New("no preview can be made of this file")

// Sizes are the widths a preview is served at. A fixed set keeps the work
// bounded and lets a browser cache what it has already fetched instead of
// asking for one more pixel every time a layout changes. The widest one is for
// a photograph opened across a large or dense screen.
var Sizes = []int{160, 320, 640, 1280, 1920}

// RendererVersion names the way previews are rendered. It is part of every
// preview's storage key and of its ETag, so a change of renderer makes the
// kept previews missing - the walk at start-up renders them again - and every
// browser fetches the new ones instead of keeping the old.
const RendererVersion = 2

// maxPixels bounds the pictures that are decoded. A small file can declare an
// enormous canvas, and decoding it would hold gigabytes; a hundred megapixels
// is already far beyond any camera.
const maxPixels = 100_000_000

// thumbnailQuality is the JPEG quality previews are written at: small enough
// that a gallery loads at once, good enough that a face is a face.
const thumbnailQuality = 82

// HasSize - says whether a width is one of the sizes previews are served at.
//
// Arguments:
//   - width: the requested width in pixels.
//
// Returns:
//   - true when a preview of that width is served.
func HasSize(width int) bool {
	for _, size := range Sizes {
		if size == width {
			return true
		}
	}
	return false
}

// Dimensions - reads the pixel size of a picture without decoding it.
//
// Arguments:
//   - data: the file's bytes.
//
// Returns:
//   - the width and height in pixels.
//   - an error when the bytes are not a picture this service decodes.
func Dimensions(data []byte) (int, int, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, fmt.Errorf("read picture size: %w", err)
	}
	return config.Width, config.Height, nil
}

// Thumbnail - renders a preview of a picture, no wider than the given size.
//
// The picture is turned the way its EXIF orientation says: a camera stores the
// frame as the sensor saw it and records which way up it was, so a preview that
// ignores the tag comes out on its side. A picture already narrower than the
// requested width is scaled up to nothing - it is returned at its own size.
//
// Arguments:
//   - data: the file's bytes.
//   - width: the requested width in pixels.
//
// Returns:
//   - a JPEG of the preview.
//   - ErrNoThumbnail when the file cannot be decoded, or another error.
func Thumbnail(data []byte, width int) ([]byte, error) {
	if width <= 0 {
		return nil, ErrNoThumbnail
	}
	previews, err := render(data, []int{width})
	if err != nil {
		return nil, err
	}
	return previews[width], nil
}

// Previews - renders a picture at every width in Sizes.
//
// Every width is rendered from the original, each decoded at no more of its
// size than that width needs, so no preview is scaled from another one that
// has already lost detail. The previews follow the rules of Thumbnail.
//
// Arguments:
//   - data: the file's bytes.
//
// Returns:
//   - a JPEG for every width in Sizes, keyed by that width.
//   - ErrNoThumbnail when the file cannot be decoded, or another error.
func Previews(data []byte) (map[int][]byte, error) {
	return render(data, Sizes)
}

// renderer holds the one start of libvips a process makes and the logger its
// messages go to.
var renderer struct {
	once   sync.Once
	err    error
	logger atomic.Pointer[slog.Logger]
}

// StartRenderer - starts libvips, which renders every preview.
//
// It is called once at start-up so that a build without a working libvips
// fails there rather than at the first upload; rendering starts it on its own
// when nothing did, which is what the tests rely on. libvips runs each
// operation on one thread and keeps no cache: the service decides how many
// pictures are rendered at once, and a cache of decoded pictures would hold
// memory a small server does not have.
//
// Arguments:
//   - logger: receives the warnings and errors libvips reports.
//
// Returns:
//   - an error when libvips cannot be started.
func StartRenderer(logger *slog.Logger) error {
	renderer.logger.Store(logger)
	return startRenderer()
}

// RendererLibraryVersion - names the libvips the service was built against.
//
// Returns:
//   - the version, such as "8.18.2".
func RendererLibraryVersion() string {
	return vips.Version
}

// startRenderer starts libvips the first time it is called.
func startRenderer() error {
	renderer.once.Do(func() {
		vips.LoggingSettings(logRenderer, vips.LogLevelWarning)
		renderer.err = vips.Startup(&vips.Config{ConcurrencyLevel: 1})
	})
	return renderer.err
}

// logRenderer passes a message of libvips on to the service's log.
func logRenderer(domain string, level vips.LogLevel, message string) {
	logger := renderer.logger.Load()
	if logger == nil {
		logger = slog.Default()
	}
	switch level {
	case vips.LogLevelError, vips.LogLevelCritical:
		logger.Error("libvips reported an error", "domain", domain, "message", message)
	case vips.LogLevelWarning:
		logger.Warn("libvips reported a warning", "domain", domain, "message", message)
	default:
		logger.Debug("libvips reported", "domain", domain, "message", message)
	}
}

// render writes a JPEG of a picture at each width.
//
// libvips does the work: it decodes a JPEG straight at a half, a quarter or an
// eighth of its size when that is still wider than needed - which is what makes
// the narrow widths nearly free - turns the picture upright, converts an
// embedded colour profile to sRGB and scales with a sharp filter. The
// preview keeps none of the original's metadata, so neither the place a
// photograph was taken nor the camera leaves with it.
//
// Arguments:
//   - data: the file's bytes.
//   - widths: the requested widths, all above zero.
//
// Returns:
//   - a JPEG for every requested width, keyed by that width.
//   - ErrNoThumbnail when the file cannot be decoded, or another error.
func render(data []byte, widths []int) (map[int][]byte, error) {
	// The canvas is checked before libvips sees the file: a small file can
	// declare an enormous one.
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, ErrNoThumbnail
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > maxPixels {
		return nil, ErrNoThumbnail
	}
	if err := startRenderer(); err != nil {
		return nil, fmt.Errorf("start libvips: %w", err)
	}

	previews := make(map[int][]byte, len(widths))
	for _, width := range widths {
		preview, err := renderWidth(data, width)
		if err != nil {
			return nil, err
		}
		previews[width] = preview
	}
	return previews, nil
}

// renderWidth writes one preview of a picture. A picture libvips opens is read
// once, front to back, so each width is its own reading of the original rather
// than a second scaling of a picture already read.
//
// Arguments:
//   - data: the file's bytes.
//   - width: the width of the preview.
//
// Returns:
//   - the JPEG of the preview.
//   - ErrNoThumbnail when libvips cannot read the file, or another error.
func renderWidth(data []byte, width int) ([]byte, error) {
	// The height a preview may reach is left open, so the width alone decides
	// its size; SizeDown keeps a narrow picture at its own size.
	picture, err := vips.NewThumbnailWithSizeFromBuffer(data, width, maxPixels, vips.InterestingNone, vips.SizeDown)
	if err != nil {
		return nil, ErrNoThumbnail
	}
	defer picture.Close()
	preview, _, err := picture.ExportJpeg(&vips.JpegExportParams{
		Quality:        thumbnailQuality,
		StripMetadata:  true,
		OptimizeCoding: true,
	})
	if err != nil {
		return nil, fmt.Errorf("encode preview: %w", err)
	}
	return preview, nil
}
