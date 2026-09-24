package domain

import (
	"sort"
	"time"

	"github.com/google/uuid"
)

// A backup is a copy of the whole service: every table and every uploaded file,
// in one archive. It belongs to the instance rather than to a trip, because
// where the copies go, how often and how long they are kept are decisions about
// the server, and the archive a run produces holds every trip on it.

// The destinations an archive can be written to.
//
// Plain FTP and FTPS are absent by decision rather than by omission: FTP sends
// the password in the clear, and FTPS adds certificate handling for nothing SFTP
// does not already give - any machine that can run an FTP server can run SFTP.
const (
	// BackupDestinationLocal writes to a directory of the server itself.
	BackupDestinationLocal = "local"
	// BackupDestinationSFTP writes to another machine over SSH.
	BackupDestinationSFTP = "sftp"
)

// The states a run passes through. A run is recorded as running before the work
// starts, so one interrupted by a restart shows as a run that never finished
// rather than not showing at all.
const (
	BackupRunning = "running"
	BackupSuccess = "success"
	BackupFailed  = "failed"
)

// Bounds on what a configuration keeps. A count of zero means "not by count":
// retention then goes by age, or, with neither set, keeps everything. The upper
// bounds exist so that a typo cannot ask a disk to hold a decade of daily copies
// by accident.
const (
	MaxBackupRetainCount = 365
	MaxBackupRetainDays  = 3650
)

// BackupSecretPassphrase names the credential that locks the archives an
// encrypted configuration writes. It is named here because more than one package
// has to agree on it: the one that stores it and the one that opens it to use it.
const BackupSecretPassphrase = "passphrase"

// maxBackupScheduleLength bounds a cron expression. The expression itself is
// checked where the parser lives; this only keeps an absurd string out of the
// database before it gets there.
const maxBackupScheduleLength = 200

// BackupConfig is one standing instruction to copy the server somewhere.
type BackupConfig struct {
	ID uuid.UUID

	DestinationType string
	// DestinationParams is whatever the destination needs: nothing for the local
	// disk, a host and a directory for SFTP. No credential is ever kept here.
	DestinationParams map[string]string
	// SealedSecrets holds the credentials typed into the interface - an SFTP
	// password or private key, and the passphrase archives are locked with -
	// each sealed on its own with the instance's secrets key. The names are plain
	// so a listing can say which are set; the values open only with that key.
	SealedSecrets map[string][]byte

	// Encrypted says whether archives are locked with age. The passphrase is one
	// of the sealed secrets above rather than a column of its own.
	Encrypted bool

	// ScheduleCron is a five-field cron expression or a descriptor such as
	// @daily. Empty means the configuration runs only when somebody asks for it.
	ScheduleCron string
	// RetainCount is how many of the newest archives to keep, and RetainDays how
	// many days an archive is kept. At most one of them is set; both zero means
	// every archive is kept.
	RetainCount int
	RetainDays  int
	Enabled     bool

	CreatedAt time.Time
	UpdatedAt time.Time
	// DeletedAt marks an instruction that was withdrawn. The row stays: taking
	// the instruction back is not a request to destroy the copies it already
	// made, and those archives are read back through it.
	DeletedAt *time.Time
}

// backupDestinationTypes is the set the constants declare, for validation.
var backupDestinationTypes = map[string]bool{
	BackupDestinationLocal: true,
	BackupDestinationSFTP:  true,
}

// Normalize - trims a configuration's schedule and checks the rules that hold
// whatever the destination is.
//
// The destination's own parameters are checked by the package that talks to it:
// what an SFTP server needs is not something the domain can know.
//
// Returns:
//   - the normalised configuration.
//   - the first *ValidationError found.
func (c BackupConfig) Normalize() (BackupConfig, error) {
	var err error
	if c.ScheduleCron, err = trimmedText("schedule_cron", c.ScheduleCron, maxBackupScheduleLength); err != nil {
		return c, err
	}
	if !backupDestinationTypes[c.DestinationType] {
		return c, NewValidationError("destination_type", "unsupported", "must be local or sftp")
	}
	if c.RetainCount < 0 || c.RetainCount > MaxBackupRetainCount {
		return c, NewValidationError("retain_count", "out_of_range", "must be between 0 and 365")
	}
	if c.RetainDays < 0 || c.RetainDays > MaxBackupRetainDays {
		return c, NewValidationError("retain_days", "out_of_range", "must be between 0 and 3650")
	}
	if c.RetainCount > 0 && c.RetainDays > 0 {
		return c, NewValidationError("retain_days", "conflict", "archives are kept either by count or by age, not both")
	}
	if c.Encrypted && len(c.SealedSecrets[BackupSecretPassphrase]) == 0 {
		return c, NewValidationError("passphrase", "required", "an encrypted backup needs a passphrase")
	}
	return c, nil
}

// SecretNames - lists the credentials the configuration holds, sorted the way
// the constants are not: the caller wants a stable order for an API answer.
//
// The values never leave the database; only the names do, so that the interface
// can show which credentials are set without being able to read them.
//
// Returns:
//   - the names of the sealed credentials, in ascending order.
func (c BackupConfig) SecretNames() []string {
	names := make([]string, 0, len(c.SealedSecrets))
	for name, sealed := range c.SealedSecrets {
		if len(sealed) > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// BackupRun is one attempt to carry out a configuration.
type BackupRun struct {
	ID       uuid.UUID
	ConfigID uuid.UUID

	StartedAt  time.Time
	FinishedAt *time.Time
	Status     string

	SizeBytes *int64
	// Artifact is the name of what the run wrote, so a listing can offer it back.
	Artifact string
	// RotatedAt is when retention removed that archive from its destination. The
	// name stays: the history says what was copied and when, and an entry whose
	// name had been cleared would say nothing at all.
	RotatedAt    *time.Time
	ErrorMessage string
}

// ArchiveHeld - reports whether the run's archive is still at its destination.
//
// Returns:
//   - true when the run produced an archive that retention has not removed.
func (r BackupRun) ArchiveHeld() bool {
	return r.Status == BackupSuccess && r.Artifact != "" && r.RotatedAt == nil
}
