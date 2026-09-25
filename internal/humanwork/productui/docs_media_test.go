package productui

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func docsMediaTestPort() *DocumentMediaPort {
	return &DocumentMediaPort{
		List:      func(string, func([]DocumentAttachment, error)) {},
		Upload:    func(string, string, []byte, func(int64, int64), func(DocumentAttachment, error)) {},
		ObjectURL: func(string, string, func(string, error)) {},
		Open:      func(string, string, func(error)) {},
		Download:  func(string, string, func(error)) {},
		Export:    func(string, string, func(error)) {},
	}
}

func renderDocsMarkdown(t *testing.T, view View, markdown string) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(docsMarkdownBody, docsMarkdownBodyProps{Locale: view.Locale.Resolved, VersionID: "v1", Markdown: markdown, DocumentID: view.DocumentID, Media: view.DocumentMedia}))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// TestDocsMedia_ReaderRendersAttachments: an attachment image renders as a
// labelled placeholder until its authenticated URL resolves, a PDF link as a
// card with its name, and ordinary images and links keep their old rendering.
// No style attribute is ever written.
func TestDocsMedia_ReaderRendersAttachments(t *testing.T) {
	port := docsMediaTestPort()
	var imageCalls int
	port.ObjectURL = func(documentID, attachmentID string, done func(string, error)) {
		imageCalls++
		if documentID != "doc-1" || attachmentID != "docm-1" {
			t.Errorf("unexpected image request: %s/%s", documentID, attachmentID)
		}
	}
	view := View{Locale: LocaleContext{Resolved: "en-US"}, DocumentID: "doc-1", DocumentMedia: port}
	markup := renderDocsMarkdown(t, view, "Intro\n\n![Org chart](attachment:docm-1)\n\n[Policy.pdf](attachment:docm-2)\n\n![remote](https://evil.example/x.png) and [site](https://example.com)\n")
	if imageCalls != 1 {
		t.Fatalf("image requests = %d, want one render-time request", imageCalls)
	}
	for _, want := range []string{
		`class="docs-media-figure"`, `data-attachment-id="docm-1"`, `role="img"`, `aria-label="Org chart"`, `aria-busy="true"`, "Loading image…",
		`class="docs-media-card"`, `data-attachment-id="docm-2"`, "Policy.pdf", `aria-label="Open Policy.pdf"`, `aria-label="Download Policy.pdf"`, `data-media-action="download"`,
		`href="https://example.com"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("reader markup lacks %q:\n%s", want, markup)
		}
	}
	for _, forbidden := range []string{"evil.example", "<img", "style="} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("reader markup contains %q", forbidden)
		}
	}
	offline := renderDocsMarkdown(t, View{Locale: LocaleContext{Resolved: "de-DE"}, DocumentID: "doc-1"}, "![Diagramm](attachment:docm-1)\n\n[Plan.pdf](attachment:docm-2)")
	if !strings.Contains(offline, "Bild nicht verfügbar") || strings.Contains(offline, `data-media-action`) || !strings.Contains(offline, "Plan.pdf") {
		t.Errorf("reader without a media port:\n%s", offline)
	}
	hostile := renderDocsMarkdown(t, view, "![x](attachment:../../etc) [y](attachment:a%2Fb)")
	if strings.Contains(hostile, "docs-media-") {
		t.Errorf("malformed attachment ids were accepted:\n%s", hostile)
	}
}

func TestDocsMedia_AttachmentsSectionAndExportMenu(t *testing.T) {
	section, err := ui.RenderToString(ui.CreateElement(docsAttachmentsSection, docsAttachmentsSectionProps{Locale: "ar", DocumentID: "doc-1", Media: docsMediaTestPort()}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(section, "hidden") || !strings.Contains(section, "المرفقات") || !strings.Contains(section, `aria-labelledby="docs-attachments-heading"`) {
		t.Fatalf("empty attachments section should be hidden and labelled:\n%s", section)
	}
	for locale, want := range map[string][]string{
		"en-US": {"Export", "Plain text (.txt)", "Markdown (.md)", "PDF (.pdf)"},
		"de-DE": {"Exportieren", "Nur Text (.txt)"},
		"ar":    {"تصدير", "نص عادي (.txt)"},
	} {
		menu, err := ui.RenderToString(ui.CreateElement(docsExportMenu, docsExportMenuProps{Locale: locale, DocumentID: "doc-1", Media: docsMediaTestPort()}))
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range append(want, `data-media-action="pdf"`, `class="docs-menu-item"`, `role="group"`) {
			if !strings.Contains(menu, w) {
				t.Errorf("%s export menu lacks %q", locale, w)
			}
		}
	}
	rows := docsAttachmentRow("de-DE", DocumentAttachment{ID: "a", Filename: "Plan.pdf", MediaType: "application/pdf", Size: 1536, Pages: 3}, "", true, "")
	markup, err := ui.RenderToString(ui.CreateElement(func(struct{}) ui.Node { return ui.Fragment(rows...) }, struct{}{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "PDF · 1,5 KB · 3 Seiten") || !strings.Contains(markup, "Plan.pdf herunterladen") {
		t.Errorf("attachment row: %s", markup)
	}
}

// TestDocsMedia_EditorTools: the split editor offers Insert image and
// Attach PDF only with a media port, and always keeps its upload status
// region so the panes' siblings never change.
func TestDocsMedia_EditorTools(t *testing.T) {
	props := docsSplitEditorProps{Locale: "de-DE", DocumentID: "doc-1", BaseVersionID: "v1", Title: "T", Markdown: "Text", Save: func(DocumentEditRequest, func(error)) {}, Cancel: func() {}, Media: docsMediaTestPort()}
	markup, err := ui.RenderToString(ui.CreateElement(docsSplitEditor, props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="docs-editor-insert-image"`, `aria-label="Bild einfügen"`, `aria-label="PDF anhängen"`, `id="docs-editor-media-status"`, `aria-live="polite"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("editor lacks %q", want)
		}
	}
	props.Media = nil
	markup, err = ui.RenderToString(ui.CreateElement(docsSplitEditor, props))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "docs-editor-insert-image") || !strings.Contains(markup, `id="docs-editor-media-status"`) {
		t.Errorf("editor without media port:\n%s", markup)
	}
}

// TestDocsMedia_EditorUpload drives an upload through a fake port: the
// progress and outcome are reported in the locale, a refusal says why, an
// oversized file never leaves the page, and the result is inserted as
// Markdown in the text.
func TestDocsMedia_EditorUpload(t *testing.T) {
	var reports []docsEditorUploadStatus
	report := func(status docsEditorUploadStatus) { reports = append(reports, status) }
	sent := 0
	result := DocumentAttachment{ID: "docm-9", Filename: "Org chart.png", MediaType: "image/png"}
	var failure error
	port := docsMediaTestPort()
	port.Upload = func(documentID, name string, content []byte, progress func(int64, int64), done func(DocumentAttachment, error)) {
		sent++
		if documentID != "doc-1" || name != "Org chart.png" {
			t.Fatalf("upload got %q %q", documentID, name)
		}
		progress(50, 100)
		progress(55, 100)
		progress(100, 100)
		done(result, failure)
	}
	c := newDocsEditorController("en-US", "T", "Intro")
	docsEditorUpload(c, "en-US", "doc-1", port, `C:\fakepath\Org chart.png`, []byte("\x89PNG"), report)
	if c.markdown != "Intro\n\n![Org chart](attachment:docm-9)\n" || !c.dirty {
		t.Fatalf("inserted markdown = %q", c.markdown)
	}
	texts := []string{}
	for _, r := range reports {
		texts = append(texts, r.text)
	}
	if strings.Join(texts, "|") != "Uploading Org chart.png… 0%|Uploading Org chart.png… 50%|Uploading Org chart.png… 100%|Org chart.png added." {
		t.Fatalf("progress reports = %q", texts)
	}

	reports = nil
	failure = ErrDocumentMediaUnsupported
	docsEditorUpload(c, "ar", "doc-1", port, "Org chart.png", []byte("x"), report)
	last := reports[len(reports)-1]
	if !last.alert || !strings.Contains(last.text, "لا يمكن إضافة Org chart.png") {
		t.Fatalf("refusal = %+v", last)
	}
	for err, want := range map[error]string{ErrDocumentMediaTooLarge: "too large", ErrDocumentMediaForbidden: "can't add files", errors.New("x"): "could not be uploaded"} {
		reports, failure = nil, err
		docsEditorUpload(c, "en-US", "doc-1", port, "Org chart.png", []byte("x"), report)
		if last := reports[len(reports)-1]; !last.alert || !strings.Contains(last.text, want) {
			t.Errorf("%v: %+v", err, last)
		}
	}

	before := sent
	reports = nil
	docsEditorUpload(c, "en-US", "doc-1", port, "big.png", make([]byte, docsMediaImageCap+1), report)
	if sent != before || len(reports) != 1 || !reports[0].alert || !strings.Contains(reports[0].text, "10 MB") {
		t.Fatalf("oversized image was sent or not refused: %+v", reports)
	}
	docsEditorUpload(c, "en-US", "doc-1", nil, "x.png", []byte("x"), report)
	if sent != before {
		t.Fatal("upload without a port")
	}
}

func TestDocsMedia_Helpers(t *testing.T) {
	for _, c := range []struct {
		value      string
		start, end int
		want       string
		caret      int
	}{
		{"One two", 3, 3, "One\n\nX\n\ntwo", 6},
		{"", 0, 0, "X\n", 1},
		{"Keep this", 4, 9, "Keep\n\nX\n", 7},
	} {
		next, caret, _ := docsMediaInsertBlock(c.value, c.start, c.end, "X")
		if next != c.want || caret != c.caret {
			t.Errorf("docsMediaInsertBlock(%q,%d,%d) = %q,%d", c.value, c.start, c.end, next, caret)
		}
	}
	for _, c := range []struct {
		locale string
		size   int64
		want   string
	}{{"en-US", 512, "512 B"}, {"en-US", 2048, "2.0 KB"}, {"de-DE", 3 << 20, "3,0 MB"}, {"ar", 1536, "١٫٥ كيلوبايت"}} {
		if got := docsMediaSize(c.locale, c.size); got != c.want {
			t.Errorf("docsMediaSize(%s,%d) = %q, want %q", c.locale, c.size, got, c.want)
		}
	}
	if docsMediaPages("de-DE", 1) != "1 Seite" || docsMediaPages("ar", 12) != "١٢ صفحات" {
		t.Error("page counts")
	}
	for in, ok := range map[string]bool{"attachment:docm-1": true, " attachment:a_b ": true, "attachment:": false, "attachment:a/b": false, "https://x": false, "attachment:" + strings.Repeat("a", 129): false} {
		if _, got := docsAttachmentID(in); got != ok {
			t.Errorf("docsAttachmentID(%q) = %v", in, got)
		}
	}
	if got := docsMediaMarkdown(DocumentAttachment{ID: "a", Filename: "Plan [v2].pdf", MediaType: "application/pdf"}, "x"); got != "[Plan v2.pdf](attachment:a)" {
		t.Errorf("pdf markdown = %q", got)
	}
	if got := docsMediaMarkdown(DocumentAttachment{ID: "b", MediaType: "image/gif"}, "loop.gif"); got != "![loop](attachment:b)" {
		t.Errorf("image markdown = %q", got)
	}
	if docsMediaDisplayName("") != "file" || docsMediaDisplayName("a/b/c.png") != "c.png" {
		t.Error("display name")
	}
	for key := range docsMediaCopy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			if docsMediaCopy[locale][key] == "" {
				t.Errorf("%s lacks %q", locale, key)
			}
		}
	}
	if len(docsMediaCopy["de-DE"]) != len(docsMediaCopy["en-US"]) || len(docsMediaCopy["ar"]) != len(docsMediaCopy["en-US"]) {
		t.Error("locales carry keys en-US does not")
	}
	css := docsMediaStylesheet()
	for _, forbidden := range []string{"left", "right"} {
		if strings.Contains(css, forbidden) {
			t.Errorf("media stylesheet uses physical %q", forbidden)
		}
	}
	if !strings.Contains(css, "--hcm-color-focus") || !strings.Contains(css, "prefers-reduced-motion") {
		t.Error("media stylesheet lacks focus ring or reduced motion")
	}
	if !strings.Contains(docsStylesheet(), ".docs-media-card{") {
		t.Error("media stylesheet is not part of the Docs stylesheet")
	}
}
