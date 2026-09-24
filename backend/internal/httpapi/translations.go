package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
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
