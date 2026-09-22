package talent

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
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

// TestTodo_CONF_012 is the PRIMARY conformance claim: the compiled talent
// performance calibration reference workflow walks, through the real
// SIMULATE-mode interpreter, along the golden path CONF-012's GREEN clause
// names - a population snapshot (FACT), a human rating claim
// (HUMAN_OPINION), a model recommendation (MODEL_INFERENCE), a calibration
// committee resolution, a bound footprint, a bound proposal, a
// calibration-and-correction decision and a post-decision observation - and
// ends at a terminal that reports exact, separately-tracked lifecycle
// dimensions with the calibration's provenance-retention, calibration-record
// and evidence-retention obligations outstanding rather than collapsed into
// a false success.
func TestTodo_CONF_012(t *testing.T) {
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
		NodeReadPopulation,
		NodeReadRatingClaim,
		NodeReadModelInference,
		NodeResolveCalibrationCommittee,
		NodeComputeCalibrationFootprint,
		NodeBuildAssessmentProposal,
		NodeCalibrationDecision,
		NodeObserveCalibrationRecord,
		NodeEndPendingApprovals,
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

	if receipt.Terminal.TerminalCode != "TALENT_CALIBRATION_PENDING_APPROVALS" {
		t.Errorf("terminal code = %q, want TALENT_CALIBRATION_PENDING_APPROVALS", receipt.Terminal.TerminalCode)
	}
	wantObligations := []string{ObligationProvenanceRetention, ObligationCalibrationRecord, ObligationEvidenceRetention}
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

	if len(receipt.WorkItems) != 3 {
		t.Fatalf("work items = %d, want 3 (one per declared approval requirement)", len(receipt.WorkItems))
	}
	wantApprovals := []string{ApprovalCalibrationCommittee, ApprovalHRBP, ApprovalRewardsPartner}
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

// TestTodo_CONF_012_Conformance proves the whole terminal lattice at once:
// every reachable terminal reports the exact five-dimension tuple CONF-012's
// GREEN clause requires, and every RED clause the todo names (stale
// population, unauthorized rating, calibration conflict, correction) is
// proven by name, never merged into an approximate "it failed somehow"
// terminal.
func TestTodo_CONF_012_Conformance(t *testing.T) {
	successPending := simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"}
	hardBlocked := simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"}
	degradedOrUnknown := simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"}
	revisionCreated := simulate.LifecycleState{RequestState: "SUPERSEDED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"}

	cases := []struct {
		name        string
		env         *Environment
		params      Params
		terminal    string
		want        simulate.LifecycleState
		obligations []string
	}{
		{
			"pending_approvals", GoldenEnvironment(), Params{}, NodeEndPendingApprovals, successPending,
			[]string{ObligationProvenanceRetention, ObligationCalibrationRecord, ObligationEvidenceRetention},
		},
		{
			"stale_population_unknown", StalePopulationEnvironment(), Params{}, NodeEndStalePopulation, degradedOrUnknown,
			[]string{ObligationEvidenceRetention},
		},
		{
			// ObligationState=NOT_APPLICABLE on a HARD_BLOCKED terminal means
			// the terminal itself declares no OutstandingObligationRefs (the
			// compiler refuses declaring one alongside NOT_APPLICABLE); the
			// records-retention obligation still applies via the terminal's
			// governance, not via the receipt's outstanding-obligation set.
			"unauthorized_rating_blocked", GoldenEnvironment(), Params{RatingAuthorID: "principal:mismatched-author"}, NodeEndUnauthorizedRating, hardBlocked,
			nil,
		},
		{
			"calibration_conflict_blocked", CalibrationConflictEnvironment(), Params{}, NodeEndCalibrationConflict, hardBlocked,
			nil,
		},
		{
			"correction_creates_new_revision", GoldenEnvironment(),
			Params{IsCorrection: true, PriorAssessmentDigest: "sha256:prior-assessment-fixture-0001"},
			NodeEndRevisionCreated, revisionCreated,
			[]string{ObligationPriorAssessmentPreserved, ObligationEvidenceRetention},
		},
		{
			"degraded_repair", DegradedObservationEnvironment(), Params{}, NodeEndDegradedRepair, degradedOrUnknown,
			[]string{ObligationCalibrationRecord, ObligationEvidenceRetention},
		},
		{
			"missing_context_unknown", GoldenEnvironment(), Params{OmitPolicyContext: true}, NodeEndUnknown, degradedOrUnknown,
			[]string{ObligationEvidenceRetention},
		},
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
			if c.obligations != nil && !equalSets(receipt.Terminal.OutstandingObligationRefs, c.obligations) {
				t.Fatalf("outstanding obligations = %v, want %v", receipt.Terminal.OutstandingObligationRefs, c.obligations)
			}
		})
	}

	// ---- RED clauses proven explicitly by name, beyond the table above.

	t.Run("stale_population_is_unknown_not_a_silent_success", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, StalePopulationEnvironment(), Params{}))
		if receipt.Lifecycle.BusinessState == "COMPLETED" || receipt.Lifecycle.ConsistencyState == "CONSISTENT" {
			t.Fatalf("a stale population must never resolve to a completed, consistent outcome: %+v", receipt.Lifecycle)
		}
	})

	t.Run("unauthorized_rating_is_rejected_not_silently_accepted", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{RatingAuthorID: "principal:mismatched-author"}))
		if receipt.Lifecycle.RequestState != "REJECTED" {
			t.Fatalf("an unauthorized rating author must be REJECTED, got RequestState=%q", receipt.Lifecycle.RequestState)
		}
	})

	t.Run("calibration_conflict_is_rejected_not_silently_agreed", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, CalibrationConflictEnvironment(), Params{}))
		if receipt.Lifecycle.RequestState != "REJECTED" {
			t.Fatalf("a contested calibration must be REJECTED, got RequestState=%q", receipt.Lifecycle.RequestState)
		}
	})

	t.Run("correction_never_mutates_the_prior_assessment", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{IsCorrection: true, PriorAssessmentDigest: "sha256:prior-assessment-fixture-0001"}))
		if receipt.Lifecycle.RequestState == "SIMULATED" {
			t.Fatalf("a correction reaching the new-revision terminal must never report the same RequestState as an original, unmutated request")
		}
		if receipt.Lifecycle.RequestState != "SUPERSEDED" {
			t.Fatalf("correction RequestState = %q, want SUPERSEDED (a distinct new revision, not a mutation)", receipt.Lifecycle.RequestState)
		}
		node, ok := mustSetup(t, GoldenEnvironment(), Params{IsCorrection: true, PriorAssessmentDigest: "sha256:prior-assessment-fixture-0001"}).Plan.Node(NodeEndRevisionCreated)
		if !ok || node.Terminal == nil {
			t.Fatalf("plan declares no %s terminal", NodeEndRevisionCreated)
		}
		found := false
		for _, ref := range node.Terminal.OutstandingObligationRefs {
			if ref == ObligationPriorAssessmentPreserved {
				found = true
			}
		}
		if !found {
			t.Fatalf("new-revision terminal does not outstanding-obligate preservation of the prior assessment digest: %v", node.Terminal.OutstandingObligationRefs)
		}
	})

	t.Run("degraded_calibration_record_is_never_reported_consistent", func(t *testing.T) {
		setup := mustSetup(t, DegradedObservationEnvironment(), Params{})
		receipt := mustRun(t, setup)
		if receipt.Lifecycle.ConsistencyState == "CONSISTENT" || receipt.Lifecycle.BusinessState == "COMPLETED" {
			t.Fatalf("a degraded calibration-record observation must never be reported as consistent, completed: %+v", receipt.Lifecycle)
		}
		node, ok := setup.Plan.Node(NodeEndDegradedRepair)
		if !ok || node.Terminal == nil || len(node.Terminal.RepairRefs) == 0 {
			t.Fatalf("degraded terminal carries no RepairRefs; a degraded dimension must create bounded repair evidence")
		}
	})

	// The degraded repair terminal's obligation set must never widen to
	// include the unrelated provenance-retention obligation, which this
	// particular observation failure never touched.
	t.Run("degraded_repair_does_not_widen_to_unrelated_obligations", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, DegradedObservationEnvironment(), Params{}))
		for _, got := range receipt.Terminal.OutstandingObligationRefs {
			if got == ObligationProvenanceRetention {
				t.Fatalf("degraded calibration-record terminal names unrelated obligation %q", ObligationProvenanceRetention)
			}
		}
	})
}

