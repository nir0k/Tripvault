package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestStatusNamesTheConfiguredProviders checks the status screen says which
// services this instance talks to, including one that is switched off: a hosted
// service with no key builds no client to ask, and the screen must still name it.
func TestStatusNamesTheConfiguredProviders(t *testing.T) {
	admin := domain.User{ID: uuid.New(), IsActive: true, IsAdmin: true}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:              &fakeAuth{user: admin},
		Users:             fakeUsers{},
		Database:          fakeProbe{},
		RoutingProvider:   "osrm",
		GeocodingProvider: "nominatim",
		RoutingDailyLimit: 0,
	})

	recorder := send(s, http.MethodGet, "/api/v1/admin/status", "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
	}

	var status statusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("response %q is not a status: %v", recorder.Body.String(), err)
	}
	if status.Routing.Provider != "osrm" || status.Geocoding.Provider != "nominatim" {
		t.Errorf("the providers are reported as %s and %s",
			status.Routing.Provider, status.Geocoding.Provider)
	}
	// Neither service was built here, so neither counts as configured.
	if status.Routing.Configured || status.Geocoding.Configured {
		t.Error("a service that was never built reports itself as configured")
	}
	if status.Routing.DailyLimit != 0 {
		t.Errorf("the daily limit is %d, want none", status.Routing.DailyLimit)
	}
}

// fakeProbe answers the readiness questions the status screen asks.
type fakeProbe struct{}

// Ping reports a database that answers.
func (fakeProbe) Ping(context.Context) error { return nil }

// SchemaVersion reports the migration the fake database is at.
func (fakeProbe) SchemaVersion(context.Context) (int64, error) { return 1, nil }
