package productui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_015(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "principal", "scope")
	view.Viewer = ViewerProfile{PersonID: "worker"}
	view.Work = []WorkItem{
		{ID: "draft", PersonRef: "worker", Title: "Promotion draft", Status: "Draft", Due: "2026-09-14", Href: "/draft"},
		{ID: "due", PersonRef: "worker", Title: "Review request", Status: "Awaiting approval", Due: "2026-09-13", Href: "/work"},
		{ID: "wait", PersonRef: "worker", Title: "Employee request", Status: "Waiting on employee", Href: "/journey"},
	}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Needs your attention", "Resumable drafts", "Tracked requests", "Recent people", "Promotion draft"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("Home missing prioritized continuity surface %q: %s", want, markup)
		}
	}
	if strings.Index(markup, "Review request") > strings.Index(markup, "Promotion draft") {
		t.Fatalf("due work should precede draft continuity: %s", markup)
	}
}

func TestTodo_UXAUDIT_015_Regression(t *testing.T) {
	props := HomePageProps{
		ShowDrafts: true, Drafts: WorkCollectionProps{Title: "Resumable drafts"},
		DraftsEmptyTitle: "No drafts to resume", DraftsEmptyDetail: "Saved work will appear here.",
		ShowTracked: true, Tracked: TrackedRequestsProps{Title: "Tracked requests", EmptyTitle: "No tracked requests", EmptyDetail: "Requests you follow will appear here."},
		ShowPeople: true, RecentPeople: RecentPeopleProps{Title: "Recent people", EmptyTitle: "No recent people", EmptyDetail: "People will appear here."},
		Recent: RecentActivityProps{Title: "Recently completed", EmptyTitle: "No completed journeys", EmptyDescription: "Completed journeys will appear here."}, ShowRecent: true,
	}
	markup, err := ui.RenderToString(ui.CreateElement(HomePage, props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"No drafts to resume", "No tracked requests", "No recent people", "No completed journeys"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("Home empty state missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "surface empty-state") || strings.Count(markup, "<h2>") != 4 {
		t.Fatalf("continuity cards must have one heading each and no nested surfaces: %s", markup)
	}
}

func TestTodo_UXAUDIT_015_Accessibility(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "principal", "scope")
	view.Roles = []string{"employee"}
	view.Viewer.Role = "employee"
	view = ApplyPagePermissions(view, []RolePagePermission{{Page: PagePeople, View: true}, {Page: PageJourneys, Create: false}})
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Start a request") || !strings.Contains(markup, "Quick links") {
		t.Fatalf("role-inaccurate quick-action heading: %s", markup)
	}
	if !strings.Contains(markup, "Find an employee") || strings.Contains(markup, "Choose a worker") {
		t.Fatalf("role-appropriate people action missing: %s", markup)
	}
	if !strings.Contains(markup, "<a ") || !strings.Contains(markup, "href=") || strings.Contains(markup, "<button") {
		t.Fatalf("quick link is not a native, keyboard-reachable navigation target: %s", markup)
	}
}

func TestTodo_UXAUDIT_015_Browser(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "principal", "scope")
	view.EffectivePermissions = []RolePagePermission{{Page: PageWork, View: false}}
	view.Work = []WorkItem{{ID: "hidden", Title: "Hidden request", Status: "Awaiting approval"}}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Hidden request") || strings.Contains(markup, "Needs your attention") {
		t.Fatalf("denied work appeared on Home: %s", markup)
	}
	assertCSSContains(t, ".work-row>.row-end", "grid-column:1", "max-width:none")
}

func TestTodo_UXAUDIT_015_Regression_DeniedWorkAndPeople(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "principal", "scope")
	view.EffectivePermissions = []RolePagePermission{{Page: PageWork}, {Page: PagePeople}}
	view.Work = []WorkItem{{ID: "hidden", Title: "Hidden request", Status: "Awaiting approval"}}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Current activity") || strings.Contains(markup, "Visible workers") || strings.Contains(markup, "Hidden request") {
		t.Fatalf("Home exposed unavailable orientation: %s", markup)
	}
}

