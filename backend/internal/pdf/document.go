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

	doc := &document{pdf: pdf, footer: footer}
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
	d.pdf.MultiCell(contentWidth, sizeSubhead*0.5, text, "", "L", false)
}

// body writes a paragraph of ordinary text, with an optional indent for the
// items of a list.
func (d *document) body(text string, indent float64, style string) {
	if text == "" {
		return
	}
	d.pdf.SetFont(fontFamily, style, sizeBody)
	d.pdf.SetX(marginLeft + indent)
	d.pdf.MultiCell(contentWidth-indent, lineHeight, text, "", "L", false)
}

// note writes a line of secondary text: a caption, a date, a set of figures.
func (d *document) note(text string) {
	if text == "" {
		return
	}
	d.pdf.SetFont(fontFamily, "", sizeSmall)
	d.pdf.SetTextColor(grey[0], grey[1], grey[2])
	d.pdf.MultiCell(contentWidth, lineHeight*0.85, text, "", "L", false)
	d.pdf.SetTextColor(0, 0, 0)
}

// rule draws a hairline across the page, between the days.
func (d *document) rule() {
	d.pdf.Ln(2)
	d.pdf.SetDrawColor(grey[0], grey[1], grey[2])
	y := d.pdf.GetY()
	d.pdf.Line(marginLeft, y, pageWidth-marginRight, y)
	d.pdf.SetDrawColor(0, 0, 0)
	d.pdf.Ln(3)
}

// figures writes a row of labelled numbers, as the summary opens with.
//
// Arguments:
//   - pairs: the label and the value of each figure, in the order they appear.
func (d *document) figures(pairs [][2]string) {
	if len(pairs) == 0 {
		return
	}
	// Four to a row: wider than that and the labels run into each other on the
	// narrowest of them.
	const perRow = 4
	width := contentWidth / perRow

	for start := 0; start < len(pairs); start += perRow {
		end := min(start+perRow, len(pairs))
		row := pairs[start:end]
		d.keepTogether(lineHeight * 2.4)

		top := d.pdf.GetY()
		for i, pair := range row {
			d.pdf.SetXY(marginLeft+width*float64(i), top)
			d.pdf.SetFont(fontFamily, "", sizeSmall)
			d.pdf.SetTextColor(grey[0], grey[1], grey[2])
			d.pdf.CellFormat(width, lineHeight*0.9, pair[0], "", 0, "L", false, 0, "")
		}
		for i, pair := range row {
			d.pdf.SetXY(marginLeft+width*float64(i), top+lineHeight*0.9)
			d.pdf.SetFont(fontFamily, "B", sizeSubhead)
			d.pdf.SetTextColor(0, 0, 0)
			d.pdf.CellFormat(width, lineHeight*1.2, pair[1], "", 0, "L", false, 0, "")
		}
		d.pdf.SetXY(marginLeft, top+lineHeight*2.4)
	}
	d.pdf.SetTextColor(0, 0, 0)
}

