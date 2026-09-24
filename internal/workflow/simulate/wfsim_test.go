package simulate_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden receipts")

// golden compares rendered bytes against a checked-in file, or rewrites it
// under -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s drifted\n got:\n%s\nwant:\n%s", path, got, want)
	}
}

// TestTodo_WF_SIM_001_GoldenPromotionReceipt pins the whole receipt of the
// reference run, byte for byte.
//
// A digest assertion alone would only prove the receipt is stable; pinning the
// rendered artifact is what makes a change to the trace, the evidence, the
// work items or the lifecycle tuple visible in review rather than merely
// visible as a changed hash.
func TestTodo_WF_SIM_001_GoldenPromotionReceipt(t *testing.T) {
	receipt := mustRun(t, mustSetup(t, simulate.PromotionWithinThresholdPay))
	rendered, err := receipt.JSON()
	if err != nil {
		t.Fatalf("render receipt: %v", err)
	}
	golden(t, "promotion-receipt.json", rendered)
}

// TestTodo_WF_SIM_002_GoldenObservationBranchReceipt pins the branch the
// management promotion never takes: an in-grade increase stays under the
// threshold, so the drift observation runs and the walk ends consistent.
func TestTodo_WF_SIM_002_GoldenObservationBranchReceipt(t *testing.T) {
	setup, err := simulate.NewInGradeSetup(simulate.PromotionWithinThresholdPay)
	if err != nil {
		t.Fatalf("in-grade setup: %v", err)
	}
	receipt := mustRun(t, setup)

	if got := receipt.Terminal.NodeID; got != workflow.PromotionNodeEndSimulated {
		t.Fatalf("terminal = %q, want %q", got, workflow.PromotionNodeEndSimulated)
	}
	observation, ok := receipt.Node(workflow.PromotionNodeObserveDrift)
	if !ok {
		t.Fatal("the observation node did not run")
	}
	if observation.Outcome != workflow.OutcomePass {
		t.Errorf("observation outcome = %q, want PASS", observation.Outcome)
	}
	// A PASS observation must cite the source watermark it was taken at; an
	// observation with no position is not evidence of business state.
	watermark := ""
	for _, ev := range observation.Evidence {
		if ev.Ref == "source_watermark" {
			watermark = ev.ID
		}
	}
	if watermark == "" {
		t.Error("a passing observation recorded no source watermark")
	}

	rendered, err := receipt.JSON()
	if err != nil {
		t.Fatalf("render receipt: %v", err)
	}
	golden(t, "in-grade-receipt.json", rendered)
}

// TestTodo_WF_SIM_003_PropertyTraceIsAWalkOfTheCompiledGraph proves the trace
// is a real path through the compiled plan rather than a list of nodes that
// happened to run: it starts at the declared start node, every hop follows a
// declared edge whose route key is the outcome the node produced, and it ends
// at a compiled terminal.
func TestTodo_WF_SIM_003_PropertyTraceIsAWalkOfTheCompiledGraph(t *testing.T) {
	for _, pay := range []string{simulate.PromotionWithinThresholdPay, simulate.PromotionExceedsThresholdPay} {
		setup := mustSetup(t, pay)
		receipt := mustRun(t, setup)

		if receipt.Trace[0].NodeID != setup.Plan.StartNodeID {
			t.Fatalf("pay %s: trace starts at %q, plan starts at %q",
				pay, receipt.Trace[0].NodeID, setup.Plan.StartNodeID)
		}
		for i, entry := range receipt.Trace {
			if entry.Order != i+1 {
				t.Errorf("pay %s: entry %d declares order %d", pay, i, entry.Order)
			}
			if i == len(receipt.Trace)-1 {
				if entry.Type != workflow.StepEnd {
					t.Errorf("pay %s: walk stopped at %s %q, which is not an END", pay, entry.Type, entry.NodeID)
				}
				break
			}
			edge := false
			for _, e := range setup.Plan.Edges {
				if e.From == entry.NodeID && e.RouteKey == entry.RouteKey && e.To == receipt.Trace[i+1].NodeID {
					edge = true
					break
				}
			}
			if !edge {
				t.Errorf("pay %s: hop %s --%s--> %s follows no declared edge",
					pay, entry.NodeID, entry.RouteKey, receipt.Trace[i+1].NodeID)
			}
			if entry.RouteKey != string(entry.Outcome) {
				t.Errorf("pay %s: node %s produced outcome %q but took route %q",
					pay, entry.NodeID, entry.Outcome, entry.RouteKey)
			}
		}
		if _, ok := setup.Plan.Node(receipt.Terminal.NodeID); !ok {
			t.Errorf("pay %s: terminal %q is not a plan node", pay, receipt.Terminal.NodeID)
		}
	}
}

