package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_017(t *testing.T) {
	view := testView(PageHome)
	pending := PendingWork(view)
	if got := len(pending); got != 1 {
		t.Fatalf("PendingWork() = %d, want one viewer action", got)
	}

	for _, page := range []PageID{PageHome, PageWork, PageInsights} {
		pageView := view
		pageView.Page = page
		doc, err := Render(pageView)
		if err != nil {
			t.Fatalf("Render(%s): %v", page, err)
		}
		var want string
		switch page {
		case PageHome:
			want = "<dt>" + view.Locale.Text("home.needs_action") + "</dt><dd>"
		case PageWork:
			want = view.Locale.Plural("work.item_count", 1)
		case PageInsights:
			want = "<dt>" + view.Locale.Text("insights.needs_attention") + "</dt><dd>1</dd>"
		}
		if !strings.Contains(doc, want) {
			t.Fatalf("Render(%s) omitted shared pending count %q: %s", page, want, doc)
		}
		// UXBLIND-RR: the link holds only the number; its label is the paired <dt>.
		if page == PageHome && !strings.Contains(doc, `href="/workspace/app/work">1</a>`) {
			t.Fatalf("Render(%s) did not link the shared pending count to My Work", page)
		}
	}
}

func TestTodo_UXBLIND_017_Browser(t *testing.T) {
	view := testView(PageWork)
	view.SelectedWork = "intent-1"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `data-work-list-detail="true"`) {
		t.Fatal("My Work did not render its list/detail surface")
	}
	if !strings.Contains(doc, view.Locale.Plural("work.item_count", 1)) {
		t.Fatalf("My Work did not expose the shared pending count: %s", doc)
	}
}

func TestTodo_UXBLIND_017_Property(t *testing.T) {
	base := testView(PageHome)
	for _, pendingCount := range []int{0, 1, 2} {
		view := base
		view.Work = append([]WorkItem(nil), base.Work...)
		for index := 0; index < pendingCount; index++ {
			view.Work[index].Terminal = false
			view.Work[index].ViewerResponsibility = "ACTION_REQUIRED"
			view.Work[index].ViewerRelationships = []string{"ASSIGNEE"}
		}
		if pendingCount == 0 {
			view.Work[0].Terminal = true
			view.Work[0].ViewerResponsibility = "CLOSED"
		}
		if got := len(PendingWork(view)); got != pendingCount {
			t.Fatalf("PendingWork() for state %d = %d", pendingCount, got)
		}
		for _, page := range []PageID{PageHome, PageWork, PageInsights} {
			pageView := view
			pageView.Page = page
			if got := len(PendingWork(pageView)); got != pendingCount {
				t.Fatalf("PendingWork(%s) for state %d = %d", page, pendingCount, got)
			}
		}
	}
}

func TestTodo_UXBLIND_018(t *testing.T) {
	view := testView(PageHome)
	view.Viewer.Name = "Rafael Torres"
	view.People[0].PreferredName = "Rafa"
	if got := preferredViewerFirstName(view); got != "Rafa" {
		t.Fatalf("preferredViewerFirstName() = %q, want Rafa", got)
	}
	for _, test := range []struct {
		hour int
		want string
	}{
		{hour: 9, want: "Good morning, Rafa."},
		{hour: 13, want: "Good afternoon, Rafa."},
		{hour: 19, want: "Good evening, Rafa."},
	} {
		at := time.Date(2026, time.September, 28, test.hour, 47, 0, 0, time.FixedZone("viewer", -4*60*60))
		if got := homeGreetingAt(view.Locale, "Rafa", at); got != test.want {
			t.Errorf("homeGreetingAt(%d) = %q, want %q", test.hour, got, test.want)
		}
	}
}

func TestTodo_UXBLIND_018_Browser(t *testing.T) {
	view := testView(PageHome)
	view.Viewer.Name = "Rafael Torres"
	view.People[0].PreferredName = "Rafa"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Rafa") {
		t.Fatalf("Home did not greet the preferred first name: %s", doc)
	}
}

func TestTodo_UXBLIND_019(t *testing.T) {
	view := testView(PageWork)
	item := view.Work[0]
	item.WaitingOn = "proposer"
	item.ViewerRelationships = []string{"INITIATOR", "ASSIGNEE"}
	item.WorkSummary = true
	item.ViewerMembership = "ASSIGNEE"
	item.WorkDue = "2026-09-30"
	item.PermittedActions = []string{"decide_approval"}
	item.CurrentBase = testMoney("34.50", "USD")
	item.ProposedBase = testMoney("40.00", "USD")
	item.CurrentPayBasis = "HOURLY_RATE"
	item.ProposedPayBasis = "HOURLY_RATE"
	props := workPreviewProps(view, item)
	markup, err := ui.RenderToString(WorkPreview(props))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, view.Locale.Text("work.waiting_on.you")) {
		t.Fatalf("detail panel did not address the viewer: %s", markup)
	}
	for _, label := range []string{
		view.Locale.Text("work.assignment_label"),
		view.Locale.Text("work.due_label"),
		view.Locale.Text("work.next_action_label"),
	} {
		if got := strings.Count(markup, label); got != 1 {
			t.Errorf("detail label %q appears %d times, want once", label, got)
		}
	}
	if strings.Contains(markup, "Current work item") || !strings.Contains(markup, view.Locale.Text("work.action.decide_approval")) {
		t.Fatalf("detail panel retained stale copy or omitted the real action: %s", markup)
	}
	for _, want := range []string{
		view.Locale.FormatMoneyWithUnit("34.50", "USD", "hourly_rate", 2),
		view.Locale.FormatMoneyWithUnit("40.00", "USD", "hourly_rate", 2),
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("detail panel did not format hourly pay as %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "per year") {
		t.Fatalf("detail panel mislabeled hourly pay as annual: %s", markup)
	}
}

func TestTodo_UXBLIND_019_Browser(t *testing.T) {
	view := testView(PageWork)
	view.SelectedWork = "intent-1"
	view.Work[0].PermittedActions = []string{"decide_approval"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, view.Locale.Text("work.action.decide_approval")) {
		t.Fatalf("browser-rendered detail omitted the decision action: %s", doc)
	}
}

func TestTodo_UXBLIND_036(t *testing.T) {
	view := testView(PageInsights)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		view.Locale.Text("insights.headcount_by_unit"),
		view.Locale.Text("insights.headcount_by_location"),
		view.Locale.Text("insights.promotion_throughput"),
		view.Locale.Text("insights.promotion_cycle_time"),
	} {
		if !strings.Contains(doc, text) {
			t.Errorf("Insights omitted %q", text)
		}
	}
	if strings.Contains(doc, "Broader workforce reporting is not available yet") {
		t.Fatal("Insights retained the obsolete workforce-unavailable claim")
	}
}

func TestTodo_UXBLIND_036_Browser(t *testing.T) {
	view := testView(PageInsights)
	markup, err := ui.RenderToString(BuildPageContent(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "insights-workforce") {
		t.Fatalf("Insights workforce panel was not rendered: %s", markup)
	}
}
