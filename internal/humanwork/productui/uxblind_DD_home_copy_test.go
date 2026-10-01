package productui

import (
	"strings"
	"testing"
)

func restrictedHomeView() View {
	view := testView(PageHome)
	view.Work = []WorkItem{
		{ID: "tracked", Person: "Jordan Lee", PersonRef: "worker-jordan", Status: "In progress", ViewerRelationships: []string{"INITIATOR"}, ViewerResponsibility: "TRACKING", Href: "/workspace/app/journeys?journey=tracked"},
		{ID: "closed", Person: "Avery Patel", PersonRef: "worker-avery", Status: "Completed", Terminal: true, ViewerResponsibility: "CLOSED", Href: "/workspace/app/journeys?journey=closed", CompletedAt: "4 Aug 2026 · 14:32 UTC"},
	}
	return ApplyPagePermissions(view, []RolePagePermission{
		{Page: PageHome, View: true},
		{Page: PageWork, View: true},
	})
}

func TestTodo_UXBLIND_051(t *testing.T) {
	view := restrictedHomeView()
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `/workspace/app/journeys`) {
		t.Fatalf("Home exposed a Journeys link without Journeys access: %s", doc)
	}
	if strings.Contains(doc, "Follow the status of requests in Journeys") {
		t.Fatalf("Home used forbidden-page copy: %s", doc)
	}
	if !strings.Contains(doc, view.Locale.Text("work.action_queue_empty_detail_no_journeys")) {
		t.Fatalf("Home omitted the access-scoped empty copy: %s", doc)
	}
}

func TestTodo_UXBLIND_051_Browser(t *testing.T) {
	view := restrictedHomeView()
	journeyRows := homeJourneyRows(view, view.Work, true)
	for _, row := range journeyRows.Items {
		if row.Href != "" || row.Navigate != nil {
			t.Fatalf("journey row retained a forbidden destination: %+v", row)
		}
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `/workspace/app/journeys`) {
		t.Fatalf("tracked Home card exposed a forbidden destination: %s", doc)
	}
}

func TestTodo_UXBLIND_084(t *testing.T) {
	view := NewView(PageHome, "ironridge-demo", "Ir 00001", "self_service_view")
	view.Viewer = ViewerProfile{PersonID: "IR-00001", Name: "Ir 00001"}
	view.People = []Person{{ID: "worker-walt", WorkerID: "worker-id-walt", WorkerNumber: "IR-00001", Name: "Walt Brennan", PreferredName: "Walt"}}
	if got := preferredViewerFirstName(view); got != "Walt" {
		t.Fatalf("preferredViewerFirstName() = %q, want Walt", got)
	}
	identity := ResolvePageIdentity(view)
	if !strings.Contains(identity.Title, "Walt") || strings.Contains(identity.Title, "Ir") {
		t.Fatalf("Home greeting = %q, want Walt and no worker-number fragment", identity.Title)
	}
}

func TestTodo_UXBLIND_084_Browser(t *testing.T) {
	view := NewView(PageHome, "ironridge-demo", "Ir 00001", "self_service_view")
	view.Viewer = ViewerProfile{PersonID: "IR-00001", Name: "Ir 00001"}
	view.People = []Person{{ID: "worker-walt", WorkerNumber: "IR-00001", Name: "Walt Brennan", PreferredName: "Walt"}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Walt") || strings.Contains(doc, "Good afternoon, Ir.") {
		t.Fatalf("browser Home greeting used the worker number: %s", doc)
	}
}

func TestTodo_UXBLIND_084_Regression(t *testing.T) {
	for _, tc := range []struct {
		name, tenant, principal, workerNumber, preferred string
	}{
		{name: "ironridge prefix", tenant: "ironridge-demo", principal: "IR-00001", workerNumber: "IR-00001", preferred: "Walt"},
		{name: "harborcare prefix", tenant: "harborcare-demo", principal: "HC-21050", workerNumber: "HC-21050", preferred: "Amina"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := NewView(PageHome, tc.tenant, tc.principal, "self_service_view")
			view.Viewer = ViewerProfile{PersonID: tc.principal, Name: tc.principal}
			view.People = []Person{{ID: "worker-stable", WorkerNumber: tc.workerNumber, PreferredName: tc.preferred, Name: tc.preferred + " Example"}}
			if got := preferredViewerFirstName(view); got != tc.preferred {
				t.Fatalf("preferredViewerFirstName() = %q, want %q", got, tc.preferred)
			}
			if got := ResolvePageIdentity(view).Title; !strings.Contains(got, tc.preferred) {
				t.Fatalf("greeting = %q, want preferred first name %q", got, tc.preferred)
			}
		})
	}
}