// TestTodo_WF_SIM_004_PropertyEveryAdmittedNodeIsZeroEffect proves the
// admission gate is total over the reference plan: every node the plan can
// reach is PURE or READ_ONLY and admits SIMULATE, which is why the walk never
// has to decide whether to suppress anything.
func TestTodo_WF_SIM_004_PropertyEveryAdmittedNodeIsZeroEffect(t *testing.T) {
	setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
	if err := simulate.Admit(setup.Plan); err != nil {
		t.Fatalf("the reference plan must be admissible: %v", err)
	}
	if !setup.Plan.Effects.ZeroEffect {
		t.Fatal("the reference plan is not zero-effect")
	}
	for _, node := range setup.Plan.Nodes {
		if node.EffectClass.IsWrite() {
			t.Errorf("node %s declares write class %s", node.ID, node.EffectClass)
		}
		simulateAdmitted := false
		for _, mode := range node.AllowedModes {
			if mode == workflow.ModeSimulate {
				simulateAdmitted = true
			}
		}
		if !simulateAdmitted {
			t.Errorf("node %s does not admit SIMULATE (%v)", node.ID, node.AllowedModes)
		}
	}
}

// TestTodo_WF_SIM_005_PropertyReceiptIgnoresInputInsertionOrder proves the
// receipt digest is a function of the values, not of the order a caller
// happened to build the input map in.
func TestTodo_WF_SIM_005_PropertyReceiptIgnoresInputInsertionOrder(t *testing.T) {
	base := mustSetup(t, simulate.PromotionWithinThresholdPay)
	first := mustRun(t, base)

	permuted := mustSetup(t, simulate.PromotionWithinThresholdPay)
	rebuilt := simulate.Bag{}
	order := []string{"grade_change", "budget_authority", "effective_date", "proposed_base_pay", "target_job_id", "worker_id"}
	for _, path := range order {
		v, err := permuted.Inputs.Values.Get(path)
		if err != nil {
			t.Fatalf("input %s: %v", path, err)
		}
		rebuilt[path] = v
	}
	permuted.Inputs.Values = rebuilt
	second := mustRun(t, permuted)

	if first.Digest() != second.Digest() {
		t.Fatalf("receipt digest depends on input insertion order:\n %s\n %s", first.Digest(), second.Digest())
	}
	if first.InputsDigest != second.InputsDigest {
		t.Errorf("inputs digest depends on insertion order:\n %s\n %s", first.InputsDigest, second.InputsDigest)
	}
}

// TestTodo_WF_SIM_006_FaultObservationDriftRoutesToDegraded proves a
// disagreeing projection is not collapsed into success: the walk takes the
// FAIL route and lands on a terminal that says BLOCKED and UNKNOWN rather than
// completed and consistent.
func TestTodo_WF_SIM_006_FaultObservationDriftRoutesToDegraded(t *testing.T) {
	setup, err := simulate.NewInGradeSetup(simulate.PromotionWithinThresholdPay)
	if err != nil {
		t.Fatalf("in-grade setup: %v", err)
	}
	setup.Options.Reads = simulate.ProjectionReads{Watermark: setup.Env.Watermark, Drifted: true}
	receipt := mustRun(t, setup)

	if receipt.Terminal.NodeID != workflow.PromotionNodeEndDegraded {
		t.Fatalf("terminal = %q, want %q", receipt.Terminal.NodeID, workflow.PromotionNodeEndDegraded)
	}
	if receipt.Terminal.RuntimeStatus != workflow.RuntimeBlocked {
		t.Errorf("runtime status = %q, want BLOCKED", receipt.Terminal.RuntimeStatus)
	}
	want := simulate.LifecycleState{
		RequestState:     "SIMULATED",
		ExecutionState:   "BLOCKED",
		BusinessState:    "UNKNOWN",
		ConsistencyState: "UNKNOWN",
		ObligationState:  "PENDING",
	}
	if receipt.Lifecycle != want {
		t.Errorf("lifecycle = %+v, want %+v", receipt.Lifecycle, want)
	}
	if !receipt.EffectCounters().IsZero() {
		t.Errorf("a degraded run still counted effects: %v", receipt.EffectCounters().NonZero())
	}
}

