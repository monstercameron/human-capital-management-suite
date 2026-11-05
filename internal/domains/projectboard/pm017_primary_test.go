package projectboard

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestTodo_PM_017 proves the GREEN paging contract: stable authorized
// cursors page at most MaxPageSize cards with bounded predicates, declared
// ordering, and typed failures (stale/invalid pages never render silently).
func TestTodo_PM_017(t *testing.T) {
	if MaxPageSize != 100 {
		t.Fatalf("MaxPageSize = %d, want the 100-card page bound", MaxPageSize)
	}
	tasks := []Task{
		{ID: "b", Title: "zeta", StatusID: "open", AssigneeID: "u1", AssigneeName: "Rae", Priority: "high"},
		{ID: "a", Title: "Alpha", StatusID: "open", AssigneeID: "u1", AssigneeName: "Rae", Priority: "high"},
	}
	next := &Cursor{Token: "stable-next"}
	source := &taskSource{page: tasks, next: next}
	view := baseView()
	view.Filter = Filter{Priorities: []string{"high"}}
	in := &Cursor{Token: "stable-in"}
	page, err := BuildPage(context.Background(), source, view, MaxPageSize, in)
	if err != nil {
		t.Fatal(err)
	}
	if source.query.Limit != MaxPageSize || source.query.Cursor != *in || source.query.OrderBy != OrderTitle || len(source.query.Filter.Priorities) != 1 {
		t.Fatalf("bounded declared query = %+v", source.query)
	}
	if page.Next == nil || *page.Next != *next {
		t.Fatalf("stable cursor not chained: %+v", page.Next)
	}
	cards := page.Columns[0].Lanes[0].Cards
	if len(cards) != 2 || cards[0].Task.ID != "a" || cards[1].Task.ID != "b" {
		t.Fatalf("cards not in declared title order: %+v", cards)
	}
	sentinel := errors.New("stale cursor")
	stale := &taskSource{err: sentinel}
	if _, err := BuildPage(context.Background(), stale, view, 10, in); !errors.Is(err, sentinel) {
		t.Fatalf("stale source failure = %v, want the typed error", err)
	}
}

func BenchmarkTodo_PM_017(b *testing.B) {
	tasks := make([]Task, MaxPageSize)
	for i := range tasks {
		tasks[i] = Task{ID: fmt.Sprintf("task-%03d", i), Title: fmt.Sprintf("Task %03d", i), StatusID: "open", AssigneeID: "u1", AssigneeName: "Rae", Priority: "high"}
	}
	source := &taskSource{page: tasks}
	view := baseView()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := BuildPage(ctx, source, view, MaxPageSize, nil); err != nil {
			b.Fatal(err)
		}
	}
}
