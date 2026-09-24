package docsexport

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

const sample = `# Leave policy

Employees accrue **paid leave** monthly. See [the portal](https://hr.example.com/leave) and ` + "`code`" + `.

## Steps

1. Open the request form
2. Pick *dates*
   - Nested note
- [x] Manager approved

> Quoted guidance.

| Region | Days |
| --- | --: |
| Germany — Größe | 30 |
| Egypt | 21 |

` + "```go\nfunc main() {}\n```" + `

![Org chart](attachment:docm-123)

[Handbook.pdf](attachment:docm-456)

---

سياسة الإجازة السنوية
`

// TestDocsExport_PlainTextGolden pins the plain-text export byte for byte.
func TestDocsExport_PlainTextGolden(t *testing.T) {
	want := `Leave policy

Employees accrue paid leave monthly. See the portal (https://hr.example.com/leave) and code.

Steps

1. Open the request form
2. Pick dates
   - Nested note

- [x] Manager approved

> Quoted guidance.

Region	Days
Germany — Größe	30
Egypt	21

    func main() {}

[Org chart]

Handbook.pdf

----

سياسة الإجازة السنوية
`
	if got := PlainText(sample); got != want {
		t.Fatalf("plain text:\n%s\n---want---\n%s", got, want)
	}
}

// TestDocsExport_MarkdownGolden: attachment references become absolute
// download addresses; everything else is the source unchanged.
func TestDocsExport_MarkdownGolden(t *testing.T) {
	got := Markdown(sample, func(id string) string { return "https://hcm.example/v1/documents/media/doc-1/" + id + "?download=1" })
	want := strings.NewReplacer(
		"(attachment:docm-123)", "(https://hcm.example/v1/documents/media/doc-1/docm-123?download=1)",
		"(attachment:docm-456)", "(https://hcm.example/v1/documents/media/doc-1/docm-456?download=1)",
	).Replace(sample)
	if got != want {
		t.Fatalf("markdown export:\n%s", got)
	}
	if Markdown("x", nil) != "x" {
		t.Fatal("nil resolver changed the text")
	}
	ids := AttachmentIDs(sample + "\n![again](attachment:docm-123)")
	if strings.Join(ids, ",") != "docm-123,docm-456" {
		t.Fatalf("attachment ids: %v", ids)
	}
}

