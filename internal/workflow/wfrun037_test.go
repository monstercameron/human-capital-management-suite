package workflow_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Fixture capabilities for the effect-role chain: one per effect class, so a
// generated node's class is chosen by which capability it binds.
const (
	roleCapRead     = "fixture.roles.read"
	roleCapInternal = "fixture.roles.internal_write"
	roleCapExternal = "fixture.roles.external_write"
	roleCapObserve  = "fixture.roles.observe"

	roleObserve   = "observe_effects"
	roleEndCommit = "end_committed"
	roleEndDegrad = "end_degraded"
	roleEndRepair = "end_repair"
)

var roleCapabilityByClass = map[capability.EffectClass]string{
	capability.EffectReadOnly:         roleCapRead,
	capability.EffectInternalMutation: roleCapInternal,
	capability.EffectExternalMutation: roleCapExternal,
}

func roleRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	for _, d := range []capability.Definition{
		fixtureCapability(roleCapRead, "people", capability.EffectReadOnly),
		fixtureCapability(roleCapInternal, "people", capability.EffectInternalMutation),
		fixtureCapability(roleCapExternal, "payroll", capability.EffectExternalMutation),
		fixtureCapability(roleCapObserve, "payroll", capability.EffectReadOnly),
	} {
		if err := r.Register(d, echoHandler); err != nil {
			t.Fatalf("publish %s: %v", d.Key(), err)
		}
	}
	return r
}

func roleOptions(t *testing.T) workflow.Options {
	t.Helper()
	return workflow.Options{Phase: workflow.PhaseP1B, Capabilities: roleRegistry(t)}
}

// roleStep is one generated capability node of the chain.
type roleStep struct {
	class capability.EffectClass
	role  workflow.EffectRole
}

func roleEnd(id, code string, status workflow.RuntimeStatus, dims map[string]string, repair bool) workflow.Node {
	end := &workflow.EndSpec{TerminalCode: code, RuntimeStatus: status, CompletionMapping: dims}
	if status == workflow.RuntimeCompleted {
		end.CommitReceiptRef = "receipt.fixture.roles/v1"
	}
	if repair {
		end.RepairRefs = []string{"repair.fixture.roles/v1"}
	}
	return workflow.Node{
		ID: id, Type: workflow.StepEnd, Inputs: fixtureTerminalInputs(), InputMappings: fixtureTerminalMappings(code), End: end,
		Governance: workflow.NodeGovernance{Purpose: "FIXTURE_EXECUTE", Classification: "CONFIDENTIAL_HR", RevalidationBoundary: workflow.RevalidatePreClosure, DataAccessManifestRef: "dam.fixture/v1"},
	}
}

