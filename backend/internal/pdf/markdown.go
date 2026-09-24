package pdf

import (
	"regexp"
	"strings"
)

// The stories in a report are written in Markdown and shown as rich text in the
// browser. A PDF has no HTML to hand, so the text is read into blocks here and
// each is written in the style it stands for.
//
// This is deliberately a subset. Headings, paragraphs, lists, quotes and the
// three inline marks are what people actually write in a travel diary; tables,
// images and raw HTML are not, and guessing at them would produce a worse page
// than leaving the characters where the writer put them.

// blockKind says how a block of text is written.
type blockKind int

// The kinds of block a story is read into.
const (
	// blockParagraph is ordinary prose.
	blockParagraph blockKind = iota
	// blockHeading is a line the writer marked with hashes.
	blockHeading
	// blockBullet is one item of a bulleted list.
	blockBullet
	// blockNumber is one item of a numbered list, already carrying its number.
	blockNumber
	// blockQuote is a quoted passage.
	blockQuote
)

// block is one piece of a story, ready to be written.
type block struct {
	kind blockKind
	text string
	// level is the depth of a heading, 1 to 6; zero for everything else.
	level int
}

// The patterns a line is recognised by.
var (
	headingPattern = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	bulletPattern  = regexp.MustCompile(`^\s*[-*+]\s+(.*)$`)
	numberPattern  = regexp.MustCompile(`^\s*(\d{1,3})[.)]\s+(.*)$`)
	quotePattern   = regexp.MustCompile(`^\s*>\s?(.*)$`)
	fencePattern   = regexp.MustCompile("^\\s*```")
)

// The inline marks, removed once the text is no longer going to be styled.
//
// A PDF cell takes one style at a time, so bold inside a sentence would mean
// measuring and placing every run by hand. The marks are dropped instead: what
// the writer emphasised reads the same, and the asterisks do not sit in the
// middle of the sentence pretending to be punctuation.
var (
	strongPattern = regexp.MustCompile(`(\*\*|__)(.+?)(\*\*|__)`)
	emphPattern   = regexp.MustCompile(`(^|[^*_\w])[*_]([^*_]+)[*_]([^*_\w]|$)`)
	codePattern   = regexp.MustCompile("`([^`]+)`")
	imagePattern  = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	linkPattern   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)[^)]*\)`)
)

// parseMarkdown - reads a story into the blocks a page is written from.
//
// Arguments:
//   - source: the Markdown the writer typed.
//
// Returns:
//   - the blocks, in the order they appear; empty for text that is only
//     whitespace.
func parseMarkdown(source string) []block {
	var blocks []block
	var paragraph []string
	fenced := false

	// flush closes the paragraph being collected, if there is one.
	flush := func() {
		if len(paragraph) == 0 {
			return
		}
		blocks = append(blocks, block{kind: blockParagraph, text: inline(strings.Join(paragraph, " "))})
		paragraph = nil
	}

	for _, raw := range strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")

		// A fenced block is kept as it was written, line by line: it is code, or
		// something the writer wanted left alone.
		if fencePattern.MatchString(line) {
			flush()
			fenced = !fenced
			continue
		}
		if fenced {
			blocks = append(blocks, block{kind: blockParagraph, text: line})
			continue
		}

		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}

		switch {
		case headingPattern.MatchString(line):
			flush()
			parts := headingPattern.FindStringSubmatch(line)
			blocks = append(blocks, block{
				kind: blockHeading, level: len(parts[1]), text: inline(parts[2]),
			})
		case bulletPattern.MatchString(line):
			flush()
			parts := bulletPattern.FindStringSubmatch(line)
			blocks = append(blocks, block{kind: blockBullet, text: inline(parts[1])})
		case numberPattern.MatchString(line):
			flush()
			parts := numberPattern.FindStringSubmatch(line)
			blocks = append(blocks, block{kind: blockNumber, text: parts[1] + ". " + inline(parts[2])})
		case quotePattern.MatchString(line):
			flush()
			parts := quotePattern.FindStringSubmatch(line)
			blocks = append(blocks, block{kind: blockQuote, text: inline(parts[1])})
		default:
			paragraph = append(paragraph, strings.TrimSpace(line))
		}
	}
	flush()
	return blocks
}

// escaped maps a mark the writer escaped onto a character no text contains, so
// that it survives the patterns below and is put back afterwards. A mark written
// to be seen is not a mark to act on.
var escaped = []struct{ mark, hidden string }{
	{`\*`, "\ue000"},
	{`\_`, "\ue001"},
	{"\\`", "\ue002"},
	{`\#`, "\ue003"},
}

// inline removes the marks that cannot be carried into a single-styled cell and
// rewrites a link as its text followed by its address, which is the only way a
// printed page can offer one.
func inline(text string) string {
	for _, pair := range escaped {
		text = strings.ReplaceAll(text, pair.mark, pair.hidden)
	}

	text = imagePattern.ReplaceAllString(text, "$1")
	text = linkPattern.ReplaceAllString(text, "$1 ($2)")
	text = strongPattern.ReplaceAllString(text, "$2")
	text = emphPattern.ReplaceAllString(text, "$1$2$3")
	text = codePattern.ReplaceAllString(text, "$1")

	for _, pair := range escaped {
		text = strings.ReplaceAll(text, pair.hidden, strings.TrimPrefix(pair.mark, `\`))
	}
	return strings.TrimSpace(text)
}

// writeMarkdown - writes a story into the document in the styles its blocks name.
//
// Arguments:
//   - doc: the document being built.
//   - source: the Markdown the writer typed.
func writeMarkdown(doc *document, source string) {
	for _, item := range parseMarkdown(source) {
		switch item.kind {
		case blockHeading:
			// A heading inside a story is one step smaller than the section it
			// sits in, so it never competes with the day it belongs to.
			doc.subheading(item.text)
		case blockBullet:
			doc.body(bulletMark+"  "+item.text, 4, "")
		case blockNumber:
			doc.body(item.text, 4, "")
		case blockQuote:
			doc.body(item.text, 4, "I")
		default:
			doc.body(item.text, 0, "")
		}
	}
}
