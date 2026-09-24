package docsexport

import (
	"bytes"
	"image"
	_ "image/gif"  // decode attached GIFs for embedding
	_ "image/jpeg" // decode attached JPEGs for embedding
	_ "image/png"  // decode attached PNGs for embedding
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	_ "golang.org/x/image/webp" // decode attached WebP images for embedding
)

// Page geometry in PDF points (A4).
const (
	pageWidth  = 595.28
	pageHeight = 841.89
	margin     = 56.0
	bodySize   = 10.5
	codeSize   = 9.0
	lineFactor = 1.4
)

// ImageSource resolves an image destination (attachment:<id>) to its bytes;
// ok is false for anything it will not embed.
type ImageSource func(destination string) (content []byte, ok bool)

// PDF renders a document's Markdown as a PDF: headings, paragraphs,
// emphasis, inline code, lists (nested, numbered, tasks), block quotes,
// code blocks, simple grid tables, rules and embedded images. The title is
// set as the first heading unless the text already opens with it.
//
// Arabic text is kept in logical order and extracts correctly, but the
// bundled Go fonts have no Arabic glyphs and the writer does no shaping,
// so Arabic letters draw as empty boxes.
func PDF(title, markdown string, images ImageSource) []byte {
	doc := newPDFDoc(title)
	l := &layout{doc: doc, images: images}
	l.page()
	source := []byte(markdown)
	root := markdownParser.Parse(text.NewReader(source))
	if title != "" && !opensWithTitle(root, source, title) {
		l.heading(1, []span{{text: title}})
	}
	l.blocks(root, source, 0)
	return doc.bytes()
}

func opensWithTitle(root ast.Node, source []byte, title string) bool {
	first := root.FirstChild()
	heading, ok := first.(*ast.Heading)
	return ok && strings.EqualFold(strings.TrimSpace(inlineText(heading, source)), strings.TrimSpace(title))
}

// span is a run of inline text in one style; image marks an image token.
type span struct {
	text  string
	style fontStyle
	image string
	alt   string
	brk   bool
}

type layout struct {
	doc    *pdfDoc
	images ImageSource
	cur    *bytes.Buffer
	y      float64
	// column narrows the line width (table cells); noBreak keeps a cell
	// from starting a page of its own.
	column  float64
	noBreak bool
}

func (l *layout) page() {
	l.cur = l.doc.newPage()
	l.y = pageHeight - margin
}

// ensure starts a new page when h more points do not fit.
func (l *layout) ensure(h float64) {
	if !l.noBreak && l.y-h < margin {
		l.page()
	}
}

func (l *layout) gap(h float64) {
	if l.y < pageHeight-margin {
		l.y -= h
	}
}

func (l *layout) blocks(parent ast.Node, source []byte, indent float64) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		l.block(child, source, indent)
	}
}

func (l *layout) block(node ast.Node, source []byte, indent float64) {
	switch n := node.(type) {
	case *ast.Heading:
		l.heading(n.Level, inlineSpans(n, source, styleBold))
	case *ast.Paragraph, *ast.TextBlock:
		l.paragraph(inlineSpans(n, source, styleRegular), indent, bodySize, 0)
		l.gap(bodySize * 0.6)
	case *ast.List:
		l.list(n, source, indent)
		l.gap(bodySize * 0.4)
	case *ast.Blockquote:
		top, startPage := l.y, len(l.doc.pages)
		l.blocks(n, source, indent+14)
		if len(l.doc.pages) == startPage {
			l.doc.line(l.cur, margin+indent+4, top, margin+indent+4, l.y+bodySize*0.6, 0.7, 2)
		}
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		l.code(blockLines(node, source), indent)
	case *east.Table:
		l.table(n, source, indent)
	case *ast.ThematicBreak:
		l.ensure(12)
		l.y -= 6
		l.doc.line(l.cur, margin+indent, l.y, pageWidth-margin, l.y, 0.75, 0.8)
		l.y -= 8
	case *ast.HTMLBlock:
		l.paragraph([]span{{text: strings.TrimSpace(blockLines(n, source))}}, indent, bodySize, 0)
		l.gap(bodySize * 0.6)
	default:
		l.blocks(node, source, indent)
	}
}

