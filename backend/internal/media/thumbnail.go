package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // registers the PNG decoder

	xdraw "golang.org/x/image/draw"
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
// The picture is turned the way its EXIF orientation says before it is scaled:
// a camera stores the frame as the sensor saw it and records which way up it
// was, so a preview that ignores the tag comes out on its side. A picture
// already narrower than the requested width is scaled up to nothing - it is
// returned at its own size.
//
// Arguments:
//   - data: the file's bytes.
//   - width: the requested width, one of Sizes.
//
// Returns:
//   - a JPEG of the preview.
//   - ErrNoThumbnail when the file cannot be decoded, or another error.
func Thumbnail(data []byte, width int) ([]byte, error) {
	if width <= 0 {
		return nil, ErrNoThumbnail
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, ErrNoThumbnail
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > maxPixels {
		return nil, ErrNoThumbnail
	}

	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrNoThumbnail
	}

	bounds := source.Bounds()
	target := width
	if bounds.Dx() < target {
		target = bounds.Dx()
	}
	height := bounds.Dy() * target / max(bounds.Dx(), 1)
	scaled := image.NewRGBA(image.Rect(0, 0, max(target, 1), max(height, 1)))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), source, bounds, draw.Src, nil)

	oriented := orient(scaled, ReadMetadata(data).Orientation)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, oriented, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return nil, fmt.Errorf("encode preview: %w", err)
	}
	return out.Bytes(), nil
}

// orient turns a picture the way an EXIF orientation value says. Values below 2
// and above 8 mean "as stored", and so does anything unknown.
func orient(source *image.RGBA, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return source
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	// The four values from 5 up turn the picture a quarter, so the preview
	// swaps its sides.
	if orientation >= 5 {
		width, height = height, width
	}
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range bounds.Dy() {
		for x := range bounds.Dx() {
			var tx, ty int
			switch orientation {
			case 2:
				tx, ty = bounds.Dx()-1-x, y
			case 3:
				tx, ty = bounds.Dx()-1-x, bounds.Dy()-1-y
			case 4:
				tx, ty = x, bounds.Dy()-1-y
			case 5:
				tx, ty = y, x
			case 6:
				tx, ty = bounds.Dy()-1-y, x
			case 7:
				tx, ty = bounds.Dy()-1-y, bounds.Dx()-1-x
			case 8:
				tx, ty = y, bounds.Dx()-1-x
			}
			out.Set(tx, ty, source.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return out
}
