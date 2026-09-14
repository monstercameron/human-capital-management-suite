package productui

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

func TestTodo_UXAUDIT_011_MergedUX(t *testing.T) {
	assertRegisteredFallbackAbsentFromNavigation(t, PageStudio)
	for _, role := range []string{RoleHCMAdmin, "hr_partner", "manager", "worker_self"} {
		if navigationContains(navigationForRoles(ResolveProductLocale("en-US"), []string{role}), PageStudio) {
			t.Fatalf("Experience Studio was published for %s without an authoring capability", role)
		}
	}
}

// The direct fallback remains routable, but neither the browser shell nor its
// adjacent discovery surfaces offer Studio as a working destination.
func TestTodo_UXAUDIT_011_Browser_MergedUX(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageStudio), []string{RoleHCMAdmin})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Custom pages cannot be edited here yet") ||
		!strings.Contains(doc, "Not available") {
		t.Fatal("direct Studio route does not explain its unavailable state")
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	navigation := findElementByID(root, "workspace-navigation")
	if navigation == nil {
		t.Fatal("rendered shell has no navigation")
	}
	if linkForRoute(navigation, pageHref(PageStudio)) != nil {
		t.Fatal("unavailable Studio appeared in shell navigation")
	}
	for _, item := range globalSearchItems(view) {
		if item.ID == "page:"+string(PageStudio) {
			t.Fatal("unavailable Studio appeared in global search")
		}
	}
	for _, section := range utilityDrawerSections(view) {
		for _, item := range section.Items {
			if item.Page == PageStudio {
				t.Fatal("unavailable Studio appeared in page utilities")
			}
		}
	}
}

func TestTodo_UXAUDIT_011_Security_MergedUX(t *testing.T) {
	items := navigationForPermissions(ResolveProductLocale("en-US"), []RolePagePermission{
		{Page: PageAdmin, View: true}, {Page: PageStudio, View: true},
	})
	if navigationContains(items, PageStudio) {
		t.Fatal("view permission alone published an unavailable authoring route")
	}
	admin := authorizedNavigationTestItem(PageAdmin)
	admin.Children = []AuthorizedNavigationItem{authorizedNavigationTestItem(PageStudio)}
	view := ApplyNavigationProjection(testView(PageAdmin), AuthorizedNavigationProjection{Version: 3, Items: []AuthorizedNavigationItem{admin}})
	if len(view.Navigation) != 0 || len(view.NavigationSupport) != 0 {
		t.Fatal("forged navigation projection reintroduced Studio")
	}
}

func TestTodo_UXAUDIT_011_Conformance_MergedUX(t *testing.T) {
	definition, ok := LookupRoute("/workspace/app/studio")
	if !ok || definition.ID != PageStudio || definition.NavigationPublished {
		t.Fatalf("Studio route and publication disagree: %+v", definition)
	}
	for _, code := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(ApplyRoleVisibility(testView(PageStudio), []string{RoleHCMAdmin}), ResolveProductLocale(code))
		markup, err := ui.RenderToString(BuildPageContent(view))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{view.Locale.Text("studio.unavailable_title"), view.Locale.Text("studio.unavailable_description")} {
			if want == "" || !strings.Contains(markup, html.EscapeString(want)) {
				t.Fatalf("%s Studio fallback lacks localized limitation %q", code, want)
			}
		}
		if strings.Contains(markup, "<form") || strings.Contains(markup, `type="submit"`) {
			t.Fatalf("%s Studio fallback exposes an authoring form", code)
		}
	}
}
