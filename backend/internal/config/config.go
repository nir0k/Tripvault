// Package config loads and validates the service configuration from the
// environment. Every variable carries the TRIPVAULT_ prefix.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/nir0k/tripvault/backend/internal/telemetry"
)

// Environment names the deployment mode the service runs in. Development allows
// a missing signing secret; production refuses to start without a real one.
type Environment string

const (
	// EnvDevelopment is the relaxed mode used for local work and the test stand.
	EnvDevelopment Environment = "development"
	// EnvProduction is the strict mode used for real deployments.
	EnvProduction Environment = "production"
)

// envPrefix is prepended to every variable name.
const envPrefix = "TRIPVAULT_"

// Config is the complete runtime configuration of the API service.
type Config struct {
	Env       Environment `env:"ENV" envDefault:"production"`
	HTTP      HTTP        `envPrefix:"HTTP_"`
	Log       Log         `envPrefix:"LOG_"`
	Database  Database    `envPrefix:"DB_"`
	Auth      Auth        `envPrefix:"AUTH_"`
	Admin     Admin       `envPrefix:"ADMIN_"`
	Routing   Routing     `envPrefix:"ROUTING_"`
	Map       Map         `envPrefix:"MAP_"`
	Media     Media       `envPrefix:"MEDIA_"`
	Geocoding Geocoding   `envPrefix:"GEOCODING_"`
	Backup    Backup      `envPrefix:"BACKUP_"`
	Secrets   Secrets     `envPrefix:"SECRETS_"`

	// DemoData fills an empty instance with two example trips, for looking at
	// the interface with something in it. Development only.
	DemoData bool `env:"DEMO_DATA" envDefault:"false"`

	// LogLevel is the parsed form of Log.Level, filled in by Load.
	LogLevel slog.Level `env:"-"`
}

// Media holds where uploaded files are kept and how much room they may take.
// The path is a directory inside the container, backed by a volume in the
// deployment; the limits are what keeps one trip from filling the disk.
type Media struct {
	Path string `env:"PATH" envDefault:"/app/media"`
	// MaxSizeMB bounds one uploaded file.
	MaxSizeMB int `env:"MAX_SIZE_MB" envDefault:"25"`
	// TripQuotaMB bounds everything one trip keeps; zero means no limit.
	TripQuotaMB int `env:"TRIP_QUOTA_MB" envDefault:"2048"`
	// TrackMaxSizeMB bounds an imported GPX or KML file, which is text and far
	// smaller than a photograph.
	TrackMaxSizeMB int `env:"TRACK_MAX_SIZE_MB" envDefault:"10"`
}

// MaxSizeBytes - returns the largest accepted upload in bytes.
//
// Returns:
//   - the limit of a single file.
func (m Media) MaxSizeBytes() int64 {
	return int64(m.MaxSizeMB) * 1024 * 1024
}

// TripQuotaBytes - returns how much one trip may keep, in bytes.
//
// Returns:
//   - the quota, or zero when a trip is not limited.
func (m Media) TripQuotaBytes() int64 {
	return int64(m.TripQuotaMB) * 1024 * 1024
}

// TrackMaxBytes - returns the largest accepted track file in bytes.
//
// Returns:
//   - the limit of one imported GPX or KML file.
func (m Media) TrackMaxBytes() int64 {
	return int64(m.TrackMaxSizeMB) * 1024 * 1024
}

// Backup holds where archives are written and how often the schedule is
// checked.
//
// What to copy, when, and with what passphrase is not here: it is configured on
// the site by an administrator, because it is their decision rather than the
// operator's, and the passphrase is sealed with the instance's secrets key.
type Backup struct {
	// Path is the directory local backups are written to.
	Path string `env:"PATH" envDefault:"/app/backups"`
	// SchedulerInterval is how often the service looks for a configuration that
	// has come due. A cron expression has a resolution of one minute, so
	// checking more often than that could not find anything new.
	SchedulerInterval time.Duration `env:"SCHEDULER_INTERVAL" envDefault:"1m"`
}

