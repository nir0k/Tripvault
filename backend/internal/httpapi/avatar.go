package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
)

// The picture an account is shown by. It lives in the media store beside the
// trips' files and is served only from here, like everything else that was
// uploaded: a static path would hand every avatar to anybody who guessed a
// user identifier.
//
// What is stored is never what was sent. The browser crops the picture to the
// circle the reader chose and the server renders that to one small square, so
// nothing keeps a camera's metadata, and an avatar costs the same wherever it
// came from.

// avatarWidth is the size an avatar is kept and served at.
const avatarWidth = 256

// avatarKey lays an account's picture out under its own name. One account keeps
// one object, overwritten in place, because nothing points at the old bytes.
func avatarKey(userID uuid.UUID) string {
	return "avatars/" + userID.String() + ".jpg"
}

// handleSetAvatar stores the picture the signed-in account is shown by.
func (s *Server) handleSetAvatar(w http.ResponseWriter, r *http.Request) {
	user := principalFrom(r.Context()).user
	if s.mediaFiles == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "unavailable", "File storage is not configured")
		return
	}

	s.extendUploadDeadlines(w, r)
	data, ok := s.readAvatarPart(w, r)
	if !ok {
		return
	}
	square, err := media.Thumbnail(data, avatarWidth)
	if err != nil {
		s.writeMediaError(w, r, domain.ErrMediaUnsupported)
		return
	}

	key := avatarKey(user.ID)
	if _, err := s.mediaFiles.Put(r.Context(), key, bytes.NewReader(square)); err != nil {
		s.internalError(w, r, "store avatar", err)
		return
	}
	updated, previous, err := s.users.SetAvatar(r.Context(), user.ID, key, s.now())
	if err != nil {
		// The row is what makes the bytes reachable, so bytes without it are
		// rubbish and go at once.
		_ = s.mediaFiles.Delete(r.Context(), key)
		s.writeDomainError(w, r, "set avatar", err)
		return
	}
	s.deleteAvatarBytes(r, previous)
	writeJSON(w, s.logger, http.StatusOK, newUserResponse(updated))
}

// handleDeleteAvatar takes the picture away, leaving the account with the
// placeholder everybody starts with.
func (s *Server) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	user := principalFrom(r.Context()).user
	updated, previous, err := s.users.SetAvatar(r.Context(), user.ID, "", s.now())
	if err != nil {
		s.writeDomainError(w, r, "remove avatar", err)
		return
	}
	s.deleteAvatarBytes(r, previous)
	writeJSON(w, s.logger, http.StatusOK, newUserResponse(updated))
}

// handleGetAvatar serves an account's picture to anybody signed in: the people
// of a trip see each other's names, and the picture says no more than the name
// does. An account without one answers 404, which is what the interface draws
// its placeholder on.
func (s *Server) handleGetAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.pathUUID(w, r, "userID")
	if !ok {
		return
	}
	user, err := s.users.GetByID(r.Context(), userID)
	if err != nil {
		s.writeDomainError(w, r, "get account", err)
		return
	}
	if user.AvatarKey == "" || s.mediaFiles == nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return
	}

	// An avatar keeps one name for the life of the account, so the moment it
	// changed is what tells a browser its copy is stale.
	changed := time.Time{}
	if user.AvatarUpdatedAt != nil {
		changed = *user.AvatarUpdatedAt
	}
	tag := `"` + strconv.FormatInt(changed.UnixNano(), 36) + `"`
	if match := r.Header.Get("If-None-Match"); match == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	file, err := s.mediaFiles.Open(r.Context(), user.AvatarKey)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
			return
		}
		s.internalError(w, r, "open avatar", err)
		return
	}
	defer func() { _ = file.Close() }()

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	if _, err := io.Copy(w, file); err != nil {
		s.logger.Warn("send avatar failed", "error", err, "user_id", userID.String())
	}
}

// readAvatarPart reads the one file part of an avatar upload, bounded by the
// same limit a photograph is bounded by.
//
// Arguments:
//   - w: the response, written only when the request is refused.
//   - r: the multipart request.
//
// Returns:
//   - the bytes that were sent.
//   - false when the request was refused, in which case the answer is written.
func (s *Server) readAvatarPart(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	reader, err := r.MultipartReader()
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request",
			"The request must be a multipart upload with one file part")
		return nil, false
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "The upload is malformed")
			return nil, false
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		// One byte over the limit is read on purpose, so a file exactly at the
		// limit is accepted and the first byte above it is noticed.
		data, err := io.ReadAll(io.LimitReader(part, s.mediaMaxBytes+1))
		_ = part.Close()
		if err != nil {
			s.internalError(w, r, "read avatar", err)
			return nil, false
		}
		if int64(len(data)) > s.mediaMaxBytes {
			s.writeMediaError(w, r, domain.ErrMediaTooLarge)
			return nil, false
		}
		if len(data) == 0 {
			s.writeDomainError(w, r, "read avatar",
				domain.NewValidationError("file", "empty_file", "the file is empty"))
			return nil, false
		}
		header := data
		if len(header) > media.SniffLength {
			header = header[:media.SniffLength]
		}
		if _, _, ok := media.DetectContentType(header); !ok {
			s.writeMediaError(w, r, domain.ErrMediaUnsupported)
			return nil, false
		}
		return data, true
	}
	s.writeError(w, r, http.StatusBadRequest, "invalid_request", "The request carries no file")
	return nil, false
}

// deleteAvatarBytes removes the object an account no longer wears, or a file of
// a trip the account took with it, together with any preview rendered of it.
// The row is already gone, so a leftover on disk is worth a log line rather
// than a failed request.
func (s *Server) deleteAvatarBytes(r *http.Request, key string) {
	if key == "" || s.mediaFiles == nil {
		return
	}
	if err := media.DeleteWithPreviews(r.Context(), s.mediaFiles, key); err != nil {
		s.logger.Error("delete avatar file failed", "error", err, "key", key)
	}
}

// handleDeleteMe deletes the signed-in account: the trips it owns go with it,
// with their documents, costs and photographs, and so do its memberships of
// other people's trips and its sessions. What it uploaded into somebody else's
// trip stays there; only the record of who added it is forgotten. There is no
// undo, which is why the interface asks for the word first.
func (s *Server) handleDeleteMe(w http.ResponseWriter, r *http.Request) {
	user := principalFrom(r.Context()).user
	keys, err := s.users.Delete(r.Context(), user.ID)
	if err != nil {
		s.writeDomainError(w, r, "delete account", err)
		return
	}
	for _, key := range keys {
		s.deleteAvatarBytes(r, key)
	}
	w.WriteHeader(http.StatusNoContent)
}
