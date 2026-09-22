package learning

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// mustSetup wires env/params against the compiled reference workflow,
// failing the test with the full diagnostic set if the reference stops
// compiling.
func mustSetup(t *testing.T, env *Environment, params Params) *Setup {
	t.Helper()
	setup, err := NewSetupWithParams(env, params)
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
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

// TestTodo_CONF_013 is the PRIMARY conformance claim: the compiled learning
// credential-satisfaction reference workflow walks, through the real
// SIMULATE-mode interpreter, along the golden path CONF-013's GREEN clause
// names - requirement and enrollment-history reads, credential-evidence and
// waiver reads, an evidence-expiry footprint, a bound proposal, a
// satisfaction decision and a post-decision provider-reconciliation
// observation - and ends at a terminal that reports exact,
// separately-tracked lifecycle dimensions with the evidence-kind,
// provider-reconciliation and evidence-retention obligations outstanding
// rather than collapsed into a false success.
func TestTodo_CONF_013(t *testing.T) {
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
		NodeReadRequirement,
		NodeReadEnrollmentHistory,
		NodeReadCredentialEvidence,
		NodeReadWaiver,
		NodeComputeEvidenceFootprint,
		NodeBuildSatisfactionProposal,
		NodeSatisfactionDecision,
		NodeObserveProviderReconciliation,
		NodeEndSatisfied,
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

	if receipt.Terminal.TerminalCode != "LEARNING_SATISFIED_PENDING_RECONCILIATION" {
		t.Errorf("terminal code = %q, want LEARNING_SATISFIED_PENDING_RECONCILIATION", receipt.Terminal.TerminalCode)
	}
	wantObligations := []string{ObligationEvidenceKindRecorded, ObligationProviderReconciliation, ObligationEvidenceRetained}
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
	wantApprovals := []string{ApprovalLnDPartner, ApprovalComplianceOfficer}
	var gotApprovals []string
	for _, item := range receipt.WorkItems {
		if item.State != simulate.WouldAwait {
			t.Errorf("work item %s state = %q, want %q", item.RequirementID, item.State, simulate.WouldAwait)
		}
		if len(item.Candidates) == 0 {
			t.Errorf("work item %s resolved no candidate approver", item.RequirementID)
		}
		gotApprovals = append(gotApprovals, item.RequirementID)
	}
	if !equalSets(gotApprovals, wantApprovals) {
		t.Errorf("approval requirement refs = %v, want %v", gotApprovals, wantApprovals)
	}

	// The receipt digest is a content identity, not a timestamp: a second
	// independent run of the same environment and inputs reproduces it
	// exactly.
	second := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{}))
	if receipt.Digest() != second.Digest() {
		t.Fatalf("receipt digest drifted between runs:\n first  %s\n second %s", receipt.Digest(), second.Digest())
	}
	if err := receipt.Verify(); err != nil {
		t.Errorf("receipt does not verify: %v", err)
	}
}

