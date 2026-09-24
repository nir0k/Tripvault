package media

import (
	"context"
	"errors"
	"path"
	"strconv"
)

// previewDirectory is where rendered previews are kept, apart from the uploaded
// files. The leading dot keeps it clear of any storage key, which always begins
// with a trip or an account identifier.
const previewDirectory = ".previews"

// PreviewKey - names the object a rendered preview of a file is kept under.
//
// A preview is derived from its file alone, so it is kept beside the store's
// objects and made once rather than decoded from the original on every first
// view. It is not a record of the service: backups copy only the files the
// catalogue lists, and a missing preview is simply rendered again.
//
// Arguments:
//   - key: the storage key of the original file.
//   - width: the preview width, one of Sizes.
//
// Returns:
//   - the storage key of that preview.
func PreviewKey(key string, width int) string {
	return path.Join(previewDirectory, key) + "@" + strconv.Itoa(width) + ".jpg"
}

// DeleteWithPreviews - removes a file and every preview rendered of it.
//
// A preview is a copy of the photograph, so one left behind would keep a
// deleted picture on the disk; every place that removes a file of a trip goes
// through here.
//
// Arguments:
//   - ctx: context of the removal.
//   - store: the store holding the file.
//   - key: the storage key of the original file.
//
// Returns:
//   - the errors of every removal that failed, joined; nothing under a key is
//     not an error.
func DeleteWithPreviews(ctx context.Context, store Store, key string) error {
	errs := []error{store.Delete(ctx, key)}
	for _, width := range Sizes {
		errs = append(errs, store.Delete(ctx, PreviewKey(key, width)))
	}
	return errors.Join(errs...)
}
