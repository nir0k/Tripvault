package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Provider errors, which decide the reason recorded on an estimate.
var (
	// ErrNoRoute reports that no road route exists, as between islands.
	ErrNoRoute = errors.New("no route between the points")
	// ErrRateLimited reports that the provider refused because of its limits.
	ErrRateLimited = errors.New("routing provider rate limit reached")
	// ErrUnavailable reports any other failure to get an answer.
	ErrUnavailable = errors.New("routing provider unavailable")
	// ErrDailyLimit reports that this service's own daily limit is spent.
	ErrDailyLimit = errors.New("routing daily limit reached")
	// ErrDisabled reports that no routing provider is configured.
	ErrDisabled = errors.New("no routing provider is configured")
	// ErrNotRouted reports a mode that follows a straight line, not roads.
	ErrNotRouted = errors.New("the travel mode is not routed on roads")
)

// Route is a road route as a provider returns it.
type Route struct {
	DistanceM int
	DurationS int
	// Geometry is the line in the encoded polyline format.
	Geometry string
}

// Request is what a route is asked for.
type Request struct {
	// Profile is one of this package's profiles.
	Profile string
	// Points are where the route starts, the points it passes through in
	// order, and where it ends: at least two.
	Points []domain.Point
	// Preference is what the route is optimised for. A provider that cannot
	// route by it answers with its fastest route.
	Preference domain.RoutePreference
	// Alternatives is how many routes are wanted in all. More than one is
	// honoured only between two points and only by a provider that offers
	// alternatives; the others answer with one.
	Alternatives int
}

// Capabilities say what a provider can be asked beyond the fastest route.
type Capabilities struct {
	// Shortest is true when the provider routes by distance on request.
	Shortest bool
	// Alternatives is true when the provider offers other routes than its best.
	Alternatives bool
}

// Provider calculates road routes.
type Provider interface {
	// Name identifies the provider in the route cache and the status screen.
	Name() string
	// Capabilities say what the provider can be asked for.
	Capabilities() Capabilities
	// Route calculates a route, and when asked and able, its alternatives:
	// the best first, never an empty list without an error.
	Route(ctx context.Context, request Request) ([]Route, error)
}

// wantsAlternatives says whether a request asks for more than one route and
// can have them: alternatives are routes between two points, never through
// points of one's own.
func wantsAlternatives(request Request) bool {
	return request.Alternatives > 1 && len(request.Points) == 2
}

// ORS is the openrouteservice Directions V2 client.
type ORS struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// orsTimeout bounds one directions request.
const orsTimeout = 15 * time.Second

// NewORS - creates the openrouteservice client.
//
// Arguments:
//   - baseURL: the API root, such as https://api.heigit.org/openrouteservice.
//   - apiKey: the key, sent in the Authorization header and never logged.
//
// Returns:
//   - the client.
func NewORS(baseURL, apiKey string) *ORS {
	return &ORS{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: orsTimeout},
	}
}

// Name - identifies the provider.
//
// Returns:
//   - "openrouteservice".
func (o *ORS) Name() string {
	return "openrouteservice"
}

// Capabilities - says what openrouteservice can be asked for.
//
// Returns:
//   - both the shortest route and alternatives.
func (o *ORS) Capabilities() Capabilities {
	return Capabilities{Shortest: true, Alternatives: true}
}

// orsPreference names a preference the way openrouteservice takes it. Its own
// default, "recommended", weighs roads by a judgement of its own and often
// leaves the main road for a smaller one; the fastest route has to be asked
// for by name.
func orsPreference(preference domain.RoutePreference) string {
	if preference == domain.RouteShortest {
		return "shortest"
	}
	return "fastest"
}

// ORS alternative routes: how much of the best route another may share, and
// how much longer it may be, as openrouteservice measures both.
const (
	orsShareFactor  = 0.6
	orsWeightFactor = 1.4
)

// orsResponse is the part of a directions answer the client reads.
type orsResponse struct {
	Routes []struct {
		Summary struct {
			Distance float64 `json:"distance"`
			Duration float64 `json:"duration"`
		} `json:"summary"`
		Geometry string `json:"geometry"`
	} `json:"routes"`
	Error *struct {
		Code int `json:"code"`
	} `json:"error"`
}

// orsProfiles translates this service's profiles into openrouteservice's own.
var orsProfiles = map[string]string{
	profileWalk: "foot-walking",
	profileBike: "cycling-regular",
	profileCar:  "driving-car",
}

// ORS error codes that mean "no route" rather than a failure: a point far from
// any road (2010), no route found (2009), or a route longer than the profile
// allows (2004).
var orsNoRouteCodes = map[int]bool{2004: true, 2009: true, 2010: true}

// orsSnapRadiusM is how far from a point openrouteservice may look for a road
// to start or end on. Its own default is 350 metres, which a pin on a waterfall
// or a viewpoint often misses, turning a short drive into "no route"; OSRM and
// Valhalla snap to the nearest road wherever it is.
const orsSnapRadiusM = 5000

// Route - asks openrouteservice for a route, through the request's points.
//
// Arguments:
//   - ctx: context bounding the request.
//   - request: the profile, the points, the preference and the alternatives.
//
// Returns:
//   - the routes, the best first.
//   - ErrNoRoute, ErrRateLimited or ErrUnavailable wrapping the cause.
func (o *ORS) Route(ctx context.Context, request Request) ([]Route, error) {
	coordinates := make([][2]float64, len(request.Points))
	radiuses := make([]int, len(request.Points))
	for index, point := range request.Points {
		coordinates[index] = [2]float64{point.Lng, point.Lat}
		radiuses[index] = orsSnapRadiusM
	}
	query := map[string]any{
		"coordinates":  coordinates,
		"radiuses":     radiuses,
		"instructions": false,
		"preference":   orsPreference(request.Preference),
	}
	if wantsAlternatives(request) {
		query["alternative_routes"] = map[string]any{
			"target_count":  request.Alternatives,
			"share_factor":  orsShareFactor,
			"weight_factor": orsWeightFactor,
		}
	}
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("%w: encode request: %v", ErrUnavailable, err)
	}
	name, ok := orsProfiles[request.Profile]
	if !ok {
		name = orsProfiles[profileCar]
	}
	call, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/v2/directions/"+name, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrUnavailable, err)
	}
	call.Header.Set("Authorization", o.apiKey)
	call.Header.Set("Content-Type", "application/json")
	call.Header.Set("Accept", "application/json")

	response, err := o.client.Do(call)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %v", ErrUnavailable, err)
	}
	var decoded orsResponse
	_ = json.Unmarshal(raw, &decoded)

	switch {
	case response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: status %d", ErrRateLimited, response.StatusCode)
	case decoded.Error != nil && orsNoRouteCodes[decoded.Error.Code]:
		return nil, fmt.Errorf("%w: code %d", ErrNoRoute, decoded.Error.Code)
	case response.StatusCode != http.StatusOK || len(decoded.Routes) == 0:
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, response.StatusCode)
	}

	routes := make([]Route, 0, len(decoded.Routes))
	for _, route := range decoded.Routes {
		routes = append(routes, Route{
			DistanceM: int(math.Round(route.Summary.Distance)),
			DurationS: int(math.Round(route.Summary.Duration)),
			Geometry:  route.Geometry,
		})
	}
	return routes, nil
}
