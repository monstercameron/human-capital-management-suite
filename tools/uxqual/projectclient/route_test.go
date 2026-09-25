package projectclient

import (
	"strings"
	"testing"
	"time"
)

func TestProjectRouteRoundTrip(t *testing.T) {
	state, err := ParseState(ProjectPath, "project=p_7f2&board_view=view-3&task=tsk-19&view=list&lane=doing&filter=due%3Asoon%3Bpriority%3Aurgent%2Chigh%3Bbogus%3Ax&q=launch+plan&cursor=sign-abc_2")
	if err != nil {
		t.Fatal(err)
	}
	if state.Route != RouteProject || state.ProjectID != "p_7f2" || state.BoardViewID != "view-3" || state.TaskID != "tsk-19" || state.View != ViewList || state.Lane != "doing" || state.Filter != "priority:high,urgent;due:soon" || state.Query != "launch plan" || state.Cursor != "sign-abc_2" {
		t.Fatalf("parsed state = %+v", state)
	}
	want := ProjectPath + "?board_view=view-3&cursor=sign-abc_2&filter=priority%3Ahigh%2Curgent%3Bdue%3Asoon&lane=doing&project=p_7f2&q=launch+plan&task=tsk-19&view=list"
	if got := CanonicalHref(state); got != want {
		t.Fatalf("canonical href = %q, want %q", got, want)
	}
}

func TestProjectRouteDefaultsBoardAndSupportsDeepLink(t *testing.T) {
	state, err := ParseState(ProjectPath, "task=tsk-19&project=p_7f2")
	if err != nil {
		t.Fatal(err)
	}
	if state.View != ViewBoard || state.TaskID != "tsk-19" {
		t.Fatalf("parsed state = %+v", state)
	}
	if got, want := CanonicalHref(state), ProjectPath+"?project=p_7f2&task=tsk-19"; got != want {
		t.Fatalf("canonical href = %q, want %q", got, want)
	}
}

func TestProjectHomeDropsSelectors(t *testing.T) {
	state, err := ParseState(ProjectsPath, "project=private-id&task=secret&view=list&token=credential")
	if err != nil {
		t.Fatal(err)
	}
	if state.Route != RouteProjects || state.ProjectID != "" || state.TaskID != "" {
		t.Fatalf("home state retained selectors: %+v", state)
	}
	if got := CanonicalHref(state); got != ProjectsPath {
		t.Fatalf("canonical href = %q", got)
	}
}

func TestProjectRouteDropsUnknownKeysAndCanonicalizes(t *testing.T) {
	state, err := ParseState(ProjectPath, "z=secret&project=p-1&view=board&token=credential&lane=&q=%20launch%20")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := CanonicalHref(state), ProjectPath+"?project=p-1&q=launch"; got != want {
		t.Fatalf("canonical href = %q, want %q", got, want)
	}
}

func TestProjectRouteRejectsInvalidRecognizedState(t *testing.T) {
	cases := []struct{ path, query string }{
		{ProjectPath, "project=p-1&project=p-2"},
		{ProjectPath, "project=p-1&task=../other"},
		{ProjectPath, "project=p-1&view=grid"},
		{ProjectPath, "project=p-1&lane=bad%2Fvalue"},
		{ProjectPath, "project=p-1&board_view=bad%2Fvalue"},
		{ProjectPath, "task=t-1"},
		{ProjectsPath, strings.Repeat("x", maxRouteQueryBytes+1)},
		{ProjectPath, "project=%zz"},
		{"/workspace/app/projects/private-id", ""},
	}
	for _, tc := range cases {
		if _, err := ParseState(tc.path, tc.query); err == nil {
			t.Errorf("ParseState(%q, %q) succeeded", tc.path, tc.query)
		}
	}
}

func TestCanonicalHrefOmitsForgedValues(t *testing.T) {
	got := CanonicalHref(State{Route: RouteProject, ProjectID: "p-1", View: "grid", Lane: "../lane", Filter: "\nsecret", Query: "safe"})
	want := ProjectPath + "?project=p-1&q=safe"
	if got != want {
		t.Fatalf("canonical href = %q, want %q", got, want)
	}
}

