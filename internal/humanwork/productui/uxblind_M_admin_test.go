package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_037(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "hcm_admin", Name: "HCM administrator", Active: true}}
	view.RolePagePermissions = []RolePagePermission{{RoleID: "hcm_admin", Page: PagePeople, View: true}, {RoleID: "hcm_admin", Page: PageRoles, View: true}}
	view.RoleFeaturePermissions = []RoleFeaturePermission{{RoleID: "hcm_admin", Page: PagePeople, Feature: "content", View: true}}
	view.People = []Person{{ID: "worker-1", Name: "Rafael"}}
	view.RoleAssignments = []WorkerRoleAssignment{{WorkerRef: "worker-1", RoleIDs: []string{"hcm_admin"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="admin-page-frame roles-access-page"`,
		"Pages and actions this role can use: 2",
		"Feature grants narrow page access",
		"HCM administrator",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("roles surface missing %q", want)
		}
	}
	if strings.Contains(doc, "No explicit assignment") || strings.Contains(doc, "← Admin") {
		t.Fatal("roles surface still exposes the blind assignment or duplicate back copy")
	}
}

func TestTodo_UXBLIND_037_Browser(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "People manager", Active: true}}
	view.People = []Person{{ID: "worker-1", Name: "Priya"}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(doc, `class="admin-page-frame roles-access-page"`) != 1 || strings.Contains(doc, "← Admin") {
		t.Fatal("browser projection does not have one admin frame and one back path")
	}
}

func TestTodo_UXBLIND_039(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{
		{ID: "manager", Name: "People manager", Active: true},
		{ID: "hcm_admin", Name: "HCM administrator", Active: true},
	}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{
		{RoleID: "manager", Mode: "OWN_UNIT"},
		{RoleID: "hcm_admin", Mode: "ALL"},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	question := view.Locale.Text("organization_visibility.scope_title")
	if got := strings.Count(doc, question); got != 1 {
		t.Fatalf("visibility question repeated %d times, want one column header", got)
	}
	for _, want := range []string{
		`class="role-visibility-column-header"`,
		`name="organization-visibility-mode-manager"`,
		`checked`,
		view.Locale.Text("organization_visibility.save"),
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("visibility surface missing %q", want)
		}
	}
}

func TestTodo_UXBLIND_039_Browser(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "People manager", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{RoleID: "manager", Mode: "ALL"}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `class="admin-page-frame organization-visibility-page"`) || strings.Contains(doc, `organization-visibility-summary-prompt`) {
		t.Fatal("visibility browser projection still repeats the row question")
	}
}

func TestTodo_UXBLIND_041(t *testing.T) {
	markup, err := ui.RenderToString(AppearancePage(AppearancePageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Theme: DefaultCustomerTheme(),
		ColorModes: ColorModeOptions(), Palettes: PaletteOptions(), Shapes: ShapeOptions(), Densities: DensityOptions(),
		Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(), Navigation: NavigationOptions(), Motions: MotionOptions(), Editable: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, `class="surface appearance-intro"`) != 1 || !strings.Contains(markup, `class="appearance-section-link is-selected"`) || !strings.Contains(markup, `aria-current="page"`) {
		t.Fatal("appearance surface does not expose one intro and a selected section")
	}
	nav := strings.Index(markup, `class="appearance-section-nav surface"`)
	controls := strings.Index(markup, `id="appearance-section-brand"`)
	if nav < 0 || controls < 0 || nav > controls {
		t.Fatal("appearance controls are not reachable after the selected section navigation")
	}
}

func TestTodo_UXBLIND_041_Browser(t *testing.T) {
	markup, err := ui.RenderToString(AppearancePage(AppearancePageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Theme: DefaultCustomerTheme(), Editable: true}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="admin-page-frame appearance-page"`) || strings.Contains(markup, "Make the workspace feel like your organization\nMake the workspace feel like your organization") {
		t.Fatal("appearance browser surface is not using the shared admin frame")
	}
}

func TestTodo_UXBLIND_042(t *testing.T) {
	view := testView(PageHelp)
	view.EffectivePermissions = []RolePagePermission{
		{Page: PageHelp, View: true}, {Page: PageChat, View: true}, {Page: PageDocs, View: true},
		{Page: PageProjects, View: true}, {Page: PageWorkflowDesigner, View: true},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		view.Locale.Text("page.chat.label"), view.Locale.Text("page.docs.label"),
		view.Locale.Text("page.projects.label"), view.Locale.Text("page.workflow_designer.label"),
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("help missing navigation topic %q", want)
		}
	}
	if strings.Contains(doc, "contact your HR administrator") || strings.Contains(doc, "No explicit assignment") {
		t.Fatal("help still sends an administrator to the old HR dead end")
	}
	myself, err := ui.RenderToString(SelfServiceBoundary(SelfServiceBoundaryProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(myself, `M20 6 9 17`) {
		t.Fatal("Myself banner still uses the success checkmark")
	}
	if strings.Contains(ResolveProductLocale("en-US").Text("page.myself.subtitle"), "payroll") {
		t.Fatal("Myself subtitle still promises payroll information")
	}
}

func TestTodo_UXBLIND_042_Browser(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		view := testView(PageHelp)
		view.Locale = ResolveProductLocale(locale)
		view.EffectivePermissions = []RolePagePermission{{Page: PageHelp, View: true}, {Page: PageChat, View: true}, {Page: PageDocs, View: true}, {Page: PageProjects, View: true}, {Page: PageWorkflowDesigner, View: true}}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, view.Locale.Text("page.chat.label")) || !strings.Contains(doc, view.Locale.Text("page.docs.label")) {
			t.Fatalf("%s help omitted a navigation topic", locale)
		}
	}
}
