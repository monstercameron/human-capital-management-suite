package productui

import (
	"bytes"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

var unpublishedAdminPages = []PageID{
	PageStudio, PagePolicyStudio, PagePolicySimulation, PageConfigurationCenter,
	PageIntegrationOperations, PageReconciliationWorkbench, PagePrivacyTelemetry,
	PagePerformanceBudgets, PageBrowserMatrix, PageAssistiveTech,
	PageDisasterRecovery, PageReleaseGate,
}

func TestTodo_UXAUDIT_005(t *testing.T) {
	items := navigationForRoles(ResolveProductLocale("en-US"), []string{RoleHCMAdmin})
	wantPrimary := []PageID{PageHome, PageMyself, PageJourneys, PageWorkflowDesigner, PageWork, PagePeople, PageOrganization, PageInsights, PageAdmin}
	if len(items) != len(wantPrimary) {
		t.Fatalf("primary destinations = %d, want %d: %+v", len(items), len(wantPrimary), items)
	}
	for index, want := range wantPrimary {
		if items[index].Page != want {
			t.Fatalf("primary destination %d = %s, want %s", index, items[index].Page, want)
		}
	}
	admin := items[len(items)-1]
	wantAdmin := []PageID{PageAdmin, PageWorkerIDs, PageRoles, PageOrganizationVisibility, PageAppearance}
	if len(admin.Children) != len(wantAdmin) {
		t.Fatalf("Admin children = %+v, want published configuration only", admin.Children)
	}
	for index, want := range wantAdmin {
		if admin.Children[index].Page != want {
			t.Fatalf("Admin child %d = %s, want %s", index, admin.Children[index].Page, want)
		}
	}
	for _, page := range unpublishedAdminPages {
		assertRegisteredFallbackAbsentFromNavigation(t, page)
	}
}

func TestTodo_UXAUDIT_005_Golden(t *testing.T) {
	items := navigationForRoles(ResolveProductLocale("en-US"), []string{RoleHCMAdmin})
	var actual strings.Builder
	for _, item := range items {
		actual.WriteString(string(item.Page) + ":" + item.Label + "\n")
		for _, child := range item.Children {
			actual.WriteString("  " + string(child.Page) + ":" + child.Label + "\n")
		}
	}
	const want = "home:Home\nmyself:Myself\njourneys:Journeys\nworkflow-designer:Workflow editor\nwork:My Work\n  work:Work queue\n  history:Work History\npeople:People\norganization:Organization\ninsights:Insights\nadmin:Admin\n  admin:Admin overview\n  worker-ids:Worker IDs\n  roles:Roles & access\n  organization-visibility:Organization visibility\n  appearance:Brand & appearance\n"
	if actual.String() != want {
		t.Fatalf("published navigation changed:\n%s", actual.String())
	}
}

func TestTodo_UXAUDIT_005_Browser(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageAdmin), []string{RoleHCMAdmin})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	navigation := findElementByID(root, "workspace-navigation")
	if navigation == nil {
		t.Fatal("rendered shell has no navigation")
	}
	var markup bytes.Buffer
	if err := xhtml.Render(&markup, navigation); err != nil {
		t.Fatal(err)
	}
	for _, page := range []PageID{PageWorkflowDesigner, PageWorkerIDs, PageRoles, PageOrganizationVisibility, PageAppearance} {
		if !strings.Contains(markup.String(), pageHref(page)) {
			t.Fatalf("published Admin destination %s is missing from rendered navigation", page)
		}
	}
	for _, page := range unpublishedAdminPages {
		if strings.Contains(markup.String(), pageHref(page)) {
			t.Fatalf("unpublished Admin destination %s escaped into rendered navigation", page)
		}
		for _, result := range globalSearchItems(view) {
			if result.ID == "page:"+string(page) {
				t.Fatalf("unpublished Admin destination %s escaped into global search", page)
			}
		}
	}
	for _, section := range utilityDrawerSections(ApplyRoleVisibility(testView(PageRoles), []string{RoleHCMAdmin})) {
		for _, item := range section.Items {
			for _, page := range unpublishedAdminPages {
				if item.Page == page {
					t.Fatalf("unpublished Admin destination %s escaped into the utility drawer", page)
				}
			}
		}
	}
	home, err := Render(ApplyRoleVisibility(testView(PageHome), []string{RoleHCMAdmin}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Needs your attention", "Current activity", "Your assigned work and records visible to you.", "Start a request"} {
		if !strings.Contains(home, want) {
			t.Fatalf("Home lacks durable heading or honest activity scope %q", want)
		}
	}
	for _, stale := range []string{"Open promotion work", ">Promotion journeys</h", "Start something"} {
		if strings.Contains(home, stale) {
			t.Fatalf("Home still promotes one workflow as its whole information architecture: %q", stale)
		}
	}
}

