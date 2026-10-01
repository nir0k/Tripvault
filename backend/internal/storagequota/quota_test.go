package storagequota

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
)

// fakeDatabase supplies fixed storage figures to the quota tests.
type fakeDatabase struct {
	settings domain.StorageSettings
	bytes    int64
}

// Settings returns the configured trip quota.
func (f *fakeDatabase) Settings(context.Context) (domain.StorageSettings, error) {
	return f.settings, nil
}

// SetTripQuota changes and returns the configured trip quota.
func (f *fakeDatabase) SetTripQuota(_ context.Context, quota int64) (domain.StorageSettings, error) {
	f.settings.TripQuotaBytes = quota
	return f.settings, nil
}

// DatabaseBytes returns the fixed database-file usage.
func (f *fakeDatabase) DatabaseBytes(context.Context) (int64, error) { return f.bytes, nil }

// TrackBytes reports no existing track in these tests.
func (f *fakeDatabase) TrackBytes(context.Context, uuid.UUID) (int64, error) { return 0, nil }

// TestUsageCountsUserFiles checks originals and fixed idea previews are charged,
// while reproducible trip previews are left out.
func TestUsageCountsUserFiles(t *testing.T) {
	ctx := context.Background()
	files, err := media.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for key, data := range map[string][]byte{
		"trip/photo.jpg":               []byte("photo"),
		"ideas/idea/photo-preview.jpg": []byte("thumb"),
		".previews/v1/trip/photo.jpg":  []byte("generated preview"),
	} {
		if _, err := files.Put(ctx, key, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	database := &fakeDatabase{settings: domain.StorageSettings{TripQuotaBytes: 20}, bytes: 7}
	quota := New(database, files, files, 30)

	usage, err := quota.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if usage.MediaBytes != 10 || usage.DatabaseBytes != 7 || usage.UsedBytes() != 17 ||
		usage.LimitBytes != 30 || usage.TripQuotaBytes != 20 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	if release, err := quota.Reserve(ctx, 14); err != domain.ErrStorageQuota || release != nil {
		t.Fatalf("an oversized reservation returned a release=%t, error=%v", release != nil, err)
	}
	if release, err := quota.Reserve(ctx, 13); err != nil {
		t.Fatalf("a fitting reservation was refused: %v", err)
	} else {
		release()
	}
}

// TestReservationsAreSerial checks concurrent uploads measure usage one after
// another instead of both passing from the same snapshot.
func TestReservationsAreSerial(t *testing.T) {
	ctx := context.Background()
	files, err := media.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	quota := New(&fakeDatabase{}, files, files, 100)
	release, err := quota.Reserve(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		next, err := quota.Reserve(ctx, 1)
		if next != nil {
			next()
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("a second reservation passed the first one: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("the second reservation stayed blocked after release")
	}
}