// TestTodo_CONF_013_Integration proves the whole port composition
// integrates correctly across combined multi-port scenarios the PRIMARY
// test alone does not cover: the Capability handlers, TRANSFORM, DECISION,
// OBSERVE and Approvals ports must reconcile together, not merely each work
// in isolation.
func TestTodo_CONF_013_Integration(t *testing.T) {
	t.Run("valid_credential_plus_duplicate_enrollment_still_blocks", func(t *testing.T) {
		// Otherwise-golden, valid VERIFIED_CREDENTIAL evidence combined with
		// a duplicate-enrollment flag: duplicate enrollment must still block
		// even though the evidence itself looks fine.
		env := GoldenEnvironment()
		env.DuplicateEnrollmentDetected = true
		receipt := mustRun(t, mustSetup(t, env, Params{}))

		if receipt.Terminal.NodeID != NodeEndDuplicateEnrollment {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndDuplicateEnrollment, receipt.NodeIDs())
		}
		wantLifecycle := simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "PENDING"}
		if receipt.Lifecycle != wantLifecycle {
			t.Fatalf("lifecycle = %+v, want %+v", receipt.Lifecycle, wantLifecycle)
		}
		if !equalSets(receipt.Terminal.OutstandingObligationRefs, []string{ObligationEvidenceRetained}) {
			t.Fatalf("outstanding obligations = %v, want [%s]", receipt.Terminal.OutstandingObligationRefs, ObligationEvidenceRetained)
		}
	})

	t.Run("expiring_credential_plus_invalid_waiver_is_not_rescued", func(t *testing.T) {
		// Evidence that would otherwise be EXPIRING_SATISFACTION, combined
		// with a present-but-invalid waiver: the invalid waiver must not
		// silently rescue the outcome into a success, nor may it be ignored
		// in favor of the (later-precedence) expiring-evidence route.
		env := ExpiringEnvironment()
		env.WaiverPresent = true
		env.WaiverValid = false
		receipt := mustRun(t, mustSetup(t, env, Params{}))

		if receipt.Terminal.NodeID != NodeEndUnsatisfiedInvalidWaiver {
			t.Fatalf("terminal = %q, want %q (path %v); an invalid waiver must block, not be silently superseded by an expiring-but-otherwise-valid credential",
				receipt.Terminal.NodeID, NodeEndUnsatisfiedInvalidWaiver, receipt.NodeIDs())
		}
		wantLifecycle := simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"}
		if receipt.Lifecycle != wantLifecycle {
			t.Fatalf("lifecycle = %+v, want %+v", receipt.Lifecycle, wantLifecycle)
		}
		// Never routed through either observe node: the decision itself
		// blocked before any provider reconciliation was needed.
		for _, id := range receipt.NodeIDs() {
			if id == NodeObserveProviderReconciliation || id == NodeObserveProviderReconciliationExpiring {
				t.Fatalf("path %v reached an observe node; an invalid waiver must block at the decision, not proceed to reconciliation", receipt.NodeIDs())
			}
		}
	})

	t.Run("valid_waiver_plus_missing_evidence_resolves_exempt", func(t *testing.T) {
		// A valid waiver combined with no accessible evidence at all: the
		// waiver alone must resolve to EXEMPT, and the workflow must never
		// spuriously demand evidence in addition to an already-granted
		// waiver.
		env := ValidWaiverEnvironment()
		env.EvidencePresent = false
		receipt := mustRun(t, mustSetup(t, env, Params{}))

		if receipt.Terminal.NodeID != NodeEndExemptValidWaiver {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndExemptValidWaiver, receipt.NodeIDs())
		}
		if receipt.Terminal.TerminalCode != "LEARNING_EXEMPT_VALID_WAIVER" {
			t.Fatalf("terminal code = %q, want LEARNING_EXEMPT_VALID_WAIVER", receipt.Terminal.TerminalCode)
		}
		wantLifecycle := simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"}
		if receipt.Lifecycle != wantLifecycle {
			t.Fatalf("lifecycle = %+v, want %+v", receipt.Lifecycle, wantLifecycle)
		}
		if !equalSets(receipt.Terminal.OutstandingObligationRefs, []string{ObligationEvidenceRetained}) {
			t.Fatalf("outstanding obligations = %v, want [%s]; an exemption must never carry the evidence-kind obligation it never touched", receipt.Terminal.OutstandingObligationRefs, ObligationEvidenceRetained)
		}
		if len(receipt.WorkItems) != 2 {
			t.Fatalf("work items = %d, want 2 (the pending exemption still awaits its declared approvals)", len(receipt.WorkItems))
		}
		counters := receipt.EffectCounters()
		if !counters.IsZero() {
			t.Fatalf("simulation counted effects: %v", counters.NonZero())
		}
	})
}

