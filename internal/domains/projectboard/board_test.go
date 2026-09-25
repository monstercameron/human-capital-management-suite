package projectboard

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type taskSource struct {
	page  []Task
	next  *Cursor
	query AuthorizedTaskQuery
	err   error
}

type accessFilteringSource struct {
	tasks      []Task
	restricted map[string]bool
}

func (s accessFilteringSource) ListAuthorizedTasks(_ context.Context, q AuthorizedTaskQuery) ([]Task, *Cursor, error) {
	visible := make([]Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		if !s.restricted[task.ID] {
			visible = append(visible, task)
		}
	}
	if len(visible) > q.Limit {
		return visible[:q.Limit], &Cursor{Token: "more"}, nil
	}
	return visible, nil, nil
}

func (s *taskSource) ListAuthorizedTasks(_ context.Context, q AuthorizedTaskQuery) ([]Task, *Cursor, error) {
	s.query = q
	if s.err != nil {
		return nil, nil, s.err
	}
	return append([]Task(nil), s.page...), s.next, nil
}

func baseView() BoardView {
	return BoardView{ID: "view-1", Name: "Team board", Version: 3, Audience: AudienceProject,
		Columns:  []Column{{ID: "todo", Label: "To do", StatusIDs: []string{"open"}}, {ID: "doing", Label: "Doing", StatusIDs: []string{"active"}}},
		Grouping: Grouping{Kind: GroupAssignee}, OrderBy: OrderTitle,
		CardFields: []string{CardTitle, CardAssignee}}
}

func TestTodo_PM_017_TitleOrderFoldIsASCIIStable(t *testing.T) {
	if got, want := FoldTitleForOrder("IİÉZ"), "iİÉz"; got != want {
		t.Fatalf("title ordering fold = %q, want %q", got, want)
	}
}

func TestTodo_PM_016(t *testing.T) {
	view := baseView()
	view.Filter = Filter{Priorities: []string{"high"}, EnumFields: map[string][]string{"region": {"west"}}}
	next := &Cursor{Token: "opaque-next"}
	source := &taskSource{page: []Task{
		{ID: "b", Title: "zeta", StatusID: "open", AssigneeID: "u1", AssigneeName: "Rae", Priority: "high", EnumFields: map[string]string{"region": "west"}},
		{ID: "a", Title: "Alpha", StatusID: "open", AssigneeID: "u1", AssigneeName: "Rae", Priority: "high", EnumFields: map[string]string{"region": "west"}},
		{ID: "c", Title: "Hidden by filter", StatusID: "open", AssigneeID: "u2", AssigneeName: "Lee", Priority: "low", EnumFields: map[string]string{"region": "west"}},
		{ID: "d", Title: "Unmapped", StatusID: "other", Priority: "high", EnumFields: map[string]string{"region": "west"}},
	}, next: next}
	cursor := &Cursor{Token: "cursor-in"}
	page, err := BuildPage(context.Background(), source, view, 25, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if source.query.Limit != 25 || source.query.Cursor != *cursor || source.query.ViewID != view.ID || source.query.ViewVersion != view.Version || source.query.OrderBy != view.OrderBy || source.query.Descending != view.Descending || len(source.query.StatusIDs) != 2 || source.query.Filter.Priorities[0] != "high" {
		t.Fatalf("source query = %+v", source.query)
	}
	if page.ViewID != "view-1" || page.Version != 3 || page.Next == nil || *page.Next != *next {
		t.Fatalf("page metadata = %+v", page)
	}
	if len(page.Columns) != 2 || page.Columns[0].Count != 2 || len(page.Columns[0].Lanes) != 1 {
		t.Fatalf("unexpected columns: %+v", page.Columns)
	}
	cards := page.Columns[0].Lanes[0].Cards
	if len(cards) != 2 || cards[0].Task.ID != "a" || cards[1].Task.ID != "b" {
		t.Fatalf("cards not deterministically ordered: %+v", cards)
	}
	if page.Columns[1].Count != 0 {
		t.Fatalf("empty configured column count = %d", page.Columns[1].Count)
	}
	if got := cards[0].Task; got.Title != "Alpha" || got.AssigneeName != "Rae" || got.Priority != "" || got.EnumFields != nil {
		t.Fatalf("card fields were not projected: %+v", got)
	}
}

func TestTodo_PM_016_Security(t *testing.T) {
	// This source models the authorization boundary: the restricted card has the
	// same status, assignee, priority and field value as the visible card, but it
	// never crosses that boundary and therefore cannot affect lanes or counts.
	authorized := []Task{{ID: "visible", StatusID: "open", AssigneeID: "u1", AssigneeName: "Rae", Priority: "high", EnumFields: map[string]string{"team": "blue"}}}
	restricted := Task{ID: "restricted", StatusID: "open", AssigneeID: "u1", AssigneeName: "Rae", Priority: "high", EnumFields: map[string]string{"team": "blue"}}
	source := accessFilteringSource{tasks: []Task{authorized[0], restricted}, restricted: map[string]bool{"restricted": true}}
	view := baseView()
	view.Grouping = Grouping{Kind: GroupEnum, FieldID: "team"}
	page, err := BuildPage(context.Background(), source, view, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Columns[0].Count != 1 {
		t.Fatalf("count included restricted task: %d", page.Columns[0].Count)
	}
	if got := page.Columns[0].Lanes[0].ID; got != "blue" {
		t.Fatalf("lane id = %q", got)
	}
	if got := page.Columns[0].Lanes[0].Cards[0].Task.ID; got != "visible" {
		t.Fatalf("card = %q", got)
	}
}

func TestTodo_PM_017_Security(t *testing.T) {
	source := &taskSource{}
	if _, err := BuildPage(context.Background(), source, baseView(), MaxPageSize+1, nil); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("large page error = %v", err)
	}
	source.page = make([]Task, 2)
	if _, err := BuildPage(context.Background(), source, baseView(), 1, nil); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("overfull source error = %v", err)
	}
}

