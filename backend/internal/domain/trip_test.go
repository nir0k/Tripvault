package domain

import (
	"errors"
	"testing"
	"time"
)

// date builds a calendar date for the tests.
func date(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := ParseDate("date", value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return &parsed
}

// validationCode returns the reason of a validation error, or "" for nil.
func validationCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("%v is not a validation error", err)
	}
	return validation.Field + ":" + validation.Code
}

// TestRolePermissions pins the role table of the specification.
func TestRolePermissions(t *testing.T) {
	actions := []TripAction{ActionView, ActionEdit, ActionManageMembers, ActionManageShareLinks, ActionDelete}
	want := map[TripRole][]bool{
		RoleOwner:  {true, true, true, true, true},
		RoleEditor: {true, true, false, false, false},
		RoleViewer: {true, false, false, false, false},
		"":         {false, false, false, false, false},
	}
	for role, allowed := range want {
		for i, action := range actions {
			if got := role.Can(action); got != allowed[i] {
				t.Errorf("role %q action %d: got %v, want %v", role, action, got, allowed[i])
			}
		}
	}
	if ValidateMemberRole(RoleOwner) == nil {
		t.Error("the owner role can be given to a member")
	}
}

// TestTripNormalize checks defaults and the rules on every field.
func TestTripNormalize(t *testing.T) {
	valid := Trip{Kind: DocumentPlan, Title: "  Iceland  ", Currency: "isk", Travelers: 2,
		StartDate: date(t, "2026-06-20"), EndDate: date(t, "2026-06-27")}
	normalized, err := valid.Normalize()
	if err != nil {
		t.Fatalf("valid trip refused: %v", err)
	}
	if normalized.Title != "Iceland" || normalized.Currency != "ISK" || normalized.Timezone != "UTC" {
		t.Errorf("unexpected normalisation: %+v", normalized)
	}

	cases := map[string]struct {
		change func(*Trip)
		want   string
	}{
		"no kind":       {func(t *Trip) { t.Kind = "" }, "kind:unsupported"},
		"a report":      {func(t *Trip) { t.Kind = DocumentReport }, ""},
		"no title":      {func(t *Trip) { t.Title = " " }, "title:required"},
		"no start":      {func(tr *Trip) { tr.StartDate = nil }, "start_date:required"},
		"no end":        {func(tr *Trip) { tr.EndDate = nil }, "end_date:required"},
		"reversed":      {func(tr *Trip) { tr.StartDate, tr.EndDate = date(t, "2026-06-20"), date(t, "2026-06-19") }, "end_date:end_before_start"},
		"too long":      {func(tr *Trip) { tr.StartDate, tr.EndDate = date(t, "2026-01-01"), date(t, "2027-01-02") }, "end_date:trip_too_long"},
		"bad zone":      {func(t *Trip) { t.Timezone = "Mars/Olympus" }, "timezone:invalid_timezone"},
		"local zone":    {func(t *Trip) { t.Timezone = "Local" }, "timezone:invalid_timezone"},
		"bad currency":  {func(t *Trip) { t.Currency = "euro" }, "currency:invalid_currency"},
		"no travelers":  {func(t *Trip) { t.Travelers = 0 }, "travelers:out_of_range"},
		"one-day trip":  {func(tr *Trip) { tr.StartDate, tr.EndDate = date(t, "2026-06-20"), date(t, "2026-06-20") }, ""},
		"leap year max": {func(tr *Trip) { tr.StartDate, tr.EndDate = date(t, "2028-01-01"), date(t, "2028-12-31") }, ""},
	}
	for name, tc := range cases {
		trip := valid
		tc.change(&trip)
		_, err := trip.Normalize()
		if got := validationCode(t, err); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

// TestTripStatus checks the status follows the dates in the trip's own zone.
func TestTripStatus(t *testing.T) {
	trip := Trip{StartDate: date(t, "2026-06-20"), EndDate: date(t, "2026-06-27"), Timezone: "Atlantic/Reykjavik"}

	cases := map[string]TripStatus{
		"2026-06-19T23:59:00Z": StatusUpcoming,
		"2026-06-20T00:00:00Z": StatusOngoing,
		"2026-06-27T23:59:00Z": StatusOngoing,
		"2026-06-28T00:00:00Z": StatusCompleted,
	}
	for instant, want := range cases {
		now, _ := time.Parse(time.RFC3339, instant)
		if got := trip.StatusAt(now); got != want {
			t.Errorf("%s: got %s, want %s", instant, got, want)
		}
	}

	// 23:30 UTC on the eve is already the first day in Tokyo.
	trip.Timezone = "Asia/Tokyo"
	now, _ := time.Parse(time.RFC3339, "2026-06-19T23:30:00Z")
	if got := trip.StatusAt(now); got != StatusOngoing {
		t.Errorf("tokyo: got %s", got)
	}

	if got := (Trip{}).StatusAt(now); got != StatusNoDates {
		t.Errorf("no dates: got %s", got)
	}
	if days := trip.DayCount(); days == nil || *days != 8 {
		t.Errorf("day count: %v", days)
	}
}

// TestParseMoney checks amounts are read exactly and malformed ones refused.
func TestParseMoney(t *testing.T) {
	valid := map[string]Money{"0": 0, "85": 8500, "1240.5": 124050, "0.07": 7, "999999999999.99": 99_999_999_999_999}
	for input, want := range valid {
		got, err := ParseMoney("amount", input)
		if err != nil || got != want {
			t.Errorf("%q: got %d, %v; want %d", input, got, err, want)
		}
	}
	if got := Money(124050).String(); got != "1240.50" {
		t.Errorf("format: %s", got)
	}
	for _, input := range []string{"", "-1", "1.234", "1.", ".5", "1,5", "1e3", "1000000000000", "12 3"} {
		if _, err := ParseMoney("amount", input); validationCode(t, err) != "amount:invalid_amount" {
			t.Errorf("%q accepted", input)
		}
	}
}

// TestTripFilterNormalize checks defaults and page-size clamping.
func TestTripFilterNormalize(t *testing.T) {
	filter, err := TripFilter{Limit: 5000}.Normalize()
	if err != nil || filter.Scope != ScopeAll || filter.Sort != SortRelevance || filter.Limit != MaxPageSize {
		t.Errorf("defaults: %+v %v", filter, err)
	}
	if _, err := (TripFilter{Scope: "mine"}).Normalize(); validationCode(t, err) != "scope:unsupported" {
		t.Errorf("bad scope accepted: %v", err)
	}
	if _, err := (TripFilter{Kind: "budget"}).Normalize(); validationCode(t, err) != "kind:unsupported" {
		t.Errorf("bad kind accepted: %v", err)
	}
	if _, err := (TripFilter{Sort: "title"}).Normalize(); validationCode(t, err) != "sort:unsupported" {
		t.Errorf("bad sort accepted: %v", err)
	}
}
