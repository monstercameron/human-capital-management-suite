package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_017_MyWorkPresentsAnAssignedActionQueue(t *testing.T) {
	view := testView(PageWork)
	view.Viewer.PersonID = "viewer"
	view.Work = []WorkItem{
		{ID: "later", Person: "Later Worker", AssigneeRef: "viewer", Status: "Awaiting approval", Due: "2026-09-25", NextStep: "approval_decision", ViewerResponsibility: "ACTION_REQUIRED", ViewerMembership: "ASSIGNEE", Href: "/workspace/app/journeys?journey=later"},
		{ID: "passive", Person: "Waiting Worker", AssigneeRef: "viewer", Status: "Waiting for effective date", Due: "2026-09-01", NextStep: "await_effective_date", ViewerResponsibility: "TRACKING", ViewerRelationships: []string{"INITIATOR"}, Href: "/workspace/app/journeys?journey=passive"},
		{ID: "other", Person: "Other Worker", AssigneeRef: "other", Status: "Awaiting approval", Due: "2026-09-01", NextStep: "approval_decision", ViewerResponsibility: "OBSERVING", Href: "/workspace/app/journeys?journey=other"},
		{ID: "sooner", Person: "Sooner Worker", AssigneeRef: "viewer", Status: "Awaiting approval", Due: "2026-09-18", NextStep: "approval_decision", ViewerResponsibility: "ACTION_REQUIRED", ViewerMembership: "ASSIGNEE", Href: "/workspace/app/journeys?journey=sooner"},
	}
	view.SelectedWork = ""
	markup, err := ui.RenderToString(workPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Other Worker") {
		t.Fatal("another assignee leaked into My Work")
	}
	queue := strings.SplitN(markup, `data-work-kind="action-queue"`, 2)
	if len(queue) != 2 {
		t.Fatal("My Work did not render its explicit action queue")
	}
	queueMarkup := strings.SplitN(queue[1], `</section>`, 2)[0]
	if strings.Contains(queueMarkup, "Waiting Worker") {
		t.Fatal("passive wait displaced decisions in the action queue")
	}
	if first, second := strings.Index(queueMarkup, "Sooner Worker"), strings.Index(queueMarkup, "Later Worker"); first < 0 || second < 0 || first >= second {
		t.Fatal("action queue is not due-first")
	}
	if !strings.Contains(markup, view.Locale.Text("work.action_queue_description")) {
		t.Fatal("My Work lost localized task language")
	}
}

func TestTodo_UXAUDIT_017_MyWorkEmptyExplainsWhereToTrackRequests(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageWork)
		view.Locale = ResolveProductLocale(locale)
		view.Work = nil
		markup, err := ui.RenderToString(workPage(view))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"home.needs_action", "work.empty_title", "work.action_queue_empty_detail"} {
			if !strings.Contains(markup, view.Locale.Text(key)) {
				t.Fatalf("%s missing localized %s", locale, key)
			}
		}
		if !strings.Contains(markup, view.Locale.Text("work.track_requests")) || strings.Contains(markup, view.Locale.Text("work.authorized")) {
			t.Fatalf("%s empty queue needs a useful tracking path, not a redundant footer", locale)
		}
		if strings.Contains(markup, "Decisions and next steps that need your attention now") {
			t.Fatal("hardcoded English task copy leaked into My Work")
		}
	}
}

func TestTodo_UXAUDIT_017_EmptyWorkDoesNotOfferDeniedJourneyRoute(t *testing.T) {
	view := testView(PageWork)
	view.Work = nil
	view.EffectivePermissions = []RolePagePermission{{Page: PageWork, View: true}, {Page: PageJourneys, View: false}}
	markup, err := ui.RenderToString(workPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, view.Locale.Text("work.track_requests")) {
		t.Fatal("empty work state offered a denied tracking route")
	}
}

func TestTodo_UXAUDIT_017_UnboundViewerCannotSeeAssignmentsOrCompensation(t *testing.T) {
	view := testView(PageWork)
	view.Viewer.PersonID = ""
	view.Work[0].AssigneeRef = "another-worker"
	markup, err := ui.RenderToString(workPage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Jordan Lee", "ENG2 G6", "work-preview", "another-worker"} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("unbound viewer saw %q", forbidden)
		}
	}
	if !strings.Contains(markup, view.Locale.Text("work.action_queue_empty_title")) {
		t.Fatal("unbound viewer did not receive an honest empty state")
	}
}

func TestTodo_UXAUDIT_017_JourneysFallbackTracksLifecycleWithoutDuplicatingMyWork(t *testing.T) {
	view := testView(PageJourneys)
	view.Work[0].StatusKey = "work.awaiting"
	markup, err := ui.RenderToString(journeysPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `tracked-requests-panel`) || !strings.Contains(markup, view.Locale.Text("work.past")) {
		t.Fatal("Journeys fallback lost lifecycle tracking or history continuity")
	}
	if strings.Contains(markup, `data-work-kind="action-queue"`) || strings.Contains(markup, `work-preview`) {
		t.Fatal("Journeys fallback duplicates the My Work assignment workbench")
	}
	if !strings.Contains(markup, view.Locale.Text("work.awaiting")) || !strings.Contains(markup, "Jordan Lee") {
		t.Fatal("Journeys fallback lost localized status or subject identity")
	}
	denied := view
	denied.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true}, {Page: PageHistory, View: false}}
	deniedMarkup, err := ui.RenderToString(journeysPage(denied))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(deniedMarkup, view.Locale.Text("work.past")) {
		t.Fatal("history link leaked without page admission")
	}
}

func TestTodo_UXAUDIT_017_ViewerScopedPageCopyAndFilterEmptyStates(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, filter := range []struct{ name, title, detail string }{
			{"review", "work.review_empty_title", "work.review_empty_detail"},
			{"blocked", "work.blocked_empty_title", "work.blocked_empty_detail"},
		} {
			view := testView(PageWork)
			view.Locale = ResolveProductLocale(locale)
			view.Work = nil
			view.WorkFilter = filter.name
			markup, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"page.work.subtitle", filter.title, filter.detail} {
				label := view.Locale.Text(key)
				if label == "" || label == key || !strings.Contains(markup, label) {
					t.Errorf("%s %s missing localized viewer-scoped %s", locale, filter.name, key)
				}
			}
			if strings.Contains(markup, "Live promotion journeys that need attention") {
				t.Fatal("My Work must not claim that all live journeys require the viewer's action")
			}
		}
	}
}

func TestTodo_UXAUDIT_017_ActionRowsLabelDueDates(t *testing.T) {
	view := testView(PageWork)
	view.Viewer.PersonID = "viewer"
	view.Work = []WorkItem{{ID: "approval", Title: "Review promotion", Person: "Priya", AssigneeRef: "viewer", Status: "Awaiting approval", NextStep: "approval_decision", ViewerResponsibility: "ACTION_REQUIRED", WorkSummary: true, ViewerMembership: "ASSIGNEE", WorkDue: "2026-09-25", Due: "2026-10-01", Href: "/workspace/app/journeys?journey=approval"}}
	markup, err := ui.RenderToString(workPage(view))
	if err != nil {
		t.Fatal(err)
	}
	want := view.Locale.Text("work.approval_due", map[string]string{"date": "2026-09-25"})
	if !strings.Contains(markup, want) || !strings.Contains(markup, `class="row-work-due"`) {
		t.Fatalf("action row must label projected due date %q", want)
	}
}