func TestProjectTaskPageRouteRoundTrip(t *testing.T) {
	state, err := ParseState(ProjectPath, "view=task&task=tsk-19&project=p_7f2&board_view=default")
	if err != nil {
		t.Fatal(err)
	}
	if state.View != ViewTask || state.TaskID != "tsk-19" || state.BoardViewID != "default" {
		t.Fatalf("parsed state = %+v", state)
	}
	if got, want := CanonicalHref(state), ProjectPath+"?board_view=default&project=p_7f2&task=tsk-19&view=task"; got != want {
		t.Fatalf("canonical href = %q, want %q", got, want)
	}
	// The modal selector is the same task= on the board view: dropping the
	// task closes the modal and keeps the board.
	state.View, state.TaskID = ViewBoard, ""
	if got, want := CanonicalHref(state), ProjectPath+"?board_view=default&project=p_7f2"; got != want {
		t.Fatalf("board href = %q, want %q", got, want)
	}
}

func TestProjectTaskPageWithoutTaskIsTheBoard(t *testing.T) {
	state, err := ParseState(ProjectPath, "project=p-1&view=task")
	if err != nil {
		t.Fatal(err)
	}
	if state.View != ViewBoard {
		t.Fatalf("view = %q, want board", state.View)
	}
	if got, want := CanonicalHref(state), ProjectPath+"?project=p-1"; got != want {
		t.Fatalf("canonical href = %q, want %q", got, want)
	}
	if got := CanonicalHref(State{Route: RouteProject, ProjectID: "p-1", View: ViewTask}); got != ProjectPath+"?project=p-1" {
		t.Fatalf("forged task view without task = %q", got)
	}
}

func TestProjectRouteKeepsShellPresentationKeys(t *testing.T) {
	state, err := ParseState(ProjectPath, "project=p-1&task=t-2&view=task&locale=ar&nav=collapsed&token=secret")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := CanonicalHref(state), ProjectPath+"?locale=ar&nav=collapsed&project=p-1&task=t-2&view=task"; got != want {
		t.Fatalf("canonical href = %q, want %q", got, want)
	}
	home, err := ParseState(ProjectsPath, "locale=de-DE&project=x")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := CanonicalHref(home), ProjectsPath+"?locale=de-DE"; got != want {
		t.Fatalf("home href = %q, want %q", got, want)
	}
}

func TestProjectListSortRoundTripsOnlyInListView(t *testing.T) {
	state, err := ParseState(ProjectPath, "project=p-1&view=list&sort=-due")
	if err != nil {
		t.Fatal(err)
	}
	if state.Sort != "-due" {
		t.Fatalf("sort = %q, want -due", state.Sort)
	}
	if got, want := CanonicalHref(state), ProjectPath+"?project=p-1&sort=-due&view=list"; got != want {
		t.Fatalf("canonical href = %q, want %q", got, want)
	}
	state.View = ViewBoard
	if got := CanonicalHref(state); strings.Contains(got, "sort=") {
		t.Fatalf("board href kept the list sort: %q", got)
	}
	unknown, err := ParseState(ProjectPath, "project=p-1&view=list&sort=secret")
	if err != nil || unknown.Sort != "" {
		t.Fatalf("unknown sort = %q, %v; want dropped", unknown.Sort, err)
	}
}

