package docsexport

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// The PDF writer: just enough of PDF 1.7 for exported documents. Text is
// set in the Go fonts (BSD licensed, shipped inside golang.org/x/image),
// embedded whole as CIDFontType2 with Identity-H encoding. Every character
// gets its own CID from one allocation shared by all fonts, so a single
// ToUnicode map makes every run copyable and searchable, and each font's
// CIDToGIDMap points the CID at its own glyph (glyph 0 when the font has
// none, as for Arabic, whose letters then extract correctly but draw as
// empty boxes).

type fontStyle int

const (
	styleRegular fontStyle = iota
	styleBold
	styleItalic
	styleMono
	styleCount
)

type pdfFont struct {
	name string
	data []byte
	sf   *sfnt.Font
	buf  sfnt.Buffer
	upem float64
	// widths caches advance per rune in 1/1000 em.
	widths map[rune]float64
	gids   map[rune]uint16
	ascent float64
}

func loadFont(name string, data []byte) *pdfFont {
	sf, err := sfnt.Parse(data)
	if err != nil {
		panic("docsexport: bundled font does not parse: " + err.Error())
	}
	f := &pdfFont{name: name, data: data, sf: sf, upem: float64(sf.UnitsPerEm()), widths: map[rune]float64{}, gids: map[rune]uint16{}}
	if m, err := sf.Metrics(&f.buf, fixed.I(int(sf.UnitsPerEm())), font.HintingNone); err == nil {
		f.ascent = float64(m.Ascent.Round()) * 1000 / f.upem
	}
	return f
}

func (f *pdfFont) glyph(r rune) uint16 {
	if gid, ok := f.gids[r]; ok {
		return gid
	}
	gid, err := f.sf.GlyphIndex(&f.buf, r)
	if err != nil {
		gid = 0
	}
	f.gids[r] = uint16(gid)
	return uint16(gid)
}

// width is the advance of r in 1/1000 em.
func (f *pdfFont) width(r rune) float64 {
	if w, ok := f.widths[r]; ok {
		return w
	}
	w := 500.0
	if gid := f.glyph(r); gid != 0 {
		if adv, err := f.sf.GlyphAdvance(&f.buf, sfnt.GlyphIndex(gid), fixed.I(int(f.upem)), font.HintingNone); err == nil {
			w = float64(adv) / 64 * 1000 / f.upem
		}
	}
	f.widths[r] = w
	return w
}

func (f *pdfFont) measure(text string, size float64) float64 {
	total := 0.0
	for _, r := range text {
		total += f.width(r)
	}
	return total * size / 1000
}

type pdfImage struct {
	width, height int
	filter        string // "DCTDecode" or "FlateDecode"
	colorSpace    string
	data          []byte
}

// pdfDoc collects pages and resources, then serializes them.
type pdfDoc struct {
	fonts  [styleCount]*pdfFont
	cids   map[rune]uint16
	order  []rune
	pages  []*bytes.Buffer
	images []pdfImage
	title  string
}

func newPDFDoc(title string) *pdfDoc {
	return &pdfDoc{
		fonts: [styleCount]*pdfFont{
			loadFont("GoRegular", goregular.TTF), loadFont("GoBold", gobold.TTF),
			loadFont("GoItalic", goitalic.TTF), loadFont("GoMono", gomono.TTF),
		},
		cids:  map[rune]uint16{},
		title: title,
	}
}

func (d *pdfDoc) cid(r rune) uint16 {
	if c, ok := d.cids[r]; ok {
		return c
	}
	if len(d.order) >= 0xFFFE {
		r = '?'
		if c, ok := d.cids[r]; ok {
			return c
		}
	}
	d.order = append(d.order, r)
	c := uint16(len(d.order))
	d.cids[r] = c
	return c
}

func (d *pdfDoc) newPage() *bytes.Buffer {
	page := &bytes.Buffer{}
	d.pages = append(d.pages, page)
	return page
}

