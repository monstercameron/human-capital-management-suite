package projectui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderPM033(t *testing.T, model Model) string {
	t.Helper()
	markup, err := ui.RenderToString(Board(model))
	if err != nil {
		t.Fatalf("render board: %v", err)
	}
	return markup
}

func pm033Model(copy Copy) Model {
	return Model{
		Title:            "Project board",
		WorkflowRevision: 7,
		Columns:          []Column{{ID: "todo", Label: "To do", Statuses: []Status{{ID: "open", Label: "Open"}}}, {ID: "done", Label: "Done", Statuses: []Status{{ID: "closed", Label: "Closed"}}}},
		Lanes:            []Lane{{ID: "unassigned", Label: copy.Unassigned, Count: 1, MayEdit: true}, {ID: "restricted", Label: "Restricted", Count: 0, MayEdit: false}},
		Cards:            []Card{{ID: "task-1", TaskRevision: 12, WorkflowRevision: 7, Title: "Prepare launch", StatusID: "open", StatusOptions: []Status{{ID: "open", Label: "Open"}}, LaneID: "unassigned", CanMoveStatus: true, CanMoveLane: true, DetailHref: "/projects/p1/tasks/task-1"}},
		Copy:             copy,
	}
}

func TestTodo_PM_033(t *testing.T) {
	markup := renderPM033(t, pm033Model(Copy{
		Status: "Status", Lane: "Lane", StatusFor: "Status for ", LaneFor: "Lane for ",
		MoveTo: "Move to", MoreActions: "More actions", OpenTask: "Open task", Unassigned: "Unassigned",
	}))
	for _, want := range []string{
		`data-projectui-action="move-status"`, `data-projectui-action="move-lane"`,
		`role="menuitemradio"`, `data-task-revision="12"`, `data-workflow-revision="7"`,
		`type="button"`, `Open task`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("keyboard/mobile board proof missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `value="restricted"`) {
		t.Fatalf("read-only lane became a move target: %s", markup)
	}
}

func TestTodo_PM_033_Browser(t *testing.T) {
	markup := renderPM033(t, pm033Model(Copy{
		Status: "Status", Lane: "Lane", StatusFor: "Status for ", LaneFor: "Lane for ",
		MoveTo: "Move to", MoreActions: "More actions", OpenTask: "Open task", Unassigned: "Unassigned",
	}))
	if strings.Contains(markup, "<main") {
		t.Fatal("project board introduced a nested main landmark")
	}
	if !strings.Contains(Styles(), "@media (max-width:40rem)") || !strings.Contains(Styles(), "min-block-size:2.75rem") {
		t.Fatal("mobile board controls do not have the narrow-screen touch target contract")
	}
}

func TestTodo_PM_033_Accessibility(t *testing.T) {
	markup := renderPM033(t, pm033Model(Copy{
		Status: "Status", Lane: "Lane", StatusFor: "Status for ", LaneFor: "Lane for ",
		MoveTo: "Move to", MoreActions: "More actions", OpenTask: "Open task", Unassigned: "Unassigned",
	}))
	for _, want := range []string{`aria-label="Status for Prepare launch"`, `aria-label="Lane for Prepare launch"`, `aria-checked="true"`, `role="group"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("assistive technology contract missing %q: %s", want, markup)
		}
	}
}

func TestTodo_PM_033_Conformance(t *testing.T) {
	translations := []Copy{
		{Status: "Status", Lane: "Spur", StatusFor: "Status für ", LaneFor: "Spur für ", MoveTo: "Verschieben nach", MoreActions: "Weitere Aktionen", OpenTask: "Aufgabe öffnen", Unassigned: "Nicht zugewiesen"},
		{Status: "الحالة", Lane: "المسار", StatusFor: "الحالة لـ ", LaneFor: "المسار لـ ", MoveTo: "نقل إلى", MoreActions: "إجراءات أخرى", OpenTask: "فتح المهمة", Unassigned: "غير معيّن"},
	}
	for _, copy := range translations {
		markup := renderPM033(t, pm033Model(copy))
		for _, want := range []string{copy.MoveTo, copy.MoreActions, copy.OpenTask, copy.Unassigned} {
			if !strings.Contains(markup, want) {
				t.Fatalf("localized board omitted %q: %s", want, markup)
			}
		}
	}
	styles := Styles()
	for _, want := range []string{"prefers-reduced-motion:reduce", `[dir="rtl"]`, "inset-inline-start", "overscroll-behavior-inline"} {
		if !strings.Contains(styles, want) {
			t.Fatalf("board style conformance missing %q", want)
		}
	}
}

func TestTodo_PM_034(t *testing.T) {
	cards := make([]Card, 100_000)
	for i := range cards {
		cards[i] = Card{ID: "task-" + strconv.Itoa(i), Title: "Task", ColumnID: "todo"}
	}
	markup := renderPM033(t, Model{Title: "Pilot board", Columns: []Column{{ID: "todo", Label: "To do"}}, Cards: cards})
	if got := strings.Count(markup, `class="projectui-card"`); got != pageCardLimit {
		t.Fatalf("pilot board rendered %d cards, want bounded page size %d", got, pageCardLimit)
	}
}

func BenchmarkTodo_PM_034(b *testing.B) {
	cards := make([]Card, pageCardLimit)
	for i := range cards {
		cards[i] = Card{ID: "task", Title: "Task", ColumnID: "todo"}
	}
	model := Model{Title: "Pilot board", Columns: []Column{{ID: "todo", Label: "To do"}}, Cards: cards}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ui.RenderToString(Board(model)); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTodo_PM_034_Fault(t *testing.T) {
	markup := renderPM033(t, Model{Title: "Pilot board", Columns: []Column{{ID: "todo", Label: "To do", Statuses: []Status{{ID: "todo", Label: "To do"}}}}, Cards: []Card{{ID: "pending", Title: "Pending", StatusID: "todo", Pending: true, CanMoveStatus: true, StatusOptions: []Status{{ID: "todo", Label: "To do"}}}, {ID: "conflict", Title: "Conflict", StatusID: "todo", ConflictState: true, Conflict: "Revision conflict", CanMoveStatus: true, StatusOptions: []Status{{ID: "todo", Label: "To do"}}}}})
	for _, want := range []string{`aria-busy="true"`, `role="alert"`, `Revision conflict`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workflow fault state missing %q: %s", want, markup)
		}
	}
}

func TestTodo_PM_034_Conformance(t *testing.T) {
	styles := Styles()
	for _, want := range []string{"max-block-size", "overflow-x:auto", "prefers-reduced-motion:reduce", "scroll-snap-type:x proximity"} {
		if !strings.Contains(styles, want) {
			t.Fatalf("pilot board conformance missing %q", want)
		}
	}
}
