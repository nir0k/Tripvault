package postgres

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// translationTargets maps each kind of translated element onto the column that
// names it in translations and the table it lives in. The trip itself has
// neither: its rows name only trip_id.
var translationTargets = map[domain.TranslationTarget]struct {
	column string
	table  string
}{
	domain.TranslateDocument: {column: "document_id", table: "documents"},
	domain.TranslateDay:      {column: "day_id", table: "days"},
	domain.TranslateStay:     {column: "stay_id", table: "stays"},
	domain.TranslateTransfer: {column: "transfer_id", table: "transfers"},
	// A stay mark shows its stay's name and has no words of its own.
	domain.TranslateItem: {column: "item_id", table: "items"},
	domain.TranslateLeg:  {column: "leg_id", table: "legs"},
}

// translationKey is the expression the unique index of translations is built
// on: the element translated, or the trip when the row names no element.
const translationKey = `COALESCE(document_id, day_id, stay_id, item_id, leg_id, transfer_id, trip_id)`

// documentTranslations reads the translations of a report's elements, leaving
// out the trip's own title and summary, which travel with the trip.
func documentTranslations(ctx context.Context, q querier, tripID uuid.UUID) ([]domain.Translation, error) {
	return collect(ctx, q, func(row pgx.Row) (domain.Translation, error) {
		var t domain.Translation
		err := row.Scan(&t.Target, &t.TargetID, &t.Field, &t.Lang, &t.Value)
		return t, err
	},
		`SELECT CASE WHEN document_id IS NOT NULL THEN 'document'
		             WHEN day_id IS NOT NULL THEN 'day'
		             WHEN stay_id IS NOT NULL THEN 'stay'
		             WHEN transfer_id IS NOT NULL THEN 'transfer'
		             WHEN item_id IS NOT NULL THEN 'item'
		             ELSE 'leg' END,
		        `+translationKey+`, field, lang, value
		 FROM translations
		 WHERE trip_id = $1 AND num_nonnulls(document_id, day_id, stay_id, item_id, leg_id, transfer_id) = 1
		 ORDER BY lang, field`, tripID)
}

// SaveTranslations - writes the translations of a report's words into one of
// its further languages. A translation with an empty value is removed, so the
// field is read in the original again.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - documentID: the report.
//   - language: the language translated into.
//   - translations: the validated translations, each naming a field of the
//     report or of one of its elements.
//
// Returns:
//   - domain.ErrNotFound when the document does not exist.
//   - a *domain.ValidationError when the document is a plan, the language is
//     not one the report is translated into, or an element is not the report's.
func (r *DocumentRepository) SaveTranslations(ctx context.Context, documentID uuid.UUID, language string,
	translations []domain.Translation) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, documentID)
		if err != nil {
			return err
		}
		if document.Kind != domain.DocumentReport {
			return domain.NewValidationError("document", "report_only", "only a report is translated")
		}
		var languages []string
		if err := tx.QueryRow(ctx, `SELECT languages FROM trips WHERE id = $1`, document.TripID).
			Scan(&languages); err != nil {
			return fmt.Errorf("read report languages: %w", err)
		}
		if len(languages) < 2 || !slices.Contains(languages[1:], language) {
			return domain.NewValidationError("lang", "not_a_translation", "is not a language the report is translated into")
		}

		for _, translation := range translations {
			if err := checkTranslationTarget(ctx, tx, document, translation); err != nil {
				return err
			}
			if err := writeTranslation(ctx, tx, document.TripID, language, translation); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET updated_at = now() WHERE id = $1`, documentID); err != nil {
			return fmt.Errorf("touch document: %w", err)
		}
		return nil
	})
}

// checkTranslationTarget refuses a translation of anything that is not the
// report itself or one of its elements.
func checkTranslationTarget(ctx context.Context, tx pgx.Tx, document domain.Document,
	translation domain.Translation) error {
	unknown := domain.NewValidationError("target_id", "unknown_target", "is not an element of this report")
	switch translation.Target {
	case domain.TranslateTrip:
		if translation.TargetID != document.TripID {
			return unknown
		}
		return nil
	case domain.TranslateDocument:
		if translation.TargetID != document.ID {
			return unknown
		}
		return nil
	}

	target := translationTargets[translation.Target]
	condition := ""
	if translation.Target == domain.TranslateItem {
		condition = " AND kind <> 'stay_anchor'"
	}
	var found bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM `+target.table+` WHERE id = $1 AND document_id = $2`+condition+`)`,
		translation.TargetID, document.ID).Scan(&found); err != nil {
		return fmt.Errorf("check translated element: %w", err)
	}
	if !found {
		return unknown
	}
	return nil
}

// writeTranslation stores one translated field, or removes it when the value
// is empty.
func writeTranslation(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, language string,
	translation domain.Translation) error {
	if translation.Value == "" {
		if _, err := tx.Exec(ctx,
			`DELETE FROM translations
			 WHERE trip_id = $1 AND `+translationKey+` = $2 AND field = $3 AND lang = $4`,
			tripID, translation.TargetID, translation.Field, language); err != nil {
			return fmt.Errorf("remove translation: %w", err)
		}
		return nil
	}

	// The trip's own row names no element: trip_id already says which it is.
	columns, values := "trip_id, field, lang, value", "$1, $2, $3, $4"
	args := []any{tripID, translation.Field, language, translation.Value}
	if target, ok := translationTargets[translation.Target]; ok {
		columns += ", " + target.column
		values += ", $5"
		args = append(args, translation.TargetID)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO translations (`+columns+`) VALUES (`+values+`)
		 ON CONFLICT ((`+translationKey+`), field, lang) DO UPDATE SET value = EXCLUDED.value`,
		args...); err != nil {
		return fmt.Errorf("write translation: %w", err)
	}
	return nil
}
