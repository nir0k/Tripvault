package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/auth"
	"github.com/nir0k/tripvault/backend/internal/backup"
	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/httpapi/docs"
	"github.com/nir0k/tripvault/backend/internal/media"
	"github.com/nir0k/tripvault/backend/internal/routing"
	"github.com/nir0k/tripvault/backend/internal/staticmap"
	"github.com/nir0k/tripvault/backend/internal/telemetry"
)

// Options configures the HTTP server.
type Options struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	// MapTileURL and MapAttribution are handed to clients for their maps.
	MapTileURL     string
	MapAttribution string
}

// AuthService is the authentication behaviour the handlers depend on.
type AuthService interface {
	Login(ctx context.Context, email, password, userAgent string) (auth.Session, error)
	Refresh(ctx context.Context, refreshToken string) (auth.Session, error)
	Logout(ctx context.Context, refreshToken string) error
	Authenticate(ctx context.Context, token string) (domain.User, uuid.UUID, error)
	ChangePassword(ctx context.Context, user domain.User, sessionID uuid.UUID, current, next string) error
}

// UserStore is the account persistence the profile and administration
// handlers depend on.
type UserStore interface {
	Create(ctx context.Context, user domain.User) (domain.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
	Update(ctx context.Context, id uuid.UUID, changes domain.UserChanges) (domain.User, error)
	UpdateProfile(ctx context.Context, id uuid.UUID, profile domain.Profile) (domain.User, error)
	SetAvatar(ctx context.Context, id uuid.UUID, key string, at time.Time) (domain.User, string, error)
	Delete(ctx context.Context, id uuid.UUID) ([]string, error)
	ResetPassword(ctx context.Context, id uuid.UUID, hash string) error
	Stats(ctx context.Context) (domain.UserStats, error)
	Search(ctx context.Context, query string, exclude uuid.UUID) ([]domain.TripUser, error)
}

// TripStore is the trip persistence the trip, member and administration
// handlers depend on.
type TripStore interface {
	Create(ctx context.Context, trip domain.Trip) (domain.TripSummary, error)
	Get(ctx context.Context, tripID, userID uuid.UUID) (domain.TripSummary, error)
	List(ctx context.Context, userID uuid.UUID, filter domain.TripFilter, now time.Time) (domain.TripPage, error)
	Years(ctx context.Context, userID uuid.UUID, kind domain.DocumentKind) ([]int, error)
	Update(ctx context.Context, trip domain.Trip, confirm bool) error
	Delete(ctx context.Context, tripID uuid.UUID) ([]string, error)
	Members(ctx context.Context, tripID uuid.UUID) ([]domain.TripMember, error)
	AddMember(ctx context.Context, tripID, userID uuid.UUID, role domain.TripRole) (domain.TripMember, error)
	UpdateMember(ctx context.Context, tripID, userID uuid.UUID, role domain.TripRole) (domain.TripMember, error)
	RemoveMember(ctx context.Context, tripID, userID uuid.UUID) error
	ShareLinks(ctx context.Context, tripID uuid.UUID) ([]domain.ShareLink, error)
	CreateShareLink(ctx context.Context, link domain.ShareLink, tokenHash []byte) error
	ShareLinkTrip(ctx context.Context, linkID uuid.UUID) (uuid.UUID, error)
	RevokeShareLink(ctx context.Context, tripID, linkID uuid.UUID, at time.Time) error
	ShareAccess(ctx context.Context, tokenHash []byte, at time.Time) (domain.ShareAccess, error)
	RecordShareVisit(ctx context.Context, linkID uuid.UUID, at time.Time) error
}

// DocumentStore is the persistence of plans and reports: days, places and stays.
type DocumentStore interface {
	Document(ctx context.Context, id uuid.UUID) (domain.Document, error)
	Day(ctx context.Context, id uuid.UUID) (domain.Day, error)
	Item(ctx context.Context, id uuid.UUID) (domain.Item, error)
	Stay(ctx context.Context, id uuid.UUID) (domain.Stay, error)
	Content(ctx context.Context, id uuid.UUID) (domain.DocumentContent, error)
	CreateReport(ctx context.Context, report domain.Trip, planID uuid.UUID) error
	UpdateDocument(ctx context.Context, id uuid.UUID, intro, summary string) error
	AddDay(ctx context.Context, day domain.Day, position *int) error
	DuplicateDay(ctx context.Context, sourceID uuid.UUID) (uuid.UUID, error)
	UpdateDay(ctx context.Context, day domain.Day) error
	DeleteDay(ctx context.Context, dayID uuid.UUID, confirm bool) error
	ReorderDays(ctx context.Context, documentID uuid.UUID, order []uuid.UUID) error
	CreatePlace(ctx context.Context, place domain.Item, position *int) error
	UpdatePlace(ctx context.Context, place domain.Item) error
	DeletePlace(ctx context.Context, place domain.Item) error
	MovePlace(ctx context.Context, documentID, placeID uuid.UUID, dayID *uuid.UUID, position int) error
	CopyPlace(ctx context.Context, source domain.Item, copyID uuid.UUID, dayID *uuid.UUID, position *int) error
	CreateStay(ctx context.Context, stay domain.Stay) error
	UpdateStay(ctx context.Context, stay domain.Stay) error
	DeleteStay(ctx context.Context, stay domain.Stay) error
	Transfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error)
	CreateTransfer(ctx context.Context, transfer domain.Transfer) error
	UpdateTransfer(ctx context.Context, transfer domain.Transfer) error
	DeleteTransfer(ctx context.Context, id uuid.UUID) error
	Expense(ctx context.Context, id uuid.UUID) (domain.Expense, error)
	CreateExpense(ctx context.Context, expense domain.Expense) error
	UpdateExpense(ctx context.Context, expense domain.Expense) error
	DeleteExpense(ctx context.Context, id uuid.UUID) error
	SaveTrack(ctx context.Context, track domain.Track, file []byte) (domain.Track, error)
	Track(ctx context.Context, id uuid.UUID) (domain.Track, error)
	TrackFile(ctx context.Context, id uuid.UUID) (domain.TrackFile, error)
	DeleteTrack(ctx context.Context, itemID uuid.UUID) error
	Leg(ctx context.Context, id uuid.UUID) (domain.Leg, error)
	UpdateLeg(ctx context.Context, leg domain.Leg) error
	SaveLegCalculation(ctx context.Context, legID uuid.UUID, input string, calculation domain.LegCalculation, at time.Time) error
	SaveTranslations(ctx context.Context, documentID uuid.UUID, language string, translations []domain.Translation) error
}