// Secrets holds the key that seals the credentials the service has to store: an
// SFTP password or private key entered in the backup settings, and the
// passphrase archives are locked with.
type Secrets struct {
	// Key is an age secret key, "AGE-SECRET-KEY-1...". It is optional: left
	// empty, the service reads KeyFile instead and makes a key there on its
	// first start. Setting it is for an operator who would rather keep the key
	// in their own secret store.
	Key string `env:"KEY"`
	// KeyFile is where the key is kept when none is configured. It must be on a
	// volume that survives a restart: losing it means every stored credential -
	// the SFTP password, the archive passphrase - has to be entered again.
	KeyFile string `env:"KEY_FILE" envDefault:"/app/config/secrets.key"`
}

// HTTP holds the settings of the HTTP listener.
type HTTP struct {
	Addr            string        `env:"ADDR" envDefault:":8080"`
	ReadTimeout     time.Duration `env:"READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT" envDefault:"60s"`
	IdleTimeout     time.Duration `env:"IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"20s"`
}

// Log holds the logging settings. The format is deliberately absent: the
// service always writes logfmt to stdout.
type Log struct {
	Level string `env:"LEVEL" envDefault:"info"`
}

// Database holds the PostgreSQL connection settings.
type Database struct {
	DSN            string        `env:"DSN"`
	MaxConns       int32         `env:"MAX_CONNS" envDefault:"10"`
	MinConns       int32         `env:"MIN_CONNS" envDefault:"1"`
	ConnectTimeout time.Duration `env:"CONNECT_TIMEOUT" envDefault:"10s"`
}

// Auth holds token signing and sign-in settings.
type Auth struct {
	JWTSecret       string        `env:"JWT_SECRET"`
	AccessTokenTTL  time.Duration `env:"ACCESS_TOKEN_TTL" envDefault:"15m"`
	RefreshTokenTTL time.Duration `env:"REFRESH_TOKEN_TTL" envDefault:"720h"`
}

// Admin holds the credentials of the first administrator, created on the first
// start of an instance that has no accounts yet.
type Admin struct {
	Email       string `env:"EMAIL"`
	Password    string `env:"PASSWORD"`
	DisplayName string `env:"DISPLAY_NAME" envDefault:"Administrator"`
}

// The services that can calculate road routes. openrouteservice is a hosted
// service with a key; the other two are what an operator runs themselves.
const (
	RouterORS      = "ors"
	RouterOSRM     = "osrm"
	RouterValhalla = "valhalla"
)

// The services that can find places. Pelias is the hosted one, behind the same
// key as openrouteservice; the other two are commonly self-hosted.
const (
	GeocoderPelias    = "pelias"
	GeocoderPhoton    = "photon"
	GeocoderNominatim = "nominatim"
)

// defaultRoutingBaseURL and defaultGeocodingBaseURL are the addresses of the
// hosted services. A service of the operator's own has no default: only they
// know where it runs.
const (
	defaultRoutingBaseURL   = "https://api.heigit.org/openrouteservice"
	defaultGeocodingBaseURL = "https://api.heigit.org/pelias/v1"
)

// Routing holds which service calculates road routes and how it is reached.
//
// The settings are named after what they do rather than after one provider, so
// that pointing the service at an OSRM of one's own does not mean filling in a
// variable with another provider's name in it.
type Routing struct {
	// Provider is ors, osrm or valhalla.
	Provider string `env:"PROVIDER" envDefault:"ors"`
	// BaseURL is the API root. Empty means the hosted service's own address,
	// which exists for ors alone: a service of the operator's own has to say
	// where it runs.
	BaseURL string `env:"BASE_URL"`
	// APIKey is sent to services that need one. A service of one's own usually
	// does not, and then routes are calculated without a key.
	APIKey string `env:"API_KEY"`
	// RequestsPerMinute and DailyLimit keep this service below the provider's
	// own limits; the defaults sit under the openrouteservice standard plan (60
	// a minute and 10 000 a day). Zero means no limit, which is what a service
	// of one's own wants.
	RequestsPerMinute int           `env:"REQUESTS_PER_MINUTE" envDefault:"40"`
	DailyLimit        int           `env:"DAILY_LIMIT" envDefault:"9000"`
	CacheTTL          time.Duration `env:"CACHE_TTL" envDefault:"2160h"`
}

