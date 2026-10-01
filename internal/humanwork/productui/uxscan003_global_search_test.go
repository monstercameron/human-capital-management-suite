package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXSCAN_003(t *testing.T) {
	// UXBLIND-055 separates executable action discovery into the quick
	// launcher, while global search keeps the Journeys page destination.
	view := testView(PageHome)
	items := globalSearchItems(view)
	var journeys *GlobalSearchItem
	for index := range items {
		item := &items[index]
		switch item.ID {
		case "action:promotion":
			t.Fatal("global search must leave executable promotion actions to the quick launcher")
		case "page:journeys":
			journeys = item
		case "workflow:promotion":
			t.Fatal("promotion must not be advertised as a tracker workflow")
		}
	}
	if journeys == nil {
		t.Fatal("global search lost the Journeys page result")
	}
	if journeys.Kind != "page" || journeys.KindLabel != view.Locale.Text("global_search.kind_page") {
		t.Fatalf("journeys result type = %#v, want page", journeys)
	}
	if results := SearchGlobalItems(items, "promotion", globalSearchLimit); hasGlobalSearchKind(results, "action") || !hasSearchResult(results, "page:journeys") {
		t.Fatalf("promotion search results = %#v", searchResultIDs(results))
	}
}

func TestTodo_UXSCAN_003_Browser(t *testing.T) {
	// The global-search browser contract now renders tracker destinations;
	// action results are intentionally exercised by the launcher tests.
	props := globalSearchProps(testView(PageHome))
	props.InitialQuery = "promotion"
	markup, err := ui.RenderToString(ui.CreateElement(GlobalSearch, props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="/workspace/app/journeys"`,
		viewText(props, "page.journeys.label"),
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("promotion search markup missing %q: %s", want, markup)
		}
	}
}

func TestTodo_UXSCAN_003_Accessibility(t *testing.T) {
	view := testView(PageHome)
	items := globalSearchItems(view)
	for _, id := range []string{"page:journeys"} {
		item := findGlobalSearchItem(items, id)
		if item == nil || strings.TrimSpace(item.Label) == "" || strings.TrimSpace(item.Description) == "" || strings.TrimSpace(item.KindLabel) == "" {
			t.Fatalf("result %q lacks accessible label/type/description: %#v", id, item)
		}
	}
}

func TestTodo_UXSCAN_003_Security(t *testing.T) {
	view := testView(PageHome)
	view.LauncherActions = nil
	items := globalSearchItems(view)
	if hasSearchResult(items, "action:promotion") {
		t.Fatal("promotion action exposed without an authorized launcher projection")
	}
	if !hasSearchResult(items, "page:journeys") {
		t.Fatal("authorized journeys tracker disappeared when promotion action was unavailable")
	}
}

func TestTodo_UXSCAN_003_Regression(t *testing.T) {
	view := testView(PageHome)
	view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: false}, {Page: PagePeople, View: true}}
	items := globalSearchItems(view)
	if hasSearchResult(items, "action:promotion") {
		t.Fatal("promotion action exposed when journeys create permission is denied")
	}
}

func TestTodo_UXSCAN_003_ShortResultListKeepsWorkerSelection(t *testing.T) {
	// A short global-search result list must retain the tracker without
	// reintroducing executable actions from the quick launcher.
	view := testView(PageHome)
	view.Work = nil
	items := globalSearchItems(view)
	results := SearchGlobalItems(items, "promotion", 5)
	if hasGlobalSearchKind(results, "action") || !hasSearchResult(results, "page:journeys") {
		t.Fatalf("short result list did not preserve the tracker without duplicating actions: %#v", searchResultIDs(results))
	}
}

func findGlobalSearchItem(items []GlobalSearchItem, id string) *GlobalSearchItem {
	for index := range items {
		if items[index].ID == id {
			return &items[index]
		}
	}
	return nil
}

func viewText(props GlobalSearchProps, key string) string {
	return props.Locale.Text(key)
}