// Router calculates legs.
type Router interface {
	Enabled() bool
	ProviderName() string
	Calculate(ctx context.Context, mode domain.TravelMode, from, to *domain.Point) domain.LegCalculation
	Forget(ctx context.Context, mode domain.TravelMode, from, to domain.Point) error
}

// RoutingStats reports a provider's usage and its cache.
type RoutingStats interface {
	Stats(ctx context.Context, now time.Time) (routing.UsageStats, error)
}

// MediaStore is the persistence of a trip's files: their catalogue and where
// each one is shown.
type MediaStore interface {
	Create(ctx context.Context, item domain.Media, quota int64) error
	Get(ctx context.Context, id uuid.UUID) (domain.Media, error)
	ListByTrip(ctx context.Context, tripID uuid.UUID) ([]domain.Media, error)
	ListAll(ctx context.Context) ([]domain.Media, error)
	Update(ctx context.Context, id uuid.UUID, changes domain.MediaChanges) (domain.Media, error)
	Delete(ctx context.Context, id uuid.UUID) (string, error)
	UsedBytes(ctx context.Context, tripID uuid.UUID) (int64, error)
	SetLinks(ctx context.Context, target domain.MediaTarget, targetID uuid.UUID, mediaIDs []uuid.UUID) error
	SetFavorites(ctx context.Context, target domain.MediaTarget, targetID uuid.UUID, mediaIDs []uuid.UUID) error
	SetFavorite(ctx context.Context, mediaIDs []uuid.UUID, favorite bool) error
	LinksOfTrip(ctx context.Context, tripID uuid.UUID) ([]domain.MediaLink, error)
	TripOfTarget(ctx context.Context, target domain.MediaTarget, targetID uuid.UUID) (uuid.UUID, error)
}

// SessionStore is the session persistence the session list depends on.
type SessionStore interface {
	ListActive(ctx context.Context, userID uuid.UUID, now time.Time) ([]domain.Session, error)
	Revoke(ctx context.Context, userID, id uuid.UUID, at time.Time) error
}