// Geocoding holds which service finds places and how it is reached. Its
// settings mirror Routing's, and its limits are counted separately.
type Geocoding struct {
	// Provider is pelias, photon or nominatim.
	Provider string `env:"PROVIDER" envDefault:"pelias"`
	BaseURL  string `env:"BASE_URL"`
	APIKey   string `env:"API_KEY"`
	// The defaults sit under the Pelias standard plan (150 a minute and 15 000
	// a day). Zero means no limit.
	RequestsPerMinute int           `env:"REQUESTS_PER_MINUTE" envDefault:"100"`
	DailyLimit        int           `env:"DAILY_LIMIT" envDefault:"12000"`
	CacheTTL          time.Duration `env:"CACHE_TTL" envDefault:"720h"`
}

// NeedsKey - reports whether the routing service is one that is reached with a
// key, and is therefore switched off without one.
//
// Returns:
//   - true for the hosted service, false for one the operator runs themselves.
func (r Routing) NeedsKey() bool {
	return r.Provider == RouterORS
}

// NeedsKey - reports whether the geocoding service is reached with a key.
//
// Returns:
//   - true for the hosted service, false for one the operator runs themselves.
func (g Geocoding) NeedsKey() bool {
	return g.Provider == GeocoderPelias
}

// Address - returns the API root of the routing service.
//
// Returns:
//   - the configured address, or the hosted service's own when none is set.
func (r Routing) Address() string {
	if address := strings.TrimSpace(r.BaseURL); address != "" {
		return address
	}
	return defaultRoutingBaseURL
}

// Address - returns the API root of the geocoding service.
//
// Returns:
//   - the configured address, or the hosted service's own when none is set.
func (g Geocoding) Address() string {
	if address := strings.TrimSpace(g.BaseURL); address != "" {
		return address
	}
	return defaultGeocodingBaseURL
}

// Map holds the raster tiles the browser draws maps with. The default OSM
// servers allow light use only; a busy instance should use its own provider.
//
// An empty value means the default: compose passes unset variables as empty.
type Map struct {
	TileURL     string `env:"TILE_URL"`
	Attribution string `env:"ATTRIBUTION"`
}

// Default map tiles.
const (
	defaultMapTileURL     = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
	defaultMapAttribution = `&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors`
)

// insecureSecrets are placeholder signing secrets that must never reach
// production.
var insecureSecrets = map[string]bool{
	"":          true,
	"change-me": true,
	"changeme":  true,
	"secret":    true,
}

// minSecretLength is the shortest signing secret production accepts: anything
// shorter than 32 characters is within reach of an offline guess against a
// captured token.
const minSecretLength = 32

