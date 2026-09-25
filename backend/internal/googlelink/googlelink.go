// Package googlelink reads the points of a route out of a Google Maps link.
//
// A link to a route drawn in Google Maps does not carry the route itself, only
// the places it joins and the points somebody dragged the line through. Those
// are enough: routed through the same points, this service's own provider
// follows very nearly the same roads. Nothing here asks Google for a route;
// a short link is expanded into the long one it stands for by the geocoding
// package's expander before it reaches this one.
package googlelink

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Errors a link can be refused with.
var (
	// ErrNotALink reports text that is not a Google Maps link to a route.
	ErrNotALink = errors.New("not a Google Maps route link")
	// ErrNoPoints reports a route link that names no position this service
	// can read: places given only by name or by "your location".
	ErrNoPoints = errors.New("the link names no positions")
)

// Point is one position of a route, in the order the route passes it.
type Point struct {
	domain.Point
	// Stop is true for a place the route was asked to visit - its start, its
	// end or a stop between them - and false for a point the line was dragged
	// through on the way.
	Stop bool
}

// Route is what a link says about a route.
type Route struct {
	// Points are the positions the link names, in order. A stop given only by
	// name is missing from them.
	Points []Point
	// Labelled is true when every point is known to be a stop or a dragged
	// point; false when the link was read by its coordinates alone, and every
	// point is marked as a stop.
	Labelled bool
}

// coordinatePattern matches a path segment that is a position typed in as
// "latitude,longitude".
var coordinatePattern = regexp.MustCompile(`^\s*(-?\d{1,2}(?:\.\d+)?)\s*,\s*(-?\d{1,3}(?:\.\d+)?)\s*$`)

// pairPattern matches a longitude and a latitude as the data parameter writes
// them, for a link whose structure cannot be read.
var pairPattern = regexp.MustCompile(`!1d(-?\d+(?:\.\d+)?)!2d(-?\d+(?:\.\d+)?)`)

// IsGoogleHost - says whether a host is one of Google's, for a maps link or a
// short link that leads to one.
//
// Arguments:
//   - host: the host name, without a port.
//
// Returns:
//   - true for google.<tld>, its subdomains, and the goo.gl shorteners.
func IsGoogleHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "goo.gl" || host == "maps.app.goo.gl" {
		return true
	}
	labels := strings.Split(host, ".")
	for index, label := range labels {
		if label != "google" || index == len(labels)-1 {
			continue
		}
		// google.com, google.de, google.co.uk: what follows is a public
		// suffix of at most two labels.
		if rest := labels[index+1:]; len(rest) <= 2 {
			return true
		}
	}
	return false
}

// IsShortLink - says whether a link is a short one that has to be expanded
// before it can be read.
//
// Arguments:
//   - link: the parsed link.
//
// Returns:
//   - true for maps.app.goo.gl and goo.gl/maps links.
func IsShortLink(link *url.URL) bool {
	host := strings.ToLower(link.Hostname())
	return host == "maps.app.goo.gl" || (host == "goo.gl" && strings.HasPrefix(link.Path, "/maps"))
}

// Parse - reads the points of a route out of a long Google Maps link.
//
// Two shapes of link are read: the one the site puts in the address bar,
// /maps/dir/<start>/<stop>/.../data=..., and the documented one,
// /maps/dir/?api=1&origin=..&destination=..&waypoints=... In the first the
// structure of the data parameter says which positions are stops and which
// were dragged through; when it cannot be read, every coordinate pair in it is
// taken in order.
//
// Arguments:
//   - raw: the link as pasted.
//
// Returns:
//   - the route's points.
//   - ErrNotALink for anything that is not a route link, ErrNoPoints for one
//     that names no position.
func Parse(raw string) (Route, error) {
	link, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (link.Scheme != "https" && link.Scheme != "http") || !IsGoogleHost(link.Hostname()) {
		return Route{}, ErrNotALink
	}
	if IsShortLink(link) {
		return Route{}, ErrNotALink
	}
	segments := strings.Split(link.EscapedPath(), "/")
	directions := -1
	for index, segment := range segments {
		if segment == "dir" && index > 0 && segments[index-1] == "maps" {
			directions = index
			break
		}
	}
	if directions < 0 {
		return Route{}, ErrNotALink
	}

	if link.Query().Get("api") == "1" {
		return parseAPILink(link.Query())
	}

	var stops []string
	data := ""
	for _, segment := range segments[directions+1:] {
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			decoded = segment
		}
		switch {
		case strings.HasPrefix(decoded, "data="):
			data = strings.TrimPrefix(decoded, "data=")
		case strings.HasPrefix(decoded, "@"), strings.HasPrefix(decoded, "am="):
			// The camera, and parameters of the page rather than of the route.
		default:
			stops = append(stops, decoded)
		}
	}
	// A trailing slash leaves an empty segment that names nothing.
	for len(stops) > 0 && stops[len(stops)-1] == "" {
		stops = stops[:len(stops)-1]
	}

	route, ok := parseData(data, stops)
	if !ok {
		route = Route{}
		for _, match := range pairPattern.FindAllStringSubmatch(data, -1) {
			if point, ok := lngLat(match[1], match[2]); ok {
				route.Points = append(route.Points, Point{Point: point, Stop: true})
			}
		}
		if len(route.Points) == 0 {
			for _, stop := range stops {
				if point, ok := typedPoint(stop); ok {
					route.Points = append(route.Points, Point{Point: point, Stop: true})
				}
			}
		}
	}
	if len(route.Points) == 0 {
		return Route{}, ErrNoPoints
	}
	return route, nil
}

