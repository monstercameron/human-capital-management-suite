package outbox

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// TestTodo_EVENT_004_Race: concurrent publishers of one identity apply
// the effect exactly once.
func TestTodo_EVENT_004_Race(t *testing.T) {
	journal := NewDispatchJournal()
	var applied atomic.Int64
	apply := func() error { applied.Add(1); return nil }
	const publishers = 16
	var wg sync.WaitGroup
	outcomes := make([]PublishOutcome, publishers)
	errs := make([]error, publishers)
	for i := range publishers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outcomes[i], errs[i] = journal.Publish("op-race", fakeSender(true), apply)
		}(i)
	}
	wg.Wait()
	committed, duplicate := 0, 0
	for i := range publishers {
		if errs[i] != nil {
			t.Fatalf("publisher %d: %v", i, errs[i])
		}
		switch outcomes[i] {
		case PublishCommitted:
			committed++
		case PublishDuplicate:
			duplicate++
		default:
			t.Fatalf("publisher %d unexpected outcome %v", i, outcomes[i])
		}
	}
	if committed != 1 || duplicate != publishers-1 {
		t.Fatalf("committed=%d duplicate=%d, want 1 and %d", committed, duplicate, publishers-1)
	}
	if applied.Load() != 1 {
		t.Fatalf("effect applied %d times under race", applied.Load())
	}
}

// TestTodo_EVENT_004_Integration: outage, durable resume, and
// downstream verification compose into exactly-once effects.
func TestTodo_EVENT_004_Integration(t *testing.T) {
	journal := NewDispatchJournal()
	downstream := map[string]bool{}
	apply := func(id string) error {
		if downstream[id] {
			return fmt.Errorf("duplicate downstream effect %s", id)
		}
		downstream[id] = true
		return nil
	}
	counting := func() error { return apply("op-live") }
	if _, err := journal.Publish("op-live", fakeSender(true), counting); err != nil {
		t.Fatalf("live publish: %v", err)
	}
	// Two operations strand ambiguous when the queue partitions.
	if _, err := journal.Publish("op-a", fakeSender(false), counting); err != nil {
		t.Fatalf("partitioned publish a: %v", err)
	}
	if _, err := journal.Publish("op-b", fakeSender(false), counting); err != nil {
		t.Fatalf("partitioned publish b: %v", err)
	}
	// The broker actually took op-b: downstream already has it.
	downstream["op-b"] = true
	snap := journal.Snapshot()
	if len(snap.Ambiguous) != 2 || len(snap.Committed) != 1 {
		t.Fatalf("snapshot: %+v", snap)
	}
	resumed, err := Resume(snap, func(id string) (bool, error) { return downstream[id], nil }, apply)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	after := resumed.Snapshot()
	if len(after.Ambiguous) != 0 || len(after.Committed) != 3 {
		t.Fatalf("post-resume snapshot: %+v", after)
	}
	if !downstream["op-a"] || !downstream["op-b"] || !downstream["op-live"] {
		t.Fatalf("downstream outcome incomplete: %v", downstream)
	}
}

// TestTodo_EVENT_004_Fault: empty identities, silent senders, unknown
// resolves and reconciler failures fail closed.
func TestTodo_EVENT_004_Fault(t *testing.T) {
	journal := NewDispatchJournal()
	apply := func() error { return nil }
	if _, err := journal.Publish("  ", fakeSender(true), apply); err == nil {
		t.Fatal("empty effect identity admitted")
	}
	if _, err := journal.Publish("op-x", nil, apply); err == nil {
		t.Fatal("nil sender admitted")
	}
	if _, err := journal.Publish("op-x", func() (bool, error) { return false, nil }, apply); err == nil {
		t.Fatal("silent sender admitted")
	} else if !strings.Contains(err.Error(), "never stay silent") {
		t.Fatalf("silent sender wrong error: %v", err)
	}
	if _, err := journal.Publish("op-x", fakeSender(true), nil); err == nil {
		t.Fatal("nil applier admitted")
	}
	if err := journal.Resolve("op-unknown", fakeReconciler(false), apply); err == nil {
		t.Fatal("unknown resolve admitted")
	}
	if _, err := journal.Publish("op-y", fakeSender(false), apply); err != nil {
		t.Fatalf("partitioned publish: %v", err)
	}
	reconcilerErr := fmt.Errorf("downstream unreachable")
	if err := journal.Resolve("op-y", func(string) (bool, error) { return false, reconcilerErr }, apply); err == nil {
		t.Fatal("failed reconciliation admitted as resolved")
	}
	if _, err := Resume(journal.Snapshot(), nil, func(string) error { return nil }); err == nil {
		t.Fatal("resume without reconciler admitted")
	}
}

// TestTodo_EVENT_004_Mutation: identity edges resolve on the documented
// side — exact match only, no silent folding.
func TestTodo_EVENT_004_Mutation(t *testing.T) {
	journal := NewDispatchJournal()
	var applied atomic.Int64
	apply := func() error { applied.Add(1); return nil }
	if _, err := journal.Publish("Op-Case", fakeSender(true), apply); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// Case differs: a distinct operation, applied again, never folded.
	outcome, err := journal.Publish("op-case", fakeSender(true), apply)
	if err != nil || outcome != PublishCommitted {
		t.Fatalf("case variant: outcome=%v err=%v", outcome, err)
	}
	if applied.Load() != 2 {
		t.Fatalf("case variants folded: applied=%d", applied.Load())
	}
	// Padded identity refuses instead of trimming into a collision.
	if _, err := journal.Publish(" Op-Case ", fakeSender(true), apply); err == nil {
		t.Fatal("padded identity admitted")
	} else if !strings.Contains(err.Error(), "effect identity") {
		t.Fatalf("padded identity wrong error: %v", err)
	}
}