// TestTodo_WF_SIM_007_FaultUnreadableSourceIsUnknownNotPass proves that a
// source the run could not read is UNKNOWN. A simulation that could not see
// the projection must not report that the projection agreed.
func TestTodo_WF_SIM_007_FaultUnreadableSourceIsUnknownNotPass(t *testing.T) {
	setup, err := simulate.NewInGradeSetup(simulate.PromotionWithinThresholdPay)
	if err != nil {
		t.Fatalf("in-grade setup: %v", err)
	}
	setup.Options.Reads = simulate.ProjectionReads{Unavailable: true}
	receipt := mustRun(t, setup)

	observation, ok := receipt.Node(workflow.PromotionNodeObserveDrift)
	if !ok {
		t.Fatal("the observation node did not run")
	}
	if observation.Outcome != workflow.OutcomeUnknown {
		t.Errorf("observation outcome = %q, want UNKNOWN", observation.Outcome)
	}
	if receipt.Terminal.NodeID != workflow.PromotionNodeEndUnknown {
		t.Errorf("terminal = %q, want %q", receipt.Terminal.NodeID, workflow.PromotionNodeEndUnknown)
	}
}

// TestTodo_WF_SIM_008_FaultUnresolvedBudgetAuthorityBlocks proves an
// unresolved budget authority never resolves to "no finance approval
// required": the threshold table blocks, the decision routes UNKNOWN, and the
// run ends on the unknown terminal.
func TestTodo_WF_SIM_008_FaultUnresolvedBudgetAuthorityBlocks(t *testing.T) {
	setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
	blocked := setup.Options.Decisions.(simulate.RulesDecisions)
	setup.Inputs.Values["budget_authority"] = simulate.NewString(string(rules.BudgetAuthorityUnknown))
	setup.Options.Approvals = simulate.HumanWorkApprovals{
		Decisions:      blocked,
		ProposalNodeID: workflow.PromotionNodeBuildProposal,
	}
	receipt := mustRun(t, setup)

	decision, ok := receipt.Node(workflow.PromotionNodeRaiseThreshold)
	if !ok {
		t.Fatal("the threshold decision did not run")
	}
	if decision.Outcome != workflow.OutcomeUnknown {
		t.Errorf("decision outcome = %q, want UNKNOWN", decision.Outcome)
	}
	if receipt.Terminal.NodeID != workflow.PromotionNodeEndUnknown {
		t.Fatalf("terminal = %q, want %q", receipt.Terminal.NodeID, workflow.PromotionNodeEndUnknown)
	}
	if len(receipt.WorkItems) != 0 {
		t.Errorf("a blocked run raised %d work item(s); the unknown terminal declares no approval requirement",
			len(receipt.WorkItems))
	}
}

// TestTodo_WF_SIM_009_FaultMissingContextTakesTheDeclaredBehavior proves a
// context snapshot the run was not given is neither absent, zero nor false:
// the requirement's declared UNKNOWN behavior routes the node, and the
// capability behind it is never invoked.
func TestTodo_WF_SIM_009_FaultMissingContextTakesTheDeclaredBehavior(t *testing.T) {
	setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
	setup.Inputs.Context = nil
	receipt := mustRun(t, setup)

	snapshot, ok := receipt.Node(workflow.PromotionNodeSnapshotWorker)
	if !ok {
		t.Fatal("the snapshot node did not run")
	}
	if snapshot.Outcome != workflow.OutcomeUnknown {
		t.Errorf("snapshot outcome = %q, want UNKNOWN", snapshot.Outcome)
	}
	if receipt.Terminal.NodeID != workflow.PromotionNodeEndUnknown {
		t.Errorf("terminal = %q, want %q", receipt.Terminal.NodeID, workflow.PromotionNodeEndUnknown)
	}
	if len(receipt.Trace) != 2 {
		t.Errorf("walked %v, want the snapshot and the unknown terminal only", receipt.NodeIDs())
	}
}