// table writes a simple table with a header row, used for the comparison of
// what was planned against what was spent.
//
// Arguments:
//   - header: the column titles.
//   - rows: the cells, in the same order as the columns.
//   - widths: the share of the content width each column takes, summing to 1.
func (d *document) table(header []string, rows [][]string, widths []float64) {
	columns := make([]float64, len(widths))
	for i, share := range widths {
		columns[i] = contentWidth * share
	}

	d.keepTogether(lineHeight * 2)
	d.pdf.SetFont(fontFamily, "B", sizeSmall)
	d.pdf.SetTextColor(grey[0], grey[1], grey[2])
	for i, cell := range header {
		align := "L"
		if i > 0 {
			align = "R"
		}
		d.pdf.CellFormat(columns[i], lineHeight*1.1, cell, "B", 0, align, false, 0, "")
	}
	d.pdf.Ln(-1)
	d.pdf.SetTextColor(0, 0, 0)

	d.pdf.SetFont(fontFamily, "", sizeBody)
	for _, row := range rows {
		d.keepTogether(lineHeight * 1.2)
		for i, cell := range row {
			align := "L"
			if i > 0 {
				align = "R"
			}
			d.pdf.CellFormat(columns[i], lineHeight*1.1, cell, "", 0, align, false, 0, "")
		}
		d.pdf.Ln(-1)
	}
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

// picture places one photograph, scaled to fit the width it is given. It draws
// the cover; the photographs of the days and places are laid out in rows by
// pictureRow instead.
//
// The bytes are a JPEG rendered by the media package, whatever the file was
// uploaded as, so nothing here has to know about WebP or about orientation.
//
// Arguments:
//   - jpeg: the encoded picture.
//   - width: how wide to draw it, in millimetres.
//   - caption: the line under it, or empty for none.
//
// Returns:
//   - an error when the picture cannot be read.
func (d *document) picture(jpeg []byte, width float64, caption string) error {
	name, info, err := d.register(jpeg)
	if err != nil {
		return err
	}

	height := width * info.Height() / info.Width()
	// A picture taller than the page cannot be placed at all, and one nearly
	// that tall leaves a page with a single photograph on it; both are better
	// served by shrinking it to a half page.
	if maxHeight := (pageHeight - marginTop - marginBottom) * 0.55; height > maxHeight {
		width *= maxHeight / height
		height = maxHeight
	}

	d.keepTogether(height + lineHeight)
	d.pdf.ImageOptions(name, marginLeft, d.pdf.GetY(), width, height, true, fpdf.ImageOptions{ImageType: "JPG"}, 0, "")
	if caption != "" {
		d.note(caption)
	}
	d.pdf.Ln(2)
	return nil
}

// The layout of the photographs of a day or a place: rows across the text
// column, each photograph whole and every one in a row as tall as the others.
const (
	// photosPerRow is how many photographs share a row.
	photosPerRow = 3
	// photoGap is the room between two photographs of a row, and below a row.
	photoGap = 3.0
	// maxRowHeight keeps a row of tall pictures from taking half a page. At the
	// width a row gives each picture, the 640 px previews print at roughly 280
	// dots to the inch.
	maxRowHeight = 55.0
	// sizeCaption is the type size of the time under each photograph.
	sizeCaption = 7.0
)

// rowPhoto is one photograph of a row and the line under it.
type rowPhoto struct {
	jpeg    []byte
	caption string
}

// pictureRow lays photographs side by side, all as tall as each other, so that
// together they fill the width of the text column. Nothing is cropped: a
// portrait picture is simply narrower than a landscape one. A row that would
// have to grow taller than limit to fill the width - the last, short row of a
// gallery, or one of tall pictures - is drawn at limit and left short.
//
// A row is kept on one page with its captions.
//
// Arguments:
//   - photos: the photographs of the row, in order.
//   - limit: the tallest the row may be, in millimetres.
//
// Returns:
//   - how tall the row was drawn, so the next short row can match it.
//   - an error when a picture cannot be read.
func (d *document) pictureRow(photos []rowPhoto, limit float64) (float64, error) {
	names := make([]string, len(photos))
	ratios := make([]float64, len(photos))
	sum := 0.0
	captioned := false
	for i, photo := range photos {
		name, info, err := d.register(photo.jpeg)
		if err != nil {
			return 0, err
		}
		names[i] = name
		ratios[i] = info.Width() / info.Height()
		sum += ratios[i]
		captioned = captioned || photo.caption != ""
	}

	height := min((contentWidth-photoGap*float64(len(photos)-1))/sum, limit)
	captionHeight := 0.0
	if captioned {
		captionHeight = sizeCaption * 0.5
	}
	d.keepTogether(height + captionHeight + photoGap)

	top := d.pdf.GetY()
	x := marginLeft
	d.pdf.SetFont(fontFamily, "", sizeCaption)
	d.pdf.SetTextColor(grey[0], grey[1], grey[2])
	for i, photo := range photos {
		width := height * ratios[i]
		d.pdf.ImageOptions(names[i], x, top, width, height, false, fpdf.ImageOptions{ImageType: "JPG"}, 0, "")
		if photo.caption != "" {
			d.pdf.SetXY(x, top+height+0.5)
			d.pdf.CellFormat(width, captionHeight, photo.caption, "", 0, "L", false, 0, "")
		}
		x += width + photoGap
	}
	d.pdf.SetTextColor(0, 0, 0)
	d.pdf.SetXY(marginLeft, top+height+captionHeight+photoGap)
	return height, nil
}

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
