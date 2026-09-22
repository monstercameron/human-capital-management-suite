package chatauthority

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type rejectedTx struct{ calls int }

func (f *rejectedTx) RunTx(_ context.Context, fn func(dbport.Tx) error) error {
	f.calls++
	return errors.New("unexpected database transaction")
}

func TestTodo_CHAT_012_Security(t *testing.T) {
	ctx := context.Background()
	store := New(&rejectedTx{})
	g, err := chatpolicy.ProposeGrant("g", "c", "host", "consumer", "conversation", "internal", "US", 1, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Propose(ctx, "consumer", "actor", g); !errors.Is(err, ErrDenied) {
		t.Fatalf("consumer self proposal: %v", err)
	}
	g.AcceptedByConsumer = true
	if err := store.Propose(ctx, "host", "actor", g); !errors.Is(err, ErrDenied) {
		t.Fatalf("preaccepted proposal: %v", err)
	}
	if err := store.Accept(ctx, "host", "host", "c", "g", "actor", time.Now()); !errors.Is(err, ErrDenied) {
		t.Fatalf("same company acceptance: %v", err)
	}
	if err := store.Revoke(ctx, "host", "consumer", "c", "g", "third-party", "actor", time.Now()); !errors.Is(err, ErrDenied) {
		t.Fatalf("third-party revoke: %v", err)
	}
}