// Load - reads and validates the service configuration from the environment.
//
// Returns:
//   - the parsed and validated configuration.
//   - an error listing every invalid or missing setting.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.ParseWithOptions(cfg, env.Options{Prefix: envPrefix}); err != nil {
		return nil, fmt.Errorf("parse environment: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// routers and geocoders are the services each setting accepts, for validation.
var (
	routers   = []string{RouterORS, RouterOSRM, RouterValhalla}
	geocoders = []string{GeocoderPelias, GeocoderPhoton, GeocoderNominatim}
)

// validateRouting checks the routing service can be reached as described.
func (c *Config) validateRouting() []error {
	return validateProvider(serviceSettings{
		prefix: "TRIPVAULT_ROUTING_", role: "routing", provider: c.Routing.Provider, allowed: routers,
		needsKey: c.Routing.NeedsKey(), baseURL: c.Routing.BaseURL, address: c.Routing.Address(),
		perMinute: c.Routing.RequestsPerMinute, daily: c.Routing.DailyLimit, cacheTTL: c.Routing.CacheTTL,
	})
}

// validateGeocoding checks the geocoding service can be reached as described.
func (c *Config) validateGeocoding() []error {
	return validateProvider(serviceSettings{
		prefix: "TRIPVAULT_GEOCODING_", role: "geocoding", provider: c.Geocoding.Provider, allowed: geocoders,
		needsKey: c.Geocoding.NeedsKey(), baseURL: c.Geocoding.BaseURL, address: c.Geocoding.Address(),
		perMinute: c.Geocoding.RequestsPerMinute, daily: c.Geocoding.DailyLimit, cacheTTL: c.Geocoding.CacheTTL,
	})
}

// serviceSettings is an outside service as its settings describe it: routing
// and geocoding are configured alike and checked by the same rules.
type serviceSettings struct {
	// prefix is the variables' common prefix, such as TRIPVAULT_ROUTING_.
	prefix string
	// role names the service in a message: routing or geocoding.
	role      string
	provider  string
	allowed   []string
	needsKey  bool
	baseURL   string
	address   string
	perMinute int
	daily     int
	cacheTTL  time.Duration
}

// validateProvider checks an outside service can be reached as described.
//
// A service of the operator's own has no default address, so leaving it out is
// refused here rather than discovered as every request quietly failing.
//
// Arguments:
//   - service: the settings of the routing or geocoding service.
//
// Returns:
//   - one error per setting that is wrong, named by its variable.
func validateProvider(service serviceSettings) []error {
	var errs []error

	if !slices.Contains(service.allowed, service.provider) {
		last := len(service.allowed) - 1
		errs = append(errs, fmt.Errorf("%sPROVIDER: must be %s or %s, got %q", service.prefix,
			strings.Join(service.allowed[:last], ", "), service.allowed[last], service.provider))
	}
	if !service.needsKey && strings.TrimSpace(service.baseURL) == "" {
		errs = append(errs, fmt.Errorf("%sBASE_URL: required for a %s service of your own",
			service.prefix, service.role))
	}
	if !isHTTPAddress(service.address) {
		errs = append(errs, fmt.Errorf("%sBASE_URL: must be an http or https address", service.prefix))
	}
	if service.perMinute < 0 {
		errs = append(errs, fmt.Errorf("%sREQUESTS_PER_MINUTE: must not be negative", service.prefix))
	}
	if service.daily < 0 {
		errs = append(errs, fmt.Errorf("%sDAILY_LIMIT: must not be negative", service.prefix))
	}
	if service.cacheTTL < time.Hour {
		errs = append(errs, fmt.Errorf("%sCACHE_TTL: must be at least 1h", service.prefix))
	}
	return errs
}

// isHTTPAddress reports whether a value is an address this service can call.
func isHTTPAddress(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// validate rejects configurations that would leave the service unsafe or
// unusable, reporting every problem at once rather than one per restart.
func (c *Config) validate() error {
	var errs []error

	switch c.Env {
	case EnvDevelopment, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("TRIPVAULT_ENV: must be development or production, got %q", c.Env))
	}

	level, err := telemetry.ParseLevel(c.Log.Level)
	if err != nil {
		errs = append(errs, fmt.Errorf("TRIPVAULT_LOG_LEVEL: %w", err))
	}
	c.LogLevel = level

	if c.Database.DSN == "" {
		errs = append(errs, errors.New("TRIPVAULT_DB_DSN: required"))
	}
	if c.Database.MaxConns < 1 {
		errs = append(errs, errors.New("TRIPVAULT_DB_MAX_CONNS: must be at least 1"))
	}
	if strings.TrimSpace(c.Media.Path) == "" {
		errs = append(errs, errors.New("TRIPVAULT_MEDIA_PATH: required"))
	}
	if c.Media.MaxSizeMB < 1 {
		errs = append(errs, errors.New("TRIPVAULT_MEDIA_MAX_SIZE_MB: must be at least 1"))
	}
	if c.Media.TripQuotaMB < 0 {
		errs = append(errs, errors.New("TRIPVAULT_MEDIA_TRIP_QUOTA_MB: must not be negative"))
	}
	if c.Media.TrackMaxSizeMB < 1 {
		errs = append(errs, errors.New("TRIPVAULT_MEDIA_TRACK_MAX_SIZE_MB: must be at least 1"))
	}
	if c.Database.MinConns < 0 || c.Database.MinConns > c.Database.MaxConns {
		errs = append(errs, errors.New("TRIPVAULT_DB_MIN_CONNS: must be between 0 and TRIPVAULT_DB_MAX_CONNS"))
	}

	if c.Auth.AccessTokenTTL < time.Minute {
		errs = append(errs, errors.New("TRIPVAULT_AUTH_ACCESS_TOKEN_TTL: must be at least 1m"))
	}
	if c.Auth.RefreshTokenTTL <= c.Auth.AccessTokenTTL {
		errs = append(errs, errors.New("TRIPVAULT_AUTH_REFRESH_TOKEN_TTL: must be longer than the access token TTL"))
	}
	if c.Env == EnvProduction {
		if insecureSecrets[c.Auth.JWTSecret] || len(c.Auth.JWTSecret) < minSecretLength {
			errs = append(errs, fmt.Errorf(
				"TRIPVAULT_AUTH_JWT_SECRET: a random secret of at least %d characters is required in production",
				minSecretLength))
		}
	}

	if (c.Admin.Email == "") != (c.Admin.Password == "") {
		errs = append(errs, errors.New("TRIPVAULT_ADMIN_EMAIL and TRIPVAULT_ADMIN_PASSWORD: set both or neither"))
	}

	// Example trips carry accounts with published passwords, so a production
	// instance must not be able to create them even by accident.
	if c.DemoData && c.Env == EnvProduction {
		errs = append(errs, errors.New("TRIPVAULT_DEMO_DATA: example trips are for development only"))
	}

	errs = append(errs, c.validateRouting()...)
	errs = append(errs, c.validateGeocoding()...)
	if strings.TrimSpace(c.Map.TileURL) == "" {
		c.Map.TileURL = defaultMapTileURL
	}
	if strings.TrimSpace(c.Map.Attribution) == "" {
		c.Map.Attribution = defaultMapAttribution
	}
	if parsed, err := url.Parse(c.Map.TileURL); err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.Host == "" || !strings.Contains(c.Map.TileURL, "{z}") || !strings.Contains(c.Map.TileURL, "{x}") ||
		!strings.Contains(c.Map.TileURL, "{y}") {
		errs = append(errs, errors.New("TRIPVAULT_MAP_TILE_URL: must be an http or https URL with {z}, {x} and {y}"))
	}
	if strings.TrimSpace(c.Backup.Path) == "" {
		errs = append(errs, errors.New("TRIPVAULT_BACKUP_PATH: required"))
	}
	if c.Backup.SchedulerInterval < time.Minute {
		errs = append(errs, errors.New("TRIPVAULT_BACKUP_SCHEDULER_INTERVAL: must be at least 1m"))
	}
	// The key file is where a key is made when none is configured, so an
	// instance with neither has no way to store a credential at all.
	if strings.TrimSpace(c.Secrets.Key) == "" && strings.TrimSpace(c.Secrets.KeyFile) == "" {
		errs = append(errs, errors.New("TRIPVAULT_SECRETS_KEY_FILE: required unless TRIPVAULT_SECRETS_KEY is set"))
	}

	return errors.Join(errs...)
}
