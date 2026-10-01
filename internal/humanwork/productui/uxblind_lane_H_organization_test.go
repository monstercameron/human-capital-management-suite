package productui

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_024(t *testing.T) {
	view := ApplyRequest(testView(PageOrganization), PageRequest{Query: "Linh", OrganizationView: organizationViewFlat})

	profile, _, ok := PageProfiles(PageOrganization)
	if !ok || profile.StateProfile().CarriesDirectoryQuery {
		t.Fatal("Organization must not adopt a remembered People-directory query")
	}
	if got := organizationStateHref(view, "org_view", organizationViewTree); strings.Contains(got, "q=") {
		t.Fatalf("changing Organization view carried the page's search: %q", got)
	}
	cleared := organizationFilterHref(view, "")
	parsed, err := url.Parse(cleared)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := parsed.Query()["q"]; !present {
		t.Fatalf("clearing Organization search did not address q explicitly: %q", cleared)
	}
}

func TestTodo_UXBLIND_024_Browser(t *testing.T) {
	view := ApplyRequest(testView(PageOrganization), PageRequest{Query: "Linh", OrganizationView: organizationViewFlat})
	view.People = append(view.People, Person{ID: "worker-linh", Name: "Linh", Team: "Strategy"})
	doc, err := ui.RenderToString(organizationPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `value="Linh"`) || !strings.Contains(doc, "Clear") {
		t.Fatalf("the Organization search did not render its own active state: %s", doc)
	}
	if strings.Contains(doc, `org_view=tree&amp;q=Linh`) || strings.Contains(doc, `org_view=tree&q=Linh`) {
		t.Fatal("the reporting-lines control carried the Organization search into a view change")
	}
}

func TestTodo_UXBLIND_024_Regression(t *testing.T) {
	for _, page := range []PageID{PageHome, PagePeople, PageOrganization, PageRoles} {
		profile, _, ok := PageProfiles(page)
		if !ok {
			t.Fatalf("page %q has no route profile", page)
		}
		keys := profile.StateProfile().QueryKeys
		ownsQ := false
		for _, key := range keys {
			ownsQ = ownsQ || key == "q"
		}
		switch page {
		case PageHome:
			if ownsQ {
				t.Fatalf("page %q unexpectedly owns the generic q filter", page)
			}
		case PagePeople, PageOrganization, PageRoles:
			if !ownsQ {
				t.Fatalf("page %q lost its page-owned q filter", page)
			}
		}
	}
	if profile, _, _ := PageProfiles(PageOrganization); profile.StateProfile().CarriesDirectoryQuery {
		t.Fatal("Organization route regressed to a cross-page remembered search")
	}
}

func TestTodo_UXBLIND_025(t *testing.T) {
	view := testView(PageOrganization)
	view.People = []Person{
		{ID: "worker-a", Name: "A", Team: "Care"},
		{ID: "worker-b", Name: "B", Team: "Care"},
		{ID: "worker-c", Name: "C", Team: "Finance", PromotionAvailability: PromotionEligible},
	}
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"worker-a": {ID: "worker-a", Disclosable: true},
		"worker-b": {ID: "worker-b", Disclosable: false},
		"worker-c": {ID: "worker-c", Disclosable: true},
	}
	population := visibleWorkforcePeople(view)
	summary := visibleWorkforceSummary(population, false, organizationGraphProjection{})
	if summary.Workforce != 2 || summary.Units != 2 {
		t.Fatalf("visible workforce summary = %+v, want two admitted workers in two units", summary)
	}
	peopleView := view
	peopleView.Page = PagePeople
	peopleView.PeopleEligibleOnly = true
	if got := len(filteredPeople(peopleView)); got != 1 {
		t.Fatalf("eligible People projection = %d, want one of the two visible workers", got)
	}
	if got := len(visibleWorkforcePeople(peopleView)); got != summary.Workforce {
		t.Fatalf("People denominator = %d, want Organization workforce %d", got, summary.Workforce)
	}
}

func TestTodo_UXBLIND_025_Browser(t *testing.T) {
	view := testView(PageOrganization)
	view.People = []Person{
		{ID: "worker-a", Name: "A", Team: "Care"},
		{ID: "worker-b", Name: "B", Team: "Care", PromotionAvailability: PromotionEligible},
		{ID: "worker-c", Name: "C", Team: "Finance", PromotionAvailability: PromotionEligible},
	}
	organizationDoc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	workforceLabel := strings.Contains(organizationDoc, `>3</strong><small>Visible workforce</small>`)
	unitsLabel := strings.Contains(organizationDoc, `>2</strong><small>Organization units</small>`)
	if !workforceLabel || !unitsLabel {
		t.Fatalf("Organization summary labels workforce=%t units=%t", workforceLabel, unitsLabel)
	}
	peopleView := view
	peopleView.Page = PagePeople
	peopleView.PeopleEligibleOnly = true
	peopleDoc, err := Render(peopleView)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(peopleDoc, "2 eligible of 3 you can see") {
		t.Fatalf("People did not name the filtered count against the shared visible workforce")
	}
	homeView := view
	homeView.Page = PageHome
	homeDoc, err := Render(homeView)
	if err != nil {
		t.Fatal(err)
	}
	if got := homeFactValue(homeDoc, view.Locale.Text("home.visible_workers")); got != "3" {
		t.Fatalf("Home People you can see count = %q, want 3", got)
	}
	insightsView := view
	insightsView.Page = PageInsights
	insightsDoc, err := Render(insightsView)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(insightsDoc, "insights-workforce") || !strings.Contains(insightsDoc, view.Locale.Text("insights.headcount_by_unit")) {
		t.Fatalf("Insights did not render the shared workforce projection: %s", insightsDoc)
	}
}

