package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_101(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	generic := "No eligible promotion role is published for this employee. Ask your HR administrator to review the job ladder and pay band."
	profile := PersonProfileProps{
		Organization: EmploymentDetailsProps{
			I18nProps: I18nProps{Locale: locale},
			Facts:     []ProfileFactProps{{Label: locale.Text("person.manager"), Value: locale.Text("common.not_reported"), Status: WorkerFactMissing}},
		},
		Workflows: WorkflowLauncherProps{
			I18nProps:         I18nProps{Locale: locale},
			TotalCount:        1,
			UnavailableDetail: generic,
			Workflows:         []WorkflowCardProps{{Name: "Promotion", Href: "/workspace/app/journeys?mode=new&worker=self"}},
		},
	}

	got := myselfProfileWithoutSelfPromotion(profile, "You cannot request a promotion for your own record.")
	if got.Workflows.TotalCount != 0 || len(got.Workflows.Workflows) != 0 {
		t.Fatalf("self-promotion entry remained available: %+v", got.Workflows)
	}
	if got.Workflows.UnavailableDetail == generic || strings.Contains(got.Workflows.UnavailableDetail, "Ask your HR administrator") {
		t.Fatalf("generic administrator advice survived the self-service refusal: %q", got.Workflows.UnavailableDetail)
	}

	got = myselfProfileWithTopOrganizationFact(got, []OwnershipNodeProps{{ID: "self"}}, locale.Text("organization.top_of_organization"))
	manager := got.Organization.Facts[0]
	if manager.Value != locale.Text("organization.top_of_organization") || manager.Status != WorkerFactPresent {
		t.Fatalf("top-of-organization manager fact was not projected: %+v", manager)
	}
}