// TestTodo_CONF_012_Mutation proves the calibration-and-correction DECISION's
// own precedence is independently load-bearing on two fronts: (1) the
// footprint TRANSFORM never itself decides that a mismatch or conflict
// blocks anything - it only reports flags - so a mutation that deleted the
// DECISION's own checks could not hide behind the transform quietly blocking
// on its behalf; and (2) a correction is only recognized because of the
// is_correction AND prior_assessment_digest!="" pair together, never by
// accident of either one alone.
func TestTodo_CONF_012_Mutation(t *testing.T) {
	t.Run("footprint_transform_reports_but_never_blocks", func(t *testing.T) {
		result, err := Transforms{}.Transform(context.Background(), simulate.TransformRequest{
			Transform: workflow.CompiledTransform{TransformRef: TransformCalibrationFootprint},
			Inputs: simulate.Bag{
				"population_active":           simulate.NewBool(true),
				"rating_author_id":            simulate.NewBranded("WorkerID", "principal:mismatched-author"),
				"rater_id":                    simulate.NewBranded("WorkerID", "principal:manager-4471"),
				"rating_value":                simulate.NewString("EXCEEDS_EXPECTATIONS"),
				"calibration_adjusted_rating": simulate.NewString("MEETS_EXPECTATIONS"),
				"calibration_status":          simulate.NewString("CONTESTED"),
			},
		})
		if err != nil {
			t.Fatalf("Transform: %v", err)
		}
		mismatch, err := result.Outputs["authority_mismatch"].Bool()
		if err != nil {
			t.Fatalf("authority_mismatch: %v", err)
		}
		conflict, err := result.Outputs["calibration_conflict"].Bool()
		if err != nil {
			t.Fatalf("calibration_conflict: %v", err)
		}
		if !mismatch || !conflict {
			t.Fatalf("footprint must report both flags accurately: mismatch=%v conflict=%v", mismatch, conflict)
		}
		if _, hasRoute := result.Outputs["route_key"]; hasRoute {
			t.Fatal("the footprint transform must never itself produce a route decision; that is the DECISION node's own job")
		}

		receipt := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{RatingAuthorID: "principal:mismatched-author"}))
		if receipt.Terminal.NodeID != NodeEndUnauthorizedRating {
			t.Fatalf("terminal = %q, want %q; the DECISION's own authority check must still block "+
				"even though the footprint transform never blocks anything itself", receipt.Terminal.NodeID, NodeEndUnauthorizedRating)
		}
	})

	t.Run("correction_route_requires_both_is_correction_and_a_nonempty_prior_digest", func(t *testing.T) {
		env := GoldenEnvironment()

		both := mustRun(t, mustSetup(t, env, Params{IsCorrection: true, PriorAssessmentDigest: "sha256:prior-assessment-fixture-0001"}))
		if both.Terminal.NodeID != NodeEndRevisionCreated {
			t.Fatalf("is_correction=true with a prior digest: terminal = %q, want %q", both.Terminal.NodeID, NodeEndRevisionCreated)
		}

		flagOnly := mustRun(t, mustSetup(t, env, Params{IsCorrection: true, PriorAssessmentDigest: ""}))
		if flagOnly.Terminal.NodeID == NodeEndRevisionCreated {
			t.Fatal("is_correction=true with an empty prior digest must not reach the new-revision terminal; there is nothing to reference")
		}

		digestOnly := mustRun(t, mustSetup(t, env, Params{IsCorrection: false, PriorAssessmentDigest: "sha256:prior-assessment-fixture-0001"}))
		if digestOnly.Terminal.NodeID == NodeEndRevisionCreated {
			t.Fatal("a prior digest alone, without is_correction=true, must not reach the new-revision terminal")
		}

		if flagOnly.Terminal.NodeID != NodeEndPendingApprovals || digestOnly.Terminal.NodeID != NodeEndPendingApprovals {
			t.Fatalf("neither partial signal should divert the golden environment from CALIBRATION_OK: flagOnly=%q digestOnly=%q",
				flagOnly.Terminal.NodeID, digestOnly.Terminal.NodeID)
		}
	})

	t.Run("proposal_digest_incorporates_prior_digest_without_discarding_it", func(t *testing.T) {
		base := simulate.Bag{
			"worker_id":                   simulate.NewBranded("WorkerID", "w1"),
			"review_cycle_id":             simulate.NewBranded("ReviewCycleID", "RC-1"),
			"footprint_digest":            simulate.NewString("sha256:footprint-fixture"),
			"rating_value":                simulate.NewString("EXCEEDS_EXPECTATIONS"),
			"calibration_adjusted_rating": simulate.NewString("MEETS_EXPECTATIONS"),
			"inference_recommendation":    simulate.NewString("MODEL_RECOMMENDS_X"),
			"effective_date":              simulate.NewString("2026-12-01"),
			"is_correction":               simulate.NewBool(true),
		}
		withPriorA := clone(base)
		withPriorA["prior_assessment_digest"] = simulate.NewString("sha256:prior-A")
		withPriorB := clone(base)
		withPriorB["prior_assessment_digest"] = simulate.NewString("sha256:prior-B")
		noPrior := clone(base)
		noPrior["is_correction"] = simulate.NewBool(false)
		noPrior["prior_assessment_digest"] = simulate.NewString("")

		resultA, err := Transforms{}.Transform(context.Background(), simulate.TransformRequest{
			Transform: workflow.CompiledTransform{TransformRef: TransformBuildProposal}, Inputs: withPriorA,
		})
		if err != nil {
			t.Fatalf("Transform (prior A): %v", err)
		}
		resultB, err := Transforms{}.Transform(context.Background(), simulate.TransformRequest{
			Transform: workflow.CompiledTransform{TransformRef: TransformBuildProposal}, Inputs: withPriorB,
		})
		if err != nil {
			t.Fatalf("Transform (prior B): %v", err)
		}
		resultNone, err := Transforms{}.Transform(context.Background(), simulate.TransformRequest{
			Transform: workflow.CompiledTransform{TransformRef: TransformBuildProposal}, Inputs: noPrior,
		})
		if err != nil {
			t.Fatalf("Transform (no prior): %v", err)
		}

		digestA := resultA.Outputs["proposal_digest"].Text
		digestB := resultB.Outputs["proposal_digest"].Text
		digestNone := resultNone.Outputs["proposal_digest"].Text
		if digestA == digestB {
			t.Fatal("changing prior_assessment_digest did not change proposal_digest; the correction is not referencing the prior assessment")
		}
		if digestA == digestNone || digestB == digestNone {
			t.Fatal("a correction's proposal digest must differ from an original (non-correction) proposal digest built from the same other fields")
		}
	})
}

