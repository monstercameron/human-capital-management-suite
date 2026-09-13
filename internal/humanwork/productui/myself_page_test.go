package productui

import (
	"strings"
	"testing"
)

// TestProfileWorkflowEmptyReasonRespectsCreatePermission proves the person
// workflow launcher never renders a bare, unexplained empty state.
//
// PROMOUX-001 changed the unauthorized branch's expectation: before, a
// denied requester saw a blank UnavailableDetail alongside zero workflows --
// exactly the "no available workflows, no reason" defect this todo's RED
// names. A denied requester must still see zero launchable workflows (no
// job-ladder detail that would tell them anything about this specific
// person), but the reason itself is never blank: it is the generic,
// viewer-safe PromotionWithheld text every worker gets under a denied
// create authority, so it carries no signal about this person's real
// ladder or conflict state.
func TestProfileWorkflowEmptyReasonRespectsCreatePermission(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		view := testView(PageMyself)
		view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: allowed}}
		person := Person{ID: "worker-avery", Name: "Avery", PromotionAvailability: PromotionIneligible}
		props := personWorkflowLauncherProps(view, person, PageMyself)
		if allowed && props.UnavailableDetail != view.Locale.Text("workflow.no_promotion_path") {
			t.Fatal("authorized requester lost the job-ladder explanation")
		}
		if !allowed && (props.UnavailableDetail != view.Locale.Text("workflow.promotion_withheld") || len(props.Workflows) != 0) {
			t.Fatal("denied requester either received job-ladder advice/launch actions, or lost the generic withheld reason")
		}
	}
}

func TestMyselfPageUsesOnlyTheAuthenticatedViewerBinding(t *testing.T) {
	view := testView(PageMyself)
	view.SelectedPerson = "worker-jordan"
	view.Viewer = ViewerProfile{PersonID: "worker-avery", Name: "Avery Patel", Initials: "AP", PhotoURL: "/workspace/assets/person-jane-small.jpg", Role: "DES2 · G6"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Changes use governed workflows", "View only", "Avery Patel", "Payroll &amp; compensation",
		"CAD 118,000", "CA-ON", "Pay statements, deductions, taxes, bank details, and pay schedules are not exposed",
		"My workflow history", `href="/workspace/app/journeys?mode=new&amp;worker=worker-avery"`,
		`action="/workspace/app/myself"`,
		// UXAUDIT-004: the Myself subtree is a real WAI-ARIA tree
		// (role="tree"/"treeitem"), not a generic role="list" -- see
		// TestTodo_UXAUDIT_004_Accessibility for the full contract.
		"My organization tree", `role="tree"`, `aria-current="true"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("Myself page missing %q", want)
		}
	}
	if strings.Contains(doc, "<h2>Jordan Lee</h2>") || strings.Contains(doc, `name="person"`) {
		index := strings.Index(doc, `name="person"`)
		if index >= 0 {
			start, end := index-120, index+180
			if start < 0 {
				start = 0
			}
			if end > len(doc) {
				end = len(doc)
			}
			t.Fatalf("Myself exposed person selector: %s", doc[start:end])
		}
		t.Fatal("Myself accepted a selected-person identity instead of the viewer binding")
	}
	for _, forbidden := range []string{"Edit payroll", "Edit compensation", "Save changes", "Update profile"} {
		if strings.Contains(doc, forbidden) {
			t.Fatalf("Myself exposed direct mutation control %q", forbidden)
		}
	}
}

func TestMyselfFailsClosedWithoutAuthorizedWorkerBinding(t *testing.T) {
	view := testView(PageMyself)
	view.Viewer = ViewerProfile{}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Employee profile not connected") || !strings.Contains(doc, "No employee or payroll data can be shown") {
		t.Fatal("Myself did not explain the missing identity binding")
	}
	if strings.Contains(doc, "<h2>Avery Patel</h2>") || strings.Contains(doc, "Payroll &amp; compensation") || strings.Contains(doc, "Start a workflow") {
		t.Fatal("Myself leaked worker data or actions without an identity binding")
	}
}

func TestMyselfHistoryAndFiltersStayOnTheSelfServiceRoute(t *testing.T) {
	view := testView(PageMyself)
	view.Viewer = ViewerProfile{PersonID: "worker-avery", Name: "Avery Patel", Initials: "AP"}
	view = ApplyRequest(view, PageRequest{WorkflowQuery: "promotion", HistoryOutcome: "completed", HistoryPageSize: 10})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`action="/workspace/app/myself"`, `name="workflow_q"`, `name="outcome"`, `href="/workspace/app/myself?`} {
		if !strings.Contains(doc, want) {
			t.Errorf("self-service state missing %q", want)
		}
	}
	if strings.Contains(doc, `name="history_person"`) || strings.Contains(doc, `name="person"`) {
		t.Fatal("self-service filters exposed a worker selector")
	}
}

func TestMyselfUsesSharedProfileComposition(t *testing.T) {
	view := testView(PageMyself)
	view.Viewer = ViewerProfile{PersonID: "worker-avery", Name: "Avery Patel", Initials: "AP"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `class="person-profile-composition"`) || !strings.Contains(doc, `class="person-layout"`) {
		t.Fatal("Myself did not reuse the shared person-profile composition")
	}
}
