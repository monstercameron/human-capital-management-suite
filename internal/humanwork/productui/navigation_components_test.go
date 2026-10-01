package productui

import (
	"strings"
	"testing"
)

// UXBLIND-102: the My Work overview child is named for what it lists, not for its group.
func TestNavigationRegistryBuildsReusableSubmenus(t *testing.T) {
	view := testView(PageWorkflowHistory)
	_, items := projectNavigation(view)
	work, ok := projectedNavigationItem(items, PageWork)
	if !ok {
		t.Fatal("My Work navigation group is missing")
	}
	if work.Active || len(work.Children) != 1 || work.Children[0].Page != PageWork || work.Children[0].Label != view.Locale.Text("home.needs_action") {
		t.Fatalf("My Work submenu = %+v", work)
	}
	workflowStart, ok := projectedNavigationItem(items, PageWorkflowStart)
	if !ok || !workflowStart.Active || len(workflowStart.Children) != 2 || workflowStart.Children[0].Page != PageWorkflowStart || workflowStart.Children[1].Page != PageWorkflowHistory {
		t.Fatalf("Workflows submenu = %+v", workflowStart)
	}
	// UXAUDIT-011: Studio and the ten other unbuilt admin fallback surfaces
	// (Policy Studio, Policy simulation, Configuration center, Integration
	// operations, Reconciliation workbench, Privacy telemetry, Performance
	// budgets, Browser matrix, Assistive tech, Disaster recovery, Release
	// gate) keep their ParentNav wiring to Admin but declare no admitted
	// capability. The published Chat settings and Personas pages follow the
	// overview and other admitted configuration destinations.
	admin, ok := projectedNavigationItem(items, PageAdmin)
	wantAdminChildren := []PageID{PageAdmin, PageWorkerIDs, PageRoles, PageOrganizationVisibility, PageAppearance, PageChatSettings, PagePersonaAdmin, PageClockDevices}
	if !ok || len(admin.Children) != len(wantAdminChildren) {
		t.Fatalf("Admin submenu = %+v, present=%t", admin, ok)
	}
	for index, want := range wantAdminChildren {
		if admin.Children[index].Page != want {
			t.Fatalf("Admin child %d = %s, want %s", index, admin.Children[index].Page, want)
		}
	}
	for _, unadmitted := range []PageID{PageStudio, PagePolicyStudio, PagePolicySimulation, PageConfigurationCenter, PageIntegrationOperations, PageReconciliationWorkbench, PagePrivacyTelemetry, PagePerformanceBudgets, PageBrowserMatrix, PageAssistiveTech, PageDisasterRecovery, PageReleaseGate} {
		for _, child := range admin.Children {
			if child.Page == unadmitted {
				t.Fatalf("Admin submenu still carries unadmitted page %s", unadmitted)
			}
		}
	}
}

func TestMenuFilterKeepsOnlyMatchingHierarchy(t *testing.T) {
	view := ApplyRequest(testView(PageHome), PageRequest{MenuQuery: "history"})
	favorites, items := projectNavigation(view)
	if len(favorites) != 0 || len(items) != 1 {
		t.Fatalf("filtered navigation favorites=%+v items=%+v", favorites, items)
	}
	workflows := items[0]
	if workflows.Page != PageWorkflowStart || !workflows.Expanded || len(workflows.Children) != 1 || workflows.Children[0].Page != PageWorkflowHistory {
		t.Fatalf("history filter did not retain its open parent: %+v", workflows)
	}

	view.MenuQuery = "does not exist"
	favorites, items = projectNavigation(view)
	if len(favorites) != 0 || len(items) != 0 {
		t.Fatalf("empty menu filter invented results: favorites=%+v items=%+v", favorites, items)
	}
}

func TestUXBLIND025MenuFilterPreservesSupportNavigation(t *testing.T) {
	view := ApplyRequest(testView(PageSettings), PageRequest{MenuQuery: "pe"})
	favorites, items := projectNavigation(view)
	// The short prefix ranks People first and also reaches the admitted
	// Personas child under Admin. Both are real destinations, not fallback
	// aliases.
	if len(favorites) != 0 || len(items) != 2 || items[0].Page != PagePeople || items[1].Page != PageAdmin || len(items[1].Children) != 1 || items[1].Children[0].Page != PagePersonaAdmin {
		t.Fatalf("short prefix should resolve People: favorites=%+v items=%+v", favorites, items)
	}
	props := navigationSidebarProps(view)
	if len(props.Support) != 2 || props.Support[0].Page != PageHelp || props.Support[1].Page != PageSettings {
		t.Fatalf("support escape routes were not preserved during search: %+v", props.Support)
	}
	if items[0].MatchDetail == "" || items[0].MatchScore == 0 {
		t.Fatalf("matching metadata was not projected: %+v", items[0])
	}
}

