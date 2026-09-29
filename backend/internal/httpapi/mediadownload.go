package httpapi

import (
	"archive/zip"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
)

// Pictures are downloaded by the browser itself, not by the page: a page can
// only fetch what it then holds whole in memory, which an archive of a long
// trip does not fit. The page asks for a download and gets an address back;
// the browser opens that address with its own downloader, which writes to the
// disk as the bytes come, shows its progress and resumes a single file.
//
// A browser opening an address sends no Authorization header, so the address
// is backed by a ticket. Its secret travels in a cookie scoped to that one
// address - never in the address itself, which the proxies in front of the
// service would write to their logs - and the address carries only the
// ticket's identifier, which is worth nothing without the cookie. A ticket
// serves for downloadLifetime, as often as the browser asks: a download
// manager may ask twice, and a retry after a dropped connection asks again.
// It is kept in memory, since the service runs as one process; a restart ends
// the downloads it would end anyway.

// maxDownloadMedia bounds the pictures one download holds: a whole gallery of
// a long trip fits, a request naming everything the service has does not.
const maxDownloadMedia = 5000

// downloadLifetime is how long a ticket serves; a download started within it
// runs to its end however long that takes.
const downloadLifetime = 10 * time.Minute

// downloadCookie starts the name of the cookie holding a ticket's secret; the
// identifier ends it, so several downloads can run at once.
const downloadCookie = "tripvault_download_"

// errDownloadNotAllowed refuses a download to a read-only link whose owner
// did not allow one.
var errDownloadNotAllowed = errors.New("the link does not allow downloads")

// downloadRequest is the body of POST .../media:download: the pictures to
// download, in the order they are wanted.
type downloadRequest struct {
	MediaIDs []string `json:"media_ids"`
}

// downloadResponse is where the browser downloads what it asked for.
type downloadResponse struct {
	URL string `json:"url"`
}

// downloadTicket is what one download address serves: one picture as its
// file, or several as an archive named after the trip.
type downloadTicket struct {
	secret  [sha256.Size]byte
	expires time.Time
	title   string
	items   []domain.Media
}

// downloadTickets keeps the tickets in use.
type downloadTickets struct {
	mu      sync.Mutex
	tickets map[string]downloadTicket
}

// issue stores a ticket for some pictures and returns its identifier and
// secret; expired tickets go at the same time.
func (d *downloadTickets) issue(now time.Time, title string, items []domain.Media) (string, string) {
	id := randomToken(16)
	secret := randomToken(32)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tickets == nil {
		d.tickets = map[string]downloadTicket{}
	}
	for key, ticket := range d.tickets {
		if now.After(ticket.expires) {
			delete(d.tickets, key)
		}
	}
	d.tickets[id] = downloadTicket{
		secret: sha256.Sum256([]byte(secret)), expires: now.Add(downloadLifetime), title: title, items: items,
	}
	return id, secret
}

// redeem finds the ticket of an identifier whose secret matches, while it serves.
func (d *downloadTickets) redeem(now time.Time, id, secret string) (downloadTicket, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ticket, ok := d.tickets[id]
	sum := sha256.Sum256([]byte(secret))
	if !ok || now.After(ticket.expires) || subtle.ConstantTimeCompare(sum[:], ticket.secret[:]) != 1 {
		return downloadTicket{}, false
	}
	return ticket, true
}

// randomToken is a random value of so many bytes, in base64url.
func randomToken(size int) string {
	buffer := make([]byte, size)
	_, _ = rand.Read(buffer)
	return base64.RawURLEncoding.EncodeToString(buffer)
}

