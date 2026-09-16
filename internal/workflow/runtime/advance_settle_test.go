package runtime_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestAdvanceSettleAsRecordsSkippedOrOverriddenOnTheDeclaredRoute proves the
// WF-RUN-015 settle status: a routed completion may settle the node SKIPPED
// (or OVERRIDDEN) instead of SUCCEEDED while the declared route still
// activates the successor, and a settle status on anything but a routed
// completion, or outside that vocabulary, is refused before any write.
func TestAdvanceSettleAsRecordsSkippedOrOverriddenOnTheDeclaredRoute(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "advance-settle-as")
	pf := newPromotionFixture(t, values.TenantId("advance-settle-as-tenant"), "intent:advance-settle-as")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-settle-as"))

	base := runtime.AdvanceRequest{
		TenantID: tenantID, InstanceID: start.InstanceID,
		ExpectedInstanceVersion: start.InstanceVersion, Attempt: 1, Plan: pf.Plan,
		Outcome: frontier.NodeOutcome{
			NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded,
			OutputDigest: "sha256:intervention-skip",
		},
		RecordedAt: fixedInstant, Sink: runtime.NewMemorySink(),
	}

	for name, mutate := range map[string]func(*runtime.AdvanceRequest){
		"undeclared settle status": func(r *runtime.AdvanceRequest) { r.SettleAs = runtime.NodeCancelled },
		"settle on a failure": func(r *runtime.AdvanceRequest) {
			r.SettleAs = runtime.NodeSkipped
			r.Outcome.Outcome = ""
			r.Outcome.Failed = true
		},
	} {
		req := base
		mutate(&req)
		if _, err := advanceOnce(t, conn, tenantID, req); runtime.CodeOf(err) != runtime.CodeInvalidRecord {
			t.Fatalf("%s: code = %q, want %q (%v)", name, runtime.CodeOf(err), runtime.CodeInvalidRecord, err)
		}
	}

	skip := base
	skip.SettleAs = runtime.NodeSkipped
	receipt, err := advanceOnce(t, conn, tenantID, skip)
	if err != nil {
		t.Fatalf("settle-as Advance: %v", err)
	}
	if receipt.CompletedState != string(runtime.NodeSkipped) || len(receipt.Frontier) != 1 || receipt.Frontier[0] != workflow.PromotionNodeSimulateComp {
		t.Fatalf("receipt = %+v, want SKIPPED with the declared successor on the frontier", receipt)
	}
	var status string
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		n, err := (runtime.Store{}).LoadNodeExecution(t.Context(), tx, tenantID, start.InstanceID, workflow.PromotionNodeSnapshotWorker, 1)
		status = string(n.Status)
		return err
	})
	if status != string(runtime.NodeSkipped) {
		t.Fatalf("durable node status = %s, want SKIPPED", status)
	}
	// The identical request replays its receipt: SettleAs is part of the
	// request identity, so a different settle status is not the same request.
	if replay, err := advanceOnce(t, conn, tenantID, skip); err != nil || replay.Digest() != receipt.Digest() {
		t.Fatalf("replay = %+v, %v; want the recorded receipt", replay, err)
	}
	override := skip
	override.SettleAs = runtime.NodeOverridden
	if _, err := advanceOnce(t, conn, tenantID, override); runtime.CodeOf(err) != runtime.CodeStaleInstance {
		t.Fatalf("different settle status on the same version: code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeStaleInstance, err)
	}
}
