package workflow_test

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Fixture capability identities for the WF-EXT-003 definition: one read, one
// internal write and one observation. The write is internal so the fixture
// needs no observation leg; the overlay is what the SIMULATE projection
// exercises.
const (
	extCapRead  = "fixture.ext.read_worker"
	extCapWrite = "fixture.ext.commit_unit"
	extCapWatch = "fixture.ext.observe_unit"
)

// Node ids of the WF-EXT-003 fixture.
const (
	extStart  = "start"
	extWait   = "wait_gate"
	extTask   = "fix_task"
	extWatch  = "watch_unit"
	extCommit = "commit_unit"
	extEndOK  = "end_ok"
	extEndDeg = "end_degraded"
	extEndRep = "end_repair"
)

func ext003Registry(t *testing.T) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	for _, d := range []capability.Definition{
		fixtureCapability(extCapRead, "people", capability.EffectReadOnly),
		fixtureCapability(extCapWrite, "people", capability.EffectInternalMutation),
		fixtureCapability(extCapWatch, "observation", capability.EffectReadOnly),
	} {
		if err := r.Register(d, echoHandler); err != nil {
			t.Fatalf("publish fixture capability %s: %v", d.Key(), err)
		}
	}
	return r
}

func ext003Options(t *testing.T) workflow.Options {
	t.Helper()
	return workflow.Options{Phase: workflow.PhaseP1B, Capabilities: ext003Registry(t)}
}

func ext003SimulateOptions(t *testing.T) workflow.Options {
	t.Helper()
	opts := ext003Options(t)
	opts.SimulateProjection = true
	return opts
}

