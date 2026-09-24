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

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
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

	// HUB-033/HUB-035 evidence now comes from the served productui
	// components themselves (docs_editor.go, docs_editor_suggest.go,
	// docs_compare.go, docs_backlinks.go), not the tools/uxqual/render/docs
	// fixture renderer above: the G-L reachability audit found that
	// renderer sits outside the shipped binary's dependency closure, so it
	// could never prove the served app actually behaves this way.
	editorDoc, err := productui.DocsEditorFixture("en-US", "doc-1", "docv-1", "Guide", "# Guide\n\nNew words.\n")
	if err != nil {
		log.Fatalf("productui.DocsEditorFixture: %v", err)
	}
	editorConflictDoc, err := productui.DocsEditorConflictFixture("en-US")
	if err != nil {
		log.Fatalf("productui.DocsEditorConflictFixture: %v", err)
	}
	compareDoc, err := productui.DocsCompareFixture("en-US",
		productui.DocumentVersionProjection{DocumentID: "doc-1", VersionID: "docv-1", Title: "Guide", Markdown: "# Guide\n", Readable: true},
		productui.DocumentVersionProjection{DocumentID: "doc-1", VersionID: "docv-2", Title: "Guide", Markdown: "# Guide\n\nMore.\n", Readable: true},
	)
	if err != nil {
		log.Fatalf("productui.DocsCompareFixture: %v", err)
	}
	write("docs-editor.html", editorDoc)
	write("docs-editor-conflict.html", editorConflictDoc)
	write("docs-compare.html", compareDoc)

	pickerDoc, err := productui.DocsSuggestFixture("en-US", productui.DocsSuggestDocs, []productui.DocsReferenceSuggestion{
		{ID: "doc-b", Label: "Bee", Insert: productui.DocsSuggestDocInsert("doc-b", "Bee")},
		{ID: "doc-c", Label: "Sea", Insert: productui.DocsSuggestDocInsert("doc-c", "Sea")},
	})
	if err != nil {
		log.Fatalf("productui.DocsSuggestFixture: %v", err)
	}
	backlinksDoc, err := productui.DocsBacklinksFixture("en-US", []productui.DocumentBacklink{
		{SourceDocumentID: "doc-a", SourceVersionID: "docv-1", SourceTitle: "Ay", Label: "bee", State: ""},
		{SourceDocumentID: "doc-x", SourceVersionID: "docv-1", SourceTitle: "Restricted source", Label: "bee", State: "stale"},
	})
	if err != nil {
		log.Fatalf("productui.DocsBacklinksFixture: %v", err)
	}
	write("docs-picker.html", pickerDoc)
	write("docs-backlinks.html", backlinksDoc)

	// HUB-032/HUB-034/HUB-036 evidence now comes from productui.Render, the
	// exact served application render path (docsPage/docsLibrary,
	// docsReviewControls, docsSearch), not tools/uxqual/render/docs: the
	// traceability audit found that mirror renderer sits outside the shipped
	// binary's dependency closure, so it could never prove the served app
	// actually behaves this way.
	// The document set below is the identical fixture TestTodo_HUB_032 (PRIMARY,
	// internal/humanwork/productui/docs_page_test.go) already renders and
	// asserts against, so the Browser evidence and the Go PRIMARY evidence
	// exercise one proven presentation model rather than two.
	hubDoc, err := productui.DocsHubFixture("en-US", []productui.DocumentSummary{
		{ID: "doc-private", Title: "Private draft", Owner: "Taylor", OwnerID: "reader-a", Status: productui.DocumentPrivate, Version: "v2"},
		{ID: "doc-team", Title: "Team handbook", Owner: "People Ops", Status: productui.DocumentTeamOfficial, Version: "v7", Scope: "People Ops", ReviewDue: "2026-10-01", Sharing: "People Ops"},
	})
	if err != nil {
		log.Fatalf("productui.DocsHubFixture: %v", err)
	}
	write("docs-hub.html", hubDoc)

	reviewDoc, err := productui.DocsReviewFixture("en-US", []productui.DocumentReviewProjection{{
		DocumentID: "doc-team", VersionID: "version-7", VersionHash: "9e1e4a7c", Title: "Team handbook",
		Scope: "People Ops (team)", Diff: "- Old vacation policy text\n+ New vacation policy text",
		ReviewState: "pending", ReviewAction: "/docs/doc-team/review", DeployAction: "/docs/doc-team/deploy",
		CanReview: true, CanDeploy: true,
	}})
	if err != nil {
		log.Fatalf("productui.DocsReviewFixture: %v", err)
	}
	write("docs-review.html", reviewDoc)

	reviewStaleDoc, err := productui.DocsReviewFixture("en-US", []productui.DocumentReviewProjection{{
		DocumentID: "doc-team", VersionID: "version-7", VersionHash: "9e1e4a7c", Title: "Team handbook",
		Scope: "People Ops (team)", Diff: "- Old vacation policy text\n+ New vacation policy text",
		ReviewState: "approved", ReviewAction: "/docs/doc-team/review", DeployAction: "/docs/doc-team/deploy",
		CanReview: false, CanDeploy: false,
	}})
	if err != nil {
		log.Fatalf("productui.DocsReviewFixture (stale authority): %v", err)
	}
	write("docs-review-stale.html", reviewStaleDoc)

	searchDoc, err := productui.DocsSearchFixture("en-US", productui.DocumentSearchProjection{
		Ready: true, Query: "handbook", Mode: "keyword", SemanticAvailable: false, FallbackUsed: true,
		Filters: productui.DocumentSearchFilters{Team: "People Ops", Status: "team_official"},
		Results: []productui.DocumentSearchResult{
			{DocumentID: "doc-team", VersionID: "version-7", Title: "Team handbook", Owner: "People Ops", Scope: "People Ops", Why: "keyword", Snippet: "The <handbook> covers <script>alert(1)</script> leave requests."},
			{DocumentID: "doc-onboard", VersionID: "version-3", Title: "Onboarding guide", Owner: "People Ops", Scope: "People Ops", Why: "semantic", Snippet: "New hires start here."},
		},
	})
	if err != nil {
		log.Fatalf("productui.DocsSearchFixture: %v", err)
	}
	write("docs-search.html", searchDoc)
}
