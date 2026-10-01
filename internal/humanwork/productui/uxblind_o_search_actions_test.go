package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_054(t *testing.T) {
	items := []GlobalSearchItem{
		{ID: "person:ana", Kind: "person", KindLabel: "Person", Label: "Ana Lopez", Description: "Finance"},
		{ID: "person:hannah", Kind: "person", KindLabel: "Person", Label: "Hannah Reed", Description: "Project Management"},
		{ID: "person:worker", Kind: "person", KindLabel: "Person", Label: "Jordan Reed", Description: "Project Management", Keywords: []string{"NW-40118"}},
	}
	if got := SearchGlobalItems(items, "ana", 3); len(got) == 0 || got[0].ID != "person:ana" {
		t.Fatalf("whole-word name match did not outrank description substring: %#v", searchResultIDs(got))
	}
	if got := SearchGlobalItems(items, "NW-401", 3); len(got) == 0 || got[0].ID != "person:worker" {
		t.Fatalf("worker-number prefix did not rank first: %#v", searchResultIDs(got))
	}
	props := GlobalSearchProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}}
	markup, err := ui.RenderToString(globalSearchResults(props, nil, 0, false, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="global-search-loading"`) || !strings.Contains(markup, `role="status"`) {
		t.Fatalf("unresolved search did not render a loading status: %s", markup)
	}
}

func TestTodo_UXBLIND_054_Browser(t *testing.T) {
	props := GlobalSearchProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}}
	markup, err := ui.RenderToString(globalSearchResults(props, nil, 0, false, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-live="polite"`) || !strings.Contains(markup, "Loading") {
		t.Fatalf("browser-facing loading row lacks polite status copy: %s", markup)
	}
}

func TestTodo_UXBLIND_054_Property(t *testing.T) {
	for _, query := range []string{"Ana", "Jordan", "Mira", "Noah"} {
		exact := GlobalSearchItem{ID: "exact", Kind: "person", Label: query}
		noise := GlobalSearchItem{ID: "noise", Kind: "person", Label: "Unrelated", Description: "Project " + query + " Management"}
		got := SearchGlobalItems([]GlobalSearchItem{noise, exact}, query, 2)
		if len(got) == 0 || got[0].ID != exact.ID {
			t.Fatalf("exact-name property failed for %q: %#v", query, searchResultIDs(got))
		}
	}
}

func TestTodo_UXBLIND_055(t *testing.T) {
	view := testView(PageHome)
	props := actionLauncherProps(view)
	markup, err := ui.RenderToString(ui.CreateElement(ActionLauncher, props))
	if err != nil {
		t.Fatal(err)
	}
	triggerName := view.Locale.Text("action_launcher.dialog_title")
	for _, want := range []string{
		`aria-label="` + triggerName + `"`,
		`title="` + triggerName + ` (Alt+K)"`,
		`aria-keyshortcuts="Alt+K"`,
		`aria-label="` + triggerName + `"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("quick-action surface missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Search actions or employees") {
		t.Fatal("action launcher still advertises a duplicate employee search surface")
	}
	if !actionLauncherHasAction(props.Items) {
		t.Fatal("fixture no longer exercises executable actions in the quick launcher")
	}
	if !hasSearchResult(SearchGlobalItems(globalSearchItems(view), "Avery", globalSearchLimit), "person:worker-avery") {
		t.Fatal("global search lost record discovery while action discovery moved to the launcher")
	}
	if got := globalSearchItems(view); hasGlobalSearchKind(got, "action") {
		t.Fatalf("global search still offers executable actions alongside the quick launcher: %#v", got)
	}
	for _, item := range props.Items {
		definition, ok := LookupPage(item.Page)
		if !ok || !item.IsNavigationDestination {
			continue
		}
		if item.Label != view.Locale.Text(definition.LabelKey) {
			t.Fatalf("launcher destination %s is not registry-labeled: %q", item.Page, item.Label)
		}
	}
}

func TestTodo_UXBLIND_055_Browser(t *testing.T) {
	view := testView(PageHome)
	props := actionLauncherProps(view)
	markup, err := ui.RenderToString(ui.CreateElement(ActionLauncher, props))
	if err != nil {
		t.Fatal(err)
	}
	name := view.Locale.Text("action_launcher.dialog_title")
	if !strings.Contains(markup, `id="action-launcher-trigger"`) || !strings.Contains(markup, `title="`+name+` (Alt+K)"`) {
		t.Fatalf("rendered trigger has no discoverable shortcut tooltip: %s", markup)
	}
	if !strings.Contains(markup, `aria-label="`+name+`"`) {
		t.Fatalf("rendered dialog does not use the trigger's name: %s", markup)
	}
}

func TestTodo_UXBLIND_055_Accessibility(t *testing.T) {
	view := testView(PageHome)
	props := actionLauncherProps(view)
	markup, err := ui.RenderToString(ui.CreateElement(ActionLauncher, props))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-keyshortcuts="Alt+K"`) {
		t.Fatal("quick-action trigger does not announce Alt+K")
	}
	triggerName := view.Locale.Text("action_launcher.dialog_title")
	if strings.Count(markup, `aria-label="`+triggerName+`"`) < 2 {
		t.Fatalf("quick-action trigger and dialog do not share one accessible name: %s", markup)
	}
}