// text writes one run with its baseline starting at (x, y).
func (d *pdfDoc) text(page *bytes.Buffer, style fontStyle, size, x, y float64, value string, gray float64) {
	if value == "" {
		return
	}
	var hex strings.Builder
	for _, r := range value {
		fmt.Fprintf(&hex, "%04X", d.cid(r))
	}
	fmt.Fprintf(page, "BT %s g /F%d %s Tf 1 0 0 1 %s %s Tm <%s> Tj ET\n", num(gray), int(style)+1, num(size), num(x), num(y), hex.String())
}

func (d *pdfDoc) rect(page *bytes.Buffer, x, y, w, h, gray float64, fill bool) {
	if fill {
		fmt.Fprintf(page, "%s g %s %s %s %s re f\n", num(gray), num(x), num(y), num(w), num(h))
		return
	}
	fmt.Fprintf(page, "%s G 0.6 w %s %s %s %s re S\n", num(gray), num(x), num(y), num(w), num(h))
}

func (d *pdfDoc) line(page *bytes.Buffer, x1, y1, x2, y2, gray, width float64) {
	fmt.Fprintf(page, "%s G %s w %s %s m %s %s l S\n", num(gray), num(width), num(x1), num(y1), num(x2), num(y2))
}

// addImage registers an image and returns its index. JPEGs in RGB or gray
// pass through as DCTDecode; everything else is decoded, flattened onto
// white and stored as deflated RGB.
func (d *pdfDoc) addImage(content []byte, decoded image.Image, format string) int {
	bounds := decoded.Bounds()
	if format == "jpeg" {
		if cfg, err := jpeg.DecodeConfig(bytes.NewReader(content)); err == nil {
			switch cfg.ColorModel {
			case color.YCbCrModel, color.RGBAModel:
				d.images = append(d.images, pdfImage{width: cfg.Width, height: cfg.Height, filter: "DCTDecode", colorSpace: "/DeviceRGB", data: content})
				return len(d.images) - 1
			case color.GrayModel:
				d.images = append(d.images, pdfImage{width: cfg.Width, height: cfg.Height, filter: "DCTDecode", colorSpace: "/DeviceGray", data: content})
				return len(d.images) - 1
			}
		}
	}
	raw := make([]byte, 0, bounds.Dx()*bounds.Dy()*3)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := decoded.At(x, y).RGBA()
			// Composite onto white: straight channel = premultiplied + (1-a).
			white := 0xffff - a
			raw = append(raw, byte((r+white)>>8), byte((g+white)>>8), byte((b+white)>>8))
		}
	}
	d.images = append(d.images, pdfImage{width: bounds.Dx(), height: bounds.Dy(), filter: "FlateDecode", colorSpace: "/DeviceRGB", data: deflate(raw)})
	return len(d.images) - 1
}

func (d *pdfDoc) drawImage(page *bytes.Buffer, index int, x, y, w, h float64) {
	fmt.Fprintf(page, "q %s 0 0 %s %s %s cm /Im%d Do Q\n", num(w), num(h), num(x), num(y), index+1)
}

func deflate(data []byte) []byte {
	var out bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&out, zlib.BestCompression)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return out.Bytes()
}

func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

// pdfString encodes text as a UTF-16BE hex string with a byte-order mark.
func pdfString(value string) string {
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, unit := range utf16.Encode([]rune(value)) {
		fmt.Fprintf(&b, "%04X", unit)
	}
	b.WriteString(">")
	return b.String()
}

