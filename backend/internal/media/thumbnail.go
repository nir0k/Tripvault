package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // registers the PNG decoder
	"slices"

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
// Decoding the original is most of the cost of a preview, so it is decoded once
// and every width is scaled from the one above it rather than from the
// original. The previews follow the rules of Thumbnail.
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

// render decodes a picture once and writes a JPEG of it at each width, the
// widest first, so that each narrower one is scaled from the one before it.
// Sizes mostly halve from one to the next, and a half is exactly what shrink
// makes, so most of them never reach the slower filter.
//
// Arguments:
//   - data: the file's bytes.
//   - widths: the requested widths, all above zero.
//
// Returns:
//   - a JPEG for every requested width, keyed by that width.
//   - ErrNoThumbnail when the file cannot be decoded, or another error.
func render(data []byte, widths []int) (map[int][]byte, error) {
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
	orientation := ReadMetadata(data).Orientation

	ordered := slices.Clone(widths)
	slices.Sort(ordered)
	slices.Reverse(ordered)

	bounds := source.Bounds()
	current := source
	previews := make(map[int][]byte, len(ordered))
	for _, width := range ordered {
		target := max(min(width, bounds.Dx()), 1)
		height := max(bounds.Dy()*target/max(bounds.Dx(), 1), 1)
		scaled := scale(shrink(current, target), target, height)
		current = scaled

		var out bytes.Buffer
		if err := jpeg.Encode(&out, orient(scaled, orientation), &jpeg.Options{Quality: thumbnailQuality}); err != nil {
			return nil, fmt.Errorf("encode preview: %w", err)
		}
		previews[width] = out.Bytes()
	}
	return previews, nil
}

// scale resizes a picture to the given size. It is only ever asked to shrink by
// less than half - shrink has done the rest - and over so short a step a
// bilinear filter loses nothing a sharper and far slower one would keep. A
// picture already of that size is only brought into the pixel layout the rest
// of the rendering works on.
func scale(source image.Image, width, height int) *image.RGBA {
	bounds := source.Bounds()
	if bounds.Dx() == width && bounds.Dy() == height {
		return toRGBA(source)
	}
	scaled := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), source, bounds, draw.Src, nil)
	return scaled
}

// toRGBA returns a picture as RGBA pixels starting at the origin, converting it
// when it is stored any other way.
func toRGBA(source image.Image) *image.RGBA {
	if rgba, ok := source.(*image.RGBA); ok && rgba.Rect.Min == (image.Point{}) {
		return rgba
	}
	bounds := source.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), source, bounds.Min, draw.Src)
	return rgba
}

// shrink brings a picture close to the width it is about to be scaled to by
// averaging whole blocks of its pixels.
//
// The filter that makes the previews reads more of the source the further it
// scales down, and from a camera's frame to a gallery tile that is most of the
// time a preview takes. Averaging is the cheapest way to shrink without
// aliasing, so it takes the picture to no less than the width asked for and
// leaves the last, small step to the filter. A picture less than twice that
// width is returned as it is.
//
// Arguments:
//   - source: the picture to shrink.
//   - width: the width it is about to be scaled to.
//
// Returns:
//   - the averaged picture, or the source itself when it is small enough.
func shrink(source image.Image, width int) image.Image {
	bounds := source.Bounds()
	factor := bounds.Dx() / max(width, 1)
	if factor < 2 {
		return source
	}
	outWidth := bounds.Dx() / factor
	outHeight := max((bounds.Dy()*outWidth+bounds.Dx()/2)/bounds.Dx(), 1)
	columns := blockEdges(bounds.Min.X, bounds.Dx(), outWidth)
	rows := blockEdges(bounds.Min.Y, bounds.Dy(), outHeight)

	if frame, ok := source.(*image.YCbCr); ok {
		return averageYCbCr(frame, columns, rows)
	}
	return averageRGBA(toRGBA(source), blockEdges(0, bounds.Dx(), outWidth), blockEdges(0, bounds.Dy(), outHeight))
}

