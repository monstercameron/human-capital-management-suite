package productui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func uxblindWorkflowStartItems() []WorkflowStartItem {
	return []WorkflowStartItem{
		{WorkflowID: "new-hire", Version: 1, Name: "New hire", Description: "Collect the information needed to welcome a new employee.", Category: "People", Keywords: []string{"onboarding", "employee"}, Icon: "people", Availability: WorkflowStartAvailable},
		{WorkflowID: "promotion", Version: 2, Name: "Promotion review", Description: "Request a governed promotion review.", Category: "People", Keywords: []string{"career", "raise"}, Icon: "journeys", Availability: WorkflowStartMissingAuthority},
	}
}

func TestTodo_WFPAGE_003(t *testing.T) {
	items := uxblindWorkflowStartItems()
	got := RankWorkflowStartItems(items, "new hire")
	if len(got) != 1 || got[0].WorkflowID != "new-hire" {
		t.Fatalf("type-ahead ranking = %+v, want New hire first and only", got)
	}
	markup, err := ui.RenderToString(WorkflowStartPage(WorkflowStartPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Catalog: items}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="workflow-start-search-input"`, "New hire", "Promotion review", "People", "Start"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("start page missing %q:\n%s", want, markup)
		}
	}
}

func TestTodo_WFPAGE_003_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := Render(func() View {
			view := NewView(PageWorkflowStart, "tenant-a", "viewer-a", "scope-a")
			view.Locale = ResolveProductLocale(locale)
			view.WorkflowStartCatalog = uxblindWorkflowStartItems()
			return view
		}())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, `id="workflow-start-search-input"`) || !strings.Contains(markup, `dir="`+string(ResolveProductLocale(locale).Direction)+`"`) {
			t.Fatalf("%s start page did not render localized search and direction", locale)
		}
	}
}

func TestTodo_WFPAGE_003_Performance(t *testing.T) {
	items := make([]WorkflowStartItem, 500)
	for index := range items {
		items[index] = WorkflowStartItem{WorkflowID: "workflow-" + string(rune('a'+index%26)), Name: "Workflow " + string(rune('a'+index%26)), Category: "People", Keywords: []string{"new hire", "employee"}, Availability: WorkflowStartAvailable}
	}
	started := time.Now()
	got := RankWorkflowStartItems(items, "new hire")
	if len(got) != len(items) || time.Since(started) > 250*time.Millisecond {
		t.Fatalf("500-item filter returned %d in %s", len(got), time.Since(started))
	}
}

func TestTodo_WFPAGE_004(t *testing.T) {
	markup, err := ui.RenderToString(WorkflowStartPage(WorkflowStartPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Catalog: uxblindWorkflowStartItems(), Favorites: []string{"new-hire"}, Recent: []string{"new-hire", "promotion"}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Favourites") || !strings.Contains(markup, "Recently started") || strings.Count(markup, `data-workflow-id="new-hire"`) < 3 {
		t.Fatalf("favorites/recent sections missing or unavailable recent leaked:\n%s", markup)
	}
}

func TestTodo_WFPAGE_004_Browser(t *testing.T) {
	items := uxblindWorkflowStartItems()
	if got := workflowStartResolveIDs(items, []string{"promotion", "new-hire", "new-hire"}, true); len(got) != 1 || got[0].WorkflowID != "new-hire" {
		t.Fatalf("unavailable or duplicate favorite was retained: %+v", got)
	}
	ids := workflowStartRecentIDs([]string{"new-hire", "new-hire", "promotion", "stale"})
	if len(ids) != 3 || ids[0] != "new-hire" || ids[1] != "promotion" || ids[2] != "stale" {
		t.Fatalf("recent ids were not newest-first and distinct: %v", ids)
	}
}

func TestTodo_WFPAGE_004_RecentLimit(t *testing.T) {
	ids := make([]string, 0, workflowStartRecentLimit+2)
	for i := 0; i < workflowStartRecentLimit+2; i++ {
		ids = append(ids, "workflow-"+strconv.Itoa(i))
	}
	got := workflowStartRecentIDs(ids)
	if len(got) != workflowStartRecentLimit || got[0] != "workflow-0" || got[len(got)-1] != "workflow-9" {
		t.Fatalf("recent ids = %v, want newest ten", got)
	}
}

func TestTodo_WFPAGE_005(t *testing.T) {
	for _, props := range []WorkflowStartPageProps{
		{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}},
		{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Catalog: uxblindWorkflowStartItems(), DeepLinkID: "promotion"},
		{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Catalog: uxblindWorkflowStartItems(), DeepLinkID: "hidden-id"},
	} {
		markup, err := ui.RenderToString(WorkflowStartPage(props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, `id="workflow-start-heading"`) {
			t.Fatalf("state has no stable heading:\n%s", markup)
		}
	}
}

func TestTodo_WFPAGE_005_Browser(t *testing.T) {
	view := NewView(PageWorkflowStart, "tenant-a", "viewer-a", "scope-a")
	if got := WorkflowStartHref(view, "workflow/new-hire"); got != "/workspace/app/workflows/start/workflow%2Fnew-hire" {
		t.Fatalf("deep-link href = %q", got)
	}
}

func TestTodo_WFPAGE_005_Golden(t *testing.T) {
	markup, err := ui.RenderToString(WorkflowStartPage(WorkflowStartPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "No workflow is available to you") || !strings.Contains(markup, "No workflows have been published in this workspace yet") {
		t.Fatalf("empty-state golden changed:\n%s", markup)
	}
}

func TestTodo_WFPAGE_006(t *testing.T) {
	view := NewView(PageWorkflowStart, "tenant-a", "viewer-a", "scope-a")
	view.Navigation = []NavItem{{Page: PageWorkflowStart, Label: "Workflows", Description: "Start workflows", Keywords: []string{"workflow"}}}
	view.WorkflowStartCatalog = uxblindWorkflowStartItems()
	items := globalSearchItems(view)
	results := SearchGlobalItems(items, "new hire", 10)
	if len(results) == 0 || results[0].ID != "workflow-start:new-hire" {
		t.Fatalf("global search workflow result = %+v", results)
	}
}

func TestTodo_WFPAGE_006_Browser(t *testing.T) {
	view := NewView(PageWorkflowStart, "tenant-a", "scope", "scope")
	view.WorkflowStartCatalog = uxblindWorkflowStartItems()
	view.WorkflowStartFavorites = []string{"new-hire"}
	actions := ResolveHomeQuickActions(view)
	for _, action := range actions {
		if strings.Contains(action.Label, "New hire") {
			return
		}
	}
	t.Fatal("favorite workflow was not offered as a Home quick action")
}

func TestTodo_WFPAGE_005_EmptyStateNamesTheRealReason(t *testing.T) {
	render := func(canDesign bool, deep string, catalog []WorkflowStartItem) string {
		markup, err := ui.RenderToString(WorkflowStartPage(WorkflowStartPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, CanDesign: canDesign, DeepLinkID: deep, Catalog: catalog}))
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}
	designer := render(true, "", nil)
	if !strings.Contains(designer, "Publish one in the Workflow Designer") || strings.Contains(designer, "administrator") {
		t.Fatalf("a designer must be told the real cause, not to ask an administrator:\n%s", designer)
	}
	worker := render(false, "", nil)
	if !strings.Contains(worker, "No workflows have been published") || strings.Contains(worker, "Ask your") {
		t.Fatalf("worker empty state:\n%s", worker)
	}
	unavailable := render(true, "promotion", uxblindWorkflowStartItems())
	if strings.Contains(unavailable, "administrator") || !strings.Contains(unavailable, "Additional authority is required") {
		t.Fatalf("designer unavailable state:\n%s", unavailable)
	}
	if !workflowStartViewerCanDesign([]NavItem{{Page: PageAdmin, Children: []NavItem{{Page: PageWorkflowDesigner}}}}) || workflowStartViewerCanDesign([]NavItem{{Page: PageHome}}) {
		t.Fatal("designer reach must follow the viewer's own navigation, children included")
	}
}

func TestTodo_UXBLIND_119(t *testing.T) {
	item := WorkflowStartItem{
		WorkflowID:   "clock-in-out",
		Name:         "Clock in and clock out",
		Description:  "Record\nwhen you begin and finish work.",
		Category:     "Time",
		Availability: WorkflowStartAvailable,
	}
	markup, err := ui.RenderToString(WorkflowStartPage(WorkflowStartPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Catalog:   []WorkflowStartItem{item},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="button primary workflow-start-open"`,
		">Start Clock in and clock out</a>",
		"Record when you begin and finish work.",
		`aria-label="Add to favourites"`,
		`title="Add to favourites"`,
		`aria-pressed="false"`,
		`class="workflow-start-favorite-icon"`,
		`d="M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 7v5l3 2"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow start card missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, `class="workflow-start-availability"`) || strings.Contains(markup, ">Available<") {
		t.Fatalf("available card exposed a duplicate availability status:\n%s", markup)
	}

	item.Availability = WorkflowStartMissingAuthority
	markup, err = ui.RenderToString(WorkflowStartPage(WorkflowStartPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Catalog:   []WorkflowStartItem{item},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="workflow-start-unavailable"`) || !strings.Contains(markup, "Additional authority is required") {
		t.Fatalf("unavailable card did not explain its reason:\n%s", markup)
	}
	if strings.Contains(markup, `class="button primary workflow-start-open"`) || strings.Count(markup, "Additional authority is required") != 1 {
		t.Fatalf("unavailable card rendered an action or duplicate reason:\n%s", markup)
	}
}