// parseAPILink reads a link in the documented format, whose stops are in the
// query and whose line cannot have been dragged.
func parseAPILink(query url.Values) (Route, error) {
	names := []string{query.Get("origin")}
	if waypoints := query.Get("waypoints"); waypoints != "" {
		names = append(names, strings.Split(waypoints, "|")...)
	}
	names = append(names, query.Get("destination"))
	route := Route{Labelled: true}
	for _, name := range names {
		if point, ok := typedPoint(name); ok {
			route.Points = append(route.Points, Point{Point: point, Stop: true})
		}
	}
	if len(route.Points) == 0 {
		return Route{}, ErrNoPoints
	}
	return route, nil
}

// typedPoint reads a position typed in as "latitude,longitude".
func typedPoint(text string) (domain.Point, bool) {
	match := coordinatePattern.FindStringSubmatch(text)
	if match == nil {
		return domain.Point{}, false
	}
	lat, errLat := strconv.ParseFloat(match[1], 64)
	lng, errLng := strconv.ParseFloat(match[2], 64)
	if errLat != nil || errLng != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return domain.Point{}, false
	}
	return domain.Point{Lat: lat, Lng: lng}, true
}

// lngLat reads a longitude and a latitude in the order the data parameter
// writes them.
func lngLat(lngText, latText string) (domain.Point, bool) {
	lng, errLng := strconv.ParseFloat(lngText, 64)
	lat, errLat := strconv.ParseFloat(latText, 64)
	if errLng != nil || errLat != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return domain.Point{}, false
	}
	return domain.Point{Lat: lat, Lng: lng}, true
}

// token is one entry of the data parameter: "!<id><kind><value>". An entry of
// kind 'm' is a message whose value counts the entries nested in it.
type token struct {
	id       int
	kind     byte
	value    string
	children []*token
}

// tokenize splits the data parameter into its entries, in order.
func tokenize(data string) ([]*token, bool) {
	var tokens []*token
	for _, part := range strings.Split(data, "!") {
		if part == "" {
			continue
		}
		index := 0
		for index < len(part) && part[index] >= '0' && part[index] <= '9' {
			index++
		}
		if index == 0 || index >= len(part) {
			return nil, false
		}
		id, err := strconv.Atoi(part[:index])
		if err != nil {
			return nil, false
		}
		tokens = append(tokens, &token{id: id, kind: part[index], value: part[index+1:]})
	}
	return tokens, true
}

// parseData reads a route from the structure of the data parameter: the
// directions message holds one message per stop, and each stop's message holds
// its position and the points the line leaving it was dragged through.
//
// Arguments:
//   - data: the data parameter, without "data=".
//   - stops: the stops the path names, in order, as typed or as names.
//
// Returns:
//   - the route, labelled.
//   - false when the structure is not the one expected.
func parseData(data string, stops []string) (Route, bool) {
	tokens, ok := tokenize(data)
	if !ok || len(tokens) == 0 {
		return Route{}, false
	}
	tree, ok := nestDescendants(tokens)
	if !ok {
		return Route{}, false
	}
	directions := findDirections(tree)
	if directions == nil {
		return Route{}, false
	}

	route := Route{Labelled: true}
	stop := 0
	for _, child := range directions.children {
		if child.id != 1 || child.kind != 'm' {
			continue
		}
		var name string
		if stop < len(stops) {
			name = stops[stop]
		}
		stop++
		position, found := childPoint(child, 2)
		if !found {
			position, found = typedPoint(name)
		}
		if found {
			route.Points = append(route.Points, Point{Point: position, Stop: true})
		}
		for _, via := range child.children {
			if via.id != 3 || via.kind != 'm' {
				continue
			}
			if point, found := childPoint(via, 1); found {
				route.Points = append(route.Points, Point{Point: point})
			}
		}
	}
	if stop == 0 {
		return Route{}, false
	}
	return route, true
}

// nestDescendants builds the tree the 'm' entries describe. Google writes each
// count as the number of entries nested in the message at any depth, not only
// its direct children, and a count that runs past its parent's end means the
// parameter is not what it seems.
func nestDescendants(tokens []*token) ([]*token, bool) {
	var build func(start, end int) ([]*token, bool)
	build = func(start, end int) ([]*token, bool) {
		var nodes []*token
		for index := start; index < end; {
			node := tokens[index]
			index++
			if node.kind == 'm' {
				size, _ := strconv.Atoi(node.value)
				if index+size > end {
					return nil, false
				}
				children, ok := build(index, index+size)
				if !ok {
					return nil, false
				}
				node.children = children
				index += size
			}
			nodes = append(nodes, node)
		}
		return nodes, true
	}
	return build(0, len(tokens))
}

// findDirections finds the message that lists the stops: a "4m" whose
// children are "1m" stop messages beside the "3e" travel mode.
func findDirections(nodes []*token) *token {
	for _, node := range nodes {
		if node.kind != 'm' {
			continue
		}
		if node.id == 4 {
			stops := 0
			for _, child := range node.children {
				if child.id == 1 && child.kind == 'm' {
					stops++
				}
			}
			if stops >= 2 {
				return node
			}
		}
		if found := findDirections(node.children); found != nil {
			return found
		}
	}
	return nil
}

// childPoint reads the position held in a message's child with the given id:
// a message of "1d" longitude and "2d" latitude.
func childPoint(message *token, id int) (domain.Point, bool) {
	for _, child := range message.children {
		if child.id != id || child.kind != 'm' {
			continue
		}
		var lngText, latText string
		for _, value := range child.children {
			switch {
			case value.id == 1 && value.kind == 'd':
				lngText = value.value
			case value.id == 2 && value.kind == 'd':
				latText = value.value
			}
		}
		if lngText != "" && latText != "" {
			return lngLat(lngText, latText)
		}
	}
	return domain.Point{}, false
}
