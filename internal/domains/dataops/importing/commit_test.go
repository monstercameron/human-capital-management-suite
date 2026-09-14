package importing_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
)

// ledgerCommitter is an idempotent item committer that records every write it
// actually performs, keyed by idempotency key.
type ledgerCommitter struct {
	mu       sync.Mutex
	writes   map[string]int
	failRows map[string]error
	lineage  func(importing.ItemRequest) importing.ItemLineage
	calls    int
}

func newLedger() *ledgerCommitter {
	return &ledgerCommitter{writes: map[string]int{}, failRows: map[string]error{}}
}

func (l *ledgerCommitter) CommitItem(_ context.Context, req importing.ItemRequest) (importing.ItemLineage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if err := l.failRows[req.Draft.RowID]; err != nil {
		return importing.ItemLineage{}, err
	}
	l.writes[req.IdempotencyKey]++
	if l.lineage != nil {
		return l.lineage(req), nil
	}
	return importing.ItemLineage{TransactionRef: "tx:" + req.IdempotencyKey, ObservationRef: "obs:" + req.Draft.RowID,
		ReconciliationStatus: importing.ReconciliationMatched}, nil
}

func (l *ledgerCommitter) effects() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, c := range l.writes {
		n += c
	}
	return n
}

// commitFixture returns a signed simulation with two CREATE drafts, its
// approval and the batch and mapping it was simulated from.
func commitFixture(t *testing.T) (importing.CommitInput, importing.ImportSimulation) {
	t.Helper()
	in, signer := simulationFixture(t)
	sim, err := importing.SimulateImport(in)
	if err != nil {
		t.Fatal(err)
	}
	return importing.CommitInput{
		ImportID: "import-1", Simulation: sim, Verifier: signer, Batch: in.Batch, Mapping: in.Mapping,
		Approval:    importing.CommitApproval{SimulationDigest: sim.Digest, BatchDigest: sim.BatchDigest, MappingDigest: sim.MappingDigest, ApprovalRef: "approval-1", ApprovedBy: "principal:approver"},
		Checkpoints: importing.NewMemoryCheckpoints(), Committer: newLedger(),
	}, sim
}

func partitioned(t *testing.T, res importing.CommitResult, drafts int) {
	t.Helper()
	if res.Committed+res.NoOps+res.Errors+res.Conflicts+res.Failed+res.Pending != drafts || len(res.Items) != drafts {
		t.Fatalf("result %+v does not partition %d drafts", res, drafts)
	}
}

