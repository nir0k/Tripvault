// Package pdf renders a trip's report as a PDF file.
//
// The document is built on the server rather than printed by the browser, so
// that it looks the same wherever it was asked for and can be handed out through
// a read-only link, where there is no interface to print from.
package pdf

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"

	"github.com/go-pdf/fpdf"
)

// The typeface is embedded rather than installed, because the runtime stage of
// the image runs no command that could install one, and a document that renders
// differently depending on what the host happens to have is not a document.
//
// Liberation Sans covers Latin and Cyrillic, which is what the interface's two
// languages need; its licence travels beside it in fonts/LICENSE.
var (
	//go:embed fonts/LiberationSans-Regular.ttf
	fontRegular []byte
	//go:embed fonts/LiberationSans-Bold.ttf
	fontBold []byte
	//go:embed fonts/LiberationSans-Italic.ttf
	fontItalic []byte
)

// fontFamily is the name the embedded typeface is registered under.
const fontFamily = "Liberation"

// The page geometry, in millimetres. A4 with margins wide enough that a printed
// page does not lose a line to the printer's own unprintable edge.
const (
	pageWidth    = 210.0
	pageHeight   = 297.0
	marginLeft   = 18.0
	marginRight  = 18.0
	marginTop    = 18.0
	marginBottom = 18.0
	// contentWidth is what is left for the text and the pictures.
	contentWidth = pageWidth - marginLeft - marginRight
)

// The type sizes, in points.
const (
	sizeTitle    = 26.0
	sizeHeading  = 15.0
	sizeSubhead  = 12.0
	sizeBody     = 10.5
	sizeSmall    = 9.0
	lineHeight   = 5.2
	headingSpace = 3.0
)

// grey is the ink of everything secondary: labels, captions, the footer. It is
// dark enough to photocopy and light enough to stay out of the way.
var grey = [3]int{110, 110, 110}

// document wraps fpdf with the few operations this report needs, so that the
// report itself reads as a description of the page rather than as a sequence of
// coordinates.
type document struct {
	pdf *fpdf.Fpdf
	// images counts the pictures registered, which is how each gets a name of
	// its own: fpdf keeps them by name and would otherwise reuse the first.
	images int
	// footer is the line printed at the bottom of every page after the first.
	footer string
	// left and width are the column text is written in: the whole text column
	// unless a part of the page asked for a narrower one (inColumn).
	left, width float64
	// card is the card being written, whose paper is laid when it ends.
	card *openCard
}

// inColumn writes with the paragraphs of write kept to a column of the page,
// starting at x, and gives the whole text column back afterwards.
func (d *document) inColumn(x, width float64, write func()) {
	left, previous := d.left, d.width
	d.left, d.width = x, width
	defer func() { d.left, d.width = left, previous }()
	write()
}

// newDocument - opens an A4 document with the embedded typeface.
//
// Arguments:
//   - title: what the reader's PDF viewer shows as the document's name.
//   - footer: the line printed at the foot of every page, beside its number.
//
// Returns:
//   - the document, with no page started yet.
func newDocument(title, footer string) *document {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle(title, true)
	pdf.SetMargins(marginLeft, marginTop, marginRight)
	pdf.SetAutoPageBreak(true, marginBottom)

	pdf.AddUTF8FontFromBytes(fontFamily, "", fontRegular)
	pdf.AddUTF8FontFromBytes(fontFamily, "B", fontBold)
	pdf.AddUTF8FontFromBytes(fontFamily, "I", fontItalic)

	doc := &document{pdf: pdf, footer: footer, left: marginLeft, width: contentWidth}
	pdf.SetFooterFunc(doc.drawFooter)
	return doc
}

// drawFooter prints the trip and the page number at the foot of every page.
//
// The cover has none: a title page with a page number on it looks like a page
// torn out of something else.
func (d *document) drawFooter() {
	if d.pdf.PageNo() <= 1 {
		return
	}
	d.pdf.SetY(-marginBottom + 6)
	d.pdf.SetFont(fontFamily, "", sizeSmall)
	d.pdf.SetTextColor(grey[0], grey[1], grey[2])
	d.pdf.CellFormat(contentWidth/2, 5, d.footer, "", 0, "L", false, 0, "")
	d.pdf.CellFormat(contentWidth/2, 5, fmt.Sprint(d.pdf.PageNo()-1), "", 0, "R", false, 0, "")
	d.pdf.SetTextColor(0, 0, 0)
}

