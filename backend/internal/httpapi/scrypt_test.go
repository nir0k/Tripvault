package httpapi

import "github.com/nir0k/tripvault/backend/internal/backup"

// init makes locking an archive cheap: the tests lock and open archives by the
// dozen, and the cost that makes a passphrase slow to guess is not what they
// check.
func init() {
	backup.ScryptWorkFactor = 10
}
