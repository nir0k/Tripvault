package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/nir0k/tripvault/backend/internal/media"
)

// leftoverAge is how old an object nothing points at must be before it is
// removed. An upload writes its bytes before its row and an avatar its file
// before the account names it; an hour is far beyond the longest of either.
const leftoverAge = time.Hour

// sweepStore removes what the media store holds that no record points at: a
// file whose row never came to be because the service stopped in between, a
// file whose removal failed after its row went, and previews of no file or of
// a renderer or width no longer served. It runs after every walk over the
// previews, at start-up and after a restore.
//
// The store and the database are two places, and the walk trusts the
// database. A database that names far fewer files than the disk holds - an
// instance pointed at a new, empty database with the old volume - is not one
// to trust: when most of the files would go, nothing goes and the service
// says so instead.
func (s *Server) sweepStore() {
	walker, ok := s.mediaFiles.(media.Walker)
	if !ok || s.media == nil {
		return
	}
	s.inBackground(func(ctx context.Context) {
		before := s.now().Add(-leftoverAge)
		items, err := s.media.ListAll(ctx)
		if err != nil {
			s.logSweepFailure("list media for the sweep failed", err)
			return
		}
		others, err := s.media.OtherKeys(ctx)
		if err != nil {
			s.logSweepFailure("list avatars and idea photos for the sweep failed", err)
			return
		}
		pictures := make(map[string]int, len(items))
		for _, item := range items {
			pictures[item.StorageKey] = item.Width
		}

		found, err := media.FindLeftovers(ctx, walker, pictures, others, before)
		if err != nil {
			s.logSweepFailure("walk the media store failed", err)
			return
		}
		if len(found.Originals)*2 > found.Files {
			s.logger.Warn("the media store holds files the database does not know; left them all in place",
				slog.Int("unknown", len(found.Originals)), slog.Int("files", found.Files))
			return
		}

		removed := 0
		for _, key := range append(found.Originals, found.Previews...) {
			if err := s.mediaFiles.Delete(ctx, key); err != nil {
				s.logger.Warn("remove a leftover from the media store failed", "error", err, "key", key)
				continue
			}
			removed++
		}
		if removed > 0 {
			s.logger.Info("removed leftovers from the media store",
				slog.Int("files", len(found.Originals)), slog.Int("previews", len(found.Previews)))
		}
	})
}

// logSweepFailure records a sweep that could not run, unless it was only
// called off.
func (s *Server) logSweepFailure(message string, err error) {
	if !isCancellation(err) {
		s.logger.Warn(message, "error", err)
	}
}
