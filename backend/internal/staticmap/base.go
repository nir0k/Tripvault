package staticmap

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"sync"
)

// TileSource hands out the tiles a background is made of.
type TileSource interface {
	// Tile returns the decoded tile at a zoom level and position.
	Tile(ctx context.Context, zoom, x, y int) (image.Image, error)
}

// backgroundQuality is the JPEG quality of a finished background. The tiles
// are flat colour and thin lines, which a lower quality smears.
const backgroundQuality = 88

// tileWorkers bounds how many tiles of one background are fetched at once.
const tileWorkers = 4

// sea is what fills the frame beyond the top and bottom of the world, where
// there are no tiles.
var sea = color.RGBA{R: 170, G: 211, B: 223, A: 255}

// Background - glues the tiles under a frame into one picture.
//
// A tile that cannot be had fails the whole background rather than leaving a
// hole in it: the caller then draws the map without one, which reads better
// than a patchwork.
//
// Arguments:
//   - ctx: context bounding the fetches.
//   - source: where the tiles come from.
//   - frame: what the background covers.
//
// Returns:
//   - the background as a JPEG of the frame's size.
//   - an error when a tile could not be fetched or read.
func Background(ctx context.Context, source TileSource, frame Frame) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, frame.Width, frame.Height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(sea), image.Point{}, draw.Src)

	count := 1 << frame.Zoom
	firstX := int(math.Floor(frame.X / TileSize))
	lastX := int(math.Floor((frame.X + float64(frame.Width) - 1) / TileSize))
	firstY := max(int(math.Floor(frame.Y/TileSize)), 0)
	lastY := min(int(math.Floor((frame.Y+float64(frame.Height)-1)/TileSize)), count-1)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		group   sync.WaitGroup
		lock    sync.Mutex
		failure error
	)
	workers := make(chan struct{}, tileWorkers)
	for ty := firstY; ty <= lastY; ty++ {
		for tx := firstX; tx <= lastX; tx++ {
			group.Go(func() {
				workers <- struct{}{}
				defer func() { <-workers }()
				if ctx.Err() != nil {
					return
				}
				// The world repeats sideways, so a frame across the date line
				// takes its tiles from the other end.
				tile, err := source.Tile(ctx, frame.Zoom, ((tx%count)+count)%count, ty)
				lock.Lock()
				defer lock.Unlock()
				if err != nil {
					if failure == nil {
						failure = fmt.Errorf("tile %d/%d/%d: %w", frame.Zoom, tx, ty, err)
					}
					cancel()
					return
				}
				at := image.Pt(int(math.Round(float64(tx*TileSize)-frame.X)), int(math.Round(float64(ty*TileSize)-frame.Y)))
				draw.Draw(canvas, tile.Bounds().Sub(tile.Bounds().Min).Add(at), tile, tile.Bounds().Min, draw.Over)
			})
		}
	}
	group.Wait()
	if failure != nil {
		return nil, failure
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, canvas, &jpeg.Options{Quality: backgroundQuality}); err != nil {
		return nil, fmt.Errorf("encode the map: %w", err)
	}
	return out.Bytes(), nil
}
