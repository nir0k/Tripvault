package httpapi

import (
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/translationfile"
)

// maxTranslationsPerRequest bounds one save. The editor sends one field at a
// time; the bound only keeps a single request from rewriting a report wholesale.
const maxTranslationsPerRequest = 500

// translationRequest is one translated field of PUT
// /api/v1/documents/{documentID}/translations/{lang}.
type translationRequest struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Field      string `json:"field"`
	// Value is the translated text; an empty one removes the translation.
	Value string `json:"value"`
}

// saveTranslationsRequest is the body of PUT
// /api/v1/documents/{documentID}/translations/{lang}.
type saveTranslationsRequest struct {
	Translations []translationRequest `json:"translations"`
}

// parseTranslations reads and validates the fields of a save.
func parseTranslations(body saveTranslationsRequest, language string) ([]domain.Translation, error) {
	if len(body.Translations) > maxTranslationsPerRequest {
		return nil, domain.NewValidationError("translations", "too_many", "must hold at most 500 fields")
	}
	translations := make([]domain.Translation, 0, len(body.Translations))
	for _, entry := range body.Translations {
		targetID, err := uuid.Parse(entry.TargetID)
		if err != nil {
			return nil, domain.NewValidationError("target_id", "unknown_target", "is not an element of this report")
		}
		translation, err := domain.Translation{
			Target:   domain.TranslationTarget(entry.TargetType),
			TargetID: targetID,
			Field:    entry.Field,
			Lang:     language,
			Value:    entry.Value,
		}.Normalize()
		if err != nil {
			return nil, err
		}
		translations = append(translations, translation)
	}
	return translations, nil
}

// handleSaveTranslations writes a report's words in one of the languages it is
// translated into. Whoever may edit the report may translate it. The answer is
// the whole document, as after every other change to it; a translation of the
// trip's own title or summary is read back with the trip.
func (s *Server) handleSaveTranslations(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentFor(w, r, documentID, domain.ActionEdit); !ok {
		return
	}
	var body saveTranslationsRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	language := chi.URLParam(r, "lang")
	translations, err := parseTranslations(body, language)
	if err != nil {
		s.writeDomainError(w, r, "validate translations", err)
		return
	}
	if err := s.documents.SaveTranslations(r.Context(), documentID, language, translations); err != nil {
		s.writeDomainError(w, r, "save translations", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, documentID)
}

// translationFileResponse says what a translation file brought.
type translationFileResponse struct {
	// Saved counts the fields written, the emptied ones included.
	Saved int `json:"saved"`
	// Skipped counts entries naming an element the report no longer has.
	Skipped int `json:"skipped"`
}

// translatableReport loads the report named in the path for its editor, and
// checks that the language in the path is one it is translated into.
func (s *Server) translatableReport(w http.ResponseWriter, r *http.Request) (domain.TripSummary, domain.DocumentContent,
	string, bool) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return domain.TripSummary{}, domain.DocumentContent{}, "", false
	}
	document, err := s.documents.Document(r.Context(), documentID)
	if err != nil {
		s.writeDomainError(w, r, "get document", err)
		return domain.TripSummary{}, domain.DocumentContent{}, "", false
	}
	trip, ok := s.tripAccess(w, r, document.TripID, domain.ActionEdit)
	if !ok {
		return domain.TripSummary{}, domain.DocumentContent{}, "", false
	}
	language := chi.URLParam(r, "lang")
	if document.Kind != domain.DocumentReport || len(trip.Languages) < 2 ||
		!slices.Contains(trip.Languages[1:], language) {
		s.writeDomainError(w, r, "check translation language", domain.NewValidationError("lang", "unknown_language",
			"must be a language the report is translated into"))
		return domain.TripSummary{}, domain.DocumentContent{}, "", false
	}
	content, err := s.documents.Content(r.Context(), documentID)
	if err != nil {
		s.writeDomainError(w, r, "read document", err)
		return domain.TripSummary{}, domain.DocumentContent{}, "", false
	}
	return trip, content, language, true
}

// handleGetTranslationFile writes the words of a report into a YAML file for
// one of the languages it is translated into, each beside the translation it
// has so far, for a person or a model to fill in and bring back.
func (s *Server) handleGetTranslationFile(w http.ResponseWriter, r *http.Request) {
	trip, content, language, ok := s.translatableReport(w, r)
	if !ok {
		return
	}
	file, err := translationfile.Build(trip, content, language)
	if err != nil {
		s.internalError(w, r, "write translation file", err)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", attachment(translationFileName(trip.Title, language)))
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(file); err != nil {
		s.logger.Warn("send translation file failed", "error", err)
	}
}

// handlePutTranslationFile reads the translations out of a file written by
// handleGetTranslationFile and saves them all at once: every field is checked
// before anything is written, an empty translation removes one, and an entry
// naming an element the report no longer has is skipped and counted.
func (s *Server) handlePutTranslationFile(w http.ResponseWriter, r *http.Request) {
	trip, content, language, ok := s.translatableReport(w, r)
	if !ok {
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, translationfile.MaxFileBytes))
	if err != nil {
		s.writeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "The file is too large")
		return
	}
	read, err := translationfile.Read(data, trip, content, language)
	switch {
	case errors.Is(err, translationfile.ErrLanguage):
		s.writeDomainError(w, r, "read translation file", domain.NewValidationError("file", "wrong_language",
			"the file translates into another language"))
		return
	case err != nil:
		s.writeDomainError(w, r, "read translation file", domain.NewValidationError("file", "invalid_file",
			"is not a translation file of this service"))
		return
	}
	translations := make([]domain.Translation, 0, len(read.Translations))
	for _, translation := range read.Translations {
		normalized, err := translation.Normalize()
		if err != nil {
			s.writeDomainError(w, r, "validate translation file", err)
			return
		}
		translations = append(translations, normalized)
	}
	if err := s.documents.SaveTranslations(r.Context(), content.Document.ID, language, translations); err != nil {
		s.writeDomainError(w, r, "save translations", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, translationFileResponse{Saved: len(translations), Skipped: read.Skipped})
}

// translationFileName is the name a report's translation file is saved under,
// such as "Iceland.ru.yaml".
func translationFileName(title, language string) string {
	return fileTitle(title) + "." + language + ".yaml"
}
