package intent

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

func TestTodo_INTENT_007(t *testing.T) {
	def := Definition{Ref: Ref{TypeID: "promotion", Version: 1}}
	inst := Instance{IntentID: "intent-1", Definition: def.Ref, Lifecycle: lifecycle.Dimensions{
		Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionNotPlanned,
		Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyPendingObservation,
		Obligation: lifecycle.ObligationPending,
	}, InstanceVersion: 4}
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	receipt := OutcomeReceipt{IntentID: "intent-1", WorkflowInstanceID: "workflow-1", TerminalCode: "PROMOTION_REPAIR_REQUIRED",
		Dimensions: lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionRepairRequired,
			Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyDegraded, Obligation: lifecycle.ObligationPending},
		Reconciliation: ReconciliationRepairRequired, RepairRef: "repair.promotion.execute/v1:workflow-1", RecordedAt: at}
	if err := BindOutcome(&inst, def, receipt); err != nil {
		t.Fatalf("BindOutcome: %v", err)
	}
	if inst.RepairRef != receipt.RepairRef {
		t.Fatalf("bound repair ref = %q, want %q", inst.RepairRef, receipt.RepairRef)
	}
	if err := lifecycle.Check(inst.Lifecycle, inst.LifecycleContext(def)); err != nil {
		t.Fatalf("bound REPAIR_REQUIRED tuple must re-validate on read: %v", err)
	}
	if inst.Lifecycle.Business != lifecycle.BusinessCompleted || inst.Lifecycle.Consistency != lifecycle.ConsistencyDegraded ||
		inst.Lifecycle.Execution != lifecycle.ExecutionRepairRequired {
		t.Fatalf("bound lifecycle = %s", inst.Lifecycle)
	}
	if inst.InstanceVersion != 5 {
		t.Fatalf("instance version = %d, want 5", inst.InstanceVersion)
	}
	if err := BindOutcome(&inst, def, receipt); err != nil {
		t.Fatalf("duplicate identical outcome: %v", err)
	}
	conflict := receipt
	conflict.Dimensions.Consistency = lifecycle.ConsistencyConsistent
	conflict.Reconciliation = ReconciliationPass
	if err := BindOutcome(&inst, def, conflict); !errors.Is(err, ErrOutcomeConflict) {
		t.Fatalf("conflicting duplicate error = %v, want ErrOutcomeConflict", err)
	}
}

func TestTodo_LEGAL_014_ObligationEvidence(t *testing.T) {
	def := Definition{Ref: Ref{TypeID: "promotion", Version: 1}}
	revision := ProposalRevision{ProposalRevisionID: "proposal-1", MaterialDigest: digest.Reference{Digest: "material-1"}, Obligations: []Obligation{{ObligationID: "notice", Kind: "NOTICE"}}}
	base := Instance{IntentID: "intent-1", Definition: def.Ref, ProposalRevisions: []ProposalRevision{revision}, Lifecycle: lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionNotPlanned, Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyPendingObservation, Obligation: lifecycle.ObligationPending}, InstanceVersion: 1}
	receipt := OutcomeReceipt{IntentID: base.IntentID, WorkflowInstanceID: "workflow-1", ProposalRevisionID: revision.ProposalRevisionID, MaterialDigest: revision.MaterialDigest.Digest, TerminalCode: "BLOCKED", Dimensions: lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionBlocked, Business: lifecycle.BusinessNotAchieved, Consistency: lifecycle.ConsistencyUnknown, Obligation: lifecycle.ObligationPending}, Reconciliation: ReconciliationUnknown, RecordedAt: time.Unix(1, 0).UTC()}
	if err := BindOutcome(&base, def, receipt); !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("missing evaluation = %v", err)
	}
	obligation := LegalBoundObligation{Type: "NOTICE", ID: "notice", BodyDigest: "notice-body"}
	receipt.LegalEvidence = &LegalObligationEvidence{ReceiptRef: "legal:1", ReceiptDigest: "receipt-digest", BindingDigest: "binding-digest", ProposalRevisionID: revision.ProposalRevisionID, MaterialDigest: revision.MaterialDigest.Digest, AppliedObligations: []LegalBoundObligation{obligation}}
	if err := BindOutcome(&base, def, receipt); err != nil {
		t.Fatalf("pending with evaluation: %v", err)
	}
	if base.LegalEvidence == nil || base.LegalEvidence.BindingDigest != "binding-digest" {
		t.Fatalf("binding not retained: %+v", base.LegalEvidence)
	}

	satisfied := receipt
	satisfied.Dimensions.Obligation = lifecycle.ObligationSatisfied
	other := Instance{IntentID: "intent-1", Definition: def.Ref, ProposalRevisions: []ProposalRevision{revision}, Lifecycle: lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionNotPlanned, Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyPendingObservation, Obligation: lifecycle.ObligationPending}, InstanceVersion: 1}
	if err := BindOutcome(&other, def, satisfied); !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("evaluation used as discharge = %v", err)
	}
	satisfied.LegalEvidence.Discharges = []LegalObligationDischarge{{Obligation: obligation, EvidenceRefs: []string{"discharge:1"}}}
	if err := BindOutcome(&other, def, satisfied); err != nil {
		t.Fatalf("satisfied with discharge: %v", err)
	}
}