// page starts a new page.
func (d *document) page() {
	d.pdf.AddPage()
}

// title writes the document's own title, on the cover.
func (d *document) title(text string) {
	d.pdf.SetFont(fontFamily, "B", sizeTitle)
	d.pdf.MultiCell(contentWidth, sizeTitle*0.45, text, "", "L", false)
	d.pdf.Ln(2)
}

// heading writes a section heading, keeping it with what follows: a heading
// alone at the foot of a page is a heading on the wrong page.
func (d *document) heading(text string) {
	d.keepTogether(sizeHeading*0.5 + lineHeight*2)
	d.pdf.Ln(headingSpace)
	d.pdf.SetFont(fontFamily, "B", sizeHeading)
	d.pdf.MultiCell(contentWidth, sizeHeading*0.5, text, "", "L", false)
	d.pdf.Ln(1)
}

// subheading writes the heading of something inside a section - one place, one
// leg - and keeps it with the line after it.
func (d *document) subheading(text string) {
	d.keepTogether(sizeSubhead*0.5 + lineHeight)
	d.pdf.SetFont(fontFamily, "B", sizeSubhead)
	d.pdf.SetX(d.left)
	d.pdf.MultiCell(d.width, sizeSubhead*0.5, text, "", "L", false)
}

// body writes a paragraph of ordinary text, with an optional indent for the
// items of a list.
func (d *document) body(text string, indent float64, style string) {
	if text == "" {
		return
	}
	d.pdf.SetFont(fontFamily, style, sizeBody)
	d.pdf.SetX(d.left + indent)
	d.pdf.MultiCell(d.width-indent, lineHeight, text, "", "L", false)
}

// note writes a line of secondary text: a caption, a date, a set of figures.
func (d *document) note(text string) {
	if text == "" {
		return
	}
	d.pdf.SetFont(fontFamily, "", sizeSmall)
	d.pdf.SetTextColor(grey[0], grey[1], grey[2])
	d.pdf.SetX(d.left)
	d.pdf.MultiCell(d.width, lineHeight*0.85, text, "", "L", false)
	d.pdf.SetTextColor(0, 0, 0)
}

// register hands one photograph to fpdf and returns the name it is kept under
// with its size in pixels.
//
// The same photograph shown twice is stored once: fpdf recognises the bytes it
// already holds, so a picture that hangs on both a day and a place costs the
// document nothing the second time.
func (d *document) register(jpeg []byte) (string, *fpdf.ImageInfoType, error) {
	d.images++
	name := fmt.Sprintf("picture-%d", d.images)

	options := fpdf.ImageOptions{ImageType: "JPG", ReadDpi: false}
	info := d.pdf.RegisterImageOptionsReader(name, options, bytes.NewReader(jpeg))
	if info == nil || d.pdf.Err() {
		err := d.pdf.Error()
		d.pdf.ClearError()
		return "", nil, fmt.Errorf("read a photograph for the document: %w", err)
	}
	return name, info, nil
}

// sizeCaption is the type size of the credit under a map.
const sizeCaption = 7.0

// keepTogether starts a new page when what comes next would not fit on this one.
//
// fpdf breaks pages by itself, but only between the lines it writes; a heading
// and its first paragraph, or a picture and its caption, have to be asked for
// together or they end up on different pages.
func (d *document) keepTogether(height float64) {
	if d.pdf.GetY()+height > pageHeight-marginBottom {
		d.pdf.AddPage()
	}
}

// space adds vertical room between blocks.
func (d *document) space(height float64) {
	d.pdf.Ln(height)
}

// render - writes the finished document.
//
// Arguments:
//   - w: where the bytes go.
//
// Returns:
//   - an error from the writer, or from anything that went wrong while the
//     document was being built: fpdf records the first failure and carries on,
//     so this is where it surfaces.
func (d *document) render(w io.Writer) error {
	if err := d.pdf.Output(w); err != nil {
		return fmt.Errorf("write the document: %w", err)
	}
	return nil
}
