package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// OSRM is a client for a Project OSRM server, which is what most people run
// when they route on their own machine: one container per profile, no key and
// no limits beyond the hardware.
type OSRM struct {
	baseURL string
	client  *http.Client
}

// osrmTimeout bounds one route request. A server of one's own answers in
// milliseconds; this is the point at which it is clearly not answering at all.
const osrmTimeout = 15 * time.Second

// NewOSRM - creates the OSRM client.
//
// Arguments:
//   - baseURL: the API root, such as http://osrm:5000.
//
// Returns:
//   - the client.
func NewOSRM(baseURL string) *OSRM {
	return &OSRM{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: osrmTimeout},
	}
}

// Name - identifies the provider.
//
// Returns:
//   - "osrm".
func (o *OSRM) Name() string {
	return "osrm"
}

// osrmProfiles translates this service's profiles into the names OSRM puts in
// the path. A server is built for one profile and mostly ignores the name, but
// a deployment that puts three behind one address relies on it.
var osrmProfiles = map[string]string{
	profileWalk: "walking",
	profileBike: "cycling",
	profileCar:  "driving",
}

// osrmResponse is the part of an answer the client reads.
type osrmResponse struct {
	// Code is "Ok" on success, "NoRoute" when the points cannot be joined.
	Code   string `json:"code"`
	Routes []struct {
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
		Geometry string  `json:"geometry"`
	} `json:"routes"`
}

// Route - asks an OSRM server for the route between two points.
//
// Arguments:
//   - ctx: context bounding the request.
//   - profile: one of this package's profiles.
//   - from, to: the points.
//
// Returns:
//   - the route, its line in the five-decimal polyline this service stores.
//   - ErrNoRoute when the points cannot be joined by road, ErrUnavailable for
//     anything else.
func (o *OSRM) Route(ctx context.Context, profile string, from, to domain.Point) (Route, error) {
	address := fmt.Sprintf("%s/route/v1/%s/%s;%s?overview=full&geometries=polyline&alternatives=false&steps=false",
		o.baseURL, url.PathEscape(osrmProfile(profile)), osrmPoint(from), osrmPoint(to))

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return Route{}, fmt.Errorf("%w: build request: %v", ErrUnavailable, err)
	}
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
	var decoded osrmResponse
	_ = json.Unmarshal(raw, &decoded)

	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return Route{}, fmt.Errorf("%w: status %d", ErrRateLimited, response.StatusCode)
	case decoded.Code == "NoRoute" || decoded.Code == "NoSegment":
		return Route{}, fmt.Errorf("%w: %s", ErrNoRoute, decoded.Code)
	case response.StatusCode != http.StatusOK || decoded.Code != "Ok" || len(decoded.Routes) == 0:
		return Route{}, fmt.Errorf("%w: status %d, code %q", ErrUnavailable, response.StatusCode, decoded.Code)
	}

	route := decoded.Routes[0]
	return Route{
		DistanceM: int(math.Round(route.Distance)),
		DurationS: int(math.Round(route.Duration)),
		Geometry:  route.Geometry,
	}, nil
}

// osrmProfile names the profile in the path, falling back to driving for a
// profile this client does not know: an unrouted mode never reaches a provider,
// so the fallback is for a profile added later rather than for bad input.
func osrmProfile(profile string) string {
	if name, ok := osrmProfiles[profile]; ok {
		return name
	}
	return osrmProfiles[profileCar]
}

// osrmPoint writes a point the way OSRM takes it: longitude first.
func osrmPoint(point domain.Point) string {
	return strconv.FormatFloat(point.Lng, 'f', 6, 64) + "," + strconv.FormatFloat(point.Lat, 'f', 6, 64)
}