// TestTodo_CONF_013_Fault exercises the RED cases CONF-013 names: an
// inaccessible or absent evidence document, an expired credential, a
// duplicate enrollment, an invalid waiver and an ambiguous provider
// completion must never be quietly reported as satisfying the requirement.
func TestTodo_CONF_013_Fault(t *testing.T) {
	t.Run("inaccessible_content_resolves_to_no_evidence_not_success", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, NoEvidenceEnvironment(), Params{}))
		if receipt.Terminal.NodeID != NodeEndUnsatisfiedNoEvidence {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndUnsatisfiedNoEvidence, receipt.NodeIDs())
		}
		if receipt.Lifecycle.BusinessState == "COMPLETED" {
			t.Fatalf("inaccessible or absent evidence must never resolve to BusinessState=COMPLETED")
		}
	})

	t.Run("expired_credential_resolves_to_unsatisfied_expired", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, ExpiredEnvironment(), Params{}))
		if receipt.Terminal.NodeID != NodeEndUnsatisfiedExpired {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndUnsatisfiedExpired, receipt.NodeIDs())
		}
	})

	t.Run("duplicate_enrollment_blocks_with_otherwise_valid_evidence", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, DuplicateEnrollmentEnvironment(), Params{}))
		if receipt.Terminal.NodeID != NodeEndDuplicateEnrollment {
			t.Fatalf("terminal = %q, want %q; a duplicate enrollment must block regardless of otherwise-valid evidence", receipt.Terminal.NodeID, NodeEndDuplicateEnrollment)
		}
	})

	t.Run("invalid_waiver_never_silently_exempts", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, InvalidWaiverEnvironment(), Params{}))
		if receipt.Terminal.NodeID != NodeEndUnsatisfiedInvalidWaiver {
			t.Fatalf("terminal = %q, want %q; an invalid waiver must never be treated as a valid exemption", receipt.Terminal.NodeID, NodeEndUnsatisfiedInvalidWaiver)
		}
	})

	t.Run("ambiguous_provider_completion_resolves_to_unknown_not_satisfied", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, AmbiguousObservationEnvironment(), Params{}))
		if receipt.Terminal.NodeID != NodeEndUnknown {
			t.Fatalf("terminal = %q, want %q (path %v); an ambiguous provider completion must never be folded into SATISFIED", receipt.Terminal.NodeID, NodeEndUnknown, receipt.NodeIDs())
		}
		if receipt.Lifecycle.BusinessState == "COMPLETED" {
			t.Fatalf("an ambiguous provider completion must never resolve to BusinessState=COMPLETED")
		}
	})

	t.Run("omitted_policy_context_is_unknown_not_a_defaulted_no_waiver", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{OmitPolicyContext: true}))
		if receipt.Terminal.NodeID != NodeEndUnknown {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndUnknown, receipt.NodeIDs())
		}
		if receipt.Lifecycle.BusinessState == "COMPLETED" {
			t.Fatalf("an omitted policy context must never resolve to BusinessState=COMPLETED")
		}
	})

	t.Run("degraded_observation_routes_to_repair_not_success", func(t *testing.T) {
		setup := mustSetup(t, DegradedObservationEnvironment(), Params{})
		receipt := mustRun(t, setup)
		if receipt.Terminal.NodeID != NodeEndDegradedRepair {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndDegradedRepair, receipt.NodeIDs())
		}
		if receipt.Lifecycle.ConsistencyState == "CONSISTENT" || receipt.Lifecycle.BusinessState == "COMPLETED" {
			t.Fatalf("a degraded observation must never be reported as a consistent, completed outcome: %+v", receipt.Lifecycle)
		}
		node, ok := setup.Plan.Node(NodeEndDegradedRepair)
		if !ok || node.Terminal == nil || len(node.Terminal.RepairRefs) == 0 {
			t.Fatalf("degraded terminal carries no RepairRefs; a degraded dimension must create bounded repair evidence")
		}
	})
}