// roleChainDefinition builds a linear chain step_0 -> ... -> step_n-1 ->
// observe -> END. Every step can fail to end_repair; every write declares the
// failure route so only the role rules decide whether it compiles.
func roleChainDefinition(steps []roleStep) workflow.Definition {
	def := workflow.Definition{
		WorkflowID: "fixture.workflows.effect_roles", Version: 1, Name: "Effect role chain fixture",
		InputSchema: fixtureSchemaRef("RoleInput"), OutputSchema: fixtureSchemaRef("RoleResult"), VariablesSchema: fixtureSchemaRef("RoleVariables"),
		TenantScope: "acme", OrganizationScope: "acme/engineering", RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID:      "step_0",
		Inputs:           []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
		Outputs:          []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "terminal_code", Type: stringType()}},
		Limits:           workflow.Limits{MaxFanOut: 4, MaxDepth: 32, MaxNodes: 32},
		FailurePolicyRef: "policy.workflow.failure.execute/v1", CancellationPolicyRef: "policy.workflow.cancellation.execute/v1",
		MigrationPolicyRef: "policy.workflow.migration.pinned/v1", RetentionPolicyRef: "policy.workflow.retention.payroll/v1",
	}
	for i, s := range steps {
		id := fmt.Sprintf("step_%d", i)
		capID := roleCapabilityByClass[s.class]
		ref := &workflow.CapabilityRef{ID: capID, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:people.read"}}
		boundary := workflow.RevalidatePreExecution
		node := workflow.Node{
			ID: id, Type: workflow.StepCapability,
			InputSchema: capSchema(capID, "request"), OutputSchema: capSchema(capID, "response"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
			Outputs:       []workflow.Field{{Path: "record_id", Type: stringType()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: inputSource("worker_id")}},
			Capability:    ref, EffectRole: s.role,
		}
		if s.class.IsWrite() {
			scope := "scope:people.write"
			if s.class == capability.EffectExternalMutation {
				scope = "scope:payroll.write"
			}
			ref.AuthorityScopes = []string{scope}
			ref.EffectBinding = "roles." + id
			node.FailureRoute = roleEndRepair
			boundary = workflow.RevalidatePreEffect
		}
		node.Governance = governedNode(boundary)
		def.Nodes = append(def.Nodes, node)
		next := roleObserve
		if i+1 < len(steps) {
			next = fmt.Sprintf("step_%d", i+1)
		}
		def.Edges = append(def.Edges,
			workflow.Edge{From: id, To: next, RouteKey: "SUCCEEDED"},
			workflow.Edge{From: id, To: roleEndRepair, RouteKey: "REJECTED"},
			workflow.Edge{From: id, To: roleEndRepair, RouteKey: "UNKNOWN"},
			workflow.Edge{From: id, To: roleEndRepair, RouteKey: "AMBIGUOUS"},
		)
	}
	def.Nodes = append(def.Nodes,
		workflow.Node{
			ID: roleObserve, Type: workflow.StepObserve,
			InputSchema: capSchema(roleCapObserve, "request"), OutputSchema: capSchema(roleCapObserve, "response"),
			Inputs:        []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}, {Path: "expected_status", Type: stringType()}},
			Outputs:       []workflow.Field{{Path: "observed_status", Type: stringType()}, {Path: "source_watermark", Type: stringType()}},
			InputMappings: []workflow.Mapping{{Target: "worker_id", Source: inputSource("worker_id")}, {Target: "expected_status", Source: constantSource("SYNCED")}},
			Capability:    &workflow.CapabilityRef{ID: roleCapObserve, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:payroll.read"}},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "payroll.provider.acme",
				ExpectedStateFields: []string{"expected_status"}, RequiredWatermarks: []string{"payroll.provider.acme.cursor"},
				MaxAgeSeconds: 600, ComparisonProfile: "comparison.payroll.worker_status/v1",
			},
			Governance: governedNode(workflow.RevalidatePreExecution),
		},
		roleEnd(roleEndCommit, "COMMITTED", workflow.RuntimeCompleted, fixtureCompletion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"), false),
		roleEnd(roleEndDegrad, "COMMITTED_DEGRADED", workflow.RuntimeCompleted, fixtureCompletion("APPROVED", "COMMITTED", "COMPLETED", "DEGRADED", "SATISFIED"), true),
		roleEnd(roleEndRepair, "REPAIR_REQUIRED", workflow.RuntimeRepairRequired, fixtureCompletion("APPROVED", "REPAIR_REQUIRED", "UNKNOWN", "UNKNOWN", "PENDING"), true),
	)
	def.Edges = append(def.Edges,
		workflow.Edge{From: roleObserve, To: roleEndCommit, RouteKey: "PASS"},
		workflow.Edge{From: roleObserve, To: roleEndDegrad, RouteKey: "FAIL"},
		workflow.Edge{From: roleObserve, To: roleEndDegrad, RouteKey: "PARTIAL"},
		workflow.Edge{From: roleObserve, To: roleEndRepair, RouteKey: "UNKNOWN"},
	)
	return def
}

