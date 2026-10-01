// Package config loads and validates the service configuration from the
// environment. Every variable carries the TRIPVAULT_ prefix.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	stdmail "net/mail"
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
	Storage   Storage     `envPrefix:"STORAGE_"`
	Geocoding Geocoding   `envPrefix:"GEOCODING_"`
	Backup    Backup      `envPrefix:"BACKUP_"`
	Secrets   Secrets     `envPrefix:"SECRETS_"`
	Mail      Mail        `envPrefix:"MAIL_"`

	// LogLevel is the parsed form of Log.Level, filled in by Load.
	LogLevel slog.Level `env:"-"`
}

// Media holds where uploaded files are kept, the single-file limits and the
// initial per-trip policy used before an administrator stores one.
type Media struct {
	Path string `env:"PATH" envDefault:"/app/media"`
	// MaxSizeMB bounds one uploaded file.
	MaxSizeMB int `env:"MAX_SIZE_MB" envDefault:"25"`
	// TripQuotaMB seeds the stored per-trip policy; zero means no limit.
	TripQuotaMB int `env:"TRIP_QUOTA_MB" envDefault:"2048"`
	// TrackMaxSizeMB bounds an imported GPX or KML file, which is text and far
	// smaller than a photograph.
	TrackMaxSizeMB int `env:"TRACK_MAX_SIZE_MB" envDefault:"10"`
	// AttachmentMaxSizeMB bounds one file attached to a place, such as a
	// ticket or a booking.
	AttachmentMaxSizeMB int `env:"ATTACHMENT_MAX_SIZE_MB" envDefault:"10"`
}

// Storage holds the operator's ceiling for user files across the instance.
// Database housekeeping, generated caches and backup archives are outside this
// allowance and need capacity limits at the deployment layer.
type Storage struct {
	// QuotaMB bounds original media, attachments and track files together; zero
	// leaves the instance unlimited by the application.
	QuotaMB int64 `env:"QUOTA_MB" envDefault:"0"`
}

// QuotaBytes - returns the instance-wide user-file allowance in bytes.
//
// Returns:
//   - the quota, or zero when the instance is not limited.
func (s Storage) QuotaBytes() int64 {
	return s.QuotaMB * 1024 * 1024
}

// MaxSizeBytes - returns the largest accepted upload in bytes.
//
// Returns:
//   - the limit of a single file.
func (m Media) MaxSizeBytes() int64 {
	return int64(m.MaxSizeMB) * 1024 * 1024
}

// TripQuotaBytes - returns the initial allowance for one trip, in bytes.
//
// Returns:
//   - the initial quota, or zero when a trip is not limited.
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

// AttachmentMaxBytes - returns the largest accepted attachment in bytes.
//
// Returns:
//   - the limit of one file attached to a place.
func (m Media) AttachmentMaxBytes() int64 {
	return int64(m.AttachmentMaxSizeMB) * 1024 * 1024
}

