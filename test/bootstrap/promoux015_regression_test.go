package bootstrap_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// TestTodo_PROMOUX_015_Integration is the bounded multi-persona promotion
// regression. It drives the composed, PostgreSQL-backed journey engine from
// seeded workforce data through proposal, execution, routed approval, and one
// terminal outcome. The test uses a real PostgreSQL-backed composed cell, but
// calls the engine in process; it does not exercise the browser or endpoints.
//
// The shipped journey graph currently has one routed approval requirement.
// Finance therefore proves the negative authorization boundary and the
// assigned manager proves the positive one; this test does not claim two
// independent approval gates.
func TestTodo_PROMOUX_015_Integration(t *testing.T) {
	h := newJourneyHarness(t)
	proposer := h.operatorCtx(t)

	workers, _, err := h.engine.ListWorkers(proposer)
	if err != nil {
		t.Fatalf("ListWorkers(proposer): %v", err)
	}
	var eligible *workspace.WorkerSummary
	for i := range workers {
		if workers[i].WorkerRef == "omar-reyes" {
			eligible = &workers[i]
			break
		}
	}
	if eligible == nil {
		t.Fatalf("discoverable eligible worker omar-reyes absent from %d workers", len(workers))
	}

	proposed, err := h.engine.Propose(proposer, journeyProposalFor(eligible.WorkerRef))
	if err != nil {
		t.Fatalf("Propose(%s): %v", eligible.WorkerRef, err)
	}
	if proposed.Stage != workspace.JourneyStageProposed || proposed.MaterialDigest == "" || proposed.ProposalRevisionID == "" {
		t.Fatalf("proposal = %+v, want PROPOSED with immutable revision and digest", proposed)
	}
	if proposed.WorkerName == "" || proposed.ProposedBase != "98000.00" || proposed.EffectiveDate != "2026-06-01" {
		t.Fatalf("proposal omitted governed presentation facts: %+v", proposed)
	}

	beforeExecute, err := h.engine.Inspect(proposer, proposed.IntentID)
	if err != nil {
		t.Fatalf("Inspect(proposed): %v", err)
	}
	if beforeExecute.Instance != nil || beforeExecute.Summary.Stage != workspace.JourneyStageProposed {
		t.Fatalf("proposal unexpectedly has runtime state: %+v", beforeExecute)
	}

	executed, err := h.engine.Execute(proposer, proposed.IntentID)
	if err != nil {
		t.Fatalf("Execute(proposer): %v", err)
	}
	if executed.Summary.Stage != workspace.JourneyStageAwaitingApproval || executed.Instance == nil {
		t.Fatalf("executed journey = %+v, want AWAITING_APPROVAL with instance", executed.Summary)
	}
	if executed.Approver != "Assigned reviewer" || len(executed.WorkItems) != 1 {
		t.Fatalf("approval presentation = approver %q, work items %d; want Assigned reviewer and one item", executed.Approver, len(executed.WorkItems))
	}
	if executed.WorkItems[0].OwnerRef != journeyApprover {
		t.Fatalf("approval owner = %q, want routed principal %q", executed.WorkItems[0].OwnerRef, journeyApprover)
	}

	finance := h.ctxAs(t, "principal:finance-approver", journeyOperatorRoles()...)
	beforeRefusal := snapshotDatabase(t, h.cell)
	if _, err := h.engine.Decide(finance, proposed.IntentID, workspace.Decision{Approve: true, Reason: "finance-approved"}); err == nil || !strings.Contains(err.Error(), "not the routed approver") {
		t.Fatalf("Decide(finance): %v, want routed-approver authorization refusal", err)
	}
	assertSnapshotsEqual(t, beforeRefusal, snapshotDatabase(t, h.cell))
	stillWaiting, err := h.engine.Inspect(proposer, proposed.IntentID)
	if err != nil {
		t.Fatalf("Inspect(after finance refusal): %v", err)
	}
	if stillWaiting.Summary.Stage != workspace.JourneyStageAwaitingApproval || stillWaiting.Instance == nil || len(stillWaiting.WorkItems) != 1 {
		t.Fatalf("finance refusal changed approval state: %+v", stillWaiting.Summary)
	}
	// An employee has neither proposal nor decision authority. This probes a
	// separate persona rather than relying on a hidden UI omission.
	employee := h.ctxAs(t, "principal:employee", "employee")
	if _, err := h.engine.Inspect(employee, proposed.IntentID); !errors.Is(err, workspace.ErrDenied) {
		t.Fatalf("Inspect(employee): %v, want authorization refusal", err)
	}

	completed, err := h.engine.Decide(h.approverCtx(t), proposed.IntentID, workspace.Decision{
		Approve: true, Reason: "manager-approved-promotion",
	})
	if err != nil {
		t.Fatalf("Decide(manager): %v", err)
	}
	if completed.Summary.Stage != workspace.JourneyStageCompleted || completed.Instance == nil || completed.Instance.Status != "COMPLETED" {
		t.Fatalf("completed journey = %+v, want terminal COMPLETED", completed.Summary)
	}
	if completed.Ledger == nil || completed.Ledger.Sequence != 1 || completed.Ledger.Digest == "" {
		t.Fatalf("terminal ledger = %+v, want one governed fact", completed.Ledger)
	}
	if got := ledgerEventsOn(t, h.cell, completed.Instance.InstanceID); got != 1 {
		t.Fatalf("ledger events for completed journey = %d, want exactly one", got)
	}

	history, err := h.engine.ListJourneys(proposer)
	if err != nil {
		t.Fatalf("ListJourneys(after completion): %v", err)
	}
	found := false
	for _, item := range history {
		if item.IntentID == proposed.IntentID {
			found = true
			if item.Stage != workspace.JourneyStageCompleted || item.InstanceID != completed.Instance.InstanceID {
				t.Fatalf("history item = %+v, want completed instance %s", item, completed.Instance.InstanceID)
			}
		}
	}
	if !found {
		t.Fatalf("completed promotion %s absent from history", proposed.IntentID)
	}

	// The terminal detail remains business-facing; raw workflow machinery is
	// represented only by typed state, not leaked into the human reason text.
	for _, event := range completed.Timeline {
		if strings.Contains(event.Title, "workflow") || strings.Contains(event.Detail, "JourneyService") {
			t.Errorf("terminal timeline leaks machinery: %+v", event)
		}
	}
}
