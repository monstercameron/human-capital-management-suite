package productui

import "testing"

func TestPageDefinitionsReturnIndependentSearchTerms(t *testing.T) {
	first := PageDefinitions()
	if len(first) == 0 || len(first[0].SearchTerms) == 0 {
		t.Fatal("page catalogue has no search terms to protect")
	}
	want := first[0].SearchTerms[0]
	first[0].SearchTerms[0] = "corrupted"
	second := PageDefinitions()
	if second[0].SearchTerms[0] != want {
		t.Fatalf("page definitions share search-term storage: got %q, want %q", second[0].SearchTerms[0], want)
	}
}

func TestPageCatalogueBuildsIndependentSearchTerms(t *testing.T) {
	first := buildRegisteredPages()
	second := buildRegisteredPages()
	if len(first) == 0 || len(first) != len(second) {
		t.Fatalf("fresh catalogues have inconsistent lengths: %d and %d", len(first), len(second))
	}
	first[0].SearchTerms[0] = "corrupted"
	if second[0].SearchTerms[0] == "corrupted" {
		t.Fatal("fresh catalogues share mutable search-term storage")
	}
}

func TestPageLookupsReturnIndependentSearchTerms(t *testing.T) {
	page, ok := LookupPage(PageHome)
	if !ok || len(page.SearchTerms) == 0 {
		t.Fatal("home lookup has no search terms")
	}
	want := page.SearchTerms[0]
	page.SearchTerms[0] = "corrupted page lookup"
	route, ok := LookupRoute(page.Route)
	if !ok || len(route.SearchTerms) == 0 || route.SearchTerms[0] != want {
		t.Fatal("page lookup mutation escaped into route lookup")
	}
	route.SearchTerms[0] = "corrupted route lookup"
	again, ok := LookupPage(PageHome)
	if !ok || len(again.SearchTerms) == 0 || again.SearchTerms[0] != want {
		t.Fatal("route lookup mutation escaped into page lookup")
	}
}
