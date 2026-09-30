package media

import (
	"context"
	"errors"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

// previewDirectory is where rendered previews are kept, apart from the uploaded
// files. The leading dot keeps it clear of any storage key, which always begins
// with a trip or an account identifier.
const previewDirectory = ".previews"

// renderedSizes are every width a preview has ever been rendered at, the ones
// no longer offered included. A removal looks for all of them, so a width taken
// out of Sizes does not leave its previews - copies of a photograph - on the
// disk.
var renderedSizes = []int{160, 320, 640, 1280, 1920}

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
	return versionedPreviewKey(key, width, RendererVersion)
}

// versionedPreviewKey names a preview the way the renderer of a version did.
// The first renderer, version 1, did not put its version in the name.
func versionedPreviewKey(key string, width, version int) string {
	name := path.Join(previewDirectory, key) + "@" + strconv.Itoa(width)
	if version > 1 {
		name += ".v" + strconv.Itoa(version)
	}
	return name + ".jpg"
}

// PreviewWidths - lists the widths a picture's previews are rendered at.
//
// A preview is never wider than its picture, so every width from the first one
// that reaches the picture's own width upward would be the same picture again.
// Only that first one is rendered, and it serves the wider ones (ServedWidth).
//
// Arguments:
//   - pictureWidth: the picture's width as it is shown, zero when unknown.
//
// Returns:
//   - the widths to render, narrowest first; all of Sizes for a picture wider
//     than the widest or of unknown width.
func PreviewWidths(pictureWidth int) []int {
	widths := make([]int, 0, len(Sizes))
	for _, size := range Sizes {
		widths = append(widths, size)
		if pictureWidth > 0 && size >= pictureWidth {
			break
		}
	}
	return widths
}

// ServedWidth - names the rendered preview that answers a request for a width.
//
// Arguments:
//   - requested: the width asked for, one of Sizes.
//   - pictureWidth: the picture's width as it is shown, zero when unknown.
//
// Returns:
//   - the requested width, or the widest one rendered when the picture is
//     narrower than what was asked for.
func ServedWidth(requested, pictureWidth int) int {
	widths := PreviewWidths(pictureWidth)
	return min(requested, widths[len(widths)-1])
}

// OfferedSize - names the width a request for a preview width is served at.
//
// A width offered is served as asked. One an older build offered and this one
// no longer does is served at the narrowest width offered above it, so a page
// loaded before an upgrade keeps its pictures rather than getting refusals.
//
// Arguments:
//   - width: the width asked for.
//
// Returns:
//   - the width to serve, and false for a width no build ever offered.
func OfferedSize(width int) (int, bool) {
	if HasSize(width) {
		return width, true
	}
	if !slices.Contains(renderedSizes, width) {
		return 0, false
	}
	for _, size := range Sizes {
		if size >= width {
			return size, true
		}
	}
	return 0, false
}

// previewKeys names every preview any renderer may have made of a file.
func previewKeys(key string) []string {
	keys := make([]string, 0, len(renderedSizes)*RendererVersion)
	for version := 1; version <= RendererVersion; version++ {
		for _, width := range renderedSizes {
			keys = append(keys, versionedPreviewKey(key, width, version))
		}
	}
	return keys
}

// DeleteStalePreviews - removes the previews of a file the service no longer
// serves: those an older renderer made, and those at widths it does not need.
//
// They are called for once the file's current previews are in place, so a disk
// does not keep two sets of copies of every photograph.
//
// Arguments:
//   - ctx: context of the removal.
//   - store: the store holding the file.
//   - key: the storage key of the original file.
//   - keep: the widths whose current previews stay.
//
// Returns:
//   - the errors of every removal that failed, joined; nothing under a key is
//     not an error.
func DeleteStalePreviews(ctx context.Context, store Store, key string, keep []int) error {
	kept := make(map[string]bool, len(keep))
	for _, width := range keep {
		kept[PreviewKey(key, width)] = true
	}
	var errs []error
	for _, preview := range previewKeys(key) {
		if !kept[preview] {
			errs = append(errs, store.Delete(ctx, preview))
		}
	}
	return errors.Join(errs...)
}

// DeleteWithPreviews - removes a file and every preview rendered of it.
//
// A preview is a copy of the photograph, so one left behind would keep a
// deleted picture on the disk; every place that removes a file of a trip goes
// through here. Previews of every renderer and every width go too.
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
	errs := []error{store.Delete(ctx, key), DeleteStalePreviews(ctx, store, key, nil)}
	return errors.Join(errs...)
}

// Leftovers are the objects of a store its records no longer point at.
type Leftovers struct {
	// Originals are objects outside the preview directory nothing names: a
	// file whose row never came to be or whose removal failed.
	Originals []string
	// Previews are previews of no file, of a renderer or width not served.
	Previews []string
	// Files is how many objects outside the preview directory the store holds,
	// named or not.
	Files int
}

// FindLeftovers - walks a store for the objects nothing points at.
//
// Only objects written before the given moment are candidates, so a file
// stored a moment ago whose row is still being written is never among them.
//
// Arguments:
//   - ctx: context of the walk.
//   - store: the store to walk.
//   - pictures: the storage keys of the files previews are rendered of, each
//     with the picture's width as it is shown.
//   - others: the storage keys of the other objects the records name, which
//     have no previews: avatars and the photos of ideas.
//   - before: objects written at or after it are left alone.
//
// Returns:
//   - what the records do not account for.
//   - an error when the store cannot be walked.
func FindLeftovers(ctx context.Context, store Walker, pictures map[string]int, others []string,
	before time.Time) (Leftovers, error) {
	named := make(map[string]bool, len(pictures)+len(others))
	wanted := make(map[string]bool, len(pictures)*len(Sizes))
	for key, width := range pictures {
		named[canonicalKey(key)] = true
		for _, size := range PreviewWidths(width) {
			wanted[canonicalKey(PreviewKey(key, size))] = true
		}
	}
	for _, key := range others {
		named[canonicalKey(key)] = true
	}

	var found Leftovers
	err := store.Walk(ctx, func(object StoredObject) error {
		preview := strings.HasPrefix(object.Key, previewDirectory+"/")
		if !preview {
			found.Files++
		}
		if !object.Modified.Before(before) {
			return nil
		}
		switch {
		case preview && !wanted[object.Key]:
			found.Previews = append(found.Previews, object.Key)
		case !preview && !named[object.Key]:
			found.Originals = append(found.Originals, object.Key)
		}
		return nil
	})
	return found, err
}

// canonicalKey spells a storage key the way a walk over the store reports it:
// the path below the root, without a leading or doubled slash.
func canonicalKey(key string) string {
	return strings.TrimPrefix(path.Clean("/"+strings.TrimSpace(key)), "/")
}
