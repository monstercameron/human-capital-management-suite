package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_073(t *testing.T) {
	view := employeePageTestView(PageOrganization)
	view.JourneyPopulation = &JourneyPopulation{}
	view.Work = nil

	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, view.Locale.Text("page.insights.label")) {
		t.Fatal("Insights remained in employee navigation without a journey summary")
	}
	if strings.Contains(doc, view.Locale.Text("organization.metadata_title")) || strings.Contains(doc, "Visible workforce") {
		t.Fatal("employee Organization rendered a zero metadata card beside its empty state")
	}
	if !strings.Contains(doc, view.Locale.Text("organization.empty_title")) {
		t.Fatal("employee Organization omitted its single honest empty state")
	}
}

func TestTodo_UXBLIND_073_Browser(t *testing.T) {
	view := employeePageTestView(PageOrganization)
	view.OrganizationView = organizationViewTree
	view.Viewer = ViewerProfile{PersonID: "self", Name: "Ana Flores", Initials: "AF"}
	view.People = []Person{
		{ID: "manager", WorkerID: "11111111-1111-4111-8111-111111111111", Name: "Morgan Lee", Team: "Care Operations", ManagerRelationship: OrganizationRelationshipRoot},
		{ID: "self", WorkerID: "22222222-2222-4222-8222-222222222222", Name: "Ana Flores", Team: "Care Operations", ManagerID: "11111111-1111-4111-8111-111111111111", Manager: "Morgan Lee", ManagerRelationship: OrganizationRelationshipVisible},
		{ID: "report", WorkerID: "33333333-3333-4333-8333-333333333333", Name: "Sam Ortiz", Team: "Care Operations", ManagerID: "22222222-2222-4222-8222-222222222222", Manager: "Ana Flores", ManagerRelationship: OrganizationRelationshipVisible},
	}

	markup, err := ui.RenderToString(Build(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Morgan Lee", "Ana Flores", "Sam Ortiz", "Reports to Morgan Lee", "Direct reports: 1", "Care Operations", "Visible workforce"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("employee Organization omitted %q: %s", want, markup)
		}
	}
}

func employeePageTestView(page PageID) View {
	view := ApplyPagePermissions(NewView(page, "tenant-test", "Ana Flores", "employee"), []RolePagePermission{
		{Page: PageHome, View: true}, {Page: PageMyself, View: true}, {Page: PageOrganization, View: true}, {Page: PageInsights, View: true}, {Page: PageHelp, View: true}, {Page: PageSettings, View: true},
	})
	view.Roles = []string{"worker_self"}
	view.Viewer = ViewerProfile{PersonID: "self", Name: "Ana Flores", Initials: "AF"}
	return view
}
