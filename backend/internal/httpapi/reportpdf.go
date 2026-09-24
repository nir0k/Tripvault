package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
	"github.com/nir0k/tripvault/backend/internal/pdf"
	"github.com/nir0k/tripvault/backend/internal/staticmap"
)

// A report is written up to be read, and read by people who were not there. The
// PDF is rendered on the server rather than printed by the browser, so that it
// looks the same wherever it was asked for and can be handed out through a
// read-only link, where there is no interface to print from.

// pdfPhotoWidth is the preview width photographs are taken at. 640 px across the
// text column of an A4 page is about 90 dots to the inch: enough that a face is
// a face on paper, small enough that a report of two hundred pictures is still a
// file somebody can send.
const pdfPhotoWidth = 640

// pdfWriteTimeout is how long one document may take to build and send. A report
// of two hundred photographs rendered for the first time takes longer than the
// server allows any other answer, so this one request is given more.
const pdfWriteTimeout = 5 * time.Minute

// pdfPhotoWorkers bounds how many photographs of one document are read at once.
// Rendering a missing preview is bounded again by the server's own limit on
// decoding, which every reader of previews shares.
const pdfPhotoWorkers = 8

// pdfPhotoLimit bounds how many photographs one document carries. A report is a
// story with pictures in it, not an album, and a thousand of them would be a
// file nobody can open and a request nobody can wait for.
const pdfPhotoLimit = 200

// handleReportPDF renders the trip's report for the signed-in reader.
func (s *Server) handleReportPDF(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	if trip.ReportID == nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "This trip has no report")
		return
	}

	reader := principalFrom(r.Context()).user
	// Somebody who may read the trip may read all of its files: private keeps a
	// photograph out of a read-only link, not away from the people on the trip.
	s.writeReportPDF(w, r, trip, *trip.ReportID, true, reader.Locale, reader.Units)
}

// handleSharedReportPDF renders the report a read-only link opens.
//
// The link decides what the document holds, exactly as it decides what the
// screen shows: a link made without private files renders without them.
func (s *Server) handleSharedReportPDF(w http.ResponseWriter, r *http.Request) {
	access := shareFrom(r.Context())
	if !access.Opens(domain.DocumentReport) {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return
	}

	// A link has no account behind it, so the language and the units come from
	// the request: whoever opened the link is reading in their own browser, and
	// the page sends what it is showing them.
	s.writeReportPDF(w, r, access.Trip, *access.Trip.ReportID,
		access.Link.IncludePrivateMedia, r.URL.Query().Get("lang"),
		domain.Units(r.URL.Query().Get("units")))
}

