package backup

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// ScheduleStore supplies the configurations that run on a schedule, and when
// each last ran.
type ScheduleStore interface {
	// DueConfigs returns every enabled configuration that has a schedule, with
	// the instant it last started. A configuration that has never run has the
	// zero time.
	DueConfigs(ctx context.Context) ([]ScheduledConfig, error)
}

// ScheduledConfig is one configuration together with what the scheduler needs to
// decide whether it is due.
type ScheduledConfig struct {
	Config domain.BackupConfig
	// LastStartedAt is when a run of this configuration last began, or the zero
	// time when none ever has.
	LastStartedAt time.Time
}

// parser accepts the five-field expressions everybody writes in a crontab, plus
// the named shorthands. Seconds are deliberately not accepted: a backup that
// runs on a particular second is not a thing anybody wants, and allowing the
// field would silently reinterpret every ordinary expression by one place.
var parser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ParseSchedule - checks a cron expression and returns it ready to use.
//
// Arguments:
//   - expression: a five-field cron expression, or a descriptor such as @daily.
//
// Returns:
//   - the parsed schedule.
//   - a *domain.ValidationError when the expression cannot be read, so that a
//     configuration is refused when it is saved rather than failing silently
//     every night afterwards.
func ParseSchedule(expression string) (cron.Schedule, error) {
	trimmed := strings.TrimSpace(expression)
	if trimmed == "" {
		return nil, domain.NewValidationError("schedule_cron", "required", "there is no schedule to parse")
	}

	schedule, err := parser.Parse(trimmed)
	if err != nil {
		return nil, domain.NewValidationError("schedule_cron", "invalid_schedule",
			"is not a schedule: "+err.Error())
	}
	return schedule, nil
}

// Scheduler runs the configurations that are due.
//
// It holds no registry of its own. On every tick it reads the configurations
// from the database and works out which are due from when each last ran, so a
// configuration added, changed or disabled through the API takes effect on the
// next tick with nothing to reload, and a restart does not lose track of what
// has already been done today.
type Scheduler struct {
	store    ScheduleStore
	runner   *Runner
	logger   *slog.Logger
	interval time.Duration
}

// defaultTick is how often the scheduler looks for work when the caller names no
// interval. A minute is the resolution a cron expression has, so checking more
// often could not find anything new.
const defaultTick = time.Minute

// NewScheduler - builds the scheduler.
//
// Arguments:
//   - store: where the configurations are read from.
//   - runner: what carries a due configuration out.
//   - logger: where scheduling decisions and failures are reported.
//   - interval: how often to look for due configurations.
//
// Returns:
//   - a scheduler ready to be started with Run.
func NewScheduler(store ScheduleStore, runner *Runner, logger *slog.Logger,
	interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = defaultTick
	}
	return &Scheduler{store: store, runner: runner, logger: logger, interval: interval}
}

// Run - looks for due configurations on every tick until the context is cancelled.
//
// Arguments:
//   - ctx: cancelling it stops the loop after the pass in flight.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pass(ctx, time.Now())
		}
	}
}

// pass runs everything that has come due.
//
// Configurations are run one after another rather than together: they read the
// same database and write to the same disk, and several instances archiving at
// once would make the service unusable for whoever is still editing a trip.
func (s *Scheduler) pass(ctx context.Context, now time.Time) {
	configs, err := s.store.DueConfigs(ctx)
	if err != nil {
		s.logger.Error("scheduled backups could not be read", slog.Any("error", err))
		return
	}

	for _, scheduled := range configs {
		if ctx.Err() != nil {
			return
		}
		if !s.isDue(scheduled, now) {
			continue
		}
		// A failure is already recorded against the run and logged by the
		// runner; the scheduler's job is only to keep going to the next one. A
		// configuration somebody started by hand is still running, and is left
		// to finish rather than started a second time. During a restore every
		// run is refused unrecorded, and the next pass finds it still due.
		if _, err := s.runner.Run(ctx, scheduled.Config); err != nil {
			continue
		}
	}
}

// isDue decides whether a configuration should run now.
//
// The next due time is computed from when the configuration last started rather
// than from the previous tick, which is what makes a restart harmless: a service
// that was down over the scheduled hour runs the backup when it comes back
// instead of skipping the day.
//
// A configuration that has never run is due immediately for the same reason:
// whoever sets one up expects a copy to exist, not to wait until tomorrow
// morning to find out whether it works.
func (s *Scheduler) isDue(scheduled ScheduledConfig, now time.Time) bool {
	schedule, err := ParseSchedule(scheduled.Config.ScheduleCron)
	if err != nil {
		s.logger.Warn("a backup schedule could not be read and was skipped",
			slog.String("backup_config_id", scheduled.Config.ID.String()),
			slog.String("schedule", scheduled.Config.ScheduleCron),
			slog.Any("error", err))
		return false
	}

	if scheduled.LastStartedAt.IsZero() {
		return true
	}
	return !schedule.Next(scheduled.LastStartedAt).After(now)
}