// TestTodo_WF_SIM_010_FaultBlockedPreflightFailsTheTransform proves the
// proposal transform does not manufacture a digest for something the promotion
// domain refused: a preflight that is not READY takes the FAILED route.
func TestTodo_WF_SIM_010_FaultBlockedPreflightFailsTheTransform(t *testing.T) {
	setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
	// A promotion with no stated business reason is blocked by the domain's
	// own policy, which is a business refusal and not an interpreter error.
	setup.Env.BusinessReason = ""
	receipt := mustRun(t, setup)

	transform, ok := receipt.Node(workflow.PromotionNodeBuildProposal)
	if !ok {
		t.Fatal("the proposal transform did not run")
	}
	if transform.Outcome != workflow.OutcomeFailed {
		t.Fatalf("transform outcome = %q, want FAILED", transform.Outcome)
	}
	if receipt.Terminal.NodeID != workflow.PromotionNodeEndUnknown {
		t.Errorf("terminal = %q, want %q", receipt.Terminal.NodeID, workflow.PromotionNodeEndUnknown)
	}
}

// TestTodo_WF_SIM_011_FaultMissingWorkflowInputIsRefused proves a mapping
// whose source was never supplied is a typed refusal, not a defaulted value. A
// simulation that defaulted a missing input would report a number nobody gave
// it.
func TestTodo_WF_SIM_011_FaultMissingWorkflowInputIsRefused(t *testing.T) {
	setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
	delete(setup.Inputs.Values, "target_job_id")

	_, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err == nil {
		t.Fatal("Run accepted a plan whose input mapping had no source value")
	}
	if code := simulate.CodeOf(err); code != simulate.CodeUnresolvedSource {
		t.Fatalf("refusal code = %q, want %q (%v)", code, simulate.CodeUnresolvedSource, err)
	}
}

// TestTodo_WF_SIM_012_FaultUnboundCapabilityRegistryIsRefused proves the
// interpreter refuses rather than skipping a capability node it cannot invoke.
func TestTodo_WF_SIM_012_FaultUnboundCapabilityRegistryIsRefused(t *testing.T) {
	setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
	setup.Options.Capabilities = nil

	_, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err == nil {
		t.Fatal("Run walked a plan with capability nodes and no registry")
	}
	if code := simulate.CodeOf(err); code != simulate.CodeInvalidOptions {
		t.Fatalf("refusal code = %q, want %q (%v)", code, simulate.CodeInvalidOptions, err)
	}
}

// TestTodo_WF_SIM_013_FaultStepBudgetIsBounded proves the walk is bounded
// without a timer or a lease: a budget too small to reach a terminal is a
// typed refusal rather than a hang.
func TestTodo_WF_SIM_013_FaultStepBudgetIsBounded(t *testing.T) {
	setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
	setup.Options.MaxSteps = 2

	_, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err == nil {
		t.Fatal("Run ignored its step budget")
	}
	if code := simulate.CodeOf(err); code != simulate.CodeStepBudgetExceeded {
		t.Fatalf("refusal code = %q, want %q (%v)", code, simulate.CodeStepBudgetExceeded, err)
	}
}

// TestTodo_WF_SIM_014_FaultTamperedPlanIsRefused proves a plan whose content
// no longer matches the digest it was compiled with is not simulated. A
// receipt that pinned a plan nobody could reproduce would prove nothing.
func TestTodo_WF_SIM_014_FaultTamperedPlanIsRefused(t *testing.T) {
	setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
	setup.Plan.Nodes[0].SafePoint = !setup.Plan.Nodes[0].SafePoint

	_, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err == nil {
		t.Fatal("Run simulated a plan that no longer verifies")
	}
	if code := simulate.CodeOf(err); code != simulate.CodePlanUnverified {
		t.Fatalf("refusal code = %q, want %q (%v)", code, simulate.CodePlanUnverified, err)
	}
}
