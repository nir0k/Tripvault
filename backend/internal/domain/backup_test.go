package domain

import (
	"errors"
	"testing"
	"time"
)

// TestBackupConfigNormalize checks the rules a configuration must meet before it
// can be stored.
func TestBackupConfigNormalize(t *testing.T) {
	passphrase := map[string][]byte{BackupSecretPassphrase: []byte("sealed")}

	cases := map[string]struct {
		config BackupConfig
		field  string
	}{
		"local disk": {BackupConfig{DestinationType: BackupDestinationLocal}, ""},
		"encrypted with a passphrase": {
			BackupConfig{DestinationType: BackupDestinationSFTP, Encrypted: true, SealedSecrets: passphrase}, "",
		},
		"kept by count": {BackupConfig{DestinationType: BackupDestinationLocal, RetainCount: 7}, ""},
		"kept by age":   {BackupConfig{DestinationType: BackupDestinationLocal, RetainDays: 30}, ""},

		"unknown destination":   {BackupConfig{DestinationType: "dropbox"}, "destination_type"},
		"no destination":        {BackupConfig{}, "destination_type"},
		"negative count":        {BackupConfig{DestinationType: BackupDestinationLocal, RetainCount: -1}, "retain_count"},
		"count beyond the cap":  {BackupConfig{DestinationType: BackupDestinationLocal, RetainCount: MaxBackupRetainCount + 1}, "retain_count"},
		"age beyond the cap":    {BackupConfig{DestinationType: BackupDestinationLocal, RetainDays: MaxBackupRetainDays + 1}, "retain_days"},
		"both kinds of keeping": {BackupConfig{DestinationType: BackupDestinationLocal, RetainCount: 7, RetainDays: 30}, "retain_days"},
		"encrypted without a passphrase": {
			BackupConfig{DestinationType: BackupDestinationLocal, Encrypted: true}, "passphrase",
		},
	}

	for name, test := range cases {
		_, err := test.config.Normalize()
		if test.field == "" {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		var invalid *ValidationError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: error = %v, want a validation error on %s", name, err, test.field)
			continue
		}
		if invalid.Field != test.field {
			t.Errorf("%s: field = %s, want %s", name, invalid.Field, test.field)
		}
	}
}

// TestBackupConfigNormalizeTrimsSchedule checks the schedule arrives without the
// spaces a form leaves around it.
func TestBackupConfigNormalizeTrimsSchedule(t *testing.T) {
	config, err := BackupConfig{
		DestinationType: BackupDestinationLocal,
		ScheduleCron:    "  0 3 * * *  ",
	}.Normalize()
	if err != nil {
		t.Fatalf("a configuration with a schedule: %v", err)
	}
	if config.ScheduleCron != "0 3 * * *" {
		t.Errorf("schedule = %q, want it trimmed", config.ScheduleCron)
	}
}

// TestBackupConfigSecretNames checks a listing can say which credentials are set
// without the values leaving the configuration.
func TestBackupConfigSecretNames(t *testing.T) {
	config := BackupConfig{SealedSecrets: map[string][]byte{
		"password":             []byte("sealed"),
		BackupSecretPassphrase: []byte("sealed"),
		// A name whose value was cleared is not a credential the instance holds.
		"private_key": {},
	}}

	names := config.SecretNames()
	if len(names) != 2 || names[0] != BackupSecretPassphrase || names[1] != "password" {
		t.Fatalf("names = %v, want the two that are set, in order", names)
	}
}

// TestBackupRunArchiveHeld checks only a successful run whose archive has not
// been rotated away is offered back.
func TestBackupRunArchiveHeld(t *testing.T) {
	rotated := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

	cases := map[string]struct {
		run  BackupRun
		want bool
	}{
		"succeeded":     {BackupRun{Status: BackupSuccess, Artifact: "tripvault_2026.tar.gz"}, true},
		"still running": {BackupRun{Status: BackupRunning, Artifact: "tripvault_2026.tar.gz"}, false},
		"failed":        {BackupRun{Status: BackupFailed}, false},
		"wrote nothing": {BackupRun{Status: BackupSuccess}, false},
		"rotated away": {
			BackupRun{Status: BackupSuccess, Artifact: "tripvault_2026.tar.gz", RotatedAt: &rotated}, false,
		},
	}
	for name, test := range cases {
		if got := test.run.ArchiveHeld(); got != test.want {
			t.Errorf("%s: held = %v, want %v", name, got, test.want)
		}
	}
}
