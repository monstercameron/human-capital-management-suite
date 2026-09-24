package simulate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// mustSetup builds a ready-to-run promotion simulation, failing the test with
// the whole compile diagnostic set if the reference workflow stops publishing.
func mustSetup(t *testing.T, proposedBasePay string) *simulate.PromotionSetup {
	t.Helper()
	setup, err := simulate.NewPromotionSetup(proposedBasePay)
	if err != nil {
		t.Fatalf("promotion setup: %v", err)
	}
	return setup
}

// mustRun walks the plan and fails on any refusal.
func mustRun(t *testing.T, setup *simulate.PromotionSetup) simulate.Receipt {
	t.Helper()
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return receipt
}

// TestSimulateModeExecutesThePromotionReferenceWithZeroEffects is the golden
// end-to-end claim: the compiled reference workflow is walked node by node
// through real domain calculations, reaches a stated terminal with all five
// lifecycle dimensions, and every effect counter is zero.
func TestSimulateModeExecutesThePromotionReferenceWithZeroEffects(t *testing.T) {
	setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
	receipt := mustRun(t, setup)

	if receipt.Mode != workflow.ModeSimulate {
		t.Errorf("mode = %s, want %s", receipt.Mode, workflow.ModeSimulate)
	}
	if receipt.WorkflowID != workflow.PromotionWorkflowID {
		t.Errorf("workflow id = %q, want %q", receipt.WorkflowID, workflow.PromotionWorkflowID)
	}
	if receipt.PlanDigest != setup.Plan.Digest() {
		t.Errorf("receipt pins plan %q, plan digest is %q", receipt.PlanDigest, setup.Plan.Digest())
	}

	// The reference promotion changes Jane's grade, and the reference
	// threshold table escalates every grade change to FINANCE_REQUIRED. The
	// walk therefore takes the EXCEEDS_THRESHOLD branch to the terminal that
	// reports a pending Finance Partner approval, which is the outcome
	// promote-into-management.md describes.
	wantPath := []string{
		workflow.PromotionNodeSnapshotWorker,
		workflow.PromotionNodeSimulateComp,
		workflow.PromotionNodeEvaluateBand,
		workflow.PromotionNodeBuildProposal,
		workflow.PromotionNodeRaiseThreshold,
		workflow.PromotionNodeEndApproval,
	}
	got := receipt.NodeIDs()
	if len(got) != len(wantPath) {
		t.Fatalf("walked %d nodes %v, want %d %v", len(got), got, len(wantPath), wantPath)
	}
	for i := range wantPath {
		if got[i] != wantPath[i] {
			t.Fatalf("node %d = %q, want %q (whole path %v)", i, got[i], wantPath[i], got)
		}
	}

	// Every executed node records the evidence its step type declares, and
	// every reference resolves to a real identity rather than a placeholder.
	for _, entry := range receipt.Trace {
		node, ok := setup.Plan.Node(entry.NodeID)
		if !ok {
			t.Fatalf("trace names node %q, which the plan does not declare", entry.NodeID)
		}
		if len(entry.Evidence) != len(node.EvidenceRefs) {
			t.Errorf("node %s recorded %d evidence entries, want %d (%v)",
				entry.NodeID, len(entry.Evidence), len(node.EvidenceRefs), node.EvidenceRefs)
		}
		for i, ev := range entry.Evidence {
			if i < len(node.EvidenceRefs) && ev.Ref != node.EvidenceRefs[i] {
				t.Errorf("node %s evidence %d is %q, want %q", entry.NodeID, i, ev.Ref, node.EvidenceRefs[i])
			}
			if ev.ID == "" {
				t.Errorf("node %s evidence %q has no identity", entry.NodeID, ev.Ref)
			}
		}
		if entry.InputDigest == "" || entry.OutputDigest == "" {
			t.Errorf("node %s recorded input digest %q output digest %q", entry.NodeID, entry.InputDigest, entry.OutputDigest)
		}
		if entry.EffectClass.IsWrite() {
			t.Errorf("node %s executed with write effect class %s", entry.NodeID, entry.EffectClass)
		}
	}

	if receipt.Terminal.NodeID != workflow.PromotionNodeEndApproval {
		t.Fatalf("terminal = %q, want %q", receipt.Terminal.NodeID, workflow.PromotionNodeEndApproval)
	}
	if receipt.Terminal.TerminalCode != "SIMULATION_APPROVAL_REQUIRED" {
		t.Errorf("terminal code = %q, want SIMULATION_APPROVAL_REQUIRED", receipt.Terminal.TerminalCode)
	}
	// The outstanding Finance Partner approval is reported as outstanding, not
	// collapsed into a discharged obligation.
	if len(receipt.Terminal.OutstandingObligationRefs) != 1 ||
		receipt.Terminal.OutstandingObligationRefs[0] != workflow.PromotionObligationApproval {
		t.Errorf("outstanding obligations = %v, want [%s]",
			receipt.Terminal.OutstandingObligationRefs, workflow.PromotionObligationApproval)
	}
	want := simulate.LifecycleState{
		RequestState:     "SIMULATED",
		ExecutionState:   "NOT_PLANNED",
		BusinessState:    "NOT_STARTED",
		ConsistencyState: "NOT_APPLICABLE",
		ObligationState:  "PENDING",
	}
	if receipt.Lifecycle != want {
		t.Errorf("lifecycle = %+v, want %+v", receipt.Lifecycle, want)
	}

	counters := receipt.EffectCounters()
	if !counters.IsZero() {
		t.Fatalf("simulation counted effects: %v", counters.NonZero())
	}
	if err := receipt.ZeroEffect.Validate(); err != nil {
		t.Fatalf("zero-effect receipt: %v", err)
	}
	if receipt.ZeroEffect.ExecutionState != "NOT_PLANNED" {
		t.Errorf("zero-effect receipt execution state = %q, want NOT_PLANNED", receipt.ZeroEffect.ExecutionState)
	}
	if len(receipt.ZeroEffect.Controls) == 0 {
		t.Error("zero-effect receipt pins no control versions")
	}

	// Virtual time advanced exactly one declared step per node, and no wall
	// clock was consulted anywhere.
	wantElapsed := simulate.DefaultStepDuration * time.Duration(len(wantPath))
	if receipt.Elapsed != wantElapsed {
		t.Errorf("elapsed virtual time = %s, want %s", receipt.Elapsed, wantElapsed)
	}
	if !receipt.StartedAt.Equal(simulate.DefaultClockStart) {
		t.Errorf("started at %s, want the fixed clock start %s", receipt.StartedAt, simulate.DefaultClockStart)
	}

	// The standard tier still awaits the reference workflow's constant
	// baseline approval graph, and none of it was decided.
	if len(receipt.WorkItems) == 0 {
		t.Fatal("no work item was recorded; the promotion still requires human approval")
	}
	for _, item := range receipt.WorkItems {
		if item.State != simulate.WouldAwait {
			t.Errorf("work item %s state = %q, want %q", item.RequirementID, item.State, simulate.WouldAwait)
		}
		if item.Outcome != "RESOLVED" {
			t.Errorf("work item %s outcome = %q, want RESOLVED", item.RequirementID, item.Outcome)
		}
		if len(item.Candidates) == 0 {
			t.Errorf("work item %s resolved no candidate approver", item.RequirementID)
		}
	}
}

