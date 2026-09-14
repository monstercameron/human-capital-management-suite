package productui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_009_Wave3(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{Version: 7, ID: "hrbp", Name: "HR business partner", Description: "Supports people decisions across the business.", Active: true}}
	view.RolePagePermissions = []RolePagePermission{{Version: 3, RoleID: "hrbp", Page: PagePeople, View: true, Update: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{Version: 9, RoleID: "hrbp", Mode: "ALLOWLIST", OrganizationUnits: []string{"Product", "People"}, DataDomains: []string{"people", "organization"}}}
	markup, err := ui.RenderToString(rolesPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"role-definition", "HR business partner", "Supports people decisions across the business.", "hrbp", view.Locale.Text("organization_visibility.mode_allow"), "Product, People", "people, organization", view.Locale.Text("organization_visibility.boundary_detail")} {
		if !strings.Contains(markup, want) {
			t.Errorf("role definition disclosure missing %q", want)
		}
	}
}

func TestTodo_UXAUDIT_009_Wave3ProjectionComposition(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "auditor", Name: "Auditor", Description: "Read-only review role.", Active: true}}
	view.RolePagePermissions = []RolePagePermission{{RoleID: "auditor", Page: PageHistory, View: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "auditor", Mode: "OWN_UNIT", OrganizationUnits: []string{"Finance"}}}
	markup, err := ui.RenderToString(rolesPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, view.Locale.Text("organization_visibility.mode_own")) || !strings.Contains(markup, "Finance") || !strings.Contains(markup, "History") {
		t.Fatal("server-projected role page, scope, and definition were not composed")
	}
}

func TestTodo_UXAUDIT_009_Wave3DisclosureMarkup(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "Manager", Description: "Full description retained.", Active: true}}
	markup, err := ui.RenderToString(rolesPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="role-definition"`) || !strings.Contains(markup, "<summary>") || strings.Contains(markup, `class="role-definition-disclosure"`) {
		t.Fatal("opening a role must reveal its definition without another nested toggle")
	}
}

func TestTodo_UXAUDIT_009_Wave3Accessibility(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "Manager", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "manager", Mode: "ALL"}}
	markup, err := ui.RenderToString(rolesPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<summary>", "<dl>", "<dt>Role ID</dt>", "<dd>", "manager"} {
		if !strings.Contains(markup, want) {
			t.Errorf("accessible role definition missing %q", want)
		}
	}
}

func TestTodo_UXAUDIT_009_Wave3LargeCatalogMarkup(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = make([]AccessRole, 250)
	for i := range view.AccessRoles {
		view.AccessRoles[i] = AccessRole{ID: "role-" + strconv.Itoa(i), Name: "Role", Description: "Description", Active: true}
	}
	markup, err := ui.RenderToString(rolesPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "role-0") || !strings.Contains(markup, "role-249") {
		t.Fatal("large role catalog did not render its projected role definitions")
	}
}

func TestTodo_UXAUDIT_009_Wave3Security(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "Manager", Active: true}}
	// A missing policy is an explicit unavailable projection, never an inferred
	// global scope. The page may disclose the role but must not fabricate one.
	markup, err := ui.RenderToString(rolesPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Everyone") || strings.Contains(markup, "global tenant") {
		t.Fatal("missing server scope must not be rendered as a global permission")
	}
}

func TestTodo_UXAUDIT_009_ProgressiveAccessSections(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "Manager", Active: true}}
	markup, err := ui.RenderToString(rolesPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if catalog, assignments := strings.Index(markup, `id="role-catalog"`), strings.Index(markup, `id="role-assignments"`); catalog < 0 || assignments < 0 || catalog >= assignments {
		t.Fatal("role creation and inspection must precede the long worker directory")
	}
	for _, want := range []string{`href="#role-catalog"`, `href="#role-assignments"`, `class="role-page-access"`, "<summary>Page and action access</summary>", "Page permissions and organization scope are enforced by the server."} {
		if !strings.Contains(markup, want) {
			t.Errorf("progressive role navigation missing %q", want)
		}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		if label := ResolveProductLocale(locale).Text("roles.sections"); label == "" || label == "roles.sections" {
			t.Errorf("roles section navigation lacks a localized name for %s", locale)
		}
	}
}

func TestTodo_UXAUDIT_009_PublishedPageGrouping(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "Manager", Active: true}}
	markup, err := ui.RenderToString(rolesPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-label="Published pages"`) || !strings.Contains(markup, `class="role-unpublished-pages"`) {
		t.Fatal("published and preview-only page grants need distinct scan groups")
	}
	if !strings.Contains(markup, view.Locale.Text("roles.unpublished_pages_help")) {
		t.Fatal("preview-only grants need an explicit availability warning")
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, key := range []string{"roles.published_pages", "roles.unpublished_pages", "roles.unpublished_pages_help", "roles.effective_boundary", "roles.inspect_role", "roles.scroll_hint"} {
			if label := ResolveProductLocale(locale).Text(key); label == "" || label == key {
				t.Errorf("%s missing %s", locale, key)
			}
		}
	}
}

func TestTodo_UXAUDIT_009_CompactRoleHierarchy(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "Manager", Active: true}}
	markup, err := ui.RenderToString(rolesPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, repeated := range []string{"AUTHORIZATION", "Roles and employee access"} {
		if strings.Contains(markup, repeated) {
			t.Errorf("page-level heading already provides context; redundant hero contains %q", repeated)
		}
	}
	for _, want := range []string{`class="roles-access-intro"`, `href="#role-catalog"`, `href="#role-assignments"`, "Inspect role", "Scroll this table horizontally"} {
		if !strings.Contains(markup, want) {
			t.Errorf("compact role hierarchy missing %q", want)
		}
	}
	css := Stylesheet()
	for _, want := range []string{"min(100%,380px)", "overflow-x:auto", ".role-page-scroll-hint"} {
		if !strings.Contains(css, want) {
			t.Errorf("role density/responsive stylesheet missing %q", want)
		}
	}
}
