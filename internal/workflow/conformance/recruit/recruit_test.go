package recruit

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// mustSetup wires env against the compiled reference workflow, failing the
// test with the full diagnostic set if the reference stops compiling.
func mustSetup(t *testing.T, env *Environment) *Setup {
	t.Helper()
	setup, err := NewSetup(env)
	if err != nil {
		t.Fatalf("NewSetup: %v", err)
	}
	return setup
}

// mustRun walks the plan and fails on any refusal.
func mustRun(t *testing.T, setup *Setup) simulate.Receipt {
	t.Helper()
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return receipt
}

func sameElements(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]int, len(got))
	for _, v := range got {
		seen[v]++
	}
	for _, v := range want {
		seen[v]--
		if seen[v] < 0 {
			return false
		}
	}
	return true
}

func walkedPath(t *testing.T, receipt simulate.Receipt) []string {
	t.Helper()
	return receipt.NodeIDs()
}

// TestTodo_REV_020_01 is the PRIMARY claim: the compiled Recruit/Hire/Onboard
// reference workflow walks, through the real SIMULATE-mode interpreter, along
// the golden path REV-020-01's GREEN clause names - person-registry,
// position/budget, offer binding, the offer-clock observation, document
// work-authorization evidence, downstream readiness, a bound proposal and a
// routing decision - and ends at a terminal that holds the position,
// employment and evidence obligations outstanding with the hiring-manager and
// HRBP approvals declared rather than collapsing into a false hire.
func TestTodo_REV_020_01(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment())
	receipt := mustRun(t, setup)
	definition := ReferenceDefinition()
	var documentNode *workflow.Node
	for i := range definition.Nodes {
		if definition.Nodes[i].ID == NodeVerifyWorkAuth {
			documentNode = &definition.Nodes[i]
			break
		}
	}
	if documentNode == nil || documentNode.Type != workflow.StepCapability || documentNode.Capability == nil || documentNode.Capability.ID != CapVerifyWorkAuth || len(documentNode.Capability.AuthorityScopes) != 1 || documentNode.Capability.AuthorityScopes[0] != "scope:documents.read" {
		t.Fatalf("DOCUMENT migration must bind work-authorization evidence through documents.* CAPABILITY: %+v", documentNode)
	}

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
		NodeReadPerson,
		NodeReadCapacity,
		NodeVerifyOffer,
		NodeObserveOffer,
		NodeVerifyWorkAuth,
		NodeCheckReadiness,
		NodeBuildProposal,
		NodeRouteHire,
		NodeEndHired,
	}
	got := walkedPath(t, receipt)
	if len(got) != len(wantPath) {
		t.Fatalf("walked %d nodes %v, want %d %v", len(got), got, len(wantPath), wantPath)
	}
	for i := range wantPath {
		if got[i] != wantPath[i] {
			t.Fatalf("node %d = %q, want %q (whole path %v)", i, got[i], wantPath[i], got)
		}
	}

	if receipt.Terminal.TerminalCode != TerminalHired {
		t.Errorf("terminal code = %q, want %q", receipt.Terminal.TerminalCode, TerminalHired)
	}
	wantObligations := []string{ObligationPositionHold, ObligationEmployment, ObligationEvidence}
	if !sameElements(receipt.Terminal.OutstandingObligationRefs, wantObligations) {
		t.Errorf("outstanding obligations = %v, want %v", receipt.Terminal.OutstandingObligationRefs, wantObligations)
	}
	if len(receipt.WorkItems) != 2 {
		t.Fatalf("approval work items = %d, want the hiring-manager and HRBP approvals: %+v", len(receipt.WorkItems), receipt.WorkItems)
	}
	approvalIDs := []string{receipt.WorkItems[0].RequirementID, receipt.WorkItems[1].RequirementID}
	if !sameElements(approvalIDs, []string{ApprovalHiringManager, ApprovalHRBP}) {
		t.Errorf("approval requirements = %v, want hiring-manager and HRBP", approvalIDs)
	}
	for _, item := range receipt.WorkItems {
		if item.State != simulate.WouldAwait {
			t.Errorf("approval %q state = %q, want %q", item.RequirementID, item.State, simulate.WouldAwait)
		}
		if item.RequirementDigest == "" || item.ExpressionDigest == "" || item.QuorumMin != 1 {
			t.Errorf("approval %q lacks its bound expression, requirement digest, or quorum: %+v", item.RequirementID, item)
		}
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

	// Golden pin: the plan digest is deterministic across compilations, so
	// the receipt's plan identity is a stable golden value, not a per-run
	// accident.
	again := mustSetup(t, GoldenEnvironment())
	if again.Plan.Digest() != setup.Plan.Digest() {
		t.Errorf("plan digest unstable: %q vs %q", setup.Plan.Digest(), again.Plan.Digest())
	}
	if setup.Plan.Digest() == "" {
		t.Error("plan digest is empty")
	}
}