func TestBoardFilterRoundTripsThroughTheAddress(t *testing.T) {
	filter := BoardFilter{}.ToggleAssignee("hc-005-mei-chen").ToggleAssignee(FilterUnassigned).TogglePriority("urgent").ToggleDueSoon()
	state := State{Route: RouteProject, ProjectID: "p-1", BoardViewID: "default", Filter: filter.String(), Query: "enroll"}
	href := CanonicalHref(state)
	query := href[strings.Index(href, "?")+1:]
	parsed, err := ParseState(ProjectPath, query)
	if err != nil {
		t.Fatal(err)
	}
	back := ParseBoardFilter(parsed.Filter)
	if !back.HasAssignee("hc-005-mei-chen") || !back.HasAssignee(FilterUnassigned) || !back.HasPriority("urgent") || !back.DueSoon || parsed.Query != "enroll" {
		t.Fatalf("filter lost in round trip: %q -> %+v (q=%q)", href, back, parsed.Query)
	}
	if got := CanonicalHref(parsed); got != href {
		t.Fatalf("canonical href changed on round trip: %q vs %q", got, href)
	}
	if off := filter.ToggleDueSoon().TogglePriority("urgent").ToggleAssignee("hc-005-mei-chen").ToggleAssignee(FilterUnassigned); !off.Empty() || off.String() != "" {
		t.Fatalf("toggling every value off left %q", off.String())
	}
	if forged := ParseBoardFilter("assignee:../x,ok-1;priority:critical;due:later"); forged.String() != "assignee:ok-1" {
		t.Fatalf("forged filter = %q", forged.String())
	}
}

func TestBoardFilterMatches(t *testing.T) {
	today := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	task := FilterTask{Title: "Carrier EDI 834 file test", AssigneeID: "", PriorityLevel: "high", DueDate: "2026-09-27"}
	cases := []struct {
		filter BoardFilter
		query  string
		want   bool
	}{
		{BoardFilter{}, "", true},
		{BoardFilter{Assignees: []string{FilterUnassigned}}, "", true},
		{BoardFilter{Assignees: []string{"someone"}}, "", false},
		{BoardFilter{Priorities: []string{"urgent", "high"}}, "", true},
		{BoardFilter{Priorities: []string{"low"}}, "", false},
		{BoardFilter{DueSoon: true}, "", true},
		{BoardFilter{}, "edi carrier", true},
		{BoardFilter{}, "payroll", false},
	}
	for _, c := range cases {
		if got := c.filter.Matches(task, c.query, today); got != c.want {
			t.Errorf("%q q=%q = %v, want %v", c.filter.String(), c.query, got, c.want)
		}
	}
	if (BoardFilter{DueSoon: true}).Matches(FilterTask{DueDate: "2026-10-30"}, "", today) {
		t.Error("a date a month out counted as due soon")
	}
}

func TestBoardShortcutIgnoresTyping(t *testing.T) {
	for _, tag := range []string{"INPUT", "textarea", "Select"} {
		if got := BoardShortcut(KeyPress{Key: "c", Target: KeyTarget{Tag: tag}}); got != "" {
			t.Errorf("c in %s = %q, want nothing", tag, got)
		}
	}
	if got := BoardShortcut(KeyPress{Key: "/", Target: KeyTarget{Tag: "DIV", Editable: true}}); got != "" {
		t.Errorf("/ in contenteditable = %q", got)
	}
	if got := BoardShortcut(KeyPress{Key: "j", Target: KeyTarget{Tag: "BUTTON", InOverlay: true}}); got != "" {
		t.Errorf("j inside a menu = %q", got)
	}
	if got := BoardShortcut(KeyPress{Key: "c", Ctrl: true, Target: KeyTarget{Tag: "BODY"}}); got != "" {
		t.Errorf("ctrl+c = %q", got)
	}
	want := map[string]string{"c": ShortcutNewTask, "/": ShortcutSearch, "f": ShortcutFilters, "j": ShortcutNext, "ArrowUp": ShortcutPrev, "?": ShortcutHelp, "x": ""}
	for key, action := range want {
		if got := BoardShortcut(KeyPress{Key: key, Target: KeyTarget{Tag: "A"}}); got != action {
			t.Errorf("%s = %q, want %q", key, got, action)
		}
	}
}

