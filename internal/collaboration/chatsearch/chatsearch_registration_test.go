package chatsearch

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_CHATSEARCH_001_Registration(t *testing.T) {
	r := NewRegistry()
	q := searchRequest()
	row := Row{Kind: Saved, ID: "mine", TenantID: "tenant", OwnerID: "alice", Private: true, Text: "budget", At: q.At}
	allowed := true
	s := SourceFuncs{SearchRows: func(_ context.Context, q Request) ([]Row, error) {
		if allowed && q.Actor.PersonID == row.OwnerID {
			return []Row{row}, nil
		}
		return nil, nil
	}, OpenRow: func(_ context.Context, a Actor, _ Row) (bool, error) {
		return allowed && a.PersonID == row.OwnerID, nil
	}}
	if err := r.RegisterSource(Saved, s); err != nil {
		t.Fatal(err)
	}
	if err := r.ValidateStoredKinds([]Kind{Saved}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Search(context.Background(), q)
	if err != nil || len(resultIDs(got)) != 1 {
		t.Fatalf("extension %+v %v", got, err)
	}
	allowed = false
	got, err = r.Search(context.Background(), q)
	if err != nil || len(resultIDs(got)) != 0 {
		t.Fatalf("extension revoke %+v %v", got, err)
	}
	if !errors.Is(r.RegisterSource("unknown", s), ErrRegistry) {
		t.Fatal("undeclared extension registered")
	}
	if !errors.Is(r.RegisterSource(Voice, SourceFuncs{}), ErrRegistry) {
		t.Fatal("incomplete source registered")
	}
	unavailable := SourceFuncs{SearchRows: func(context.Context, Request) ([]Row, error) { return nil, ErrSourceUnavailable }, OpenRow: s.OpenRow}
	if err := r.RegisterSource(Voice, unavailable); err != nil {
		t.Fatal(err)
	}
	q.Filters.Kind = Voice
	got, err = r.Search(context.Background(), q)
	if err != nil || len(got.Groups) != 0 || len(got.Unavailable) != 1 || got.Unavailable[0] != Voice {
		t.Fatalf("typed unavailable %+v %v", got, err)
	}
	if _, err := (SourceFuncs{}).Search(context.Background(), q); !errors.Is(err, ErrRegistry) {
		t.Fatal("incomplete adapter accepted")
	}
	if _, err := (SourceFuncs{}).CanOpen(context.Background(), q.Actor, row); !errors.Is(err, ErrRegistry) {
		t.Fatal("incomplete authority accepted")
	}
	q.Filters.Kind = "unknown"
	if _, err := r.Search(context.Background(), q); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown kind filter accepted")
	}
}
