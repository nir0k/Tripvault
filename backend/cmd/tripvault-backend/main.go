// Command tripvault-backend is the HTTP API server of the Tripvault trip
// planning service.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	// The IANA time zone database is embedded so the runtime image needs no
	// tzdata package, which keeps its stage free of RUN instructions.
	_ "time/tzdata"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/auth"
	"github.com/nir0k/tripvault/backend/internal/backup"
	"github.com/nir0k/tripvault/backend/internal/bootstrap"
	"github.com/nir0k/tripvault/backend/internal/config"
	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/geocoding"
	"github.com/nir0k/tripvault/backend/internal/httpapi"
	"github.com/nir0k/tripvault/backend/internal/media"
	"github.com/nir0k/tripvault/backend/internal/routing"
	"github.com/nir0k/tripvault/backend/internal/secrets"
	"github.com/nir0k/tripvault/backend/internal/staticmap"
	"github.com/nir0k/tripvault/backend/internal/storage/migrations"
	"github.com/nir0k/tripvault/backend/internal/storage/postgres"
	"github.com/nir0k/tripvault/backend/internal/telemetry"
)

// Start-up wait for PostgreSQL. Compose health checks normally make it
// unnecessary, but a cold database may still be initialising.
const (
	databaseWaitTimeout  = 60 * time.Second
	databaseWaitInterval = 2 * time.Second
)

// sweepInterval is how often sessions that can never be used again and expired
// cache entries are deleted; sessionRetention is how long a finished session is
// kept before that.
const (
	sweepInterval    = time.Hour
	sessionRetention = 7 * 24 * time.Hour
)

// main parses flags, runs the service and maps a fatal error to exit 1.
func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	makeSecretsKey := flag.Bool("secrets-key", false,
		"print a new TRIPVAULT_SECRETS_KEY and exit")
	restorePath := flag.String("restore", "", "restore the whole instance from this archive and exit")
	restorePassphraseFile := flag.String("restore-passphrase-file", "",
		"read the restore archive passphrase from this file")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s\n", telemetry.ServiceName, telemetry.Build().Version)
		return
	}

	// An operator who would rather keep the key in their own secret store than
	// in a volume needs a way to make one without the service running.
	if *makeSecretsKey {
		key, err := secrets.GenerateKey()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", telemetry.ServiceName, err)
			os.Exit(1)
		}
		fmt.Println(key)
		return
	}
	if *restorePath != "" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if err := restoreArchive(ctx, *restorePath, *restorePassphraseFile); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", telemetry.ServiceName, err)
			os.Exit(1)
		}
		return
	}

	if err := serve(); err != nil {
		// The logger may not exist yet, so failures go to stderr directly.
		fmt.Fprintf(os.Stderr, "%s: %v\n", telemetry.ServiceName, err)
		os.Exit(1)
	}
}

// restoreArchive replaces the configured database and media store before the
// HTTP service starts. It is the disaster-recovery path when no working web
// interface is available.
func restoreArchive(ctx context.Context, archivePath, passphrasePath string) error {
	cfg, logger, pool, err := openDatabase(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	mediaFiles, err := media.NewLocalStore(cfg.Media.Path)
	if err != nil {
		return fmt.Errorf("open media store: %w", err)
	}
	restoreWorkDir, err := prepareRestoreWorkDir(cfg.Media.Path)
	if err != nil {
		return err
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open restore archive: %w", err)
	}
	defer func() { _ = archive.Close() }()

	passphrase := ""
	if passphrasePath != "" {
		value, err := os.ReadFile(passphrasePath)
		if err != nil {
			return fmt.Errorf("read restore passphrase: %w", err)
		}
		passphrase = strings.TrimRight(string(value), "\r\n")
	}
	plain, err := backup.Plain(archive, backup.Encryption{Passphrase: passphrase})
	if err != nil {
		return err
	}
	result, err := backup.Restore(ctx, plain, restoreWorkDir,
		migrations.NewMigrator(pool, logger), mediaFiles, nil)
	if err != nil {
		return err
	}
	fmt.Printf("Restored %d trips, %d tables, and %d files (schema %d).\n",
		result.Trips, result.Tables, result.Files, result.SchemaVersion)
	return nil
}

// prepareRestoreWorkDir removes staging left by an interrupted process and
// recreates the dedicated directory beside the media generations.
func prepareRestoreWorkDir(mediaPath string) (string, error) {
	workDir := filepath.Join(mediaPath, ".restore")
	if err := os.RemoveAll(workDir); err != nil {
		return "", fmt.Errorf("clear restore staging: %w", err)
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", fmt.Errorf("create restore staging: %w", err)
	}
	return workDir, nil
}

// openDatabase loads the configuration, builds the logger and waits for the
// database. The caller closes the pool.
func openDatabase(ctx context.Context) (*config.Config, *slog.Logger, *pgxpool.Pool, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, err
	}
	logger := telemetry.NewLogger(os.Stdout, cfg.LogLevel)

	pool, err := postgres.WaitForPool(ctx, postgres.PoolConfig{
		DSN:            cfg.Database.DSN,
		MaxConns:       cfg.Database.MaxConns,
		MinConns:       cfg.Database.MinConns,
		ConnectTimeout: cfg.Database.ConnectTimeout,
	}, databaseWaitTimeout, databaseWaitInterval)
	if err != nil {
		return nil, nil, nil, err
	}
	return cfg, logger, pool, nil
}

