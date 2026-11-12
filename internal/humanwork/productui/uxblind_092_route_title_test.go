package productui

import (
	"strings"
	"testing"
)

// TestTodo_UXBLIND_092_RouteInterimTitle pins the router's pre-render tab
// title: the route writes it before the page loads, so it must name the page
// in the active locale rather than the English registry title.
func TestTodo_UXBLIND_092_RouteInterimTitle(t *testing.T) {
	definition, ok := LookupPage(PageJourneys)
	if !ok {
		t.Fatal("journeys page is not registered")
	}
	for _, code := range []string{"de-DE", "ar"} {
		locale := ResolveProductLocale(code)
		got := InterimRouteDocumentTitle(definition, locale, DefaultCustomerTheme(), "HarborCare")
		want := locale.Text(definition.TitleKey) + " · HarborCare"
		if got != want {
			t.Fatalf("%s interim route title = %q, want %q", code, got, want)
		}
		if definition.Title != "" && strings.Contains(got, definition.Title) && locale.Text(definition.TitleKey) != definition.Title {
			t.Fatalf("%s interim route title kept the English page name: %q", code, got)
		}
	}
}
