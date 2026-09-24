package docs

import (
	"strings"
	"testing"
)

// TestRenderHub exercises hub.go: it distinguishes private drafts, shared
// pages, and official guidance by class and text, and carries owner,
// deployed version, official scope, review date, and sharing state.
func TestRenderHub(t *testing.T) {
	page, err := RenderHub(HubPage{
		Locale: "en-US", Title: "Documents",
		Documents: []HubDocument{
			{ID: "doc-private", Title: "Private draft", Owner: "Taylor", Status: "private", Sharing: "Only you"},
			{ID: "doc-team", Title: "Team handbook", Owner: "People Ops", Status: "team_official", Version: "v7", Scope: "People Ops", ReviewDue: "2026-10-01", Sharing: "Team"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"docs-kind-private", "docs-kind-team_official", "Private draft", "Team handbook",
		"Taylor", "People Ops", "v7", "2026-10-01", "Only you", "Team guidance",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("hub page missing %q", want)
		}
	}
	if strings.Contains(page, "Unpublished secret") {
		t.Fatal("hub page invented a document")
	}
}

// TestRenderHub_Accessibility checks the RTL/locale contract holds for the
// hub renderer the same way the editor and picker renderers do.
func TestRenderHub_Accessibility(t *testing.T) {
	page, err := RenderHub(HubPage{Locale: "ar", Title: "المستندات", Documents: []HubDocument{{ID: "doc-1", Title: "دليل", Status: "private", Owner: "مالك"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, `dir="rtl"`) || !strings.Contains(page, `lang="ar"`) {
		t.Fatal("hub page missing rtl direction/lang")
	}
	de, err := RenderHub(HubPage{Locale: "de-DE", Title: "Dokumente", Documents: []HubDocument{{ID: "doc-1", Title: "Handbuch", Status: "team_official", Owner: "Team"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(de, "Teamleitfaden") {
		t.Fatal("hub page missing German status label")
	}
}