func clone(b simulate.Bag) simulate.Bag {
	out := make(simulate.Bag, len(b))
	for k, v := range b {
		out[k] = v
	}
	return out
}

// TestTodo_CONF_012_Security proves the REFACTOR clause structurally and at
// runtime - a model recommendation is never an authoritative decision - and
// that a write-class node cannot execute under SIMULATE no matter how it is
// reached.
func TestTodo_CONF_012_Security(t *testing.T) {
	t.Run("decision_node_never_reads_the_model_inference_output", func(t *testing.T) {
		def := ReferenceDefinition()
		var decisionNode *workflow.Node
		for i := range def.Nodes {
			if def.Nodes[i].ID == NodeCalibrationDecision {
				decisionNode = &def.Nodes[i]
			}
		}
		if decisionNode == nil {
			t.Fatalf("definition declares no %s node", NodeCalibrationDecision)
		}
		for _, m := range decisionNode.InputMappings {
			if m.Source.NodeID == NodeReadModelInference {
				t.Fatalf("DECISION InputMappings target %q reads node %q directly; a recommendation must never become the authoritative decision's input", m.Target, NodeReadModelInference)
			}
			if m.Source.Path == "inference_recommendation" {
				t.Fatalf("DECISION InputMappings target %q reads field %q; a recommendation must never become the authoritative decision's input", m.Target, m.Source.Path)
			}
		}
		// The only legitimate consumer of the model recommendation anywhere
		// in this definition is the proposal-building TRANSFORM.
		var proposalNode *workflow.Node
		for i := range def.Nodes {
			if def.Nodes[i].ID == NodeBuildAssessmentProposal {
				proposalNode = &def.Nodes[i]
			}
		}
		if proposalNode == nil {
			t.Fatalf("definition declares no %s node", NodeBuildAssessmentProposal)
		}
		referencesInference := false
		for _, m := range proposalNode.InputMappings {
			if m.Source.NodeID == NodeReadModelInference {
				referencesInference = true
			}
		}
		if !referencesInference {
			t.Fatalf("%s must reference the model recommendation for the proposal; it appears to be unused entirely", NodeBuildAssessmentProposal)
		}
		for i := range def.Nodes {
			n := &def.Nodes[i]
			if n.ID == NodeBuildAssessmentProposal {
				continue
			}
			for _, m := range n.InputMappings {
				if m.Source.NodeID == NodeReadModelInference {
					t.Fatalf("node %q reads the model-inference output via InputMappings target %q; only %s may reference it", n.ID, m.Target, NodeBuildAssessmentProposal)
				}
			}
		}
	})

	t.Run("mismatched_rater_is_blocked_at_runtime", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{RatingAuthorID: "principal:some-other-author"}))
		if receipt.Terminal.NodeID != NodeEndUnauthorizedRating {
			t.Fatalf("terminal = %q, want %q; a rating author who is not the rater of record must be blocked", receipt.Terminal.NodeID, NodeEndUnauthorizedRating)
		}
		if receipt.Lifecycle.RequestState != "REJECTED" {
			t.Fatalf("lifecycle request state = %q, want REJECTED", receipt.Lifecycle.RequestState)
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
// used only to prove CONF-012's "hidden effects" RED case independently of
// every other conformance package's own proof of the same interpreter
// guarantee. P1A refuses a write effect at compile time, so a P1A plan with a
// mutating node cannot exist; compiling under P1B is what makes the
// interpreter's own SIMULATE refusal the thing under test.

const (
	hiddenCapRead      = "fixture.conformance.talent.read_worker"
	hiddenCapWrite     = "fixture.conformance.talent.write_assessment"
	hiddenCapObserve   = "fixture.conformance.talent.observe_worker"
	hiddenNodeRead     = "read_worker"
	hiddenNodeSync     = "write_assessment"
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
		WorkflowID: "fixture.conformance.talent.hidden_effect", Version: 1, Name: "Hidden effect fixture",
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
				Capability: &workflow.CapabilityRef{ID: hiddenCapRead, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:talent.population.read"}},
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
				Capability:   &workflow.CapabilityRef{ID: hiddenCapWrite, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:talent.assessment.write"}, IdempotencyKeyMapping: "worker_id", EffectBinding: "talent.hidden_effect"},
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
				Capability: &workflow.CapabilityRef{ID: hiddenCapObserve, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:talent.calibration.read"}},
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
	readDef := readOnlyDefinition(hiddenCapRead, "talent.population")
	writeDef := readOnlyDefinition(hiddenCapWrite, "talent.assessment")
	writeDef.EffectClass = capability.EffectExternalMutation
	writeDef.WriteData = capability.DataDomainFieldSet{DataDomains: []string{"talent.assessment"}}
	writeDef.AuthZScopeRef = "scope:talent.assessment.write"
	writeDef.IdempotencyPolicyRef = "idempotency.effect-key.v1"
	writeDef.RiskClass = "HIGH"
	observeDef := readOnlyDefinition(hiddenCapObserve, "talent.calibration")

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