// serve wires up the service and blocks until a termination signal triggers a
// graceful shutdown.
func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, logger, pool, err := openDatabase(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	logger.Info("starting", slog.String("environment", string(cfg.Env)))

	if err := migrations.Apply(ctx, pool, logger); err != nil {
		return err
	}
	version, err := migrations.Version(ctx, pool)
	if err != nil {
		return err
	}
	logger.Info("database ready", slog.Int64("schema_version", version))

	users := postgres.NewUserRepository(pool)
	sessions := postgres.NewSessionRepository(pool)
	trips := postgres.NewTripRepository(pool)
	documents := postgres.NewDocumentRepository(pool)
	mediaCatalogue := postgres.NewMediaRepository(pool)
	mediaFiles, err := media.NewLocalStore(cfg.Media.Path)
	if err != nil {
		return fmt.Errorf("open media store: %w", err)
	}
	if err := media.StartRenderer(logger); err != nil {
		return fmt.Errorf("start the preview renderer: %w", err)
	}
	logger.Info("preview renderer ready", slog.String("libvips", media.RendererLibraryVersion()))
	restoreWorkDir, err := prepareRestoreWorkDir(cfg.Media.Path)
	if err != nil {
		return err
	}
	routes := postgres.NewRoutingRepository(pool)

	router := routing.NewService(roadRouter(cfg, logger), routes, routes, routing.Options{
		PerMinute: cfg.Routing.RequestsPerMinute,
		Daily:     cfg.Routing.DailyLimit,
		CacheTTL:  cfg.Routing.CacheTTL,
	}, logger)

	geocodes := postgres.NewGeocodingRepository(pool)
	places := geocoding.NewService(placeFinder(cfg, logger), geocodes, geocodes, geocoding.Options{
		PerMinute: cfg.Geocoding.RequestsPerMinute,
		Daily:     cfg.Geocoding.DailyLimit,
		CacheTTL:  cfg.Geocoding.CacheTTL,
	}, logger)

	if err := bootstrap.Seed(ctx, users, bootstrap.Options{
		Email:       cfg.Admin.Email,
		Password:    cfg.Admin.Password,
		DisplayName: cfg.Admin.DisplayName,
	}, logger); err != nil {
		return err
	}

	// The example trips come after the administrator, since they belong to them.
	// A failure here is logged rather than fatal: they are a convenience for
	// looking at the interface, and a service that refuses to start without them
	// is worse than one that starts empty.
	if cfg.DemoData {
		if err := bootstrap.SeedDemo(ctx, users, trips, documents, logger); err != nil {
			logger.Error("could not create the example trips", slog.Any("error", err))
		}
	}

	secret, err := signingSecret(cfg, logger)
	if err != nil {
		return err
	}
	issuer, err := auth.NewTokenIssuer(secret, cfg.Auth.AccessTokenTTL, cfg.Auth.RefreshTokenTTL)
	if err != nil {
		return err
	}

	tiles := postgres.NewTileRepository(pool)
	go sweep(ctx, sessions, routes, geocodes, tiles, logger)

	backups := postgres.NewBackupRepository(pool)
	sealer, err := instanceSecrets(cfg, logger)
	if err != nil {
		return err
	}
	// A backup directory that cannot be opened leaves the service running
	// without backups rather than refusing to start: an instance nobody can
	// reach is worse than one nobody can copy until the volume is fixed.
	var runner *backup.Runner
	var destinations *backup.Resolver
	archives, err := backup.NewLocalDestination(cfg.Backup.Path)
	if err != nil {
		logger.Error("backups are unavailable",
			slog.String("path", cfg.Backup.Path), slog.Any("error", err))
	} else {
		destinations = backup.NewResolver(archives, sealer, backups)
		runner = backup.NewRunner(backups, mediaFiles, destinations, backups,
			logger, telemetry.Build().Version)
		go backup.NewScheduler(backups, runner, logger, cfg.Backup.SchedulerInterval).Run(ctx)
	}

	server := httpapi.NewServer(httpapi.Options{
		Addr:            cfg.HTTP.Addr,
		ReadTimeout:     cfg.HTTP.ReadTimeout,
		WriteTimeout:    cfg.HTTP.WriteTimeout,
		IdleTimeout:     cfg.HTTP.IdleTimeout,
		ShutdownTimeout: cfg.HTTP.ShutdownTimeout,
		MapTileURL:      cfg.Map.TileURL,
		MapAttribution:  cfg.Map.Attribution,
	}, logger, httpapi.Dependencies{
		Auth:              auth.NewService(users, sessions, issuer),
		Users:             users,
		Sessions:          sessions,
		Trips:             trips,
		Documents:         documents,
		Media:             mediaCatalogue,
		MediaFiles:        mediaFiles,
		MediaMaxBytes:     cfg.Media.MaxSizeBytes(),
		MediaTripQuota:    cfg.Media.TripQuotaBytes(),
		TrackMaxBytes:     cfg.Media.TrackMaxBytes(),
		Routing:           router,
		RoutingStats:      routes,
		RoutingDailyLimit: cfg.Routing.DailyLimit,
		RoutingProvider:   cfg.Routing.Provider,
		GeocodingProvider: cfg.Geocoding.Provider,
		Geocoder:          places,
		GeocodeStats:      geocodes,
		GeocodeDailyLimit: cfg.Geocoding.DailyLimit,
		Expander:          geocoding.NewExpander(),
		Database:          postgres.NewProbe(pool),
		// The report's PDF draws its maps over the same tiles the interface
		// shows, fetched by the server and kept in the database for a month.
		MapTiles: staticmap.NewFetcher(cfg.Map.TileURL, telemetry.Build().Version, tiles, logger),

		Backups:       backupStore(backups, runner),
		BackupService: backupServiceOf(runner, destinations, archives, ctx),
		Sealer:        sealerOrNil(sealer),
		Migrator:      migrations.NewMigrator(pool, logger),
		RestoreFiles:  mediaFiles,
		// The archive is unpacked and verified beside the media it will replace,
		// so a restore fails on a full disk before it empties the database
		// rather than after.
		RestoreWorkDir: restoreWorkDir,
		WorkContext:    ctx,
	})
	if err := server.Run(ctx); err != nil {
		return err
	}
	logger.Info("stopped")
	return nil
}

