package geocoding

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrUnrecognized reports text that holds no position this service can read.
var ErrUnrecognized = errors.New("no position found in the text")

// Location is a position read from coordinates or a map link.
type Location struct {
	Lat float64
	Lng float64
	// Name is the place name a link carried, such as a Google Maps place.
	Name string
}

var (
	// pairPattern matches "64.1466, -21.9426" and "64.1466 -21.9426".
	pairPattern = regexp.MustCompile(`^\s*(-?\d{1,2}(?:\.\d+)?)\s*[,; ]\s*(-?\d{1,3}(?:\.\d+)?)\s*$`)
	// googlePlacePattern matches the exact place position Google Maps links carry.
	googlePlacePattern = regexp.MustCompile(`!3d(-?\d+(?:\.\d+)?)!4d(-?\d+(?:\.\d+)?)`)
	// googleViewPattern matches the map centre in a Google Maps path.
	googleViewPattern = regexp.MustCompile(`@(-?\d+(?:\.\d+)?),(-?\d+(?:\.\d+)?)`)
	// osmFragmentPattern matches an openstreetmap.org "#map=zoom/lat/lng".
	osmFragmentPattern = regexp.MustCompile(`map=\d+(?:\.\d+)?/(-?\d+(?:\.\d+)?)/(-?\d+(?:\.\d+)?)`)
)

// newLocation checks a pair of coordinates lies on Earth.
func newLocation(latText, lngText string) (Location, bool) {
	lat, latErr := strconv.ParseFloat(latText, 64)
	lng, lngErr := strconv.ParseFloat(lngText, 64)
	if latErr != nil || lngErr != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return Location{}, false
	}
	return Location{Lat: lat, Lng: lng}, true
}

// pair reads "lat,lng" out of a value.
func pair(value string) (Location, bool) {
	match := pairPattern.FindStringSubmatch(strings.ReplaceAll(value, "+", " "))
	if match == nil {
		return Location{}, false
	}
	return newLocation(match[1], match[2])
}

// ParseLocation - reads a position from coordinates or a map link.
//
// Understood forms: a "lat, lng" pair; a geo: URI; Google Maps links with a
// place (!3d…!4d…), a map centre (@lat,lng) or a q/ll/query/destination
// parameter; openstreetmap.org links with mlat/mlon or #map=zoom/lat/lng.
// Short links must be expanded first with ExpandShortLink.
//
// Arguments:
//   - text: what the person pasted.
//
// Returns:
//   - the position, with the place name when the link carries one.
//   - ErrUnrecognized when no position can be read.
func ParseLocation(text string) (Location, error) {
	text = strings.TrimSpace(text)
	if location, ok := pair(text); ok {
		return location, nil
	}
	if rest, ok := strings.CutPrefix(strings.ToLower(text), "geo:"); ok {
		if location, ok := pair(strings.SplitN(rest, ";", 2)[0]); ok {
			return location, nil
		}
	}

	link, err := url.Parse(text)
	if err != nil || link.Host == "" {
		return Location{}, ErrUnrecognized
	}
	host := strings.ToLower(link.Hostname())
	decodedPath, _ := url.PathUnescape(link.EscapedPath())

	switch {
	case strings.Contains(host, "google.") || strings.HasPrefix(host, "maps.google"):
		name := googlePlaceName(decodedPath)
		if match := googlePlacePattern.FindStringSubmatch(decodedPath + link.RawQuery); match != nil {
			if location, ok := newLocation(match[1], match[2]); ok {
				location.Name = name
				return location, nil
			}
		}
		for _, key := range []string{"q", "query", "ll", "destination", "center"} {
			if location, ok := pair(link.Query().Get(key)); ok {
				location.Name = name
				return location, nil
			}
		}
		for _, marker := range []string{"/search/", "/place/", "/dir//"} {
			if _, after, found := strings.Cut(decodedPath, marker); found {
				if location, ok := pair(strings.SplitN(after, "/", 2)[0]); ok {
					return location, nil
				}
			}
		}
		if match := googleViewPattern.FindStringSubmatch(decodedPath); match != nil {
			if location, ok := newLocation(match[1], match[2]); ok {
				location.Name = name
				return location, nil
			}
		}
	case host == "openstreetmap.org" || strings.HasSuffix(host, ".openstreetmap.org") || host == "osm.org":
		query := link.Query()
		if location, ok := newLocation(query.Get("mlat"), query.Get("mlon")); ok && query.Get("mlat") != "" {
			return location, nil
		}
		if match := osmFragmentPattern.FindStringSubmatch(link.Fragment); match != nil {
			if location, ok := newLocation(match[1], match[2]); ok {
				return location, nil
			}
		}
	}
	return Location{}, ErrUnrecognized
}

