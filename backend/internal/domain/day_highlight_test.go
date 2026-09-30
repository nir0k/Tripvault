package domain

import (
	"strings"
	"testing"
)

// TestDayHighlight checks the moment a day is remembered by is folded into one
// line, bounded at 200 characters and refused on a plan's day.
func TestDayHighlight(t *testing.T) {
	day, err := Day{Highlight: "  Whales breaching,\n  twice  "}.Normalize()
	if err != nil || day.Highlight != "Whales breaching, twice" {
		t.Errorf("normalised to %q, %v", day.Highlight, err)
	}
	if _, err := (Day{Highlight: strings.Repeat("ы", MaxDayHighlight)}).Normalize(); err != nil {
		t.Errorf("a highlight of exactly 200 characters: %v", err)
	}
	if _, err := (Day{Highlight: strings.Repeat("ы", MaxDayHighlight+1)}).Normalize(); err == nil {
		t.Error("a highlight of 201 characters was accepted")
	}
	if err := day.CheckKind(DocumentPlan); err == nil {
		t.Error("a plan's day took a highlight")
	}
	if err := day.CheckKind(DocumentReport); err != nil {
		t.Errorf("a report's day refused its highlight: %v", err)
	}
	if err := (Day{}).CheckKind(DocumentPlan); err != nil {
		t.Errorf("a plan's day without a highlight was refused: %v", err)
	}
}