// writeReportPDF renders one report and streams it back.
//
// The words of the trip are written in the language asked for as content_lang,
// or in the language of the document's own labels, when the report is
// translated into it; otherwise, and field by field where nobody translated,
// in the original.
func (s *Server) writeReportPDF(w http.ResponseWriter, r *http.Request, trip domain.TripSummary,
	documentID uuid.UUID, includePrivateMedia bool, language string, units domain.Units) {
	// A recorder in a test cannot move its deadline, and nothing else refuses to.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(pdfWriteTimeout)); err != nil &&
		!errors.Is(err, http.ErrNotSupported) {
		s.logger.Warn("extend the report deadline failed",
			slog.String("request_id", RequestIDFrom(r.Context())), slog.Any("error", err))
	}
	content, err := s.documents.Content(r.Context(), documentID)
	if err != nil {
		s.writeDomainError(w, r, "read report", err)
		return
	}
	requested := r.URL.Query().Get("content_lang")
	if requested == "" {
		requested = language
	}
	reading := domain.ReadingLanguage(trip.Languages, requested)
	content = content.Translated(reading)
	trip.Trip = trip.Translated(trip.Translations, reading)

	// Units the request does not name, or names wrongly, are the kilometres the
	// document defaults to: a mistyped parameter is not worth refusing a report
	// over.
	if domain.ValidateUnits(units) != nil {
		units = domain.UnitsKilometres
	}
	report := pdf.Report{
		Trip:     trip.Trip,
		Content:  content,
		Totals:   domain.BuildReportTotals(trip.Trip, content),
		Language: language,
		Units:    units,
	}

	// The maps are drawn in both versions: they are what a reader who left the
	// photographs out still wants, and they cost the file little. Their tiles
	// are fetched while the photographs are read.
	var maps map[uuid.UUID]pdf.Map
	var drawing sync.WaitGroup
	drawing.Go(func() { maps = s.reportMaps(r.Context(), content) })

	// Photographs are what makes the document slow and large, so they are opt
	// out: a reader who wants the words alone asks for photos=false.
	if r.URL.Query().Get("photos") != "false" {
		cover, photos, err := s.reportPhotos(r.Context(), trip, content, includePrivateMedia)
		if err != nil {
			s.writeDomainError(w, r, "read report photographs", err)
			return
		}
		report.Cover, report.Photos = cover, photos
	}
	drawing.Wait()
	report.Maps, report.MapAttribution = maps, plainText(s.opts.MapAttribution)

	// The document is built into memory rather than into the response: a failure
	// half way through would otherwise arrive as a broken file under a 200,
	// which a browser saves without complaint.
	var document bytes.Buffer
	if err := pdf.Render(&document, report); err != nil {
		s.internalError(w, r, "render the report", err)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Length", strconv.Itoa(document.Len()))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(pdfFilename(trip.Title)))

	if _, err := w.Write(document.Bytes()); err != nil {
		s.logger.Warn("report transfer interrupted",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.Any("error", err))
	}
}

// reportMaps frames every map of a report and fetches the backgrounds.
//
// The trip's own map is fetched first. When it cannot be, the tile server is
// not answering, and every map is drawn without a background rather than each
// waiting out a timeout of its own; a map is still worth having without one.
//
// Arguments:
//   - ctx: context bounding the fetches.
//   - content: the report.
//
// Returns:
//   - the maps, keyed as pdf.MapRequests keys them.
func (s *Server) reportMaps(ctx context.Context, content domain.DocumentContent) map[uuid.UUID]pdf.Map {
	requests := pdf.MapRequests(content)
	maps := make(map[uuid.UUID]pdf.Map, len(requests))
	for _, request := range requests {
		maps[request.Key] = pdf.Map{Frame: staticmap.Fit(request.Points, request.Width, request.Height)}
	}
	if s.mapTiles == nil || len(requests) == 0 {
		return maps
	}

	var lock sync.Mutex
	background := func(key uuid.UUID) error {
		lock.Lock()
		m := maps[key]
		lock.Unlock()
		picture, err := staticmap.Background(ctx, s.mapTiles, m.Frame)
		if err != nil {
			return err
		}
		m.Background = picture
		lock.Lock()
		maps[key] = m
		lock.Unlock()
		return nil
	}
	if err := background(requests[0].Key); err != nil {
		s.logger.Warn("the report's maps are drawn without a background",
			slog.String("request_id", RequestIDFrom(ctx)), slog.Any("error", err))
		return maps
	}
	var group sync.WaitGroup
	for _, request := range requests[1:] {
		group.Go(func() {
			if err := background(request.Key); err != nil {
				s.logger.Warn("a map of the report is drawn without a background",
					slog.String("request_id", RequestIDFrom(ctx)), slog.Any("error", err))
			}
		})
	}
	group.Wait()
	return maps
}

// markup matches the tags of the attribution the interface shows as HTML.
var markup = regexp.MustCompile(`<[^>]*>`)

// plainText turns the map attribution, which may hold links and entities for
// the interface, into the words a printed page can carry.
func plainText(attribution string) string {
	return strings.Join(strings.Fields(html.UnescapeString(markup.ReplaceAllString(attribution, " "))), " ")
}

// reportPhotos renders the pictures of a report to the size the document uses.
//
// Arguments:
//   - ctx: context bounding the reads.
//   - trip: the trip, for its cover picture.
//   - content: the report, for the days and places pictures hang on.
//   - includePrivateMedia: whether files marked private may be shown.
//
// Returns:
//   - the cover, empty when the trip has none or it cannot be read.
//   - the pictures of each day and place, by the identifier they hang on.
//   - an error only when the catalogue cannot be read; a single picture that
//     cannot be rendered is left out rather than failing the document, because a
//     report missing one photograph is worth more than no report at all.
func (s *Server) reportPhotos(ctx context.Context, trip domain.TripSummary,
	content domain.DocumentContent, includePrivateMedia bool) ([]byte, map[uuid.UUID][]pdf.Photo, error) {
	pictures, err := s.galleryOf(ctx, trip.ID, includePrivateMedia)
	if err != nil {
		return nil, nil, err
	}

	var cover []byte
	if trip.CoverMediaID != nil {
		if item, held := pictures.byID[*trip.CoverMediaID]; held {
			cover, _ = s.photoPreview(ctx, item)
		}
	}

	// The targets are walked in the order the document writes them, so that a
	// report over the limit loses its last pictures rather than an arbitrary
	// scattering of them.
	targets := make([]uuid.UUID, 0, len(content.Days)+len(content.Items))
	for _, day := range content.Days {
		targets = append(targets, day.ID)
		for _, place := range content.Items {
			if place.DayID != nil && *place.DayID == day.ID {
				targets = append(targets, place.ID)
			}
		}
	}

	type wanted struct {
		target uuid.UUID
		item   domain.Media
		body   []byte
	}
	var chosen []wanted
	for _, target := range targets {
		for _, item := range reportPictures(pictures.byTarget[target]) {
			if len(chosen) < pdfPhotoLimit {
				chosen = append(chosen, wanted{target: target, item: item})
			}
		}
	}

	// The pictures are read side by side and put back in the order chosen, so a
	// document is as fast as the store allows and still reads the same each time.
	var group sync.WaitGroup
	workers := make(chan struct{}, pdfPhotoWorkers)
	for index := range chosen {
		group.Go(func() {
			workers <- struct{}{}
			defer func() { <-workers }()
			body, err := s.photoPreview(ctx, chosen[index].item)
			if err != nil {
				s.logger.Warn("a photograph was left out of a report",
					slog.String("media_id", chosen[index].item.ID.String()), slog.Any("error", err))
				return
			}
			chosen[index].body = body
		})
	}
	group.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	photos := make(map[uuid.UUID][]pdf.Photo)
	for _, each := range chosen {
		if each.body != nil {
			photos[each.target] = append(photos[each.target], pdf.Photo{JPEG: each.body, TakenAt: each.item.TakenAt})
		}
	}
	return cover, photos, nil
}

// reportPictures picks the pictures a day or a place shows in the report: the
// favourites it was given, and while it was given none, the first few of its
// gallery - a report written before anybody chose is still a report with
// photographs in it. The rest of the gallery is read in the trip's own.
func reportPictures(gallery []shown) []domain.Media {
	chosen := make([]domain.Media, 0, domain.MediaFavoriteLimit)
	for _, link := range gallery {
		if link.isFavorite {
			chosen = append(chosen, link.item)
		}
	}
	if len(chosen) == 0 {
		for _, link := range gallery {
			if len(chosen) == domain.MediaFavoriteLimit {
				break
			}
			chosen = append(chosen, link.item)
		}
	}
	if len(chosen) > domain.MediaFavoriteLimit {
		chosen = chosen[:domain.MediaFavoriteLimit]
	}
	return chosen
}

// photoPreview returns a photograph at the width the document draws pictures
// at. It is the same preview the gallery asks for: the one kept in the store
// when there is one, or a fresh one, rendered from the original and kept for
// the next reader - so a report is slow to export only the first time. The
// result is a JPEG whatever the file was uploaded as, which is also what keeps
// WebP out of a format that cannot carry it.
func (s *Server) photoPreview(ctx context.Context, item domain.Media) ([]byte, error) {
	if s.mediaFiles == nil {
		return nil, fmt.Errorf("this service has no media store")
	}
	if preview, ok := s.storedPreview(ctx, item, pdfPhotoWidth); ok {
		return preview, nil
	}

	// Decoding a picture costs memory, so it waits its turn with every other
	// preview being rendered.
	select {
	case s.thumbnails <- struct{}{}:
		defer func() { <-s.thumbnails }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	reader, err := s.mediaFiles.Open(ctx, item.StorageKey)
	if err != nil {
		return nil, err
	}
	original, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		return nil, err
	}
	preview, err := media.Thumbnail(original, pdfPhotoWidth)
	if err != nil {
		return nil, err
	}
	s.keepPreview(ctx, item, pdfPhotoWidth, preview)
	return preview, nil
}

// pdfFilename turns a trip's title into a filename a browser will save happily.
//
// Everything but letters, digits and a few separators is dropped rather than
// escaped: the name is a convenience, and one that survives every filesystem is
// worth more than one that reproduces the title exactly.
func pdfFilename(title string) string {
	var name strings.Builder
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			name.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			name.WriteRune('-')
		}
	}

	trimmed := strings.Trim(name.String(), "-")
	for strings.Contains(trimmed, "--") {
		trimmed = strings.ReplaceAll(trimmed, "--", "-")
	}
	if trimmed == "" {
		// A title written in an alphabet this keeps none of still needs a name.
		trimmed = "report"
	}
	if len(trimmed) > 60 {
		trimmed = strings.Trim(trimmed[:60], "-")
	}
	return trimmed + ".pdf"
}