// signingSecret resolves the key that signs access tokens.
//
// Production refuses to start without a real secret (see config). In
// development an empty value is replaced by a random one, so tokens are still
// properly signed; the cost - sessions end on restart - is logged.
func signingSecret(cfg *config.Config, logger *slog.Logger) (string, error) {
	if cfg.Auth.JWTSecret != "" {
		return cfg.Auth.JWTSecret, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate a development signing secret: %w", err)
	}
	logger.Warn("no token signing secret is configured, using a random one",
		slog.String("effect", "every session ends when the service restarts"),
		slog.String("hint", "set TRIPVAULT_AUTH_JWT_SECRET"))
	return base64.RawStdEncoding.EncodeToString(buf), nil
}

// sweep periodically deletes sessions that expired or were revoked more than
// sessionRetention ago, and expired routes, geocodes and map tiles, until the
// context ends.
func sweep(ctx context.Context, sessions *postgres.SessionRepository, routes *postgres.RoutingRepository, geocodes *postgres.GeocodingRepository, tiles *postgres.TileRepository,
	logger *slog.Logger) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			removed, err := sessions.DeleteFinished(ctx, now.Add(-sessionRetention))
			if err != nil {
				logger.Warn("session sweep failed", slog.Any("error", err))
			} else if removed > 0 {
				logger.Debug("swept finished sessions", slog.Int64("removed", removed))
			}

			if expired, err := routes.Purge(ctx, now); err != nil {
				logger.Warn("route cache purge failed", slog.Any("error", err))
			} else if expired > 0 {
				logger.Debug("purged expired routes", slog.Int64("removed", expired))
			}

			if expired, err := geocodes.Purge(ctx, now); err != nil {
				logger.Warn("geocode cache purge failed", slog.Any("error", err))
			} else if expired > 0 {
				logger.Debug("purged expired geocodes", slog.Int64("removed", expired))
			}

			if expired, err := tiles.Purge(ctx, now); err != nil {
				logger.Warn("tile cache purge failed", slog.Any("error", err))
			} else if expired > 0 {
				logger.Debug("purged expired map tiles", slog.Int64("removed", expired))
			}
		}
	}
}

