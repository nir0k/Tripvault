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

// Capabilities - says what a Valhalla server can be asked for.
//
// Returns:
//   - both the shortest route and alternatives.
func (v *Valhalla) Capabilities() Capabilities {
	return Capabilities{Shortest: true, Alternatives: true}
}

// valhallaCosting translates this service's profiles into Valhalla's costing
// models.
var valhallaCosting = map[string]string{
	profileWalk: "pedestrian",
	profileBike: "bicycle",
	profileCar:  "auto",
}

// valhallaTrip is one route of an answer.
type valhallaTrip struct {
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
}

// valhallaResponse is the part of an answer the client reads.
type valhallaResponse struct {
	Trip valhallaTrip `json:"trip"`
	// Alternates are the other routes, when they were asked for.
	Alternates []struct {
		Trip valhallaTrip `json:"trip"`
	} `json:"alternates"`
	// A refused request answers with these instead of a trip.
	ErrorCode int    `json:"error_code"`
	Error     string `json:"error"`
}

// route converts one trip of an answer into a route, joining the lines of its
// legs into one.
func (t valhallaTrip) route() Route {
	var line []domain.Point
	for _, leg := range t.Legs {
		points := domain.DecodePolyline(leg.Shape, valhallaPrecision)
		// Each leg starts where the one before it ended.
		if len(line) > 0 && len(points) > 0 {
			points = points[1:]
		}
		line = append(line, points...)
	}
	return Route{
		DistanceM: int(math.Round(t.Summary.Length * 1000)),
		DurationS: int(math.Round(t.Summary.Time)),
		Geometry:  domain.EncodePolyline(line),
	}
}

// valhallaNoRouteCodes are the refusals that mean the points cannot be joined
// rather than that the server is unwell: no path found (442), and a point too
// far from any road to snap to (171).
var valhallaNoRouteCodes = map[int]bool{171: true, 442: true}

// Route - asks a Valhalla server for a route, through the request's points.
// The points between the ends are "via" locations: the route passes through
// them without being split into legs, and may turn round at one that sits on
// the far side of a road.
//
// Arguments:
//   - ctx: context bounding the request.
//   - request: the profile, the points, the preference and the alternatives.
//
// Returns:
//   - the routes, the best first, each line converted to the five-decimal
//     polyline this service stores.
//   - ErrNoRoute when the points cannot be joined, ErrUnavailable otherwise.
func (v *Valhalla) Route(ctx context.Context, request Request) ([]Route, error) {
	costing, ok := valhallaCosting[request.Profile]
	if !ok {
		costing = valhallaCosting[profileCar]
	}
	locations := make([]map[string]any, len(request.Points))
	for index, point := range request.Points {
		location := map[string]any{"lat": point.Lat, "lon": point.Lng}
		if index > 0 && index < len(request.Points)-1 {
			location["type"] = "via"
		}
		locations[index] = location
	}
	query := map[string]any{
		"locations":          locations,
		"costing":            costing,
		"directions_type":    "none",
		"units":              "kilometers",
		"shape_match":        "map_snap",
		"directions_options": map[string]any{"language": "en"},
	}
	if request.Preference == domain.RouteShortest {
		query["costing_options"] = map[string]any{costing: map[string]any{"shortest": true}}
	}
	if wantsAlternatives(request) {
		query["alternates"] = request.Alternatives - 1
	}
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("%w: encode request: %v", ErrUnavailable, err)
	}

	call, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+"/route", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrUnavailable, err)
	}
	call.Header.Set("Content-Type", "application/json")
	call.Header.Set("Accept", "application/json")

	response, err := v.client.Do(call)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %v", ErrUnavailable, err)
	}
	var decoded valhallaResponse
	_ = json.Unmarshal(raw, &decoded)

	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("%w: status %d", ErrRateLimited, response.StatusCode)
	case valhallaNoRouteCodes[decoded.ErrorCode]:
		return nil, fmt.Errorf("%w: code %d", ErrNoRoute, decoded.ErrorCode)
	case response.StatusCode != http.StatusOK || decoded.Trip.Status != 0 || len(decoded.Trip.Legs) == 0:
		return nil, fmt.Errorf("%w: status %d, %s", ErrUnavailable, response.StatusCode, decoded.Error)
	}

	routes := []Route{decoded.Trip.route()}
	for _, alternate := range decoded.Alternates {
		if alternate.Trip.Status == 0 && len(alternate.Trip.Legs) > 0 {
			routes = append(routes, alternate.Trip.route())
		}
	}
	return routes, nil
}