// roleOracle restates the WF-RUN-037 rules independently of the compiler, for
// a linear chain where step i reaches step j exactly when i < j.
func roleOracle(steps []roleStep) (valid bool, codes []string) {
	set := map[string]bool{}
	for i, s := range steps {
		switch {
		case !s.class.IsWrite() && s.role != "":
			set[workflow.CodeEffectRoleConflict] = true
			continue
		case !s.class.IsWrite():
			continue
		case s.role == "":
			set[workflow.CodeEffectRoleMissing] = true
			continue
		case s.class == capability.EffectExternalMutation && s.role != workflow.RoleDownstreamEffect:
			set[workflow.CodeEffectRoleConflict] = true
			continue
		case s.role == workflow.RoleAuthoritativeCore:
			continue
		}
		coreBefore, coreAfter := false, false
		for j, o := range steps {
			if o.role != workflow.RoleAuthoritativeCore || o.class != capability.EffectInternalMutation {
				continue
			}
			if j < i {
				coreBefore = true
			}
			if j > i {
				coreAfter = true
			}
		}
		if coreAfter {
			set[workflow.CodeEffectRoleOrder] = true
		}
		if (s.role == workflow.RoleDerivedUpdate || s.class == capability.EffectInternalMutation) && !coreBefore {
			set[workflow.CodeEffectRoleOrder] = true
		}
	}
	for c := range set {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return len(codes) == 0, codes
}

func compileRoles(t *testing.T, steps []roleStep) (*workflow.CompiledWorkflow, error) {
	t.Helper()
	return workflow.Compile(roleChainDefinition(steps), roleOptions(t))
}

func diagnosticsOf(t *testing.T, err error) *workflow.Diagnostics {
	t.Helper()
	d, ok := err.(*workflow.Diagnostics)
	if !ok {
		t.Fatalf("error %T %v is not *workflow.Diagnostics", err, err)
	}
	return d
}

// TestTodo_WF_RUN_037 proves the compiler requires the classification of every
// write effect, refuses a role its effect class does not admit, a role on a
// read, a dependent node with no failure route and a dependent node that gates
// or does not follow a core, and pins the accepted classification in the plan
// digest.
func TestTodo_WF_RUN_037(t *testing.T) {
	core := roleStep{capability.EffectInternalMutation, workflow.RoleAuthoritativeCore}
	outbox := roleStep{capability.EffectInternalMutation, workflow.RoleDownstreamEffect}
	payroll := roleStep{capability.EffectExternalMutation, workflow.RoleDownstreamEffect}
	projection := roleStep{capability.EffectInternalMutation, workflow.RoleDerivedUpdate}
	read := roleStep{capability.EffectReadOnly, ""}

	plan, err := compileRoles(t, []roleStep{read, core, outbox, payroll, projection})
	if err != nil {
		t.Fatalf("classified core -> outbox -> payroll -> projection must compile: %v", err)
	}
	for id, want := range map[string]workflow.EffectRole{"step_0": "", "step_1": workflow.RoleAuthoritativeCore, "step_2": workflow.RoleDownstreamEffect, "step_3": workflow.RoleDownstreamEffect, "step_4": workflow.RoleDerivedUpdate, roleObserve: ""} {
		if n, ok := plan.Node(id); !ok || n.EffectRole != want {
			t.Fatalf("compiled %s role = %q, want %q", id, n.EffectRole, want)
		}
	}
	wantByRole := map[string][]string{"AUTHORITATIVE_CORE": {"step_1"}, "DOWNSTREAM_EFFECT": {"step_2", "step_3"}, "DERIVED_UPDATE": {"step_4"}}
	if !reflect.DeepEqual(plan.Effects.NodesByRole, wantByRole) {
		t.Fatalf("NodesByRole = %v, want %v", plan.Effects.NodesByRole, wantByRole)
	}
	if got := plan.NodesWithRole(workflow.RoleDownstreamEffect); !reflect.DeepEqual(got, []string{"step_2", "step_3"}) {
		t.Fatalf("NodesWithRole(DOWNSTREAM_EFFECT) = %v", got)
	}
	if _, err := compileRoles(t, []roleStep{payroll}); err != nil {
		t.Fatalf("a standalone external downstream effect needs no core: %v", err)
	}

	// The digest pins the classification: an edited role stops verifying.
	for i := range plan.Nodes {
		if plan.Nodes[i].ID == "step_2" {
			plan.Nodes[i].EffectRole = workflow.RoleDerivedUpdate
		}
	}
	if plan.Verify() == nil {
		t.Fatal("a plan whose effect role was edited after compilation still verifies")
	}
	fresh, err := compileRoles(t, []roleStep{read, core, outbox, payroll, projection})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(fresh)
	if !strings.Contains(string(raw), `"effect_role":"DOWNSTREAM_EFFECT"`) {
		t.Fatal("the canonical plan bytes do not carry the effect role")
	}

	for _, tc := range []struct {
		name  string
		steps []roleStep
		code  string
		node  string
	}{
		{"missing classification", []roleStep{{capability.EffectInternalMutation, ""}}, workflow.CodeEffectRoleMissing, "step_0"},
		{"external effect claimed as core", []roleStep{{capability.EffectExternalMutation, workflow.RoleAuthoritativeCore}}, workflow.CodeEffectRoleConflict, "step_0"},
		{"external effect claimed as derived", []roleStep{core, {capability.EffectExternalMutation, workflow.RoleDerivedUpdate}}, workflow.CodeEffectRoleConflict, "step_1"},
		{"read carries a role", []roleStep{{capability.EffectReadOnly, workflow.RoleDerivedUpdate}}, workflow.CodeEffectRoleConflict, "step_0"},
		{"unknown role", []roleStep{{capability.EffectInternalMutation, "CACHE_WARMING"}}, workflow.CodeEffectRoleConflict, "step_0"},
		{"downstream leg gates the core", []roleStep{payroll, core}, workflow.CodeEffectRoleOrder, "step_0"},
		{"derived update gates the core", []roleStep{core, projection, core}, workflow.CodeEffectRoleOrder, "step_1"},
		{"derived update with no core", []roleStep{projection}, workflow.CodeEffectRoleOrder, "step_0"},
		{"outbox leg with no core", []roleStep{outbox}, workflow.CodeEffectRoleOrder, "step_0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileRoles(t, tc.steps)
			if err == nil {
				t.Fatalf("%v compiled; want %s", tc.steps, tc.code)
			}
			if d := diagnosticsOf(t, err); !d.HasAt(tc.code, tc.node) {
				t.Fatalf("diagnostics %v lack %s at %s", d.Codes(), tc.code, tc.node)
			}
		})
	}

	// A dependent node with no failure route is refused: its failure must
	// have somewhere to go other than back into the core.
	def := roleChainDefinition([]roleStep{core, outbox})
	nodeRef(t, &def, "step_1").FailureRoute = ""
	if _, err := workflow.Compile(def, roleOptions(t)); err == nil || !diagnosticsOf(t, err).HasAt(workflow.CodeEffectRoleUnrouted, "step_1") {
		t.Fatalf("unrouted downstream leg = %v, want %s", err, workflow.CodeEffectRoleUnrouted)
	}
	// The manifest, not the author, decides which roles are admitted: the
	// payroll fixture's external sync cannot be relabelled as the core.
	payrollDef := effectsDefinition()
	nodeRef(t, &payrollDef, fxSync).EffectRole = workflow.RoleAuthoritativeCore
	if _, err := workflow.Compile(payrollDef, effectsOptions(t)); err == nil || !diagnosticsOf(t, err).HasAt(workflow.CodeEffectRoleConflict, fxSync) {
		t.Fatalf("external sync relabelled as core = %v, want %s", err, workflow.CodeEffectRoleConflict)
	}
}

