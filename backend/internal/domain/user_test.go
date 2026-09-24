package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestNormalizeEmail checks the accepted and refused address shapes.
func TestNormalizeEmail(t *testing.T) {
	valid := map[string]string{
		"  Ann@Example.com ":    "ann@example.com",
		"a.b+c@sub.example.org": "a.b+c@sub.example.org",
	}
	for input, want := range valid {
		got, err := NormalizeEmail(input)
		if err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v; want %q", input, got, err, want)
		}
	}

	for _, input := range []string{"", "ann", "@example.com", "ann@example", "ann @example.com", "ann@.com", "ann@example."} {
		_, err := NormalizeEmail(input)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Code != "invalid_email" {
			t.Errorf("NormalizeEmail(%q) error = %v; want invalid_email", input, err)
		}
	}
}

// TestNormalizeDisplayName checks trimming and the length bounds, counted in
// characters rather than bytes so Cyrillic names get the same allowance.
func TestNormalizeDisplayName(t *testing.T) {
	if got, err := NormalizeDisplayName("  Анна  "); err != nil || got != "Анна" {
		t.Fatalf("NormalizeDisplayName = %q, %v", got, err)
	}
	if _, err := NormalizeDisplayName("   "); err == nil {
		t.Error("an empty name was accepted")
	}
	if _, err := NormalizeDisplayName(strings.Repeat("я", 120)); err != nil {
		t.Errorf("120 Cyrillic characters were refused: %v", err)
	}
	if _, err := NormalizeDisplayName(strings.Repeat("я", 121)); err == nil {
		t.Error("121 characters were accepted")
	}
}

// TestProfileValidate checks each profile field is validated.
func TestProfileValidate(t *testing.T) {
	good := Profile{DisplayName: "Ann", Locale: "ru", Theme: ThemeDark, Units: UnitsMiles,
		DateFormat: DateFormatMonthDay, TimeFormat: TimeFormatH12, DefaultCurrency: "ISK"}
	if err := good.Validate(); err != nil {
		t.Fatalf("a valid profile was refused: %v", err)
	}

	// Every case below is good except for the one field it is named after.
	sound := Profile{DisplayName: "Ann", Theme: ThemeAuto, Units: UnitsKilometres,
		DateFormat: DateFormatDayMonth, TimeFormat: TimeFormatH24, DefaultCurrency: "EUR"}
	with := func(change func(*Profile)) Profile {
		profile := sound
		change(&profile)
		return profile
	}
	cases := map[string]Profile{
		"locale":           with(func(p *Profile) { p.Locale = "de" }),
		"theme":            with(func(p *Profile) { p.Theme = "blue" }),
		"units":            with(func(p *Profile) { p.Units = "leagues" }),
		"date_format":      with(func(p *Profile) { p.DateFormat = "ymd" }),
		"time_format":      with(func(p *Profile) { p.TimeFormat = "h6" }),
		"default_currency": with(func(p *Profile) { p.DefaultCurrency = "eur" }),
		"display_name":     with(func(p *Profile) { p.DisplayName = "" }),
	}
	for field, profile := range cases {
		var validation *ValidationError
		if err := profile.Validate(); !errors.As(err, &validation) || validation.Field != field {
			t.Errorf("profile with a bad %s: error = %v", field, err)
		}
	}
}

// TestSessionIsUsable checks expiry and revocation.
func TestSessionIsUsable(t *testing.T) {
	now := time.Now()
	live := Session{ExpiresAt: now.Add(time.Hour)}
	if !live.IsUsable(now) {
		t.Error("a live session is not usable")
	}
	if (Session{ExpiresAt: now}).IsUsable(now) {
		t.Error("an expired session is usable")
	}
	revoked := live
	revoked.RevokedAt = &now
	if revoked.IsUsable(now) {
		t.Error("a revoked session is usable")
	}
}