// googlePlaceName reads the place name of a /maps/place/<name>/ path, unless
// that segment is itself a pair of coordinates.
func googlePlaceName(path string) string {
	_, after, found := strings.Cut(path, "/place/")
	if !found {
		return ""
	}
	name := strings.TrimSpace(strings.ReplaceAll(strings.SplitN(after, "/", 2)[0], "+", " "))
	if _, isPair := pair(name); isPair || strings.HasPrefix(name, "@") {
		return ""
	}
	return name
}

// shortLinkHosts are the link shorteners whose redirects are followed, and
// redirectHosts where a redirect may lead. Nothing else is ever requested, so
// the endpoint cannot be used to reach other servers.
var (
	shortLinkHosts = map[string]bool{"maps.app.goo.gl": true, "goo.gl": true}
	redirectHosts  = map[string]bool{"maps.app.goo.gl": true, "goo.gl": true, "www.google.com": true,
		"google.com": true, "maps.google.com": true, "consent.google.com": true}
)

// IsShortLink - reports whether a text is a map short link ExpandShortLink handles.
//
// Arguments:
//   - text: what the person pasted.
//
// Returns:
//   - true for an https maps.app.goo.gl or goo.gl link.
func IsShortLink(text string) bool {
	link, err := url.Parse(strings.TrimSpace(text))
	return err == nil && link.Scheme == "https" && shortLinkHosts[strings.ToLower(link.Hostname())]
}

// Expander follows map short links to the full link they stand for.
type Expander struct {
	client *http.Client
}

// NewExpander - creates the short link expander.
//
// Returns:
//   - an expander that never follows redirects on its own.
func NewExpander() *Expander {
	return &Expander{client: &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// maxRedirects bounds how far a short link is followed.
const maxRedirects = 5

// Expand - follows a short link's redirects, one at a time, while they stay on
// Google hosts, and returns the first link that holds a position.
//
// Arguments:
//   - ctx: context bounding the requests.
//   - text: a short link, as IsShortLink accepts.
//
// Returns:
//   - the expanded link.
//   - ErrUnrecognized when the redirects lead elsewhere or nowhere.
func (e *Expander) Expand(ctx context.Context, text string) (string, error) {
	current := strings.TrimSpace(text)
	for range maxRedirects {
		link, err := url.Parse(current)
		if err != nil || link.Scheme != "https" || !redirectHosts[strings.ToLower(link.Hostname())] {
			return "", ErrUnrecognized
		}
		if _, err := ParseLocation(current); err == nil {
			return current, nil
		}
		// A consent page carries the real link in its "continue" parameter.
		if strings.EqualFold(link.Hostname(), "consent.google.com") {
			if next := link.Query().Get("continue"); next != "" {
				current = next
				continue
			}
			return "", ErrUnrecognized
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrUnrecognized, err)
		}
		response, err := e.client.Do(request)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		_ = response.Body.Close()
		location := response.Header.Get("Location")
		if response.StatusCode < 300 || response.StatusCode >= 400 || location == "" {
			return "", ErrUnrecognized
		}
		next, err := link.Parse(location)
		if err != nil {
			return "", ErrUnrecognized
		}
		current = next.String()
	}
	return "", ErrUnrecognized
}
