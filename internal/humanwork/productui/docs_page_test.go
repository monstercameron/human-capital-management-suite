package productui

import (
	"errors"
	"strings"
	"testing"
)

func TestTodo_HUB_032(t *testing.T) {
	definition, ok := LookupPage(PageDocs)
	if !ok || definition.Route != "/workspace/app/docs" {
		t.Fatalf("docs route missing: %+v", definition)
	}
	if roundTrip, ok := LookupRoute(definition.Route); !ok || roundTrip.ID != PageDocs {
		t.Fatal("docs route does not round-trip")
	}
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.DocumentsReady = true
	view.Documents = []DocumentSummary{
		{ID: "doc-private", Title: "Private draft", Owner: "Taylor", OwnerID: "reader-a", Status: DocumentPrivate, Version: "v2"},
		{ID: "doc-team", Title: "Team handbook", Owner: "People Ops", Status: DocumentTeamOfficial, Version: "v7", Scope: "People Ops", ReviewDue: "2026-10-01", Sharing: "People Ops"},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	// HUB-032: the hub distinguishes private drafts from official guidance
	// and shows owner, deployed version, official scope, review date and
	// sharing state.
	for _, text := range []string{"Private draft", "Team handbook", "People Ops", "Version v7", "Review due 2026-10-01", "Private", "Team guidance", "docs-kind-private", "docs-kind-team_official", `href="/workspace/app/docs?document=doc-private"`, `href="/workspace/app/docs?document=doc-team"`} {
		if !strings.Contains(doc, text) {
			t.Fatalf("docs page omitted %q", text)
		}
	}
	if strings.Contains(doc, "Unpublished secret") {
		t.Fatal("docs page invented a document")
	}
}

func TestTodo_HUB_032_Accessibility(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		view := ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") || !strings.Contains(doc, "role=\"status\"") {
			t.Fatalf("docs empty state is not accessible in %s", locale)
		}
	}
}

// TestTodo_HUB_034 is the PRIMARY test for HUB-034: the review screen
// displays the exact diff, content hash and deploy scope for a candidate
// version, and review/deploy forms appear only under current, independently
// checked authority.
func TestTodo_HUB_034(t *testing.T) {
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.DocumentsReady = true
	view.DocumentReviews = []DocumentReviewProjection{{
		DocumentID: "doc-team", VersionID: "version-7", VersionHash: "9e1e4a7c", Title: "Team handbook",
		Scope: "People Ops (team)", Diff: "- Old vacation text\n+ New vacation text",
		ReviewState: "pending", ReviewAction: "/docs/doc-team/review", DeployAction: "/docs/doc-team/deploy",
		CanReview: true, CanDeploy: true,
	}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`action="/docs/doc-team/review"`, `action="/docs/doc-team/deploy"`, `name="version_id"`, "version-7",
		"9e1e4a7c", "People Ops (team)", "Old vacation text", "New vacation text",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("review controls omitted %q", want)
		}
	}
	view.DocumentReviews[0].CanReview = false
	view.DocumentReviews[0].CanDeploy = false
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `/docs/doc-team/review`) || strings.Contains(doc, `/docs/doc-team/deploy`) {
		t.Fatal("unauthorized review or deploy action rendered")
	}
}