func TestTodo_UXAUDIT_015_Regression_DeniedPersonDoesNotEnterRecentPeopleOrCount(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "principal", "scope")
	view.Viewer = ViewerProfile{PersonID: "viewer"}
	view.People = []Person{{ID: "visible", Name: "Visible Person"}, {ID: "hidden", Name: "Hidden Person"}}
	view.RecordVerdicts = map[string]AuthorizedRecord{
		"visible": {ID: "visible", Disclosable: true},
		"hidden":  {ID: "hidden", Disclosable: false},
	}
	view.Work = []WorkItem{
		{ID: "visible-work", PersonRef: "visible", Person: "Visible Person", Status: "Completed", Terminal: true},
		{ID: "hidden-work", PersonRef: "hidden", Person: "Hidden Person", Status: "Completed", Terminal: true},
	}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Visible Person") || !strings.Contains(markup, "<dt>Visible workers</dt><dd>1</dd>") {
		t.Fatalf("admitted person and count missing: %s", markup)
	}
	if strings.Contains(markup, "Hidden Person") || strings.Contains(markup, "person=hidden") || strings.Contains(markup, "<dt>Visible workers</dt><dd>2</dd>") {
		t.Fatalf("denied person influenced Home: %s", markup)
	}
}

func TestTodo_UXAUDIT_015_Regression_HistoryGrantIsIndependentOfWork(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "viewer", "scope")
	view.Viewer = ViewerProfile{PersonID: "viewer"}
	view.Work = []WorkItem{
		{ID: "approval", PersonRef: "worker", AssigneeRef: "viewer", Title: "Pending decision", Status: "Awaiting approval", NextStep: "approval_decision", ViewerResponsibility: "ACTION_REQUIRED", ViewerMembership: "ASSIGNEE"},
		{ID: "closed", PersonRef: "worker", Title: "Confidential closure", Status: "Recorded", Terminal: true},
	}
	for _, test := range []struct {
		name             string
		work, history    bool
		wantPending      bool
		wantConfidential bool
	}{
		{name: "work only", work: true, wantPending: true},
		{name: "history only", history: true, wantConfidential: true},
		{name: "neither"},
	} {
		t.Run(test.name, func(t *testing.T) {
			scoped := ApplyPagePermissions(view, []RolePagePermission{
				{Page: PageWork, View: test.work}, {Page: PageHistory, View: test.history}, {Page: PagePeople, View: false},
			})
			markup, err := ui.RenderToString(homePage(scoped))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(markup, "Pending decision") != test.wantPending || strings.Contains(markup, "Confidential closure") != test.wantConfidential {
				t.Fatalf("Home did not separate Work and History grants: %s", markup)
			}
			if !test.history && (strings.Contains(markup, "Recently completed") || strings.Contains(markup, "Completed or closed")) {
				t.Fatalf("Home exposed History surface without grant: %s", markup)
			}
		})
	}
}

func TestTodo_UXAUDIT_015_Regression_RecentPeopleRespectsWorkKindGrants(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "viewer", "scope")
	view.People = []Person{{ID: "active-worker", Name: "Active Worker"}, {ID: "closed-worker", Name: "Closed Worker"}}
	view.Work = []WorkItem{
		{ID: "active", PersonRef: "active-worker", Person: "Active Worker", Title: "Pending decision", Status: "Awaiting approval"},
		{ID: "closed", PersonRef: "closed-worker", Person: "Closed Worker", Title: "Completed decision", Status: "Recorded", Terminal: true},
	}
	for _, test := range []struct {
		name, want, denied string
		work, history      bool
	}{
		{name: "work only", want: "Active Worker", denied: "Closed Worker", work: true},
		{name: "history only", want: "Closed Worker", denied: "Active Worker", history: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			scoped := ApplyPagePermissions(view, []RolePagePermission{
				{Page: PageWork, View: test.work}, {Page: PageHistory, View: test.history}, {Page: PagePeople, View: true},
			})
			markup, err := ui.RenderToString(homePage(scoped))
			if err != nil {
				t.Fatal(err)
			}
			start := strings.Index(markup, "<h2>Recent people</h2>")
			if start < 0 {
				t.Fatalf("recent people card missing: %s", markup)
			}
			recentPeople := markup[start:]
			if end := strings.Index(recentPeople, "<h2>Recently completed</h2>"); end >= 0 {
				recentPeople = recentPeople[:end]
			}
			if !strings.Contains(recentPeople, test.want) || strings.Contains(recentPeople, test.denied) {
				t.Fatalf("recent people used a denied work kind: %s", recentPeople)
			}
		})
	}
}

func TestTodo_UXAUDIT_015_Accessibility_CompletedStatusIsNotATime(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "viewer", "scope")
	view.Work = []WorkItem{{ID: "closed", PersonRef: "worker", Title: "Recorded promotion", Status: "Recorded", Terminal: true}}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Recorded promotion") || !strings.Contains(markup, "<small>Recorded</small>") || strings.Contains(markup, "<time>Recorded</time>") {
		t.Fatalf("terminal status must be readable text, not a false date: %s", markup)
	}
}

