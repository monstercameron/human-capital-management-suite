package agentinvocationstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The rating a person holds on an answer can be read back, for that person and
// that conversation only, and is gone once it was undone. This reaches the real
// store through the runtime role.
func TestTodo_CHATBUG_066_Integration(t *testing.T) {
	store, tenantA, tenantB, cleanup := agentUXR5SrvFeedbackStore(t)
	defer cleanup()
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 22, 25, 0, 0, time.UTC)

	none, err := store.ListAnswerFeedback(ctx, tenantA.String(), "alice", "room-a")
	if err != nil || len(none) != 0 {
		t.Fatalf("ratings before any was given=%+v err=%v", none, err)
	}
	if _, err := store.SubmitAnswerFeedback(ctx, tenantA.String(), "alice", "invocation-a", true, "", "chatbug066-001", at); err != nil {
		t.Fatal(err)
	}
	held, err := store.ListAnswerFeedback(ctx, tenantA.String(), "alice", "room-a")
	if err != nil || len(held) != 1 || !held["invocation-a"].Helpful || !held["invocation-a"].Active || held["invocation-a"].OutputID != "output-a" {
		t.Fatalf("the stored rating is not read back: %+v err=%v", held, err)
	}
	for _, tc := range []struct{ name, tenant, person, conversation string }{
		{"another conversation", tenantA.String(), "alice", "room-b"},
		{"another person in the conversation", tenantA.String(), "bob", "room-a"},
		{"another tenant", tenantB.String(), "alice", "room-a"},
	} {
		if got, err := store.ListAnswerFeedback(ctx, tc.tenant, tc.person, tc.conversation); err != nil || len(got) != 0 {
			t.Fatalf("%s reads the rating: %+v err=%v", tc.name, got, err)
		}
	}
	if _, err := store.SubmitAnswerFeedback(ctx, tenantA.String(), "alice", "invocation-a", false, "", "chatbug066-002", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if held, err = store.ListAnswerFeedback(ctx, tenantA.String(), "alice", "room-a"); err != nil || len(held) != 1 || held["invocation-a"].Helpful {
		t.Fatalf("a changed rating is not read back: %+v err=%v", held, err)
	}
	if _, err := store.UndoAnswerFeedback(ctx, tenantA.String(), "alice", "invocation-a", "chatbug066-undo", at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if held, err = store.ListAnswerFeedback(ctx, tenantA.String(), "alice", "room-a"); err != nil || len(held) != 0 {
		t.Fatalf("an undone rating is still read back: %+v err=%v", held, err)
	}
	for _, invalid := range [][3]string{{"not-a-tenant", "alice", "room-a"}, {tenantA.String(), "", "room-a"}, {tenantA.String(), "alice", ""}} {
		if _, err := store.ListAnswerFeedback(ctx, invalid[0], invalid[1], invalid[2]); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%v: err=%v, want invalid", invalid, err)
		}
	}
}