// TestTodo_HUB_034_Security is the SECURITY test for HUB-034: the review
// and deploy forms for one candidate version always carry the identical
// content hash from the same authorized projection, so this UI layer has
// no path that could show a reviewer one hash while submitting a different
// one to deploy; and deploy authority is rechecked independently of review
// authority and independently of a prior "approved" review state, not
// cached from an earlier render.
func TestTodo_HUB_034_Security(t *testing.T) {
	review := DocumentReviewProjection{
		DocumentID: "doc-team", VersionID: "version-7", VersionHash: "9e1e4a7c", Title: "Team handbook",
		Scope: "People Ops (team)", ReviewState: "pending",
		ReviewAction: "/docs/doc-team/review", DeployAction: "/docs/doc-team/deploy",
		CanReview: true, CanDeploy: true,
	}
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.DocumentsReady = true
	view.DocumentReviews = []DocumentReviewProjection{review}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	// The exact hash must appear in the visible facts AND in both the
	// review and deploy forms' hidden version_hash field: three
	// occurrences, none of them a different value.
	if strings.Count(doc, "9e1e4a7c") < 3 {
		t.Fatalf("version hash did not appear in the visible facts and both action forms: %s", doc)
	}
	if strings.Contains(doc, `name="version_hash" value="9e1e"`) {
		t.Fatal("a divergent, truncated hash was rendered")
	}

	// A previously approved review does not grant deploy authority by
	// itself: CanDeploy is independently false (authority revoked or never
	// held) and the deploy form must not render even though CanReview and
	// ReviewState both say the version was already reviewed.
	stale := review
	stale.ReviewState = "approved"
	stale.CanReview = true
	stale.CanDeploy = false
	view.DocumentReviews = []DocumentReviewProjection{stale}
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `action="/docs/doc-team/deploy"`) {
		t.Fatal("deploy action rendered for a version whose current deploy authority is false")
	}
	if !strings.Contains(doc, `action="/docs/doc-team/review"`) {
		t.Fatal("review action incorrectly withheld when only deploy authority is false")
	}

	// Symmetric case: deploy authority present without review authority
	// (e.g. a publisher who is not the reviewer) still requires its own
	// exact hash and must not imply review authority.
	publisherOnly := review
	publisherOnly.CanReview = false
	publisherOnly.CanDeploy = true
	view.DocumentReviews = []DocumentReviewProjection{publisherOnly}
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `action="/docs/doc-team/review"`) {
		t.Fatal("review action rendered without review authority")
	}
	if !strings.Contains(doc, `action="/docs/doc-team/deploy"`) || !strings.Contains(doc, "9e1e4a7c") {
		t.Fatal("deploy action or its exact hash missing for an authorized publisher")
	}
}

// TestTodo_HUB_036 is the PRIMARY test for HUB-036: search results label
// their provenance, filters are offered, snippets render as safe text, and
// a vector-backend outage keeps lexical results on screen with a visible
// fallback notice instead of blanking the page.
func TestTodo_HUB_036(t *testing.T) {
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.DocumentsReady = true
	view.DocumentSearch = DocumentSearchProjection{
		Ready: true, Query: "handbook", Mode: "keyword", SemanticAvailable: false, FallbackUsed: true,
		Filters: DocumentSearchFilters{Team: "People Ops", Status: "team_official"},
		Results: []DocumentSearchResult{
			{DocumentID: "doc-team", VersionID: "version-7", Title: "Team handbook", Owner: "People Ops", Snippet: "<script>alert(1)</script> leave", Why: "keyword"},
			{DocumentID: "doc-onboard", VersionID: "version-3", Title: "Onboarding guide", Owner: "People Ops", Snippet: "New hires start here.", Why: "semantic"},
		},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`role="search"`, `name="docs_q"`, `value="handbook"`,
		`id="docs-search-team"`, `id="docs-search-channel"`, `id="docs-search-owner"`, `id="docs-search-status"`,
		`id="docs-search-date-from"`, `id="docs-search-date-to"`,
		"New hires start here.", "Keyword match", "Semantic match", `href="/workspace/app/docs?document=doc-team"`,
		"Semantic search is unavailable",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("search controls omitted %q", want)
		}
	}
	if strings.Contains(doc, "<script>alert(1)</script>") {
		t.Fatal("search snippet was rendered as executable HTML")
	}
	if !strings.Contains(doc, "&lt;script&gt;") {
		t.Fatal("search snippet was not escaped")
	}

	// RED: a vector outage must never blank all results or hide exact
	// matching documents -- both results, including the exact keyword hit,
	// must still be present alongside the fallback notice.
	if !strings.Contains(doc, "Team handbook") {
		t.Fatal("vector-outage fallback hid an exact matching document")
	}
}