func TestTodo_UXAUDIT_015_Regression_UnboundViewerGetsNoActionQueue(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "principal", "scope")
	view.Work = []WorkItem{{ID: "other", PersonRef: "worker", Person: "Private Worker", Title: "Private approval", Status: "Awaiting approval"}}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Private approval") || strings.Contains(markup, "<dt>Your actions</dt><dd>1</dd>") {
		t.Fatalf("unbound viewer received assigned action: %s", markup)
	}
}

func TestTodo_UXAUDIT_015_Regression_HomeSeparatesAssignedWorkFromVisibleJourneys(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "Rafael", "scope")
	view.Viewer = ViewerProfile{PersonID: "rafael"}
	view.Work = []WorkItem{{ID: "sofia-wait", PersonRef: "sofia", AssigneeRef: "sofia", Person: "Sofia", Status: "Waiting for effective date", Href: "/workspace/app/journeys?journey=sofia-wait"}}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Nothing needs your action", "Your assigned work and records visible to you.", "<dt>Your actions</dt><dd>0</dd>", "<dt>Your tracked requests and waits</dt><dd>0</dd>"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("Home does not explain viewer-scoped zero beside visible journeys: missing %q", want)
		}
	}
	if strings.Contains(markup, "No work in this view") || strings.Contains(markup, "sofia-wait") {
		t.Fatalf("Home leaked another person's request into an assigned-work slot: %s", markup)
	}
	view.Work[0].AssigneeRef = "rafael"
	markup, err = ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "<dt>Your tracked requests and waits</dt><dd>1</dd>") || !strings.Contains(markup, "sofia-wait") {
		t.Fatalf("assigned passive wait did not enter Rafael's tracking: %s", markup)
	}
}

