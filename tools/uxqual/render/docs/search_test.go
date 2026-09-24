package docs

import (
	"strings"
	"testing"
)

// TestRenderSearch exercises search.go: results carry a provenance label,
// filters are offered, snippets are escaped safe text, and a vector outage
// shows a visible fallback notice while keeping lexical results on screen.
func TestRenderSearch(t *testing.T) {
	page, err := RenderSearch(SearchPage{
		Locale: "en-US", Title: "Search documents", Query: "handbook", Mode: "keyword",
		SemanticAvailable: false, FallbackUsed: true,
		Filters: SearchFilters{Team: "People Ops", Status: "team_official"},
		Results: []SearchResult{
			{DocumentID: "doc-team", Title: "Team handbook", Owner: "People Ops", Why: "keyword", Snippet: "<script>alert(1)</script> leave"},
			{DocumentID: "doc-onboard", Title: "Onboarding guide", Owner: "People Ops", Why: "semantic", Snippet: "New hires start here."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Keyword match", "Semantic match", "Semantic search is unavailable",
		`name="team" value="People Ops"`, `name="status" value="team_official"`,
		"&lt;script&gt;", "Team handbook", "Onboarding guide",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("search page missing %q", want)
		}
	}
	if strings.Contains(page, "<script>alert(1)</script>") {
		t.Fatal("snippet was rendered as executable HTML")
	}
	if strings.Contains(page, "disabled") == false {
		t.Fatal("semantic option should be disabled when unavailable")
	}
}

// TestRenderSearch_NoResultsIsVisibleNotBlank exercises the RED case: an
// empty result set must still render a visible message, not a blank page.
func TestRenderSearch_NoResultsIsVisibleNotBlank(t *testing.T) {
	page, err := RenderSearch(SearchPage{Locale: "en-US", Title: "Search documents", Query: "nothing"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, "No documents match this search.") {
		t.Fatal("empty search result did not explain itself")
	}
}

// TestRenderSearch_Accessibility checks locale coverage for provenance and
// filter labels, matching the editor/picker/hub renderers' i18n contract.
func TestRenderSearch_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		page, err := RenderSearch(SearchPage{
			Locale: locale, Title: "x", Results: []SearchResult{{Title: "x", Why: "semantic"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(searchProvenanceLabel(locale, "semantic")) == "" {
			t.Fatalf("%s: empty semantic provenance label", locale)
		}
		if locale == "ar" && !strings.Contains(page, `dir="rtl"`) {
			t.Fatal("ar search page missing rtl direction")
		}
	}
}
