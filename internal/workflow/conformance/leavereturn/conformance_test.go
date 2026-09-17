package leavereturn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/parallel"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// CONF-004 RED: the leave-and-return reference walk does not exist yet.
// Everything below must fail to compile until the fixture lands.

func mustSetup(t *testing.T, env *Environment, params Params) *Setup {
	t.Helper()
	setup, err := NewSetupWithParams(env, params)
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
	}
	return setup
}

func mustRun(t *testing.T, setup *Setup) simulate.Receipt {
	t.Helper()
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return receipt
}

// TestTodo_CONF_004 is the PRIMARY conformance claim: the compiled
// leave-and-return reference workflow walks, through the real SIMULATE-mode
// interpreter, along the golden path CONF-004's GREEN clause names -
// employment/authority read, independent program resolution, a sealed
// restricted review, an immutable proposal, a clean benefits observation, a
// readiness-gated return - and ends at a terminal that reports exact,
// separately-tracked lifecycle dimensions with the leave's four
// obligations (payroll effect, benefits recovery, notice/evidence, records
// retention) outstanding rather than collapsed into a false success.
func TestTodo_CONF_004(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment(), Params{})
	receipt := mustRun(t, setup)

	if receipt.Mode != workflow.ModeSimulate {
		t.Errorf("mode = %s, want %s", receipt.Mode, workflow.ModeSimulate)
	}
	if receipt.WorkflowID != WorkflowID {
		t.Errorf("workflow id = %q, want %q", receipt.WorkflowID, WorkflowID)
	}
	if receipt.PlanDigest != setup.Plan.Digest() {
		t.Errorf("receipt pins plan %q, plan digest is %q", receipt.PlanDigest, setup.Plan.Digest())
	}

	wantPath := []string{
		NodeReadEmploymentAuthority,
		NodeResolveLeavePrograms,
		NodeEvidenceReviewDecision,
		NodeBuildLeaveProposal,
		NodeObserveBenefits,
		NodeResolveReadiness,
		NodeReadinessReturnDecision,
		NodeEndPendingObligations,
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

	if receipt.Terminal.TerminalCode != "LEAVE_SIMULATION_PENDING_OBLIGATIONS" {
		t.Errorf("terminal code = %q, want LEAVE_SIMULATION_PENDING_OBLIGATIONS", receipt.Terminal.TerminalCode)
	}
	wantObligations := []string{
		ObligationPayrollEffect, ObligationBenefitsRecovery, ObligationNoticeEvidence, ObligationRecordsRetention,
	}
	if !equalSets(receipt.Terminal.OutstandingObligationRefs, wantObligations) {
		t.Errorf("outstanding obligations = %v, want %v", receipt.Terminal.OutstandingObligationRefs, wantObligations)
	}

	wantLifecycle := simulate.LifecycleState{
		RequestState:     "SIMULATED",
		ExecutionState:   "NOT_PLANNED",
		BusinessState:    "NOT_STARTED",
		ConsistencyState: "PENDING_OBSERVATION",
		ObligationState:  "PENDING",
	}
	if receipt.Lifecycle != wantLifecycle {
		t.Errorf("lifecycle = %+v, want %+v", receipt.Lifecycle, wantLifecycle)
	}

	counters := receipt.EffectCounters()
	if !counters.IsZero() {
		t.Fatalf("simulation counted effects: %v", counters.NonZero())
	}
	if err := receipt.ZeroEffect.Validate(); err != nil {
		t.Fatalf("zero-effect receipt: %v", err)
	}

	if len(receipt.WorkItems) != 2 {
		t.Fatalf("work items = %d, want 2 (one per declared approval requirement)", len(receipt.WorkItems))
	}
}

func equalSets(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	set := make(map[string]int, len(got))
	for _, v := range got {
		set[v]++
	}
	for _, v := range want {
		set[v]--
		if set[v] < 0 {
			return false
		}
	}
	return true
}

func TestTodo_CONF_004_Property(t *testing.T) {
	first := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{}))
	second := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{}))
	if first.Digest() != second.Digest() {
		t.Fatal("golden walk is not deterministic")
	}
	if first.PlanDigest != second.PlanDigest {
		t.Fatal("compiled plan digest is not deterministic")
	}
}

func TestTodo_CONF_004_Golden(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment(), Params{})
	receipt := mustRun(t, setup)
	lines := []string{
		"workflow=" + WorkflowID,
		"path=" + strings.Join(receipt.NodeIDs(), ","),
		"terminal=" + receipt.Terminal.TerminalCode,
		"obligations=" + strings.Join(receipt.Terminal.OutstandingObligationRefs, ","),
		"lifecycle=" + receipt.Lifecycle.RequestState + "/" + receipt.Lifecycle.ExecutionState + "/" + receipt.Lifecycle.BusinessState + "/" + receipt.Lifecycle.ConsistencyState + "/" + receipt.Lifecycle.ObligationState,
		"plan=" + receipt.PlanDigest,
		"digest=" + receipt.Digest(),
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "conf004_leave.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_CONF_004_Fault(t *testing.T) {
	// A failed benefits observation routes to the bounded repair terminal:
	// valid leave is never rolled back and the repair is tracked.
	setup := mustSetup(t, DegradedObservationEnvironment(), Params{})
	receipt := mustRun(t, setup)
	if receipt.Terminal.TerminalCode != "LEAVE_SIMULATION_DEGRADED" {
		t.Fatalf("terminal code = %q, want LEAVE_SIMULATION_DEGRADED", receipt.Terminal.TerminalCode)
	}
	if receipt.Terminal.NodeID != NodeEndDegradedRepair {
		t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndDegradedRepair, receipt.NodeIDs())
	}
	node, ok := setup.Plan.Node(NodeEndDegradedRepair)
	if !ok || node.Terminal == nil || len(node.Terminal.RepairRefs) == 0 {
		t.Fatal("degraded terminal carries no RepairRefs; degraded benefits must create bounded repair evidence")
	}
	if node.Terminal.RepairRefs[0] != repairRef {
		t.Fatalf("repair ref = %q, want %q", node.Terminal.RepairRefs[0], repairRef)
	}
	// A not-ready return blocks instead of committing a return.
	blocked := mustRun(t, mustSetup(t, NotReadyEnvironment(), Params{}))
	if blocked.Terminal.TerminalCode != "LEAVE_RETURN_BLOCKED_NOT_READY" {
		t.Fatalf("terminal code = %q, want LEAVE_RETURN_BLOCKED_NOT_READY", blocked.Terminal.TerminalCode)
	}
}

