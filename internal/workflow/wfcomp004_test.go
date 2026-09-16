package workflow_test

import (
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Fixture capability identities and node ids for the concurrent fan-out
// definition WF-COMP-004 needs. The effect-carrying fixture in
// fixtures_test.go is linear; nothing in this repository declared a PARALLEL
// or a JOIN before this ticket, so the branch analysis needs its own.
const (
	capWriteBenefits = "fixture.benefits.enroll_worker"
	capWriteLearning = "fixture.learning.assign_curriculum"
	capWriteBenefit2 = "fixture.benefits.adjust_coverage"

	pxParallel   = "fan_out_onboarding"
	pxBenefits   = "enroll_benefits"
	pxLearning   = "assign_learning"
	pxJoin       = "join_onboarding"
	pxEndCommit  = "end_committed"
	pxEndDegrade = "end_degraded"
	pxEndRepair  = "end_repair"
)

// parallelRegistry publishes the two disjoint internal-mutation capabilities
// the concurrent branches bind, plus a second benefits writer used only by
// the conflicting-write-set mutants.
//
// Internal (not external) mutation is deliberate: it is a real write, with a
// real effect key and a real declared write data domain, without dragging in
// WF-COMP-003's separate requirement that every external effect be observed
// and failure-routed. This fixture is about concurrency, not observation.
func parallelRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	defs := []capability.Definition{
		fixtureCapability(capWriteBenefits, "benefits", capability.EffectInternalMutation),
		fixtureCapability(capWriteLearning, "learning", capability.EffectInternalMutation),
		fixtureCapability(capWriteBenefit2, "benefits", capability.EffectInternalMutation),
	}
	for _, d := range defs {
		if err := r.Register(d, echoHandler); err != nil {
			t.Fatalf("publish fixture capability %s: %v", d.Key(), err)
		}
	}
	return r
}

func parallelOptions(t *testing.T) workflow.Options {
	t.Helper()
	return workflow.Options{Phase: workflow.PhaseStructural, Capabilities: parallelRegistry(t)}
}

// parallelBranchNode builds one concurrent branch's single mutating node.
func parallelBranchNode(id, capID, domain, effectBinding string) workflow.Node {
	return workflow.Node{
		ID:           id,
		Type:         workflow.StepCapability,
		InputSchema:  capSchema(capID, "request"),
		OutputSchema: capSchema(capID, "response"),
		Inputs:       []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
		Outputs:      []workflow.Field{{Path: "record_id", Type: stringType()}},
		InputMappings: []workflow.Mapping{
			{Target: "worker_id", Source: inputSource("worker_id")},
		},
		Capability: &workflow.CapabilityRef{
			ID: capID, Version: 1,
			OperationMode:   workflow.ModeExecute,
			AuthorityScopes: []string{"scope:" + domain + ".write"},
			EffectBinding:   effectBinding,
		},
		Governance: governedNode(workflow.RevalidatePreEffect),
		// Each branch writes its own data domain inside that domain's own
		// commit boundary (WF-RUN-037).
		EffectRole: workflow.RoleAuthoritativeCore,
	}
}

