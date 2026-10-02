package documenthubstore

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestDocumentPreviews_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader = "tenant-previews", "owner-p", "reader-p"
	shared, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Onboarding guide", "# Onboarding guide\n\nWelcome to the team. Start with the **benefits** overview.\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, shared, owner, reader, RoleViewer); err != nil {
		t.Fatal(err)
	}
	secret, _, err := s.CreatePersonalDocument(ctx, tenant, owner, "Secret comp plan", "# Secret comp plan\n\nSalary bands for next year.\n")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.DocumentPreviews(ctx, tenant, reader, []string{secret, shared, "doc-missing", shared, " "})
	if err != nil || len(got) != 3 {
		t.Fatalf("reader previews = %+v, %v", got, err)
	}
	// The unreadable document and the missing one are indistinguishable
	// and carry nothing but their ID.
	for _, i := range []int{0, 2} {
		if got[i].Readable || got[i].Title != "" || got[i].OwnerID != "" || got[i].Snippet != "" || !got[i].UpdatedAt.IsZero() {
			t.Fatalf("unreadable preview %d leaked %+v", i, got[i])
		}
	}
	if got[0].DocumentID != secret || got[2].DocumentID != "doc-missing" {
		t.Fatalf("order = %+v", got)
	}
	ready := got[1]
	if !ready.Readable || ready.DocumentID != shared || ready.Title != "Onboarding guide" || ready.OwnerID != owner || ready.UpdatedAt.IsZero() {
		t.Fatalf("readable preview = %+v", ready)
	}
	if !strings.HasPrefix(ready.Snippet, "Welcome to the team.") || strings.Contains(ready.Snippet, "**") {
		t.Fatalf("snippet = %q", ready.Snippet)
	}
	ownerView, err := s.DocumentPreviews(ctx, tenant, owner, []string{secret})
	if err != nil || len(ownerView) != 1 || !ownerView[0].Readable || ownerView[0].Title != "Secret comp plan" {
		t.Fatalf("owner previews = %+v, %v", ownerView, err)
	}
	if _, err := s.DocumentPreviews(ctx, tenant, "", []string{shared}); err == nil {
		t.Fatal("anonymous previews resolved")
	}
	var many []string
	for i := 0; i < MaxPreviewIDs+10; i++ {
		many = append(many, fmt.Sprintf("doc-%d", i))
	}
	bounded, err := s.DocumentPreviews(ctx, tenant, reader, many)
	if err != nil || len(bounded) != MaxPreviewIDs {
		t.Fatalf("bounded previews = %d, %v", len(bounded), err)
	}
}

func TestDocumentPreviewSnippet(t *testing.T) {
	if got := previewSnippet("# Title\n\nShort body.", "Title"); got != "Short body." {
		t.Fatalf("short = %q", got)
	}
	long := "# T\n\n" + strings.Repeat("word ", 100)
	got := previewSnippet(long, "T")
	if !strings.HasSuffix(got, "…") || len([]rune(got)) > previewSnippetRunes+1 || strings.Contains(got, "  ") {
		t.Fatalf("long = %q", got)
	}
}

// TestTodo_CHATUX_025_PreviewSnippet: the excerpt of a linked document is its
// body text. Headings and the table's header row, which read as "Common
// questions. Question Answer.", are left out; a document of nothing but
// headings has no excerpt.
func TestTodo_CHATUX_025_PreviewSnippet(t *testing.T) {
	doc := "# Holiday guide\n\n**Owner:** People Ops\n\n## Common questions\n\n| Question | Answer |\n| --- | --- |\n| Carry over? | Five days |\n\nCompany holidays follow the local calendar.\n\n```\n# not a heading\n```\n"
	got := previewSnippet(doc, "Holiday guide")
	if strings.Contains(got, "Common questions") || strings.Contains(got, "Question") || strings.Contains(got, "Answer") || strings.Contains(got, "Owner") {
		t.Fatalf("headings or table rows in the excerpt: %q", got)
	}
	if !strings.HasPrefix(got, "Company holidays follow the local calendar.") || !strings.Contains(got, "not a heading") {
		t.Fatalf("body text lost: %q", got)
	}
	if got := previewSnippet("# Only\n\n## Headings\n", "Only"); got != "" {
		t.Fatalf("a document of headings has an excerpt %q", got)
	}
}
