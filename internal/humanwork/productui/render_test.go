package productui

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenderEveryAuthorizedProductPage(t *testing.T) {
	for _, definition := range PageDefinitions() {
		definition := definition
		t.Run(string(definition.ID), func(t *testing.T) {
			doc, err := Render(testView(definition.ID))
			if err != nil {
				t.Fatal(err)
			}

			for _, want := range []string{"Human Capital Management Suite", `id="main-content"`, `aria-label="Main"`, "manager", "Workspace information"} {
				if !strings.Contains(doc, want) {
					t.Fatalf("document missing %q", want)
				}
			}
			if strings.Contains(strings.ToLower(doc), "medical leave") || strings.Contains(doc, "<script") {
				t.Fatal("document leaked restricted detail or a JavaScript application runtime")
			}
			again, _ := Render(testView(definition.ID))
			if doc != again {
				t.Fatal("render is not deterministic")
			}
		})
	}
}

func TestPersonPageUsesLazySmallPhotoProxy(t *testing.T) {
	doc, err := Render(testView(PagePerson))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`src="/workspace/assets/person-jane-small.jpg"`,
		`loading="lazy"`,
		`decoding="async"`,
		`width="64"`,
		`height="64"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("person photo is missing %q", want)
		}
	}
	if strings.Contains(doc, `src="/workspace/assets/person-jane.png"`) {
		t.Fatal("person page loaded the retained original instead of its display proxy")
	}
}

func TestShellOwnsViewportAndSeparatesNavigationFromContentScroll(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `id="main-content"`) || !strings.Contains(doc, `class="main-scroll"`) || !strings.Contains(doc, `<div class="main network-stage network-stage-ready" data-network-state="ready">`) {
		t.Fatal("main workspace is not wrapped as its own scroll region")
	}
	css := Stylesheet()
	for _, want := range []string{
		`html,body,#app{height:100%;overflow:hidden;width:100%;}`,
		`.sidebar{height:100%;min-height:0;overflow-y:auto;overscroll-behavior:contain;}`,
		`.main-scroll{background-color:var(--canvas);height:100%;min-height:0;min-width:0;overflow-x:hidden;overflow-y:auto;`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("viewport shell is missing independent scroll contract %q", want)
		}
	}
}

func TestProductionPagesNeverLinkToJavaScriptReference(t *testing.T) {
	for _, definition := range PageDefinitions() {
		doc, err := Render(testView(definition.ID))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, `href="/design`) {
			t.Fatalf("%s links the production Go UI to the JavaScript reference", definition.ID)
		}
	}
}

func TestExperienceStudioDoesNotSimulateAnUnpublishedService(t *testing.T) {
	doc, err := Render(testView(PageStudio))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Custom pages cannot be edited here yet") || strings.Contains(doc, "Validation passed") || strings.Contains(doc, "Request publication") {
		t.Fatal("Studio simulated a configuration service the cell did not publish")
	}
}

func TestAuthorizationResolvedNavigationDoesNotInventHiddenPages(t *testing.T) {
	view := testView(PageHome)
	view.Navigation = view.Navigation[:2]
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, ">People<") || strings.Contains(doc, ">Admin<") || strings.Contains(doc, ">Experience Studio<") {
		t.Fatal("renderer invented a page omitted by the authorized projection")
	}
}

// TestExperienceStudioIsOmittedFromAuthorizedAdminNavigation replaces the
// prior expectation (RED for UXAUDIT-011) that Studio nested under Admin's
// menu. The registry's admission gate now keeps any page without a genuine
// capability -- Studio included -- out of navigation entirely; omission
// from the menu is not deletion of the route, so it still resolves
// directly and still explains why it is unavailable.
func TestExperienceStudioIsOmittedFromAuthorizedAdminNavigation(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "Customize workspace") {
		t.Fatal("vertical-slice customization control escaped into the production header")
	}
	if strings.Contains(doc, `>Experience Studio</span>`) {

		t.Fatal("Experience Studio still claims a menu slot under Admin")
	}
	navigation := findElementByID(mustParse(t, doc), "workspace-navigation")
	if linkForRoute(navigation, "/workspace/app/studio") != nil {
		t.Fatal("Experience Studio still renders a navigation link")
	}

	doc, err = Render(testView(PageStudio))
	if err != nil {
		t.Fatal(err)
	}

	if primaryNavAriaCurrentCount(doc) != 0 || strings.Contains(doc, `class="nav-group current"`) {
		t.Fatal("an unadmitted route falsely claimed an active navigation leaf")
	}
	if !strings.Contains(doc, "Custom pages cannot be edited here yet") {
		t.Fatal("Studio route no longer explains its unavailable state directly")
	}
}

