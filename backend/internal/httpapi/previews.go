package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
)

// renderWorkers - decides how many previews are rendered at once, from the
// processors this process may use.
//
// Decoding a picture keeps one processor busy, so the background takes half of
// them for uploads and leaves the rest to the API, the database and the uploads
// themselves: an import must not make the interface crawl. Every rendering -
// for a reader, a PDF or the background - also holds a decoded picture in
// memory, so the total is bounded too, with room above the background for the
// one walk over older pictures and for a reader asking for a preview nobody
// has rendered yet. A Raspberry Pi with four cores gets two workers and four
// renderings in all.
//
// Arguments:
//   - processors: the processors available, as runtime.GOMAXPROCS reports
//     them; it follows a container's CPU limit.
//
// Returns:
//   - the number of workers rendering queued uploads.
//   - the number of renderings allowed at once, whoever asked for them.
func renderWorkers(processors int) (int, int) {
	background := max(1, processors/2)
	return background, background + 2
}

// uploadQueue holds the uploads waiting for their previews, in the order they
// arrived. It has no fixed bound: an import sends pictures far faster than a
// small server renders them, and a picture turned away would wait for its
// first reader. One entry is a catalogue row, so even thousands are little
// memory; the queue lives only in the process, and whatever it held when the
// service stopped is found by the walk at the next start.
type uploadQueue struct {
	mu    sync.Mutex
	items []domain.Media
	// ready wakes a waiting worker; one signal is enough, because a worker that
	// takes an item and leaves more behind passes the signal on.
	ready chan struct{}
}

// newUploadQueue makes an empty queue.
func newUploadQueue() *uploadQueue {
	return &uploadQueue{ready: make(chan struct{}, 1)}
}

// push adds a picture to the end of the queue and wakes a worker.
func (q *uploadQueue) push(item domain.Media) {
	q.mu.Lock()
	q.items = append(q.items, item)
	q.mu.Unlock()
	q.signal()
}

// pop takes the picture at the front of the queue, reporting false when there
// is none.
func (q *uploadQueue) pop() (domain.Media, bool) {
	q.mu.Lock()
	if len(q.items) == 0 {
		q.mu.Unlock()
		return domain.Media{}, false
	}
	item := q.items[0]
	q.items[0] = domain.Media{}
	q.items = q.items[1:]
	left := len(q.items)
	if left == 0 {
		// Dropping the drained array lets the memory of a large import go.
		q.items = nil
	}
	q.mu.Unlock()
	if left > 0 {
		q.signal()
	}
	return item, true
}

