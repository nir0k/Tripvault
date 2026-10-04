package domain

import (
	"strings"
	"testing"
)

// fullPalette returns a palette naming every colour of the interface.
func fullPalette(value string) ThemePalette {
	palette := ThemePalette{}
	for _, name := range ThemeColorNames {
		palette[name] = value
	}
	return palette
}

// TestValidateThemeColor checks hexadecimal colours and colour functions are
// taken, and anything that could leave its declaration in a style sheet is
// not.
func TestValidateThemeColor(t *testing.T) {
	for _, value := range []string{
		"#fff", "#FFFF", "#c2410c", "#c2410c80", "oklch(100% 0 0)", "oklch(25.33% 0.016 252.42)",
		"rgb(12, 34, 56)", "rgba(12 34 56 / 50%)", "hsl(120deg 50% 50%)", "oklch(70% 0.1 none / 0.5)",
	} {
		if !ValidateThemeColor(value) {
			t.Errorf("%q refused", value)
		}
	}
	for _, value := range []string{
		"", "red", "#ff", "#fffff", "var(--x)", "oklch(1 0 0); background: url(x)", "rgb(1 2 3)}",
		"rgb(1 2 (3))", "url(x)", "rgb(\"1\")", "oklch(" + strings.Repeat("1 ", 40) + ")",
	} {
		if ValidateThemeColor(value) {
			t.Errorf("%q taken", value)
		}
	}
}

// TestParseThemeFile checks a file with one palette or both is taken and
// normalised, and every kind of mistake is named.
func TestParseThemeFile(t *testing.T) {
	theme, err := ParseThemeFile(ThemeFile{Format: 1, Name: "  Deep   forest ", Dark: fullPalette(" #ABCDEF ")})
	if err != nil {
		t.Fatalf("dark only: %v", err)
	}
	if theme.Name != "Deep forest" || theme.Light != nil || theme.Dark["primary"] != "#abcdef" {
		t.Errorf("dark only: %+v", theme)
	}
	if _, err := ParseThemeFile(ThemeFile{Format: 1, Name: "Both", Light: fullPalette("#fff"),
		Dark: fullPalette("#000")}); err != nil {
		t.Errorf("both: %v", err)
	}

	missing := fullPalette("#fff")
	delete(missing, "error-content")
	unknown := fullPalette("#fff")
	unknown["link"] = "#fff"
	bad := fullPalette("#fff")
	bad["primary"] = "red"
	cases := map[string]ThemeFile{
		"format:unsupported":           {Format: 2, Name: "x", Light: fullPalette("#fff")},
		"name:required":                {Format: 1, Name: " ", Light: fullPalette("#fff")},
		"name:too_long":                {Format: 1, Name: strings.Repeat("я", 51), Light: fullPalette("#fff")},
		"light:required":               {Format: 1, Name: "x"},
		"light.error-content:required": {Format: 1, Name: "x", Light: missing},
		"dark.link:unsupported":        {Format: 1, Name: "x", Dark: unknown},
		"dark.primary:invalid":         {Format: 1, Name: "x", Dark: bad},
	}
	for want, file := range cases {
		if _, err := ParseThemeFile(file); validationCode(t, err) != want {
			t.Errorf("%s: %v", want, err)
		}
	}
}
