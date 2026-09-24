package staticmap

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestFitHoldsEveryPoint checks the frame shows every point with room to spare,
// and is as close as the size allows.
func TestFitHoldsEveryPoint(t *testing.T) {
	// Reykjavík and Vík, about 180 km apart.
	points := []domain.Point{{Lat: 64.1466, Lng: -21.9426}, {Lat: 63.4186, Lng: -19.0060}}
	frame := Fit(points, 800, 500)
	for _, point := range points {
		x, y := frame.Project(point)
		if x < framePadding-1 || x > 800-framePadding+1 || y < framePadding-1 || y > 500-framePadding+1 {
			t.Errorf("%v lands at %.0f,%.0f, outside the padded frame", point, x, y)
		}
	}
	closer := frame
	closer.Zoom++
	left, top, right, bottom := bounds(points, closer.Zoom)
	if right-left <= 800-2*framePadding && bottom-top <= 500-2*framePadding {
		t.Errorf("zoom %d would still hold the points, but %d was chosen", closer.Zoom, frame.Zoom)
	}
}

// TestFitOnePoint checks a single place is shown with its surroundings rather
// than as close as the tiles go.
func TestFitOnePoint(t *testing.T) {
	frame := Fit([]domain.Point{{Lat: 63.6156, Lng: -19.9928}}, 800, 320)
	if frame.Zoom != maxZoom {
		t.Errorf("zoom %d, want %d", frame.Zoom, maxZoom)
	}
	if x, y := frame.Project(domain.Point{Lat: 63.6156, Lng: -19.9928}); int(x) != 400 || int(y) != 160 {
		t.Errorf("the place lands at %.1f,%.1f, want the middle", x, y)
	}
}

// solidTiles hands out plain tiles and counts what it was asked for.
type solidTiles struct {
	asked atomic.Int32
	fail  bool
}

// Tile returns a grey tile, or fails when told to.
func (s *solidTiles) Tile(_ context.Context, _, _, _ int) (image.Image, error) {
	s.asked.Add(1)
	if s.fail {
		return nil, errors.New("no tiles today")
	}
	tile := image.NewRGBA(image.Rect(0, 0, TileSize, TileSize))
	for x := range TileSize {
		for y := range TileSize {
			tile.Set(x, y, color.RGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	return tile, nil
}

// TestBackgroundGluesTheTilesUnderTheFrame checks the background is the size of
// the frame and is made of the tiles it covers.
func TestBackgroundGluesTheTilesUnderTheFrame(t *testing.T) {
	source := &solidTiles{}
	frame := Frame{Zoom: 5, X: 100, Y: 100, Width: 600, Height: 300}
	body, err := Background(t.Context(), source, frame)
	if err != nil {
		t.Fatalf("Background() returned an unexpected error: %v", err)
	}
	picture, err := jpeg.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("the background is not a JPEG: %v", err)
	}
	if size := picture.Bounds().Size(); size.X != 600 || size.Y != 300 {
		t.Errorf("the background is %v, want 600x300", size)
	}
	// Columns 0 to 2 and rows 0 to 1.
	if got := source.asked.Load(); got != 6 {
		t.Errorf("%d tiles were fetched, want 6", got)
	}
}

// TestBackgroundFailsWithATile checks a missing tile fails the background
// rather than leaving a hole in it.
func TestBackgroundFailsWithATile(t *testing.T) {
	if _, err := Background(t.Context(), &solidTiles{fail: true}, Frame{Zoom: 3, Width: 300, Height: 200}); err == nil {
		t.Error("a background was made without its tiles")
	}
}

// memoryCache keeps tiles in a map.
type memoryCache struct {
	lock  sync.Mutex
	tiles map[string][]byte
}

// Tile returns a kept tile.
func (m *memoryCache) Tile(_ context.Context, key []byte, _ time.Time) ([]byte, bool, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	body, found := m.tiles[string(key)]
	return body, found, nil
}

// SaveTile keeps a tile.
func (m *memoryCache) SaveTile(_ context.Context, key, body []byte, _ time.Time) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.tiles[string(key)] = body
	return nil
}

// TestFetcherIdentifiesItselfAndCaches checks a tile is asked for once, with
// the User-Agent a tile server requires, and read from the cache afterwards.
func TestFetcherIdentifiesItselfAndCaches(t *testing.T) {
	var tile bytes.Buffer
	if err := png.Encode(&tile, image.NewRGBA(image.Rect(0, 0, TileSize, TileSize))); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if !strings.HasPrefix(r.UserAgent(), "Tripvault/1.2.3") {
			t.Errorf("the tile was asked for as %q", r.UserAgent())
		}
		if r.URL.Path != "/a/7/66/35.png" {
			t.Errorf("asked for %s", r.URL.Path)
		}
		_, _ = w.Write(tile.Bytes())
	}))
	defer server.Close()

	fetcher := NewFetcher(server.URL+"/{s}/{z}/{x}/{y}{r}.png", "1.2.3", &memoryCache{tiles: map[string][]byte{}},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	for range 2 {
		if _, err := fetcher.Tile(t.Context(), 7, 66, 35); err != nil {
			t.Fatalf("Tile() returned an unexpected error: %v", err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("the tile server was asked %d times, want once", got)
	}
}

// TestFetcherRefusesWhatIsNotATile checks an error page is not mistaken for a
// tile, nor kept as one.
func TestFetcherRefusesWhatIsNotATile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/1/") {
			http.Error(w, "blocked", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("API key required"))
	}))
	defer server.Close()

	cache := &memoryCache{tiles: map[string][]byte{}}
	fetcher := NewFetcher(server.URL+"/{z}/{x}/{y}.png", "dev", cache, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := fetcher.Tile(t.Context(), 1, 0, 0); err == nil {
		t.Error("a refused tile was accepted")
	}
	if _, err := fetcher.Tile(t.Context(), 2, 0, 0); err == nil {
		t.Error("a page of text was accepted as a tile")
	}
	if len(cache.tiles) != 0 {
		t.Errorf("%d answers that are not tiles were kept", len(cache.tiles))
	}
}
