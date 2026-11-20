package chatsearch

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// windowSource is an authorized source that pages through WindowRows, as the
// directory-style sources do.
type windowSource struct{ rows []Row }

func (s windowSource) Search(_ context.Context, q Request) ([]Row, error) {
	matched := []Row{}
	for _, r := range s.rows {
		if Match(r, q) {
			matched = append(matched, r)
		}
	}
	return WindowRows(matched, q), nil
}
func (windowSource) CanOpen(context.Context, Actor, Row) (bool, error) { return true, nil }

func orderRows() []Row {
	base := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	mk := func(kind Kind, id string, day int) Row {
		return Row{Kind: kind, ID: id, TenantID: "tenant", Text: "deadline " + id, At: base.AddDate(0, 0, day)}
	}
	return []Row{mk(Message, "m1", 0), mk(Message, "m2", 1), mk(Message, "m3", 1), mk(Message, "m4", 5), mk(Message, "m5", 9)}
}

func pageAll(t *testing.T, r *Registry, q Request) []string {
	t.Helper()
	var ids []string
	for i := 0; i < 10; i++ {
		got, err := r.Search(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, resultIDs(got)...)
		if got.NextCursor == "" {
			return ids
		}
		q.Cursor = got.NextCursor
	}
	t.Fatal("cursor never ended")
	return nil
}

func TestIssue3945_OldestFirstAcrossPagesWithStableTieBreak(t *testing.T) {
	r := NewRegistry()
	addFixture(t, r, Message, windowSource{rows: orderRows()})
	q := searchRequest()
	q.Query, q.Limit = "deadline", 2
	newest := pageAll(t, r, q)
	if want := []string{"m5", "m4", "m3", "m2", "m1"}; !reflect.DeepEqual(newest, want) {
		t.Fatalf("default order = %v want %v", newest, want)
	}
	q.Order = OrderNewest
	if got := pageAll(t, r, q); !reflect.DeepEqual(got, newest) {
		t.Fatalf("explicit newest = %v want %v", got, newest)
	}
	q.Order = OrderOldest
	if got, want := pageAll(t, r, q), []string{"m1", "m2", "m3", "m4", "m5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("oldest = %v want %v", got, want)
	}
}

func TestIssue3945_OldestFirstOrdersKindsTogetherByDate(t *testing.T) {
	r := NewRegistry()
	base := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	addFixture(t, r, Message, windowSource{rows: []Row{{Kind: Message, ID: "m", TenantID: "tenant", Text: "deadline", At: base.AddDate(0, 0, 4)}}})
	addFixture(t, r, Thread, windowSource{rows: []Row{{Kind: Thread, ID: "t", TenantID: "tenant", Text: "deadline", At: base}}})
	q := searchRequest()
	q.Query, q.Limit, q.Order = "deadline", 1, OrderOldest
	if got, want := pageAll(t, r, q), []string{"t", "m"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("oldest across kinds = %v want %v", got, want)
	}
}

func TestIssue3945_Security_CursorIsBoundToOrder(t *testing.T) {
	r := NewRegistry()
	addFixture(t, r, Message, windowSource{rows: orderRows()})
	q := searchRequest()
	q.Query, q.Limit = "deadline", 2
	first, err := r.Search(context.Background(), q)
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first page %+v %v", first, err)
	}
	q.Order, q.Cursor = OrderOldest, first.NextCursor
	if _, err := r.Search(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a newest cursor replayed oldest-first: %v", err)
	}
}

func TestIssue3945_Security_UnknownOrderRefused(t *testing.T) {
	r := NewRegistry()
	addFixture(t, r, Message, windowSource{rows: orderRows()})
	q := searchRequest()
	q.Order = "random"
	if _, err := r.Search(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown order accepted: %v", err)
	}
}

func TestIssue3945_SortWordInQueryOrdersAndIsNotAFilter(t *testing.T) {
	r := NewRegistry()
	addFixture(t, r, Message, windowSource{rows: orderRows()})
	q := searchRequest()
	q.Query, q.Limit = "deadline sort:oldest", 3
	if got, want := pageAll(t, r, q), []string{"m1", "m2", "m3", "m4", "m5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sort:oldest = %v want %v", got, want)
	}
	p, err := Parse("sort:oldest")
	if err != nil || !p.Filters.Empty() || p.Filters.Order != OrderOldest || !reflect.DeepEqual(p.Chips, []string{"sort:oldest"}) {
		t.Fatalf("parse %+v %v", p, err)
	}
	q.Query = "sort:oldest"
	if _, err := r.Search(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an order alone is not a search: %v", err)
	}
	q.Query = "deadline sort:sideways"
	if _, err := r.Search(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown sort word accepted: %v", err)
	}
	q.Query, q.Order = "deadline sort:oldest", OrderNewest
	if _, err := r.Search(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Fatalf("disagreeing orders accepted: %v", err)
	}
}
