package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestShareLinkActive checks a link stops working when it is revoked or expires,
// and keeps working without an end date.
func TestShareLinkActive(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)

	cases := map[string]struct {
		link ShareLink
		want bool
	}{
		"no end date":  {ShareLink{}, true},
		"expires soon": {ShareLink{ExpiresAt: &future}, true},
		"expired":      {ShareLink{ExpiresAt: &past}, false},
		"expires now":  {ShareLink{ExpiresAt: &now}, false},
		"revoked":      {ShareLink{RevokedAt: &past}, false},
		// Revocation wins over an end date still in the future.
		"revoked early": {ShareLink{RevokedAt: &past, ExpiresAt: &future}, false},
	}
	for name, test := range cases {
		if got := test.link.Active(now); got != test.want {
			t.Errorf("%s: active = %v, want %v", name, got, test.want)
		}
	}
}

// TestShareAccessOpens checks a link opens the one document its trip holds.
func TestShareAccessOpens(t *testing.T) {
	id := uuid.New()
	plan := ShareAccess{Trip: TripSummary{Trip: Trip{Kind: DocumentPlan}, PlanID: &id}}
	if !plan.Opens(DocumentPlan) || plan.Opens(DocumentReport) {
		t.Errorf("a plan's link: plan %v, report %v", plan.Opens(DocumentPlan), plan.Opens(DocumentReport))
	}
	report := ShareAccess{Trip: TripSummary{Trip: Trip{Kind: DocumentReport}, ReportID: &id}}
	if report.Opens(DocumentPlan) || !report.Opens(DocumentReport) {
		t.Errorf("a report's link: plan %v, report %v", report.Opens(DocumentPlan), report.Opens(DocumentReport))
	}
}

// TestShareLinkNormalize checks the defaults and the rules a new link must meet.
func TestShareLinkNormalize(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

	link, err := ShareLink{Label: "  For my parents  "}.Normalize(now)
	if err != nil {
		t.Fatalf("a link with only a label: %v", err)
	}
	if link.Label != "For my parents" {
		t.Errorf("the label was not trimmed: %q", link.Label)
	}
	if link.IncludePrivateMedia {
		t.Error("private media are shown by default")
	}

	past := now.Add(-time.Second)
	if _, err := (ShareLink{ExpiresAt: &past}).Normalize(now); validationCode(t, err) != "expires_at:already_past" {
		t.Errorf("an end date in the past: %v", err)
	}
}
