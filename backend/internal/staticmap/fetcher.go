package staticmap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	// The tile servers answer in PNG or JPEG; both decoders are registered.
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// TileCache keeps the tiles fetched, so a second map of the same place does not
// ask the tile server again.
type TileCache interface {
	Tile(ctx context.Context, key []byte, now time.Time) ([]byte, bool, error)
	SaveTile(ctx context.Context, key, body []byte, expiresAt time.Time) error
}

// Fetching bounds. A tile is small; one that takes longer than this is not
// coming, and the map is drawn without a background instead.
const (
	tileTimeout  = 10 * time.Second
	maxTileBytes = 2 << 20
	// cacheFor is how long a tile is kept. The OpenStreetMap servers ask for at
	// least a week; a month keeps a report's second export off the network.
	cacheFor = 30 * 24 * time.Hour
	// fetchWorkers bounds the tiles fetched at once across every document being
	// exported, which is what a tile server's usage policy asks of a client.
	fetchWorkers = 4
)

// Fetcher downloads tiles from the tile server the interface uses, through a
// cache.
type Fetcher struct {
	template  string
	userAgent string
	client    *http.Client
	cache     TileCache
	logger    *slog.Logger
	now       func() time.Time
	slots     chan struct{}
}

// NewFetcher - builds the tile source the report's maps are drawn from.
//
// Arguments:
//   - template: the tile address, with {z}, {x} and {y}, and optionally {s}
//     and {r} as Leaflet reads them.
//   - version: the service version, sent in the User-Agent the tile servers
//     ask every client to identify itself by.
//   - cache: where fetched tiles are kept.
//   - logger: where a tile that could not be cached is reported.
//
// Returns:
//   - the fetcher.
func NewFetcher(template, version string, cache TileCache, logger *slog.Logger) *Fetcher {
	return &Fetcher{
		template:  template,
		userAgent: "Tripvault/" + version + " (report maps)",
		client:    &http.Client{Timeout: tileTimeout},
		cache:     cache,
		logger:    logger,
		now:       time.Now,
		slots:     make(chan struct{}, fetchWorkers),
	}
}

// address fills the template in for one tile.
func (f *Fetcher) address(zoom, x, y int) string {
	return strings.NewReplacer(
		"{z}", strconv.Itoa(zoom),
		"{x}", strconv.Itoa(x),
		"{y}", strconv.Itoa(y),
		// A subdomain rotation and a retina suffix are how Leaflet addresses
		// some servers; one subdomain and plain tiles are what a document needs.
		"{s}", "a",
		"{r}", "",
	).Replace(f.template)
}

// Tile - returns one tile, from the cache or from the tile server.
//
// Arguments:
//   - ctx: context bounding the fetch.
//   - zoom, x, y: the tile.
//
// Returns:
//   - the decoded tile.
//   - an error when it could not be fetched or is not a picture.
func (f *Fetcher) Tile(ctx context.Context, zoom, x, y int) (image.Image, error) {
	address := f.address(zoom, x, y)
	sum := sha256.Sum256([]byte(address))
	key := sum[:]

	if body, found, err := f.cache.Tile(ctx, key, f.now()); err == nil && found {
		if tile, _, err := image.Decode(bytes.NewReader(body)); err == nil {
			return tile, nil
		}
	}

	select {
	case f.slots <- struct{}{}:
		defer func() { <-f.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	body, err := f.fetch(ctx, address)
	if err != nil {
		return nil, err
	}
	tile, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("the tile server sent something that is not a picture: %w", err)
	}
	if err := f.cache.SaveTile(ctx, key, body, f.now().Add(cacheFor)); err != nil {
		f.logger.Warn("keep a map tile failed", slog.Any("error", err))
	}
	return tile, nil
}

// errTileRefused is a tile server that answered, but not with a tile.
var errTileRefused = errors.New("the tile server refused the tile")

// fetch downloads one tile.
func (f *Fetcher) fetch(ctx context.Context, address string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("build the tile request: %w", err)
	}
	request.Header.Set("User-Agent", f.userAgent)
	response, err := f.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch a tile: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s", errTileRefused, response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read a tile: %w", err)
	}
	if len(body) > maxTileBytes {
		return nil, fmt.Errorf("%w: the tile is larger than a tile can be", errTileRefused)
	}
	return body, nil
}