func TestTodo_UXAUDIT_005_Security(t *testing.T) {
	permissions := make([]RolePagePermission, 0, len(PageDefinitions()))
	for _, definition := range PageDefinitions() {
		permissions = append(permissions, RolePagePermission{Page: definition.ID, View: true})
	}
	items := navigationForPermissions(ResolveProductLocale("en-US"), permissions)
	for _, page := range unpublishedAdminPages {
		if navigationContains(items, page) {
			t.Fatalf("view permission alone published unavailable destination %s", page)
		}
	}
	admin := authorizedNavigationTestItem(PageAdmin)
	admin.Children = []AuthorizedNavigationItem{authorizedNavigationTestItem(PageStudio)}
	view := ApplyNavigationProjection(testView(PageHome), AuthorizedNavigationProjection{Version: 3, Items: []AuthorizedNavigationItem{admin}})
	if len(view.Navigation) != 0 || len(view.NavigationSupport) != 0 {
		t.Fatal("a forged projection reintroduced an unpublished destination")
	}
}

func TestTodo_UXAUDIT_005_Conformance(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		resolved := ResolveProductLocale(locale)
		items := navigationForRoles(resolved, []string{RoleHCMAdmin})
		for _, item := range items {
			assertPublishedNavigationItem(t, item)
			for _, child := range item.Children {
				assertPublishedNavigationItem(t, child)
			}
		}
		for _, key := range []string{"home.attention_title", "home.activity_title", "home.activity_scope", "home.start_title"} {
			result, err := resolved.Resolve(key)
			if err != nil || result.Text == "" || result.Locale != resolved.Resolved || result.FallbackPath != resolved.Resolved {
				t.Fatalf("%s has no direct localized %s label: %+v, %v", locale, key, result, err)
			}
		}
	}
	for _, page := range unpublishedAdminPages {
		definition, ok := LookupPage(page)
		if !ok || definition.NavigationPublished {
			t.Fatalf("%s must remain a registered, unpublished fallback", page)
		}
		resolved, ok := LookupRoute(definition.Route)
		if !ok || resolved.ID != page {
			t.Fatalf("%s lost its direct fallback route", page)
		}
		doc, err := Render(testView(page))
		if err != nil {
			t.Fatalf("fallback %s: %v", page, err)
		}
		if !strings.Contains(strings.ToLower(doc), "not published") && !strings.Contains(strings.ToLower(doc), "not enabled") && !strings.Contains(strings.ToLower(doc), "cannot be edited here yet") {
			t.Fatalf("fallback %s does not explain its unavailable state", page)
		}
	}
}

func assertRegisteredFallbackAbsentFromNavigation(t *testing.T, page PageID) {
	t.Helper()
	definition, ok := LookupPage(page)
	if !ok || definition.NavigationPublished || definition.ParentNav != PageAdmin {
		t.Fatalf("%s is not a registered unpublished Admin fallback: %+v", page, definition)
	}
	if resolved, ok := LookupRoute(definition.Route); !ok || resolved.ID != page {
		t.Fatalf("%s direct fallback route does not round-trip", page)
	}
	if navigationContains(navigationForRoles(ResolveProductLocale("en-US"), []string{RoleHCMAdmin}), page) {
		t.Fatalf("unpublished fallback %s appears in live Admin navigation", page)
	}
}

func navigationContains(items []NavItem, page PageID) bool {
	for _, item := range items {
		if item.Page == page || navigationContains(item.Children, page) {
			return true
		}
	}
	return false
}

func assertPublishedNavigationItem(t *testing.T, item NavItem) {
	t.Helper()
	definition, ok := LookupPage(item.Page)
	if !ok || !definition.NavigationPublished || !PageVisible(item.Page, []string{RoleHCMAdmin}) {
		t.Fatalf("navigation destination %s lacks publication or authorization", item.Page)
	}
	if item.Label == "" || item.Description == "" {
		t.Fatalf("navigation destination %s lacks localized identity", item.Page)
	}
}