// parallelDefinition is a PARALLEL fanning one SUCCEEDED route to two
// concurrent branches that write disjoint data domains, joined by a JOIN that
// accounts for both, then a terminal per join outcome.
func parallelDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        "fixture.workflows.concurrent_onboarding",
		Version:           1,
		Name:              "Concurrent onboarding fixture",
		InputSchema:       fixtureSchemaRef("OnboardingInput"),
		OutputSchema:      fixtureSchemaRef("OnboardingResult"),
		VariablesSchema:   fixtureSchemaRef("OnboardingVariables"),
		TenantScope:       "acme",
		OrganizationScope: "acme/engineering",
		RiskClass:         "MEDIUM",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       pxParallel,
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
		},
		Outputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
			{Path: "terminal_code", Type: stringType()},
		},
		Limits:                workflow.Limits{MaxFanOut: 4, MaxDepth: 10, MaxNodes: 12},
		FailurePolicyRef:      "policy.workflow.failure.execute/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.execute/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.onboarding/v1",
		Nodes: []workflow.Node{
			{
				ID:           pxParallel,
				Type:         workflow.StepParallel,
				InputSchema:  fixtureSchemaRef("FanOutInput"),
				OutputSchema: fixtureSchemaRef("FanOutResult"),
				Inputs:       []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: inputSource("worker_id")},
				},
				DeclaredEffect: capability.EffectPure,
			},
			parallelBranchNode(pxBenefits, capWriteBenefits, "benefits", "benefits.enrollment"),
			parallelBranchNode(pxLearning, capWriteLearning, "learning", "learning.curriculum"),
			{
				ID:             pxJoin,
				Type:           workflow.StepJoin,
				InputSchema:    fixtureSchemaRef("JoinInput"),
				OutputSchema:   fixtureSchemaRef("JoinResult"),
				DeclaredEffect: capability.EffectPure,
			},
			{
				ID:            pxEndCommit,
				Type:          workflow.StepEnd,
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("COMMITTED"),
				End: &workflow.EndSpec{
					TerminalCode:      "COMMITTED",
					RuntimeStatus:     workflow.RuntimeCompleted,
					CompletionMapping: fixtureCompletion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture.onboarding/v1",
				},
				Governance: terminalGovernance(),
			},
			{
				ID:            pxEndDegrade,
				Type:          workflow.StepEnd,
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("COMMITTED_DEGRADED"),
				End: &workflow.EndSpec{
					TerminalCode:      "COMMITTED_DEGRADED",
					RuntimeStatus:     workflow.RuntimeCompleted,
					CompletionMapping: fixtureCompletion("APPROVED", "COMMITTED", "COMPLETED", "DEGRADED", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture.onboarding/v1",
					RepairRefs:        []string{"repair.fixture.onboarding/v1"},
				},
				Governance: terminalGovernance(),
			},
			{
				ID:            pxEndRepair,
				Type:          workflow.StepEnd,
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("REPAIR_REQUIRED"),
				End: &workflow.EndSpec{
					TerminalCode:      "REPAIR_REQUIRED",
					RuntimeStatus:     workflow.RuntimeRepairRequired,
					CompletionMapping: fixtureCompletion("APPROVED", "REPAIR_REQUIRED", "UNKNOWN", "UNKNOWN", "PENDING"),
					RepairRefs:        []string{"repair.fixture.onboarding/v1"},
				},
				Governance: terminalGovernance(),
			},
		},
		Edges: []workflow.Edge{
			// One route key, two targets: the fan-out that makes the two
			// branches concurrent rather than alternative.
			{From: pxParallel, To: pxBenefits, RouteKey: "SUCCEEDED"},
			{From: pxParallel, To: pxLearning, RouteKey: "SUCCEEDED"},
			{From: pxParallel, To: pxEndRepair, RouteKey: "FAILED"},

			{From: pxBenefits, To: pxJoin, RouteKey: "SUCCEEDED"},
			{From: pxBenefits, To: pxEndRepair, RouteKey: "REJECTED"},
			{From: pxBenefits, To: pxEndRepair, RouteKey: "UNKNOWN"},
			{From: pxBenefits, To: pxEndRepair, RouteKey: "AMBIGUOUS"},

			{From: pxLearning, To: pxJoin, RouteKey: "SUCCEEDED"},
			{From: pxLearning, To: pxEndRepair, RouteKey: "REJECTED"},
			{From: pxLearning, To: pxEndRepair, RouteKey: "UNKNOWN"},
			{From: pxLearning, To: pxEndRepair, RouteKey: "AMBIGUOUS"},

			{From: pxJoin, To: pxEndCommit, RouteKey: "SUCCEEDED"},
			{From: pxJoin, To: pxEndDegrade, RouteKey: "PARTIAL"},
			{From: pxJoin, To: pxEndRepair, RouteKey: "FAILED"},
		},
	}
}

func terminalGovernance() workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:               "FIXTURE_EXECUTE",
		Classification:        "CONFIDENTIAL_HR",
		RevalidationBoundary:  workflow.RevalidatePreClosure,
		DataAccessManifestRef: "dam.fixture/v1",
	}
}

