package domain

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestAttachmentName checks the name a browser sends is reduced to the last part
// of a path without control characters, shortened before its extension, and
// refused when nothing is left of it.
func TestAttachmentName(t *testing.T) {
	cases := map[string]string{
		"ticket.pdf":                       "ticket.pdf",
		"  Паром Берген.pdf  ":             "Паром Берген.pdf",
		`C:\Users\me\Downloads\ticket.pdf`: "ticket.pdf",
		"/home/me/ticket.pdf":              "ticket.pdf",
		"tick\x00et\r\n.pdf":               "ticket.pdf",
	}
	for input, want := range cases {
		got, err := AttachmentName(input)
		if err != nil || got != want {
			t.Errorf("AttachmentName(%q) = %q, %v; want %q", input, got, err, want)
		}
	}

	long, err := AttachmentName(strings.Repeat("я", 300) + ".docx")
	if err != nil {
		t.Fatalf("a long name was refused: %v", err)
	}
	if utf8.RuneCountInString(long) != maxOriginalNameLength || !strings.HasSuffix(long, ".docx") {
		t.Errorf("a long name became %d characters ending %q", utf8.RuneCountInString(long), long[len(long)-8:])
	}

	for _, input := range []string{"", "   ", "folder/", "..", "\x00"} {
		if _, err := AttachmentName(input); !errors.Is(err, ErrAttachmentName) {
			t.Errorf("AttachmentName(%q) gave %v", input, err)
		}
	}
}