func pngBytes(t *testing.T) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for x := 0; x < 40; x++ {
		img.Set(x, 10, color.NRGBA{R: 255, A: 128})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestDocsExport_PDFGolden: the PDF parses (header, every xref offset
// lands on its object, trailer), carries its text in extractable form
// (Unicode and Arabic included), embeds the attached image, draws a grid
// for the table and breaks long documents across pages.
func TestDocsExport_PDFGolden(t *testing.T) {
	var asked []string
	out := PDF("Leave policy", sample, func(dest string) ([]byte, bool) {
		asked = append(asked, dest)
		return pngBytes(t), dest == "attachment:docm-123"
	})
	parsed := parsePDF(t, out)
	textContent := parsed.text()
	for _, want := range []string{"Leave policy", "paid", "leave", "monthly.", "Open", "request", "Nested", "[x]", "Quoted", "Germany", "Größe", "30", "func main() {}", "Org chart", "Handbook.pdf", "سياسة", "الإجازة"} {
		if !strings.Contains(textContent, want) {
			t.Errorf("PDF text is missing %q:\n%s", want, textContent)
		}
	}
	if strings.Count(textContent, "Leave policy") != 1 {
		t.Errorf("title repeated when the text opens with it: %q", textContent)
	}
	if strings.Join(asked, ",") != "attachment:docm-123" {
		t.Errorf("image source asked for %v", asked)
	}
	if !bytes.Contains(out, []byte("/Subtype /Image /Width 40 /Height 20")) {
		t.Error("attached image is not embedded")
	}
	if !strings.Contains(parsed.content, " re S") {
		t.Error("table grid not drawn")
	}
	if parsed.pages != 1 {
		t.Errorf("short document spans %d pages", parsed.pages)
	}

	long := strings.Repeat("A paragraph of policy text that wraps across the column several times over.\n\n", 120)
	longOut := PDF("Long", long, nil)
	longParsed := parsePDF(t, longOut)
	if longParsed.pages < 3 {
		t.Errorf("long document has %d pages", longParsed.pages)
	}
	if !strings.HasPrefix(longParsed.text(), "Long") {
		t.Errorf("title not set first: %.40q", longParsed.text())
	}
}

func TestDocsExport_PDFJPEGPassThrough(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	out := PDF("", "![photo](attachment:a)\n\n![missing](attachment:b)", func(dest string) ([]byte, bool) {
		if dest == "attachment:a" {
			return buf.Bytes(), true
		}
		return nil, false
	})
	if !bytes.Contains(out, []byte("/Filter /DCTDecode")) || !bytes.Contains(out, buf.Bytes()) {
		t.Fatal("JPEG was not passed through")
	}
	if text := parsePDF(t, out).text(); !strings.Contains(text, "[missing]") {
		t.Fatalf("unresolved image not labelled: %q", text)
	}
}

func TestDocsExport_WordSplitting(t *testing.T) {
	if got := strings.Join(splitWords("a  bc d"), "|"); got != "a|  |bc| |d" {
		t.Fatalf("splitWords = %q", got)
	}
	long := strings.Repeat("x", 400)
	text := parsePDF(t, PDF("", long, nil)).text()
	if strings.Count(text, "x") != 400 {
		t.Fatalf("an unbreakable word lost characters: %d", strings.Count(text, "x"))
	}
}

// --- a minimal PDF reader for the tests ------------------------------------

type parsedPDF struct {
	content string
	cmap    map[string]string
	pages   int
}

var (
	objHeader  = regexp.MustCompile(`^(\d+) 0 obj`)
	streamBody = regexp.MustCompile(`(?s)<<(.*?)>>\nstream\n`)
	hexShow    = regexp.MustCompile(`<([0-9A-F]*)> Tj`)
	bfchar     = regexp.MustCompile(`<([0-9A-F]{4})> <([0-9A-F]+)>`)
)

func parsePDF(t *testing.T, data []byte) parsedPDF {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("%PDF-1.7\n")) || !bytes.HasSuffix(data, []byte("%%EOF\n")) {
		t.Fatal("PDF header or trailer missing")
	}
	tail := string(data[bytes.LastIndex(data, []byte("startxref")):])
	xrefAt, err := strconv.Atoi(strings.Fields(tail)[1])
	if err != nil || !bytes.HasPrefix(data[xrefAt:], []byte("xref\n")) {
		t.Fatalf("startxref does not point at the xref table: %v", err)
	}
	lines := strings.Split(string(data[xrefAt:]), "\n")
	count, _ := strconv.Atoi(strings.Fields(lines[1])[1])
	for i := 1; i < count; i++ {
		offset, _ := strconv.Atoi(lines[2+i][:10])
		match := objHeader.FindSubmatch(data[offset:min(len(data), offset+20)])
		if match == nil || string(match[1]) != strconv.Itoa(i) {
			t.Fatalf("xref entry %d points at %q", i, data[offset:min(len(data), offset+20)])
		}
	}
	out := parsedPDF{cmap: map[string]string{}, pages: bytes.Count(data, []byte("/Type /Page /Parent"))}
	for _, loc := range streamBody.FindAllSubmatchIndex(data, -1) {
		dict := string(data[loc[2]:loc[3]])
		length, _ := strconv.Atoi(regexp.MustCompile(`/Length (\d+) *$`).FindStringSubmatch(strings.TrimSpace(dict) + " ")[1])
		body := data[loc[1] : loc[1]+length]
		if strings.Contains(dict, "/FlateDecode") && !strings.Contains(dict, "/Subtype /Image") {
			zr, err := zlib.NewReader(bytes.NewReader(body))
			if err != nil {
				t.Fatalf("stream does not inflate: %v", err)
			}
			body, err = io.ReadAll(zr)
			if err != nil {
				t.Fatal(err)
			}
		}
		switch {
		case bytes.Contains(body, []byte("beginbfchar")):
			for _, m := range bfchar.FindAllSubmatch(body, -1) {
				raw, _ := hex.DecodeString(string(m[2]))
				units := make([]uint16, len(raw)/2)
				for i := range units {
					units[i] = uint16(raw[2*i])<<8 | uint16(raw[2*i+1])
				}
				out.cmap[string(m[1])] = string(utf16.Decode(units))
			}
		case bytes.Contains(body, []byte(" Tj ET")):
			out.content += string(body)
		}
	}
	if len(out.cmap) == 0 || out.content == "" {
		t.Fatal("no ToUnicode map or no page content")
	}
	return out
}

// text is every shown run, decoded through the ToUnicode map and joined
// with spaces between runs.
func (p parsedPDF) text() string {
	var b strings.Builder
	for _, m := range hexShow.FindAllStringSubmatch(p.content, -1) {
		for i := 0; i+4 <= len(m[1]); i += 4 {
			b.WriteString(p.cmap[m[1][i:i+4]])
		}
		b.WriteString(" ")
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
