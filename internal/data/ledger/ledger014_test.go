package ledger

import (
	"testing"
)

func TestTodo_LEDGER_014(t *testing.T) {
	journal := NewCommitJournal()
	// A clean commit resolves committed by identity.
	if err := journal.Begin("tx-1", "promo-1:w-1"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	receipt := mustCommitTx(t, journal, "tx-1", 7)
	if receipt.State != CommitCommitted {
		t.Fatalf("state=%v", receipt.State)
	}
	// Connection loss at the commit boundary records ambiguity: the
	// caller must not append a second effect.
	if err := journal.Begin("tx-2", "promo-1:w-2"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	ambiguous := mustInterruptTx(t, journal, "tx-2")
	if ambiguous.State != CommitAmbiguous {
		t.Fatalf("interrupted state=%v", ambiguous.State)
	}
	// Recovery resolves by identity against the observed head: the event
	// made it, so committed — no duplicate append.
	resolved := mustResolveTx(t, journal, "tx-2", 8, true)
	if resolved.State != CommitCommitted {
		t.Fatalf("resolved state=%v", resolved.State)
	}
	if resolved.AppendedEffects != 0 {
		t.Fatalf("resolution appended %d effects", resolved.AppendedEffects)
	}
	// Replaying the same transaction replays the verdict, never a second
	// effect.
	again := mustResolveTx(t, journal, "tx-2", 8, true)
	if again.State != CommitCommitted || again.AppendedEffects != 0 {
		t.Fatalf("replay: %+v", again)
	}
	// Derived rows repair from the ledger without new history.
	derived := map[string]uint64{"projection:w-2": 0}
	repaired := mustRepairDerived(t, journal, derived)
	if repaired["projection:w-2"] != 8 {
		t.Fatalf("repaired: %+v", repaired)
	}
}