// TestSimulateModeRefusesWriteClassNodes is the structural proof that a
// write-class node cannot execute in SIMULATE mode.
//
// The fixture plan is compiled under P1B precisely because P1A refuses a write
// effect at compile time; a plan that could not exist would prove nothing
// about the interpreter. The refusal here is the interpreter's own, before any
// node runs, and the handler behind the mutating capability fails the test if
// it is ever reached.
func TestSimulateModeRefusesWriteClassNodes(t *testing.T) {
	invoked := false
	plan := mustCompileMutatingPlan(t, func() { invoked = true })

	if err := simulate.Admit(plan); err == nil {
		t.Fatal("Admit accepted a plan containing a write-class node")
	} else if code := simulate.CodeOf(err); code != simulate.CodeWriteEffectInSimulate {
		t.Fatalf("Admit refusal code = %q, want %q (%v)", code, simulate.CodeWriteEffectInSimulate, err)
	}

	registry := mutatingRegistry(t, func() { invoked = true })
	_, err := simulate.Run(context.Background(), plan, simulate.Inputs{
		Values: simulate.Bag{"worker_id": simulate.NewBranded("WorkerID", "11111111-1111-4111-8111-111111111111")},
	}, simulate.Options{Capabilities: registry})
	if err == nil {
		t.Fatal("Run executed a plan containing a write-class node")
	}
	if !errors.Is(err, simulate.ErrSimulate) {
		t.Errorf("refusal does not unwrap to ErrSimulate: %v", err)
	}
	if code := simulate.CodeOf(err); code != simulate.CodeWriteEffectInSimulate {
		t.Fatalf("Run refusal code = %q, want %q (%v)", code, simulate.CodeWriteEffectInSimulate, err)
	}
	if invoked {
		t.Fatal("the mutating capability handler was reached; SIMULATE must refuse, not suppress")
	}

	// The governed gateway refuses the same capability independently, so the
	// interpreter's refusal is not the only thing standing between a
	// simulation and a mutation.
	sink := &countingSink{}
	gateway := capability.NewGateway(registry, sink)
	if _, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability:    capability.Key{ID: capSyncPayroll, Version: 1},
		Payload:       simulate.CapabilityRequest{NodeID: fxSync},
		Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{"scope:payroll.write"}},
	}); err == nil {
		t.Fatal("the governed gateway invoked a write-effect capability")
	}
	if invoked {
		t.Fatal("the gateway reached the mutating handler")
	}
}