// Probe reports on a dependency for the readiness and status endpoints.
type Probe interface {
	Ping(ctx context.Context) error
	SchemaVersion(ctx context.Context) (int64, error)
}

// Dependencies bundles what the handlers read and write through, so adding one
// does not change the signature of NewServer.
type Dependencies struct {
	Auth      AuthService
	Users     UserStore
	Sessions  SessionStore
	Trips     TripStore
	Documents DocumentStore
	// Media is the catalogue of the files; MediaFiles is where their bytes live.
	Media      MediaStore
	MediaFiles media.Store
	// MediaMaxBytes bounds one uploaded file, MediaTripQuota everything a trip
	// keeps; a quota of zero is unlimited.
	MediaMaxBytes  int64
	MediaTripQuota int64
	// TrackMaxBytes bounds an imported GPX or KML file.
	TrackMaxBytes int64
	Routing       Router
	RoutingStats  RoutingStats
	Geocoder      Geocoder
	GeocodeStats  RoutingStats
	Expander      LinkExpander
	// GeocodeDailyLimit is shown next to the day's geocoding usage.
	GeocodeDailyLimit int
	// RoutingDailyLimit is shown next to the day's usage on the status screen.
	RoutingDailyLimit int
	// RoutingProvider and GeocodingProvider name the services this instance is
	// configured to use. They are taken from the configuration rather than from
	// the clients, because a service that is switched off - a hosted one with no
	// key - builds no client to ask, and the status screen still has to say
	// which service it would be.
	RoutingProvider   string
	GeocodingProvider string
	Database          Probe
	// MapTiles is where the backgrounds of a report's maps come from; nil
	// draws them without one.
	MapTiles staticmap.TileSource

	// Backups is where backup configurations and their history are kept, and
	// BackupService is what carries one out and reads an archive back. Both are
	// nil on an instance whose backup directory could not be opened, which
	// leaves the endpoints answering that backups are unavailable rather than
	// stopping the service.
	Backups       BackupStore
	BackupService BackupService
	// Sealer seals the credentials a backup configuration stores, and is nil
	// when the instance has no secrets key.
	Sealer SecretSealer
	// Migrator builds the replacement schema a restore loads the archive into,
	// and RestoreFiles the replacement media beside the live files.
	Migrator     backup.DatabaseStager
	RestoreFiles backup.FileStager
	// RestoreWorkDir is where an archive is unpacked and verified before
	// anything is replaced.
	RestoreWorkDir string
	// WorkContext ends background restores when the service shuts down. A nil
	// value uses a process-lifetime context, primarily for tests.
	WorkContext context.Context
}

// Server owns the HTTP listener, the route table and the handlers' dependencies.
type Server struct {
	opts           Options
	logger         *slog.Logger
	auth           AuthService
	users          UserStore
	sessions       SessionStore
	trips          TripStore
	documents      DocumentStore
	media          MediaStore
	mediaFiles     media.Store
	mediaMaxBytes  int64
	mediaTripQuota int64
	trackMaxBytes  int64
	// thumbnails bounds how many previews are rendered at once, because each
	// one holds a decoded picture in memory.
	thumbnails chan struct{}
	// previewWorkers is how many workers render the previews of queued uploads.
	previewWorkers int
	// uploads carries fresh uploads to the background workers that render
	// their previews; renders holds the renderings in flight, by storage key.
	uploads         *uploadQueue
	rendersMu       sync.Mutex
	renders         map[string]*previewRender
	previewsRunning atomic.Bool
	backfillWanted  atomic.Bool
	backfillRunning atomic.Bool
	progress        previewProgress
	routing         Router
	routingStats    RoutingStats
	routingDaily    int
	geocoder        Geocoder
	geocodeStats    RoutingStats
	geocodeDaily    int
	// routingProvider and geocodingProvider are the configured service names.
	routingProvider   string
	geocodingProvider string
	expander          LinkExpander
	database          Probe
	mapTiles          staticmap.TileSource

	backups        BackupStore
	backupService  BackupService
	sealer         SecretSealer
	migrator       backup.DatabaseStager
	restoreFiles   backup.FileStager
	restoreWorkDir string
	workContext    context.Context
	// restoreGate is held shared by every API request and exclusively for the
	// whole of a restore; serving is the context those requests are tied to,
	// cancelled by stopServing when a restore begins.
	restoreGate   sync.RWMutex
	servingMu     sync.Mutex
	serving       context.Context
	stopServing   context.CancelFunc
	restoreJobsMu sync.Mutex
	restoreActive bool
	restoreJobs   map[uuid.UUID]*restoreJob

	signIns *signInLimits
	docs    *docs.Handler
	http    *http.Server
	now     func() time.Time
}