// bytes serializes the document. Object numbers are assigned in order.
func (d *pdfDoc) bytes() []byte {
	if len(d.pages) == 0 {
		d.newPage()
	}
	var out bytes.Buffer
	offsets := []int{0}
	obj := func(body string) int {
		offsets = append(offsets, out.Len())
		n := len(offsets) - 1
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, body)
		return n
	}
	stream := func(dict string, data []byte) int {
		offsets = append(offsets, out.Len())
		n := len(offsets) - 1
		fmt.Fprintf(&out, "%d 0 obj\n<< %s /Length %d >>\nstream\n", n, dict, len(data))
		out.Write(data)
		out.WriteString("\nendstream\nendobj\n")
		return n
	}
	out.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")

	toUnicode := stream("/Filter /FlateDecode", deflate([]byte(d.toUnicodeCMap())))
	fontRefs := make([]int, styleCount)
	for i, f := range d.fonts {
		fontFile := stream(fmt.Sprintf("/Filter /FlateDecode /Length1 %d", len(f.data)), deflate(f.data))
		descriptor := obj(fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags %d /FontBBox [-200 -300 1200 1000] /ItalicAngle %d /Ascent %s /Descent -250 /CapHeight 700 /StemV 80 /FontFile2 %d 0 R >>",
			f.name, map[bool]int{true: 33, false: 32}[fontStyle(i) == styleMono], map[bool]int{true: -12, false: 0}[fontStyle(i) == styleItalic], num(f.ascent), fontFile))
		gidMap := make([]byte, 2*(len(d.order)+1))
		var widths strings.Builder
		widths.WriteString("[")
		for index, r := range d.order {
			gid := f.glyph(r)
			gidMap[2*(index+1)] = byte(gid >> 8)
			gidMap[2*(index+1)+1] = byte(gid)
			fmt.Fprintf(&widths, " %d [%s]", index+1, num(f.width(r)))
		}
		widths.WriteString(" ]")
		cidToGID := stream("/Filter /FlateDecode", deflate(gidMap))
		descendant := obj(fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /DW 500 /W %s /CIDToGIDMap %d 0 R >>",
			f.name, descriptor, widths.String(), cidToGID))
		fontRefs[i] = obj(fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>", f.name, descendant, toUnicode))
	}
	imageRefs := make([]int, len(d.images))
	for i, img := range d.images {
		imageRefs[i] = stream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /%s", img.width, img.height, img.colorSpace, img.filter), img.data)
	}
	var resources strings.Builder
	resources.WriteString("<< /Font <<")
	for i, ref := range fontRefs {
		fmt.Fprintf(&resources, " /F%d %d 0 R", i+1, ref)
	}
	resources.WriteString(" >>")
	if len(imageRefs) > 0 {
		resources.WriteString(" /XObject <<")
		for i, ref := range imageRefs {
			fmt.Fprintf(&resources, " /Im%d %d 0 R", i+1, ref)
		}
		resources.WriteString(" >>")
	}
	resources.WriteString(" >>")
	resourceRef := obj(resources.String())

	// Pages refer to their parent, so the page tree number is reserved:
	// it is the object right after the last page.
	contentRefs := make([]int, len(d.pages))
	for i, page := range d.pages {
		contentRefs[i] = stream("/Filter /FlateDecode", deflate(page.Bytes()))
	}
	pagesRef := len(offsets) + len(d.pages)
	pageRefs := make([]string, len(d.pages))
	for i := range d.pages {
		ref := obj(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] /Resources %d 0 R /Contents %d 0 R >>", pagesRef, num(pageWidth), num(pageHeight), resourceRef, contentRefs[i]))
		pageRefs[i] = strconv.Itoa(ref) + " 0 R"
	}
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(pageRefs, " "), len(pageRefs)))
	catalog := obj(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesRef))
	info := obj(fmt.Sprintf("<< /Title %s /Producer %s >>", pdfString(d.title), pdfString("hcm-next docs export")))

	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), catalog, info, xref)
	return out.Bytes()
}

// toUnicodeCMap maps every allocated CID back to its character.
func (d *pdfDoc) toUnicodeCMap() string {
	var b strings.Builder
	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	cids := make([]int, 0, len(d.order))
	for i := range d.order {
		cids = append(cids, i+1)
	}
	sort.Ints(cids)
	for start := 0; start < len(cids); start += 100 {
		end := min(start+100, len(cids))
		fmt.Fprintf(&b, "%d beginbfchar\n", end-start)
		for _, c := range cids[start:end] {
			fmt.Fprintf(&b, "<%04X> <", c)
			for _, unit := range utf16.Encode([]rune{d.order[c-1]}) {
				fmt.Fprintf(&b, "%04X", unit)
			}
			b.WriteString(">\n")
		}
		b.WriteString("endbfchar\n")
	}
	b.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	return b.String()
}
