package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/geocoding"
)

// fakeGeocoder records the query and answers with one place, or fails.
type fakeGeocoder struct {
	query geocoding.Query
	err   error
}

// Enabled reports a configured geocoder.
func (f *fakeGeocoder) Enabled() bool { return f.err != geocoding.ErrDisabled }

// Search records the query.
func (f *fakeGeocoder) Search(_ context.Context, query geocoding.Query) ([]geocoding.Place, error) {
	f.query = query
	return []geocoding.Place{{Name: "Vík", Lat: 63.41, Lng: -19.0}}, f.err
}

// Reverse answers like Search.
func (f *fakeGeocoder) Reverse(_ context.Context, _ domain.Point, lang string) ([]geocoding.Place, error) {
	f.query = geocoding.Query{Lang: lang}
	return []geocoding.Place{{Name: "Vík"}}, f.err
}

// TestGeoEndpoints checks parameters, the language fallback and error mapping.
func TestGeoEndpoints(t *testing.T) {
	geocoder := &fakeGeocoder{}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:     &fakeAuth{user: domain.User{ID: uuid.New(), IsActive: true, Locale: "ru"}},
		Geocoder: geocoder,
	})

	recorder := send(s, http.MethodGet, "/api/v1/geo/search?q=V%C3%ADk&lat=63.4&lng=-19", "good", "")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"name":"Vík"`) ||
		geocoder.query.Lang != "ru" || geocoder.query.Focus == nil {
		t.Errorf("search: %d %s %+v", recorder.Code, recorder.Body.String(), geocoder.query)
	}
	if recorder = send(s, http.MethodGet, "/api/v1/geo/search?q=V", "good", ""); !strings.Contains(recorder.Body.String(), `"items":[]`) {
		t.Errorf("short query: %s", recorder.Body.String())
	}
	if recorder = send(s, http.MethodGet, "/api/v1/geo/reverse?lat=95&lng=0", "good", ""); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad point: %d", recorder.Code)
	}
	if recorder = send(s, http.MethodGet, "/api/v1/geo/reverse?lat=63.4&lng=-19&lang=en", "good", ""); recorder.Code != http.StatusOK ||
		geocoder.query.Lang != "en" {
		t.Errorf("reverse: %d %+v", recorder.Code, geocoder.query)
	}

	geocoder.err = geocoding.ErrDisabled
	if recorder = send(s, http.MethodGet, "/api/v1/geo/search?q=Vik", "good", ""); recorder.Code != http.StatusServiceUnavailable ||
		errorCode(t, recorder) != "geocoding_disabled" {
		t.Errorf("disabled: %d %s", recorder.Code, recorder.Body.String())
	}

	link := "/api/v1/geo/parse-link?url=" + "https%3A%2F%2Fwww.google.com%2Fmaps%2Fplace%2FV%C3%ADk%2F%4063.41%2C-19.0%2C14z"
	if recorder = send(s, http.MethodGet, link, "good", ""); recorder.Code != http.StatusOK ||
		!strings.Contains(recorder.Body.String(), `"name":"Vík"`) {
		t.Errorf("parse link: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder = send(s, http.MethodGet, "/api/v1/geo/parse-link?url=Reykjavik", "good", ""); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("unrecognized link: %d", recorder.Code)
	}
}