// NewServer - builds the HTTP server with its full route table.
//
// Arguments:
//   - opts: listener address and timeouts.
//   - logger: logger shared by middleware and handlers.
//   - deps: the services and repositories the handlers use.
//
// Returns:
//   - a server ready to be started with Run.
func NewServer(opts Options, logger *slog.Logger, deps Dependencies) *Server {
	workContext := deps.WorkContext
	if workContext == nil {
		workContext = context.Background()
	}
	previewWorkers, renderings := renderWorkers(runtime.GOMAXPROCS(0))
	s := &Server{
		opts:           opts,
		logger:         logger,
		auth:           deps.Auth,
		users:          deps.Users,
		sessions:       deps.Sessions,
		trips:          deps.Trips,
		documents:      deps.Documents,
		media:          deps.Media,
		mediaFiles:     deps.MediaFiles,
		mediaMaxBytes:  deps.MediaMaxBytes,
		mediaTripQuota: deps.MediaTripQuota,
		trackMaxBytes:  deps.TrackMaxBytes,
		previewWorkers: previewWorkers,
		thumbnails:     make(chan struct{}, renderings),
		uploads:        newUploadQueue(),
		renders:        make(map[string]*previewRender),
		routing:        deps.Routing,
		routingStats:   deps.RoutingStats,
		routingDaily:   deps.RoutingDailyLimit,
		geocoder:       deps.Geocoder,
		geocodeStats:   deps.GeocodeStats,
		geocodeDaily:   deps.GeocodeDailyLimit,

		routingProvider:   deps.RoutingProvider,
		geocodingProvider: deps.GeocodingProvider,

		expander: deps.Expander,
		database: deps.Database,
		mapTiles: deps.MapTiles,

		backups:        deps.Backups,
		backupService:  deps.BackupService,
		sealer:         deps.Sealer,
		migrator:       deps.Migrator,
		restoreFiles:   deps.RestoreFiles,
		restoreWorkDir: deps.RestoreWorkDir,
		workContext:    workContext,
		restoreJobs:    make(map[uuid.UUID]*restoreJob),

		signIns: newSignInLimits(),
		now:     time.Now,
	}
	s.serving, s.stopServing = context.WithCancel(context.Background())

	// The reference is optional: failing to load it must not stop the API.
	reference, err := docs.NewHandler(telemetry.Build().Version)
	if err != nil {
		logger.Error("api reference is unavailable", slog.Any("error", err))
	} else {
		s.docs = reference
	}

	s.http = &http.Server{
		Addr:              opts.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: opts.ReadTimeout,
		ReadTimeout:       opts.ReadTimeout,
		WriteTimeout:      opts.WriteTimeout,
		IdleTimeout:       opts.IdleTimeout,
	}
	return s
}