func TestTodo_CONF_004_Security(t *testing.T) {
	// A manager determining legal eligibility is blocked, not resolved.
	manager := mustRun(t, mustSetup(t, ManagerEligibilityEnvironment(), Params{}))
	if manager.Terminal.TerminalCode != "LEAVE_MANAGER_ELIGIBILITY_BLOCKED" {
		t.Fatalf("terminal code = %q, want LEAVE_MANAGER_ELIGIBILITY_BLOCKED", manager.Terminal.TerminalCode)
	}
	// Unsealed medical evidence blocks the restricted review.
	unsealed := mustRun(t, mustSetup(t, UnsealedEvidenceEnvironment(), Params{}))
	if unsealed.Terminal.TerminalCode != "LEAVE_COMPARTMENT_BREACH_BLOCKED" {
		t.Fatalf("terminal code = %q, want LEAVE_COMPARTMENT_BREACH_BLOCKED", unsealed.Terminal.TerminalCode)
	}
}

func TestTodo_CONF_004_Conformance(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment(), Params{})
	receipt := mustRun(t, setup)
	if err := receipt.Verify(); err != nil {
		t.Fatalf("receipt verify: %v", err)
	}
	// The walked plan binds exactly the capability versions the definition
	// declares: no silent capability substitution.
	registry, err := GoldenEnvironment().Registry()
	if err != nil {
		t.Fatalf("Registry: %v", err)
	}
	for _, key := range CapabilityIDs() {
		if _, ok := registry.Lookup(key); !ok {
			t.Fatalf("registry omits capability %s", key.ID)
		}
	}
	// The three restoration branches join with degraded benefits named as
	// an unknown dimension, never coerced: this is WF-STEP-008's REQUIRED_SET
	// contract applied to the leave's own payroll/benefits/schedule effects.
	outcome, err := parallel.Join(
		parallel.JoinPlan{Strategy: parallel.JoinRequiredSet, Version: "wf.join/v1", RequiredID: []string{"payroll", "benefits", "schedule"}},
		[]parallel.BranchResult{
			{BranchID: "payroll", Outcome: parallel.OutcomeSucceeded},
			{BranchID: "benefits", Outcome: parallel.OutcomeUnknown},
			{BranchID: "schedule", Outcome: parallel.OutcomeSucceeded},
		},
	)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if outcome.Verdict != parallel.JoinUnknown || len(outcome.Unknown) != 1 || outcome.Unknown[0] != "benefits" {
		t.Fatalf("join outcome = %+v, want UNKNOWN naming benefits", outcome)
	}
	// The reference stays bounded: the walked path fits the decomposition
	// budget the definition declares, so SUBWORKFLOW-style decomposition
	// stays a bounded refinement, never an escape from the plan.
	definition := ReferenceDefinition()
	if uint32(len(receipt.NodeIDs())) > definition.Limits.MaxNodes {
		t.Fatalf("walked %d nodes, budget is %d", len(receipt.NodeIDs()), definition.Limits.MaxNodes)
	}
}

func TestTodo_CONF_004_Mutation(t *testing.T) {
	// A tampered input mapping cannot silently reroute the walk: omitting
	// the legal context the program node requires lands at the unknown
	// terminal instead of a false completion.
	setup := mustSetup(t, GoldenEnvironment(), Params{OmitLegalContext: true})
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if receipt.Terminal.NodeID != NodeEndUnknown {
		t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndUnknown, receipt.NodeIDs())
	}
	if receipt.Lifecycle.BusinessState == "COMPLETED" {
		t.Fatal("an omitted jurisdiction section resolved to BusinessState=COMPLETED")
	}
	// Forged terminal codes never verify: the receipt seal binds the
	// walked terminal.
	forged := receipt
	forged.Terminal.TerminalCode = "LEAVE_SIMULATION_COMPLETE_SUCCESS"
	if err := forged.Verify(); err == nil {
		t.Fatal("forged terminal verified")
	}
}

func TestTodo_CONF_004_Race(t *testing.T) {
	const racers = 4
	digests := make([]string, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			setup, err := NewSetupWithParams(GoldenEnvironment(), Params{})
			if err != nil {
				errs[i] = err
				return
			}
			receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = receipt.Digest()
		}(i)
	}
	wg.Wait()
	for i := 0; i < racers; i++ {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if digests[i] != digests[0] {
			t.Fatalf("racer %d diverged", i)
		}
	}
}