// attachment names a file the browser saves: the name travels encoded, so a
// name in any alphabet survives, and a path in it is dropped.
func attachment(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

// handleMediaDownload issues a download of pictures of a trip to somebody with
// an account. Every picture must belong to the trip.
func (s *Server) handleMediaDownload(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	s.issueDownload(w, r, trip.Trip, true)
}

// handleSharedMediaDownload issues a download to a read-only link that allows
// downloads; a private picture needs a link made to show private pictures, as
// for reading one.
func (s *Server) handleSharedMediaDownload(w http.ResponseWriter, r *http.Request) {
	access := shareFrom(r.Context())
	if !access.Link.AllowDownload {
		s.writeError(w, r, http.StatusForbidden, "download_not_allowed", errDownloadNotAllowed.Error())
		return
	}
	s.issueDownload(w, r, access.Trip.Trip, access.Link.IncludePrivateMedia)
}

// issueDownload checks the pictures named in the request and answers with the
// address the browser downloads them from, the ticket's secret set as a cookie
// scoped to that address.
//
// Arguments:
//   - w, r: the request and its answer.
//   - trip: the trip the pictures must belong to.
//   - includePrivate: whether pictures marked private may be included.
func (s *Server) issueDownload(w http.ResponseWriter, r *http.Request, trip domain.Trip, includePrivate bool) {
	var body downloadRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if len(body.MediaIDs) == 0 || len(body.MediaIDs) > maxDownloadMedia {
		s.writeDomainError(w, r, "validate download", domain.NewValidationError("media_ids", "out_of_range",
			"must name between 1 and 5000 pictures"))
		return
	}
	items, err := s.media.ListByTrip(r.Context(), trip.ID)
	if err != nil {
		s.internalError(w, r, "list media", err)
		return
	}
	byID := make(map[uuid.UUID]domain.Media, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	chosen := make([]domain.Media, 0, len(body.MediaIDs))
	seen := make(map[uuid.UUID]bool, len(body.MediaIDs))
	for _, raw := range body.MediaIDs {
		id, err := uuid.Parse(raw)
		item, known := byID[id]
		// A picture of another trip, or a private one a link may not show, is
		// reported exactly like an unknown one.
		if err != nil || !known || !item.VisibleTo(includePrivate) {
			s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
			return
		}
		if !seen[id] {
			seen[id] = true
			chosen = append(chosen, item)
		}
	}

	id, secret := s.downloads.issue(s.now(), trip.Title, chosen)
	address := "/api/v1/downloads/" + id
	http.SetCookie(w, &http.Cookie{
		Name: downloadCookie + id, Value: secret, Path: address, MaxAge: int(downloadLifetime.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	writeJSON(w, s.logger, http.StatusCreated, downloadResponse{URL: address})
}

// handleDownload serves a download the browser opened: one picture as its
// file, which the browser may resume, or several as a ZIP archive. The ticket
// is read from the cookie issued with the address; without it, or once it has
// expired, the address answers 404.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "downloadID")
	cookie, err := r.Cookie(downloadCookie + id)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return
	}
	ticket, ok := s.downloads.redeem(s.now(), id, cookie.Value)
	if !ok {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return
	}
	if len(ticket.items) == 1 {
		s.writeDownloadedFile(w, r, ticket.items[0])
		return
	}
	s.writeArchive(w, r, ticket)
}

// writeDownloadedFile sends one picture to be saved under its original name,
// answering a range, so a download broken off resumes where it stopped.
func (s *Server) writeDownloadedFile(w http.ResponseWriter, r *http.Request, item domain.Media) {
	file, err := s.mediaFiles.Open(r.Context(), item.StorageKey)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
			return
		}
		s.internalError(w, r, "open media", err)
		return
	}
	defer func() { _ = file.Close() }()
	w.Header().Set("Content-Type", item.MIME)
	w.Header().Set("Content-Disposition", attachment(item.OriginalName))
	if seeker, ok := file.(io.ReadSeeker); ok {
		http.ServeContent(w, r, "", item.CreatedAt, seeker)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(item.Size, 10))
	if _, err := io.Copy(w, file); err != nil {
		s.logger.Warn("send media failed", "error", err, "media_id", item.ID.String())
	}
}

// writeArchive writes the pictures of a ticket into a ZIP archive, stored
// rather than compressed: a photograph is compressed already, and squeezing it
// again costs time for nothing. The archive is written as it is read from the
// store, so it never sits whole in memory; names that repeat get a number,
// "IMG_0001 (2).jpg".
func (s *Server) writeArchive(w http.ResponseWriter, r *http.Request, ticket downloadTicket) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", attachment(archiveName(ticket.title)))
	archive := zip.NewWriter(w)
	names := make(map[string]int, len(ticket.items))
	for _, item := range ticket.items {
		if err := s.addToArchive(r, archive, item, uniqueName(names, item.OriginalName)); err != nil {
			// The answer has begun; all that is left is to stop it short, which
			// the browser reports as a failed download rather than a broken file.
			s.logger.Warn("write media archive failed", "error", err, "media_id", item.ID.String())
			return
		}
	}
	if err := archive.Close(); err != nil {
		s.logger.Warn("finish media archive failed", "error", err)
	}
}

// addToArchive copies one stored file into the archive under a name.
func (s *Server) addToArchive(r *http.Request, archive *zip.Writer, item domain.Media, name string) error {
	file, err := s.mediaFiles.Open(r.Context(), item.StorageKey)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) {
			// A file lost from the store is left out rather than failing the rest.
			return nil
		}
		return err
	}
	defer func() { _ = file.Close() }()
	modified := item.CreatedAt
	if item.TakenAt != nil {
		modified = *item.TakenAt
	}
	entry, err := archive.CreateHeader(&zip.FileHeader{
		Name: name, Method: zip.Store, Modified: modified.In(time.UTC),
	})
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, file)
	return err
}

// uniqueName keeps the names in an archive apart: a name seen before gets the
// number of its turn before its extension, "IMG_0001 (2).jpg".
func uniqueName(seen map[string]int, name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	if name == "" || name == "." || name == "/" {
		name = "photo"
	}
	key := strings.ToLower(name)
	seen[key]++
	if seen[key] == 1 {
		return name
	}
	extension := path.Ext(name)
	candidate := strings.TrimSuffix(name, extension) + " (" + strconv.Itoa(seen[key]) + ")" + extension
	// The numbered name may itself be a name that comes later; count it too.
	seen[strings.ToLower(candidate)]++
	return candidate
}

// archiveName is the name the archive of a trip's pictures is saved under.
func archiveName(title string) string {
	return fileTitle(title) + " - photos.zip"
}

// fileTitle turns a trip's title into the start of a file name, in any
// alphabet, without the characters a file system refuses.
func fileTitle(title string) string {
	title = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < ' ' {
			return -1
		}
		return r
	}, strings.TrimSpace(title))
	if title == "" {
		return "Tripvault"
	}
	return title
}