// TestTodo_HUB_036_Accessibility is the ACCESSIBILITY test for HUB-036:
// filter controls are labelled native inputs, the fallback notice and empty
// state announce via role=status, and provenance labels are non-empty text
// (not color alone) in every supported locale.
func TestTodo_HUB_036_Accessibility(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		view := ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), ResolveProductLocale(locale))
		view.DocumentsReady = true
		view.DocumentSearch = DocumentSearchProjection{
			Ready: true, Query: "handbook", Mode: "keyword", FallbackUsed: true,
			Results: []DocumentSearchResult{{DocumentID: "doc-team", Title: "Team handbook", Why: "semantic"}},
		}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("search UI has an untranslated placeholder in %s", locale)
		}
		if !strings.Contains(doc, `for="docs-search-team"`) || !strings.Contains(doc, `for="docs-search-status"`) {
			t.Fatalf("search filters are not labelled native controls in %s", locale)
		}
		if !strings.Contains(doc, `role="status"`) {
			t.Fatalf("search fallback notice does not announce in %s", locale)
		}
		if strings.TrimSpace(docsSearchProvenance(locale, "semantic")) == "" {
			t.Fatalf("empty provenance label in %s", locale)
		}
	}
	empty := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	empty.DocumentsReady = true
	empty.DocumentSearch = DocumentSearchProjection{Ready: true, Query: "nothing"}
	doc, err := Render(empty)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `role="status"`) {
		t.Fatal("empty search result set is not announced accessibly")
	}
}

func TestTodo_HUB_032_DocumentReadIsPlainTextAndHasReturnControl(t *testing.T) {
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-1", Title: "Handbook", Owner: "People Operations", OwnerID: "owner-1", VersionID: "version-2", Status: DocumentPrivate}, Markdown: "# Heading\n\n<script>should stay text</script>\n\n[Team guide](doc:team-7#intro), doc:private-2@version-3, and doc:plain-3\n[Live refresh](doc:plain-4)\n[Unsafe](javascript:alert(1))", ContentHash: "hash-2"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="/workspace/app/docs"`, `tabIndex="-1">Handbook</h1>`, `<h2 dir="auto" id="sec-heading">Heading</h2>`, "&lt;script&gt;should stay text&lt;/script&gt;", "People Operations", `href="/workspace/app/docs?document=plain-3"`, `href="/workspace/app/docs?document=plain-4"`, "doc:team-7#intro", "doc:private-2@version-3", "javascript:alert(1)"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("document read omitted %q", want)
		}
	}
	if strings.Contains(doc, "<script>should stay text</script>") {
		t.Fatal("markdown was emitted as executable HTML")
	}
	if strings.Contains(doc, `href="/workspace/app/docs?document=team-7#intro"`) || strings.Contains(doc, `href="/workspace/app/docs?document=private-2@version-3"`) {
		t.Fatal("pinned or anchored document reference was silently rewritten")
	}
}

func TestTodo_HUB_032_DocumentWorkspaceUsesPlatformTokens(t *testing.T) {
	css := docsStylesheet()
	for _, want := range []string{"var(--surface)", "var(--line)", "var(--hcm-space-2)", "var(--hcm-density)", "var(--hcm-radius-surface)", "var(--hcm-motion-fast", "docs-detail-layout", "grid-template-columns:minmax(0,1fr) minmax(16rem,20rem)", "max-width:72ch", "prefers-reduced-motion", "@container docslib", "pointer:coarse"} {
		if !strings.Contains(css, want) {
			t.Fatalf("document workspace stylesheet omitted %q", want)
		}
	}
	for _, forbidden := range []string{"border-radius:.75rem", "border:1px solid currentColor"} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("document workspace retained unthemed style %q", forbidden)
		}
	}
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.DocumentsReady = true
	view.Documents = []DocumentSummary{{ID: "doc-1", Title: "Handbook", OwnerID: "opaque-owner", VersionID: "opaque-version", Status: DocumentShared, Sharing: "shared", UpdatedAt: "2026-09-22T10:00:00Z"}}
	view.CreateDocument = func(DocumentCreateRequest, func(error)) {}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="docs-table"`, `docs-row docs-kind-shared`, `New document`, `datetime="2026-09-22T10:00:00Z"`, `opaque-owner`, `Shared`, `aria-expanded="false"`, `aria-controls="docs-create-panel"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("document workspace omitted %q", want)
		}
	}
	for _, forbidden := range []string{"Authorized owner", "Current version", `id="docs-create-markdown"`} {
		if strings.Contains(doc, forbidden) {
			t.Fatalf("document workspace rendered placeholder or expanded editor %q", forbidden)
		}
	}
}

