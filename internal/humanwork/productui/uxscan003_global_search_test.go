package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXSCAN_003(t *testing.T) {
	view := testView(PageHome)
	items := globalSearchItems(view)
	var promotion, journeys *GlobalSearchItem
	for index := range items {
		item := &items[index]
		switch item.ID {
		case "action:promotion":
			promotion = item
		case "page:journeys":
			journeys = item
		case "workflow:promotion":
			t.Fatal("promotion must not be advertised as a tracker workflow")
		}
	}
	if promotion == nil || journeys == nil {
		t.Fatalf("promotion search destinations missing: action=%v journeys=%v", promotion != nil, journeys != nil)
	}
	if promotion.Kind != "action" || promotion.KindLabel != view.Locale.Text("global_search.kind_action") {
		t.Fatalf("promotion result type = %#v, want action", promotion)
	}
	if promotion.Href != statefulHref(view, PagePeople, "eligible", "1") {
		t.Fatalf("promotion action href = %q, want authorized worker selection", promotion.Href)
	}
	if journeys.Kind != "page" || journeys.KindLabel != view.Locale.Text("global_search.kind_page") {
		t.Fatalf("journeys result type = %#v, want page", journeys)
	}
	if promotion.Description == "" || promotion.Description == journeys.Description {
		t.Fatalf("promotion and tracker descriptions are not distinct: %q / %q", promotion.Description, journeys.Description)
	}
	if results := SearchGlobalItems(items, "promotion", globalSearchLimit); !hasSearchResult(results, "action:promotion") || !hasSearchResult(results, "page:journeys") {
		t.Fatalf("promotion search results = %#v", searchResultIDs(results))
	}
}

func TestTodo_UXSCAN_003_Browser(t *testing.T) {
	props := globalSearchProps(testView(PageHome))
	props.InitialQuery = "promotion"
	markup, err := ui.RenderToString(ui.CreateElement(GlobalSearch, props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="/workspace/app/people?eligible=1"`,
		`href="/workspace/app/journeys"`,
		viewText(props, "action_launcher.promote_worker"),
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
	for _, id := range []string{"action:promotion", "page:journeys"} {
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
	view := testView(PageHome)
	view.Work = nil
	items := globalSearchItems(view)
	for _, name := range []string{"Adrian", "Amara", "Andre", "Anika", "Aya", "Benjamin"} {
		items = append(items, GlobalSearchItem{
			ID: "action:promotion:" + name, Kind: "action", Label: "Start promotion for " + name,
			Keywords: []string{"promotion"},
		})
	}
	results := SearchGlobalItems(items, "promotion", 5)
	if !hasSearchResult(results, "action:promotion") || !hasSearchResult(results, "page:journeys") {
		t.Fatalf("short result list obscured selection or tracker: %#v", searchResultIDs(results))
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