// mustCompileParallel compiles the concurrent fixture and reports the full
// diagnostic set on failure.
func mustCompileParallel(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(parallelDefinition(), parallelOptions(t))
	if err != nil {
		t.Fatalf("the concurrent fixture must compile: %v", err)
	}
	return plan
}

// --- WF-COMP-004 -----------------------------------------------------------

// TestTodo_WF_COMP_004 is the PRIMARY test: a plan whose concurrent branches
// declare disjoint write sets publishes, and the published plan states the
// branch consistency vectors, the join contract, the safe checkpoints and the
// intervention (cancellation/pause) eligibility as compiled facts rather than
// leaving them to an operator's judgement.
func TestTodo_WF_COMP_004(t *testing.T) {
	plan := mustCompileParallel(t)
	conc := plan.Concurrency
	if conc == nil {
		t.Fatal("a plan with a PARALLEL node states no concurrency analysis")
	}

	if len(conc.Branches) != 2 {
		t.Fatalf("branch consistency vectors = %d, want 2 (%+v)", len(conc.Branches), conc.Branches)
	}
	byEntry := map[string]workflow.BranchConsistencyVector{}
	for _, b := range conc.Branches {
		byEntry[b.EntryNodeID] = b
	}
	benefits, ok := byEntry[pxBenefits]
	if !ok {
		t.Fatalf("no branch vector for %s: %+v", pxBenefits, conc.Branches)
	}
	learning, ok := byEntry[pxLearning]
	if !ok {
		t.Fatalf("no branch vector for %s: %+v", pxLearning, conc.Branches)
	}
	for _, b := range []workflow.BranchConsistencyVector{benefits, learning} {
		if b.ParallelNodeID != pxParallel {
			t.Fatalf("branch %s names PARALLEL %q, want %q", b.EntryNodeID, b.ParallelNodeID, pxParallel)
		}
		if !reflect.DeepEqual(b.JoinNodeIDs, []string{pxJoin}) {
			t.Fatalf("branch %s joins %v, want [%s]", b.EntryNodeID, b.JoinNodeIDs, pxJoin)
		}
		if len(b.WriteKeys) != 1 {
			t.Fatalf("branch %s declares write keys %v, want exactly one", b.EntryNodeID, b.WriteKeys)
		}
		if b.Irreversible {
			t.Fatalf("branch %s is reported irreversible; it carries only an internal mutation", b.EntryNodeID)
		}
	}
	if !reflect.DeepEqual(benefits.WriteDomains, []string{"benefits"}) {
		t.Fatalf("benefits branch write domains = %v, want [benefits]", benefits.WriteDomains)
	}
	if !reflect.DeepEqual(learning.WriteDomains, []string{"learning"}) {
		t.Fatalf("learning branch write domains = %v, want [learning]", learning.WriteDomains)
	}
	// A branch is the nodes up to but excluding the JOIN that closes it: the
	// JOIN belongs to no branch, which is what makes it the place their
	// consistency is re-established.
	for _, b := range conc.Branches {
		for _, id := range b.NodeIDs {
			if id == pxJoin {
				t.Fatalf("branch %s claims the JOIN %s as one of its own nodes", b.EntryNodeID, pxJoin)
			}
		}
	}

	if len(conc.Joins) != 1 {
		t.Fatalf("join contracts = %d, want 1 (%+v)", len(conc.Joins), conc.Joins)
	}
	join := conc.Joins[0]
	if join.JoinNodeID != pxJoin {
		t.Fatalf("join contract names %q, want %q", join.JoinNodeID, pxJoin)
	}
	wantBranches := []string{pxLearning, pxBenefits}
	sort.Strings(wantBranches)
	if !reflect.DeepEqual(join.BranchEntryNodeIDs, wantBranches) {
		t.Fatalf("join branches = %v, want %v", join.BranchEntryNodeIDs, wantBranches)
	}
	if !reflect.DeepEqual(join.ArrivingNodeIDs, wantBranches) {
		t.Fatalf("join arrivals = %v, want %v", join.ArrivingNodeIDs, wantBranches)
	}
	wantAccounted := append(append([]string(nil), benefits.WriteKeys...), learning.WriteKeys...)
	if len(join.AccountedWriteKeys) != len(wantAccounted) {
		t.Fatalf("join accounts for %v, want every branch write key %v",
			join.AccountedWriteKeys, wantAccounted)
	}

	// This fixture holds no external effect open, so nothing is inside an
	// atomic region and every position admits intervention.
	if len(conc.AtomicRegions) != 0 || len(conc.InterventionIneligibleNodes) != 0 {
		t.Fatalf("a plan with only internal mutations declares atomic regions %+v / ineligible %v, want none",
			conc.AtomicRegions, conc.InterventionIneligibleNodes)
	}
	for _, n := range plan.Nodes {
		if !plan.InterventionEligible(n.ID) {
			t.Fatalf("node %s is intervention-ineligible in a plan with no atomic region", n.ID)
		}
	}

	// The safe checkpoints are compiled: every terminal, and nothing an
	// author asked for, because this fixture asks for nothing.
	for _, n := range plan.Nodes {
		want := n.Type == workflow.StepEnd
		if n.SafePoint != want {
			t.Fatalf("node %s safe_point = %v, want %v", n.ID, n.SafePoint, want)
		}
	}
}

