package config

import (
	"strings"
	"testing"
)

// setBaseEnv sets the variables a valid development configuration needs.
func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TRIPVAULT_ENV", "development")
	t.Setenv("TRIPVAULT_DB_DSN", "postgres://localhost/tripvault")
}

// TestLoadDevelopmentDefaults checks a minimal development configuration loads
// with its documented defaults.
func TestLoadDevelopmentDefaults(t *testing.T) {
	setBaseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Addr != ":8080" || cfg.Log.Level != "info" || cfg.Admin.DisplayName != "Administrator" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

// TestLoadRefusesUnsafeProduction checks production needs a real signing secret
// and that every problem is reported at once.
func TestLoadRefusesUnsafeProduction(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("TRIPVAULT_ENV", "production")
	t.Setenv("TRIPVAULT_AUTH_JWT_SECRET", "change-me")
	t.Setenv("TRIPVAULT_LOG_LEVEL", "verbose")

	_, err := Load()
	if err == nil {
		t.Fatal("an unsafe production configuration loaded")
	}
	for _, want := range []string{"TRIPVAULT_AUTH_JWT_SECRET", "TRIPVAULT_LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}

	t.Setenv("TRIPVAULT_AUTH_JWT_SECRET", strings.Repeat("x", 32))
	t.Setenv("TRIPVAULT_LOG_LEVEL", "warn")
	if _, err := Load(); err != nil {
		t.Errorf("a safe production configuration was refused: %v", err)
	}
}

// TestLoadRequiresBothAdminCredentials checks half an administrator is refused.
func TestLoadRequiresBothAdminCredentials(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("TRIPVAULT_ADMIN_EMAIL", "admin@example.com")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TRIPVAULT_ADMIN_PASSWORD") {
		t.Errorf("an email without a password: %v", err)
	}
}

// TestMapTiles checks empty tile settings fall back to OpenStreetMap and a
// template without its placeholders is refused.
func TestMapTiles(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("TRIPVAULT_MAP_TILE_URL", "")
	cfg, err := Load()
	if err != nil || !strings.Contains(cfg.Map.TileURL, "openstreetmap") || !strings.Contains(cfg.Map.Attribution, "OpenStreetMap") {
		t.Errorf("defaults: %+v %v", cfg, err)
	}
	t.Setenv("TRIPVAULT_MAP_TILE_URL", "https://tiles.example.com/map.png")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TRIPVAULT_MAP_TILE_URL") {
		t.Errorf("a template without placeholders: %v", err)
	}
}

// TestBackupDefaults checks an instance that says nothing about backups still
// has somewhere to write them and somewhere to keep its secrets key.
func TestBackupDefaults(t *testing.T) {
	setBaseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backup.Path != "/app/backups" || cfg.Secrets.KeyFile != "/app/config/secrets.key" {
		t.Errorf("unexpected backup defaults: %+v %+v", cfg.Backup, cfg.Secrets)
	}
}

