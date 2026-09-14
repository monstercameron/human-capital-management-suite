package productui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestHomeWorkRefinementKeepsDueFirstAndSeparatesWaits(t *testing.T) {
	items := []WorkItem{
		{ID: "wait", Status: "Waiting on employee", Due: "2026-09-01"},
		{ID: "later", Status: "Awaiting approval", Due: "2026-10-01"},
		{ID: "soon", Status: "Blocked", Due: "2026-09-15"},
		{ID: "draft", Status: "Draft"},
	}
	if got := ActionQueue(items); len(got) != 2 || got[0].ID != "later" || got[1].ID != "soon" {
		t.Fatalf("action queue = %+v", got)
	}
	ordered := PrioritizeDueWork(ActionQueue(items))
	if got := []string{ordered[0].ID, ordered[1].ID}; !reflect.DeepEqual(got, []string{"soon", "later"}) {
		t.Fatalf("due order = %q", got)
	}
	if got := PassiveWaits(items); len(got) != 1 || got[0].ID != "wait" {
		t.Fatalf("passive waits = %+v", got)
	}
	if got := ResumableDrafts(items, ViewerProfile{PersonID: "worker"}); len(got) != 0 {
		t.Fatalf("unassigned draft escaped viewer scope: %+v", got)
	}
}

func TestMyWorkBucketsDoNotDuplicateActionAndPassiveItems(t *testing.T) {
	items := []WorkItem{
		{ID: "action", PersonRef: "worker", Status: "Awaiting approval"},
		{ID: "wait", PersonRef: "worker", Status: "Pending external review"},
		{ID: "draft", PersonRef: "worker", Status: "Draft"},
		{ID: "track", PersonRef: "worker", Status: "Tracked"},
		{ID: "other", PersonRef: "other", Status: "Blocked"},
	}
	buckets := ResolveMyWorkBuckets(items, ViewerProfile{PersonID: "worker"})
	if len(buckets.ActionQueue) != 1 || buckets.ActionQueue[0].ID != "action" {
		t.Fatalf("action bucket = %+v", buckets.ActionQueue)
	}
	if len(buckets.PassiveWaits) != 1 || buckets.PassiveWaits[0].ID != "wait" {
		t.Fatalf("wait bucket = %+v", buckets.PassiveWaits)
	}
	if len(buckets.Drafts) != 1 || buckets.Drafts[0].ID != "draft" || len(buckets.Tracked) != 1 || buckets.Tracked[0].ID != "track" {
		t.Fatalf("secondary buckets = %+v", buckets)
	}
}

func TestRecentPeopleJoinsOnlyAuthorizedPeopleInRecentOrder(t *testing.T) {
	people := []Person{{ID: "p1", Name: "Avery", Initials: "AP"}, {ID: "p2", Name: "Jordan", Initials: "JL"}}
	recent := []WorkItem{{ID: "one", PersonRef: "p2"}, {ID: "two", PersonRef: "hidden"}, {ID: "three", PersonRef: "p2"}, {ID: "four", Person: "Avery"}}
	got := RecentPeople(people, recent, 5)
	if len(got) != 2 || got[0].ID != "p2" || got[1].ID != "p1" {
		t.Fatalf("recent people = %+v", got)
	}
}

func TestHomeCompositionRendersDraftTrackedAndRecentPeopleRails(t *testing.T) {
	props := HomePageProps{
		ShowWork:     true,
		Work:         WorkCollectionProps{Title: "Action queue", Rows: []WorkRowProps{{ID: "a", Title: "Review", Href: "/work"}}},
		ShowDrafts:   true,
		Drafts:       WorkCollectionProps{Title: "Resumable drafts", Rows: []WorkRowProps{{ID: "d", Title: "Draft", Href: "/draft"}}},
		ShowTracked:  true,
		Tracked:      TrackedRequestsProps{Title: "Tracked requests", Description: "Waiting", Items: []TrackedRequestProps{{ID: "t", Status: "Pending", Open: true}}},
		ShowPeople:   true,
		RecentPeople: RecentPeopleProps{Title: "Recent people", Items: []RecentPerson{{ID: "p", Name: "Taylor", Role: "Manager"}}},
		Overview:     SummaryCardProps{Title: "Overview"}, QuickStart: QuickActionsProps{Title: "Start"}, Recent: RecentActivityProps{Title: "Recent"},
	}
	markup, err := ui.RenderToString(ui.CreateElement(HomePage, props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Resumable drafts", "Tracked requests", "Recent people", `class="surface work-list"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("home refinement missing %q: %s", want, markup)
		}
	}
}

func TestHomeRecentPeopleIntroKeepsSpaceBeforeRows(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(RecentPeoplePanel, RecentPeopleProps{
		Title: "Recent people", Description: "People connected to your recent work.",
		Items: []RecentPerson{{ID: "p", Name: "Taylor", Role: "Manager"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="muted recent-people-intro"`) || !strings.Contains(markup, `class="recent"`) {
		t.Fatalf("recent people intro lacks its own spacing slot: %s", markup)
	}
	if !strings.Contains(Stylesheet(), `.recent-people-intro{margin:0;padding-block:0 calc(var(--hcm-space-2) * var(--hcm-density));padding-inline:calc(var(--hcm-space-3) * var(--hcm-density));`) {
		t.Fatal("recent people intro must reserve tokenized space before the bordered list")
	}
}

func TestHomeQuickActionsRespectResolvedRolePermissions(t *testing.T) {
	view := NewView(PageHome, "tenant", "principal", "scope")
	view.Roles = []string{"employee"}
	view.Viewer.Role = "employee"
	view = ApplyPagePermissions(view, []RolePagePermission{{Page: PagePeople, View: false}, {Page: PageJourneys, Create: false}})
	for _, action := range ResolveHomeQuickActions(view) {
		if action.Label == "Choose a worker" || action.Label == "Choose an employee to promote" {
			t.Fatalf("denied worker start rendered: %+v", action)
		}
	}
}

func TestHomeActionQueueUsesTheSameRoutedOwnershipAsMyWork(t *testing.T) {
	view := NewView(PageHome, "HarborCare Demo", "worker-thomas", "payroll")
	view.Viewer = ViewerProfile{PersonID: "worker-thomas", Name: "Thomas"}
	view.Work = []WorkItem{
		{ID: "finance", Person: "Omar", PersonRef: "worker-omar", AssigneeRef: "worker-thomas", Status: "Finance approval", NextStep: "finance_decision", ViewerResponsibility: "ACTION_REQUIRED", ViewerRelationships: []string{"ASSIGNEE"}, Href: "/journey/finance"},
		{ID: "manager", Person: "Omar", PersonRef: "worker-omar", AssigneeRef: "worker-dominic", Status: "Manager approval", NextStep: "manager_decision", ViewerResponsibility: "OBSERVING", Href: "/journey/manager"},
	}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "selected=finance") || strings.Contains(markup, "selected=manager") {
		t.Fatalf("home did not share My Work's routed queue: %s", markup)
	}
}