// TestTodo_CONF_013_Security proves the REFACTOR clause structurally and at
// runtime, proves a waiver's authority is never resolved outside its pinned
// policy context, and proves a write-class node - a hidden effect - cannot
// execute under SIMULATE no matter how it is reached.
func TestTodo_CONF_013_Security(t *testing.T) {
	t.Run("waiver_read_declares_a_pinned_policy_context_requirement", func(t *testing.T) {
		def := ReferenceDefinition()
		var found bool
		for _, n := range def.Nodes {
			if n.ID != NodeReadWaiver {
				continue
			}
			found = true
			if len(n.RequiredContext) == 0 {
				t.Fatalf("%s declares no RequiredContext; a waiver's authority must never be a live re-read", NodeReadWaiver)
			}
			req := n.RequiredContext[0]
			if req.Kind != "PolicyContext" || !req.Pinned || req.MissingBehavior != workflow.MissingUnknown {
				t.Fatalf("%s policy context requirement = %+v, want Kind=PolicyContext Pinned=true MissingBehavior=UNKNOWN", NodeReadWaiver, req)
			}
			hasScope := false
			for _, f := range req.FieldPaths {
				if f == "waiver_authority_scope" {
					hasScope = true
				}
			}
			if !hasScope {
				t.Fatalf("%s policy context field paths = %v, want waiver_authority_scope declared", NodeReadWaiver, req.FieldPaths)
			}
		}
		if !found {
			t.Fatalf("reference definition declares no %s node", NodeReadWaiver)
		}
	})

	t.Run("evidence_kind_is_a_declared_output_never_folded_into_a_bare_bool", func(t *testing.T) {
		// The REFACTOR clause structurally: evidence_kind is its own typed
		// STRING output, never collapsed into (e.g.) a single
		// "evidence_satisfactory" boolean that would make a verified
		// credential and a self-claim indistinguishable downstream.
		def := ReferenceDefinition()
		for _, n := range def.Nodes {
			if n.ID != NodeReadCredentialEvidence {
				continue
			}
			var hasKind bool
			for _, f := range n.Outputs {
				if f.Path == "evidence_kind" {
					hasKind = true
				}
				if f.Path == "evidence_satisfactory" || f.Path == "satisfactory" {
					t.Fatalf("%s declares a collapsed satisfactory-looking output %q; evidence kind and satisfaction must remain distinct", NodeReadCredentialEvidence, f.Path)
				}
			}
			if !hasKind {
				t.Fatalf("%s declares no evidence_kind output", NodeReadCredentialEvidence)
			}
		}
	})

	t.Run("self_claim_is_blocked_at_runtime_regardless_of_otherwise_valid_evidence", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, SelfClaimEnvironment(), Params{}))
		if receipt.Terminal.NodeID != NodeEndUnsatisfiedUnverified {
			t.Fatalf("terminal = %q, want %q; a self-reported claim must never alone satisfy the requirement", receipt.Terminal.NodeID, NodeEndUnsatisfiedUnverified)
		}
	})

	t.Run("hidden_write_effect_cannot_execute_under_simulate", func(t *testing.T) {
		invoked := false
		plan := mustCompileHiddenEffectPlan(t, func() { invoked = true })

		if err := simulate.Admit(plan); err == nil {
			t.Fatal("Admit accepted a plan containing a write-class node")
		} else if code := simulate.CodeOf(err); code != simulate.CodeWriteEffectInSimulate {
			t.Fatalf("Admit refusal code = %q, want %q (%v)", code, simulate.CodeWriteEffectInSimulate, err)
		}

		registry := hiddenEffectRegistry(t, func() { invoked = true })
		_, err := simulate.Run(context.Background(), plan, simulate.Inputs{
			Values: simulate.Bag{"worker_id": simulate.NewBranded("WorkerID", "33333333-3333-4333-8333-333333333333")},
		}, simulate.Options{Capabilities: registry})
		if err == nil {
			t.Fatal("Run executed a plan containing a write-class node")
		}
		if !errors.Is(err, simulate.ErrSimulate) {
			t.Errorf("refusal does not unwrap to ErrSimulate: %v", err)
		}
		if invoked {
			t.Fatal("the mutating capability handler was reached; SIMULATE must refuse, not suppress a hidden effect")
		}
	})
}