// routes builds the router. Everything the clients use lives under /api/v1;
// probes and the API reference sit at the root.
func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(requestID)
	r.Use(s.recoverPanic)
	r.Use(requestLogger(s.logger))
	r.Use(middleware.CleanPath)
	r.Use(middleware.StripSlashes)

	if s.docs != nil {
		s.docs.Register(r.Get)
	}
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReadiness)
	r.Get("/version", s.handleVersion)

	// A restore status is addressed by an unguessable run ID and contains no
	// archive data. It stays reachable through cutover, when restored sessions
	// may make the initiating administrator's token cease to exist.
	r.With(apiHeaders).Get("/api/v1/restore-runs/{restoreID}", s.handleGetRestoreRun)

	r.Route("/api/v1", func(v1 chi.Router) {
		v1.Use(apiHeaders)
		v1.Use(s.requireAvailable)

		v1.Get("/config", s.handleClientConfig)
		v1.Post("/auth/login", s.handleLogin)
		v1.Post("/auth/refresh", s.handleRefresh)
		v1.Post("/auth/logout", s.handleLogout)

		// Reading by a share link needs no account: the token in X-Share-Token
		// is the whole credential, and it grants reading and nothing else.
		v1.Route("/shared", func(shared chi.Router) {
			shared.Use(s.requireShareToken)

			shared.Get("/", s.handleShared)
			shared.Get("/document", s.handleSharedDocument)
			shared.Get("/report/pdf", s.handleSharedReportPDF)
			shared.Get("/media/{mediaID}", s.handleSharedMediaFile)
			shared.Get("/media/{mediaID}/thumbnail", s.handleSharedMediaThumbnail)
			shared.Get("/tracks/{trackID}/file", s.handleSharedTrackFile)
		})

		v1.Group(func(private chi.Router) {
			private.Use(s.requireUser)

			// Reachable with a temporary password: reading who is signed in
			// and replacing that password are the only way out of it.
			private.Get("/me", s.handleGetMe)
			private.Post("/me/password", s.handleChangePassword)

			private.Group(func(member chi.Router) {
				member.Use(s.requirePasswordChanged)

				member.Patch("/me", s.handleUpdateMe)
				member.Delete("/me", s.handleDeleteMe)
				member.Put("/me/avatar", s.handleSetAvatar)
				member.Delete("/me/avatar", s.handleDeleteAvatar)
				member.Get("/users/{userID}/avatar", s.handleGetAvatar)
				member.Get("/me/sessions", s.handleListSessions)
				member.Delete("/me/sessions/{sessionID}", s.handleRevokeSession)

				member.Get("/users/search", s.handleSearchUsers)
				member.Get("/geo/search", s.handleGeoSearch)
				member.Get("/geo/reverse", s.handleGeoReverse)
				member.Get("/geo/parse-link", s.handleParseLink)

				member.Get("/trips", s.handleListTrips)
				member.Post("/trips", s.handleCreateTrip)
				member.Get("/trips/years", s.handleTripYears)
				member.Get("/trips/{tripID}", s.handleGetTrip)
				member.Patch("/trips/{tripID}", s.handleUpdateTrip)
				member.Delete("/trips/{tripID}", s.handleDeleteTrip)
				member.Get("/trips/{tripID}/members", s.handleListMembers)
				member.Post("/trips/{tripID}/members", s.handleAddMember)
				member.Patch("/trips/{tripID}/members/{userID}", s.handleUpdateMember)
				member.Delete("/trips/{tripID}/members/{userID}", s.handleRemoveMember)
				member.Get("/trips/{tripID}/share-links", s.handleListShareLinks)
				member.Post("/trips/{tripID}/share-links", s.handleCreateShareLink)
				member.Delete("/share-links/{linkID}", s.handleRevokeShareLink)
				member.Get("/trips/{tripID}/budget", s.handleGetBudget)
				member.Get("/trips/{tripID}/media", s.handleListMedia)
				member.Post("/trips/{tripID}/media", s.handleUploadMedia)
				member.Get("/media/{mediaID}", s.handleGetMediaFile)
				member.Get("/media/{mediaID}/thumbnail", s.handleGetMediaThumbnail)
				member.Patch("/media/{mediaID}", s.handleUpdateMedia)
				member.Delete("/media/{mediaID}", s.handleDeleteMedia)
				member.Put("/media-links", s.handleSetMediaLinks)
				member.Post("/media-favorites", s.handleSetMediaFavorites)
				member.Post("/trips/{tripID}/reports", s.handleCreateReport)
				member.Get("/trips/{tripID}/report/pdf", s.handleReportPDF)

				member.Get("/documents/{documentID}", s.handleGetDocument)
				member.Patch("/documents/{documentID}", s.handleUpdateDocument)
				member.Put("/documents/{documentID}/translations/{lang}", s.handleSaveTranslations)
				member.Post("/documents/{documentID}/days", s.handleCreateDay)
				member.Post("/documents/{documentID}/days:reorder", s.handleReorderDays)
				member.Post("/documents/{documentID}/items", s.handleCreateUnassignedPlace)
				member.Post("/documents/{documentID}/items:move", s.handleMovePlace)
				member.Post("/documents/{documentID}/stays", s.handleCreateStay)
				member.Post("/documents/{documentID}/transfers", s.handleCreateTransfer)
				member.Post("/documents/{documentID}/expenses", s.handleCreateExpense)
				member.Post("/documents/{documentID}/legs:calculate", s.handleCalculateLegs)
				member.Post("/documents/{documentID}/legs:retry", s.handleRetryEstimatedLegs)
				member.Patch("/days/{dayID}", s.handleUpdateDay)
				member.Delete("/days/{dayID}", s.handleDeleteDay)
				member.Post("/days/{dayID}:duplicate", s.handleDuplicateDay)
				member.Post("/days/{dayID}:recalculate", s.handleRecalculateDay)
				member.Post("/items/{itemID}/track", s.handleImportItemTrack)
				member.Delete("/items/{itemID}/track", s.handleDeleteItemTrack)
				member.Get("/tracks/{trackID}/file", s.handleGetTrackFile)
				member.Post("/days/{dayID}/items", s.handleCreateDayPlace)
				member.Patch("/items/{itemID}", s.handleUpdatePlace)
				member.Delete("/items/{itemID}", s.handleDeletePlace)
				member.Post("/items/{itemID}:copy", s.handleCopyPlace)
				member.Patch("/stays/{stayID}", s.handleUpdateStay)
				member.Delete("/stays/{stayID}", s.handleDeleteStay)
				member.Patch("/transfers/{transferID}", s.handleUpdateTransfer)
				member.Delete("/transfers/{transferID}", s.handleDeleteTransfer)
				member.Patch("/expenses/{expenseID}", s.handleUpdateExpense)
				member.Delete("/expenses/{expenseID}", s.handleDeleteExpense)
				member.Patch("/legs/{legID}", s.handleUpdateLeg)
				member.Post("/legs/{legID}:recalculate", s.handleRecalculateLeg)

				member.Group(func(admin chi.Router) {
					admin.Use(s.requireAdmin)

					admin.Get("/admin/users", s.handleListUsers)
					admin.Post("/admin/users", s.handleCreateUser)
					admin.Patch("/admin/users/{userID}", s.handleUpdateUser)
					admin.Post("/admin/users/{userID}/reset-password", s.handleResetPassword)
					admin.Get("/admin/status", s.handleStatus)
					admin.Get("/admin/previews", s.handlePreviewStatus)

					// Backups copy the whole service, so they are the
					// administrator's business alone. A build whose backup
					// directory could not be opened has no service behind
					// these, and the guard answers for them.
					admin.Group(func(backups chi.Router) {
						backups.Use(s.requireBackups)

						backups.Get("/admin/backup-configs", s.handleListBackupConfigs)
						backups.Post("/admin/backup-configs", s.handleCreateBackupConfig)
						backups.Put("/admin/backup-configs/{configID}", s.handleUpdateBackupConfig)
						backups.Delete("/admin/backup-configs/{configID}", s.handleDeleteBackupConfig)
						backups.Post("/admin/backup-configs/{configID}/run", s.handleRunBackup)

						backups.Get("/admin/backup-runs", s.handleListBackupRuns)
						backups.Get("/admin/backup-runs/{runID}", s.handleGetBackupRun)

						backups.Get("/admin/archives", s.handleListArchives)
						backups.Post("/admin/restore", s.handleRestore)
					})
				})
			})
		})

		v1.NotFound(s.handleNotFound)
		v1.MethodNotAllowed(s.handleMethodNotAllowed)
	})

	r.NotFound(s.handleNotFound)
	r.MethodNotAllowed(s.handleMethodNotAllowed)
	return r
}

// handleNotFound answers unknown routes with the standard error envelope.
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
}

// handleMethodNotAllowed answers unsupported methods with the standard envelope.
func (s *Server) handleMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
}

// Run - serves HTTP until the context is cancelled, then shuts down gracefully.
// It also starts rendering previews in the background, until the work context
// ends.
//
// Arguments:
//   - ctx: cancelling it starts a graceful shutdown bounded by ShutdownTimeout.
//
// Returns:
//   - nil after a clean shutdown, or an error if the listener failed or
//     in-flight requests did not finish in time.
func (s *Server) Run(ctx context.Context) error {
	s.startPreviewWork()
	serveErr := make(chan error, 1)
	go func() {
		s.logger.Info("http server listening", slog.String("addr", s.opts.Addr))
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("http listen: %w", err)
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	s.logger.Info("http server shutting down", slog.Duration("timeout", s.opts.ShutdownTimeout))
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.opts.ShutdownTimeout)
	defer cancel()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}
	return <-serveErr
}
