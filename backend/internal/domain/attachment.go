package domain

import (
	"errors"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// An attachment is a file a place or an activity carries besides its
// pictures: a ticket, a booking, a timetable. It is not a picture of the trip -
// it never reaches a gallery, a cover or a read-only link - and it belongs to
// the one place it was attached to, in its plan: copying the
// place, the day or the plan into a report leaves it behind, since the tickets
// of one group are nobody else's. The file is kept with its row and leaves with
// the place.

// MaxItemAttachments is how many files one place carries. A place is a ticket
// or two and a booking, not a folder.
const MaxItemAttachments = 10

// MaxAttachmentDescription is how many characters the line describing an
// attachment may take: enough to say what a file is, not a note.
const MaxAttachmentDescription = 150

// Errors the attachment rules report.
var (
	// ErrAttachmentLimit reports a place that already carries as many files as
	// a place may.
	ErrAttachmentLimit = NewValidationError("file", "attachment_limit",
		"a place carries at most ten attachments")
	// ErrAttachmentDuplicate reports a file the place already carries.
	ErrAttachmentDuplicate = errors.New("the place already carries this file")
	// ErrAttachmentName reports an upload whose name says nothing, which leaves
	// no way to tell what kind of text it is nor to save it again.
	ErrAttachmentName = NewValidationError("file", "name_required", "the file must be sent with its name")
)

// Attachment is one file attached to a place, without its bytes.
type Attachment struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	ItemID     uuid.UUID
	// OriginalName is the name the file was uploaded under, the last part of
	// it only.
	OriginalName string
	// Description is a line on what the file is; empty when nobody wrote one.
	Description string
	// MIME is what the bytes were recognised as.
	MIME string
	Size int64
	// Checksum is the SHA-256 of the bytes.
	Checksum []byte
	// UploadedBy is nil once that account has been deleted.
	UploadedBy *uuid.UUID
	CreatedAt  time.Time
}

// AttachmentFile is an attachment's name, type and bytes, for downloading.
type AttachmentFile struct {
	Name string
	MIME string
	Data []byte
}

// AttachmentName - reduces the name a browser sent with a file to one that is
// safe to keep and to offer back as a download.
//
// A browser may send a whole path, and a name may carry characters that would
// break a header; only the last part of the path is kept, without control
// characters, and a name too long is shortened before its extension, which
// says what kind of file it is.
//
// Arguments:
//   - name: the name as uploaded.
//
// Returns:
//   - the name to keep.
//   - ErrAttachmentName when nothing is left of it.
func AttachmentName(name string) (string, error) {
	if index := strings.LastIndexAny(name, `/\`); index >= 0 {
		name = name[index+1:]
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", ErrAttachmentName
	}
	if utf8.RuneCountInString(name) > maxOriginalNameLength {
		extension := path.Ext(name)
		if utf8.RuneCountInString(extension) > 16 {
			extension = ""
		}
		stem := []rune(strings.TrimSuffix(name, extension))
		name = string(stem[:maxOriginalNameLength-utf8.RuneCountInString(extension)]) + extension
	}
	return name, nil
}

// AttachmentDescription - tidies the line describing an attachment.
//
// It is one line: breaks are turned into spaces and runs of spaces into one,
// so a description pasted from a mail reads as it would on the card.
//
// Arguments:
//   - value: the description as sent.
//
// Returns:
//   - the description to keep, empty for none.
//   - a *ValidationError when it is longer than MaxAttachmentDescription.
func AttachmentDescription(value string) (string, error) {
	value = strings.Join(strings.Fields(value), " ")
	return value, checkLength("description", value, MaxAttachmentDescription)
}

// AttachmentsOfItem - picks the files one place carries, in the order given.
//
// Arguments:
//   - attachments: the attachments of a document.
//   - itemID: the place.
//
// Returns:
//   - its attachments; empty rather than nil, so the API lists none as [].
func AttachmentsOfItem(attachments []Attachment, itemID uuid.UUID) []Attachment {
	out := []Attachment{}
	for _, attachment := range attachments {
		if attachment.ItemID == itemID {
			out = append(out, attachment)
		}
	}
	return out
}
