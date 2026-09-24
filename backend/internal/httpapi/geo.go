package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/geocoding"
)

// Geocoder finds places and names positions.
type Geocoder interface {
	Enabled() bool
	Search(ctx context.Context, query geocoding.Query) ([]geocoding.Place, error)
	Reverse(ctx context.Context, point domain.Point, lang string) ([]geocoding.Place, error)
}

// LinkExpander follows map short links.
type LinkExpander interface {
	Expand(ctx context.Context, text string) (string, error)
}

// Bounds of geocoding input.
const (
	minSearchLength = 2
	maxSearchLength = 200
	maxLinkLength   = 2000
)

// langPattern accepts a bare language code such as "ru" or "en".
var langPattern = regexp.MustCompile(`^[a-z]{2,3}$`)

// placeListResponse wraps geocoding results.
type placeListResponse struct {
	Items []geocoding.Place `json:"items"`
}

// requestLang picks the language results are named in: the lang parameter,
// else the reader's interface language, else English.
func requestLang(r *http.Request) string {
	if lang := strings.ToLower(r.URL.Query().Get("lang")); langPattern.MatchString(lang) {
		return lang
	}
	if locale := principalFrom(r.Context()).user.Locale; locale != "" {
		return locale
	}
	return "en"
}

// queryPoint reads an optional lat/lng pair from the query string.
func queryPoint(r *http.Request) (*domain.Point, error) {
	latText, lngText := r.URL.Query().Get("lat"), r.URL.Query().Get("lng")
	if latText == "" && lngText == "" {
		return nil, nil
	}
	lat, latErr := strconv.ParseFloat(latText, 64)
	lng, lngErr := strconv.ParseFloat(lngText, 64)
	if latErr != nil || lngErr != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return nil, domain.NewValidationError("lat", "out_of_range", "lat and lng must be a position on Earth")
	}
	return &domain.Point{Lat: lat, Lng: lng}, nil
}

// writeGeocodingError maps geocoder failures onto responses.
func (s *Server) writeGeocodingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, geocoding.ErrDisabled):
		s.writeError(w, r, http.StatusServiceUnavailable, "geocoding_disabled", "Place search is not configured")
	case errors.Is(err, geocoding.ErrRateLimited):
		s.writeError(w, r, http.StatusServiceUnavailable, "geocoding_limited", "Place search is busy; try again shortly")
	case errors.Is(err, geocoding.ErrUnavailable):
		s.writeError(w, r, http.StatusServiceUnavailable, "geocoding_unavailable", "Place search did not answer")
	default:
		s.writeDomainError(w, r, "geocode", err)
	}
}

// handleGeoSearch finds places by name or address, near lat/lng first.
func (s *Server) handleGeoSearch(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(text)) > maxSearchLength {
		s.writeDomainError(w, r, "validate search", domain.NewValidationError("q", "too_long", "must be at most 200 characters"))
		return
	}
	focus, err := queryPoint(r)
	if err != nil {
		s.writeDomainError(w, r, "validate search", err)
		return
	}
	if len([]rune(text)) < minSearchLength {
		writeJSON(w, s.logger, http.StatusOK, placeListResponse{Items: []geocoding.Place{}})
		return
	}
	places, err := s.geocoder.Search(r.Context(), geocoding.Query{Text: text, Lang: requestLang(r), Focus: focus})
	if err != nil {
		s.writeGeocodingError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, placeListResponse{Items: places})
}

// handleGeoReverse names the places at a position, nearest first.
func (s *Server) handleGeoReverse(w http.ResponseWriter, r *http.Request) {
	point, err := queryPoint(r)
	if err == nil && point == nil {
		err = domain.NewValidationError("lat", "required", "lat and lng are required")
	}
	if err != nil {
		s.writeDomainError(w, r, "validate reverse", err)
		return
	}
	places, err := s.geocoder.Reverse(r.Context(), *point, requestLang(r))
	if err != nil {
		s.writeGeocodingError(w, r, err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, placeListResponse{Items: places})
}

// locationResponse is a position read from coordinates or a link.
type locationResponse struct {
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
	Name string  `json:"name"`
}

// handleParseLink reads a position from pasted coordinates or a Google Maps or
// OpenStreetMap link, expanding Google's short links. It needs no provider.
func (s *Server) handleParseLink(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(r.URL.Query().Get("url"))
	unrecognized := domain.NewValidationError("url", "unrecognized_link", "no position found in the text")
	if text == "" || len(text) > maxLinkLength {
		s.writeDomainError(w, r, "parse link", unrecognized)
		return
	}
	if geocoding.IsShortLink(text) && s.expander != nil {
		expanded, err := s.expander.Expand(r.Context(), text)
		if errors.Is(err, geocoding.ErrUnavailable) {
			s.writeGeocodingError(w, r, err)
			return
		}
		if err == nil {
			text = expanded
		}
	}
	location, err := geocoding.ParseLocation(text)
	if err != nil {
		s.writeDomainError(w, r, "parse link", unrecognized)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, locationResponse{Lat: location.Lat, Lng: location.Lng, Name: location.Name})
}