// Backup holds where archives are written.
//
// What to copy, when, and with what passphrase is not here: it is configured on
// the site by an administrator, because it is their decision rather than the
// operator's, and the passphrase is sealed with the instance's secrets key.
type Backup struct {
	// Path is the directory local backups are written to.
	Path string `env:"PATH" envDefault:"/app/backups"`
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

// Mail holds the operator-controlled SMTP connection and the public address
// used in links. Administrators decide separately whether delivery is enabled.
type Mail struct {
	Host        string `env:"HOST"`
	Port        int    `env:"PORT" envDefault:"587"`
	Username    string `env:"USERNAME"`
	Password    string `env:"PASSWORD"`
	TLS         string `env:"TLS" envDefault:"starttls"`
	FromAddress string `env:"FROM_ADDRESS"`
	FromName    string `env:"FROM_NAME" envDefault:"Tripvault"`
	PublicURL   string `env:"PUBLIC_URL"`
}

// Configured - reports whether the deployment supplies a complete SMTP target.
//
// Returns:
//   - true when mail can be sent.
func (m Mail) Configured() bool {
	return strings.TrimSpace(m.Host) != "" && strings.TrimSpace(m.FromAddress) != "" && strings.TrimSpace(m.PublicURL) != ""
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
	// RequestsPerMinute and DailyLimit override the limits this service keeps
	// itself under, which are otherwise worked out from its address (Limits).
	// Zero means no limit.
	RequestsPerMinute *int          `env:"REQUESTS_PER_MINUTE"`
	DailyLimit        *int          `env:"DAILY_LIMIT"`
	CacheTTL          time.Duration `env:"CACHE_TTL" envDefault:"2160h"`
}

// Geocoding holds which service finds places and how it is reached. Its
// settings mirror Routing's, and its limits are counted separately.
type Geocoding struct {
	// Provider is pelias, photon or nominatim.
	Provider string `env:"PROVIDER" envDefault:"pelias"`
	BaseURL  string `env:"BASE_URL"`
	APIKey   string `env:"API_KEY"`
	// RequestsPerMinute and DailyLimit override the limits worked out from the
	// address, as Routing's do. Zero means no limit.
	RequestsPerMinute *int          `env:"REQUESTS_PER_MINUTE"`
	DailyLimit        *int          `env:"DAILY_LIMIT"`
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

// Limits are how many requests this service allows itself to send to an
// outside one, per minute and per rolling day. Zero means no limit.
type Limits struct {
	PerMinute int
	Daily     int
}

// routingLimits and geocodingLimits are the limits kept by default, by the host
// of the service's address. They sit under the plans and usage policies of the
// public servers: the openrouteservice standard plan (60 a minute, 10 000 a
// day), the Pelias one (150 a minute, 15 000 a day), and the one request a
// second the OSRM demo server and the public Nominatim ask for. A server of the
// operator's own is in neither list and is not limited.
var (
	routingLimits = map[string]Limits{
		"api.heigit.org":          {PerMinute: 40, Daily: 9000},
		"router.project-osrm.org": {PerMinute: 20, Daily: 2000},
	}
	geocodingLimits = map[string]Limits{
		"api.heigit.org":              {PerMinute: 100, Daily: 12000},
		"nominatim.openstreetmap.org": {PerMinute: 50},
	}
)

// Limits - returns the limits the routing service is called under.
//
// Returns:
//   - the limits configured, or those of its address when none are.
func (r Routing) Limits() Limits {
	return resolveLimits(routingLimits, r.Address(), r.RequestsPerMinute, r.DailyLimit)
}

// Limits - returns the limits the geocoding service is called under.
//
// Returns:
//   - the limits configured, or those of its address when none are.
func (g Geocoding) Limits() Limits {
	return resolveLimits(geocodingLimits, g.Address(), g.RequestsPerMinute, g.DailyLimit)
}

// resolveLimits works out the limits of one outside service.
//
// Arguments:
//   - known: the default limits by host.
//   - address: the service's API root.
//   - perMinute, daily: the configured overrides, nil when not set.
//
// Returns:
//   - the limits of the address's host, each replaced by its override if set.
func resolveLimits(known map[string]Limits, address string, perMinute, daily *int) Limits {
	var limits Limits
	if parsed, err := url.Parse(address); err == nil {
		limits = known[strings.ToLower(parsed.Hostname())]
	}
	if perMinute != nil {
		limits.PerMinute = *perMinute
	}
	if daily != nil {
		limits.Daily = *daily
	}
	return limits
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
		limits: c.Routing.Limits(), cacheTTL: c.Routing.CacheTTL,
	})
}

// validateGeocoding checks the geocoding service can be reached as described.
func (c *Config) validateGeocoding() []error {
	return validateProvider(serviceSettings{
		prefix: "TRIPVAULT_GEOCODING_", role: "geocoding", provider: c.Geocoding.Provider, allowed: geocoders,
		needsKey: c.Geocoding.NeedsKey(), baseURL: c.Geocoding.BaseURL, address: c.Geocoding.Address(),
		limits: c.Geocoding.Limits(), cacheTTL: c.Geocoding.CacheTTL,
	})
}