// TestTodo_WF_COMP_004_Property proves the analysis is a function of the
// graph, not of the order the author happened to write it in: reversing the
// node and edge declaration order produces the same plan digest and the same
// concurrency facts. A conflict analysis whose answer depended on declaration
// order would be a coin flip dressed as a proof.
func TestTodo_WF_COMP_004_Property(t *testing.T) {
	base := mustCompileParallel(t)

	// Reversing the node declaration order must change nothing at all: the
	// compiler normalizes nodes by id, and the two fan-out edges are ordered
	// by target rather than by the order the branches were listed in.
	shuffledNodes := parallelDefinition()
	for i, j := 0, len(shuffledNodes.Nodes)-1; i < j; i, j = i+1, j-1 {
		shuffledNodes.Nodes[i], shuffledNodes.Nodes[j] = shuffledNodes.Nodes[j], shuffledNodes.Nodes[i]
	}
	other, err := workflow.Compile(shuffledNodes, parallelOptions(t))
	if err != nil {
		t.Fatalf("the reordered definition must compile identically: %v", err)
	}
	if other.Digest() != base.Digest() {
		t.Fatalf("reordering the node declarations changed the plan digest: %s vs %s", other.Digest(), base.Digest())
	}
	if !reflect.DeepEqual(other.Concurrency, base.Concurrency) {
		t.Fatalf("reordering the node declarations changed the concurrency analysis:\n got %+v\nwant %+v",
			other.Concurrency, base.Concurrency)
	}

	// Reversing the edge declaration order must not change the concurrency
	// analysis either. It is asserted separately from the digest because
	// ReachabilityProof.Order, which this ticket does not own, still follows
	// the order the author listed the edges in.
	shuffledEdges := parallelDefinition()
	for i, j := 0, len(shuffledEdges.Edges)-1; i < j; i, j = i+1, j-1 {
		shuffledEdges.Edges[i], shuffledEdges.Edges[j] = shuffledEdges.Edges[j], shuffledEdges.Edges[i]
	}
	byEdges, err := workflow.Compile(shuffledEdges, parallelOptions(t))
	if err != nil {
		t.Fatalf("the edge-reordered definition must compile: %v", err)
	}
	if !reflect.DeepEqual(byEdges.Concurrency, base.Concurrency) {
		t.Fatalf("reordering the edge declarations changed the concurrency analysis:\n got %+v\nwant %+v",
			byEdges.Concurrency, base.Concurrency)
	}
	if !reflect.DeepEqual(byEdges.Edges, base.Edges) {
		t.Fatalf("reordering the edge declarations changed the normalized edge list")
	}

	// The same property over the effect-carrying linear fixture, whose
	// analysis is an atomic region rather than a branch set.
	effects, err := workflow.Compile(effectsDefinition(), effectsOptions(t))
	if err != nil {
		t.Fatalf("effects fixture must compile: %v", err)
	}
	reordered := effectsDefinition()
	for i, j := 0, len(reordered.Nodes)-1; i < j; i, j = i+1, j-1 {
		reordered.Nodes[i], reordered.Nodes[j] = reordered.Nodes[j], reordered.Nodes[i]
	}
	effects2, err := workflow.Compile(reordered, effectsOptions(t))
	if err != nil {
		t.Fatalf("reordered effects fixture must compile: %v", err)
	}
	if !reflect.DeepEqual(effects2.Concurrency, effects.Concurrency) {
		t.Fatalf("reordering changed the atomic-region analysis:\n got %+v\nwant %+v",
			effects2.Concurrency, effects.Concurrency)
	}
}

