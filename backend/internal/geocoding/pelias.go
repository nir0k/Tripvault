// Package geocoding finds places by name or address, names the place at a
// point, and reads positions out of map links. Only the backend talks to the
// provider; its key never reaches a browser.
package geocoding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Provider errors.
var (
	// ErrDisabled reports that no geocoding provider is configured.
	ErrDisabled = errors.New("geocoding is not configured")
	// ErrRateLimited reports that a request limit was reached.
	ErrRateLimited = errors.New("geocoding rate limit reached")
	// ErrUnavailable reports any other failure to get an answer.
	ErrUnavailable = errors.New("geocoding provider unavailable")
)

// Place is one geocoding result.
type Place struct {
	// Name is the place's own name, Label the full line with its region.
	Name  string  `json:"name"`
	Label string  `json:"label"`
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	// Layer is the kind of result: venue, address, street, locality and so on.
	Layer   string `json:"layer"`
	Country string `json:"country"`
	// Ref identifies the result at its source, such as openstreetmap:venue:node/123.
	Ref string `json:"ref"`
}

// Query is a text search.
type Query struct {
	Text string
	// Lang is the language results are named in.
	Lang string
	// Focus ranks results near a point first; nil searches everywhere.
	Focus *domain.Point
}

// Provider finds places.
type Provider interface {
	// Name identifies the provider in the cache and the status screen.
	Name() string
	// Search finds places matching a text.
	Search(ctx context.Context, query Query) ([]Place, error)
	// Reverse names the places at a point, nearest first.
	Reverse(ctx context.Context, point domain.Point, lang string) ([]Place, error)
}

// Result sizes: a search offers a short list to pick from, a reverse lookup
// needs the nearest few.
const (
	searchSize    = 8
	reverseSize   = 3
	peliasTimeout = 10 * time.Second
)

// Pelias is the client of the Pelias geocoder openrouteservice runs.
type Pelias struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewPelias - creates the geocoder client.
//
// Arguments:
//   - baseURL: the API root, such as https://api.heigit.org/pelias/v1.
//   - apiKey: the key, sent in the Authorization header and never logged.
//
// Returns:
//   - the client.
func NewPelias(baseURL, apiKey string) *Pelias {
	return &Pelias{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: &http.Client{Timeout: peliasTimeout}}
}

// Name - identifies the provider.
//
// Returns:
//   - "openrouteservice".
func (p *Pelias) Name() string {
	return "openrouteservice"
}

// peliasResponse is the part of a GeoJSON answer the client reads.
type peliasResponse struct {
	Features []struct {
		Geometry struct {
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			Name    string `json:"name"`
			Label   string `json:"label"`
			Layer   string `json:"layer"`
			Country string `json:"country"`
			GID     string `json:"gid"`
		} `json:"properties"`
	} `json:"features"`
}

// formatFloat renders a coordinate for a query string.
func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

// Search - finds places matching a text, near the focus first.
//
// Arguments:
//   - ctx: context bounding the request.
//   - query: the text, language and optional focus.
//
// Returns:
//   - the places, best first.
//   - ErrRateLimited or ErrUnavailable wrapping the cause.
func (p *Pelias) Search(ctx context.Context, query Query) ([]Place, error) {
	params := url.Values{"text": {query.Text}, "size": {strconv.Itoa(searchSize)}}
	if query.Lang != "" {
		params.Set("lang", query.Lang)
	}
	if query.Focus != nil {
		params.Set("focus.point.lat", formatFloat(query.Focus.Lat))
		params.Set("focus.point.lon", formatFloat(query.Focus.Lng))
	}
	return p.get(ctx, "/search", params)
}

// Reverse - names the places at a point, nearest first.
//
// Arguments:
//   - ctx: context bounding the request.
//   - point: the position.
//   - lang: the language results are named in.
//
// Returns:
//   - the places.
//   - ErrRateLimited or ErrUnavailable wrapping the cause.
func (p *Pelias) Reverse(ctx context.Context, point domain.Point, lang string) ([]Place, error) {
	params := url.Values{
		"point.lat": {formatFloat(point.Lat)},
		"point.lon": {formatFloat(point.Lng)},
		"size":      {strconv.Itoa(reverseSize)},
	}
	if lang != "" {
		params.Set("lang", lang)
	}
	return p.get(ctx, "/reverse", params)
}

// get performs one geocoder request and reads its features.
func (p *Pelias) get(ctx context.Context, path string, params url.Values) ([]Place, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrUnavailable, err)
	}
	request.Header.Set("Authorization", p.apiKey)
	request.Header.Set("Accept", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		// The URL holds the search text, not the key, so it may be reported.
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()

	switch {
	case response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: status %d", ErrRateLimited, response.StatusCode)
	case response.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, response.StatusCode)
	}

	var decoded peliasResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrUnavailable, err)
	}
	places := make([]Place, 0, len(decoded.Features))
	for _, feature := range decoded.Features {
		if len(feature.Geometry.Coordinates) < 2 {
			continue
		}
		places = append(places, Place{
			Name:    feature.Properties.Name,
			Label:   feature.Properties.Label,
			Lat:     feature.Geometry.Coordinates[1],
			Lng:     feature.Geometry.Coordinates[0],
			Layer:   feature.Properties.Layer,
			Country: feature.Properties.Country,
			Ref:     feature.Properties.GID,
		})
	}
	return places, nil
}