func TestCollapsedNavigationIsAccessibleAndPersistsAcrossPages(t *testing.T) {
	view := testView(PageStudio)
	view.NavCollapsed = true
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="app-shell nav-collapsed"`,
		`aria-label="Expand navigation"`,
		`href="/workspace/app/work?nav=collapsed"`,
		`class="nav-icon"`,
		`name="nav"`,
		`value="collapsed"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("collapsed navigation missing %q", want)
		}
	}
	if strings.Contains(doc, "Customize workspace") {
		t.Fatal("prototype-only customization control escaped into the collapsed shell")
	}

	expanded, err := Render(testView(PageStudio))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expanded, `aria-label="Collapse navigation"`) {
		t.Fatal("expanded navigation has no accessible collapse control")
	}

	view = testView(PageStudio)
	view.Mode = "validate"
	withLocalState, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withLocalState, `href="/workspace/app/studio?mode=validate&amp;nav=collapsed"`) {
		t.Fatal("collapsing the shell discarded the active Studio state")
	}
}

func TestCollapsedNavigationPersistsAcrossPageActions(t *testing.T) {
	view := testView(PageHome)
	view.NavCollapsed = true
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="/workspace/app/work?filter=review&amp;nav=collapsed"`,
		`href="/workspace/app/people?nav=collapsed"`,
		`href="/workspace/app/settings?nav=collapsed"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("collapsed page action discarded navigation state: missing %q", want)
		}
	}
	if strings.Contains(doc, `/workspace/app/home?filter=review`) {
		t.Fatal("home work filter must route to My Work, not attach a work-only query to Home")
	}
}

func TestWorkFilterIsActiveAndSurvivesSelectionAndToggle(t *testing.T) {
	view := ApplyRequest(testView(PageWork), PageRequest{WorkFilter: "review", NavCollapsed: true})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="tab active" href="/workspace/app/work?filter=review&amp;nav=collapsed"`,
		`href="/workspace/app/work?filter=review&amp;nav=collapsed&amp;selected=intent-1"`,
		`aria-label="Expand navigation"`,
		`href="/workspace/app/work?filter=review&amp;nav=expanded&amp;selected=intent-1"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("filtered work navigation missing %q", want)
		}
	}
}

func TestMyWorkKeepsTerminalJourneysInHistory(t *testing.T) {
	view := testView(PageWork)
	view.SelectedWork = "intent-2"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"Needs your action", "Jordan Lee", "Past workflows", "Nothing selected"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("open-work projection missing %q", want)
		}
	}
	if strings.Contains(doc, "Avery Patel") {
		t.Fatal("terminal journey leaked into the open My Work collection")
	}
	if !strings.Contains(doc, `aria-label="Work overview, 1 promotion item needs your action."`) {
		t.Fatal("work overview announced the total history instead of the open-work count")
	}
}

func TestMyWorkSelectsTheFirstOpenJourneyWhenTheURLHasNoSelection(t *testing.T) {
	view := testView(PageWork)
	view.SelectedWork = ""
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "Nothing selected") {
		t.Fatal("default My Work route left a populated open-work list without a preview")
	}
	if !strings.Contains(doc, `class="work-row selected"`) || !strings.Contains(doc, "Open live journey") {
		t.Fatal("default My Work route did not align its selected row and preview")
	}
}

