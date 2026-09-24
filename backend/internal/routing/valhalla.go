package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Valhalla is a client for a Valhalla server, the other router people commonly
// run themselves. It takes its request as JSON and answers in kilometres and a
// six-decimal polyline, both of which are converted here.
type Valhalla struct {
	baseURL string
	client  *http.Client
}

// valhallaTimeout bounds one route request.
const valhallaTimeout = 15 * time.Second

// valhallaPrecision is the number of decimals Valhalla encodes its shape with.
// Everything else in this service uses five, so the line is decoded and written
// again rather than passed through.
const valhallaPrecision = 6

// NewValhalla - creates the Valhalla client.
//
// Arguments:
//   - baseURL: the API root, such as http://valhalla:8002.
//
// Returns:
//   - the client.
func NewValhalla(baseURL string) *Valhalla {
	return &Valhalla{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: valhallaTimeout},
	}
}

// Name - identifies the provider.
//
// Returns:
//   - "valhalla".
func (v *Valhalla) Name() string {
	return "valhalla"
}

// valhallaCosting translates this service's profiles into Valhalla's costing
// models.
var valhallaCosting = map[string]string{
	profileWalk: "pedestrian",
	profileBike: "bicycle",
	profileCar:  "auto",
}

// valhallaResponse is the part of an answer the client reads.
type valhallaResponse struct {
	Trip struct {
		// Status is 0 on success; anything else carries a message.
		Status        int    `json:"status"`
		StatusMessage string `json:"status_message"`
		Summary       struct {
			// Length is in kilometres and Time in seconds.
			Length float64 `json:"length"`
			Time   float64 `json:"time"`
		} `json:"summary"`
		Legs []struct {
			Shape string `json:"shape"`
		} `json:"legs"`
	} `json:"trip"`
	// A refused request answers with these instead of a trip.
	ErrorCode int    `json:"error_code"`
	Error     string `json:"error"`
}

// valhallaNoRouteCodes are the refusals that mean the points cannot be joined
// rather than that the server is unwell: no path found (442), and a point too
// far from any road to snap to (171).
var valhallaNoRouteCodes = map[int]bool{171: true, 442: true}

// Route - asks a Valhalla server for the route between two points.
//
// Arguments:
//   - ctx: context bounding the request.
//   - profile: one of this package's profiles.
//   - from, to: the points.
//
// Returns:
//   - the route, its line converted to the five-decimal polyline this service
//     stores.
//   - ErrNoRoute when the points cannot be joined, ErrUnavailable otherwise.
func (v *Valhalla) Route(ctx context.Context, profile string, from, to domain.Point) (Route, error) {
	costing, ok := valhallaCosting[profile]
	if !ok {
		costing = valhallaCosting[profileCar]
	}
	body, err := json.Marshal(map[string]any{
		"locations": []map[string]float64{
			{"lat": from.Lat, "lon": from.Lng},
			{"lat": to.Lat, "lon": to.Lng},
		},
		"costing":            costing,
		"directions_type":    "none",
		"units":              "kilometers",
		"shape_match":        "map_snap",
		"directions_options": map[string]any{"language": "en"},
	})
	if err != nil {
		return Route{}, fmt.Errorf("%w: encode request: %v", ErrUnavailable, err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+"/route", bytes.NewReader(body))
	if err != nil {
		return Route{}, fmt.Errorf("%w: build request: %v", ErrUnavailable, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := v.client.Do(request)
	if err != nil {
		return Route{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return Route{}, fmt.Errorf("%w: read response: %v", ErrUnavailable, err)
	}
	var decoded valhallaResponse
	_ = json.Unmarshal(raw, &decoded)

	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return Route{}, fmt.Errorf("%w: status %d", ErrRateLimited, response.StatusCode)
	case valhallaNoRouteCodes[decoded.ErrorCode]:
		return Route{}, fmt.Errorf("%w: code %d", ErrNoRoute, decoded.ErrorCode)
	case response.StatusCode != http.StatusOK || decoded.Trip.Status != 0 || len(decoded.Trip.Legs) == 0:
		return Route{}, fmt.Errorf("%w: status %d, %s", ErrUnavailable, response.StatusCode, decoded.Error)
	}

	trip := decoded.Trip
	return Route{
		DistanceM: int(math.Round(trip.Summary.Length * 1000)),
		DurationS: int(math.Round(trip.Summary.Time)),
		Geometry:  domain.EncodePolyline(domain.DecodePolyline(trip.Legs[0].Shape, valhallaPrecision)),
	}, nil
}
