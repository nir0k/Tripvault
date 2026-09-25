package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestSortMediaByTimeTaken checks a gallery reads in the order its pictures
// were taken, those without a time following in the order they were uploaded.
func TestSortMediaByTimeTaken(t *testing.T) {
	at := func(hour int) *time.Time {
		moment := time.Date(2026, 6, 20, hour, 0, 0, 0, time.UTC)
		return &moment
	}
	uploaded := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	evening := Media{ID: uuid.New(), TakenAt: at(19), CreatedAt: uploaded}
	morning := Media{ID: uuid.New(), TakenAt: at(8), CreatedAt: uploaded.Add(time.Hour)}
	scanFirst := Media{ID: uuid.New(), CreatedAt: uploaded}
	scanSecond := Media{ID: uuid.New(), CreatedAt: uploaded.Add(time.Minute)}

	items := []Media{scanSecond, evening, scanFirst, morning}
	SortMedia(items)
	want := []uuid.UUID{morning.ID, evening.ID, scanFirst.ID, scanSecond.ID}
	for index, id := range want {
		if items[index].ID != id {
			t.Fatalf("position %d holds %v, want %v", index, items[index].ID, id)
		}
	}
}
