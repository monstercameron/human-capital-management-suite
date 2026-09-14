package ledger

import (
	"strings"
	"sync"
	"testing"
)

func mustCommitTx(t *testing.T, journal *CommitJournal, txID string, head int64) CommitReceipt {
	t.Helper()
	receipt, err := journal.Commit(txID, head)
	if err != nil {
		t.Fatalf("Commit(%s): %v", txID, err)
	}
	return receipt
}

func mustInterruptTx(t *testing.T, journal *CommitJournal, txID string) CommitReceipt {
	t.Helper()
	receipt, err := journal.Interrupt(txID, "connection lost after send, broker receipt unseen")
	if err != nil {
		t.Fatalf("Interrupt(%s): %v", txID, err)
	}
	return receipt
}

func mustResolveTx(t *testing.T, journal *CommitJournal, txID string, observedHead int64, observed bool) CommitReceipt {
	t.Helper()
	receipt, err := journal.Resolve(txID, observedHead, observed)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", txID, err)
	}
	return receipt
}

func mustRepairDerived(t *testing.T, journal *CommitJournal, derived map[string]uint64) map[string]uint64 {
	t.Helper()
	return journal.RepairDerived(derived)
}

// TestTodo_LEDGER_014_Race: concurrent commits and resolves of one
// identity append exactly one effect.
func TestTodo_LEDGER_014_Race(t *testing.T) {
	journal := NewCommitJournal()
	if err := journal.Begin("tx-race", "key-race"); err != nil {
		t.Fatal(err)
	}
	const racers = 16
	var wg sync.WaitGroup
	receipts := make([]CommitReceipt, racers)
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			receipts[i], errs[i] = journal.Commit("tx-race", 9)
		}(i)
	}
	wg.Wait()
	effects := 0
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		effects += receipts[i].AppendedEffects
		if receipts[i].State != CommitCommitted {
			t.Fatalf("racer %d: %+v", i, receipts[i])
		}
	}
	if effects != 1 {
		t.Fatalf("appended %d effects under race", effects)
	}
}

// TestTodo_LEDGER_014_Fault: unknown transactions, key reuse, head
// disagreement and evidence-less ambiguity fail closed.
func TestTodo_LEDGER_014_Fault(t *testing.T) {
	journal := NewCommitJournal()
	if _, err := journal.Commit("tx-ghost", 1); err == nil {
		t.Fatal("unknown commit admitted")
	}
	if _, err := journal.Resolve("tx-ghost", 1, true); err == nil {
		t.Fatal("unknown resolve admitted")
	}
	if _, err := journal.Interrupt("tx-ghost", "evidence"); err == nil {
		t.Fatal("unknown interrupt admitted")
	}
	if err := journal.Begin("tx-a", "key-a"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Begin("tx-b", "key-a"); err == nil {
		t.Fatal("idempotency key reuse admitted")
	}
	if _, err := journal.Interrupt("tx-a", ""); err == nil {
		t.Fatal("evidence-less interruption admitted")
	}
	mustCommitTx(t, journal, "tx-a", 5)
	if _, err := journal.Commit("tx-a", 6); err == nil {
		t.Fatal("head/event disagreement admitted")
	}
	// Re-committing the recorded head replays the verdict cleanly.
	if receipt := mustCommitTx(t, journal, "tx-a", 5); receipt.AppendedEffects != 0 {
		t.Fatalf("replay appended: %+v", receipt)
	}
	if err := journal.Begin("tx-c", "key-c"); err != nil {
		t.Fatal(err)
	}
	mustInterruptTx(t, journal, "tx-c")
	if _, err := journal.Resolve("tx-c", 3, true); err != nil {
		t.Fatalf("resolve after interrupt: %v", err)
	}
	// A not-committed resolution appends nothing and stays final.
	if err := journal.Begin("tx-d", "key-d"); err != nil {
		t.Fatal(err)
	}
	mustInterruptTx(t, journal, "tx-d")
	lost := mustResolveTx(t, journal, "tx-d", 0, false)
	if lost.State != CommitNotCommitted || lost.AppendedEffects != 0 {
		t.Fatalf("lost commit: %+v", lost)
	}
	record, err := journal.Ambiguity("tx-d")
	if err != nil || !record.Resolved || record.Evidence == "" {
		t.Fatalf("ambiguity record: %+v %v", record, err)
	}
}

// TestTodo_LEDGER_014_Mutation: identity edges resolve on the
// documented side.
func TestTodo_LEDGER_014_Mutation(t *testing.T) {
	journal := NewCommitJournal()
	if err := journal.Begin(" tx-pad", "key-pad"); err == nil {
		t.Fatal("padded transaction admitted")
	} else if !strings.Contains(err.Error(), "transaction id") {
		t.Fatalf("wrong error: %v", err)
	}
	if err := journal.Begin("tx-e", ""); err == nil {
		t.Fatal("keyless transaction admitted")
	}
	// Re-beginning replays the verdict instead of double-beginning.
	if err := journal.Begin("tx-f", "key-f"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Begin("tx-f", "key-other"); err != nil {
		t.Fatalf("re-begin: %v", err)
	}
	first := mustCommitTx(t, journal, "tx-f", 4)
	second := mustCommitTx(t, journal, "tx-f", 4)
	if first.Record != second.Record || second.AppendedEffects != 0 {
		t.Fatalf("re-commit: %+v %+v", first, second)
	}
}

// TestTodo_LEDGER_014_Integration: interruption, observation and repair
// compose with the ambiguity record as the governed seam.
func TestTodo_LEDGER_014_Integration(t *testing.T) {
	journal := NewCommitJournal()
	if err := journal.Begin("tx-int", "key-int"); err != nil {
		t.Fatal(err)
	}
	open, err := journal.Ambiguity("tx-int")
	if err != nil || open.Resolved || open.Evidence != "" {
		t.Fatalf("open record: %+v %v", open, err)
	}
	mustInterruptTx(t, journal, "tx-int")
	recorded, err := journal.Ambiguity("tx-int")
	if err != nil || recorded.Resolved || recorded.Evidence == "" {
		t.Fatalf("recorded: %+v %v", recorded, err)
	}
	resolved := mustResolveTx(t, journal, "tx-int", 12, true)
	if resolved.State != CommitCommitted || resolved.Head != 12 {
		t.Fatalf("resolved: %+v", resolved)
	}
	settled, err := journal.Ambiguity("tx-int")
	if err != nil || !settled.Resolved {
		t.Fatalf("settled: %+v %v", settled, err)
	}
	if repaired := mustRepairDerived(t, journal, map[string]uint64{"projection:w": 3}); repaired["projection:w"] != 12 {
		t.Fatalf("repaired: %+v", repaired)
	}
}
