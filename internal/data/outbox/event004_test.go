package outbox

import (
	"errors"
	"testing"
)

func TestTodo_EVENT_004(t *testing.T) {
	journal := NewDispatchJournal()
	applied := 0
	apply := func() error { applied++; return nil }

	// A clean publish commits and applies the effect exactly once.
	outcome, err := journal.Publish("op-1", fakeSender(true), apply)
	if err != nil || outcome != PublishCommitted {
		t.Fatalf("publish: outcome=%v err=%v", outcome, err)
	}
	// A redelivered publish resolves by identity: no duplicate effect.
	outcome, err = journal.Publish("op-1", fakeSender(true), apply)
	if err != nil || outcome != PublishDuplicate {
		t.Fatalf("redelivery: outcome=%v err=%v", outcome, err)
	}
	if applied != 1 {
		t.Fatalf("effect applied %d times, want exactly once", applied)
	}
	// Connection loss at the publish boundary is ambiguous, never guessed.
	outcome, err = journal.Publish("op-2", fakeSender(false), apply)
	if err != nil || outcome != PublishAmbiguous {
		t.Fatalf("outage publish: outcome=%v err=%v", outcome, err)
	}
	if applied != 1 {
		t.Fatalf("ambiguous publish applied the effect: %d", applied)
	}
	// Reconciliation verifies the downstream outcome: absent, so apply once.
	if err := journal.Resolve("op-2", fakeReconciler(false), apply); err != nil {
		t.Fatalf("resolve absent: %v", err)
	}
	if applied != 2 {
		t.Fatalf("resolved effect applied %d times", applied)
	}
	if err := journal.Resolve("op-2", fakeReconciler(true), apply); err == nil {
		t.Fatal("double resolve admitted")
	}
}

var errConnectionLost = errors.New("connection lost at publish boundary")

func fakeSender(confirm bool) Sender {
	return func() (bool, error) {
		if confirm {
			return true, nil
		}
		return false, errConnectionLost
	}
}

func fakeReconciler(downstreamHasIt bool) Reconciler {
	return func(string) (bool, error) { return downstreamHasIt, nil }
}
