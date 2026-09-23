// Command genfixtures writes the SSR and GWC renderers' real output for the
// shared Promotion workspace fixture (tools/uxqual/testdata) to
// tools/uxqual/testdata/rendered/{ssr,gwc}.html, for the Playwright browser
// pass in tools/uxqual/browser (a real, non-Go process; it cannot call the
// Go renderers directly, so it loads these static files instead).
//
// Run it before `npx playwright test --config=tools/uxqual/browser/playwright.config.ts`:
//
//	go run ./tools/uxqual/cmd/genfixtures
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/docs"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/testdata"
)

func main() {
	fixture := testdata.PromotionFixture()

	ssrDoc, err := ssr.Render(fixture)
	if err != nil {
		log.Fatalf("ssr.Render: %v", err)
	}
	gwcDoc, err := gwc.Document(fixture)
	if err != nil {
		log.Fatalf("gwc.Document: %v", err)
	}

	outDir := filepath.Join("tools", "uxqual", "testdata", "rendered")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", outDir, err)
	}
	write := func(name, content string) {
		path := filepath.Join(outDir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			log.Fatalf("write %s: %v", path, err)
		}
		log.Printf("wrote %s (%d bytes)", path, len(content))
	}
	write("ssr.html", ssrDoc)
	write("gwc.html", gwcDoc)

	editorDoc, err := docs.RenderEditor(docs.EditorPage{
		Locale: "en-US", Title: "Guide", DocumentID: "doc-1",
		BaseVersionID: "docv-1", BaseHash: "9e1e",
		Draft: "# Guide\n\nNew words.\n", Action: "/docs/doc-1/candidates",
		Conflict: &docs.BaseConflict{ExpectedVersionID: "docv-1", LiveVersionID: "docv-2"},
	})
	if err != nil {
		log.Fatalf("docs.RenderEditor: %v", err)
	}
	compareDoc, err := docs.RenderCompare(docs.ComparePage{
		Locale: "en-US", Title: "Guide",
		Base:  docs.CompareVersion{VersionID: "docv-1", ShortHash: "9e1e", Title: "Guide", BodyHTML: "<h1>Guide</h1>"},
		Other: docs.CompareVersion{VersionID: "docv-2", ShortHash: "a71f", Title: "Guide", BodyHTML: "<h1>Guide</h1><p>More.</p>"},
	})
	if err != nil {
		log.Fatalf("docs.RenderCompare: %v", err)
	}
	write("docs-editor.html", editorDoc)
	write("docs-compare.html", compareDoc)

	pickerDoc, err := docs.RenderPicker(docs.PickerPage{
		Locale: "en-US", Title: "Guide",
		Docs: []docs.PickerDoc{{ID: "doc-b", Title: "Bee"}, {ID: "doc-c", Title: "Sea"}},
	})
	if err != nil {
		log.Fatalf("docs.RenderPicker: %v", err)
	}
	backlinksDoc, err := docs.RenderBacklinks(docs.BacklinksPage{
		Locale: "en-US", Title: "Bee", TargetDocID: "doc-b",
		Links: []docs.BacklinkEntry{
			{SourceDocID: "doc-a", SourceTitle: "Ay", Label: "bee", State: "valid"},
			{SourceDocID: "doc-x", Label: "bee", State: "stale", Restricted: true},
		},
	})
	if err != nil {
		log.Fatalf("docs.RenderBacklinks: %v", err)
	}
	write("docs-picker.html", pickerDoc)
	write("docs-backlinks.html", backlinksDoc)
}