// length is the number of pictures waiting.
func (q *uploadQueue) length() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// signal wakes one waiting worker, or leaves a signal for the next one.
func (q *uploadQueue) signal() {
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

// previewRender is one rendering of a file's previews in flight, which every
// other request for the same file waits for instead of decoding it again.
type previewRender struct {
	done     chan struct{}
	previews map[int][]byte
	err      error
}

// previewsFor returns every preview width of a file, rendering them when they
// are not all kept yet.
//
// A gallery opened right after an upload asks for the very pictures the
// background is rendering, and on a small server decoding one twice costs
// seconds; so a request that finds a rendering of the same file in flight
// waits for it rather than starting its own.
//
// Arguments:
//   - ctx: bounds the wait; a rendering already started runs to its end.
//   - item: the file.
//
// Returns:
//   - a JPEG for every width in media.Sizes, keyed by that width.
//   - media.ErrNotFound when the original is gone, media.ErrNoThumbnail when
//     it is not a picture this service decodes, or another error.
func (s *Server) previewsFor(ctx context.Context, item domain.Media) (map[int][]byte, error) {
	for {
		s.rendersMu.Lock()
		render, running := s.renders[item.StorageKey]
		if !running {
			render = &previewRender{done: make(chan struct{})}
			s.renders[item.StorageKey] = render
		}
		s.rendersMu.Unlock()

		if !running {
			render.previews, render.err = s.renderPreviews(ctx, item)
			s.rendersMu.Lock()
			delete(s.renders, item.StorageKey)
			s.rendersMu.Unlock()
			close(render.done)
			return render.previews, render.err
		}

		select {
		case <-render.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		// The rendering this request waited for may have ended only because
		// the one who started it went away; the file is still worth rendering
		// for this one.
		if isCancellation(render.err) && ctx.Err() == nil {
			continue
		}
		return render.previews, render.err
	}
}

// previewProgress counts the background rendering for the administration
// page. The work comes in waves - a batch of uploads, the walk at start-up, the
// walk after a restore - and a wave is what the progress bar measures: it
// begins when work arrives at an idle background and ends when all of it is
// handled. The counters live in memory and start again with the process.
type previewProgress struct {
	mu            sync.Mutex
	waveTotal     int
	waveDone      int
	waveStartedAt time.Time
	rendering     int
	backfill      bool
	checked       int
	toCheck       int
	rendered      int
	failed        int
}

// addToWave counts files handed to the background, beginning a new wave when
// the last one is over.
func (s *Server) addToWave(count int) {
	s.progress.mu.Lock()
	defer s.progress.mu.Unlock()
	if s.progress.waveDone >= s.progress.waveTotal {
		s.progress.waveTotal, s.progress.waveDone = 0, 0
		s.progress.waveStartedAt = s.now()
	}
	s.progress.waveTotal += count
}

// finishInWave counts files the background is done with, whatever came of them.
func (s *Server) finishInWave(count int) {
	s.progress.mu.Lock()
	defer s.progress.mu.Unlock()
	s.progress.waveDone = min(s.progress.waveDone+count, s.progress.waveTotal)
}

// countRender records how one rendering ended. A file deleted meanwhile and a
// reader who went away are nobody's failure; a file that cannot be decoded is.
func (s *Server) countRender(err error) {
	s.progress.mu.Lock()
	defer s.progress.mu.Unlock()
	switch {
	case err == nil:
		s.progress.rendered++
	case errors.Is(err, media.ErrNotFound), isCancellation(err):
	default:
		s.progress.failed++
	}
}

// previewStatusResponse is the state of preview rendering on the
// administration page.
type previewStatusResponse struct {
	// Background is false when this server renders previews only on request.
	Background bool `json:"background"`
	// Active, Total, Done and StartedAt describe the current wave, or the last
	// one when Active is false.
	Active    bool                    `json:"active"`
	Total     int                     `json:"total"`
	Done      int                     `json:"done"`
	StartedAt *time.Time              `json:"started_at"`
	Queued    int                     `json:"queued"`
	Rendering int                     `json:"rendering"`
	Backfill  previewBackfillResponse `json:"backfill"`
	Rendered  int                     `json:"rendered"`
	Failed    int                     `json:"failed"`
}

// previewBackfillResponse is the walk over stored files that renders the
// previews they lack.
type previewBackfillResponse struct {
	Running bool `json:"running"`
	Checked int  `json:"checked"`
	Total   int  `json:"total"`
}

// handlePreviewStatus reports how preview rendering is going. It reads only
// counters in memory, so the administration page can poll it while work runs.
func (s *Server) handlePreviewStatus(w http.ResponseWriter, _ *http.Request) {
	s.progress.mu.Lock()
	response := previewStatusResponse{
		Background: s.previewsRunning.Load(),
		Active:     s.progress.waveDone < s.progress.waveTotal,
		Total:      s.progress.waveTotal,
		Done:       s.progress.waveDone,
		Queued:     s.uploads.length(),
		Rendering:  s.progress.rendering,
		Backfill: previewBackfillResponse{
			Running: s.progress.backfill,
			Checked: s.progress.checked,
			Total:   s.progress.toCheck,
		},
		Rendered: s.progress.rendered,
		Failed:   s.progress.failed,
	}
	if !s.progress.waveStartedAt.IsZero() {
		started := s.progress.waveStartedAt
		response.StartedAt = &started
	}
	s.progress.mu.Unlock()
	writeJSON(w, s.logger, http.StatusOK, response)
}

// isCancellation says whether an error means only that the work was called off.
func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// renderPreviews decodes the original once, renders every preview width and
// keeps them in the store for the next reader.
//
// The original is read whole whatever the upload limit is now: it was accepted
// under the limit of its day, and a limit lowered since must not leave it
// without previews.
func (s *Server) renderPreviews(ctx context.Context, item domain.Media) (map[int][]byte, error) {
	if s.mediaFiles == nil {
		return nil, fmt.Errorf("this service has no media store")
	}
	// Decoding a picture costs memory, so only a few run at a time; the rest
	// wait rather than pile up.
	select {
	case s.thumbnails <- struct{}{}:
		defer func() { <-s.thumbnails }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.progress.mu.Lock()
	s.progress.rendering++
	s.progress.mu.Unlock()
	defer func() {
		s.progress.mu.Lock()
		s.progress.rendering--
		s.progress.mu.Unlock()
	}()

	previews, err := s.decodePreviews(ctx, item)
	s.countRender(err)
	if err != nil {
		return nil, err
	}
	s.keepPreviews(ctx, item, previews)
	return previews, nil
}

// decodePreviews reads the original and renders every preview width of it.
func (s *Server) decodePreviews(ctx context.Context, item domain.Media) (map[int][]byte, error) {
	file, err := s.mediaFiles.Open(ctx, item.StorageKey)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		return nil, fmt.Errorf("read media: %w", err)
	}
	return media.Previews(data)
}

// keepPreviews stores rendered previews and removes those an older renderer
// made of the same file. Failing to keep one costs only a render next time, so
// it is logged rather than returned.
//
// The file may be deleted while its previews are being rendered, and a preview
// stored after that deletion would keep a copy of a removed photograph on the
// disk. So the original is looked for once the previews are in place, and they
// go again when the original is gone: whichever of the two removals runs last
// clears them.
func (s *Server) keepPreviews(ctx context.Context, item domain.Media, previews map[int][]byte) {
	for width, preview := range previews {
		if _, err := s.mediaFiles.Put(ctx, media.PreviewKey(item.StorageKey, width), bytes.NewReader(preview)); err != nil {
			s.logger.Warn("store preview failed", "error", err, "media_id", item.ID.String())
		}
	}
	original, err := s.mediaFiles.Open(ctx, item.StorageKey)
	if err == nil {
		_ = original.Close()
		if err := media.DeleteLegacyPreviews(ctx, s.mediaFiles, item.StorageKey); err != nil {
			s.logger.Warn("delete previews of an older renderer failed", "error", err, "media_id", item.ID.String())
		}
		return
	}
	if err := media.DeleteWithPreviews(ctx, s.mediaFiles, item.StorageKey); err != nil {
		s.logger.Warn("delete orphaned previews failed", "error", err, "media_id", item.ID.String())
	}
}

// hasAllPreviews says whether every preview width of a file is already kept.
func (s *Server) hasAllPreviews(ctx context.Context, item domain.Media) bool {
	for _, width := range media.Sizes {
		file, err := s.mediaFiles.Open(ctx, media.PreviewKey(item.StorageKey, width))
		if err != nil {
			return false
		}
		_ = file.Close()
	}
	return true
}

// startPreviewWork starts rendering previews in the background: those of every
// upload from now on, and those of the pictures stored before that have none.
// Only a running server does this; one built for a test renders on request.
func (s *Server) startPreviewWork() {
	if s.mediaFiles == nil || !s.previewsRunning.CompareAndSwap(false, true) {
		return
	}
	s.logger.Info("rendering previews in the background",
		slog.Int("workers", s.previewWorkers), slog.Int("renderings", cap(s.thumbnails)))
	for range s.previewWorkers {
		go s.renderQueuedPreviews()
	}
	s.requestPreviewBackfill()
}

// queuePreviews hands a freshly uploaded picture to the background, so its
// previews are ready before anybody opens the gallery.
func (s *Server) queuePreviews(item domain.Media) {
	if !s.previewsRunning.Load() {
		return
	}
	// The file is counted before it is queued, so a worker quick to finish it
	// never finds more done than there was to do.
	s.addToWave(1)
	s.uploads.push(item)
}

// renderQueuedPreviews renders the previews of queued uploads until the service
// stops.
func (s *Server) renderQueuedPreviews() {
	for {
		if s.workContext.Err() != nil {
			return
		}
		item, ok := s.uploads.pop()
		if !ok {
			select {
			case <-s.workContext.Done():
				return
			case <-s.uploads.ready:
			}
			continue
		}
		s.inBackground(func(ctx context.Context) {
			s.renderInBackground(ctx, item)
		})
		s.finishInWave(1)
	}
}

// renderInBackground renders the previews of one file for nobody in
// particular. A file deleted meanwhile, or one that is not a picture this
// service decodes, is not a failure; anything else is logged.
func (s *Server) renderInBackground(ctx context.Context, item domain.Media) bool {
	_, err := s.previewsFor(ctx, item)
	switch {
	case err == nil:
		return true
	case errors.Is(err, media.ErrNotFound), errors.Is(err, media.ErrNoThumbnail), isCancellation(err):
	default:
		s.logger.Warn("render previews failed", "error", err, "media_id", item.ID.String())
	}
	return false
}

// inBackground runs work of the service's own the way requireAvailable runs a
// request: it waits while a restore replaces the service, and a restore that
// begins meanwhile cancels it, so nothing is written into a store being
// replaced.
func (s *Server) inBackground(work func(ctx context.Context)) {
	s.restoreGate.RLock()
	defer s.restoreGate.RUnlock()

	ctx, cancel := context.WithCancel(s.workContext)
	defer cancel()
	stop := context.AfterFunc(s.servingContext(), cancel)
	defer stop()
	work(ctx)
}

// requestPreviewBackfill asks for one walk over every stored file that renders
// the previews missing. A request made while a walk runs is not lost: that
// walk is followed by another, because a restore may have replaced the files
// the first one was looking at.
func (s *Server) requestPreviewBackfill() {
	if !s.previewsRunning.Load() {
		return
	}
	s.backfillWanted.Store(true)
	if !s.backfillRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		for {
			for s.backfillWanted.Swap(false) {
				s.backfillPreviews()
			}
			s.backfillRunning.Store(false)
			// A request that arrived between the last walk and the line above
			// found the walk still marked as running and left it to this one.
			if !s.backfillWanted.Load() || !s.backfillRunning.CompareAndSwap(false, true) {
				return
			}
		}
	}()
}

// backfillPreviews renders, one file at a time, the previews of every stored
// file that lacks any. Pictures uploaded before previews were rendered at
// upload, and every picture after a restore - backups leave previews out -
// would otherwise wait for their first reader.
func (s *Server) backfillPreviews() {
	var items []domain.Media
	var err error
	s.inBackground(func(ctx context.Context) {
		items, err = s.media.ListAll(ctx)
	})
	if err != nil {
		if !isCancellation(err) {
			s.logger.Warn("list media for previews failed", "error", err)
		}
		return
	}

	s.addToWave(len(items))
	s.progress.mu.Lock()
	s.progress.backfill, s.progress.checked, s.progress.toCheck = true, 0, len(items)
	s.progress.mu.Unlock()
	checked := 0
	// A walk cut short by shutdown still closes its part of the wave, so the
	// progress bar does not wait for files nobody will look at.
	defer func() {
		s.finishInWave(len(items) - checked)
		s.progress.mu.Lock()
		s.progress.backfill = false
		s.progress.mu.Unlock()
	}()

	started := time.Now()
	rendered := 0
	for _, item := range items {
		if s.workContext.Err() != nil {
			return
		}
		s.inBackground(func(ctx context.Context) {
			if !s.hasAllPreviews(ctx, item) && s.renderInBackground(ctx, item) {
				rendered++
			}
		})
		checked++
		s.finishInWave(1)
		s.progress.mu.Lock()
		s.progress.checked = checked
		s.progress.mu.Unlock()
	}
	if rendered > 0 {
		s.logger.Info("rendered missing previews", slog.Int("files", rendered),
			slog.Duration("took", time.Since(started)))
	}
}