func TestTicketsTabRoundTripsFiltersSortAndPage(t *testing.T) {
	state, err := ParseState(ProjectsPath, "tab=tickets&filter=status%3Adone%3Bassignee%3Ahc-1%3Blabel%3AUrgent-Fix%3Bdue%3Aweek%3Bbogus%3Ax&q=benefits&sort=-updated&page=3&locale=de-DE&junk=1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Tab != TabTickets || state.Query != "benefits" || state.Sort != "-updated" || state.PageNumber != 3 {
		t.Fatalf("parsed state = %+v", state)
	}
	filter := ParseTicketFilter(state.Filter)
	if !filter.Has("status", "done") || !filter.Has("assignee", "hc-1") || !filter.Has("label", "urgent-fix") || filter.Due != DueThisWeek {
		t.Fatalf("filter = %+v", filter)
	}
	href := CanonicalHref(state)
	again, err := ParseState(ProjectsPath, href[strings.Index(href, "?")+1:])
	if err != nil || CanonicalHref(again) != href {
		t.Fatalf("round trip changed %q -> %q (%v)", href, CanonicalHref(again), err)
	}
	if strings.Contains(href, "junk") || !strings.Contains(href, "locale=de-DE") {
		t.Fatalf("canonical href = %q", href)
	}
	plain, _ := ParseState(ProjectsPath, "filter=status%3Adone&sort=title")
	if got := CanonicalHref(plain); got != ProjectsPath {
		t.Fatalf("projects tab kept ticket keys: %q", got)
	}
}

func TestTicketFilterMatches(t *testing.T) {
	today := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	ticket := Ticket{ProjectID: "p-1", Title: "Carrier file test", Key: "T-1234", Category: CategoryActive, PriorityLevel: "high", DueDate: "2026-09-20", Labels: []string{"EDI"}}
	for filter, want := range map[string]bool{
		"":                        true,
		"project:p-1":             true,
		"project:p-2":             false,
		"status:active":           true,
		"status:done":             false,
		"assignee:none":           true,
		"priority:high,urgent":    true,
		"due:overdue":             true,
		"due:week":                false,
		"label:edi":               true,
		"label:payroll":           false,
		"status:active;label:edi": true,
	} {
		if got := ParseTicketFilter(filter).Matches(ticket, "", today); got != want {
			t.Errorf("%q = %v, want %v", filter, got, want)
		}
	}
	if !(TicketFilter{}).Matches(ticket, "t-1234 carrier", today) || (TicketFilter{}).Matches(ticket, "payroll", today) {
		t.Error("search did not match key and title")
	}
}

func TestPageSizeRoundTripsOnTicketsAndListOnly(t *testing.T) {
	tickets, err := ParseState(ProjectsPath, "tab=tickets&page_size=50&page=2")
	if err != nil || tickets.PageSize != 50 || tickets.PageNumber != 2 {
		t.Fatalf("tickets paging = %+v, %v", tickets, err)
	}
	if got := CanonicalHref(tickets); !strings.Contains(got, "page_size=50") || !strings.Contains(got, "page=2") {
		t.Fatalf("tickets href = %q", got)
	}
	list, err := ParseState(ProjectPath, "project=p-1&view=list&page_size=10&page=3")
	if err != nil || list.PageSize != 10 || list.PageNumber != 3 {
		t.Fatalf("list paging = %+v, %v", list, err)
	}
	if got := CanonicalHref(list); got != ProjectPath+"?page=3&page_size=10&project=p-1&view=list" {
		t.Fatalf("list href = %q", got)
	}
	board, _ := ParseState(ProjectPath, "project=p-1&page_size=10")
	if got := CanonicalHref(board); strings.Contains(got, "page_size") {
		t.Fatalf("board kept a list page size: %q", got)
	}
	odd, _ := ParseState(ProjectsPath, "tab=tickets&page_size=7")
	if odd.PageSize != 0 {
		t.Fatalf("unknown page size kept: %d", odd.PageSize)
	}
}

func TestTicketFilterLinkedToWorkflow(t *testing.T) {
	filter := ParseTicketFilter("linked:workflow;status:active")
	if !filter.Workflow || filter.String() != "status:active;linked:workflow" {
		t.Fatalf("filter = %+v %q", filter, filter.String())
	}
	today := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	if filter.Matches(Ticket{Category: CategoryActive}, "", today) || !filter.Matches(Ticket{Category: CategoryActive, HasWorkflow: true}, "", today) {
		t.Fatal("linked filter did not narrow to linked tickets")
	}
	if filter.Toggle("linked", "workflow").Workflow {
		t.Fatal("toggle did not clear the linked filter")
	}
}