func TestWorkflowHistoryShowsOnlyTerminalRecordsAndPreservesFilters(t *testing.T) {
	view := testView(PageHistory)
	view.HistoryQuery = "Avery"
	view.HistoryOutcome = "completed"
	view.NavCollapsed = true
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Global workflow history", "Avery Patel", "DES2 G6 → DES3 G7", "Completed",
		"Effective · 1 Aug 2026", "Closed · 4 Aug 2026 · 14:32 UTC",
		`href="/workspace/app/journeys?journey=intent-2"`, "Open record",
		`name="history_q"`, `name="history_person"`, `name="outcome"`, `name="history_year"`, `name="nav"`,
		"Employee", "Change", "Closed ↓", "Outcome", `aria-sort="descending"`,
		`href="/workspace/app/history?history_q=Avery&amp;nav=expanded&amp;outcome=completed"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("workflow history missing %q", want)
		}
	}
	if strings.Contains(doc, "Jordan Lee") {
		t.Fatal("active work appeared in past workflow history")
	}
}

func TestPerPersonHistoryKeepsScopedRouteAndFullControls(t *testing.T) {
	view := testView(PagePerson)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Past workflows", "Completed, rejected, and failed workflows recorded for Avery Patel.",
		`class="history-filter"`, `action="/workspace/app/person"`, `name="person"`, `value="worker-avery"`,
		`name="history_year"`, `name="history_sort"`, "Workflow", "Closed ↓", "Search workflow, change, or outcome",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("person history missing %q", want)
		}
	}
	if strings.Contains(doc, "All employees") {
		t.Fatal("person-scoped history rendered a cross-employee filter")
	}
	if strings.Contains(doc, "Authoritative · v9") {
		t.Fatal("person-scoped history exposed a storage version outside evidence details")
	}
}

func TestHistoryFiltersAndSortsAuthorizedTerminalRecords(t *testing.T) {
	view := testView(PageHistory)
	view.Work = append(view.Work,
		WorkItem{ID: "intent-3", Person: "Jordan Lee", PersonRef: "worker-jordan", Summary: "ENG3 G7 → ENG4 G8", Status: "Failed", Terminal: true, EffectiveDate: "2025-02-10", CompletedAt: "2 Feb 2025 · 10:00 UTC"},
		WorkItem{ID: "intent-4", Person: "Elena Ruiz", PersonRef: "worker-elena", Summary: "VP1 G9 → VP2 G10", Status: "Completed", Terminal: true, EffectiveDate: "2026-09-01", CompletedAt: "5 Sep 2026 · 09:00 UTC"},
	)
	view.HistoryYear = "2026"
	view.HistoryOutcome = "completed"
	view.HistorySort = historySortPerson
	view.HistoryDirection = "asc"
	items := filteredHistory(view, "")
	if len(items) != 2 || items[0].Person != "Avery Patel" || items[1].Person != "Elena Ruiz" {
		t.Fatalf("filtered ascending history = %+v", items)
	}
	view.HistoryPerson = "worker-elena"
	items = filteredHistory(view, "")
	if len(items) != 1 || items[0].ID != "intent-4" {
		t.Fatalf("person-filtered history = %+v", items)
	}
	view.HistoryPerson = ""
	view.HistorySort = historySortClosed
	view.HistoryDirection = "desc"
	items = filteredHistory(view, "")
	if len(items) != 2 || items[0].ID != "intent-4" {
		t.Fatalf("newest-first history = %+v", items)
	}
}

func TestPeopleQueryFiltersBeforeRendering(t *testing.T) {
	view := testView(PagePeople)
	view.Query = "Product"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Avery Patel") || !strings.Contains(doc, "Elena Ruiz") || strings.Contains(doc, "Jordan Lee") {
		t.Fatal("people query did not bound the rendered population")
	}
}

func TestPeopleSearchHasTruthfulEmptyStateAndRowsNavigateToProfiles(t *testing.T) {
	view := testView(PagePeople)
	view.Query = "no such authorized person"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "No people found") {
		t.Fatal("empty search did not render its truthful state")
	}

	view = testView(PagePeople)
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `href="/workspace/app/person?person=worker-avery"`) || strings.Contains(doc, `aria-label="Selected person"`) {
		t.Fatal("people directory did not link each row to a dedicated person page")
	}
}

func TestPeopleRowsOfferEmployeeScopedWorkflowMenus(t *testing.T) {
	view := testView(PagePeople)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="/workspace/app/journeys?journey=intent-1"`,
		`href="/workspace/app/journeys?mode=new&amp;worker=worker-avery"`,
		`aria-label="Choose a workflow for Avery Patel · NW-40118"`,
		`aria-label="Start Promotion for Avery Patel · NW-40118"`,
		`aria-label="Open the active promotion for Jordan Lee"`,
		`class="button secondary people-row-action"`,
		`class="popover-surface people-workflow-options"`,
		`data-hcm-transient-popover="people-workflows"`,
		`class="people-workflow-options-list"`,
		">Actions</th>",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("people workflow menu missing %q", want)
		}
	}
	if strings.Contains(doc, `class="people-row" href=`) {
		t.Fatal("people row must not nest the profile and workflow links")
	}
}

