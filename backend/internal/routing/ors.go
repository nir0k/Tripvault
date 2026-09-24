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
)

// Route is a road route as a provider returns it.
type Route struct {
	DistanceM int
	DurationS int
	// Geometry is the line in the encoded polyline format.
	Geometry string
}

// Provider calculates road routes.
type Provider interface {
	// Name identifies the provider in the route cache and the status screen.
	Name() string
	// Route calculates the route between two points with a profile.
	Route(ctx context.Context, profile string, from, to domain.Point) (Route, error)
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

// Route - asks openrouteservice for the route between two points.
//
// Arguments:
//   - ctx: context bounding the request.
//   - profile: one of this package's profiles.
//   - from, to: the points.
//
// Returns:
//   - the route.
//   - ErrNoRoute, ErrRateLimited or ErrUnavailable wrapping the cause.
func (o *ORS) Route(ctx context.Context, profile string, from, to domain.Point) (Route, error) {
	body, err := json.Marshal(map[string]any{
		"coordinates":  [][2]float64{{from.Lng, from.Lat}, {to.Lng, to.Lat}},
		"instructions": false,
	})
	if err != nil {
		return Route{}, fmt.Errorf("%w: encode request: %v", ErrUnavailable, err)
	}
	name, ok := orsProfiles[profile]
	if !ok {
		name = orsProfiles[profileCar]
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/v2/directions/"+name, bytes.NewReader(body))
	if err != nil {
		return Route{}, fmt.Errorf("%w: build request: %v", ErrUnavailable, err)
	}
	request.Header.Set("Authorization", o.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := o.client.Do(request)
	if err != nil {
		return Route{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return Route{}, fmt.Errorf("%w: read response: %v", ErrUnavailable, err)
	}
	var decoded orsResponse
	_ = json.Unmarshal(raw, &decoded)

	switch {
	case response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden:
		return Route{}, fmt.Errorf("%w: status %d", ErrRateLimited, response.StatusCode)
	case decoded.Error != nil && orsNoRouteCodes[decoded.Error.Code]:
		return Route{}, fmt.Errorf("%w: code %d", ErrNoRoute, decoded.Error.Code)
	case response.StatusCode != http.StatusOK || len(decoded.Routes) == 0:
		return Route{}, fmt.Errorf("%w: status %d", ErrUnavailable, response.StatusCode)
	}

	route := decoded.Routes[0]
	return Route{
		DistanceM: int(math.Round(route.Summary.Distance)),
		DurationS: int(math.Round(route.Summary.Duration)),
		Geometry:  route.Geometry,
	}, nil
}