// ext003Definition is a dual-mode P1B definition whose business outcomes need
// the compiler: the WAIT fires FIRED, the TASK documents REAPPROVED,
// WITHDRAWN and a DROPPED outcome with no kernel continuation, and the
// OBSERVE judges CONSISTENT and DEGRADED. The one write carries a SIMULATE
// mode overlay.
func ext003Definition() workflow.Definition {
	endGovernance := workflow.NodeGovernance{
		Purpose:               "FIXTURE_EXECUTE",
		Classification:        "CONFIDENTIAL_HR",
		RevalidationBoundary:  workflow.RevalidatePreClosure,
		DataAccessManifestRef: "dam.fixture/v1",
	}
	return workflow.Definition{
		WorkflowID:        "fixture.workflows.aliased_simulated",
		Version:           1,
		Name:              "Aliased and simulated fixture",
		InputSchema:       fixtureSchemaRef("AliasedSimulatedInput"),
		OutputSchema:      fixtureSchemaRef("AliasedSimulatedResult"),
		VariablesSchema:   fixtureSchemaRef("AliasedSimulatedVariables"),
		TenantScope:       "acme",
		OrganizationScope: "acme/engineering",
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute, workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       extStart,
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
		},
		Outputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
			{Path: "terminal_code", Type: stringType()},
		},
		Limits:                workflow.Limits{MaxFanOut: 6, MaxDepth: 12, MaxNodes: 16},
		FailurePolicyRef:      "policy.workflow.failure.execute/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.execute/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.payroll/v1",
		Nodes: []workflow.Node{
			{
				ID:           extStart,
				Type:         workflow.StepCapability,
				InputSchema:  capSchema(extCapRead, "request"),
				OutputSchema: capSchema(extCapRead, "response"),
				Inputs:       []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				Outputs:      []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: inputSource("worker_id")},
				},
				Capability: &workflow.CapabilityRef{
					ID: extCapRead, Version: 1,
					OperationMode:   workflow.ModeExecute,
					AuthorityScopes: []string{"scope:people.read"},
				},
				Governance: governedNode(workflow.RevalidatePreExecution),
			},
			{
				ID:             extWait,
				Type:           workflow.StepWait,
				InputSchema:    fixtureSchemaRef("ExtWaitInput"),
				OutputSchema:   fixtureSchemaRef("ExtWaitResult"),
				Wait:           &workflow.WaitSpec{WakeKind: workflow.WaitWakeAtInstant, WakeInstant: "2026-10-01T09:00:00Z", ZoneID: "America/New_York", ZoneTzdbVersion: "2026a", CalendarRef: "us-federal", CalendarVersion: "2026.1", ReferenceUpdatePolicy: "REVIEW_REQUIRED"},
				Governance:     governedNode(workflow.RevalidatePreExecution),
				OutcomeAliases: map[string]string{"FIRED": "SUCCEEDED"},
			},
			{
				ID:             extTask,
				Type:           workflow.StepTask,
				DeclaredEffect: capability.EffectPure,
				InputSchema:    fixtureSchemaRef("ExtTaskInput"),
				OutputSchema:   fixtureSchemaRef("ExtTaskResult"),
				Inputs:         []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: nodeSource(extStart, "worker_id")},
				},
				Governance: governedNode(workflow.RevalidatePreExecution),
				OutcomeAliases: map[string]string{
					"REAPPROVED": "SUCCEEDED",
					"WITHDRAWN":  "CANCELLED",
					"DROPPED":    "",
				},
			},
			{
				ID:           extWatch,
				Type:         workflow.StepObserve,
				InputSchema:  capSchema(extCapWatch, "request"),
				OutputSchema: capSchema(extCapWatch, "response"),
				Inputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "expected_state", Type: stringType()},
				},
				Outputs: []workflow.Field{
					{Path: "observed_state", Type: stringType()},
					{Path: "source_watermark", Type: stringType()},
				},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: nodeSource(extStart, "worker_id")},
					{Target: "expected_state", Source: constantSource("READY")},
				},
				Capability: &workflow.CapabilityRef{
					ID: extCapWatch, Version: 1,
					OperationMode:   workflow.ModeExecute,
					AuthorityScopes: []string{"scope:observation.read"},
				},
				Observe: &workflow.ObserveSpec{
					EvidenceKind:         workflow.EvidenceAuthoritativeRead,
					SourceAuthority:      "fixture.unit",
					ExpectedStateFields:  []string{"expected_state"},
					RequiredWatermarks:   []string{"fixture.unit.cursor"},
					MaxAgeSeconds:        600,
					ComparisonProfile:    "comparison.fixture.unit/v1",
					RetryExhaustionRoute: extEndRep,
				},
				Governance:     governedNode(workflow.RevalidatePreExecution),
				OutcomeAliases: map[string]string{"CONSISTENT": "PASS", "DEGRADED": "PARTIAL"},
			},
			{
				ID:             extCommit,
				Type:           workflow.StepCapability,
				DeclaredEffect: capability.EffectInternalMutation,
				EffectRole:     workflow.RoleAuthoritativeCore,
				InputSchema:    capSchema(extCapWrite, "request"),
				OutputSchema:   capSchema(extCapWrite, "response"),
				Inputs:         []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				Outputs:        []workflow.Field{{Path: "commit_receipt", Type: stringType()}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: nodeSource(extStart, "worker_id")},
				},
				Capability: &workflow.CapabilityRef{
					ID: extCapWrite, Version: 1,
					OperationMode:         workflow.ModeExecute,
					AuthorityScopes:       []string{"scope:people.write"},
					IdempotencyKeyMapping: "worker_id",
					EffectBinding:         "fixture.unit_commit",
				},
				Governance:  governedNode(workflow.RevalidatePreEffect),
				ModeOverlay: &workflow.ModeOverlay{SimulateEffect: capability.EffectReadOnly},
			},
			{
				ID:            extEndOK,
				Type:          workflow.StepEnd,
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("COMMITTED"),
				End: &workflow.EndSpec{
					TerminalCode:      "COMMITTED",
					RuntimeStatus:     workflow.RuntimeCompleted,
					CompletionMapping: fixtureCompletion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture/v1",
				},
				Governance: endGovernance,
			},
			{
				ID:            extEndDeg,
				Type:          workflow.StepEnd,
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("COMMITTED_DEGRADED"),
				End: &workflow.EndSpec{
					TerminalCode:      "COMMITTED_DEGRADED",
					RuntimeStatus:     workflow.RuntimeCompleted,
					CompletionMapping: fixtureCompletion("APPROVED", "COMMITTED", "COMPLETED", "DEGRADED", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture/v1",
					RepairRefs:        []string{"repair.fixture/v1"},
				},
				Governance: endGovernance,
			},
			{
				ID:            extEndRep,
				Type:          workflow.StepEnd,
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("REPAIR_REQUIRED"),
				End: &workflow.EndSpec{
					TerminalCode:      "REPAIR_REQUIRED",
					RuntimeStatus:     workflow.RuntimeRepairRequired,
					CompletionMapping: fixtureCompletion("APPROVED", "REPAIR_REQUIRED", "UNKNOWN", "UNKNOWN", "PENDING"),
					RepairRefs:        []string{"repair.fixture/v1"},
				},
				Governance: endGovernance,
			},
		},
		Edges: []workflow.Edge{
			{From: extStart, To: extWait, RouteKey: "SUCCEEDED"},
			{From: extStart, To: extEndRep, RouteKey: "REJECTED"},
			{From: extStart, To: extEndRep, RouteKey: "UNKNOWN"},
			{From: extStart, To: extEndRep, RouteKey: "AMBIGUOUS"},

			{From: extWait, To: extTask, RouteKey: "FIRED"},
			{From: extWait, To: extEndRep, RouteKey: "LATE"},
			{From: extWait, To: extEndRep, RouteKey: "CANCELLED"},

			{From: extTask, To: extWatch, RouteKey: "REAPPROVED"},
			{From: extTask, To: extEndRep, RouteKey: "WITHDRAWN"},
			{From: extTask, To: extEndRep, RouteKey: "REJECTED"},
			{From: extTask, To: extEndRep, RouteKey: "EXPIRED"},
			{From: extTask, To: extEndRep, RouteKey: "CANCELLED"},
			{From: extTask, To: extEndRep, RouteKey: "DROPPED"},

			{From: extWatch, To: extCommit, RouteKey: "CONSISTENT"},
			{From: extWatch, To: extEndDeg, RouteKey: "DEGRADED"},
			{From: extWatch, To: extEndDeg, RouteKey: "FAIL"},
			{From: extWatch, To: extEndDeg, RouteKey: "PARTIAL"},
			{From: extWatch, To: extEndRep, RouteKey: "UNKNOWN"},

			{From: extCommit, To: extEndOK, RouteKey: "SUCCEEDED"},
			{From: extCommit, To: extEndRep, RouteKey: "REJECTED"},
			{From: extCommit, To: extEndRep, RouteKey: "UNKNOWN"},
			{From: extCommit, To: extEndRep, RouteKey: "AMBIGUOUS"},
		},
	}
}