func TestTodo_UXAUDIT_015_Regression_HomePromotionIsContextualQuickAction(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "Rafael", "scope")
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="surface panel home-quick-actions home-empty-primary"`) || !strings.Contains(markup, `class="button primary"`) || !strings.Contains(markup, "Choose an employee to promote") {
		t.Fatalf("quiet Home lost its primary promotion start: %s", markup)
	}
	assertCSSContains(t, ".home-quick-actions .quick-actions", "justify-items:start", ".home-quick-actions .quick-actions .button", "width:auto", "max-width:100%")
}

func TestTodo_UXAUDIT_015_Regression_BoundedHomeKeepsDueAndServerOrder(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "viewer", "scope")
	view.Viewer = ViewerProfile{PersonID: "viewer"}
	for index := 12; index >= 1; index-- {
		view.Work = append(view.Work, WorkItem{
			ID: fmt.Sprintf("due-%02d", index), PersonRef: "subject", AssigneeRef: "viewer",
			Title: fmt.Sprintf("Due %02d", index), Status: "Awaiting approval", Due: fmt.Sprintf("2026-09-%02d", index), NextStep: "approval_decision", ViewerResponsibility: "ACTION_REQUIRED", ViewerMembership: "ASSIGNEE",
		})
		view.Work = append(view.Work, WorkItem{
			ID: fmt.Sprintf("closed-%02d", index), PersonRef: "subject", AssigneeRef: "viewer",
			Title: fmt.Sprintf("Closed %02d", index), Status: "Recorded", Terminal: true,
		})
	}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	attentionEnd := strings.Index(markup, "<h2>Current activity</h2>")
	recentStart := strings.Index(markup, "<h2>Recently completed</h2>")
	if attentionEnd < 0 || recentStart < 0 {
		t.Fatalf("Home sections missing: %s", markup)
	}
	attention := markup[:attentionEnd]
	if strings.Count(attention, "class=\"work-row-item\"") != homeAttentionLimit {
		t.Fatalf("Home did not bound attention to %d rows: %s", homeAttentionLimit, attention)
	}
	for index := 1; index <= 12; index++ {
		want := fmt.Sprintf("Due %02d", index)
		if strings.Contains(attention, want) != (index <= homeAttentionLimit) {
			t.Fatalf("due-first attention window wrong for %s: %s", want, attention)
		}
	}
	if !strings.Contains(markup, "<dt>Your actions</dt><dd>12</dd>") || !strings.Contains(markup, ">12 items<") {
		t.Fatalf("bounded attention lost total count: %s", markup)
	}
	recent := markup[recentStart:]
	if strings.Count(recent, "class=\"activity\"") != homeRecentLimit {
		t.Fatalf("Home did not bound recent outcomes to %d: %s", homeRecentLimit, recent)
	}
	for index := 12; index >= 1; index-- {
		want := fmt.Sprintf("Closed %02d", index)
		if strings.Contains(recent, want) != (index > 12-homeRecentLimit) {
			t.Fatalf("recent outcomes changed server order for %s: %s", want, recent)
		}
	}
}

func TestTodo_UXAUDIT_015_Regression_RecentPersonProfileRequiresProfileGrant(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "viewer", "scope")
	view = ApplyPagePermissions(view, []RolePagePermission{
		{Page: PageWork, View: true}, {Page: PagePeople, View: true}, {Page: PagePerson, View: false},
	})
	view.People = []Person{{ID: "worker", Name: "Visible Worker"}}
	view.Work = []WorkItem{{ID: "waiting", PersonRef: "worker", Person: "Visible Worker", Status: "Waiting on approval"}}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "<strong>Visible Worker</strong>") || strings.Contains(markup, "/workspace/app/person?person=worker") {
		t.Fatalf("recent person linked to denied profile: %s", markup)
	}
}

func TestTodo_UXAUDIT_015_I18N(t *testing.T) {
	for locale, want := range map[string]struct{ quick, empty string }{
		"en-US": {"Quick links", "No work in progress"},
		"de-DE": {"Schnellzugriffe", "Keine laufenden Vorgänge"},
		"ar":    {"روابط سريعة", "لا يوجد عمل قيد التنفيذ"},
	} {
		t.Run(locale, func(t *testing.T) {
			view := NewView(PageHome, "HarborCare", "principal", "scope")
			view = ApplyLocale(view, ResolveProductLocale(locale))
			view = ApplyPagePermissions(view, []RolePagePermission{{Page: PageWork, View: true}, {Page: PagePeople, View: true}})
			markup, err := ui.RenderToString(homePage(view))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, want.quick) || !strings.Contains(markup, want.empty) || strings.Contains(markup, "No recent people") && locale != "en-US" {
				t.Fatalf("Home did not use localized continuity and quick-link copy: %s", markup)
			}
		})
	}
}

func TestTodo_UXAUDIT_015_I18N_TrackedClosedStatus(t *testing.T) {
	for locale, closed := range map[string]string{"en-US": "complete", "de-DE": "abgeschlossen", "ar": "مكتمل"} {
		props := TrackedRequestsProps{
			I18nProps: I18nProps{Locale: ResolveProductLocale(locale)}, Title: "Tracked",
			Items: []TrackedRequestProps{{ID: "closed", Title: "Request", Status: "Recorded", Open: false}},
		}
		markup, err := ui.RenderToString(ui.CreateElement(TrackedRequests, props))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, "Recorded · "+closed) {
			t.Fatalf("tracked closed status not localized for %s: %s", locale, markup)
		}
	}
}

func TestTodo_UXAUDIT_015_I18N_ArabicAttention(t *testing.T) {
	view := NewView(PageHome, "HarborCare", "Thomas", "scope")
	view.Viewer = ViewerProfile{PersonID: "viewer", Name: "Thomas"}
	view = ApplyLocale(view, ResolveProductLocale("ar"))
	view = ApplyPagePermissions(view, []RolePagePermission{{Page: PageWork, View: true}})
	view.Work = []WorkItem{{ID: "approval", PersonRef: "worker", AssigneeRef: "viewer", Title: "Promotion", Status: "Awaiting approval", NextStep: "approval_decision", ViewerResponsibility: "ACTION_REQUIRED", ViewerMembership: "ASSIGNEE"}}
	markup, err := ui.RenderToString(homePage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"صباح الخير، Thomas.", "ما يحتاج إلى اهتمامك", view.Locale.Text("work.promotion_journeys"), "عنصر واحد", "عرض عملي"} {
		if want == "صباح الخير، Thomas." {
			if view.Title != want {
				t.Fatalf("Arabic Home greeting = %q, want %q", view.Title, want)
			}
			continue
		}
		if !strings.Contains(markup, want) {
			t.Fatalf("Arabic attention missing %q: %s", want, markup)
		}
	}
	for _, fallback := range []string{"Open work", "1 item", "Your authorized work", "View My Work"} {
		if strings.Contains(markup, fallback) {
			t.Fatalf("Arabic attention still contains English fallback %q: %s", fallback, markup)
		}
	}
}
