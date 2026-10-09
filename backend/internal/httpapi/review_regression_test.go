package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestSharedJPEGWithoutCatalogueCoordinatesScrubsXMP exercises original reads,
// single downloads and ZIPs, rather than only calling the metadata scrubber.
func TestSharedJPEGWithoutCatalogueCoordinatesScrubsXMP(t *testing.T) {
	s, trips, catalogue, _ := newMediaServer(domain.RoleOwner)
	s.users = fakeUsers{zones: domain.PrivacyZones{{Center: domain.Point{Lat: 40, Lng: 20}, RadiusM: 500}}}
	trips.shares = map[string]domain.ShareLink{}
	picture := samplePicture(t, 100)
	ids := make([]string, 0, 2)
	for _, name := range []string{"one", "two"} {
		payload := []byte("http://ns.adobe.com/xap/1.0/\x00GPSLatitude=40;GPSLongitude=20;" + name)
		var image bytes.Buffer
		image.Write(picture[:2])
		image.Write([]byte{0xff, 0xe1})
		_ = binary.Write(&image, binary.BigEndian, uint16(len(payload)+2))
		image.Write(payload)
		image.Write(picture[2:])
		stored := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), name+".jpg", image.Bytes(), false))[0]
		item := catalogue.items[uuid.MustParse(stored.ID)]
		if item.Lat != nil || item.Lng != nil {
			t.Fatal("fixture unexpectedly has catalogue coordinates")
		}
		ids = append(ids, stored.ID)
	}
	link := createLink(t, s, trips.trip.ID.String(), `{"allow_download":true}`)
	original := sendShared(s, "/api/v1/shared/media/"+ids[0], link.Token)
	if original.Code != 200 || bytes.Contains(original.Body.Bytes(), []byte("GPSLatitude")) {
		t.Fatal("shared original leaked XMP")
	}
	for _, chosen := range [][]string{ids[:1], ids} {
		body := `{"media_ids":["` + chosen[0] + `"`
		if len(chosen) > 1 {
			body += `,"` + chosen[1] + `"`
		}
		body += `]}`
		address, cookie := issueDownload(t, sendSharedBody(s, http.MethodPost, "/api/v1/shared/media:download", link.Token, body))
		download := fetchDownload(s, address, cookie, "")
		if download.Code != 200 {
			t.Fatalf("download status %d", download.Code)
		}
		if len(chosen) == 1 {
			if bytes.Contains(download.Body.Bytes(), []byte("GPSLatitude")) {
				t.Fatal("single download leaked XMP")
			}
		} else {
			for _, file := range readArchive(t, download) {
				if bytes.Contains(file, []byte("GPSLatitude")) {
					t.Fatal("archive leaked XMP")
				}
			}
		}
	}
}

// pendingDocuments places many unrelated pending legs before the requested one.
type pendingDocuments struct{ *fakeDocuments }

// Content keeps the ordinary fake's target leg at the end of a full batch.
func (d pendingDocuments) Content(ctx context.Context, id uuid.UUID) (domain.DocumentContent, error) {
	content, err := d.fakeDocuments.Content(ctx, id)
	legs := make([]domain.Leg, 0, maxLegsPerCalculation+1)
	for range maxLegsPerCalculation {
		leg := d.leg
		leg.ID = uuid.New()
		legs = append(legs, leg)
	}
	content.Legs = append(legs, d.leg)
	return content, err
}

// TestExplicitRecalculationDoesNotSpendItsBatchElsewhere ensures a selected leg
// is calculated even when the document holds more than one batch of pending work.
func TestExplicitRecalculationDoesNotSpendItsBatchElsewhere(t *testing.T) {
	s, documents, router := newRoutingServer(domain.RoleEditor)
	s.documents = pendingDocuments{documents}
	response := send(s, http.MethodPost, "/api/v1/legs/"+documents.leg.ID.String()+":recalculate", "good", "")
	if response.Code != 200 || router.calculated != 1 || documents.leg.Source != domain.LegProvider {
		t.Fatalf("selected leg was skipped: status=%d calls=%d source=%s", response.Code, router.calculated, documents.leg.Source)
	}
}
