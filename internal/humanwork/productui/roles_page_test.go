package productui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestUXBlindRoleAssignmentsNeverInventDefaults(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		i18n := I18nProps{Locale: ResolveProductLocale(locale)}
		markup, err := ui.RenderToString(RolesPage(RolesPageProps{
			I18nProps: i18n, People: []Person{{ID: "worker-1", Name: "Rafael"}},
			Roles: []AccessRole{{ID: "worker_self", Name: "Employee self-service", Active: true}},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, i18n.Text("roles.no_explicit_assignment")) || strings.Contains(markup, `checked`) {
			t.Fatalf("%s: absent assignment must not imply a selected role", locale)
		}
	}
}

func TestUXBlindRoleBadgesUseNames(t *testing.T) {
	markup, err := ui.RenderToString(workerRoleAssignmentEditor(I18nProps{Locale: ResolveProductLocale("en-US")}, Person{Name: "Rafael"}, []AccessRole{{ID: "hcm_admin", Name: "HCM administrator", Active: true}}, WorkerRoleAssignment{RoleIDs: []string{"hcm_admin"}}, false, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="status">HCM administrator`) || strings.Contains(markup, `class="status">hcm_admin`) {
		t.Fatal("badge must use the role display name")
	}
}

func TestTodo_UXBLIND_014_RolesFilterKeepsTheDraftAcrossRerender(t *testing.T) {
	first := testView(PageRoles)
	first.People = []Person{
		{ID: "rafael", Name: "Rafael Torres", Role: "HCM administrator", Team: "People"},
		{ID: "isaac", Name: "Isaac Cole", Role: "Engineer", Team: "Product"},
	}
	first.Query = "Rafael"
	first.Navigate = func(string) {}
	firstDoc, err := Render(first)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(firstDoc, "Rafael Torres") || strings.Contains(firstDoc, "Isaac Cole") {
		t.Fatal("the filtered roles projection did not narrow the employee directory")
	}

	second := first
	second.Query = "Rafael"
	second.People = append(first.People, Person{ID: "marisol", Name: "Marisol Vega", Role: "Designer", Team: "Product"})
	secondDoc, err := Render(second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(secondDoc, `value="Rafael"`) || strings.Contains(secondDoc, "Marisol Vega") {
		t.Fatal("rerender replaced or ignored the employee filter draft")
	}
}

func TestTodo_UXBLIND_014_RolesFilterPreservesRouteQuery(t *testing.T) {
	action := "/workspace/app/roles?locale=de-DE&nav=collapsed"
	got := roleFilterHref(action, " Rafael ")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	values := parsed.Query()
	if values.Get("q") != "Rafael" || values.Get("locale") != "de-DE" || values.Get("nav") != "collapsed" {
		t.Fatalf("filter route state = %q, want q plus existing route state", got)
	}
}

func TestRolesPageComposesCatalogCreationAndEmployeeAssignments(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{
		{ID: "manager", Name: "People manager", Description: "Manages a team", System: true, Active: true},
		{ID: "recruiter", Name: "Recruiter", Description: "Supports hiring", Active: true},
	}
	view.People = []Person{{ID: "worker-1", Name: "Priya Patel", Initials: "PP", Role: "Engineer", Team: "Product"}}
	view.RoleAssignments = []WorkerRoleAssignment{{Version: 2, WorkerRef: "worker-1", RoleIDs: []string{"manager", "recruiter"}}}
	view.RolePagePermissions = []RolePagePermission{{Version: 3, RoleID: "manager", Page: PageInsights, View: true}}
	view.EffectivePermissions = []RolePagePermission{{Page: PageRoles, View: true, Create: true, Update: true}}
	view.SaveAccessRole = func(AccessRole) {}
	view.SaveWorkerRoleAssignment = func(WorkerRoleAssignment) {}
	view.SaveRolePagePermission = func(RolePagePermission) {}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="#role-catalog"`, `href="#role-assignments"`, "Role catalog", "Create a role", "Priya Patel", "People manager", "Recruiter", `name="role"`, "Save employee roles", "Page and action access", "Insights", "View", "Create", "Update", "Delete"} {
		if !strings.Contains(doc, want) {
			t.Errorf("roles page missing %q", want)
		}
	}
}

func TestRolesPageHidesMutatingAffordancesForReadOnlyViewer(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "worker_self", Name: "Employee self-service", System: true, Active: true}}
	view.RolePagePermissions = []RolePagePermission{{RoleID: "worker_self", Page: PageInsights, View: true}}
	view.EffectivePermissions = []RolePagePermission{{Page: PageRoles, View: true}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Role catalog is read-only", "View only", `disabled`} {
		if !strings.Contains(doc, want) {
			t.Errorf("read-only roles page missing %q", want)
		}
	}
	for _, forbidden := range []string{">Create role<", ">Save employee roles<"} {
		if strings.Contains(doc, forbidden) {
			t.Errorf("read-only roles page exposes %q", forbidden)
		}
	}
}

func TestRolesPageUsesScannableAssignmentTable(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "People manager", Active: true}}
	view.People = []Person{{ID: "worker-1", Name: "Priya Patel", Role: "Engineer", Team: "Product"}}
	view.RoleAssignments = []WorkerRoleAssignment{{WorkerRef: "worker-1", RoleIDs: []string{"manager"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `class="employee-role-table`) || !strings.Contains(doc, `scope="col"`) || !strings.Contains(doc, `data-worker-ref="worker-1"`) {
		t.Fatal("role assignment directory should render as an accessible table")
	}
	if strings.Contains(doc, `class="employee-role-editor"><summary`) {
		t.Fatal("workforce assignment should not use one accordion per employee")
	}
}
