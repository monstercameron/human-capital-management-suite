package execute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
)

// ---------------------------------------------------------------------------
// TestTodo_Unit5
// ---------------------------------------------------------------------------
//
// Unit 5 (evidence atomicity): an OBS-024 execution-evidence entry commits
// beside the outcome it describes and rolls back with it. The terminal
// write's TERMINAL_WRITTEN entry is recorded on the advance transaction by
// continuationSink.Complete, and the approval/task entries by advanceOnce
// itself before the advance commits; a port that cannot join the transaction
// refuses the advance instead of splitting the entry onto one of its own.

// TestTodo_Unit5_EvidenceCommitsWithItsAdvance proves the terminal write
// and both OBS-024 entries shared one transaction: the TERMINAL_WRITTEN
// entry was recorded on the exact transaction the terminal write ran on,
// and the APPROVAL_COMPLETED entry on a transaction too — never
// off-transaction on a sink-owned second commit.
func TestTodo_Unit5_EvidenceCommitsWithItsAdvance(t *testing.T) {
	scn := newOBSScenario(t, "0af7651916cd43dd8448eb211c80319c")
	driver := scn.driver(t)

	result, err := driver.Resume(context.Background(), scn.req)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result.Status != StatusComplete {
		t.Fatalf("result.Status = %s, want COMPLETE", result.Status)
	}

	entries := scn.evidence.Entries()
	if len(entries) != 2 {
		t.Fatalf("evidence entries = %d, want 2", len(entries))
	}
	if scn.terminal.calls != 1 || len(scn.terminal.txns) != 1 {
		t.Fatalf("terminal writes = %d, want exactly 1 on one transaction", scn.terminal.calls)
	}
	writeTx := scn.terminal.txns[0]
	if writeTx == nil {
		t.Fatal("terminal write ran on no transaction")
	}
	for _, e := range entries {
		if e.tx == nil {
			t.Fatalf("%s entry recorded off-transaction: outcome and evidence can split across commits", e.kind)
		}
		if e.tx != writeTx {
			t.Fatalf("%s entry joined a different transaction than the terminal write", e.kind)
		}
	}
}

// TestTodo_Unit5_EvidenceFailureRefusesTheAdvance proves a TERMINAL_WRITTEN
// recording failure refuses the whole advance: Resume errors naming the
// entry, and no TERMINAL_WRITTEN entry exists — the failed recording leaves
// no orphaned outcome behind.
func TestTodo_Unit5_EvidenceFailureRefusesTheAdvance(t *testing.T) {
	scn := newOBSScenario(t, "0af7651916cd43dd8448eb211c80319c")
	scn.evidence.failKind = EvidenceKindTerminalWritten
	driver := scn.driver(t)

	_, err := driver.Resume(context.Background(), scn.req)
	if err == nil {
		t.Fatal("Resume succeeded despite a TERMINAL_WRITTEN recording failure")
	}
	for _, e := range scn.evidence.Entries() {
		if e.kind == EvidenceKindTerminalWritten {
			t.Fatalf("failed TERMINAL_WRITTEN recording left an entry: %+v", e)
		}
	}
}

// TestTodo_Unit5_ApprovalCompletionEvidenceJoinsAdvance proves the
// CompleteApproval path records its APPROVAL_COMPLETED entry on the advance
// transaction too: the entry names the completed work item and the advanced
// node, its digest is the advancement's own output digest, its id surfaces
// on the result, and it was recorded in-transaction.
func TestTodo_Unit5_ApprovalCompletionEvidenceJoinsAdvance(t *testing.T) {
	f := newWork006Fixture(t)
	authority := &work006Authority{allowed: true}
	ev := &fakeEvidence{}
	d, err := New(Options{
		DB: f.conn, Steps: work006EndRunner{}, Advance: nil,
		Terminal: work006Terminal{}, Guard: idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 24 * time.Hour, RetryWindow: time.Hour},
		Clock:     func() time.Time { return f.at.Add(time.Minute) },
		Evidence:  ev,
	})
	if err != nil {
		t.Fatalf("New driver: %v", err)
	}
	result, err := d.CompleteApproval(context.Background(), f.request(authority))
	if err != nil {
		t.Fatalf("CompleteApproval: %v", err)
	}
	entries := ev.Entries()
	if len(entries) != 2 {
		t.Fatalf("evidence entries = %d, want APPROVAL_COMPLETED then TERMINAL_WRITTEN", len(entries))
	}
	entry := entries[0]
	if entry.kind != EvidenceKindApprovalCompleted {
		t.Fatalf("entry kind = %s, want %s", entry.kind, EvidenceKindApprovalCompleted)
	}
	if entry.instanceID != f.instanceID.String() {
		t.Fatalf("entry instance = %s, want %s", entry.instanceID, f.instanceID)
	}
	if entry.refID != f.item.WorkItemID.String() {
		t.Fatalf("entry ref = %s, want the completed work item %s", entry.refID, f.item.WorkItemID)
	}
	if len(result.Result.Advances) == 0 {
		t.Fatal("no advances recorded")
	}
	advanced := result.Result.Advances[0]
	if entry.nodeID != advanced.NodeID || entry.digest != advanced.OutputDigest || entry.digest == "" {
		t.Fatalf("entry = %+v, want node %s with the advancement's output digest %q",
			entry, advanced.NodeID, advanced.OutputDigest)
	}
	if entries[1].kind != EvidenceKindTerminalWritten {
		t.Fatalf("second entry kind = %s, want %s", entries[1].kind, EvidenceKindTerminalWritten)
	}
	for _, e := range entries {
		if e.tx == nil {
			t.Fatalf("%s entry recorded off-transaction", e.kind)
		}
	}
	if len(result.Result.EvidenceIDs) != 2 {
		t.Fatalf("result evidence ids = %v, want both entry ids", result.Result.EvidenceIDs)
	}
}

// legacyOnlyEvidence implements ExecutionEvidence without the atomic half:
// the shape every port had before Unit 5.
type legacyOnlyEvidence struct{}

func (legacyOnlyEvidence) RecordExecutionEvidence(_ context.Context, _ uuid.UUID, _, _, _, _, _ string, _ time.Time) (string, error) {
	return "", nil
}

// TestTodo_Unit5_NonTransactionalPortRefusesAdvance proves fail-closed: a
// port that cannot record on the advance transaction refuses the advance
// with ErrEvidenceNotTransactional instead of splitting the entry onto a
// second transaction where it could commit without its outcome.
func TestTodo_Unit5_NonTransactionalPortRefusesAdvance(t *testing.T) {
	scn := newOBSScenario(t, "0af7651916cd43dd8448eb211c80319c")
	scn.evidenceOverride = legacyOnlyEvidence{}
	driver := scn.driver(t)

	_, err := driver.Resume(context.Background(), scn.req)
	if !errors.Is(err, ErrEvidenceNotTransactional) {
		t.Fatalf("Resume err = %v, want %v", err, ErrEvidenceNotTransactional)
	}
}