// serviceSettings is an outside service as its settings describe it: routing
// and geocoding are configured alike and checked by the same rules.
type serviceSettings struct {
	// prefix is the variables' common prefix, such as TRIPVAULT_ROUTING_.
	prefix string
	// role names the service in a message: routing or geocoding.
	role     string
	provider string
	allowed  []string
	needsKey bool
	baseURL  string
	address  string
	limits   Limits
	cacheTTL time.Duration
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
	if service.limits.PerMinute < 0 {
		errs = append(errs, fmt.Errorf("%sREQUESTS_PER_MINUTE: must not be negative", service.prefix))
	}
	if service.limits.Daily < 0 {
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

// isPublicMailAddress checks an action-link base is an origin rather than an
// address whose query or fragment could swallow the appended application path.
func isPublicMailAddress(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" &&
		(parsed.Path == "" || parsed.Path == "/") && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.User == nil
}

// normalizeMailAddress checks that a configured sender is one mailbox rather
// than a header fragment. The display name is configured separately.
func normalizeMailAddress(value string) (string, error) {
	parsed, err := stdmail.ParseAddress(strings.TrimSpace(value))
	if err != nil || parsed.Address != strings.TrimSpace(value) {
		return "", errors.New("must be one email address without a display name")
	}
	return parsed.Address, nil
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
	if c.Media.AttachmentMaxSizeMB < 1 {
		errs = append(errs, errors.New("TRIPVAULT_MEDIA_ATTACHMENT_MAX_SIZE_MB: must be at least 1"))
	}
	if c.Storage.QuotaMB < 0 || c.Storage.QuotaMB > math.MaxInt64/(1024*1024) {
		errs = append(errs, errors.New("TRIPVAULT_STORAGE_QUOTA_MB: must be a non-negative size that fits in bytes"))
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
	mailFields := []string{strings.TrimSpace(c.Mail.Host), strings.TrimSpace(c.Mail.FromAddress), strings.TrimSpace(c.Mail.PublicURL)}
	mailParts := 0
	for _, value := range mailFields {
		if value != "" {
			mailParts++
		}
	}
	if mailParts != 0 && mailParts != len(mailFields) {
		errs = append(errs, errors.New("TRIPVAULT_MAIL_HOST, TRIPVAULT_MAIL_FROM_ADDRESS and TRIPVAULT_MAIL_PUBLIC_URL: set all or none"))
	}
	if c.Mail.Port < 1 || c.Mail.Port > 65535 {
		errs = append(errs, errors.New("TRIPVAULT_MAIL_PORT: must be between 1 and 65535"))
	}
	if !slices.Contains([]string{"starttls", "tls", "none"}, c.Mail.TLS) {
		errs = append(errs, errors.New("TRIPVAULT_MAIL_TLS: must be starttls, tls or none"))
	}
	if (strings.TrimSpace(c.Mail.Username) == "") != (c.Mail.Password == "") {
		errs = append(errs, errors.New("TRIPVAULT_MAIL_USERNAME and TRIPVAULT_MAIL_PASSWORD: set both or neither"))
	}
	if !c.Mail.Configured() && (strings.TrimSpace(c.Mail.Username) != "" || c.Mail.Password != "") {
		errs = append(errs, errors.New("TRIPVAULT_MAIL_USERNAME and TRIPVAULT_MAIL_PASSWORD: require a configured mail host, sender and public URL"))
	}
	if c.Mail.TLS == "none" && strings.TrimSpace(c.Mail.Username) != "" {
		errs = append(errs, errors.New("TRIPVAULT_MAIL_TLS: SMTP authentication requires starttls or tls"))
	}
	if c.Mail.Configured() && !isPublicMailAddress(c.Mail.PublicURL) {
		errs = append(errs, errors.New("TRIPVAULT_MAIL_PUBLIC_URL: must be an http or https origin without a path, query, credentials or fragment"))
	}
	if c.Mail.Configured() {
		if _, err := normalizeMailAddress(c.Mail.FromAddress); err != nil {
			errs = append(errs, fmt.Errorf("TRIPVAULT_MAIL_FROM_ADDRESS: %w", err))
		}
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
	// The key file is where a key is made when none is configured, so an
	// instance with neither has no way to store a credential at all.
	if strings.TrimSpace(c.Secrets.Key) == "" && strings.TrimSpace(c.Secrets.KeyFile) == "" {
		errs = append(errs, errors.New("TRIPVAULT_SECRETS_KEY_FILE: required unless TRIPVAULT_SECRETS_KEY is set"))
	}

	return errors.Join(errs...)
}
