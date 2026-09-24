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

// photonTimeout bounds one request to a Photon server.
const photonTimeout = 10 * time.Second

// Photon is the client of a Photon geocoder, which is built on OpenStreetMap
// and is the one people usually run beside their own map: it needs no key, and
// its answers are GeoJSON, close enough to Pelias that the same fields are read.
type Photon struct {
	baseURL string
	client  *http.Client
}

// NewPhoton - creates the Photon client.
//
// Arguments:
//   - baseURL: the API root, such as http://photon:2322.
//
// Returns:
//   - the client.
func NewPhoton(baseURL string) *Photon {
	return &Photon{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: photonTimeout}}
}

// Name - identifies the provider.
//
// Returns:
//   - "photon".
func (p *Photon) Name() string {
	return "photon"
}

// photonResponse is the part of a GeoJSON answer the client reads.
//
// Photon describes a place in pieces - the name, the street, the city, the
// country - and leaves the line a person reads to the caller, which is what
// photonLabel builds.
type photonResponse struct {
	Features []struct {
		Geometry struct {
			Coordinates []float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			Name        string `json:"name"`
			Street      string `json:"street"`
			HouseNumber string `json:"housenumber"`
			Postcode    string `json:"postcode"`
			City        string `json:"city"`
			District    string `json:"district"`
			State       string `json:"state"`
			Country     string `json:"country"`
			// OSMKey and OSMValue say what the place is: amenity=cafe and such.
			OSMKey   string `json:"osm_key"`
			OSMValue string `json:"osm_value"`
			OSMType  string `json:"osm_type"`
			OSMID    int64  `json:"osm_id"`
		} `json:"properties"`
	} `json:"features"`
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
func (p *Photon) Search(ctx context.Context, query Query) ([]Place, error) {
	params := url.Values{"q": {query.Text}, "limit": {strconv.Itoa(searchSize)}}
	if query.Lang != "" {
		params.Set("lang", query.Lang)
	}
	if query.Focus != nil {
		params.Set("lat", formatFloat(query.Focus.Lat))
		params.Set("lon", formatFloat(query.Focus.Lng))
	}
	return p.get(ctx, "/api", params)
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
func (p *Photon) Reverse(ctx context.Context, point domain.Point, lang string) ([]Place, error) {
	params := url.Values{
		"lat":   {formatFloat(point.Lat)},
		"lon":   {formatFloat(point.Lng)},
		"limit": {strconv.Itoa(reverseSize)},
	}
	if lang != "" {
		params.Set("lang", lang)
	}
	return p.get(ctx, "/reverse", params)
}

// get performs one request and reads its features.
func (p *Photon) get(ctx context.Context, path string, params url.Values) ([]Place, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrUnavailable, err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()

	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("%w: status %d", ErrRateLimited, response.StatusCode)
	case response.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, response.StatusCode)
	}

	var decoded photonResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrUnavailable, err)
	}

	places := make([]Place, 0, len(decoded.Features))
	for _, feature := range decoded.Features {
		if len(feature.Geometry.Coordinates) < 2 {
			continue
		}
		properties := feature.Properties
		place := Place{
			Name:    properties.Name,
			Lat:     feature.Geometry.Coordinates[1],
			Lng:     feature.Geometry.Coordinates[0],
			Layer:   photonLayer(properties.OSMKey, properties.OSMValue),
			Country: properties.Country,
		}
		if properties.OSMType != "" && properties.OSMID != 0 {
			place.Ref = fmt.Sprintf("openstreetmap:%s/%d", photonOSMType(properties.OSMType), properties.OSMID)
		}
		if place.Name == "" {
			place.Name = strings.TrimSpace(properties.Street + " " + properties.HouseNumber)
		}
		place.Label = photonLabel(place.Name, properties.Street, properties.HouseNumber,
			properties.Postcode, properties.City, properties.State, properties.Country)
		places = append(places, place)
	}
	return places, nil
}

// photonLabel joins the pieces Photon returns into the one line a person reads,
// skipping what is missing and never repeating the name.
func photonLabel(name string, parts ...string) string {
	line := make([]string, 0, len(parts)+1)
	if name != "" {
		line = append(line, name)
	}

	seen := map[string]bool{name: true}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		line = append(line, part)
	}
	return strings.Join(line, ", ")
}

// photonLayer describes what a place is in the words the interface groups
// results by. OpenStreetMap's own tags are too many to map one by one, so the
// handful that change how a result reads are named and the rest are venues.
func photonLayer(key, value string) string {
	switch {
	case key == "place" && (value == "city" || value == "town" || value == "village" || value == "hamlet"):
		return "locality"
	case key == "place" && (value == "country" || value == "state" || value == "region"):
		return "region"
	case key == "highway":
		return "street"
	case key == "building" || value == "house":
		return "address"
	default:
		return "venue"
	}
}

// photonOSMType writes the element kind the way an OpenStreetMap reference does.
func photonOSMType(kind string) string {
	switch kind {
	case "N":
		return "node"
	case "W":
		return "way"
	case "R":
		return "relation"
	default:
		return kind
	}
}