// TestTodo_CONF_013_Conformance proves the whole terminal lattice at once:
// every reachable terminal reports the exact five-dimension tuple CONF-013's
// GREEN clause requires, never a collapsed or approximate one.
func TestTodo_CONF_013_Conformance(t *testing.T) {
	successPending := simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"}
	hardBlocked := simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"}
	duplicateBlocked := simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "PENDING"}
	degradedOrUnknown := simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"}

	cases := []struct {
		name     string
		env      *Environment
		params   Params
		terminal string
		want     simulate.LifecycleState
	}{
		{"satisfied", GoldenEnvironment(), Params{}, NodeEndSatisfied, successPending},
		{"expiring", ExpiringEnvironment(), Params{}, NodeEndExpiringSatisfaction, successPending},
		{"exempt_valid_waiver", ValidWaiverEnvironment(), Params{}, NodeEndExemptValidWaiver, successPending},
		{"unsatisfied_no_evidence", NoEvidenceEnvironment(), Params{}, NodeEndUnsatisfiedNoEvidence, hardBlocked},
		{"unsatisfied_expired", ExpiredEnvironment(), Params{}, NodeEndUnsatisfiedExpired, hardBlocked},
		{"unsatisfied_unverified_claim", SelfClaimEnvironment(), Params{}, NodeEndUnsatisfiedUnverified, hardBlocked},
		{"unsatisfied_invalid_waiver", InvalidWaiverEnvironment(), Params{}, NodeEndUnsatisfiedInvalidWaiver, hardBlocked},
		{"duplicate_enrollment_blocked", DuplicateEnrollmentEnvironment(), Params{}, NodeEndDuplicateEnrollment, duplicateBlocked},
		{"degraded_repair", DegradedObservationEnvironment(), Params{}, NodeEndDegradedRepair, degradedOrUnknown},
		{"unknown_ambiguous_provider", AmbiguousObservationEnvironment(), Params{}, NodeEndUnknown, degradedOrUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			receipt := mustRun(t, mustSetup(t, c.env, c.params))
			if receipt.Terminal.NodeID != c.terminal {
				t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, c.terminal, receipt.NodeIDs())
			}
			if receipt.Lifecycle != c.want {
				t.Fatalf("lifecycle = %+v, want %+v", receipt.Lifecycle, c.want)
			}
		})
	}

	t.Run("missing_policy_context_unknown", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{OmitPolicyContext: true}))
		if receipt.Terminal.NodeID != NodeEndUnknown || receipt.Lifecycle != degradedOrUnknown {
			t.Fatalf("terminal = %q lifecycle = %+v, want %q %+v", receipt.Terminal.NodeID, receipt.Lifecycle, NodeEndUnknown, degradedOrUnknown)
		}
	})
}