// TestTodo_REV_020_01_Conformance walks every blocked and degraded variant
// and proves the terminal comes from the interpreter's own
// proposal/approval/reservation/observation state: the expired offer never
// reaches proposal construction, and each gate refusal lands on its own
// terminal code with the evidence obligation retained.
func TestTodo_REV_020_01_Conformance(t *testing.T) {
	cases := []struct {
		name         string
		env          func() *Environment
		wantTerminal string
		wantContain  []string
		wantAbsent   []string
	}{
		{
			name:         "duplicate person refused before offer state matters",
			env:          DuplicatePersonEnvironment,
			wantTerminal: TerminalDuplicate,
			wantContain:  []string{NodeReadPerson, NodeRouteHire, NodeEndDuplicate},
		},
		{
			name:         "unaccepted offer never binds",
			env:          OfferUnboundEnvironment,
			wantTerminal: TerminalOffer,
			wantContain:  []string{NodeVerifyOffer, NodeRouteHire, NodeEndOffer},
		},
		{
			name:         "expired offer never builds a proposal",
			env:          OfferExpiredEnvironment,
			wantTerminal: TerminalOfferExpired,
			wantContain:  []string{NodeObserveOffer, NodeEndOfferExpired},
			wantAbsent:   []string{NodeBuildProposal, NodeRouteHire},
		},
		{
			name:         "exhausted position refuses the hold",
			env:          ExhaustedPositionEnvironment,
			wantTerminal: TerminalCapacity,
			wantContain:  []string{NodeReadCapacity, NodeRouteHire, NodeEndCapacity},
		},
		{
			name:         "exhausted budget refuses the hold",
			env:          ExhaustedBudgetEnvironment,
			wantTerminal: TerminalCapacity,
			wantContain:  []string{NodeReadCapacity, NodeRouteHire, NodeEndCapacity},
		},
		{
			name:         "missing work authorization blocks on the document leg",
			env:          MissingWorkAuthEnvironment,
			wantTerminal: TerminalWorkAuth,
			wantContain:  []string{NodeVerifyWorkAuth, NodeRouteHire, NodeEndAuth},
		},
		{
			name:         "degraded downstream degrades to bounded repair",
			env:          DegradedReadinessEnvironment,
			wantTerminal: TerminalDegraded,
			wantContain:  []string{NodeCheckReadiness, NodeRouteHire, NodeEndDegraded},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup := mustSetup(t, tc.env())
			receipt := mustRun(t, setup)
			if receipt.Terminal.TerminalCode != tc.wantTerminal {
				t.Errorf("terminal code = %q, want %q", receipt.Terminal.TerminalCode, tc.wantTerminal)
			}
			path := walkedPath(t, receipt)
			onPath := make(map[string]bool, len(path))
			for _, id := range path {
				onPath[id] = true
			}
			for _, id := range tc.wantContain {
				if !onPath[id] {
					t.Errorf("path %v lacks required node %q", path, id)
				}
			}
			for _, id := range tc.wantAbsent {
				if onPath[id] {
					t.Errorf("path %v must not contain node %q", path, id)
				}
			}
			if !sameElements(receipt.Terminal.OutstandingObligationRefs, []string{ObligationEvidence}) &&
				tc.wantTerminal != TerminalDegraded {
				t.Errorf("blocked terminal obligations = %v, want only the evidence obligation", receipt.Terminal.OutstandingObligationRefs)
			}
			if counters := receipt.EffectCounters(); !counters.IsZero() {
				t.Errorf("simulation counted effects: %v", counters.NonZero())
			}
		})
	}
}

// TestTodo_REV_020_01_Security proves the fixture cannot hire through a
// side channel: the capability registry binds exactly the six declared
// fixture capabilities, every walk is zero-effect, and neither a duplicate
// person nor an unbound offer can ever reach the hired terminal.
func TestTodo_REV_020_01_Security(t *testing.T) {
	ids := CapabilityIDs()
	if len(ids) != 6 {
		t.Fatalf("bound capabilities = %d %v, want exactly 6", len(ids), ids)
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate capability binding %q", id)
		}
		seen[id] = true
	}

	neverHired := map[string]func() *Environment{
		"duplicate": DuplicatePersonEnvironment,
		"unbound":   OfferUnboundEnvironment,
		"expired":   OfferExpiredEnvironment,
		"capacity":  ExhaustedPositionEnvironment,
		"budget":    ExhaustedBudgetEnvironment,
		"noauth":    MissingWorkAuthEnvironment,
	}
	for name, env := range neverHired {
		setup := mustSetup(t, env())
		receipt := mustRun(t, setup)
		if receipt.Terminal.TerminalCode == TerminalHired {
			t.Errorf("%s environment reached the hired terminal", name)
		}
		for _, id := range walkedPath(t, receipt) {
			if id == NodeEndHired {
				t.Errorf("%s environment walked the hired end node", name)
			}
		}
		if counters := receipt.EffectCounters(); !counters.IsZero() {
			t.Errorf("%s environment counted simulation effects: %v", name, counters.NonZero())
		}
	}
}