func (l *layout) heading(level int, spans []span) {
	sizes := map[int]float64{1: 20, 2: 16, 3: 13.5}
	size, ok := sizes[level]
	if !ok {
		size = 12
	}
	for i := range spans {
		if spans[i].style != styleMono {
			spans[i].style = styleBold
		}
	}
	l.gap(size * 0.5)
	l.ensure(size * lineFactor * 2)
	l.paragraph(spans, 0, size, 0)
	l.gap(size * 0.35)
}

// paragraph wraps spans into lines within the text column, starting at
// indent. first is extra indent reserved on the first line (list markers).
func (l *layout) paragraph(spans []span, indent, size, first float64) {
	width := pageWidth - 2*margin - indent
	if l.column > 0 {
		width = l.column
	}
	lineHeight := size * lineFactor
	type piece struct {
		text  string
		style fontStyle
		w     float64
	}
	var line []piece
	lineW := first
	rtl := startsRTL(spans)
	flush := func() {
		l.ensure(lineHeight)
		l.y -= lineHeight
		x := margin + indent + first
		if rtl {
			x = pageWidth - margin - lineW + first
		}
		for _, p := range line {
			l.doc.text(l.cur, p.style, size, x, l.y+size*0.28, p.text, 0.1)
			x += p.w
		}
		line, lineW, first = nil, 0, 0
	}
	for _, s := range spans {
		if s.brk {
			flush()
			continue
		}
		if s.image != "" {
			if len(line) > 0 {
				flush()
			}
			l.image(s.image, s.alt, indent)
			continue
		}
		f := l.doc.fonts[s.style]
		for _, word := range splitWords(s.text) {
			w := f.measure(word, size)
			if strings.TrimSpace(word) == "" {
				if len(line) == 0 {
					continue
				}
				line = append(line, piece{word, s.style, w})
				lineW += w
				continue
			}
			if lineW+w > width && len(line) > 0 {
				// Drop the trailing space before breaking.
				if last := line[len(line)-1]; strings.TrimSpace(last.text) == "" {
					line = line[:len(line)-1]
					lineW -= last.w
				}
				flush()
			}
			for w > width {
				// A word longer than the line is split by characters.
				cut, cutW := 0, 0.0
				for i, r := range word {
					rw := f.width(r) * size / 1000
					if cutW+rw > width-lineW && i > 0 {
						break
					}
					cut, cutW = i+len(string(r)), cutW+rw
				}
				line = append(line, piece{word[:cut], s.style, cutW})
				lineW += cutW
				flush()
				word = word[cut:]
				w = f.measure(word, size)
			}
			line = append(line, piece{word, s.style, w})
			lineW += w
		}
	}
	if len(line) > 0 {
		flush()
	}
}

func startsRTL(spans []span) bool {
	for _, s := range spans {
		for _, r := range s.text {
			switch {
			case unicode.In(r, unicode.Arabic, unicode.Hebrew):
				return true
			case unicode.IsLetter(r):
				return false
			}
		}
	}
	return false
}

// splitWords splits text into words and the spaces between them.
func splitWords(value string) []string {
	var out []string
	start := 0
	inSpace := false
	for i, r := range value {
		space := r == ' '
		if i > start && space != inSpace {
			out = append(out, value[start:i])
			start = i
		}
		inSpace = space
	}
	if start < len(value) {
		out = append(out, value[start:])
	}
	return out
}

