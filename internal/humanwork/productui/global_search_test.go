package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestPromotionUnavailableHidesActionButNotPerson(t *testing.T) {
	view := testView(PagePeople)
	view.People[0].PromotionAvailability = PromotionIneligible
	person := view.People[0]
	for _, item := range globalSearchItems(view) {
		if item.ID == "action:promotion:"+person.ID {
			t.Fatal("search offers unavailable promotion")
		}
	}
	props := personWorkflowLauncherProps(view, person, PagePerson)
	for _, workflow := range props.Workflows {
		if workflow.Name == "Promotion" {
			t.Fatal("profile offers unavailable promotion")
		}
	}
	activeRecovery := false
	for _, workflow := range props.Workflows {
		activeRecovery = activeRecovery || workflow.ActionLabel == view.Locale.Text("people.open_active_promotion")
	}
	if props.UnavailableDetail == "" && !activeRecovery {
		t.Fatal("profile needs a recovery explanation or active-request link")
	}
}

func TestGlobalSearchCoversProductPagesPeopleWorkflowsSettingsAndFeatures(t *testing.T) {
	view := testView(PageHome)
	items := globalSearchItems(view)
	tests := []struct {
		query string
		id    string
	}{
		{query: "Avery", id: "person:worker-avery"},
		{query: "NW-40118", id: "person:worker-avery"},
		{query: "setings", id: "page:settings"},
		{query: "accessibility", id: "component:accessibility"},
		{query: "profile photo", id: "component:user-profile"},
		{query: "dark mode", id: "component:brand-appearance"},
		{query: "completed Avery", id: "workflow-instance:intent-2"},
		{query: "Avery promotion", id: "action:promotion:worker-avery"},
		{query: "internal transfer", id: "workflow:transfer"},
	}
	for _, test := range tests {
		t.Run(test.query, func(t *testing.T) {
			results := SearchGlobalItems(items, test.query, globalSearchLimit)
			for _, result := range results {
				if result.ID == test.id {
					return
				}
			}
			t.Fatalf("SearchGlobalItems(%q) = %#v, missing %q", test.query, searchResultIDs(results), test.id)
		})
	}
}

func TestGlobalSearchHonorsAuthorizedNavigationProjection(t *testing.T) {
	view := testView(PageHome)
	view.Navigation = view.Navigation[:2]
	items := globalSearchItems(view)
	for _, query := range []string{"Experience Studio", "brand appearance", "people directory"} {
		if results := SearchGlobalItems(items, query, globalSearchLimit); len(results) != 0 {
			t.Fatalf("hidden navigation matched %q: %#v", query, searchResultIDs(results))
		}
	}
	if results := SearchGlobalItems(items, "settings", globalSearchLimit); !hasSearchResult(results, "page:settings") {
		t.Fatal("shell support settings disappeared from global search")
	}
}

func TestGlobalSearchRendersAccessibleCommandSurfaceAndSoftwareDestinations(t *testing.T) {
	view := testView(PageHome)
	props := globalSearchProps(view)
	props.InitialQuery = "Avery"
	markup, err := ui.RenderToString(ui.CreateElement(GlobalSearch, props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`role="search"`, `role="combobox"`, `aria-autocomplete="list"`, `aria-expanded="true"`,
		`role="listbox"`, `role="option"`, `aria-selected="true"`, `Avery Patel`,
		`href="/workspace/app/person?person=worker-avery"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("global search markup missing %q: %s", want, markup)
		}
	}
}

func TestGlobalSearchIsDeterministicAndBounded(t *testing.T) {
	items := globalSearchItems(testView(PageHome))
	first := SearchGlobalItems(items, "promotion", 4)
	second := SearchGlobalItems(items, "promotion", 4)
	if len(first) != 4 || strings.Join(searchResultIDs(first), ",") != strings.Join(searchResultIDs(second), ",") {
		t.Fatalf("search ordering is not stable and bounded: %#v / %#v", searchResultIDs(first), searchResultIDs(second))
	}
}

func searchResultIDs(items []GlobalSearchItem) []string {
	result := make([]string, len(items))
	for index, item := range items {
		result[index] = item.ID
	}
	return result
}

func hasSearchResult(items []GlobalSearchItem, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
