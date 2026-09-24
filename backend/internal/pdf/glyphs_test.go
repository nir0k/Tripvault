package pdf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

// A character the embedded typeface does not have draws as an empty box, and
// nothing reports it: the document is written, looks finished, and has a hole in
// it where the rating was. This reads the fonts and checks every character the
// renderer writes on its own account, so the next one added is caught here
// rather than in somebody's printed report.

// TestTheFontsHaveEveryMarkTheDocumentDraws checks the decoration and the two
// alphabets against all three embedded faces.
func TestTheFontsHaveEveryMarkTheDocumentDraws(t *testing.T) {
	marks := []rune(bulletMark + arrowMark + separator + "–—…«»“”'’")

	// One letter from each script the interface is written in, plus the
	// punctuation a Russian page needs.
	letters := []rune("AZaz09ЖщЁё")

	fonts := map[string][]byte{
		"regular": fontRegular,
		"bold":    fontBold,
		"italic":  fontItalic,
	}
	for name, font := range fonts {
		covered, err := coverage(font)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, r := range append(marks, letters...) {
			if !covered[r] {
				t.Errorf("%s has no glyph for %q (U+%04X)", name, r, r)
			}
		}
	}
}

// coverage reads the characters a TrueType font can draw.
//
// It reads the format 4 subtable of the font's cmap, which is the one every
// font for the Basic Multilingual Plane carries; the characters this document
// uses are all in it.
func coverage(font []byte) (map[rune]bool, error) {
	cmap, err := tableOffset(font, "cmap")
	if err != nil {
		return nil, err
	}

	subtables := int(binary.BigEndian.Uint16(font[cmap+2:]))
	for i := range subtables {
		record := cmap + 4 + 8*i
		offset := int(binary.BigEndian.Uint32(font[record+4:]))
		subtable := cmap + offset
		if binary.BigEndian.Uint16(font[subtable:]) != 4 {
			continue
		}
		return format4(font, subtable)
	}
	return nil, errors.New("the font has no format 4 character map")
}

// tableOffset finds one table of a TrueType font by its tag.
func tableOffset(font []byte, tag string) (int, error) {
	tables := int(binary.BigEndian.Uint16(font[4:]))
	for i := range tables {
		record := 12 + 16*i
		if string(font[record:record+4]) == tag {
			return int(binary.BigEndian.Uint32(font[record+8:])), nil
		}
	}
	return 0, fmt.Errorf("the font has no %s table", tag)
}

// format4 reads the segments of a format 4 character map.
func format4(font []byte, subtable int) (map[rune]bool, error) {
	segments := int(binary.BigEndian.Uint16(font[subtable+6:])) / 2
	ends := subtable + 14
	starts := ends + segments*2 + 2

	covered := make(map[rune]bool)
	for i := range segments {
		first := rune(binary.BigEndian.Uint16(font[starts+i*2:]))
		last := rune(binary.BigEndian.Uint16(font[ends+i*2:]))
		// 0xFFFF closes the table rather than naming a character.
		if first > last || last == 0xFFFF {
			continue
		}
		for r := first; r <= last; r++ {
			covered[r] = true
		}
	}
	if len(covered) == 0 {
		return nil, errors.New("the character map is empty")
	}
	return covered, nil
}