func (l *layout) list(list *ast.List, source []byte, indent float64) {
	number := list.Start
	if number == 0 {
		number = 1
	}
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		marker := "•"
		if list.IsOrdered() {
			marker = strconv.Itoa(number) + "."
			number++
		}
		markerW := 16.0
		first := true
		for child := item.FirstChild(); child != nil; child = child.NextSibling() {
			switch c := child.(type) {
			case *ast.Paragraph, *ast.TextBlock:
				spans := inlineSpans(c, source, styleRegular)
				if first {
					l.ensure(bodySize * lineFactor)
					l.doc.text(l.cur, styleRegular, bodySize, margin+indent, l.y-bodySize*lineFactor+bodySize*0.28, marker, 0.1)
					first = false
				}
				l.paragraph(spans, indent+markerW, bodySize, 0)
			default:
				if first {
					l.ensure(bodySize * lineFactor)
					l.doc.text(l.cur, styleRegular, bodySize, margin+indent, l.y-bodySize*lineFactor+bodySize*0.28, marker, 0.1)
					first = false
				}
				l.block(child, source, indent+markerW)
			}
		}
	}
}

func (l *layout) code(code string, indent float64) {
	f := l.doc.fonts[styleMono]
	width := pageWidth - 2*margin - indent - 12
	lineHeight := codeSize * 1.35
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(code, "\t", "    "), "\n") {
		for f.measure(raw, codeSize) > width {
			cut := 0
			w := 0.0
			for i, r := range raw {
				rw := f.width(r) * codeSize / 1000
				if w+rw > width {
					break
				}
				cut, w = i+len(string(r)), w+rw
			}
			if cut == 0 {
				break
			}
			lines = append(lines, raw[:cut])
			raw = raw[cut:]
		}
		lines = append(lines, raw)
	}
	l.gap(2)
	for start := 0; start < len(lines); {
		l.ensure(lineHeight + 8)
		fit := max(1, int((l.y-margin-8)/lineHeight))
		end := min(len(lines), start+fit)
		h := float64(end-start)*lineHeight + 8
		l.doc.rect(l.cur, margin+indent, l.y-h, pageWidth-2*margin-indent, h, 0.94, true)
		y := l.y - 4
		for _, text := range lines[start:end] {
			y -= lineHeight
			l.doc.text(l.cur, styleMono, codeSize, margin+indent+6, y+codeSize*0.3, text, 0.1)
		}
		l.y -= h
		start = end
		if start < len(lines) {
			l.page()
		}
	}
	l.gap(bodySize * 0.6)
}

func (l *layout) table(table *east.Table, source []byte, indent float64) {
	var rows [][][]span
	var header []bool
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		_, isHeader := row.(*east.TableHeader)
		base := styleRegular
		if isHeader {
			base = styleBold
		}
		var cells [][]span
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, inlineSpans(cell, source, base))
		}
		rows = append(rows, cells)
		header = append(header, isHeader)
	}
	columns := 0
	for _, r := range rows {
		columns = max(columns, len(r))
	}
	if columns == 0 {
		return
	}
	size := bodySize - 1
	pad := 4.0
	total := pageWidth - 2*margin - indent
	colW := total / float64(columns)
	for i, cells := range rows {
		// Measure the row by laying each cell out on a scratch layout.
		heights := make([]float64, columns)
		for c := 0; c < columns && c < len(cells); c++ {
			heights[c] = l.measureSpans(cells[c], colW-2*pad, size)
		}
		h := size*lineFactor + 2*pad
		for _, v := range heights {
			h = max(h, v+2*pad)
		}
		l.ensure(h)
		top := l.y
		if header[i] {
			l.doc.rect(l.cur, margin+indent, top-h, total, h, 0.92, true)
		}
		for c := 0; c < columns; c++ {
			x := margin + indent + float64(c)*colW
			l.doc.rect(l.cur, x, top-h, colW, h, 0.55, false)
			if c < len(cells) {
				saved := l.y
				l.y = top - pad
				l.paragraphIn(cells[c], x+pad, colW-2*pad, size)
				l.y = saved
			}
		}
		l.y = top - h
	}
	l.gap(bodySize * 0.8)
}

