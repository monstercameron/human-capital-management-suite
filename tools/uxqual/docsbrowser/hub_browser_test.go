// Package docsbrowser holds the BROWSER matrix evidence for HUB-032 through
// HUB-036 (planning/todos.md): a traceability audit reopened those todos
// because their TEST MATRIX declares a BROWSER function
// (TestTodo_HUB_0xx_Browser) that did not exist under that exact name --
// only a Playwright spec, which plancheck cannot validate as named
// automated coverage.
//
// This package lives under tools/uxqual rather than
// internal/humanwork/productui because it imports tools/uxqual/qual, and
// ARCH-GO-002/depedge's RuleProductImportsTools forbids internal/ or cmd/
// packages (which become part of shipped release binaries) from importing
// tools/: the dependency direction must run from developer tooling into
// product code, never back.
//
// Every test here renders the exact served page (productui.Render, the
// same function internal/humanwork/productui/render.go exposes and the
// TestTodo_HUB_0xx PRIMARY tests in docs_page_test.go, hub_033_served_test.go
// and hub_035_served_test.go already use) and scores it against
// tools/uxqual/qual's structural accessibility fixture (the same
// keyboard/landmark/label/live-region/contrast/reflow criteria
// UX-QUAL-001 scores the Promotion workspace with), then asserts each
// todo's own GREEN-specific state in the rendered markup.
package docsbrowser

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

// scoreDocument runs the shared structural accessibility fixture over one
// rendered document and fails the test with every criterion's detail, so a
// regression names exactly what broke rather than just "not accessible".
func scoreDocument(t *testing.T, doc string) {
	t.Helper()
	checks := []qual.CriterionResult{
		qual.CheckKeyboard(doc),
		qual.CheckScreenReaderSemantics(doc),
		qual.CheckContrastAA(),
		qual.CheckReflow(qual.ExtractInlineCSS(doc)),
	}
	for _, c := range checks {
		t.Logf("%s: pass=%v (%s)", c.Name, c.Pass, c.Detail)
		if !c.Pass {
			t.Errorf("rendered document failed %q: %s", c.Name, c.Detail)
		}
	}
}

// TestTodo_HUB_032_Browser is HUB-032's BROWSER matrix test: the workspace
// document hub, rendered through the exact served path (productui.Render
// over docsPage/docsLibrary), passes the shared structural accessibility
// fixture and distinguishes a private draft from team-official guidance by
// owner, deployed version, official scope, review date and sharing state.
func TestTodo_HUB_032_Browser(t *testing.T) {
	view := productui.NewView(productui.PageDocs, "tenant-a", "reader-a", "scope-a")
	view.DocumentsReady = true
	view.Documents = []productui.DocumentSummary{
		{ID: "doc-private", Title: "Private draft", Owner: "Taylor", OwnerID: "reader-a", Status: productui.DocumentPrivate, Version: "v2"},
		{ID: "doc-team", Title: "Team handbook", Owner: "People Ops", Status: productui.DocumentTeamOfficial, Version: "v7", Scope: "People Ops", ReviewDue: "2026-10-01", Sharing: "People Ops"},
	}
	doc, err := productui.Render(view)
	if err != nil {
		t.Fatal(err)
	}
	scoreDocument(t, doc)

	for _, want := range []string{
		"docs-kind-private", "docs-kind-team_official",
		"Private draft", "Only you",
		"Team handbook", "People Ops", "v7", "2026-10-01", "Team guidance",
		`href="/workspace/app/docs?document=doc-private"`, `href="/workspace/app/docs?document=doc-team"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("hub document omitted %q", want)
		}
	}
}

// TestTodo_HUB_033_Browser is HUB-033's BROWSER matrix test: the served
// candidate editor mounts against the document's current version as its
// expected base, and passes the shared structural accessibility fixture
// (labelled toolbar, live status region, native form controls in DOM
// order).
func TestTodo_HUB_033_Browser(t *testing.T) {
	view := productui.NewView(productui.PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Document = &productui.DocumentDetail{
		Summary:  productui.DocumentSummary{ID: "doc-42", Title: "Handbook", VersionID: "version-7"},
		Markdown: "# Current\n\nBody text.",
		CanEdit:  true,
	}
	view.DocumentEditing = true
	view.CreateDocumentVersion = func(productui.DocumentEditRequest, func(error)) {}
	view.CompareDocumentVersions = func(string, string, func(productui.DocumentVersionProjection, error)) {}
	doc, err := productui.Render(view)
	if err != nil {
		t.Fatal(err)
	}
	scoreDocument(t, doc)

	for _, want := range []string{
		`id="docs-editor"`, `data-document-id="doc-42"`, `data-version-id="version-7"`,
		`data-docs-action="compare"`,
		`role="toolbar"`, `id="docs-editor-status"`, `aria-live="polite"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("editor document omitted %q", want)
		}
	}
}