func TestTodo_HUB_032_MarkdownDocumentUsesSafeTypedNodes(t *testing.T) {
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-typed", Title: "Typed guide"}, Markdown: "## Heading\n\n**bold** *emphasis* `code`\n\n- one\n- two\n\n[unsafe](javascript:alert(1))"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<h3 dir="auto" id="sec-heading">Heading</h3>`, "<strong>bold</strong>", "<em>emphasis</em>", "<code>code</code>", `<ul dir="auto">`, `<li dir="auto">one</li>`, "javascript:alert(1)"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("typed Markdown omitted %q", want)
		}
	}
	if strings.Contains(doc, `href="javascript:alert(1)"`) {
		t.Fatal("unsafe Markdown scheme became an active link")
	}
}

func TestTodo_HUB_012_ShareControlIsOwnerGatedAndCanonical(t *testing.T) {
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-42", Title: "Handbook", CanManageAccess: true}, Markdown: "Private"}
	view.DocumentOrigin = "http://127.0.0.1:8899"
	view.ShareDocument = func(DocumentShareRequest, func(error)) {}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `data-docs-action="share"`) {
		t.Fatal("owner was not offered Share")
	}
	if got := docsShareableHref(view.DocumentOrigin, "doc-42"); got != "http://127.0.0.1:8899/workspace/app/docs?document=doc-42" {
		t.Fatalf("share link = %q", got)
	}
	if got := docsShareableHref("", "doc-42"); got != "" {
		t.Fatalf("relative URI offered for copying without trusted origin: %q", got)
	}
	people := []Person{{ID: "worker-17", PreferredName: "Riley Chen", LifecycleStatus: "ACTIVE"}, {ID: "worker-18", PreferredName: "Riley Former", LifecycleStatus: "TERMINATED"}, {ID: "worker-19", Name: "Riley Owner"}}
	matches := docsPeopleMatches(people, "ril", map[string]bool{"worker-19": true}, 6)
	if len(matches) != 1 || matches[0].ID != "worker-17" {
		t.Fatalf("share suggestions = %+v, want only the active person without access", matches)
	}
	view.Document.Summary.CanManageAccess = false
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `data-docs-action="share"`) {
		t.Fatal("share control rendered without owner authorization")
	}
}

func TestDocumentCommentsAreVersionBoundAndAccessAware(t *testing.T) {
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-42", Title: "Handbook", VersionID: "version-7"}, CanComment: true, Comments: []DocumentComment{{ID: "comment-1", AuthorID: "worker-1", Author: "Riley Chen", Body: "Please clarify this step.", CreatedAt: "2026-09-22T10:00:00Z", VersionID: "version-7"}}}
	view.AddDocumentComment = func(DocumentCommentCreateRequest, func(error)) {}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Comments", "Riley Chen", `datetime="2026-09-22T10:00:00Z"`, "Please clarify this step.", `data-comment-id="comment-1"`, `data-version-id="version-7"`, `id="docs-comment-body"`, "Add a comment", `name="body"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("comments UI omitted %q", want)
		}
	}
	view.Document.CanComment = false
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `id="docs-comment-form"`) {
		t.Fatal("comment composer rendered without comment authorization")
	}
	view.Document.Comments = nil
	view.Document.CommentsUnavailable = true
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Comments could not be loaded") || strings.Contains(doc, "No open comments") {
		t.Fatal("comment load failure rendered as an empty thread")
	}
}

func TestDocumentEditIsOwnerGatedAndVersionAware(t *testing.T) {
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-42", Title: "Handbook", VersionID: "version-7"}, Markdown: "# Current", CanEdit: true}
	view.CreateDocumentVersion = func(DocumentEditRequest, func(error)) {}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Edit document"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("edit control omitted %q", want)
		}
	}
	view.Document.CanEdit = false
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "Edit document") {
		t.Fatal("edit control rendered without owner authorization")
	}
}

func TestDocumentVersionConflictIsSpecific(t *testing.T) {
	if !isDocumentVersionConflict(errors.New("rpc error: code = Aborted desc = document.stale_version")) {
		t.Fatal("stale document version was not recognized")
	}
	if isDocumentVersionConflict(errors.New("permission denied")) {
		t.Fatal("unrelated save error was classified as a version conflict")
	}
}
