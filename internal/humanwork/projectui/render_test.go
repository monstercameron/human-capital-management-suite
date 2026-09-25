package projectui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderString(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return markup
}

func TestTodo_PM_030(t *testing.T) {
	model := Model{
		Title:   "Release plan",
		Columns: []Column{{ID: "active", Label: "In progress", Statuses: []Status{{ID: "doing", Label: "Doing"}, {ID: "review", Label: "Review"}}}},
		Lanes:   []Lane{{ID: "lane-eng", Label: "Engineering", Count: 1, MayEdit: true}, {ID: "lane-fin", Label: "Finance", Count: 0, MayEdit: false}},
		Cards:   []Card{{ID: "task-1", Title: "Prepare rollout", StatusID: "doing", StatusOptions: []Status{{ID: "doing", Label: "Doing"}, {ID: "review", Label: "Review"}}, LaneID: "lane-eng", CanMoveStatus: true, CanMoveLane: true, Pending: true}},
		Page:    Page{Number: 2, Total: 120, HasPrevious: true, PreviousHref: "/board?page=1", HasNext: true, NextHref: "/board?page=3", MoreInLane: true},
	}
	markup := renderString(t, Board(model))
	for _, want := range []string{"<section", "aria-label=\"Release plan\"", "In progress", `<span class="projectui-lane-label">Engineering</span><span class="projectui-count"><span aria-hidden="true">1</span>`, "Prepare rollout", "role=\"status\"", "Saving change…", "Next page", "More authorized tasks may be in a lane", "data-projectui-action=\"move-status\""} {
		if !strings.Contains(markup, want) {
			t.Errorf("board markup missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "<main") {
		t.Fatalf("project component nested a main landmark: %s", markup)
	}
	if strings.Contains(markup, "lane-fin\"") && strings.Contains(markup, "value=\"lane-fin\"") {
		t.Errorf("read-only lane became a keyboard move target: %s", markup)
	}
}

func TestTodo_PM_030_Accessibility(t *testing.T) {
	markup := renderString(t, Board(Model{
		Title:   "Board",
		Columns: []Column{{ID: "todo", Label: "To do", Statuses: []Status{{ID: "open", Label: "Open"}}}},
		Lanes:   []Lane{{ID: "unset", Label: "Unassigned", MayEdit: true}},
		Cards:   []Card{{ID: "c1", Title: "Task", StatusID: "open", StatusOptions: []Status{{ID: "open", Label: "Open"}}, LaneID: "unset", CanMoveStatus: true, CanMoveLane: true, Conflict: "The task changed."}},
	}))
	for _, want := range []string{"<h1", "<h2", "<h3", "role=\"menuitemradio\"", "aria-checked=\"true\"", "aria-label=\"Status for Task\"", "aria-describedby=\"projectui-card-0-conflict\"", "role=\"alert\"", "tabIndex=\"-1\"", "data-projectui-action=\"move-lane\""} {
		if !strings.Contains(markup, want) {
			t.Errorf("accessible board markup missing %q: %s", want, markup)
		}
	}
}

func TestMoveControlsCarryOptimisticConcurrencyRevisions(t *testing.T) {
	markup := renderString(t, Board(Model{
		Title: "Board", WorkflowRevision: 8,
		Columns: []Column{{ID: "todo", Label: "To do", Statuses: []Status{{ID: "open", Label: "Open"}}}},
		Cards:   []Card{{ID: "task-42", TaskRevision: 13, WorkflowRevision: 8, Title: "Task", ColumnID: "todo", StatusID: "open", StatusOptions: []Status{{ID: "open", Label: "Open"}}, CanMoveStatus: true}},
	}))
	for _, want := range []string{`data-task-id="task-42"`, `data-task-revision="13"`, `data-workflow-revision="8"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("move markup missing concurrency field %q: %s", want, markup)
		}
	}
}

func TestTodo_PM_030_ConflictFallbackIsLocalized(t *testing.T) {
	markup := renderString(t, List(Model{
		Title: "Board",
		Copy:  Copy{Conflict: "Änderung abgelehnt."},
		Cards: []Card{{Title: "Task", ConflictState: true, CanMoveStatus: true, StatusOptions: []Status{{ID: "open", Label: "Open"}}}},
	}))
	if !strings.Contains(markup, "Änderung abgelehnt.") || !strings.Contains(markup, "data-focus-restore=\"projectui-card-0-status\"") {
		t.Fatalf("conflict fallback or focus target missing: %s", markup)
	}
}

func TestTodo_PM_030_BoundedPage(t *testing.T) {
	cards := make([]Card, pageCardLimit+37)
	for i := range cards {
		cards[i] = Card{ID: "id", Title: "card"}
	}
	markup := renderString(t, List(Model{Title: "Tasks", Cards: cards}))
	if got := strings.Count(markup, "class=\"projectui-card\""); got != pageCardLimit {
		t.Fatalf("rendered %d cards, want limit %d", got, pageCardLimit)
	}
}

func TestTodo_PM_030_EscapesPresentationText(t *testing.T) {
	markup := renderString(t, Board(Model{Title: `<img src=x onerror="run()">`, Columns: []Column{{ID: "c", Label: `A & B`, Statuses: []Status{{ID: "s", Label: `Done <script>`}}}}, Cards: []Card{{ID: "id", Title: `<script>alert(1)</script>`, StatusID: "s"}}}))
	if strings.Contains(markup, "<script>") || strings.Contains(markup, "<img") || !strings.Contains(markup, "&lt;script&gt;") {
		t.Fatalf("unescaped content in markup: %s", markup)
	}
}

func TestTodo_PM_031(t *testing.T) {
	markup := renderString(t, TaskDetail(DetailModel{
		Title: "Task detail", Status: "Active", Assignee: "A. Owner", DueDate: "2026-10-03",
		Fields:   []Fact{{Label: "Region", Value: "West"}},
		Comments: []Comment{{Author: "A. Owner", Time: "Yesterday", Body: "Reviewed."}},
		Activity: []Activity{{Label: "Moved to Active", Time: "Today"}},
		Links: []Reference{
			{Kind: "Chat", State: ReferenceReady, Title: "Authorized conversation", Href: "/chat/1"},
			{Kind: "Docs", State: ReferenceRestricted, Title: "Secret title must not appear", Href: "/docs/private"},
			{Kind: "Docs", State: ReferenceReady, Title: "Bad URL must not link", Href: "javascript:alert(1)"},
		},
		CommentsMoreHref: "/task/1/comments?page=2", ActivityMoreHref: "/task/1/activity?page=2",
	}))
	for _, want := range []string{"Task detail", "A. Owner", "West", "Reviewed.", "Moved to Active", "Authorized conversation", "Access restricted", "More comments", "More activity"} {
		if !strings.Contains(markup, want) {
			t.Errorf("detail markup missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Secret title must not appear") || strings.Contains(markup, "href=\"javascript:") || strings.Contains(markup, "Bad URL must not link</a>") {
		t.Fatalf("restricted or unsafe reference disclosed: %s", markup)
	}
}

func TestTodo_PM_031_BoundedAndLocalizedDetails(t *testing.T) {
	comments := make([]Comment, detailEntryLimit+8)
	for i := range comments {
		comments[i] = Comment{Body: "comment-entry"}
	}
	markup := renderString(t, TaskDetail(DetailModel{Comments: comments, Copy: Copy{
		Comments: "Kommentare", NoComments: "Keine Kommentare", TaskDetails: "Aufgabendetails",
	}}))
	if got := strings.Count(markup, "comment-entry"); got != detailEntryLimit {
		t.Fatalf("rendered %d comments, want limit %d", got, detailEntryLimit)
	}
	for _, want := range []string{"Aufgabendetails", "Kommentare"} {
		if !strings.Contains(markup, want) {
			t.Errorf("localized detail copy missing %q: %s", want, markup)
		}
	}
}

func TestTodo_PM_033_ResponsiveStylesAndKeyboardControls(t *testing.T) {
	styles := Styles()
	for _, want := range []string{"max-width:40rem", "focus-visible", "prefers-reduced-motion", "grid-template-columns"} {
		if !strings.Contains(styles, want) {
			t.Errorf("responsive/accessibility styles missing %q", want)
		}
	}
	markup := renderString(t, List(Model{Columns: []Column{{Statuses: []Status{{ID: "open", Label: "Open"}}}}, Lanes: []Lane{{ID: "unassigned", Label: "Unassigned", MayEdit: true}}, Cards: []Card{{Title: "Keyboard task", StatusOptions: []Status{{ID: "open", Label: "Open"}}, CanMoveStatus: true, CanMoveLane: true}}}))
	if got := strings.Count(markup, "<select"); got != 2 {
		t.Fatalf("keyboard operation has %d native select controls, want status and lane controls", got)
	}
}

func TestSwimlanesCollapseAndCardsCarryDragData(t *testing.T) {
	markup := renderString(t, Board(Model{
		Title: "Board", ProjectID: "p-1", ViewID: "default", LaneKind: LaneKindPriority,
		Columns: []Column{{ID: "todo", Label: "To do", Statuses: []Status{{ID: "todo", Label: "To do"}}}, {ID: "doing", Label: "Doing", Statuses: []Status{{ID: "doing", Label: "Doing"}}}},
		Lanes:   []Lane{{ID: "TASK_PRIORITY_HIGH", Label: "High", Count: 1, MayEdit: true, Collapsed: true}, {ID: "TASK_PRIORITY_LOW", Label: "Low", MayEdit: true}},
		Cards:   []Card{{ID: "t-1", TaskRevision: 3, WorkflowRevision: 2, Title: "Task", StatusID: "todo", LaneID: "TASK_PRIORITY_HIGH", StatusOptions: []Status{{ID: "todo", Label: "To do"}, {ID: "doing", Label: "Doing"}}, CanMoveStatus: true, CanMoveLane: true}},
	}))
	for _, want := range []string{
		`aria-expanded="false"`, `data-collapsed="true"`, `data-projectui-action="toggle-lane"`, `data-projectui-action="collapse-lanes"`, `data-label-expand="Expand all"`,
		`data-lane-store="hcm.projectui.lanes.p-1.default"`, `data-lane-kind="priority"`,
		`draggable="true"`, `data-move-targets="todo doing"`, `data-drop-status="doing"`, `data-drop-lane="TASK_PRIORITY_LOW"`,
		`data-projectui-action="move-status"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("board markup missing %q", want)
		}
	}
}

func TestTaskModalAndPageExposeEditableFields(t *testing.T) {
	detail := DetailModel{
		Title: "Train partners", Status: "To do", StatusID: "todo", ProjectID: "p-1", TaskID: "a1b2c3d4-e5f6", TaskRevision: 4, WorkflowRevision: 2,
		StatusOptions: []Status{{ID: "todo", Label: "To do"}, {ID: "doing", Label: "Doing"}}, CanEdit: true, CanMoveStatus: true, CanComment: true,
		Members:    []Person{{ID: "hc-1", Name: "Mei Chen"}},
		PriorityID: "TASK_PRIORITY_HIGH", PriorityOptions: []Status{{ID: "TASK_PRIORITY_HIGH", Label: "High"}},
		Comments:     []Comment{{ID: "c-1", Revision: 1, Author: "Mei Chen", Body: "Done", Own: true}},
		PageHref:     "/workspace/app/project?project=p-1&task=a1b2c3d4-e5f6&view=task",
		CommentTotal: 1,
	}
	modal := renderString(t, TaskModalContent(detail, "title-id"))
	for _, want := range []string{"T-A1B2C3D4", `data-projectui-action="move-status"`, `data-projectui-action="set-assignee"`, `data-projectui-action="set-priority"`, `data-projectui-close="true"`, "view=task"} {
		if !strings.Contains(modal, want) {
			t.Errorf("modal missing %q", want)
		}
	}
	page := renderString(t, TaskPage(detail))
	for _, want := range []string{`data-projectui-action="edit-title"`, `data-projectui-action="edit-description"`, `data-projectui-action="add-comment"`, `data-projectui-action="edit-comment"`, `data-projectui-action="delete-comment"`, `type="date"`, `data-task-revision="4"`} {
		if !strings.Contains(page, want) {
			t.Errorf("task page missing %q", want)
		}
	}
}

func TestListSortsByColumnAndLinksHeaders(t *testing.T) {
	markup := renderString(t, List(Model{
		Title: "Tasks", ListSort: "-due",
		SortHrefs: map[string]string{"task": "/p?sort=task", "due": "/p?sort=due"},
		Cards: []Card{
			{ID: "a", Title: "Early", DueDate: "2026-09-01"},
			{ID: "b", Title: "Undated"},
			{ID: "c", Title: "Late", DueDate: "2026-10-01"},
		},
	}))
	late, early, undated := strings.Index(markup, "Late"), strings.Index(markup, "Early"), strings.Index(markup, "Undated")
	if !(late < early && early < undated) {
		t.Fatalf("descending due order wrong (late %d, early %d, undated %d)", late, early, undated)
	}
	for _, want := range []string{`href="/p?sort=due"`, `data-active="true"`, "sorted descending"} {
		if !strings.Contains(markup, want) {
			t.Errorf("sorted list missing %q", want)
		}
	}
}

func TestDoneStatusExplainsTheLock(t *testing.T) {
	markup := renderString(t, List(Model{
		Title:   "Tasks",
		Columns: []Column{{ID: "done", Label: "Done", Statuses: []Status{{ID: "done", Label: "Done"}}}},
		Cards:   []Card{{ID: "t", Title: "Closed", StatusID: "done", StatusLabel: "Done"}},
	}))
	for _, want := range []string{`data-locked="true"`, `aria-describedby="projectui-card-0-lock"`, `id="projectui-card-0-lock"`, "can&#39;t be reopened"} {
		if !strings.Contains(markup, want) {
			t.Errorf("locked status missing %q: %s", want, markup)
		}
	}
}

func TestFilterBarHidesCardsAndCountsWhatMatches(t *testing.T) {
	markup := renderString(t, Board(Model{
		Title: "Board", ProjectID: "p-1",
		Columns: []Column{{ID: "todo", Label: "To do", Statuses: []Status{{ID: "todo", Label: "To do"}}}, {ID: "done", Label: "Done", Statuses: []Status{{ID: "done", Label: "Done"}}}},
		Cards: []Card{
			{ID: "a", Title: "Visible task", StatusID: "todo"},
			{ID: "b", Title: "Hidden task", StatusID: "todo", FilteredOut: true},
			{ID: "c", Title: "Hidden done", StatusID: "done", FilteredOut: true},
		},
		Filters: &FilterBar{ActiveCount: 1, ClearHref: "/p?project=p-1", SearchHref: "/p?project=p-1",
			Priorities: []FilterChoice{{ID: "high", Label: "High", Href: "/p?filter=priority%3Ahigh", Active: true}}},
	}))
	if strings.Contains(markup, "Hidden task") || !strings.Contains(markup, "Visible task") {
		t.Fatalf("filtered card still rendered: %s", markup)
	}
	for _, want := range []string{"1 of 2", "0 of 1", "No matching tasks", `aria-pressed="true"`, "Clear filters", `id="projectui-board-search"`, `data-projectui-action="toggle-filters"`, `id="projectui-shortcuts"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("filtered board missing %q", want)
		}
	}
}

func TestEmptyBoardShowsFirstRun(t *testing.T) {
	markup := renderString(t, Board(Model{Title: "Board", ProjectID: "p-1", Filters: &FilterBar{}, Columns: []Column{{ID: "todo", Label: "To do", Statuses: []Status{{ID: "todo", Label: "To do"}}}}}))
	if !strings.Contains(markup, "projectui-firstrun") || !strings.Contains(markup, `data-projectui-action="open-create-task"`) || strings.Contains(markup, "projectui-columns") {
		t.Fatalf("empty board did not render the first-run panel: %s", markup)
	}
}