func TestMenuFilterFindsAliasesTyposAndMultipleTerms(t *testing.T) {
	for _, test := range []struct {
		query string
		page  PageID
	}{
		{query: "employee", page: PagePeople},
		{query: "poeple", page: PagePeople},
		{query: "ppl", page: PagePeople},
		{query: "dark mode", page: PageAdmin},
		{query: "past workflow", page: PageWorkflowStart},
		{query: "org chart", page: PageOrganization},
	} {
		t.Run(test.query, func(t *testing.T) {
			view := ApplyRequest(testView(PageHome), PageRequest{MenuQuery: test.query})
			_, items := projectNavigation(view)
			if len(items) == 0 || items[0].Page != test.page {
				t.Fatalf("fuzzy query %q ranked %+v, want %s first", test.query, items, test.page)
			}
		})
	}
}

func TestSupportMenusRemainReachableDuringFuzzySearch(t *testing.T) {
	view := ApplyRequest(testView(PageHome), PageRequest{MenuQuery: "language"})
	props := navigationSidebarProps(view)
	if len(props.Items) != 0 || len(props.Support) != 2 {
		t.Fatalf("language query = items %+v support %+v, want stable support region", props.Items, props.Support)
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	inputStart := strings.Index(doc, `id="menu-filter"`)
	if inputStart < 0 {
		t.Fatal("missing menu filter")
	}
	inputEnd := strings.Index(doc[inputStart:], ">")
	if inputEnd < 0 || !strings.Contains(doc[inputStart:inputStart+inputEnd], `value="language"`) {
		t.Fatal("server-rendered input does not carry the active filter query")
	}
	if !strings.Contains(doc, ">Help</span>") || !strings.Contains(doc, "Manage your language, accessibility and personal preferences.") {
		t.Fatal("filtered support result did not render current localized metadata")
	}
	settings := strings.Index(doc, ">Settings</span>")
	bottom := strings.Index(doc, `class="nav-bottom"`)
	if settings < 0 || bottom < 0 || settings > bottom || strings.Count(doc, ">Settings</span>") != 1 {
		t.Fatal("matching Settings must appear once above the support recovery area")
	}
}

func TestUnmatchedMenuSearchExplainsEmptyResultsWithRecoveryLinks(t *testing.T) {
	view := ApplyRequest(testView(PageHome), PageRequest{MenuQuery: "zzzznotfound"})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `class="nav-empty"`) || !strings.Contains(doc, ">Help</span>") || !strings.Contains(doc, ">Settings</span>") {
		t.Fatal("unmatched search must explain empty results and retain recovery links")
	}
}

func TestUXBLIND025UnmatchedMenuFilterPreservesSupportNavigation(t *testing.T) {
	props := navigationSidebarProps(ApplyRequest(testView(PageHome), PageRequest{MenuQuery: "zzzznotfound"}))
	if len(props.Items) != 0 || len(props.Support) != 2 {
		t.Fatalf("unmatched query removed support recovery routes: items=%+v support=%+v", props.Items, props.Support)
	}
}

func TestEveryRegisteredPageCarriesNavigationSearchMetadata(t *testing.T) {
	for _, definition := range PageDefinitions() {
		if definition.SubtitleKey == "" || len(definition.SearchTerms) == 0 {
			t.Errorf("page %s has incomplete navigation search metadata: %+v", definition.ID, definition)
		}
	}
}

func TestFavoriteOnlySearchDoesNotRenderAnEmptyAllNavigationSection(t *testing.T) {
	view := ApplyRequest(testView(PageHome), PageRequest{MenuQuery: "staff", FavoritePages: []PageID{PagePeople}})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	// UXBLIND-066: a favorited page stays in its place in the main list, so
	// the ordinary section is never empty for a favorite-only match.
	_, items := projectNavigation(view)
	if _, ok := projectedNavigationItem(items, PagePeople); !ok {
		t.Fatalf("favorited People left the ordinary navigation: %+v", items)
	}
	if !strings.Contains(doc, ">Favorites</li>") || !strings.Contains(doc, ">All navigation</li>") {
		t.Fatal("favorite fuzzy result lost its Favorites or its in-place navigation section")
	}
}

