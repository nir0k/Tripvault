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
	if cfg.Storage.QuotaBytes() != 0 || cfg.Media.TripQuotaBytes() != 2048*1024*1024 {
		t.Errorf("unexpected storage defaults: %+v %+v", cfg.Storage, cfg.Media)
	}
}

// TestStorageQuotaValidation checks an instance ceiling may be disabled but
// never made negative.
func TestStorageQuotaValidation(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("TRIPVAULT_STORAGE_QUOTA_MB", "-1")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TRIPVAULT_STORAGE_QUOTA_MB") {
		t.Fatalf("a negative storage quota loaded: %v", err)
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

// TestMailSettings checks SMTP is either absent or complete and never sends
// credentials over a deliberately unsecured connection.
func TestMailSettings(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("TRIPVAULT_MAIL_HOST", "smtp.example.com")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TRIPVAULT_MAIL_FROM_ADDRESS") {
		t.Fatalf("a partial mail target loaded: %v", err)
	}
	t.Setenv("TRIPVAULT_MAIL_FROM_ADDRESS", "tripvault@example.com")
	t.Setenv("TRIPVAULT_MAIL_PUBLIC_URL", "https://trips.example.com")
	t.Setenv("TRIPVAULT_MAIL_USERNAME", "tripvault")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TRIPVAULT_MAIL_PASSWORD") {
		t.Fatalf("a partial mail login loaded: %v", err)
	}
	t.Setenv("TRIPVAULT_MAIL_PASSWORD", "secret")
	t.Setenv("TRIPVAULT_MAIL_TLS", "none")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "requires starttls or tls") {
		t.Fatalf("an unsecured mail login loaded: %v", err)
	}
	t.Setenv("TRIPVAULT_MAIL_TLS", "tls")
	t.Setenv("TRIPVAULT_MAIL_PUBLIC_URL", "https://trips.example.com/login?next=mail")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "without a path") {
		t.Fatalf("a public URL with an application path loaded: %v", err)
	}
	t.Setenv("TRIPVAULT_MAIL_PUBLIC_URL", "https://trips.example.com")
	cfg, err := Load()
	if err != nil || !cfg.Mail.Configured() {
		t.Fatalf("a complete mail target was refused: %+v %v", cfg.Mail, err)
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

// TestBackupSettingsAreChecked checks an instance with nowhere to keep its key
// is refused at start-up.
func TestBackupSettingsAreChecked(t *testing.T) {
	setBaseEnv(t)
	// A variable left empty falls back to the default, so a file of spaces is
	// what "configured, but to nowhere" looks like.
	t.Setenv("TRIPVAULT_SECRETS_KEY_FILE", "   ")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "TRIPVAULT_SECRETS_KEY_FILE") {
		t.Fatalf("an instance with nowhere to keep its key: %v", err)
	}

	// A key configured directly is the other supported way to have one.
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

// TestProviderLimits checks the limits are worked out from the service's
// address - the hosted plans, the public servers' usage policies, none for a
// server of one's own - and that a limit set explicitly wins, zero included.
func TestProviderLimits(t *testing.T) {
	setBaseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Routing.Limits(); got != (Limits{PerMinute: 40, Daily: 9000}) {
		t.Errorf("hosted routing limits = %+v", got)
	}
	if got := cfg.Geocoding.Limits(); got != (Limits{PerMinute: 100, Daily: 12000}) {
		t.Errorf("hosted geocoding limits = %+v", got)
	}

	t.Setenv("TRIPVAULT_ROUTING_PROVIDER", "osrm")
	t.Setenv("TRIPVAULT_ROUTING_BASE_URL", "https://router.project-osrm.org")
	t.Setenv("TRIPVAULT_GEOCODING_PROVIDER", "nominatim")
	t.Setenv("TRIPVAULT_GEOCODING_BASE_URL", "https://nominatim.openstreetmap.org")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Routing.Limits(); got != (Limits{PerMinute: 20, Daily: 2000}) {
		t.Errorf("OSRM demo server limits = %+v", got)
	}
	if got := cfg.Geocoding.Limits(); got != (Limits{PerMinute: 50}) {
		t.Errorf("public Nominatim limits = %+v", got)
	}

	t.Setenv("TRIPVAULT_ROUTING_BASE_URL", "http://osrm:5000")
	t.Setenv("TRIPVAULT_GEOCODING_REQUESTS_PER_MINUTE", "0")
	t.Setenv("TRIPVAULT_GEOCODING_DAILY_LIMIT", "500")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Routing.Limits(); got != (Limits{}) {
		t.Errorf("a server of one's own is limited: %+v", got)
	}
	if got := cfg.Geocoding.Limits(); got != (Limits{Daily: 500}) {
		t.Errorf("explicit limits = %+v", got)
	}
}