// paragraphIn lays spans out in a table cell: a fixed column at x, with
// no page breaks (the row was measured to fit first).
func (l *layout) paragraphIn(spans []span, x, width, size float64) {
	saved := *l
	l.column, l.noBreak = width, true
	l.paragraph(textOnly(spans), x-margin, size, 0)
	l.column, l.noBreak = saved.column, saved.noBreak
}

// measureSpans is the height spans take in a column of the given width.
func (l *layout) measureSpans(spans []span, width, size float64) float64 {
	scratch := &layout{doc: l.doc, cur: &bytes.Buffer{}, y: 1e6, column: width, noBreak: true}
	scratch.paragraph(textOnly(spans), 0, size, 0)
	return 1e6 - scratch.y
}

func textOnly(spans []span) []span {
	out := make([]span, 0, len(spans))
	for _, s := range spans {
		if s.image != "" {
			s = span{text: s.alt, style: s.style}
		}
		out = append(out, s)
	}
	return out
}

func (l *layout) image(destination, alt string, indent float64) {
	var content []byte
	ok := false
	if l.images != nil {
		content, ok = l.images(destination)
	}
	if ok {
		if decoded, format, err := image.Decode(bytes.NewReader(content)); err == nil {
			bounds := decoded.Bounds()
			maxW := pageWidth - 2*margin - indent
			maxH := pageHeight - 2*margin
			w := float64(bounds.Dx()) * 0.75
			h := float64(bounds.Dy()) * 0.75
			if w > maxW {
				h, w = h*maxW/w, maxW
			}
			if h > maxH*0.8 {
				w, h = w*maxH*0.8/h, maxH*0.8
			}
			index := l.doc.addImage(content, decoded, format)
			l.ensure(h + 6)
			l.y -= h + 3
			l.doc.drawImage(l.cur, index, margin+indent, l.y, w, h)
			l.y -= 3
			if alt != "" {
				l.paragraph([]span{{text: alt, style: styleItalic}}, indent, bodySize-1.5, 0)
			}
			return
		}
	}
	label := alt
	if label == "" {
		label = destination
	}
	l.paragraph([]span{{text: "[" + label + "]", style: styleItalic}}, indent, bodySize, 0)
}

// inlineSpans flattens a block's inline children into styled spans.
func inlineSpans(parent ast.Node, source []byte, base fontStyle) []span {
	var out []span
	var walk func(n ast.Node, style fontStyle)
	walk = func(n ast.Node, style fontStyle) {
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			switch c := child.(type) {
			case *ast.Text:
				out = append(out, span{text: unescape(c, source), style: style})
				if c.HardLineBreak() {
					out = append(out, span{brk: true})
				} else if c.SoftLineBreak() {
					out = append(out, span{text: " ", style: style})
				}
			case *ast.String:
				out = append(out, span{text: string(c.Value), style: style})
			case *ast.CodeSpan:
				out = append(out, span{text: inlineText(c, source), style: styleMono})
			case *ast.Emphasis:
				next := styleItalic
				if c.Level == 2 {
					next = styleBold
				}
				if style == styleBold {
					next = styleBold
				}
				walk(c, next)
			case *ast.Image:
				out = append(out, span{image: string(c.Destination), alt: inlineText(c, source)})
			case *east.TaskCheckBox:
				mark := "[ ] "
				if c.IsChecked {
					mark = "[x] "
				}
				out = append(out, span{text: mark, style: styleMono})
			case *ast.AutoLink:
				out = append(out, span{text: string(c.URL(source)), style: style})
			case *ast.RawHTML:
				out = append(out, span{text: string(c.Segments.Value(source)), style: style})
			default:
				walk(c, style)
			}
		}
	}
	walk(parent, base)
	return out
}
