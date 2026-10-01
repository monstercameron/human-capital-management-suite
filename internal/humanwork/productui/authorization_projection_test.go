package productui

import (
	"strings"
	"testing"
)

func TestInsightsCountsOnlyDiscoverableWorkflows(t *testing.T) {
	view := testView(PageInsights)
	view.Work = []WorkItem{{ID: "visible", Status: "Blocked"}, {ID: "hidden", Terminal: true}, {ID: "unclassified"}}
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"visible": {ID: "visible", Disclosable: true},
		"hidden":  {ID: "hidden", Disclosable: false},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	// UXBLIND-106: the first Insights metric is labelled Promotion requests; the count is unchanged.
	for _, want := range []string{`Promotion requests</span><strong>1</strong>`, `In progress</span><strong>1</strong>`, `Completed or closed</span><strong>0</strong>`} {
		if !strings.Contains(doc, want) {
			t.Errorf("admitted metric missing: %s", want)
		}
	}
}

func TestHomeAndInsightsDoNotAdvertiseUnauthorizedRoutes(t *testing.T) {
	permissions := []RolePagePermission{
		{Page: PageHome, View: true},
		{Page: PageMyself, View: true},
		{Page: PageOrganization, View: true},
		{Page: PageInsights, View: true},
		{Page: PageHelp, View: true},
		{Page: PageSettings, View: true},
	}
	for _, page := range []PageID{PageHome, PageInsights} {
		view := ApplyPagePermissions(testView(page), permissions)
		markup, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"Start a promotion", "Choose a worker", "Open live work", "Open My Work"} {
			if strings.Contains(markup, forbidden) {
				t.Errorf("%s advertises unauthorized action %q", page, forbidden)
			}
		}
		if page == PageHome && strings.Contains(markup, `aria-label="Filter work"`) {
			t.Error("home mounted the unauthorized work collection")
		}
	}
}

func TestWorkflowAvailabilityIsCountedAfterAuthorization(t *testing.T) {
	view := ApplyPagePermissions(testView(PagePerson), []RolePagePermission{{Page: PagePerson, View: true}})
	person, ok := exactPerson(view)
	if !ok {
		t.Fatal("expected selected test person")
	}
	props := personWorkflowLauncherProps(view, person, PagePerson)
	if props.TotalCount != 0 || len(props.Workflows) != 0 {
		t.Fatalf("unauthorized workflow projection = count %d, cards %d", props.TotalCount, len(props.Workflows))
	}
}

func TestHomeGreetingUsesViewerDisplayName(t *testing.T) {
	view := testView(PageHome)
	view.Principal = "hc-050-rafael-torres"
	view.Viewer.Name = "Rafael Torres"
	view = ApplyLocale(view, ResolveProductLocale("en-US"))
	if view.Title != "Good morning, Rafael Torres." {
		t.Fatalf("home title = %q", view.Title)
	}
}