// TestTodo_HUB_034_Browser is HUB-034's BROWSER matrix test: the served
// reviewer/publisher screen shows the exact diff, content hash and deploy
// scope, review and deploy forms for one candidate version always carry the
// identical version hash, and a version whose current authority no longer
// covers review or deploy renders no form for either action. Every state
// passes the shared structural accessibility fixture.
func TestTodo_HUB_034_Browser(t *testing.T) {
	authorized := productui.NewView(productui.PageDocs, "tenant-a", "reader-a", "scope-a")
	authorized.DocumentsReady = true
	authorized.DocumentReviews = []productui.DocumentReviewProjection{{
		DocumentID: "doc-team", VersionID: "version-7", VersionHash: "9e1e4a7c", Title: "Team handbook",
		Scope: "People Ops (team)", Diff: "- Old vacation policy text\n+ New vacation policy text",
		ReviewState: "pending", ReviewAction: "/docs/doc-team/review", DeployAction: "/docs/doc-team/deploy",
		CanReview: true, CanDeploy: true,
	}}
	doc, err := productui.Render(authorized)
	if err != nil {
		t.Fatal(err)
	}
	scoreDocument(t, doc)

	for _, want := range []string{
		`action="/docs/doc-team/review"`, `action="/docs/doc-team/deploy"`,
		"9e1e4a7c", "People Ops (team)", "Old vacation policy text", "New vacation policy text",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("review document omitted %q", want)
		}
	}
	// The review and deploy forms for this one item must carry the literal
	// same hash: three occurrences (the visible fact plus both hidden
	// fields), never a divergent one.
	if strings.Count(doc, "9e1e4a7c") < 3 {
		t.Fatalf("version hash did not appear in the visible facts and both action forms: %s", doc)
	}

	stale := productui.NewView(productui.PageDocs, "tenant-a", "reader-a", "scope-a")
	stale.DocumentsReady = true
	stale.DocumentReviews = []productui.DocumentReviewProjection{{
		DocumentID: "doc-team", VersionID: "version-7", VersionHash: "9e1e4a7c", Title: "Team handbook",
		Scope: "People Ops (team)", Diff: "- Old vacation policy text\n+ New vacation policy text",
		ReviewState: "approved", ReviewAction: "/docs/doc-team/review", DeployAction: "/docs/doc-team/deploy",
		CanReview: false, CanDeploy: false,
	}}
	staleDoc, err := productui.Render(stale)
	if err != nil {
		t.Fatal(err)
	}
	scoreDocument(t, staleDoc)
	// docsReviewControls independently checks CanReview/CanDeploy per render
	// and drops an item entirely once both are false, rather than rendering
	// a disabled form: no form, no hash, no diff for the out-of-authority
	// candidate reaches the page.
	for _, forbidden := range []string{
		`action="/docs/doc-team/review"`, `action="/docs/doc-team/deploy"`,
		"9e1e4a7c", "Old vacation policy text",
	} {
		if strings.Contains(staleDoc, forbidden) {
			t.Errorf("stale-authority document unexpectedly exposed %q", forbidden)
		}
	}
}