func TestTodo_PM_016_Validation(t *testing.T) {
	view := baseView()
	view.Columns[1].StatusIDs = []string{"open"}
	if err := Validate(view); !errors.Is(err, ErrInvalidView) {
		t.Fatalf("duplicate mapping error = %v", err)
	}
	view = baseView()
	view.Grouping = Grouping{Kind: GroupEnum}
	if err := Validate(view); !errors.Is(err, ErrInvalidView) {
		t.Fatalf("missing field error = %v", err)
	}
}

func TestTodo_PM_030_LaneGrouping(t *testing.T) {
	task := Task{ID: "task", AssigneeID: "user", AssigneeName: "Ada", Priority: "urgent", TypeID: "feature", EnumFields: map[string]string{"team": "sales"}}
	cases := []struct {
		name     string
		grouping Grouping
		want     string
	}{
		{"assignee", Grouping{Kind: GroupAssignee}, "user"},
		{"priority", Grouping{Kind: GroupPriority}, "urgent"},
		{"type", Grouping{Kind: GroupType}, "feature"},
		{"enum", Grouping{Kind: GroupEnum, FieldID: "team"}, "sales"},
		{"none", Grouping{Kind: GroupNone}, "_all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, _ := laneFor(tc.grouping, task)
			if id != tc.want {
				t.Fatalf("lane=%q want %q", id, tc.want)
			}
		})
	}
}

func TestTodo_PM_016_ViewNameAndSwimlaneOrderRoundTrip(t *testing.T) {
	view := baseView()
	view.SwimlaneValueOrder = []string{"u2", "u1", "_unassigned"}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var got BoardView
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != view.Name || len(got.SwimlaneValueOrder) != 3 || got.SwimlaneValueOrder[0] != "u2" || got.SwimlaneValueOrder[2] != "_unassigned" {
		t.Fatalf("view did not round-trip: %+v", got)
	}
	if err := Validate(got); err != nil {
		t.Fatalf("round-tripped view invalid: %v", err)
	}
}

func TestTodo_PM_016_SwimlaneValueOrder(t *testing.T) {
	view := baseView()
	view.SwimlaneValueOrder = []string{"u2"}
	source := &taskSource{page: []Task{
		{ID: "a", StatusID: "open", AssigneeID: "u1", AssigneeName: "Rae"},
		{ID: "b", StatusID: "open", AssigneeID: "u2", AssigneeName: "Lee"},
		{ID: "c", StatusID: "open", AssigneeID: "u3", AssigneeName: "Sam"},
	}}
	page, err := BuildPage(context.Background(), source, view, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	lanes := page.Columns[0].Lanes
	if len(lanes) != 3 || lanes[0].ID != "u2" || lanes[1].ID != "u1" || lanes[2].ID != "u3" {
		t.Fatalf("lane order = %+v", lanes)
	}
}

func TestTodo_PM_016_RejectsInvalidSwimlaneValueOrder(t *testing.T) {
	view := baseView()
	view.SwimlaneValueOrder = []string{"u1", "", "u1"}
	if err := Validate(view); !errors.Is(err, ErrInvalidView) {
		t.Fatalf("order validation error = %v", err)
	}
}

func TestCardProjectionIncludesOnlyConfiguredFields(t *testing.T) {
	task := Task{ID: "task", Title: "Visible title", StatusID: "open", AssigneeID: "user", AssigneeName: "Ada", Priority: "high", TypeID: "bug", DueDate: "2026-10-01", EnumFields: map[string]string{"region": "west", "secret": "hidden"}}
	got := projectCard(task, []string{CardTitle, "region"})
	want := Task{ID: "task", Title: "Visible title", EnumFields: map[string]string{"region": "west"}}
	if got.ID != want.ID || got.Title != want.Title || got.StatusID != want.StatusID || got.AssigneeID != want.AssigneeID || got.AssigneeName != want.AssigneeName || got.Priority != want.Priority || got.TypeID != want.TypeID || got.DueDate != want.DueDate || len(got.EnumFields) != 1 || got.EnumFields["region"] != "west" {
		t.Fatalf("projected card = %+v", got)
	}
}