// TestTodo_WF_COMP_004_Race compiles the same definition from many goroutines
// at once. The compiler is a pure function of its inputs and holds no
// package-level state, so every concurrent compilation must produce the same
// digest and a deeply equal concurrency analysis; a shared map or a cached
// intermediate would show up here as a data race under -race or as divergent
// output without it.
func TestTodo_WF_COMP_004_Race(t *testing.T) {
	want := mustCompileParallel(t)
	opts := parallelOptions(t)

	const workers = 8
	digests := make([]string, workers)
	summaries := make([]*workflow.ConcurrencySummary, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			plan, err := workflow.Compile(parallelDefinition(), opts)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = plan.Digest()
			summaries[i] = plan.Concurrency
		}(i)
	}
	wg.Wait()
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent compilation %d failed: %v", i, errs[i])
		}
		if digests[i] != want.Digest() {
			t.Fatalf("concurrent compilation %d digested to %s, want %s", i, digests[i], want.Digest())
		}
		if !reflect.DeepEqual(summaries[i], want.Concurrency) {
			t.Fatalf("concurrent compilation %d produced a different concurrency analysis", i)
		}
	}
}

// TestTodo_WF_COMP_004_Fault is the RED clause: each of the three failure
// shapes this analysis owns is refused at publication, with the code that
// rule owns, at the location that names the offending node.
func TestTodo_WF_COMP_004_Fault(t *testing.T) {
	t.Run("conflicting_write_set_by_effect_key", func(t *testing.T) {
		// Both branches bound to the same capability, effect binding and
		// idempotency mapping: one logical effect, produced twice,
		// concurrently.
		def := parallelDefinition()
		branch := nodeRef(t, &def, pxLearning)
		*branch = parallelBranchNode(pxLearning, capWriteBenefits, "benefits", "benefits.enrollment")
		d := mustReject(t, def, parallelOptions(t), workflow.CodeConflictingWriteSet)
		if !d.HasAt(workflow.CodeConflictingWriteSet, pxParallel) {
			t.Fatalf("the conflict is not reported at the PARALLEL that opened the branches: %v", d.Errors)
		}
	})

	t.Run("conflicting_write_set_by_data_domain", func(t *testing.T) {
		// Two different capabilities -- so two different effect keys -- that
		// nonetheless declare the same write data domain. An effect-key-only
		// comparison would call this disjoint; it is not.
		def := parallelDefinition()
		branch := nodeRef(t, &def, pxLearning)
		*branch = parallelBranchNode(pxLearning, capWriteBenefit2, "benefits", "benefits.coverage")
		mustReject(t, def, parallelOptions(t), workflow.CodeConflictingWriteSet)
	})

	t.Run("unaccounted_branch_effect", func(t *testing.T) {
		// The learning branch writes and then terminates without reaching the
		// JOIN, so nothing reconciles its effect with its sibling's.
		def := parallelDefinition()
		for i := range def.Edges {
			if def.Edges[i].From == pxLearning && def.Edges[i].To == pxJoin {
				def.Edges[i].To = pxEndDegrade
			}
		}
		d := mustReject(t, def, parallelOptions(t), workflow.CodeUnaccountedBranchEffect)
		if !d.HasAt(workflow.CodeUnaccountedBranchEffect, pxLearning) {
			t.Fatalf("the unaccounted effect is not reported at the branch entry: %v", d.Errors)
		}
	})

	t.Run("unsafe_checkpoint_inside_an_atomic_region", func(t *testing.T) {
		// The OBSERVE that closes the payroll sync's atomic region is exactly
		// the position at which an intervention would abandon an unconfirmed
		// external effect. An author asking for a safe point there is refused,
		// not quietly obeyed.
		def := effectsDefinition()
		nodeRef(t, &def, fxObserve).SafePointRequested = true
		d := mustReject(t, def, effectsOptions(t), workflow.CodeUnsafeCheckpoint)
		if !d.HasAt(workflow.CodeUnsafeCheckpoint, fxObserve) {
			t.Fatalf("the unsafe checkpoint is not reported at the requesting node: %v", d.Errors)
		}
	})
}

