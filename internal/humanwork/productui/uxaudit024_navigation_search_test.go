package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_UXAUDIT_024_SearchScopesRemainDistinct proves the two shell
// discovery controls cannot be mistaken for one another: the menu field is
// destination-only, while global search remains the record/page surface.
func TestTodo_UXAUDIT_024_SearchScopesRemainDistinct(t *testing.T) {
	view := testView(PageHome)
	markup, err := ui.RenderToString(BuildShell(view, html.Section(html.Props{ID: "uxaudit024-outlet"}), true))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, `data-hcm-search-scope="destinations"`) != 1 {
		t.Fatalf("expected exactly one destination-scoped menu filter, markup has %d", strings.Count(markup, `data-hcm-search-scope="destinations"`))
	}
	if !strings.Contains(markup, `class="menu-filter"`) || !strings.Contains(markup, `aria-label="Filter pages"`) {
		t.Fatal("menu filter is missing its explicit navigation-filter identity")
	}
	if !strings.Contains(markup, `class="global-search"`) || !strings.Contains(markup, `role="search"`) {
		t.Fatal("global search lost its independent search landmark")
	}
	if strings.Contains(markup, `class="menu-filter" role="search"`) {
		t.Fatal("destination filter was rendered as a generic global search landmark")
	}
}

// TestTodo_UXAUDIT_024_MenuFilterNeverBroadensAuthorization proves fuzzy
// matching only projects the supplied authorized navigation answer.
func TestTodo_UXAUDIT_024_MenuFilterNeverBroadensAuthorization(t *testing.T) {
	view := testView(PageHome)
	view.Navigation = view.Navigation[:1]
	view.MenuQuery = "admin"
	props := navigationSidebarProps(view)
	if len(props.Items) != 0 || len(props.Favorites) != 0 {
		t.Fatalf("menu filtering introduced an unauthorized destination: items=%#v favorites=%#v", props.Items, props.Favorites)
	}
	for _, item := range SearchGlobalItems(globalSearchItems(view), "admin", globalSearchLimit) {
		if item.ID == "page:admin" || strings.Contains(strings.ToLower(item.Href), "/admin") {
			t.Fatalf("global search leaked unauthorized admin destination: %#v", item)
		}
	}
}

func TestTodo_UXAUDIT_024_GlobalSearchHonorsActionAndFieldVerdicts(t *testing.T) {
	view := testView(PageHome)
	person := view.People[0]
	view.RecordVerdicts = map[string]AuthorizedRecord{person.ID: {
		ID: person.ID, Disclosable: true,
		Fields: map[string]AuthorizedField{
			"name":              {Effect: PresentationAllow},
			"role":              {Effect: PresentationAllow},
			"organization_unit": {Effect: PresentationWithheld},
			"work_location":     {Effect: PresentationAllow},
			"worker_number":     {Effect: PresentationWithheld},
			"job_code":          {Effect: PresentationWithheld},
		},
	}}
	person.Team = "SECRET_TEAM"
	person.WorkerNumber = "SECRET_WORKER_NUMBER"
	person.JobCode = "SECRET_JOB_CODE"
	view.People[0] = person
	view.LauncherActions = nil
	items := globalSearchItems(view)
	for _, item := range items {
		if strings.Contains(item.Description, "SECRET") || strings.Contains(strings.Join(item.Keywords, " "), "SECRET") {
			t.Fatalf("withheld person field entered search projection: %#v", item)
		}
	}
	if got := SearchGlobalItems(items, "secret_team", globalSearchLimit); len(got) != 0 {
		t.Fatalf("withheld field became a searchable side channel: %#v", got)
	}
	for _, item := range items {
		if strings.HasPrefix(item.ID, "action:promotion:") {
			t.Fatalf("promotion action advertised without an effective launcher grant: %#v", item)
		}
	}
	view.LauncherActions = []LauncherActionProjection{{ID: SemanticActionPromoteWorker, State: ActionState{Availability: ActionAvailable}}}
	if !globalSearchCanStartPromotion(view) {
		t.Fatal("explicit available launcher action was not recognized")
	}
}