func TestTodo_UXBLIND_101_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	profile := PersonProfileProps{
		Hero: PersonHeroProps{I18nProps: I18nProps{Locale: locale}, Name: "Walt Brennan", NameStatus: WorkerFactPresent},
		Organization: EmploymentDetailsProps{
			I18nProps: I18nProps{Locale: locale},
			Title:     locale.Text("person.organization"),
			Facts:     []ProfileFactProps{{Label: locale.Text("person.manager"), Value: locale.Text("common.not_reported"), Status: WorkerFactMissing}},
		},
		Workflows: WorkflowLauncherProps{
			I18nProps:         I18nProps{Locale: locale},
			PersonName:        "Walt Brennan",
			Heading:           locale.Text("workflow.start"),
			TotalCount:        1,
			UnavailableDetail: "No eligible promotion role is published for this employee. Ask your HR administrator to review the job ladder and pay band.",
			Workflows:         []WorkflowCardProps{{Name: "Promotion", Href: "/workspace/app/journeys?mode=new&worker=self"}},
		},
	}
	doc, err := ui.RenderToString(MyselfPage(MyselfPageProps{
		I18nProps:               I18nProps{Locale: locale},
		Profile:                 &profile,
		OrganizationTitle:       locale.Text("organization.structure_title"),
		OrganizationDescription: locale.Text("organization.structure_description"),
		OrganizationTree:        []OwnershipNodeProps{{ID: "self"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{locale.Text("workflow.promotion_self_subject"), locale.Text("organization.top_of_organization")} {
		if !strings.Contains(doc, want) {
			t.Errorf("Myself render missing %q: %s", want, doc)
		}
	}
	if strings.Contains(doc, "Ask your HR administrator") || strings.Contains(doc, "mode=new&amp;worker=self") {
		t.Fatalf("Myself render retained the generic advice or forbidden launch link: %s", doc)
	}
}

func TestTodo_UXBLIND_102(t *testing.T) {
	view := testView(PageWork)
	_, items := projectNavigation(view)
	group := ssNavigationItemByPage(items, PageWork)
	if group == nil {
		t.Fatal("My Work navigation group was not projected")
	}
	child := ssNavigationItemByPage(group.Children, PageWork)
	if child == nil {
		t.Fatal("My Work navigation overview child was not projected")
	}
	if group.Label == child.Label || child.Label != view.Locale.Text("home.needs_action") {
		t.Fatalf("My Work child remains ambiguous: group=%q child=%q", group.Label, child.Label)
	}
	collection := workCollectionProps(view, workCollectionOptions{Title: view.Locale.Text("page.work.label")})
	for _, tab := range collection.Tabs {
		if strings.Contains(tab.Href, "filter=review") && tab.Label != view.Locale.Text("work.awaiting_my_approval") {
			t.Fatalf("review tab is ambiguous: %+v", tab)
		}
	}
}

func TestTodo_UXBLIND_102_Browser(t *testing.T) {
	view := testView(PageWork)
	_, items := projectNavigation(view)
	group := ssNavigationItemByPage(items, PageWork)
	if group == nil {
		t.Fatal("My Work navigation group was not projected")
	}
	doc, err := ui.RenderToString(ui.CreateElement(NavigationItem, *group))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, view.Locale.Text("home.needs_action")) || strings.Contains(doc, ">"+view.Locale.Text("page.work.label")+"</a>") {
		t.Fatalf("navigation render does not distinguish the My Work child: %s", doc)
	}
}

func TestTodo_UXBLIND_103(t *testing.T) {
	view := testView(PageOrganization)
	view.OrganizationView = ""
	view.People = []Person{
		{ID: "chief", WorkerID: "chief-worker", Name: "Executive", Team: "Executive"},
		{ID: "team", WorkerID: "team-worker", Name: "People Operations", Team: "People", ManagerID: "chief-worker"},
	}
	doc, err := ui.RenderToString(organizationPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-organization-view="tree"`, `data-organization-summary="true"`, `organization-summary-facts`} {
		if !strings.Contains(doc, want) {
			t.Errorf("organization render missing %q: %s", want, doc)
		}
	}
	if strings.Contains(doc, `class="organization-search-summary"`) || strings.Contains(doc, `class="button primary"`) {
		t.Fatalf("organization render retained redundant search controls or summary: %s", doc)
	}
}

func TestTodo_UXBLIND_103_Browser(t *testing.T) {
	view := testView(PageOrganization)
	view.OrganizationView = ""
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `data-organization-view="tree"`) || !strings.Contains(doc, `data-organization-summary="true"`) {
		t.Fatalf("served organization page did not open as a hierarchy with a summary: %s", doc)
	}
	if strings.Contains(doc, `class="organization-search-summary"`) {
		t.Fatal("served organization page exposed a redundant people count below search")
	}
}

func TestTodo_UXBLIND_104(t *testing.T) {
	view := testView(PageMyself)
	view.FavoritePages = []PageID{PageMyself}
	favorites, items := projectNavigation(view)
	favorite := ssNavigationItemByPage(favorites, PageMyself)
	main := ssNavigationItemByPage(items, PageMyself)
	if favorite == nil || main == nil {
		t.Fatalf("favorited Myself entries were not projected: favorites=%+v items=%+v", favorites, items)
	}
	if favorite.Active || !main.Active {
		t.Fatalf("favorited page has the wrong active state: favorite=%t main=%t", favorite.Active, main.Active)
	}
}

func TestTodo_UXBLIND_104_Browser(t *testing.T) {
	view := testView(PageMyself)
	view.FavoritePages = []PageID{PageMyself}
	favorites, items := projectNavigation(view)
	favorite := ssNavigationItemByPage(favorites, PageMyself)
	main := ssNavigationItemByPage(items, PageMyself)
	if favorite == nil || main == nil {
		t.Fatal("favorited Myself entries were not projected")
	}
	favoriteDoc, err := ui.RenderToString(ui.CreateElement(NavigationItem, *favorite))
	if err != nil {
		t.Fatal(err)
	}
	mainDoc, err := ui.RenderToString(ui.CreateElement(NavigationItem, *main))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(favoriteDoc, `aria-current="page"`) || strings.Count(mainDoc, `aria-current="page"`) != 1 {
		t.Fatalf("favorite/current navigation markup doubled the current marker: favorite=%s main=%s", favoriteDoc, mainDoc)
	}
}

func ssNavigationItemByPage(items []NavigationItemProps, page PageID) *NavigationItemProps {
	for index := range items {
		if items[index].Page == page {
			return &items[index]
		}
		if found := ssNavigationItemByPage(items[index].Children, page); found != nil {
			return found
		}
	}
	return nil
}