func TestTodo_UXBLIND_055_WorkflowDesignerLabelSurvivesHydrationProjection(t *testing.T) {
	view := testView(PageWorkflowDesigner)
	serverProps := actionLauncherProps(view)
	serverMarkup, err := ui.RenderToString(ui.CreateElement(ActionLauncher, serverProps))
	if err != nil {
		t.Fatal(err)
	}
	clientProps := serverProps
	clientProps.Items = []ActionLauncherItem{{
		Page: PagePeople, Kind: ActionLauncherDestination, Action: "view", Availability: ActionState{Availability: ActionAvailable},
		ID: "destination:people", Label: "People", Href: "/workspace/app/people", IsNavigationDestination: true,
	}}
	clientMarkup, err := ui.RenderToString(ui.CreateElement(ActionLauncher, clientProps))
	if err != nil {
		t.Fatal(err)
	}
	name := view.Locale.Text("action_launcher.dialog_title")
	for _, markup := range []string{serverMarkup, clientMarkup} {
		for _, want := range []string{
			`aria-label="` + name + `"`,
			`title="` + name + ` (Alt+K)"`,
			`<strong>` + name + `</strong>`,
		} {
			if !strings.Contains(markup, want) {
				t.Fatalf("Workflow Designer launcher changed its name across the SSR/hydration projections; missing %q: %s", want, markup)
			}
		}
	}
}

func hasGlobalSearchKind(items []GlobalSearchItem, kind string) bool {
	for _, item := range items {
		if item.Kind == kind {
			return true
		}
	}
	return false
}

func TestTodo_UXBLIND_065(t *testing.T) {
	view := testView(PageHome)
	view.NavCollapsed = true
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `class="sidebar collapsed"`) || !strings.Contains(doc, `data-nav-tooltip=`) {
		t.Fatalf("collapsed sidebar has no item tooltip hook: %s", doc)
	}
	if !strings.Contains(doc, `title="My Work"`) || !strings.Contains(doc, `title="People"`) {
		t.Fatalf("collapsed sidebar did not preserve labels in native tooltips: %s", doc)
	}
}

func TestTodo_UXBLIND_065_Browser(t *testing.T) {
	view := testView(PageHome)
	view.NavCollapsed = true
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `aria-label="My Work"`) || !strings.Contains(doc, `aria-label="People"`) {
		t.Fatal("collapsed navigation lost accessible item names")
	}
}

func TestTodo_UXBLIND_065_Accessibility(t *testing.T) {
	view := testView(PageHome)
	view.NavCollapsed = true
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"My Work", "People", "Settings"} {
		if !strings.Contains(doc, `title="`+label+`"`) || !strings.Contains(doc, `data-nav-tooltip="`+label+`"`) {
			t.Errorf("collapsed item %q has no hover/focus label", label)
		}
	}
}
