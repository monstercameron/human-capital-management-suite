package productui

import (
	"strings"
	"testing"
)

// TestIssue3435_HelpExplainsUnavailableSearchAndSupport proves that when the
// resolved permissions withhold knowledge search and support intake, Help says
// so explicitly instead of silently presenting only navigation shortcuts. The
// destinations themselves must still stay absent.
func TestIssue3435_HelpExplainsUnavailableSearchAndSupport(t *testing.T) {
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageHelp), ResolveProductLocale(code))
		view.EffectivePermissions = []RolePagePermission{
			{Page: PageHelp, View: true},
			{Page: PageKnowledgeSearch, View: false},
			{Page: PageHRServiceRequest, View: false},
		}
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("locale %s: %v", code, err)
		}
		want := view.Locale.Text("help.search_unavailable")
		if want == "" || strings.Contains(want, "⟦") || want == ResolveProductLocale("en-US").Text("help.search_unavailable") && code != "en-US" {
			t.Fatalf("locale %s has no localized unavailable copy: %q", code, want)
		}
		if !strings.Contains(doc, want) {
			t.Fatalf("locale %s: denied search and support gave no explanation", code)
		}
		if strings.Contains(doc, statefulHref(view, PageKnowledgeSearch)) || strings.Contains(doc, statefulHref(view, PageHRServiceRequest)) {
			t.Fatalf("locale %s leaked a denied support destination", code)
		}
	}
}

// TestIssue3435_HelpOffersSearchWhenAuthorized keeps the authorized path
// unchanged: no unavailable explanation is shown when search is granted.
func TestIssue3435_HelpOffersSearchWhenAuthorized(t *testing.T) {
	view := testView(PageHelp)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, view.Locale.Text("help.search_unavailable")) {
		t.Fatal("authorized Help shows the unavailable explanation")
	}
	if !strings.Contains(doc, statefulHref(view, PageKnowledgeSearch)) {
		t.Fatal("authorized Help lost the knowledge search destination")
	}
}