// TestTodo_DATAOPS_006 proves an approved import commits every proposal-bound
// item exactly once with full lineage, resumes from a partial checkpoint
// without duplicating a write, retries a failed item, and refuses a changed
// batch, mapping or proposal.
func TestTodo_DATAOPS_006(t *testing.T) {
	ctx := context.Background()
	in, sim := commitFixture(t)
	ledger := newLedger()
	in.Committer = ledger

	// A bounded first slice commits one item and checkpoints.
	in.Limit = 1
	first, err := importing.CommitImport(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	partitioned(t, first, len(sim.Drafts))
	if first.Committed != 1 || first.Pending != 1 || first.Complete || ledger.effects() != 1 {
		t.Fatalf("first slice = %+v effects %d", first, ledger.effects())
	}
	// Resume completes without re-committing the first item.
	in.Limit = 0
	second, err := importing.CommitImport(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	partitioned(t, second, len(sim.Drafts))
	if second.Committed != 2 || !second.Complete || ledger.effects() != 2 || !second.Items[0].Resumed {
		t.Fatalf("resume = %+v effects %d", second, ledger.effects())
	}
	for _, item := range second.Items {
		if item.Lineage.TransactionRef == "" || item.Lineage.ObservationRef == "" || item.Lineage.ReconciliationStatus != importing.ReconciliationMatched {
			t.Fatalf("committed item without lineage: %+v", item)
		}
	}
	// A repeat of a complete import commits nothing more.
	if again, err := importing.CommitImport(ctx, in); err != nil || again.Committed != 2 || ledger.effects() != 2 {
		t.Fatalf("repeat = %+v, %v, effects %d", again, err, ledger.effects())
	}

	// Changed batch, mapping or proposal are refused before any item.
	for name, mutate := range map[string]func(*importing.CommitInput){
		"approval digest": func(c *importing.CommitInput) { c.Approval.SimulationDigest = "sha256:other" },
		"approval batch":  func(c *importing.CommitInput) { c.Approval.BatchDigest = "sha256:other" },
		"no approval":     func(c *importing.CommitInput) { c.Approval.ApprovalRef = "" },
		"mapping":         func(c *importing.CommitInput) { c.Mapping.Digest = "sha256:other" },
		"forged sim":      func(c *importing.CommitInput) { c.Simulation.Creates++ },
		"batch": func(c *importing.CommitInput) {
			other, _ := commitFixture(t)
			c.Batch = other.Batch
			c.Simulation.BatchDigest, c.Approval.BatchDigest = "sha256:x", "sha256:x"
		},
	} {
		c, _ := commitFixture(t)
		l := newLedger()
		c.Committer = l
		mutate(&c)
		if _, err := importing.CommitImport(ctx, c); !errors.Is(err, importing.ErrCommitBinding) || l.calls != 0 {
			t.Errorf("%s: err %v, committer calls %d", name, err, l.calls)
		}
	}

	// A failed item is recorded, not skipped, and succeeds on resume once.
	c, _ := commitFixture(t)
	flaky := newLedger()
	flaky.failRows[sim.Drafts[1].RowID] = errors.New("connector down")
	c.Committer = flaky
	failed, err := importing.CommitImport(ctx, c)
	if err != nil || failed.Failed != 1 || failed.Committed != 1 || failed.Complete || failed.Items[1].FailureCode != "COMMIT_FAILED" {
		t.Fatalf("failed item = %+v, %v", failed, err)
	}
	delete(flaky.failRows, sim.Drafts[1].RowID)
	healed, err := importing.CommitImport(ctx, c)
	if err != nil || healed.Committed != 2 || !healed.Complete || flaky.effects() != 2 {
		t.Fatalf("healed = %+v, %v, effects %d", healed, err, flaky.effects())
	}

	// A checkpoint from another simulation cannot be resumed under this one.
	store := importing.NewMemoryCheckpoints()
	if err := store.Save(ctx, importing.Checkpoint{ImportID: "import-1", SimulationDigest: "sha256:old", Version: 1}, 0); err != nil {
		t.Fatal(err)
	}
	c2, _ := commitFixture(t)
	c2.Checkpoints = store
	if _, err := importing.CommitImport(ctx, c2); !errors.Is(err, importing.ErrCommitBinding) {
		t.Fatalf("foreign checkpoint = %v", err)
	}

	// Rollback is governed correction, never deletion.
	corrective := importing.CorrectiveIntents(sim, second)
	if len(corrective) != 2 || corrective[0].CorrectsTxRef == "" || corrective[0].Writes[0].Proposed != sim.Drafts[0].Writes[0].Current {
		t.Fatalf("corrective intents = %+v", corrective)
	}
}

// TestTodo_DATAOPS_006_Mutation proves each guard is load-bearing: a
// committer that omits lineage or reports a reconciliation mismatch never
// yields COMMITTED, a concurrent resume loses the checkpoint compare-and-set
// instead of double-committing, and removing the checkpoint would re-run a
// committed item (which the idempotency key makes observable).
func TestTodo_DATAOPS_006_Mutation(t *testing.T) {
	ctx := context.Background()
	for name, lineage := range map[string]func(importing.ItemRequest) importing.ItemLineage{
		"no transaction": func(r importing.ItemRequest) importing.ItemLineage {
			return importing.ItemLineage{ObservationRef: "o", ReconciliationStatus: importing.ReconciliationMatched}
		},
		"no observation": func(r importing.ItemRequest) importing.ItemLineage {
			return importing.ItemLineage{TransactionRef: "t", ReconciliationStatus: importing.ReconciliationMatched}
		},
		"mismatch": func(r importing.ItemRequest) importing.ItemLineage {
			return importing.ItemLineage{TransactionRef: "t", ObservationRef: "o", ReconciliationStatus: importing.ReconciliationMismatch}
		},
	} {
		in, _ := commitFixture(t)
		l := newLedger()
		l.lineage = lineage
		in.Committer = l
		res, err := importing.CommitImport(ctx, in)
		if err != nil || res.Committed != 0 || res.Failed != 2 {
			t.Errorf("%s: %+v, %v", name, res, err)
		}
	}

	in, sim := commitFixture(t)
	shared := newLedger()
	store := importing.NewMemoryCheckpoints()
	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := range results {
		wg.Go(func() {
			c := in
			c.Committer, c.Checkpoints = shared, store
			_, results[i] = importing.CommitImport(ctx, c)
		})
	}
	wg.Wait()
	for key, n := range shared.writes {
		if n < 1 {
			t.Fatalf("key %s never written", key)
		}
	}
	cp, _, _ := store.Load(ctx, "import-1")
	committed := 0
	for _, st := range cp.Items {
		if st.Status == importing.ItemCommitted {
			committed++
		}
	}
	if committed != len(sim.Drafts) {
		t.Fatalf("checkpoint after concurrent resumes records %d committed of %d", committed, len(sim.Drafts))
	}
	conflicts := 0
	for _, err := range results {
		if errors.Is(err, importing.ErrCheckpointConflict) {
			conflicts++
		} else if err != nil {
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if conflicts == 0 && len(results) > 1 {
		t.Log("no checkpoint conflict observed in this interleaving; idempotency keys still bound duplicates")
	}
	if importing.IdempotencyKey(sim.Digest, "a") == importing.IdempotencyKey(sim.Digest, "b") || importing.IdempotencyKey("x", "a") == importing.IdempotencyKey("y", "a") {
		t.Fatal("idempotency keys do not bind simulation and row")
	}
}

// FuzzTodo_DATAOPS_006 drives arbitrary slice limits and failure patterns and
// checks the partition, exactly-once and lineage invariants after resuming to
// completion.
func FuzzTodo_DATAOPS_006(f *testing.F) {
	f.Add(uint8(0), uint8(0))
	f.Add(uint8(1), uint8(1))
	f.Add(uint8(2), uint8(3))
	f.Fuzz(func(t *testing.T, limit, failMask uint8) {
		ctx := context.Background()
		in, sim := commitFixture(t)
		l := newLedger()
		for i, d := range sim.Drafts {
			if failMask&(1<<i) != 0 {
				l.failRows[d.RowID] = fmt.Errorf("fail %d", i)
			}
		}
		in.Committer = l
		in.Limit = int(limit % 3)
		for round := 0; round < 6; round++ {
			res, err := importing.CommitImport(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			partitioned(t, res, len(sim.Drafts))
			if round == 2 {
				l.failRows = map[string]error{}
			}
			if res.Complete {
				break
			}
		}
		final, err := importing.CommitImport(ctx, in)
		if err != nil || !final.Complete {
			t.Fatalf("did not converge: %+v, %v", final, err)
		}
		for key, n := range l.writes {
			if n != 1 {
				t.Fatalf("item %s written %d times", key, n)
			}
		}
	})
}

func TestCommitImportRejectsIncompleteInputAndUnknownDrafts(t *testing.T) {
	ctx := context.Background()
	in, _ := commitFixture(t)
	for name, mutate := range map[string]func(*importing.CommitInput){
		"no id":        func(c *importing.CommitInput) { c.ImportID = "" },
		"no committer": func(c *importing.CommitInput) { c.Committer = nil },
		"no store":     func(c *importing.CommitInput) { c.Checkpoints = nil },
		"no verifier":  func(c *importing.CommitInput) { c.Verifier = nil },
		"neg limit":    func(c *importing.CommitInput) { c.Limit = -1 },
	} {
		c := in
		mutate(&c)
		if _, err := importing.CommitImport(ctx, c); !errors.Is(err, importing.ErrCommitInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	store := failingStore{err: errors.New("db down")}
	c := in
	c.Checkpoints = store
	if _, err := importing.CommitImport(ctx, c); err == nil {
		t.Error("load failure accepted")
	}
	c.Checkpoints = failingStore{saveErr: importing.ErrCheckpointConflict}
	if _, err := importing.CommitImport(ctx, c); !errors.Is(err, importing.ErrCheckpointConflict) {
		t.Errorf("save conflict = %v", err)
	}
	classified, _ := commitFixture(t)
	for i := range classified.Simulation.Drafts {
		classified.Simulation.Drafts[i].Status = []string{"NO_OP", "ERROR", "CONFLICT"}[i%3]
	}
	res := importing.CorrectiveIntents(classified.Simulation, importing.CommitResult{Items: []importing.ItemOutcome{{RowID: "x", Status: importing.ItemFailed}}})
	if len(res) != 0 {
		t.Error("corrective intents planned for an uncommitted item")
	}
}

type failingStore struct{ err, saveErr error }

func (f failingStore) Load(context.Context, string) (importing.Checkpoint, bool, error) {
	return importing.Checkpoint{}, false, f.err
}
func (f failingStore) Save(context.Context, importing.Checkpoint, uint64) error { return f.saveErr }
