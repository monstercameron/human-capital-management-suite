package chatsearch

import (
	"context"
	"testing"
)

// TestTodo_CHATSEARCH_002_Kinds: an answer names the kinds a source is
// registered for, in order, whatever kind the query asked for, so the page's
// kind filter can offer exactly those.
func TestTodo_CHATSEARCH_002_Kinds(t *testing.T) {
	r := NewRegistry()
	q := searchRequest()
	none := SourceFuncs{SearchRows: func(context.Context, Request) ([]Row, error) { return nil, nil }, OpenRow: func(context.Context, Actor, Row) (bool, error) { return true, nil }}
	got, err := r.Search(context.Background(), q)
	if err != nil || len(got.Kinds) != 0 {
		t.Fatalf("a registry with no source names kinds: %+v %v", got.Kinds, err)
	}
	for _, kind := range []Kind{Voice, Message, Todo} {
		if err := r.RegisterSource(kind, none); err != nil {
			t.Fatal(err)
		}
	}
	want := []Kind{Message, Todo, Voice}
	for _, filter := range []Kind{"", Todo, Reminder} {
		q.Filters.Kind = filter
		got, err = r.Search(context.Background(), q)
		if err != nil {
			t.Fatalf("kind %q: %v", filter, err)
		}
		if len(got.Kinds) != len(want) {
			t.Fatalf("kind %q: answer names %v, want %v", filter, got.Kinds, want)
		}
		for i := range want {
			if got.Kinds[i] != want[i] {
				t.Fatalf("kind %q: answer names %v, want %v", filter, got.Kinds, want)
			}
		}
		// A declared kind with no source is not an outage.
		if len(got.Unavailable) != 0 {
			t.Fatalf("kind %q: reported unavailable %v", filter, got.Unavailable)
		}
	}
}