func TestTodo_UXBLIND_025_Property(t *testing.T) {
	personas := []struct {
		name   string
		tenant string
	}{
		{name: "Rafael at HarborCare", tenant: "harborcare-demo"},
		{name: "Darius at HarborCare", tenant: "harborcare-demo"},
		{name: "Thomas at HarborCare", tenant: "harborcare-demo"},
		{name: "Linh at HarborCare", tenant: "harborcare-demo"},
		{name: "Walt at Ironridge", tenant: "ironridge-demo"},
		{name: "Curtis at Ironridge", tenant: "ironridge-demo"},
		{name: "Loretta at Ironridge", tenant: "ironridge-demo"},
		{name: "Ana at Ironridge", tenant: "ironridge-demo"},
	}
	for _, persona := range personas {
		t.Run(persona.name, func(t *testing.T) {
			base := NewView(PageOrganization, persona.tenant, persona.name, "demo-scope")
			base.People = []Person{
				{ID: "active-care", Team: "Care", Location: "Boston", LifecycleStatus: "ACTIVE", PromotionAvailability: PromotionEligible},
				{ID: "active-finance", Team: "Finance", Location: "New York", LifecycleStatus: "ACTIVE"},
				{ID: "terminated-care", Team: "Care", Location: "Boston", LifecycleStatus: "TERMINATED", PromotionAvailability: PromotionEligible},
				{ID: "leave-finance", Team: "Finance", Location: "New York", LifecycleStatus: "ON_LEAVE", PromotionAvailability: PromotionEligible},
			}
			base.RecordVerdicts = map[string]AuthorizedRecord{
				"active-care": {ID: "active-care", Disclosable: true}, "active-finance": {ID: "active-finance", Disclosable: true},
				"terminated-care": {ID: "terminated-care", Disclosable: true}, "leave-finance": {ID: "leave-finance", Disclosable: true},
			}
			organizationView := base
			peopleView := base
			peopleView.Page = PagePeople
			peopleView.PeopleEligibleOnly = true
			homeView := base
			homeView.Page = PageHome
			insightsView := base
			insightsView.Page = PageInsights
			organizationPopulation := visibleWorkforcePeople(organizationView)
			peoplePopulation := visibleWorkforcePeople(peopleView)
			if len(organizationPopulation) != len(peoplePopulation) {
				t.Fatalf("visible workforce differs across pages: Organization=%d People=%d", len(organizationPopulation), len(peoplePopulation))
			}
			summary := visibleWorkforceSummary(organizationPopulation, false, organizationGraphProjection{})
			if summary.Workforce != 2 || summary.Units != 2 {
				t.Fatalf("summary = %+v, want two active workers in two units", summary)
			}
			if got := len(filteredPeople(peopleView)); got != 1 {
				t.Fatalf("eligible People count = %d, want one active eligible worker", got)
			}
			if got := peopleCountLabel(peopleView.Locale, true, len(filteredPeople(peopleView)), len(peoplePopulation), true); got != "1 eligible of 2 you can see" {
				t.Fatalf("People denominator label = %q", got)
			}
			groups := homeOperationalGroups(homeView, JourneyPopulation{}, 0, true, true, true, visibleWorkforcePeople(homeView))
			visibleHome, eligibleHome := "", ""
			for _, group := range groups {
				for _, fact := range group.Facts {
					switch fact.Label {
					case homeView.Locale.Text("home.visible_workers"):
						visibleHome = fact.Value
					case homeView.Locale.Text("home.eligible_workers"):
						eligibleHome = fact.Value
					}
				}
			}
			if visibleHome != "2" || eligibleHome != "1" {
				t.Fatalf("Home people counts = visible %q, eligible %q; want 2 and 1", visibleHome, eligibleHome)
			}
			insights := insightsWorkforce(insightsView, nil)
			if len(insights.UnitFacts) != summary.Units || len(insights.LocationFacts) != summary.Units {
				t.Fatalf("Insights dimensions = units %d, locations %d; want %d", len(insights.UnitFacts), len(insights.LocationFacts), summary.Units)
			}
			unitTotal, locationTotal := 0, 0
			for _, fact := range insights.UnitFacts {
				value, err := strconv.Atoi(fact.Value)
				if err != nil {
					t.Fatalf("Insights unit fact %q is not numeric: %v", fact.Value, err)
				}
				unitTotal += value
			}
			for _, fact := range insights.LocationFacts {
				value, err := strconv.Atoi(fact.Value)
				if err != nil {
					t.Fatalf("Insights location fact %q is not numeric: %v", fact.Value, err)
				}
				locationTotal += value
			}
			if unitTotal != summary.Workforce || locationTotal != summary.Workforce {
				t.Fatalf("Insights totals = units %d, locations %d; want %d", unitTotal, locationTotal, summary.Workforce)
			}
		})
	}
}
