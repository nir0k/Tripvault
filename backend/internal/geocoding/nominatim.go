package geocoding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// nominatimTimeout bounds one request. Nominatim is slower than the others,
// especially the public instance, so it is given longer before it is given up on.
const nominatimTimeout = 15 * time.Second

// Nominatim is the client of a Nominatim geocoder: the search behind
// openstreetmap.org, and the one most often found already running beside an
// OpenStreetMap database.
//
// Its usage policy requires every request to identify the application it comes
// from, which is what userAgent is for. The public instance also asks for no
// more than one request a second: that is the operator's business to configure,
// through the per-minute limit, rather than something this client enforces.
type Nominatim struct {
	baseURL   string
	userAgent string
	client    *http.Client
}

// NewNominatim - creates the Nominatim client.
//
// Arguments:
//   - baseURL: the API root, such as http://nominatim:8080.
//   - userAgent: how this instance identifies itself, as the usage policy of
//     the public service requires.
//
// Returns:
//   - the client.
func NewNominatim(baseURL, userAgent string) *Nominatim {
	return &Nominatim{
		baseURL:   strings.TrimRight(baseURL, "/"),
		userAgent: userAgent,
		client:    &http.Client{Timeout: nominatimTimeout},
	}
}

// Name - identifies the provider.
//
// Returns:
//   - "nominatim".
func (n *Nominatim) Name() string {
	return "nominatim"
}

// nominatimPlace is the part of a jsonv2 answer the client reads.
type nominatimPlace struct {
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	Type        string `json:"type"`
	AddressType string `json:"addresstype"`
	OSMType     string `json:"osm_type"`
	OSMID       int64  `json:"osm_id"`
	Address     struct {
		Country string `json:"country"`
	} `json:"address"`
}

// Search - finds places matching a text, near the focus first.
//
// The focus becomes a viewbox rather than a sort order: Nominatim has no way to
// rank by distance, and a bounded box is the closest it offers.
//
// Arguments:
//   - ctx: context bounding the request.
//   - query: the text, language and optional focus.
//
// Returns:
//   - the places, best first.
//   - ErrRateLimited or ErrUnavailable wrapping the cause.
func (n *Nominatim) Search(ctx context.Context, query Query) ([]Place, error) {
	params := url.Values{
		"q":              {query.Text},
		"format":         {"jsonv2"},
		"addressdetails": {"1"},
		"limit":          {strconv.Itoa(searchSize)},
	}
	if query.Lang != "" {
		params.Set("accept-language", query.Lang)
	}
	if query.Focus != nil {
		// Roughly a degree around the focus, which is a few dozen kilometres:
		// wide enough not to hide the answer, narrow enough to prefer it.
		const around = 1.0
		params.Set("viewbox", fmt.Sprintf("%s,%s,%s,%s",
			formatFloat(query.Focus.Lng-around), formatFloat(query.Focus.Lat+around),
			formatFloat(query.Focus.Lng+around), formatFloat(query.Focus.Lat-around)))
	}

	var places []nominatimPlace
	if err := n.get(ctx, "/search", params, &places); err != nil {
		return nil, err
	}
	return nominatimPlaces(places), nil
}

// Reverse - names the place at a point.
//
// Nominatim answers a reverse lookup with one place rather than a list, so the
// answer here is one place where the others return a few.
//
// Arguments:
//   - ctx: context bounding the request.
//   - point: the position.
//   - lang: the language results are named in.
//
// Returns:
//   - the place, or nothing when the point is in the sea.
//   - ErrRateLimited or ErrUnavailable wrapping the cause.
func (n *Nominatim) Reverse(ctx context.Context, point domain.Point, lang string) ([]Place, error) {
	params := url.Values{
		"lat":            {formatFloat(point.Lat)},
		"lon":            {formatFloat(point.Lng)},
		"format":         {"jsonv2"},
		"addressdetails": {"1"},
	}
	if lang != "" {
		params.Set("accept-language", lang)
	}

	var place nominatimPlace
	if err := n.get(ctx, "/reverse", params, &place); err != nil {
		return nil, err
	}
	if place.Lat == "" {
		return nil, nil
	}
	return nominatimPlaces([]nominatimPlace{place}), nil
}

// get performs one request and decodes the answer into target.
func (n *Nominatim) get(ctx context.Context, path string, params url.Values, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, n.baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrUnavailable, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", n.userAgent)

	response, err := n.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()

	switch {
	case response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: status %d", ErrRateLimited, response.StatusCode)
	case response.StatusCode != http.StatusOK:
		return fmt.Errorf("%w: status %d", ErrUnavailable, response.StatusCode)
	}

	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(target); err != nil {
		return fmt.Errorf("%w: decode response: %v", ErrUnavailable, err)
	}
	return nil
}

// nominatimPlaces maps the answers onto this service's places, dropping any
// whose coordinates cannot be read.
func nominatimPlaces(found []nominatimPlace) []Place {
	places := make([]Place, 0, len(found))
	for _, item := range found {
		lat, latErr := strconv.ParseFloat(item.Lat, 64)
		lng, lngErr := strconv.ParseFloat(item.Lon, 64)
		if latErr != nil || lngErr != nil {
			continue
		}

		name := item.Name
		if name == "" {
			// A result with no name of its own - a plain address - is known by
			// the first part of the line Nominatim writes for it.
			name, _, _ = strings.Cut(item.DisplayName, ",")
			name = strings.TrimSpace(name)
		}
		place := Place{
			Name:    name,
			Label:   item.DisplayName,
			Lat:     lat,
			Lng:     lng,
			Layer:   nominatimLayer(item),
			Country: item.Address.Country,
		}
		if item.OSMType != "" && item.OSMID != 0 {
			place.Ref = fmt.Sprintf("openstreetmap:%s/%d", item.OSMType, item.OSMID)
		}
		places = append(places, place)
	}
	return places
}

// nominatimLayer describes what a place is in the words the interface groups
// results by.
func nominatimLayer(item nominatimPlace) string {
	switch {
	case item.AddressType == "city" || item.AddressType == "town" ||
		item.AddressType == "village" || item.AddressType == "hamlet":
		return "locality"
	case item.Category == "boundary" || item.AddressType == "state" || item.AddressType == "country":
		return "region"
	case item.Category == "highway" || item.AddressType == "road":
		return "street"
	case item.Category == "building" || item.AddressType == "house_number":
		return "address"
	default:
		return "venue"
	}
}
