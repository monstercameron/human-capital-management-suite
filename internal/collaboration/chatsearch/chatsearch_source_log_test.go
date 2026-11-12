package chatsearch

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// issue3679FailingSource answers every search with a cause that the server
// must record, because the reader only sees "content sources are unavailable".
type issue3679FailingSource struct{ err error }

func (s issue3679FailingSource) Search(context.Context, Request) ([]Row, error) {
	return nil, s.err
}

func (s issue3679FailingSource) CanOpen(context.Context, Actor, Row) (bool, error) {
	return false, s.err
}

func TestIssue_3679_SourceFailureIsLoggedWithCause(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	cause := errors.New("issue3679 thread source cause")
	r := NewRegistry()
	addFixture(t, r, Thread, issue3679FailingSource{err: cause})

	q := searchRequest()
	q.Filters.Mine = true
	res, err := r.Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Unavailable) != 1 || res.Unavailable[0] != Thread {
		t.Fatalf("unavailable = %v, want [thread]", res.Unavailable)
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "source=thread") || !strings.Contains(out, "issue3679 thread source cause") {
		t.Fatalf("source failure not logged with its cause:\n%s", out)
	}
}