func TestPersonPageShowsServerFactsAndFilterableWorkflowLaunchers(t *testing.T) {
	view := testView(PagePerson)
	view.SelectedPerson = "worker-avery"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Avery Patel", "NW-40118", "CAD 118,000", "Start a workflow", `name="workflow_q"`,
		`href="/workspace/app/journeys?mode=new&amp;worker=worker-avery"`, "Start Promotion", "Start Internal transfer",
		`href="/workspace/app/people"`, "Past workflows", `href="/workspace/app/journeys?journey=intent-2"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("person page missing %q", want)
		}
	}
	if primaryNavAriaCurrentCount(doc) != 1 {
		t.Fatal("person page must keep exactly its People navigation parent active")
	}
	if !strings.Contains(doc, `aria-current="page">Avery Patel · NW-40118</span>`) {
		t.Fatal("person breadcrumb must name its worker beside the navigation marker")
	}

	view.WorkflowQuery = "promotion"
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Start Promotion") || strings.Contains(doc, "Start Internal transfer") {
		t.Fatal("workflow query did not filter the available workflow inventory")
	}
	if !strings.Contains(doc, `href="/workspace/app/person?nav=collapsed&amp;person=worker-avery&amp;workflow_q=promotion"`) {
		t.Fatal("collapsing on a person page discarded person or workflow state")
	}
	view.NavCollapsed = true
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `href="/workspace/app/person?nav=expanded&amp;person=worker-avery&amp;workflow_q=promotion"`) {
		t.Fatal("expanding on a person page discarded person or workflow state")
	}
}

// primaryNavAriaCurrentCount scopes the single-active-leaf contract to the
// primary navigation landmark. Breadcrumb trails carry their own
// aria-current marker by design, so a document-wide count can no longer
// express "exactly one navigation leaf is current".
func primaryNavAriaCurrentCount(document string) int {
	_, body, found := strings.Cut(document, "</style>")
	if !found {
		body = document
	}
	marker := strings.Index(body, `class="primary-nav"`)
	if marker < 0 {
		return 0
	}
	start := strings.LastIndex(body[:marker], "<nav")
	if start < 0 {
		return 0
	}
	rest := body[start:]
	end := strings.Index(rest, "</nav>")
	if end < 0 {
		return 0
	}
	return strings.Count(rest[:end], `aria-current="page"`)
}

func TestPersonPageDoesNotFallBackToAnotherWorker(t *testing.T) {
	view := testView(PagePerson)
	view.SelectedPerson = "not-visible"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Person not available") || strings.Contains(doc, "Jordan Lee") || strings.Contains(doc, "Avery Patel") {
		t.Fatal("missing person route fell back to an unrelated worker")
	}
}

func TestPeopleColumnHeadersAreIndependentGridItems(t *testing.T) {
	doc, err := Render(testView(PagePeople))
	if err != nil {
		t.Fatal(err)
	}
	for _, heading := range []string{"Person ↑", "Role", "Team", "Manager", "Location"} {
		if !strings.Contains(doc, ">"+heading+"</a>") {
			t.Fatalf("directory column %q is not an independent grid item", heading)
		}
	}
	for _, href := range []string{`href="/workspace/app/people?dir=desc"`, `href="/workspace/app/people?sort=role"`, `href="/workspace/app/people?sort=team"`} {
		if !strings.Contains(doc, href) {
			t.Fatalf("directory sort control missing %q", href)
		}
	}
}

func TestPeopleDirectoryCombinesFacetsSortAndPagination(t *testing.T) {
	view := testView(PagePeople)
	view.People = []Person{
		{ID: "worker-z", Name: "Zara", Role: "Engineer", Team: "Platform", Location: "Boston", PromotionAvailability: PromotionEligible},
		{ID: "worker-a", Name: "Avery", Role: "Designer", Team: "Product", Location: "Boston", PromotionAvailability: PromotionEligible},
		{ID: "worker-m", Name: "Mateo", Role: "Engineer", Team: "Platform", Location: "Denver", PromotionAvailability: PromotionEligible},
		{ID: "worker-b", Name: "Bianca", Role: "Engineer", Team: "Platform", Location: "Boston", PromotionAvailability: PromotionEligible},
	}
	view.Query = "engineer"
	view.PeopleTeam = "Platform"
	view.PeopleLocation = "Boston"
	view.PeopleSort = peopleSortName
	view.PeopleDirection = peopleSortDescending
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"2 of 4 people", ">Zara</strong>", ">Bianca</strong>", `name="team"`, `value="Platform"`,
		`name="location"`, `value="Boston"`, `>Person ↓</a>`,
		`href="/workspace/app/people?location=Boston&amp;q=engineer&amp;sort=role&amp;team=Platform"`,
		`href="/workspace/app/people?dir=desc&amp;eligible=&amp;location=&amp;q=&amp;team="`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("faceted sorted directory missing %q", want)
		}
	}
	if strings.Contains(doc, ">Avery</strong>") || strings.Contains(doc, ">Mateo</strong>") || strings.Index(doc, ">Zara</strong>") > strings.Index(doc, ">Bianca</strong>") {
		t.Fatal("directory did not apply exact facets before descending sort")
	}
}

func TestPeopleSortAndFacetControlsUseSharedResponsiveStyles(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`.people-filter-control{align-items:center;grid-template-columns:minmax(175px,1.25fr) minmax(132px,0.85fr) minmax(170px,1.1fr)`,
		`.people-filter select{background-color:var(--surface);border:1px solid var(--control-border);`,
		`.people-sort{align-items:center;color:var(--muted);display:flex;font:inherit;min-height:44px;`,
		`@media (max-width:760px){.people-filter-control{grid-template-columns:1fr;}`,
		`.people-directory .people-columns{align-items:center;display:flex;gap:8px;overflow-x:auto;padding-bottom:8px;padding-left:12px;padding-right:12px;padding-top:8px;}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("people sort/filter styling missing %q", want)
		}
	}
}

func TestPeopleDirectoryPaginatesFilteredLiveProjection(t *testing.T) {
	view := testView(PagePeople)
	view.People = make([]Person, 45)
	for index := range view.People {
		number := index + 1
		view.People[index] = Person{
			ID: fmt.Sprintf("worker-%02d", number), Initials: "P", Name: fmt.Sprintf("Person %02d", number),
			Role: "Engineer", Team: "Platform", Location: "Remote", WorkerNumber: fmt.Sprintf("NW-%02d", number),
			PromotionAvailability: PromotionEligible,
		}
	}
	view.Query = "Engineer"
	view.PeoplePage = 2
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"45 of 45 people", "21–40 of 45", "Page 2 of 3", "Person 21", "Person 40",
		`href="/workspace/app/people?page=3&amp;q=Engineer"`,
		`href="/workspace/app/person?page=2&amp;person=worker-21&amp;q=Engineer"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("paginated directory missing %q", want)
		}
	}
	if strings.Contains(doc, "Person 20") || strings.Contains(doc, "Person 41") {
		t.Fatal("paginated directory rendered a worker outside the requested window")
	}
}

func TestPeopleFilterSearchesWorkerFactsAndKeepsControlsOnEmpty(t *testing.T) {
	view := testView(PagePeople)
	view.People[1].WorkerNumber = "NW-40118"
	view.Query = "40118"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Avery Patel") || strings.Contains(doc, "Jordan Lee") || !strings.Contains(doc, `aria-label="Filter employees"`) {
		t.Fatal("directory filter did not search worker facts")
	}

	view.Query = "no-match"
	doc, err = Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "No people found") || !strings.Contains(doc, `value="no-match"`) || !strings.Contains(doc, "Clear filter") {
		t.Fatal("empty directory result removed the filter or recovery action")
	}
}

func TestPersonBackLinkPreservesDirectoryFilterAndPage(t *testing.T) {
	view := testView(PagePerson)
	view.Query = "Product"
	view.PeoplePage = 2
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `href="/workspace/app/people?page=2&amp;q=Product"`) {
		t.Fatal("person page did not retain its directory return context")
	}
}

func TestResponsiveFocusAndPrintContractsArePlatformOwned(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{"@media (max-width:760px)", "@media (prefers-reduced-motion:reduce)", "@media (forced-colors:active)", "@media print", ":focus-visible", ".skip-link:focus"} {
		if !strings.Contains(css, want) {
			t.Fatalf("stylesheet missing %q", want)
		}
	}
	if !strings.Contains(css, "grid-template-columns:210px minmax(0,1fr)") {
		t.Fatal("intermediate desktop shell lost its content column")
	}
	if !strings.Contains(css, ".people-workspace{grid-template-columns:1fr;}") {
		t.Fatal("intermediate desktop directory does not protect readable columns")
	}
	for _, contract := range []string{
		".topbar{grid-template-columns:210px minmax(180px,1fr) auto auto;}",
		".home-grid,.workbench,.people-workspace,.settings-shell{grid-template-columns:1fr;}",
		".studio-shell{grid-template-columns:1fr;}",
		".primary-nav>ul{display:grid!important;max-width:100%!important;width:100%!important;}",
		".sidebar nav:first-of-type{max-width:100%;min-width:0;overflow-x:auto;overscroll-behavior-inline:contain;width:100%;}",
		".sidebar nav:first-of-type>ul{max-width:none;width:max-content;}",
		".app-shell,.shell-grid,.sidebar,.main{max-width:100%;min-width:0;}",
	} {
		if !strings.Contains(css, contract) {
			t.Fatalf("responsive contract missing %q", contract)
		}
	}
}
