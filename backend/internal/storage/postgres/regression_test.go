package postgres_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/storage/migrations"
	"github.com/nir0k/tripvault/backend/internal/storage/postgres"
)

// database gives each regression an isolated schema in a disposable PostgreSQL.
// The test DSN is deliberately separate from the service's deployment settings.
func database(t *testing.T) (*pgxpool.Pool, uuid.UUID) {
	t.Helper()
	dsn := os.Getenv("TRIPVAULT_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("set TRIPVAULT_TEST_DB_DSN to run PostgreSQL regressions")
	}
	ctx := t.Context()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "regression_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.ConnConfig.RuntimeParams["application_name"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		admin.Close()
	})
	if err := migrations.Apply(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	owner := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, display_name, password_hash, is_admin)
	 VALUES ($1, 'owner@example.com', 'Owner', 'not used by repository tests', true)`, owner); err != nil {
		t.Fatal(err)
	}
	return pool, owner
}

// trip writes a dated trip through the actual repository rather than a fake.
func trip(t *testing.T, pool *pgxpool.Pool, owner uuid.UUID, kind domain.DocumentKind) domain.TripSummary {
	t.Helper()
	start, end := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	value, err := (domain.Trip{ID: uuid.New(), OwnerID: owner, Kind: kind, Title: "Regression trip",
		Currency: "EUR", Travelers: 1, StartDate: &start, EndDate: &end, Languages: nil, TrackSpeedKmh: 2.5}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	created, err := postgres.NewTripRepository(pool).Create(t.Context(), value)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

// TestResizeRequiresConfirmationForEveryDayContent exercises content ignored by
// the previous SQL check, including photographs and a night rather than visits.
func TestResizeRequiresConfirmationForEveryDayContent(t *testing.T) {
	for _, kind := range []string{"highlight", "photo", "night", "expense"} {
		t.Run(kind, func(t *testing.T) {
			pool, owner := database(t)
			value := trip(t, pool, owner, domain.DocumentReport)
			documents := postgres.NewDocumentRepository(pool)
			content, err := documents.Content(t.Context(), *value.ReportID)
			if err != nil {
				t.Fatal(err)
			}
			day := content.Days[1]
			switch kind {
			case "highlight":
				_, err = pool.Exec(t.Context(), `UPDATE days SET highlight = 'Remember this' WHERE id = $1`, day.ID)
			case "photo":
				id := uuid.New()
				_, err = pool.Exec(t.Context(), `INSERT INTO media (id,trip_id,storage_key,original_name,mime,size,checksum)
				 VALUES ($1,$2,'photo.jpg','photo.jpg','image/jpeg',10,$3)`, id, value.ID, make([]byte, 32))
				if err == nil {
					_, err = pool.Exec(t.Context(), `INSERT INTO media_links (media_id,day_id) VALUES ($1,$2)`, id, day.ID)
				}
			case "night":
				stayID := uuid.New()
				_, err = pool.Exec(t.Context(), `INSERT INTO stays(id,document_id,name,kind,check_in_date,check_out_date)
				 VALUES ($1,$2,'Hotel','hotel','2026-10-02','2026-10-03')`, stayID, *value.ReportID)
				if err == nil {
					_, err = pool.Exec(t.Context(), `INSERT INTO items(id,document_id,day_id,kind,anchor,stay_id,story_md)
				 VALUES($1,$2,$3,'stay_anchor','evening',$4,'The story of the night')`, uuid.New(), *value.ReportID, day.ID, stayID)
				}
			case "expense":
				_, err = pool.Exec(t.Context(), `INSERT INTO expenses(id,document_id,day_id,note) VALUES($1,$2,$3,'Dinner')`, uuid.New(), *value.ReportID, day.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			value.EndDate = value.StartDate
			err = postgres.NewTripRepository(pool).Update(t.Context(), value.Trip, false)
			var removed *domain.DaysWouldBeRemovedError
			if !errors.As(err, &removed) || len(removed.Days) != 1 {
				t.Fatalf("lost %s without confirmation: %v", kind, err)
			}
			content, err = documents.Content(t.Context(), *value.ReportID)
			if err != nil || len(content.Days) != 2 {
				t.Fatalf("refused resize changed the document: %v", err)
			}
		})
	}
}

// TestReportCopyAndDuplicateKeepTheirSemantics checks the persisted track pace,
// leg note, and visit statuses which handler fakes cannot verify.
func TestReportCopyAndDuplicateKeepTheirSemantics(t *testing.T) {
	pool, owner := database(t)
	plan := trip(t, pool, owner, domain.DocumentPlan)
	docs := postgres.NewDocumentRepository(pool)
	content, err := docs.Content(t.Context(), *plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	day := content.Days[0].ID
	lat, lng := 40.0, 20.0
	activity, err := (domain.Item{ID: uuid.New(), DocumentID: *plan.PlanID, DayID: &day, Kind: domain.ItemActivity,
		ActivityType: domain.ActivityHike, Name: "Trail", Lat: &lat, Lng: &lng}).NormalizePlace(domain.DocumentPlan)
	if err != nil {
		t.Fatal(err)
	}
	if err := docs.CreatePlace(t.Context(), activity, nil); err != nil {
		t.Fatal(err)
	}
	place, err := (domain.Item{ID: uuid.New(), DocumentID: *plan.PlanID, DayID: &day, Kind: domain.ItemPlace, Name: "Cafe", Lat: &lat, Lng: &lng}).NormalizePlace(domain.DocumentPlan)
	if err != nil {
		t.Fatal(err)
	}
	if err := docs.CreatePlace(t.Context(), place, nil); err != nil {
		t.Fatal(err)
	}
	line := domain.EncodePolyline([]domain.Point{{Lat: 40, Lng: 20}, {Lat: 40.01, Lng: 20.01}})
	_, err = docs.SaveTrack(t.Context(), domain.Track{ID: uuid.New(), DocumentID: *plan.PlanID, ItemID: activity.ID,
		OriginalName: "route.gpx", Format: "gpx", Geometry: line, DistanceM: 1000, PointCount: 2}, []byte("original track"), 0)
	if err != nil {
		t.Fatal(err)
	}
	speed := 3.0
	if err := docs.SetTrackSpeed(t.Context(), activity.ID, &speed); err != nil {
		t.Fatal(err)
	}
	content, err = docs.Content(t.Context(), *plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if err := docs.UpdateLegNote(t.Context(), content.Legs[0].ID, "Keep this note"); err != nil {
		t.Fatal(err)
	}
	report := plan.Trip
	report.ID = uuid.New()
	report.Kind = domain.DocumentReport
	report.SourceTripID = &plan.ID
	report.Languages = []string{"en"}
	if err := docs.CreateReport(t.Context(), report, *plan.PlanID, 0); err != nil {
		t.Fatal(err)
	}
	created, err := postgres.NewTripRepository(pool).Get(t.Context(), report.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	content, err = docs.Content(t.Context(), *created.ReportID)
	if err != nil {
		t.Fatal(err)
	}
	if created.TrackSpeedKmh != 2.5 || content.Tracks[0].SpeedKmh == nil || *content.Tracks[0].SpeedKmh != 3 || content.Legs[0].Note != "Keep this note" {
		t.Fatalf("copy lost pace or notes: %+v %+v", created, content)
	}
	copyID, err := docs.DuplicateDay(t.Context(), content.Days[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	content, err = docs.Content(t.Context(), *created.ReportID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range content.Items {
		if item.DayID != nil && *item.DayID == copyID && item.Kind.IsVisit() && item.Status != domain.StatusVisited {
			t.Fatalf("duplicated report item has status %q", item.Status)
		}
	}
}

// TestConcurrentAdministratorDeletionKeepsOne checks the invariant against real
// transactions. A trigger delays deletion until both requests wait on a lock,
// so the previous unlocked count reliably allowed both deletions.
func TestConcurrentAdministratorDeletionKeepsOne(t *testing.T) {
	pool, owner := database(t)
	other := uuid.New()
	if _, err := pool.Exec(t.Context(), `INSERT INTO users(id,email,display_name,password_hash,is_admin)
	 VALUES($1,'other@example.com','Other','unused',true)`, other); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `CREATE FUNCTION delay_delete() RETURNS trigger LANGUAGE plpgsql AS $$
	 BEGIN PERFORM pg_advisory_xact_lock(684293018); RETURN OLD; END $$;
	 CREATE TRIGGER delay_delete BEFORE DELETE ON users FOR EACH ROW EXECUTE FUNCTION delay_delete()`); err != nil {
		t.Fatal(err)
	}
	gate, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gate.Rollback(context.Background()) }()
	if _, err := gate.Exec(t.Context(), `SELECT pg_advisory_xact_lock(684293018)`); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	users := postgres.NewUserRepository(pool)
	for _, id := range []uuid.UUID{owner, other} {
		go func() { _, err := users.Delete(t.Context(), id); results <- err }()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND wait_event_type='Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("deletions did not reach the transaction gate")
		}
	}
	if err := gate.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	first, second := <-results, <-results
	if first != nil {
		first, second = second, first
	}
	if first != nil || !errors.Is(second, domain.ErrLastAdmin) {
		t.Fatalf("deletion results: %v, %v", first, second)
	}
}

// TestCleanupMigrationRoundTrip checks rollback restores the preceding schema
// and reapplying cleanup retains the actual trip and document data.
func TestCleanupMigrationRoundTrip(t *testing.T) {
	pool, owner := database(t)
	value := trip(t, pool, owner, domain.DocumentPlan)
	body, err := os.ReadFile("../migrations/sql/00012_remove_unused_content_state.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(body), "-- +goose Down")
	if !ok {
		t.Fatal("migration has no rollback")
	}
	if _, err := pool.Exec(t.Context(), down); err != nil {
		t.Fatal(err)
	}
	var column string
	if err := pool.QueryRow(t.Context(), `SELECT column_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='media' AND column_name='status'`).Scan(&column); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), up); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewTripRepository(pool).Get(t.Context(), value.ID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.NewDocumentRepository(pool).Content(t.Context(), *value.PlanID); err != nil {
		t.Fatal(err)
	}
}