// blockEdges splits a run of pixels into count blocks as even as integers
// allow, so no pixel at the edge is left out.
//
// Arguments:
//   - start: the first pixel of the run.
//   - length: the number of pixels in the run.
//   - count: the number of blocks.
//
// Returns:
//   - count+1 edges; block i spans [edges[i], edges[i+1]).
func blockEdges(start, length, count int) []int {
	edges := make([]int, count+1)
	for i := range edges {
		edges[i] = start + i*length/count
	}
	return edges
}

// averageYCbCr averages blocks of a decoded JPEG straight from its planes, so
// the full frame is never converted to RGB, and converts each average instead.
func averageYCbCr(frame *image.YCbCr, columns, rows []int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, len(columns)-1, len(rows)-1))
	for j := range len(rows) - 1 {
		for i := range len(columns) - 1 {
			var sumY, sumCb, sumCr, count int
			for y := rows[j]; y < rows[j+1]; y++ {
				yi := frame.YOffset(columns[i], y)
				for x := columns[i]; x < columns[i+1]; x++ {
					ci := frame.COffset(x, y)
					sumY += int(frame.Y[yi])
					sumCb += int(frame.Cb[ci])
					sumCr += int(frame.Cr[ci])
					yi++
					count++
				}
			}
			r, g, b := color.YCbCrToRGB(uint8(sumY/count), uint8(sumCb/count), uint8(sumCr/count))
			o := out.PixOffset(i, j)
			out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = r, g, b, 0xff
		}
	}
	return out
}

// averageRGBA averages blocks of an RGBA picture whose pixels start at the
// origin.
func averageRGBA(source *image.RGBA, columns, rows []int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, len(columns)-1, len(rows)-1))
	for j := range len(rows) - 1 {
		for i := range len(columns) - 1 {
			var sum [4]int
			count := 0
			for y := rows[j]; y < rows[j+1]; y++ {
				p := source.PixOffset(columns[i], y)
				for x := columns[i]; x < columns[i+1]; x++ {
					sum[0] += int(source.Pix[p])
					sum[1] += int(source.Pix[p+1])
					sum[2] += int(source.Pix[p+2])
					sum[3] += int(source.Pix[p+3])
					p += 4
					count++
				}
			}
			o := out.PixOffset(i, j)
			for c := range 4 {
				out.Pix[o+c] = uint8(sum[c] / count)
			}
		}
	}
	return out
}

// orient turns a picture the way an EXIF orientation value says. Values below 2
// and above 8 mean "as stored", and so does anything unknown. The pixels are
// moved as whole four-byte words, because going through colours one pixel at a
// time costs more than the scaling before it.
func orient(source *image.RGBA, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return source
	}
	width, height := source.Rect.Dx(), source.Rect.Dy()
	// The four values from 5 up turn the picture a quarter, so the preview
	// swaps its sides.
	outWidth, outHeight := width, height
	if orientation >= 5 {
		outWidth, outHeight = height, width
	}
	out := image.NewRGBA(image.Rect(0, 0, outWidth, outHeight))
	for y := range height {
		from := source.PixOffset(source.Rect.Min.X, source.Rect.Min.Y+y)
		for x := range width {
			var tx, ty int
			switch orientation {
			case 2:
				tx, ty = width-1-x, y
			case 3:
				tx, ty = width-1-x, height-1-y
			case 4:
				tx, ty = x, height-1-y
			case 5:
				tx, ty = y, x
			case 6:
				tx, ty = height-1-y, x
			case 7:
				tx, ty = height-1-y, width-1-x
			case 8:
				tx, ty = y, width-1-x
			}
			to := ty*out.Stride + tx*4
			copy(out.Pix[to:to+4], source.Pix[from:from+4])
			from += 4
		}
	}
	return out
}