// roadRouter - builds the service that calculates road routes.
//
// A hosted service without its key is no service at all, so it is left out
// rather than built: every road leg then becomes a straight-line estimate, which
// the interface says plainly. A service of the operator's own needs no key.
//
// Arguments:
//   - cfg: the loaded configuration.
//   - logger: where a disabled router is reported.
//
// Returns:
//   - the provider, or nil when routes cannot be calculated.
func roadRouter(cfg *config.Config, logger *slog.Logger) routing.Provider {
	if cfg.Routing.NeedsKey() && cfg.Routing.APIKey == "" {
		logger.Warn("route calculation is disabled",
			slog.String("provider", cfg.Routing.Provider),
			slog.String("effect", "road legs are straight-line estimates"),
			slog.String("hint", "set TRIPVAULT_ROUTING_API_KEY"))
		return nil
	}

	logger.Info("routes are calculated by a provider",
		slog.String("provider", cfg.Routing.Provider),
		slog.String("address", cfg.Routing.Address()))

	switch cfg.Routing.Provider {
	case config.RouterOSRM:
		return routing.NewOSRM(cfg.Routing.Address())
	case config.RouterValhalla:
		return routing.NewValhalla(cfg.Routing.Address())
	default:
		return routing.NewORS(cfg.Routing.Address(), cfg.Routing.APIKey)
	}
}

// placeFinder - builds the service that searches for places.
//
// Without it a point is still placed by coordinates, a map link or a click on
// the map, so a missing key disables searching rather than the map.
//
// Arguments:
//   - cfg: the loaded configuration.
//   - logger: where a disabled geocoder is reported.
//
// Returns:
//   - the provider, or nil when places cannot be searched for.
func placeFinder(cfg *config.Config, logger *slog.Logger) geocoding.Provider {
	if cfg.Geocoding.NeedsKey() && cfg.Geocoding.APIKey == "" {
		logger.Warn("place search is disabled",
			slog.String("provider", cfg.Geocoding.Provider),
			slog.String("effect", "points are placed by coordinates, links or the map"),
			slog.String("hint", "set TRIPVAULT_GEOCODING_API_KEY"))
		return nil
	}

	logger.Info("places are searched through a provider",
		slog.String("provider", cfg.Geocoding.Provider),
		slog.String("address", cfg.Geocoding.Address()))

	switch cfg.Geocoding.Provider {
	case config.GeocoderPhoton:
		return geocoding.NewPhoton(cfg.Geocoding.Address())
	case config.GeocoderNominatim:
		// Nominatim's usage policy requires a request to say what it comes
		// from, and the version tells one instance's traffic from another's.
		return geocoding.NewNominatim(cfg.Geocoding.Address(),
			telemetry.ServiceName+"/"+telemetry.Build().Version)
	default:
		return geocoding.NewPelias(cfg.Geocoding.Address(), cfg.Geocoding.APIKey)
	}
}