func TestTodo_INTENT_007_Race(t *testing.T) {
	def := Definition{Ref: Ref{TypeID: "promotion", Version: 1}}
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	receipt := OutcomeReceipt{IntentID: "intent-1", WorkflowInstanceID: "workflow-1", TerminalCode: "DONE",
		Dimensions: lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionCommitted,
			Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyConsistent, Obligation: lifecycle.ObligationSatisfied},
		Reconciliation: ReconciliationPass, CommitReceiptRef: "receipt.promotion.execute/v1:workflow-1", RecordedAt: at}
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inst := Instance{IntentID: "intent-1", Definition: def.Ref, Lifecycle: lifecycle.Dimensions{
				Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionNotPlanned,
				Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyPendingObservation,
				Obligation: lifecycle.ObligationPending}, InstanceVersion: 1}
			errs <- BindOutcome(&inst, def, receipt)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent outcome binding: %v", err)
		}
	}
}

func TestTodo_INTENT_007_Mutation(t *testing.T) {
	def := Definition{Ref: Ref{TypeID: "promotion", Version: 1}}
	inst := Instance{IntentID: "intent-1", Definition: def.Ref}
	receipt := OutcomeReceipt{IntentID: "intent-1", WorkflowInstanceID: "workflow-1", TerminalCode: "DONE",
		Dimensions: lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionCommitted,
			Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyConsistent, Obligation: lifecycle.ObligationSatisfied},
		Reconciliation: ReconciliationPass, RecordedAt: time.Now().UTC()}
	if err := BindOutcome(&inst, def, receipt); !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("invalid unspecified tuple error = %v, want ErrInvalidOutcome", err)
	}
}

// TestOutcomeReceiptRequiresTerminalRefs pins the rule that made a committed
// intent unreadable: the receipt must carry the reference the lifecycle
// legality rules demand, and binding it must leave the instance re-validatable.
func TestOutcomeReceiptRequiresTerminalRefs(t *testing.T) {
	def := Definition{Ref: Ref{TypeID: "promotion", Version: 1}}
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	committed := OutcomeReceipt{IntentID: "intent-1", WorkflowInstanceID: "workflow-1", TerminalCode: "DONE",
		Dimensions: lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionCommitted,
			Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyConsistent, Obligation: lifecycle.ObligationSatisfied},
		Reconciliation: ReconciliationPass, RecordedAt: at}
	if err := committed.Validate(); !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("COMMITTED without a receipt ref = %v, want ErrInvalidOutcome", err)
	}
	committed.CommitReceiptRef = "receipt.promotion.execute/v1:workflow-1"
	if err := committed.Validate(); err != nil {
		t.Fatalf("COMMITTED with a receipt ref: %v", err)
	}
	inst := Instance{IntentID: "intent-1", Definition: def.Ref, Lifecycle: lifecycle.Dimensions{
		Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionNotPlanned,
		Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyPendingObservation,
		Obligation: lifecycle.ObligationPending}, InstanceVersion: 1}
	if err := lifecycle.Check(committed.Dimensions, inst.LifecycleContext(def)); err == nil {
		t.Fatal("a COMMITTED tuple must be illegal on an instance without a bound receipt")
	}
	if err := BindOutcome(&inst, def, committed); err != nil {
		t.Fatalf("BindOutcome: %v", err)
	}
	if inst.CommitReceiptRef != committed.CommitReceiptRef {
		t.Fatalf("bound commit receipt ref = %q, want %q", inst.CommitReceiptRef, committed.CommitReceiptRef)
	}
	if err := lifecycle.Check(inst.Lifecycle, inst.LifecycleContext(def)); err != nil {
		t.Fatalf("bound COMMITTED tuple must re-validate on read: %v", err)
	}
	repair := committed
	repair.CommitReceiptRef = ""
	repair.Dimensions.Execution = lifecycle.ExecutionRepairRequired
	repair.Dimensions.Consistency = lifecycle.ConsistencyDegraded
	repair.Reconciliation = ReconciliationRepairRequired
	if err := repair.Validate(); !errors.Is(err, ErrInvalidOutcome) {
		t.Fatalf("REPAIR_REQUIRED without a repair ref = %v, want ErrInvalidOutcome", err)
	}
}
