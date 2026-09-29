package domain

import (
	"strings"
	"testing"
)

// TestNormalizeTagName checks a name is trimmed, its spaces folded, and an
// empty or overlong one refused.
func TestNormalizeTagName(t *testing.T) {
	if name, err := NormalizeTagName("  city   break "); err != nil || name != "city break" {
		t.Errorf("folded: %q %v", name, err)
	}
	if _, err := NormalizeTagName("   "); validationCode(t, err) != "name:required" {
		t.Errorf("empty: %v", err)
	}
	if _, err := NormalizeTagName(strings.Repeat("я", 51)); validationCode(t, err) != "name:too_long" {
		t.Errorf("long: %v", err)
	}
	if _, err := NormalizeTagName(strings.Repeat("я", 50)); err != nil {
		t.Errorf("fifty letters refused: %v", err)
	}
}

// TestValidateTagColor checks only a colour of the palette is taken.
func TestValidateTagColor(t *testing.T) {
	for _, color := range TagColors {
		if err := ValidateTagColor(color); err != nil {
			t.Errorf("%s refused: %v", color, err)
		}
	}
	for _, color := range []TagColor{"", "#ff0000", "Red"} {
		if err := ValidateTagColor(color); validationCode(t, err) != "color:unsupported" {
			t.Errorf("%q: %v", color, err)
		}
	}
}