func TestMenuFilterUsesDebouncedSoftwareNavigationAndImmediateSubmit(t *testing.T) {
	view := testView(PageSettings)
	var scheduled, immediate string
	cancelled := 0
	view.Navigate = func(href string) { immediate = href }
	view.NavigateDebounced = func(href string) { scheduled = href }
	view.CancelDebouncedNavigation = func() { cancelled++ }
	filter := navigationSidebarProps(view).Filter
	if filter.OnInput == nil || filter.OnFilter == nil {
		t.Fatal("live menu filter has no input or submit enhancement")
	}

	filter.OnInput(" work ")
	if scheduled != "/workspace/app/settings?menu_q=work" || immediate != "" || cancelled != 0 {
		t.Fatalf("debounced filter scheduled=%q immediate=%q cancelled=%d", scheduled, immediate, cancelled)
	}
	filter.OnFilter("people")
	if immediate != "/workspace/app/settings?menu_q=people" || cancelled != 1 {
		t.Fatalf("submitted filter immediate=%q cancelled=%d", immediate, cancelled)
	}

	filter.OnInput("")
	if scheduled != "/workspace/app/settings" {
		t.Fatalf("clearing a stale rendered query must schedule the cleared URL: %q", scheduled)
	}
}

func TestUXBLIND024ClearMenuFilterResetsStateAndCancelsPendingNavigation(t *testing.T) {
	view := testView(PageHome)
	view.MenuQuery = "zzzznotfound"
	var navigated string
	cancelled := 0
	view.Navigate = func(href string) { navigated = href }
	view.NavigateDebounced = func(string) {}
	view.CancelDebouncedNavigation = func() { cancelled++ }

	filter := navigationSidebarProps(view).Filter
	if filter.Query != "zzzznotfound" || filter.OnClear == nil {
		t.Fatalf("clearable filter = %+v", filter)
	}
	filter.OnInput("new query")
	clearNavigate := menuFilterClearNavigate(filter)
	if clearNavigate == nil {
		t.Fatal("clear link lost navigation capability")
	}
	clearNavigate(filter.ClearHref)
	if navigated != filter.ClearHref || cancelled != 1 {
		t.Fatalf("clear link navigation=%q cancelled=%d", navigated, cancelled)
	}

	reset := ApplyRequest(view, PageRequest{})
	reset.MenuQuery = ""
	resetProps := navigationSidebarProps(reset)
	if resetProps.Filter.Query != "" || len(resetProps.Items) == 0 {
		t.Fatalf("cleared route retained stale state: filter=%q items=%d", resetProps.Filter.Query, len(resetProps.Items))
	}
}

func TestFavoritesMoveLeavesToTheTopAndToggleWithoutLosingPageState(t *testing.T) {
	view := ApplyRequest(testView(PageWork), PageRequest{
		WorkFilter: "review", MenuQuery: "", FavoritePages: []PageID{PageWorkflowHistory, PagePeople, PageWorkflowHistory, "unknown"},
	})
	if got := view.FavoritePages; len(got) != 2 || got[0] != PageWorkflowHistory || got[1] != PagePeople {
		t.Fatalf("authorized favorites = %v", got)
	}
	favorites, items := projectNavigation(view)
	if len(favorites) != 2 || favorites[0].Page != PageWorkflowHistory || favorites[1].Page != PagePeople {
		t.Fatalf("favorite order = %+v", favorites)
	}
	workflows, ok := projectedNavigationItem(items, PageWorkflowStart)
	// UXBLIND-066: favorited pages stay in place in the main list.
	if !ok || len(workflows.Children) != 2 || workflows.Children[1].Page != PageWorkflowHistory {
		t.Fatalf("favorited history left its place in Workflows: %+v", workflows)
	}
	if _, ok := projectedNavigationItem(items, PagePeople); !ok {
		t.Fatal("favorited People left its place in all navigation")
	}
	href := favoriteToggleHref(view, PageOrganization)
	for _, want := range []string{"filter=review"} {
		if !strings.Contains(href, want) {
			t.Fatalf("favorite toggle lost state %q in %s", want, href)
		}
	}
	if strings.Contains(href, "favorites=") {
		t.Fatalf("favorite toggle leaked account state into %s", href)
	}
}

