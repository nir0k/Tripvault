package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/routing"
	"github.com/nir0k/tripvault/backend/internal/telemetry"
)

// readinessProbeTimeout bounds dependency checks so a hung database cannot make
// the probe itself hang.
const readinessProbeTimeout = 3 * time.Second

// healthResponse is the payload of the liveness probe.
type healthResponse struct {
	Status string              `json:"status"`
	Build  telemetry.BuildInfo `json:"build"`
}

// readinessResponse is the payload of the readiness probe.
type readinessResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// handleHealth reports that the process is alive and which build runs. It
// checks no dependency: a database outage must not make the orchestrator
// restart a healthy container.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, healthResponse{Status: "ok", Build: telemetry.Build()})
}

// handleReadiness reports whether the service can serve traffic, which needs a
// reachable database. The failure reason is logged, not returned: the probe is
// unauthenticated.
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessProbeTimeout)
	defer cancel()

	body := readinessResponse{Status: "ready", Checks: map[string]string{"database": "ok"}}
	status := http.StatusOK
	if err := s.database.Ping(ctx); err != nil {
		s.logger.Warn("readiness check failed", slog.String("dependency", "database"), slog.Any("error", err))
		body.Status, body.Checks["database"] = "unready", "unavailable"
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, s.logger, status, body)
}

// handleVersion exposes the build information on its own endpoint.
func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, telemetry.Build())
}

// clientConfigResponse is what the web client reads before anybody signs in.
// It carries no secrets.
type clientConfigResponse struct {
	Version string   `json:"version"`
	Locales []string `json:"locales"`
	// RoutingEnabled is false when no routing provider is configured and every
	// road leg is an estimate, which the interface explains.
	RoutingEnabled bool `json:"routing_enabled"`
	// RoutingShortest and RoutingAlternatives say whether the provider can be
	// asked for the shortest route and for other routes than its best.
	RoutingShortest     bool `json:"routing_shortest"`
	RoutingAlternatives bool `json:"routing_alternatives"`
	// GeocodingEnabled is false when place search and reverse lookups are
	// unavailable; coordinates and links still work.
	GeocodingEnabled bool `json:"geocoding_enabled"`
	// MapTileURL and MapAttribution configure the map's raster tiles.
	MapTileURL     string `json:"map_tile_url"`
	MapAttribution string `json:"map_attribution"`
}

// handleClientConfig reports the instance settings the client needs up front.
func (s *Server) handleClientConfig(w http.ResponseWriter, _ *http.Request) {
	var capabilities routing.Capabilities
	if s.routing != nil {
		capabilities = s.routing.Capabilities()
	}
	writeJSON(w, s.logger, http.StatusOK, clientConfigResponse{
		Version:             telemetry.Build().Version,
		Locales:             domain.SupportedLocales,
		RoutingEnabled:      s.routing != nil && s.routing.Enabled(),
		RoutingShortest:     capabilities.Shortest,
		RoutingAlternatives: capabilities.Alternatives,
		GeocodingEnabled:    s.geocoder != nil && s.geocoder.Enabled(),
		MapTileURL:          s.opts.MapTileURL,
		MapAttribution:      s.opts.MapAttribution,
	})
}
