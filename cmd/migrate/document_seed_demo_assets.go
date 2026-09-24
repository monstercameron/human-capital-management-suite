package main

// Demo assets for the showcase documents: a PNG org chart, a PNG welcome
// badge and two small PDF handouts, all drawn in pure Go at seed time so the
// seed needs no binary fixtures. Every generator is deterministic, so an
// asset's content address is the same on every run and re-seeding never
// stores a second copy.
//
// The bytes are uploaded through documenthubstore.AddMedia into a
// documenthubstore.MediaFiles root, the same store the document media
// boundary serves attachments from.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"path/filepath"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/docsexport"
)

// EnvDocumentMediaRoot overrides where the document seed writes attachment
// bytes. It must name the root the serving stack reads.
const EnvDocumentMediaRoot = "HCMNEXT_DOCUMENT_MEDIA_ROOT"

// defaultDocumentMediaRoot matches the serve role's default: the
// document-media sibling of the chat media root.
var defaultDocumentMediaRoot = filepath.Join(defaultArtifactRootPath, "document-media")

// demoAsset is one generated file and its content address.
type demoAsset struct {
	Key, Filename, ContentType, Alt string
	Content                         []byte
}

// demoAssets builds every asset the showcase documents use, keyed by Key.
func demoAssets() map[string]demoAsset {
	list := []demoAsset{
		{Key: "orgchart", Filename: "people-ops-org-chart.png", ContentType: "image/png", Alt: "People Operations org chart", Content: peopleOpsOrgChartPNG()},
		{Key: "badge", Filename: "welcome-badge.png", ContentType: "image/png", Alt: "HarborCare new hire welcome badge", Content: welcomeBadgePNG()},
		{Key: "leave-pdf", Filename: "parental-leave-handout-2027.pdf", ContentType: "application/pdf", Content: simplePDF("Parental leave handout 2027", []string{
			"HarborCare People Operations",
			"",
			"Birthing parents: 16 weeks of fully paid leave.",
			"Non-birthing and adoptive parents: 12 weeks of fully paid leave.",
			"Leave may be taken in up to two blocks within 12 months.",
			"State paid family leave runs concurrently; HarborCare tops up to full base pay.",
			"Benefits continue unchanged throughout the leave.",
			"",
			"Request leave in Workspace at least 30 days ahead where you can.",
			"Questions: the #benefits channel or your People Partner.",
		})},
		{Key: "enroll-pdf", Filename: "open-enrollment-checklist-2027.pdf", ContentType: "application/pdf", Content: simplePDF("Open enrollment checklist 2027", []string{
			"Window: November 2 to November 20, 2026",
			"",
			"1. Review the 2027 plan comparison.",
			"2. Confirm dependents and their dates of birth.",
			"3. Choose a medical plan; the HSA plan includes a $750 contribution.",
			"4. Re-elect flexible spending accounts; they do not roll over.",
			"5. Submit before 5 p.m. Pacific on November 20.",
			"",
			"No election means your 2026 medical plan continues; FSA elections end.",
		})},
	}
	out := make(map[string]demoAsset, len(list))
	for _, a := range list {
		out[a.Key] = a
	}
	return out
}

var (
	demoNavy  = color.RGBA{38, 70, 128, 255}
	demoBlue  = color.RGBA{92, 140, 196, 255}
	demoPale  = color.RGBA{226, 236, 248, 255}
	demoLine  = color.RGBA{120, 130, 145, 255}
	demoWhite = color.RGBA{255, 255, 255, 255}
	demoInk   = color.RGBA{30, 34, 40, 255}
)

func fillRect(img *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(img, r, &image.Uniform{c}, image.Point{}, draw.Src)
}

// drawLabel centres text horizontally on cx with its baseline at y.
func drawLabel(img *image.RGBA, cx, y int, text string, c color.Color) {
	docsexport.DrawBitmapText(img, cx, y, text, c)
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// peopleOpsOrgChartPNG draws the People Operations structure by role, so
// the image does not depend on who the workforce seed put in each seat.
func peopleOpsOrgChartPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 640, 300))
	fillRect(img, img.Bounds(), color.RGBA{250, 250, 252, 255})
	box := func(cx, y, w int, fill color.Color, text string, ink color.Color) {
		fillRect(img, image.Rect(cx-w/2, y, cx+w/2, y+34), fill)
		drawLabel(img, cx, y+22, text, ink)
	}
	box(320, 24, 220, demoNavy, "Director, People Operations", demoWhite)
	fillRect(img, image.Rect(319, 58, 321, 90), demoLine)
	fillRect(img, image.Rect(90, 90, 552, 92), demoLine)
	leads := []struct {
		x     int
		title string
		team  []string
	}{
		{90, "HR Business Partners", []string{"Clinical", "Corporate"}},
		{244, "Benefits and Leave", []string{"Leave cases", "Enrollment"}},
		{398, "Talent Acquisition", []string{"Recruiting", "Onboarding"}},
		{552, "Payroll Liaison", []string{"Pay changes", "Audits"}},
	}
	for _, l := range leads {
		fillRect(img, image.Rect(l.x-1, 90, l.x+1, 112), demoLine)
		box(l.x, 112, 148, demoBlue, l.title, demoWhite)
		for j, t := range l.team {
			y := 172 + j*48
			fillRect(img, image.Rect(l.x-1, y-26, l.x+1, y), demoLine)
			box(l.x, y, 120, demoPale, t, demoInk)
		}
	}
	return encodePNG(img)
}

// welcomeBadgePNG draws the badge the onboarding checklist shows.
func welcomeBadgePNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	fillRect(img, img.Bounds(), demoWhite)
	fillRect(img, image.Rect(0, 0, 320, 48), demoNavy)
	drawLabel(img, 160, 30, "HarborCare", demoWhite)
	fillRect(img, image.Rect(24, 64, 88, 144), demoPale)
	fillRect(img, image.Rect(44, 76, 68, 100), demoBlue)
	fillRect(img, image.Rect(36, 106, 76, 136), demoBlue)
	drawLabel(img, 200, 90, "Welcome aboard", demoInk)
	drawLabel(img, 200, 112, "October 2026 cohort", demoInk)
	fillRect(img, image.Rect(112, 128, 288, 150), demoBlue)
	drawLabel(img, 200, 144, "People Operations", demoWhite)
	return encodePNG(img)
}

// simplePDF writes a one-page PDF 1.4 file: a title and body lines in
// Helvetica, with a correct cross-reference table so any reader opens it.
func simplePDF(title string, lines []string) []byte {
	var content strings.Builder
	content.WriteString("BT\n/F1 18 Tf\n72 720 Td\n")
	fmt.Fprintf(&content, "(%s) Tj\n/F1 11 Tf\n0 -30 Td\n", pdfEscape(title))
	for _, line := range lines {
		if line != "" {
			fmt.Fprintf(&content, "(%s) Tj\n", pdfEscape(line))
		}
		content.WriteString("0 -16 Td\n")
	}
	content.WriteString("ET\n")
	stream := content.String()
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// pdfEscape escapes a PDF literal string and drops anything outside
// printable ASCII, which the standard Helvetica encoding cannot show.
func pdfEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\' || r == '(' || r == ')':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r >= 0x20 && r < 0x7f:
			b.WriteRune(r)
		}
	}
	return b.String()
}