// TestSimulateReceiptIsByteStable proves the receipt digest is a content
// identity and not a timestamp: two independent runs of the same plan with the
// same inputs, in separate environments, produce identical bytes.
func TestSimulateReceiptIsByteStable(t *testing.T) {
	first := mustRun(t, mustSetup(t, simulate.PromotionWithinThresholdPay))
	second := mustRun(t, mustSetup(t, simulate.PromotionWithinThresholdPay))

	if first.Digest() != second.Digest() {
		t.Fatalf("receipt digest drifted between runs:\n first  %s\n second %s", first.Digest(), second.Digest())
	}
	if err := first.Verify(); err != nil {
		t.Errorf("first receipt does not verify: %v", err)
	}
	if err := second.Verify(); err != nil {
		t.Errorf("second receipt does not verify: %v", err)
	}

	firstJSON, err := first.JSON()
	if err != nil {
		t.Fatalf("render first receipt: %v", err)
	}
	secondJSON, err := second.JSON()
	if err != nil {
		t.Fatalf("render second receipt: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("rendered receipts differ between two runs of the same plan and inputs")
	}

	// A different proposal is a different receipt: byte stability must not be
	// stability against the inputs actually changing.
	other := mustRun(t, mustSetup(t, simulate.PromotionExceedsThresholdPay))
	if other.Digest() == first.Digest() {
		t.Fatal("a larger raise produced the same receipt digest")
	}
}

// countingSink records gateway decisions without minting anything meaningful.
type countingSink struct{ n int }

func (s *countingSink) RecordInvocation(_ context.Context, _ capability.InvocationEvidence) (string, error) {
	s.n++
	return "ev:test", nil
}

func (s *countingSink) RecordInvocationTx(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return s.RecordInvocation(ctx, evt)
}