func TestTodo_UXBLIND_119_Browser(t *testing.T) {
	locales := []struct {
		name, action, add, remove string
	}{
		{name: "en-US", action: "Start New hire", add: "Add to favourites", remove: "Remove from favourites"},
		{name: "de-DE", action: "New hire starten", add: "Zu Favoriten hinzufügen", remove: "Aus Favoriten entfernen"},
		{name: "ar", action: "بدء New hire", add: "إضافة إلى المفضلة", remove: "إزالة من المفضلة"},
	}
	for _, locale := range locales {
		view := NewView(PageWorkflowStart, "tenant-a", "viewer-a", "scope-a")
		view.Locale = ResolveProductLocale(locale.name)
		view.WorkflowStartCatalog = []WorkflowStartItem{{
			WorkflowID: "new-hire", Name: "New hire", Description: "Collect information for a new employee.", Category: "People", Availability: WorkflowStartAvailable,
		}}
		markup, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, `class="workflow-start-card-icon"`) || strings.Contains(markup, `class="workflow-start-availability"`) {
			t.Fatalf("%s card did not render a meaningful icon without duplicate availability:\n%s", locale.name, markup)
		}
		for _, want := range []string{locale.action, locale.add} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s missing localized card copy %q:\n%s", locale.name, want, markup)
			}
		}
		view.WorkflowStartFavorites = []string{"new-hire"}
		markup, err = Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, locale.remove) || !strings.Contains(markup, `aria-pressed="true"`) {
			t.Fatalf("%s missing localized favorited state %q:\n%s", locale.name, locale.remove, markup)
		}
	}
}