// TestTodo_CONF_013_Mutation proves the satisfaction DECISION's own
// self-claim route is independently load-bearing: the evidence-footprint
// TRANSFORM never itself decides a self-reported claim is unsatisfactory -
// it only reports evidence_kind and the expiry footprint - so a mutation
// that deleted the DECISION's own UNSATISFIED_UNVERIFIED_CLAIM route would
// let a self-claim reach the pending-satisfaction terminal. This test fails
// the moment that happens.
func TestTodo_CONF_013_Mutation(t *testing.T) {
	expiry, err := values.ParseLocalDate("2027-06-01")
	if err != nil {
		t.Fatalf("parse expiry date: %v", err)
	}
	effective, err := values.ParseLocalDate(EffectiveDateText)
	if err != nil {
		t.Fatalf("parse effective date: %v", err)
	}
	result, err := Transforms{}.Transform(context.Background(), simulate.TransformRequest{
		Transform: workflow.CompiledTransform{TransformRef: TransformEvidenceFootprint},
		Inputs: simulate.Bag{
			"evidence_present":     simulate.NewBool(true),
			"evidence_expiry_date": simulate.NewLocalDate(expiry),
			"effective_date":       simulate.NewLocalDate(effective),
			"evidence_kind":        simulate.NewString(EvidenceKindSelfClaim),
			"requirement_active":   simulate.NewBool(true),
		},
	})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	expired, err := result.Outputs["evidence_expired"].Bool()
	if err != nil {
		t.Fatalf("evidence_expired: %v", err)
	}
	expiringSoon, err := result.Outputs["evidence_expiring_soon"].Bool()
	if err != nil {
		t.Fatalf("evidence_expiring_soon: %v", err)
	}
	if expired || expiringSoon {
		t.Fatal("the evidence-footprint transform must not itself react to evidence_kind=SELF_CLAIM; expiry footprinting only applies to verified-credential evidence")
	}

	receipt := mustRun(t, mustSetup(t, SelfClaimEnvironment(), Params{}))
	if receipt.Terminal.NodeID != NodeEndUnsatisfiedUnverified {
		t.Fatalf("terminal = %q, want %q; the DECISION's own self-claim route must still block a self-reported claim "+
			"even though the footprint transform reports no expiry problem for it", receipt.Terminal.NodeID, NodeEndUnsatisfiedUnverified)
	}
}

func equalSets(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(want))
	for _, w := range want {
		seen[w] = true
	}
	for _, g := range got {
		if !seen[g] {
			return false
		}
		delete(seen, g)
	}
	return len(seen) == 0
}

// ---- hidden-effect fixture: a minimal write-class plan compiled under P1B,
// used to prove CONF-013's "hidden effects" RED case independently of
// CONF-003's and CONF-005's own proofs of the same interpreter guarantee.
// P1A refuses a write effect at compile time, so a P1A plan with a mutating
// node cannot exist; compiling under P1B is what makes the interpreter's own
// SIMULATE refusal the thing under test.

const (
	hiddenCapRead      = "fixture.conformance.learning.read_worker"
	hiddenCapWrite     = "fixture.conformance.learning.mutate_credential"
	hiddenCapObserve   = "fixture.conformance.learning.observe_worker"
	hiddenNodeRead     = "read_worker"
	hiddenNodeSync     = "mutate_credential"
	hiddenNodeObserve  = "observe_worker"
	hiddenNodeCommit   = "end_committed"
	hiddenNodeDegraded = "end_degraded"
	hiddenNodeRepair   = "end_repair"
)

func hiddenSchema(id, slot string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: id + "." + slot + "/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}
}

func hiddenEffectDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID: "fixture.conformance.learning.hidden_effect", Version: 1, Name: "Hidden effect fixture",
		InputSchema: workflowSchema("HiddenEffectInput"), OutputSchema: workflowSchema("HiddenEffectResult"), VariablesSchema: workflowSchema("HiddenEffectVariables"),
		TenantScope: "acme", OrganizationScope: "acme/talent", RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID:      hiddenNodeRead,
		Inputs:           []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}},
		Outputs:          []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}, {Path: "terminal_code", Type: plainStr()}},
		Limits:           workflow.Limits{MaxFanOut: 4, MaxDepth: 8, MaxNodes: 8},
		FailurePolicyRef: "policy.workflow.failure.execute/v1", CancellationPolicyRef: "policy.workflow.cancellation.execute/v1",
		MigrationPolicyRef: "policy.workflow.migration.pinned/v1", RetentionPolicyRef: "policy.workflow.retention.fixture/v1",
		Nodes: []workflow.Node{
			{
				ID: hiddenNodeRead, Type: workflow.StepCapability,
				InputSchema: hiddenSchema(hiddenCapRead, "request"), OutputSchema: hiddenSchema(hiddenCapRead, "response"),
				Inputs:  []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}},
				Outputs: []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: builders.FromInput("worker_id")},
				},
				Capability: &workflow.CapabilityRef{ID: hiddenCapRead, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:learning.evidence.read"}},
				Governance: governedInvocation(nil, nil),
			},
			{
				ID: hiddenNodeSync, Type: workflow.StepCapability,
				InputSchema: hiddenSchema(hiddenCapWrite, "request"), OutputSchema: hiddenSchema(hiddenCapWrite, "response"),
				Inputs:  []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}},
				Outputs: []workflow.Field{{Path: "submission_id", Type: plainStr()}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: builders.FromNode(hiddenNodeRead, "worker_id")},
				},
				Capability:   &workflow.CapabilityRef{ID: hiddenCapWrite, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:learning.evidence.write"}, IdempotencyKeyMapping: "worker_id", EffectBinding: "learning.hidden_effect"},
				Retry:        &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.effect.bounded/v1"},
				FailureRoute: hiddenNodeRepair,
				EffectRole:   workflow.RoleDownstreamEffect,
				Governance: workflow.NodeGovernance{
					Purpose: purpose, Classification: classification,
					RequiredDecisions:     []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk},
					RevalidationBoundary:  workflow.RevalidatePreEffect,
					DataAccessManifestRef: dataAccessManifest,
				},
			},
			{
				ID: hiddenNodeObserve, Type: workflow.StepObserve,
				InputSchema: hiddenSchema(hiddenCapObserve, "request"), OutputSchema: hiddenSchema(hiddenCapObserve, "response"),
				Inputs:  []workflow.Field{{Path: "worker_id", Type: str("WorkerID")}},
				Outputs: []workflow.Field{{Path: "observed_status", Type: plainStr()}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: builders.FromNode(hiddenNodeRead, "worker_id")},
				},
				Capability: &workflow.CapabilityRef{ID: hiddenCapObserve, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:learning.evidence.read"}},
				Observe: &workflow.ObserveSpec{
					EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "fixture.provider",
					ExpectedStateFields: []string{"worker_id"}, RequiredWatermarks: []string{"fixture.provider.cursor"},
					MaxAgeSeconds: 600, ComparisonProfile: "comparison.fixture.status/v1",
					RetryExhaustionRoute: hiddenNodeDegraded,
				},
				Retry:      &workflow.RetryPolicy{MaxAttempts: 5, BackoffRef: "policy.retry.observation.bounded/v1"},
				Governance: governedInvocation(nil, nil),
			},
			{
				ID: hiddenNodeCommit, Type: workflow.StepEnd,
				Inputs: builders.TerminalInputs("worker_id", "WorkerID"), InputMappings: builders.TerminalMappings("worker_id", "COMMITTED"),
				End: &workflow.EndSpec{
					TerminalCode: "COMMITTED", RuntimeStatus: workflow.RuntimeCompleted,
					CompletionMapping: builders.Completion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture/v1",
				},
				Governance: terminalGovernance(nil, nil),
			},
			{
				ID: hiddenNodeDegraded, Type: workflow.StepEnd,
				Inputs: builders.TerminalInputs("worker_id", "WorkerID"), InputMappings: builders.TerminalMappings("worker_id", "COMMITTED_DEGRADED"),
				End: &workflow.EndSpec{
					TerminalCode: "COMMITTED_DEGRADED", RuntimeStatus: workflow.RuntimeCompleted,
					CompletionMapping: builders.Completion("APPROVED", "COMMITTED", "COMPLETED", "DEGRADED", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture/v1",
					RepairRefs:        []string{"repair.fixture/v1"},
				},
				Governance: terminalGovernance(nil, nil),
			},
			{
				ID: hiddenNodeRepair, Type: workflow.StepEnd,
				Inputs: builders.TerminalInputs("worker_id", "WorkerID"), InputMappings: builders.TerminalMappings("worker_id", "REPAIR_REQUIRED"),
				End: &workflow.EndSpec{
					TerminalCode: "REPAIR_REQUIRED", RuntimeStatus: workflow.RuntimeRepairRequired,
					CompletionMapping: builders.Completion("APPROVED", "REPAIR_REQUIRED", "UNKNOWN", "UNKNOWN", "PENDING"),
					RepairRefs:        []string{"repair.fixture/v1"},
				},
				Governance: terminalGovernance(nil, nil),
			},
		},
		Edges: []workflow.Edge{
			{From: hiddenNodeRead, To: hiddenNodeSync, RouteKey: string(workflow.OutcomeSucceeded)},
			{From: hiddenNodeRead, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeRejected)},
			{From: hiddenNodeRead, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeUnknown)},
			{From: hiddenNodeRead, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeAmbiguous)},

			{From: hiddenNodeSync, To: hiddenNodeObserve, RouteKey: string(workflow.OutcomeSucceeded)},
			{From: hiddenNodeSync, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeRejected)},
			{From: hiddenNodeSync, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeUnknown)},
			{From: hiddenNodeSync, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeAmbiguous)},

			{From: hiddenNodeObserve, To: hiddenNodeCommit, RouteKey: string(workflow.OutcomePass)},
			{From: hiddenNodeObserve, To: hiddenNodeDegraded, RouteKey: string(workflow.OutcomeFail)},
			{From: hiddenNodeObserve, To: hiddenNodeDegraded, RouteKey: string(workflow.OutcomePartial)},
			{From: hiddenNodeObserve, To: hiddenNodeRepair, RouteKey: string(workflow.OutcomeUnknown)},
		},
	}
}