// instanceSecrets - opens the key that seals the credentials a backup stores.
//
// A key set in the environment is used as it is. With none, one is made in the
// key file and read back on every later start, so that setting a backup up needs
// no preparation by the operator; if that file cannot be written the service
// starts without a sealer, and the interface says credentials cannot be stored
// rather than storing them in the clear.
//
// Arguments:
//   - cfg: the loaded configuration.
//   - logger: where a key that had to be made, or could not be, is reported.
//
// Returns:
//   - the box that seals and opens credentials, or nil when there is no key.
//   - an error only for a key that is configured and malformed, which is a
//     start-up failure: discovering it at the first backup means a backup that
//     fails at three in the morning.
func instanceSecrets(cfg *config.Config, logger *slog.Logger) (*secrets.Box, error) {
	key := cfg.Secrets.Key
	if key == "" {
		made, created, err := secrets.KeyFromFile(cfg.Secrets.KeyFile)
		if err != nil {
			logger.Error("stored backup credentials are unavailable",
				slog.String("path", cfg.Secrets.KeyFile), slog.Any("error", err))
			return nil, nil
		}
		if created {
			logger.Info("made a secrets key", slog.String("path", cfg.Secrets.KeyFile))
		}
		key = made
	}

	box, err := secrets.NewBox(key)
	if err != nil {
		return nil, fmt.Errorf("TRIPVAULT_SECRETS_KEY: %w", err)
	}
	return box, nil
}

// backupStore hands the API the repository only when there is a runner to use
// it, so that the endpoints report backups as unavailable as one rather than
// half working.
func backupStore(repository *postgres.BackupRepository, runner *backup.Runner) httpapi.BackupStore {
	if runner == nil {
		return nil
	}
	return repository
}

// sealerOrNil turns a missing box into a nil interface, which is what the API
// checks to decide whether a credential can be stored at all.
func sealerOrNil(box *secrets.Box) httpapi.SecretSealer {
	if box == nil {
		return nil
	}
	return box
}

// backupServiceOf builds the service the API starts backups through, or nil when
// this instance has none.
func backupServiceOf(runner *backup.Runner, destinations *backup.Resolver, local backup.Destination,
	work context.Context) httpapi.BackupService {
	if runner == nil {
		return nil
	}
	return backupService{runner: runner, destinations: destinations, local: local, work: work}
}

// backupService adapts the runner and the destinations to what the API needs.
type backupService struct {
	runner       *backup.Runner
	destinations *backup.Resolver
	// local is this server's own backup directory, which a restore can read
	// from without any configuration naming it: a fresh instance has none.
	local backup.Destination
	// work is the service's own lifetime. A backup started on request outlives
	// the request that asked for it, and stops only with the service.
	work context.Context
}

// Start begins a backup immediately, on request rather than on a schedule, and
// returns while it is still running.
func (s backupService) Start(ctx context.Context, config domain.BackupConfig) (domain.BackupRun, error) {
	return s.runner.Start(ctx, s.work, config)
}

// Progress reports how far a backup in flight has got.
func (s backupService) Progress(runID uuid.UUID) (backup.Progress, bool) {
	return s.runner.Progress(runID)
}

// HoldForRestore keeps backups away while the service is being restored.
func (s backupService) HoldForRestore() (func(), error) {
	return s.runner.HoldForRestore()
}

// Archives lists what a destination holds, newest first; a nil configuration
// is this server's own backup directory.
func (s backupService) Archives(ctx context.Context, config *domain.BackupConfig) ([]backup.Artifact, error) {
	if config == nil {
		return s.local.List(ctx)
	}
	destination, closeDestination, err := s.destinations.Resolve(ctx, *config)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeDestination() }()
	return destination.List(ctx)
}

// Open reads a stored archive back from wherever its configuration put it, or
// from this server's own backup directory when there is no configuration.
//
// The connection is closed once the archive has been read, which is why the
// reader carries the release with it: an SFTP handle outlives its client by
// nothing at all.
func (s backupService) Open(ctx context.Context, config *domain.BackupConfig, name string) (
	io.ReadCloser, error) {
	if config == nil {
		return s.local.Open(ctx, name)
	}
	destination, closeDestination, err := s.destinations.Resolve(ctx, *config)
	if err != nil {
		return nil, err
	}

	archive, err := destination.Open(ctx, name)
	if err != nil {
		_ = closeDestination()
		return nil, err
	}
	return closingReader{ReadCloser: archive, release: closeDestination}, nil
}

// closingReader releases the destination once the archive has been read.
type closingReader struct {
	io.ReadCloser
	release func() error
}

// Close closes the archive and then the destination that served it.
func (c closingReader) Close() error {
	err := c.ReadCloser.Close()
	if releaseErr := c.release(); err == nil {
		err = releaseErr
	}
	return err
}