// compiledEdgeKeys returns the route keys leaving from on the compiled plan.
func compiledEdgeKeys(t *testing.T, plan *workflow.CompiledWorkflow, from string) []string {
	t.Helper()
	var out []string
	for _, e := range plan.Edges {
		if e.From == from {
			out = append(out, e.RouteKey)
		}
	}
	return out
}

// TestTodo_WF_EXT_003 proves planning/todos.md WF-EXT-003: a node declares
// outcome aliases and a SIMULATE mode overlay; the compiler canonicalizes the
// aliases onto the kernel route vocabulary and derives the SIMULATE
// projection from the declared effect classes instead of a node-id list.
func TestTodo_WF_EXT_003(t *testing.T) {
	t.Run("GREEN_aliases_canonicalized", func(t *testing.T) {
		plan, err := workflow.Compile(ext003Definition(), ext003Options(t))
		if err != nil {
			t.Fatalf("aliased definition must compile: %v", err)
		}
		if got := compiledEdgeKeys(t, plan, extWait); !reflect.DeepEqual(got, []string{"CANCELLED", "LATE", "SUCCEEDED"}) {
			t.Fatalf("wait routes = %v, want the kernel WAIT vocabulary", got)
		}
		if got := compiledEdgeKeys(t, plan, extTask); !reflect.DeepEqual(got, []string{"CANCELLED", "EXPIRED", "REJECTED", "SUCCEEDED"}) {
			t.Fatalf("task routes = %v, want SUCCEEDED/CANCELLED with the dropped outcome gone", got)
		}
		if got := compiledEdgeKeys(t, plan, extWatch); !reflect.DeepEqual(got, []string{"FAIL", "PARTIAL", "PASS", "UNKNOWN"}) {
			t.Fatalf("observe routes = %v, want the kernel OBSERVE vocabulary", got)
		}
		for _, e := range plan.Edges {
			switch e.RouteKey {
			case "FIRED", "REAPPROVED", "WITHDRAWN", "DROPPED", "CONSISTENT", "DEGRADED":
				t.Fatalf("business outcome %q leaks into the compiled plan (%s->%s)", e.RouteKey, e.From, e.To)
			}
		}
		commit, ok := plan.Node(extCommit)
		if !ok || commit.EffectClass != capability.EffectInternalMutation || commit.EffectRole != workflow.RoleAuthoritativeCore {
			t.Fatalf("execute commit = %+v, want the governed core", commit)
		}
		if plan.Effects.ZeroEffect {
			t.Fatal("EXECUTE plan must carry the core commit effect")
		}
	})

	t.Run("GREEN_simulate_projection_from_declared_effects", func(t *testing.T) {
		def := ext003Definition()
		exec, err := workflow.Compile(def, ext003Options(t))
		if err != nil {
			t.Fatalf("EXECUTE: %v", err)
		}
		sim, err := workflow.Compile(def, ext003SimulateOptions(t))
		if err != nil {
			t.Fatalf("SIMULATE: %v", err)
		}
		if !sim.Effects.ZeroEffect || len(sim.Effects.EffectKeys) != 0 {
			t.Fatalf("simulate effects = %+v, want zero effects", sim.Effects)
		}
		if sim.Digest() == exec.Digest() {
			t.Fatal("SIMULATE projection must be a distinct plan from EXECUTE")
		}
		commit, ok := sim.Node(extCommit)
		if !ok {
			t.Fatalf("simulate plan carries no commit node")
		}
		if commit.EffectClass != capability.EffectReadOnly || commit.EffectRole != "" {
			t.Fatalf("simulate commit = %s/%q, want a role-free read", commit.EffectClass, commit.EffectRole)
		}
		if commit.Capability == nil || commit.Capability.OperationMode != workflow.ModeSimulate || commit.Capability.EffectClass != capability.EffectReadOnly {
			t.Fatalf("simulate commit capability = %+v, want a SIMULATE read binding", commit.Capability)
		}
		for _, id := range []string{extStart, extWait, extTask, extWatch} {
			node, ok := sim.Node(id)
			if !ok {
				t.Fatalf("simulate plan carries no node %s", id)
			}
			if node.EffectClass.IsWrite() {
				t.Fatalf("simulate node %s carries write effect %s", id, node.EffectClass)
			}
		}
		// Reads keep their EXECUTE operation mode: only suppressed writes
		// project onto SIMULATE.
		start, _ := sim.Node(extStart)
		if start.Capability == nil || start.Capability.OperationMode != workflow.ModeExecute {
			t.Fatalf("simulate read capability = %+v, want the EXECUTE binding untouched", start.Capability)
		}
	})

	t.Run("RED_write_without_overlay_has_no_projection", func(t *testing.T) {
		def := ext003Definition()
		nodeRef(t, &def, extCommit).ModeOverlay = nil
		d := mustReject(t, def, ext003SimulateOptions(t), workflow.CodeMutationInSimulation)
		if !d.HasAt(workflow.CodeMutationInSimulation, extCommit) {
			t.Fatalf("diagnostic must name the unsuppressible write, got %v", d.Errors)
		}
		mustReject(t, def, ext003Options(t), workflow.CodeMutationInSimulation)
	})

	t.Run("RED_overlay_on_a_read_is_refused", func(t *testing.T) {
		def := ext003Definition()
		nodeRef(t, &def, extStart).ModeOverlay = &workflow.ModeOverlay{SimulateEffect: capability.EffectReadOnly}
		d := mustReject(t, def, ext003Options(t), workflow.CodeInvalidDefinition)
		if !d.HasAt(workflow.CodeInvalidDefinition, extStart) {
			t.Fatalf("diagnostic must name the overlaid read, got %v", d.Errors)
		}
	})

	t.Run("RED_alias_to_an_unknown_outcome_is_refused", func(t *testing.T) {
		def := ext003Definition()
		nodeRef(t, &def, extWait).OutcomeAliases["FIRED"] = "BOGUS"
		d := mustReject(t, def, ext003Options(t), workflow.CodeInvalidDefinition)
		if !d.HasAt(workflow.CodeInvalidDefinition, extWait) {
			t.Fatalf("diagnostic must name the misaliased node, got %v", d.Errors)
		}
	})

	t.Run("RED_alias_shadowing_a_kernel_outcome_is_refused", func(t *testing.T) {
		def := ext003Definition()
		nodeRef(t, &def, extTask).OutcomeAliases["SUCCEEDED"] = "CANCELLED"
		d := mustReject(t, def, ext003Options(t), workflow.CodeInvalidDefinition)
		if !d.HasAt(workflow.CodeInvalidDefinition, extTask) {
			t.Fatalf("diagnostic must name the shadowing node, got %v", d.Errors)
		}
	})

	t.Run("GREEN_compilation_leaves_the_definition_alone", func(t *testing.T) {
		def := ext003Definition()
		before := ext003Definition()
		if _, err := workflow.Compile(def, ext003SimulateOptions(t)); err != nil {
			t.Fatalf("SIMULATE: %v", err)
		}
		if !reflect.DeepEqual(def, before) {
			t.Fatal("SIMULATE projection rewrote the caller's definition")
		}
	})
}