func hiddenEffectRegistry(t *testing.T, reached func()) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	readDef := readOnlyDefinition(hiddenCapRead, "learning.evidence")
	writeDef := readOnlyDefinition(hiddenCapWrite, "learning.evidence")
	writeDef.EffectClass = capability.EffectExternalMutation
	writeDef.WriteData = capability.DataDomainFieldSet{DataDomains: []string{"learning.evidence"}}
	writeDef.AuthZScopeRef = "scope:learning.evidence.write"
	writeDef.IdempotencyPolicyRef = "idempotency.effect-key.v1"
	writeDef.RiskClass = "HIGH"
	observeDef := readOnlyDefinition(hiddenCapObserve, "learning.evidence")

	readHandler := func(_ context.Context, _ any) (any, error) {
		return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"worker_id": simulate.NewBranded("WorkerID", "w1")}}, nil
	}
	writeHandler := func(_ context.Context, _ any) (any, error) {
		reached()
		return simulate.CapabilityResponse{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"submission_id": simulate.NewString("sub-1")}}, nil
	}
	observeHandler := func(_ context.Context, _ any) (any, error) {
		return simulate.CapabilityResponse{Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{}}, nil
	}
	if err := r.Register(readDef, readHandler); err != nil {
		t.Fatalf("publish %s: %v", hiddenCapRead, err)
	}
	if err := r.Register(writeDef, writeHandler); err != nil {
		t.Fatalf("publish %s: %v", hiddenCapWrite, err)
	}
	if err := r.Register(observeDef, observeHandler); err != nil {
		t.Fatalf("publish %s: %v", hiddenCapObserve, err)
	}
	return r
}

func mustCompileHiddenEffectPlan(t *testing.T, reached func()) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(hiddenEffectDefinition(), workflow.Options{
		Phase:        workflow.PhaseP1B,
		Capabilities: hiddenEffectRegistry(t, reached),
	})
	if err != nil {
		t.Fatalf("hidden-effect fixture must compile under P1B: %v", err)
	}
	return plan
}
