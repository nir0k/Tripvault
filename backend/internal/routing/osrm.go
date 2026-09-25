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

// Capabilities - says what an OSRM server can be asked for. OSRM routes by
// time and nothing else: a shortest route is not among its answers.
//
// Returns:
//   - alternatives, but not the shortest route.
func (o *OSRM) Capabilities() Capabilities {
	return Capabilities{Alternatives: true}
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

// Route - asks an OSRM server for a route, through the request's points. The
// preference is not sent: OSRM always answers with its fastest route.
//
// Arguments:
//   - ctx: context bounding the request.
//   - request: the profile, the points and the alternatives.
//
// Returns:
//   - the routes, the best first, each line in the five-decimal polyline this
//     service stores.
//   - ErrNoRoute when the points cannot be joined by road, ErrUnavailable for
//     anything else.
func (o *OSRM) Route(ctx context.Context, request Request) ([]Route, error) {
	points := make([]string, len(request.Points))
	for index, point := range request.Points {
		points[index] = osrmPoint(point)
	}
	alternatives := "false"
	if wantsAlternatives(request) {
		alternatives = strconv.Itoa(request.Alternatives - 1)
	}
	address := fmt.Sprintf("%s/route/v1/%s/%s?overview=full&geometries=polyline&alternatives=%s&steps=false",
		o.baseURL, url.PathEscape(osrmProfile(request.Profile)), strings.Join(points, ";"), alternatives)

	call, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrUnavailable, err)
	}
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
	var decoded osrmResponse
	_ = json.Unmarshal(raw, &decoded)

	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("%w: status %d", ErrRateLimited, response.StatusCode)
	case decoded.Code == "NoRoute" || decoded.Code == "NoSegment":
		return nil, fmt.Errorf("%w: %s", ErrNoRoute, decoded.Code)
	case response.StatusCode != http.StatusOK || decoded.Code != "Ok" || len(decoded.Routes) == 0:
		return nil, fmt.Errorf("%w: status %d, code %q", ErrUnavailable, response.StatusCode, decoded.Code)
	}

	routes := make([]Route, 0, len(decoded.Routes))
	for _, route := range decoded.Routes {
		routes = append(routes, Route{
			DistanceM: int(math.Round(route.Distance)),
			DurationS: int(math.Round(route.Duration)),
			Geometry:  route.Geometry,
		})
	}
	return routes, nil
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
