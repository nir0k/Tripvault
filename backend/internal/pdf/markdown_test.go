package pdf

import "testing"

// TestParseMarkdownReadsTheBlocksAStoryIsMadeOf covers the shapes a travel diary
// is actually written in, together, because they interact: a list after a
// paragraph, a heading between two of them.
func TestParseMarkdownReadsTheBlocksAStoryIsMadeOf(t *testing.T) {
	blocks := parseMarkdown(`## The south coast

We left before dawn
and drove east.

- black sand
- a waterfall

1. breakfast
2) the road

> It rained the whole way.`)

	want := []block{
		{kind: blockHeading, level: 2, text: "The south coast"},
		{kind: blockParagraph, text: "We left before dawn and drove east."},
		{kind: blockBullet, text: "black sand"},
		{kind: blockBullet, text: "a waterfall"},
		{kind: blockNumber, text: "1. breakfast"},
		{kind: blockNumber, text: "2. the road"},
		{kind: blockQuote, text: "It rained the whole way."},
	}

	if len(blocks) != len(want) {
		t.Fatalf("got %d blocks, want %d: %+v", len(blocks), len(want), blocks)
	}
	for i, block := range blocks {
		if block != want[i] {
			t.Errorf("block %d is %+v, want %+v", i, block, want[i])
		}
	}
}

// TestParseMarkdownJoinsWrappedLines checks a paragraph typed across several
// lines comes out as one, and that a blank line starts another.
func TestParseMarkdownJoinsWrappedLines(t *testing.T) {
	blocks := parseMarkdown("one\ntwo\n\nthree")
	if len(blocks) != 2 || blocks[0].text != "one two" || blocks[1].text != "three" {
		t.Errorf("got %+v", blocks)
	}
}

// TestParseMarkdownKeepsFencedTextAsItWasTyped checks a fenced block is not
// read as prose, so a line of it is not joined onto its neighbour.
func TestParseMarkdownKeepsFencedTextAsItWasTyped(t *testing.T) {
	blocks := parseMarkdown("```\n  bus 14\n  bus 21\n```")
	if len(blocks) != 2 || blocks[0].text != "  bus 14" || blocks[1].text != "  bus 21" {
		t.Errorf("got %+v", blocks)
	}
}

// TestInlineMarksAreRemoved checks the marks a single-styled cell cannot carry
// are taken out, and that a link keeps both halves: on paper an address is only
// useful if it is written out.
func TestInlineMarksAreRemoved(t *testing.T) {
	cases := map[string]string{
		"**very** good":                        "very good",
		"__very__ good":                        "very good",
		"a *quiet* morning":                    "a quiet morning",
		"a _quiet_ morning":                    "a quiet morning",
		"the `N1` road":                        "the N1 road",
		"[the museum](https://example.com)":    "the museum (https://example.com)",
		"![a photograph](https://a/photo.jpg)": "a photograph",
		`a \*literal\* star`:                   "a *literal* star",
		// A word with an underscore in it is a word, not emphasis.
		"the file_name stays": "the file_name stays",
	}
	for source, want := range cases {
		if got := inline(source); got != want {
			t.Errorf("inline(%q) = %q, want %q", source, got, want)
		}
	}
}

// TestParseMarkdownOfNothing checks text that is only whitespace produces no
// blocks, so an empty story writes nothing rather than an empty line.
func TestParseMarkdownOfNothing(t *testing.T) {
	for _, source := range []string{"", "   ", "\n\n\t\n"} {
		if blocks := parseMarkdown(source); len(blocks) != 0 {
			t.Errorf("parseMarkdown(%q) returned %+v", source, blocks)
		}
	}
}