// TestBackupSettingsAreChecked checks a scheduler interval below the resolution
// of a cron expression, and an instance with nowhere to keep its key, are both
// refused at start-up.
func TestBackupSettingsAreChecked(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("TRIPVAULT_BACKUP_SCHEDULER_INTERVAL", "10s")
	// A variable left empty falls back to the default, so a file of spaces is
	// what "configured, but to nowhere" looks like.
	t.Setenv("TRIPVAULT_SECRETS_KEY_FILE", "   ")

	_, err := Load()
	if err == nil {
		t.Fatal("an unusable backup configuration loaded")
	}
	for _, want := range []string{"TRIPVAULT_BACKUP_SCHEDULER_INTERVAL", "TRIPVAULT_SECRETS_KEY_FILE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}

	// A key configured directly is the other supported way to have one.
	t.Setenv("TRIPVAULT_BACKUP_SCHEDULER_INTERVAL", "5m")
	t.Setenv("TRIPVAULT_SECRETS_KEY", "AGE-SECRET-KEY-1")
	if _, err := Load(); err != nil {
		t.Errorf("a valid backup configuration was refused: %v", err)
	}
}

// TestProviderDefaults checks an instance that says nothing about providers
// talks to the hosted ones, and knows it needs a key for them.
func TestProviderDefaults(t *testing.T) {
	setBaseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Routing.Provider != RouterORS || cfg.Geocoding.Provider != GeocoderPelias {
		t.Errorf("providers = %s, %s", cfg.Routing.Provider, cfg.Geocoding.Provider)
	}
	if !cfg.Routing.NeedsKey() || !cfg.Geocoding.NeedsKey() {
		t.Error("a hosted service was taken to need no key")
	}
	if cfg.Routing.Address() != "https://api.heigit.org/openrouteservice" ||
		cfg.Geocoding.Address() != "https://api.heigit.org/pelias/v1" {
		t.Errorf("addresses = %s, %s", cfg.Routing.Address(), cfg.Geocoding.Address())
	}
}

// TestSelfHostedProvidersNeedAnAddress checks a service of one's own is refused
// without one, rather than quietly falling back to somebody else's server.
func TestSelfHostedProvidersNeedAnAddress(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("TRIPVAULT_ROUTING_PROVIDER", "osrm")
	t.Setenv("TRIPVAULT_GEOCODING_PROVIDER", "photon")

	_, err := Load()
	if err == nil {
		t.Fatal("a self-hosted configuration with no address loaded")
	}
	for _, want := range []string{"TRIPVAULT_ROUTING_BASE_URL", "TRIPVAULT_GEOCODING_BASE_URL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}

	t.Setenv("TRIPVAULT_ROUTING_BASE_URL", "http://osrm:5000")
	t.Setenv("TRIPVAULT_GEOCODING_BASE_URL", "http://photon:2322")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("a self-hosted configuration with addresses was refused: %v", err)
	}
	if cfg.Routing.NeedsKey() || cfg.Geocoding.NeedsKey() {
		t.Error("a service of one's own was taken to need a key")
	}
	if cfg.Routing.Address() != "http://osrm:5000" {
		t.Errorf("the routing address is %q", cfg.Routing.Address())
	}
}

// TestProviderSettingsAreChecked checks an unknown service, an unusable address
// and a negative limit are all refused, and that zero is not: it is how a server
// of one's own says it has no ceiling.
func TestProviderSettingsAreChecked(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("TRIPVAULT_ROUTING_PROVIDER", "graphhopper")
	t.Setenv("TRIPVAULT_GEOCODING_BASE_URL", "not an address")
	t.Setenv("TRIPVAULT_GEOCODING_DAILY_LIMIT", "-1")

	_, err := Load()
	if err == nil {
		t.Fatal("an unusable provider configuration loaded")
	}
	for _, want := range []string{
		"TRIPVAULT_ROUTING_PROVIDER", "TRIPVAULT_GEOCODING_BASE_URL", "TRIPVAULT_GEOCODING_DAILY_LIMIT",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}

	// The variables set above outlive the check they were set for, so the ones
	// that were deliberately wrong are put right before the next case.
	t.Setenv("TRIPVAULT_GEOCODING_BASE_URL", "")
	t.Setenv("TRIPVAULT_GEOCODING_DAILY_LIMIT", "12000")
	t.Setenv("TRIPVAULT_ROUTING_PROVIDER", "valhalla")
	t.Setenv("TRIPVAULT_ROUTING_BASE_URL", "http://valhalla:8002")
	t.Setenv("TRIPVAULT_ROUTING_REQUESTS_PER_MINUTE", "0")
	t.Setenv("TRIPVAULT_ROUTING_DAILY_LIMIT", "0")
	if _, err := Load(); err != nil {
		t.Errorf("a server of one's own with no limits was refused: %v", err)
	}
}
