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
		{ID: "doc-private", Title: "Private draft", Owner: "Taylor", Status: DocumentPrivate, Version: "v2", Sharing: "Only you"},
		{ID: "doc-team", Title: "Team handbook", Owner: "People Ops", Status: DocumentTeamOfficial, Version: "v7", Scope: "People Ops", ReviewDue: "2026-10-01", Sharing: "People Ops"},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Private draft", "Team handbook", "Taylor", "People Ops", "v7", "2026-10-01", "Only you", `aria-label="Open: Private draft"`, `aria-label="Open: Team handbook"`} {
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

func TestTodo_HUB_034_036_ControlsUseAuthorizedProjection(t *testing.T) {
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.DocumentsReady = true
	view.DocumentReviews = []DocumentReviewProjection{{
		DocumentID: "doc-team", VersionID: "version-7", Title: "Team handbook",
		ReviewState: "pending", ReviewAction: "/docs/doc-team/review", DeployAction: "/docs/doc-team/deploy",
		CanReview: true, CanDeploy: true,
	}}
	view.DocumentSearch = DocumentSearchProjection{
		Ready: true, Query: "handbook", Mode: "keyword", FallbackUsed: true,
		Results: []DocumentSearchResult{{DocumentID: "doc-team", VersionID: "version-7", Title: "Team handbook", Owner: "People Ops", Snippet: "Authorized excerpt", Why: "keyword match"}},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`role="search"`, `name="docs_q"`, `value="handbook"`, "Authorized excerpt", "keyword match", `href="/workspace/app/docs?document=doc-team"`,
		`action="/docs/doc-team/review"`, `action="/docs/doc-team/deploy"`, `name="version_id"`, "version-7",
		"Semantic search is unavailable",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("docs controls omitted %q", want)
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

func TestTodo_HUB_032_DocumentReadIsPlainTextAndHasReturnControl(t *testing.T) {
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-1", Title: "Handbook", Owner: "People Operations", OwnerID: "owner-1", VersionID: "version-2", Status: DocumentPrivate}, Markdown: "# Heading\n\n<script>should stay text</script>\n\n[Team guide](doc:team-7#intro), doc:private-2@version-3, and doc:plain-3\n[Live refresh](doc:plain-4)\n[Unsafe](javascript:alert(1))", ContentHash: "hash-2"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="/workspace/app/docs"`, "<h2>Handbook</h2>", "<h3>Heading</h3>", "&lt;script&gt;should stay text&lt;/script&gt;", "People Operations", `href="/workspace/app/docs?document=plain-3"`, `href="/workspace/app/docs?document=plain-4"`, "doc:team-7#intro", "doc:private-2@version-3", "javascript:alert(1)"} {
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
	for _, want := range []string{"var(--surface)", "var(--line)", "var(--hcm-space-2)", "var(--hcm-density)", "var(--hcm-radius-surface)", "var(--hcm-motion-fast)", "docs-detail-layout", "grid-template-columns:minmax(0,1fr) minmax(18rem,26rem)", "max-width:85ch", "prefers-reduced-motion", "max-width:390px"} {
		if !strings.Contains(css, want) {
			t.Fatalf("document workspace stylesheet omitted %q", want)
		}
	}
	for _, forbidden := range []string{"currentColor", "border-radius:.75rem", "border:1px solid currentColor"} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("document workspace retained unthemed style %q", forbidden)
		}
	}
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.DocumentsReady = true
	view.Documents = []DocumentSummary{{ID: "doc-1", Title: "Handbook", OwnerID: "opaque-owner", VersionID: "opaque-version", Status: DocumentPrivate, Sharing: "shared", UpdatedAt: "2026-09-22"}}
	view.CreateDocument = func(DocumentCreateRequest, func(error)) {}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="docs-list"`, `class="docs-row"`, `New document`, `Updated 2026-09-22`, `opaque-owner`, `docs-status-shared`, `Shared`, `aria-expanded="false"`, `aria-controls="docs-create-panel"`} {
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
	for _, want := range []string{"<h4>Heading</h4>", "<strong>bold</strong>", "<em>emphasis</em>", "<code>code</code>", "<ul>", "<li>one</li>", "javascript:alert(1)"} {
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
	view.People = []Person{{ID: "worker-17", PreferredName: "Riley Chen", LifecycleStatus: "ACTIVE"}, {ID: "worker-18", PreferredName: "Former employee", LifecycleStatus: "TERMINATED"}}
	view.DocumentOrigin = "http://127.0.0.1:8899"
	view.ShareDocument = func(DocumentShareRequest, func(error)) {}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Share document", "recipient_id", `list="docs-share-people"`, `id="docs-share-people"`, `value="worker-17"`, "Riley Chen", "Workspace document link", `http://127.0.0.1:8899/workspace/app/docs?document=doc-42`, "Sharing publishes the current personal version"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("owner share control omitted %q", want)
		}
	}
	if strings.Contains(doc, `value="worker-18"`) {
		t.Fatal("inactive employee offered as a share recipient")
	}
	view.DocumentOrigin = ""
	doc, err = Render(view)
	if err != nil || strings.Contains(doc, `id="docs-share-url"`) {
		t.Fatalf("relative URI offered for copying without trusted origin: %v", err)
	}
	view.Document.Summary.CanManageAccess = false
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "recipient_id") || strings.Contains(doc, "Share document") {
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
	for _, want := range []string{"Comments", "Riley Chen", "2026-09-22 10:00 UTC", "Please clarify this step.", `data-comment-id="comment-1"`, `data-version-id="version-7"`, "docs-comment-body", "Add comment", `name="body"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("comments UI omitted %q", want)
		}
	}
	view.Document.CanComment = false
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `class="docs-comment-compose"`) {
		t.Fatal("comment composer rendered without comment authorization")
	}
	view.Document.Comments = nil
	view.Document.CommentsUnavailable = true
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Comments could not be loaded") || strings.Contains(doc, "No comments on this version yet") {
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