// TestTodo_WF_RUN_037_Property compiles generated chains of reads, internal
// and external writes with random roles and proves the compiler accepts
// exactly the chains the independently restated rules accept, reports only
// effect-role diagnostics for the rest, and carries every accepted role into
// the compiled plan.
func TestTodo_WF_RUN_037_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(37))
	classes := []capability.EffectClass{capability.EffectReadOnly, capability.EffectInternalMutation, capability.EffectExternalMutation}
	roles := []workflow.EffectRole{"", workflow.RoleAuthoritativeCore, workflow.RoleDownstreamEffect, workflow.RoleDerivedUpdate}
	options := roleOptions(t)
	roleCodes := map[string]bool{workflow.CodeEffectRoleMissing: true, workflow.CodeEffectRoleConflict: true, workflow.CodeEffectRoleOrder: true}
	accepted, refused := 0, 0
	for iter := 0; iter < 400; iter++ {
		steps := make([]roleStep, 1+rng.Intn(5))
		for i := range steps {
			steps[i] = roleStep{classes[rng.Intn(len(classes))], roles[rng.Intn(len(roles))]}
		}
		valid, wantCodes := roleOracle(steps)
		plan, err := workflow.Compile(roleChainDefinition(steps), options)
		if valid {
			if err != nil {
				t.Fatalf("iter %d %v: oracle accepts, compiler refused: %v", iter, steps, err)
			}
			accepted++
			for i, s := range steps {
				n, _ := plan.Node(fmt.Sprintf("step_%d", i))
				if n.EffectRole != s.role || n.EffectClass != s.class {
					t.Fatalf("iter %d step %d compiled %s/%q, want %s/%q", iter, i, n.EffectClass, n.EffectRole, s.class, s.role)
				}
			}
			again, err := workflow.Compile(roleChainDefinition(steps), options)
			if err != nil || again.Digest() != plan.Digest() {
				t.Fatalf("iter %d: recompilation is not deterministic (%v)", iter, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("iter %d %v: oracle refuses %v, compiler accepted", iter, steps, wantCodes)
		}
		refused++
		got := diagnosticsOf(t, err).Codes()
		for _, c := range got {
			if !roleCodes[c] {
				t.Fatalf("iter %d %v: non-role diagnostic %s in %v; the generator must isolate the role rules", iter, steps, c, got)
			}
		}
		if !reflect.DeepEqual(got, wantCodes) {
			t.Fatalf("iter %d %v: codes %v, oracle %v", iter, steps, got, wantCodes)
		}
	}
	if accepted < 20 || refused < 20 {
		t.Fatalf("generator explored %d accepted and %d refused chains; widen it", accepted, refused)
	}
}
