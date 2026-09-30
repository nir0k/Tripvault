package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The steps a check of a destination passes through, named in the result by the
// one that failed, so the form can say what to fix rather than only that
// something is wrong.
const (
	// CheckStepCredentials is reading the stored or typed credentials.
	CheckStepCredentials = "credentials"
	// CheckStepConnect is reaching the server at all.
	CheckStepConnect = "connect"
	// CheckStepSignIn is the server's host key and the account's password or key.
	CheckStepSignIn = "sign_in"
	// CheckStepDirectory is creating the directory archives go into.
	CheckStepDirectory = "directory"
	// CheckStepWrite is writing a file into it.
	CheckStepWrite = "write"
	// CheckStepDelete is removing that file again, which retention relies on.
	CheckStepDelete = "delete"
	// CheckStepList is reading what the directory holds.
	CheckStepList = "list"
)

// probePrefix starts the name of the file a check writes. The dot keeps it out
// of every listing of archives, and out of the names an archive may be opened by.
const probePrefix = ".tripvault-check-"

// CheckResult is what a check of a destination found.
type CheckResult struct {
	// Step is the step that failed, or empty when every step passed.
	Step string
	// Err is why that step failed.
	Err error
	// HostKey is the key an SFTP server presented to a configuration that pins
	// none, in authorized_keys form, for the form to fill in.
	HostKey string
	// Archives is how many archives the destination holds already.
	Archives int
}

// prober is a destination that can write and remove a file of its own to show
// it is writable. Both destinations of this package are.
type prober interface {
	probe(ctx context.Context) (string, error)
}

// checkTimeout bounds everything a check asks of a server once it is signed
// in. A backup's transfers may take hours and are bounded by their context
// alone, but a check is somebody waiting at a form.
const checkTimeout = 30 * time.Second

// Check - tries a configuration's destination without taking a backup.
//
// It does everything a backup does to the destination short of writing an
// archive: opens the credentials, connects and signs in, creates the directory,
// writes a small file and removes it, and lists what is there. Nothing is
// recorded - in particular a host key learned on the way is handed back rather
// than pinned, because the configuration being checked may not be saved yet.
//
// Arguments:
//   - ctx: context the destination's requests run under.
//   - config: the configuration to check, saved or not.
//
// Returns:
//   - what was found; a failure is a result, not an error, since reporting it is
//     the point.
func (r *Resolver) Check(ctx context.Context, config domain.BackupConfig) CheckResult {
	var destination Destination
	var result CheckResult

	switch config.DestinationType {
	case domain.BackupDestinationLocal:
		destination = r.local

	case domain.BackupDestinationSFTP:
		secrets, err := r.openSecrets(config.SealedSecrets)
		if err != nil {
			return CheckResult{Step: CheckStepCredentials, Err: err}
		}
		sftpDestination, learned, step, err := dialSFTP(ctx, config.DestinationParams, secrets)
		result.HostKey = learned
		if err != nil {
			result.Step, result.Err = step, err
			return result
		}
		defer func() { _ = sftpDestination.Close() }()
		_ = sftpDestination.raw.SetDeadline(time.Now().Add(checkTimeout))
		destination = sftpDestination

	default:
		return CheckResult{Step: CheckStepConnect,
			Err: fmt.Errorf("backups to %s are not implemented", config.DestinationType)}
	}

	if p, ok := destination.(prober); ok {
		if step, err := p.probe(ctx); err != nil {
			result.Step, result.Err = step, err
			return result
		}
	}

	archives, err := destination.List(ctx)
	if err != nil {
		result.Step, result.Err = CheckStepList, err
		return result
	}
	result.Archives = len(archives)
	return result
}

// probeName makes the name of a check's file, random so two checks at once do
// not remove each other's.
func probeName() (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", errors.New("could not name the check file")
	}
	return probePrefix + hex.EncodeToString(suffix[:]), nil
}