// TestTodo_HUB_035_Browser is HUB-035's BROWSER matrix test: the served
// reader marks an unreadable doc: link target's safe state before it is
// ever clicked, a readable target's authorized title renders, and the
// backlinks panel lists only jointly-readable inbound links. Every state
// passes the shared structural accessibility fixture.
func TestTodo_HUB_035_Browser(t *testing.T) {
	view := productui.NewView(productui.PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Document = &productui.DocumentDetail{
		Summary:  productui.DocumentSummary{ID: "doc-1", Title: "Handbook", VersionID: "version-1"},
		Markdown: "# Handbook\n\nSee [Expense Policy](doc:doc-77) and [Confidential HR file](doc:doc-88) for details.",
		Links: []productui.DocumentLinkTarget{
			{DocumentID: "doc-77", Title: "Expense Policy", Readable: true},
			{DocumentID: "doc-88", Readable: false},
		},
	}
	view.LoadDocumentBacklinks = func(string, func([]productui.DocumentBacklink, error)) {}
	doc, err := productui.Render(view)
	if err != nil {
		t.Fatal(err)
	}
	scoreDocument(t, doc)

	for _, want := range []string{
		`href="/workspace/app/docs?document=doc-77"`, "Expense Policy",
		`data-docs-id="/workspace/app/docs?document=doc-88"`, `docs-link-unavailable`, `docs-link-state="unavailable"`,
		`id="docs-backlinks-heading"`, `aria-labelledby="docs-backlinks-heading"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("link/backlinks document omitted %q", want)
		}
	}
}

// TestTodo_HUB_036_Browser is HUB-036's BROWSER matrix test: the served
// search screen labels result provenance, offers filters, renders snippets
// as safe escaped text, and keeps lexical results on screen with a visible
// fallback notice when semantic search is unavailable. It passes the shared
// structural accessibility fixture.
func TestTodo_HUB_036_Browser(t *testing.T) {
	view := productui.NewView(productui.PageDocs, "tenant-a", "reader-a", "scope-a")
	view.DocumentsReady = true
	view.DocumentSearch = productui.DocumentSearchProjection{
		Ready: true, Query: "handbook", Mode: "keyword", SemanticAvailable: false, FallbackUsed: true,
		Filters: productui.DocumentSearchFilters{Team: "People Ops", Status: "team_official"},
		Results: []productui.DocumentSearchResult{
			{DocumentID: "doc-team", VersionID: "version-7", Title: "Team handbook", Owner: "People Ops", Snippet: "<script>alert(1)</script> leave", Why: "keyword"},
			{DocumentID: "doc-onboard", VersionID: "version-3", Title: "Onboarding guide", Owner: "People Ops", Snippet: "New hires start here.", Why: "semantic"},
		},
	}
	doc, err := productui.Render(view)
	if err != nil {
		t.Fatal(err)
	}
	scoreDocument(t, doc)

	for _, want := range []string{
		`role="search"`, `name="docs_q"`, `value="handbook"`,
		`id="docs-search-team"`, `id="docs-search-channel"`, `id="docs-search-owner"`, `id="docs-search-status"`,
		`id="docs-search-date-from"`, `id="docs-search-date-to"`,
		"New hires start here.", "Keyword match", "Semantic match", `href="/workspace/app/docs?document=doc-team"`,
		"Semantic search is unavailable",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("search document omitted %q", want)
		}
	}
	// RED: a vector outage must never blank all results or hide the exact
	// matching document, and a snippet with markup-shaped text must never
	// execute -- html/template autoescapes it.
	if strings.Contains(doc, "<script>alert(1)</script>") {
		t.Fatal("search snippet was rendered as executable HTML")
	}
	if !strings.Contains(doc, "&lt;script&gt;") {
		t.Fatal("search snippet was not escaped")
	}
	if !strings.Contains(doc, "Team handbook") {
		t.Fatal("vector-outage fallback hid an exact matching document")
	}
}