func TestRemovingTheLastFavoriteAndExpandingNavigationAreExplicit(t *testing.T) {
	view := ApplyRequest(testView(PagePeople), PageRequest{FavoritePages: []PageID{PagePeople}, NavCollapsed: true})
	if href := favoriteToggleHref(view, PagePeople); strings.Contains(href, "favorites=") {
		t.Fatalf("last-favorite removal leaked account state into the URL: %s", href)
	}
	if href := navigationToggleProps(view).Href; !strings.Contains(href, "nav=expanded") {
		t.Fatalf("expanded navigation was indistinguishable from a missing server preference: %s", href)
	}
}

func TestSidebarRendersAccessibleFilterFavoriteAndDisclosureControls(t *testing.T) {
	view := ApplyRequest(testView(PageWorkflowHistory), PageRequest{FavoritePages: []PageID{PagePeople}})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="menu-filter"`, `aria-label="Filter pages"`, `>Favorites</li>`,
		`aria-label="Remove People from favorites"`, `class="nav-group current"`, `open`,
		`data-hcm-nav-group="workflow-start"`, `>Workflow history</span>`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("sidebar missing %q", want)
		}
	}
	if strings.Index(doc, `>Favorites</li>`) > strings.Index(doc, `>All navigation</li>`) {
		t.Fatal("favorites are not rendered before the ordinary menu")
	}
}

func TestCollapsedSidebarOmitsFilterAndUsesGroupDestination(t *testing.T) {
	view := ApplyRequest(testView(PageWorkflowHistory), PageRequest{NavCollapsed: true, MenuQuery: "history"})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `id="menu-filter"`) {
		t.Fatal("collapsed navigation retained a hidden focusable menu filter")
	}
	if !strings.Contains(doc, `href="/workspace/app/workflows?menu_q=history&amp;nav=collapsed"`) {
		t.Fatal("collapsed Workflows group has no software-navigation destination")
	}
}

func TestFilteredNavigationForcesMatchingGroupsOpenWithoutChangingPreference(t *testing.T) {
	view := ApplyRequest(testView(PageHome), PageRequest{MenuQuery: "history"})
	view.NavigationGroupOpen = map[PageID]bool{PageWorkflowStart: false}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-hcm-nav-group="workflow-start"`, `data-hcm-nav-force-open="true"`, `open`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("filtered navigation group missing %q", want)
		}
	}
}

func TestSavedNavigationDisclosureOverridesContextualDefault(t *testing.T) {
	view := testView(PageWorkflowHistory)
	view.NavigationGroupOpen = map[PageID]bool{PageWorkflowStart: false, PageAdmin: true}
	_, items := projectNavigation(view)
	work, _ := projectedNavigationItem(items, PageWorkflowStart)
	admin, _ := projectedNavigationItem(items, PageAdmin)
	if work.Expanded {
		t.Fatal("saved closed state did not override the active-route default")
	}
	if !admin.Expanded {
		t.Fatal("saved open state did not override the inactive-route default")
	}
}

func TestNavigationToggleIsGlyphOnlyBesideBrandAndControlsSidebar(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="brand-cluster"`, `class="header-nav-toggle"`,
		`aria-label="Collapse navigation"`, `aria-expanded="true"`,
		`aria-controls="workspace-navigation"`, `id="workspace-navigation"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("header navigation control missing %q", want)
		}
	}
	start := strings.Index(doc, `class="header-nav-toggle"`)
	if strings.Contains(doc, `class="sidebar-toggle"`) || start < 0 {
		t.Fatal("navigation toggle is not a glyph-only header control")
	}
	end := strings.Index(doc[start:], `</a>`)
	if end < 0 {
		t.Fatal("navigation toggle has no closing link")
	}
	bodyStart := strings.Index(doc[start:start+end], `>`)
	if bodyStart < 0 {
		t.Fatal("navigation toggle is not a glyph-only header control")
	}
	toggleBody := doc[start+bodyStart+1 : start+end]
	if !strings.Contains(toggleBody, `<svg`) || strings.Contains(toggleBody, `<span`) {
		t.Fatalf("navigation toggle body is not glyph-only: %s", toggleBody)
	}
	if strings.Index(doc, `class="header-nav-toggle"`) > strings.Index(doc, `id="workspace-navigation"`) {
		t.Fatal("navigation toggle is not colocated with the brand before the sidebar")
	}
}

func projectedNavigationItem(items []NavigationItemProps, page PageID) (NavigationItemProps, bool) {
	for _, item := range items {
		if item.Page == page {
			return item, true
		}
	}
	return NavigationItemProps{}, false
}