// TestTodo_WF_COMP_004_Mutation kills one mutant per compiled fact: each
// single-point edit changes the analysis in exactly the way the rule
// predicts, so the facts in the plan are load-bearing rather than decorative.
func TestTodo_WF_COMP_004_Mutation(t *testing.T) {
	t.Run("an_atomic_region_is_stated_and_bounds_intervention", func(t *testing.T) {
		plan, err := workflow.Compile(effectsDefinition(), effectsOptions(t))
		if err != nil {
			t.Fatalf("effects fixture must compile: %v", err)
		}
		conc := plan.Concurrency
		if conc == nil || len(conc.AtomicRegions) != 1 {
			t.Fatalf("an external mutation states %v atomic regions, want exactly 1", conc)
		}
		region := conc.AtomicRegions[0]
		if region.EntryNodeID != fxSync {
			t.Fatalf("atomic region opens at %q, want the mutating node %q", region.EntryNodeID, fxSync)
		}
		if !reflect.DeepEqual(region.ExitNodeIDs, []string{fxObserve}) {
			t.Fatalf("atomic region exits = %v, want [%s]", region.ExitNodeIDs, fxObserve)
		}
		if len(region.EffectKeys) != 1 {
			t.Fatalf("atomic region holds effect keys %v, want exactly the mutation's own", region.EffectKeys)
		}
		// The mutating node itself is still a safe point: the effect has not
		// run when the frontier sits on it. The observation is not: the
		// effect has run and nothing has confirmed it.
		if plan.InAtomicRegion(fxSync) {
			t.Fatalf("%s is reported inside its own atomic region; pausing before the effect runs is safe", fxSync)
		}
		if !plan.InAtomicRegion(fxObserve) {
			t.Fatalf("%s is not reported inside the atomic region it closes", fxObserve)
		}
		if plan.InAtomicRegion(fxRead) {
			t.Fatalf("%s runs before the mutation and is not inside its region", fxRead)
		}
	})

	t.Run("a_requested_safe_point_outside_a_region_is_honored", func(t *testing.T) {
		base := mustCompileParallel(t)
		before, _ := base.Node(pxBenefits)
		if before.SafePoint {
			t.Fatalf("%s is already a safe point; this mutant proves nothing", pxBenefits)
		}
		def := parallelDefinition()
		nodeRef(t, &def, pxBenefits).SafePointRequested = true
		plan, err := workflow.Compile(def, parallelOptions(t))
		if err != nil {
			t.Fatalf("a safe point requested outside every atomic region must compile: %v", err)
		}
		after, _ := plan.Node(pxBenefits)
		if !after.SafePoint {
			t.Fatalf("a safe point requested at %s outside every atomic region was dropped", pxBenefits)
		}
		if plan.Digest() == base.Digest() {
			t.Fatal("honoring a safe-point request did not change the plan's content identity")
		}
	})

	t.Run("a_second_edge_on_a_non_fan_out_route_is_still_a_duplicate", func(t *testing.T) {
		// Only PARALLEL declares a fan-out route. Loosening the duplicate
		// check for it must not loosen it anywhere else.
		def := parallelDefinition()
		def.Edges = append(def.Edges, workflow.Edge{From: pxJoin, To: pxEndDegrade, RouteKey: "SUCCEEDED"})
		mustReject(t, def, parallelOptions(t), workflow.CodeDuplicateRoute)
	})

	t.Run("a_zero_effect_plan_states_no_concurrency_analysis", func(t *testing.T) {
		// The promotion reference has no PARALLEL and no external mutation,
		// so it has nothing to state -- and its canonical bytes, and
		// therefore its published digest, are untouched by this analysis.
		plan := mustCompilePromotion(t)
		if plan.Concurrency != nil {
			t.Fatalf("the zero-effect promotion reference states a concurrency analysis: %+v", plan.Concurrency)
		}
	})
}
