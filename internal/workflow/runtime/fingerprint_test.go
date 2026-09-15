package runtime_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// fullyBoundPlan is a hand-built plan whose nodes carry every compiled fact a
// fingerprint reads, so the derivation is proven field by field rather than
// only against whatever the promotion reference happens to bind.
func fullyBoundPlan() *workflow.CompiledWorkflow {
	schema := func(id string, v uint32) workflow.SchemaRef {
		return workflow.SchemaRef{SchemaID: id, Version: v, ProtobufFullName: "hcm." + id}
	}
	return &workflow.CompiledWorkflow{
		WorkflowID: "wf.fp", Version: 3, CompilerVersion: workflow.CompilerVersion,
		FailurePolicyRef: "policy:failure/2", CancellationPolicyRef: "policy:cancel/1",
		MigrationPolicyRef: "policy:migrate/1", RetentionPolicyRef: "policy:retain/7",
		Nodes: []workflow.CompiledNode{
			{
				ID: "call", Type: workflow.StepCapability,
				InputSchema: schema("call.in", 1), OutputSchema: schema("call.out", 2),
				Retry: &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy:backoff/exp"},
				Governance: workflow.CompiledGovernance{
					DataAccessManifestRef: "dam:payroll/4", OutputValidatorRef: "validator:out/1",
					ObligationRefs: []string{"obligation:notice/1"}, ApprovalRequirements: []string{"approval:manager/2"},
				},
				Capability: &workflow.CompiledCapability{
					ID: "payroll.connector.post", Version: 5, Digest: "sha256:manifest",
					RequestSchema: schema("post.req", 1), ResponseSchema: schema("post.resp", 1),
					AuthZScopeRef: "authz:payroll.write/3", IdempotencyPolicyRef: "idem:post/1"},
			},
			{ID: "decide", Type: workflow.StepDecision, Decision: &workflow.CompiledDecision{EvaluatorRef: "rules.band", EvaluatorVersion: 9, RuleRef: "rule:band/2026.2"}},
			{ID: "map", Type: workflow.StepTransform, Transform: &workflow.CompiledTransform{TransformRef: "map.worker_to_payroll", Version: 4}},
			{ID: "sleep", Type: workflow.StepWait, Wait: &workflow.CompiledWait{CalendarRef: "cal:us-holidays", CalendarVersion: "2026.1", ZoneTzdbVersion: "2026a"}},
			{ID: "listen", Type: workflow.StepSignal, Signal: &workflow.CompiledSignal{ExpectedSchemaRef: schema("evt.hired", 3)}},
			{ID: "bare", Type: workflow.StepEnd},
		},
	}
}

func TestDeriveNodeFingerprintReadsEveryCompiledFact(t *testing.T) {
	plan := fullyBoundPlan()
	want := map[string][]runtime.FingerprintComponent{
		"call": {
			{Kind: runtime.FingerprintCapability, Ref: "payroll.connector.post@v5"},
			{Kind: runtime.FingerprintCapabilityManifest, Ref: "sha256:manifest"},
			{Kind: runtime.FingerprintSchema, Ref: "call.in@v1"}, {Kind: runtime.FingerprintSchema, Ref: "call.out@v2"},
			{Kind: runtime.FingerprintSchema, Ref: "post.req@v1"}, {Kind: runtime.FingerprintSchema, Ref: "post.resp@v1"},
			{Kind: runtime.FingerprintPolicy, Ref: "authz:payroll.write/3"}, {Kind: runtime.FingerprintPolicy, Ref: "idem:post/1"},
			{Kind: runtime.FingerprintPolicy, Ref: "policy:backoff/exp"}, {Kind: runtime.FingerprintPolicy, Ref: "dam:payroll/4"},
			{Kind: runtime.FingerprintPolicy, Ref: "validator:out/1"}, {Kind: runtime.FingerprintPolicy, Ref: "obligation:notice/1"},
			{Kind: runtime.FingerprintPolicy, Ref: "approval:manager/2"},
		},
		"decide": {{Kind: runtime.FingerprintEvaluator, Ref: "rules.band@v9"}, {Kind: runtime.FingerprintRule, Ref: "rule:band/2026.2"}},
		"map":    {{Kind: runtime.FingerprintMapping, Ref: "map.worker_to_payroll@v4"}},
		"sleep":  {{Kind: runtime.FingerprintReferenceData, Ref: "cal:us-holidays@2026.1"}, {Kind: runtime.FingerprintReferenceData, Ref: "tzdb@2026a"}},
		"listen": {{Kind: runtime.FingerprintSchema, Ref: "evt.hired@v3"}},
		"bare":   nil,
	}
	planWide := []runtime.FingerprintComponent{
		{Kind: runtime.FingerprintWorkflow, Ref: "wf.fp@v3"},
		{Kind: runtime.FingerprintCompiler, Ref: workflow.CompilerVersion},
		{Kind: runtime.FingerprintRuntime, Ref: "runtime/test"},
		{Kind: runtime.FingerprintPolicy, Ref: "policy:failure/2"}, {Kind: runtime.FingerprintPolicy, Ref: "policy:cancel/1"},
		{Kind: runtime.FingerprintPolicy, Ref: "policy:migrate/1"}, {Kind: runtime.FingerprintPolicy, Ref: "policy:retain/7"},
	}
	for nodeID, own := range want {
		f, err := runtime.DeriveNodeFingerprint(plan, nodeID, "runtime/test")
		if err != nil {
			t.Fatalf("derive %s: %v", nodeID, err)
		}
		expected := append(append([]runtime.FingerprintComponent(nil), planWide...), own...)
		if len(f.Components) != len(expected) {
			t.Errorf("%s components = %v, want exactly %v", nodeID, f.Components, expected)
		}
		for _, c := range expected {
			if !f.Touches(c.Kind, c.Ref) {
				t.Errorf("%s fingerprint misses %s=%s", nodeID, c.Kind, c.Ref)
			}
		}
		for i := 1; i < len(f.Components); i++ {
			prev, cur := f.Components[i-1], f.Components[i]
			if string(prev.Kind)+"="+prev.Ref >= string(cur.Kind)+"="+cur.Ref {
				t.Errorf("%s components are not in strict canonical order at %d: %v", nodeID, i, f.Components)
			}
		}
		if f.NodeID != nodeID || f.WorkflowID != "wf.fp" || f.WorkflowVersion != 3 || f.RuntimeVersion != "runtime/test" {
			t.Errorf("%s scalar fields = %+v", nodeID, f)
		}
		again, _ := runtime.DeriveNodeFingerprint(plan, nodeID, "runtime/test")
		if again.Digest() != f.Digest() || !strings.HasPrefix(f.Digest(), "sha256:") {
			t.Errorf("%s digest is not stable: %s vs %s", nodeID, f.Digest(), again.Digest())
		}
		other, _ := runtime.DeriveNodeFingerprint(plan, nodeID, "runtime/other")
		if other.Digest() == f.Digest() {
			t.Errorf("%s digest ignores the runtime version", nodeID)
		}
	}

	if _, err := runtime.DeriveNodeFingerprint(nil, "call", "rt"); runtimeCode(err) != runtime.CodeInvalidRecord {
		t.Errorf("nil plan = %v, want %s", err, runtime.CodeInvalidRecord)
	}
	if _, err := runtime.DeriveNodeFingerprint(plan, "missing", "rt"); runtimeCode(err) != runtime.CodeInvalidRecord {
		t.Errorf("undeclared node = %v, want %s", err, runtime.CodeInvalidRecord)
	}
}

func TestNodeFingerprintValidateRefusesMissingVersions(t *testing.T) {
	valid := runtime.NodeFingerprint{
		NodeID: "n", StepType: workflow.StepEnd, WorkflowID: "wf", CompiledPlanDigest: "sha256:p",
		CompilerVersion: "c", RuntimeVersion: "r",
		Components: []runtime.FingerprintComponent{{Kind: runtime.FingerprintPlan, Ref: "sha256:p"}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid fingerprint refused: %v", err)
	}
	for name, mutate := range map[string]func(*runtime.NodeFingerprint){
		"node":       func(f *runtime.NodeFingerprint) { f.NodeID = "" },
		"step type":  func(f *runtime.NodeFingerprint) { f.StepType = "" },
		"workflow":   func(f *runtime.NodeFingerprint) { f.WorkflowID = "" },
		"plan":       func(f *runtime.NodeFingerprint) { f.CompiledPlanDigest = "" },
		"compiler":   func(f *runtime.NodeFingerprint) { f.CompilerVersion = "" },
		"runtime":    func(f *runtime.NodeFingerprint) { f.RuntimeVersion = "" },
		"components": func(f *runtime.NodeFingerprint) { f.Components = nil },
	} {
		f := valid
		mutate(&f)
		if code := runtimeCode(f.Validate()); code != runtime.CodeInvalidRecord {
			t.Errorf("missing %s: code %q, want %s", name, code, runtime.CodeInvalidRecord)
		}
	}
	if !runtime.FingerprintMapping.Valid() || runtime.FingerprintKind("MODEL_GUESS").Valid() {
		t.Error("FingerprintKind.Valid does not match the declared vocabulary")
	}
}

// TestTodo_WF_RUN_038_Golden pins the canonical fingerprint of every node of
// the promotion reference plan. The compiled-plan digest is rendered as a
// placeholder (PRIMARY asserts it equals the pin) so a compiler change that
// only moves the digest does not masquerade as a fingerprint change.
func TestTodo_WF_RUN_038_Golden(t *testing.T) {
	plan := referencePlan(t)
	var b strings.Builder
	for _, node := range plan.Nodes {
		f, err := runtime.DeriveNodeFingerprint(plan, node.ID, runtime.RuntimeVersion)
		if err != nil {
			t.Fatalf("derive %s: %v", node.ID, err)
		}
		if err := f.Validate(); err != nil {
			t.Fatalf("reference fingerprint for %s is invalid: %v", node.ID, err)
		}
		fmt.Fprintf(&b, "node %s (%s)\n", f.NodeID, f.StepType)
		for _, c := range f.Components {
			ref := c.Ref
			if c.Kind == runtime.FingerprintPlan {
				ref = "<compiled plan digest>"
			}
			fmt.Fprintf(&b, "  %s=%s\n", c.Kind, ref)
		}
	}
	got := b.String()
	path := filepath.Join("testdata", "wfrun038_fingerprint.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (set HCMNEXT_UPDATE_GOLDEN=1 to create): %v", err)
	}
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("fingerprint golden drifted:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestTodo_WF_RUN_038 is the PRIMARY. RED: only a start digest exists; the
// versions a node execution used are not recorded or queryable. GREEN: every
// node execution Start and Advance insert records the fingerprint derived
// from the pinned plan and pinned runtime version, the stored row re-verifies
// its digest, and the tenant-scoped index answers which instances a version
// touched.
func TestTodo_WF_RUN_038(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun038")
	pf := newPromotionFixture(t, values.TenantId("wfrun038-tenant"), "intent:wf-run-038")
	started := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "wfrun038"))
	runPromotionWalk(t, conn, tenantID, pf.Plan, started.InstanceID, started.InstanceVersion, runtime.NewMemorySink(), promotionExceedsThresholdWalk)

	var executions []runtime.NodeExecution
	loaded := map[string]runtime.NodeFingerprint{}
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		if executions, err = (runtime.Store{}).LoadNodeExecutions(ctx, tx, tenantID, started.InstanceID); err != nil {
			return err
		}
		for _, ne := range executions {
			f, found, err := runtime.LoadNodeFingerprint(ctx, tx, tenantID, started.InstanceID, ne.NodeID, ne.Attempt)
			if err != nil {
				return err
			}
			if !found {
				t.Errorf("node execution %s attempt %d recorded no fingerprint", ne.NodeID, ne.Attempt)
				continue
			}
			loaded[ne.NodeID] = f
		}
		return nil
	})
	if len(executions) < len(promotionExceedsThresholdWalk) {
		t.Fatalf("walk recorded %d node executions, want at least %d", len(executions), len(promotionExceedsThresholdWalk))
	}
	for nodeID, f := range loaded {
		derived, err := runtime.DeriveNodeFingerprint(pf.Plan, nodeID, started.ExecutionContext.RuntimeVersion)
		if err != nil {
			t.Fatal(err)
		}
		if f.Digest() != derived.Digest() {
			t.Errorf("%s stored fingerprint %+v differs from the one derived from the pinned plan %+v", nodeID, f, derived)
		}
		if f.CompiledPlanDigest != started.CompiledPlanDigest || !f.Touches(runtime.FingerprintPlan, pf.Plan.Digest()) ||
			!f.Touches(runtime.FingerprintRuntime, runtime.RuntimeVersion) {
			t.Errorf("%s fingerprint does not carry the pinned plan and runtime versions: %+v", nodeID, f)
		}
	}

	query := func(kind runtime.FingerprintKind, ref string) ([]uuid.UUID, error) {
		var out []uuid.UUID
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			var err error
			out, err = runtime.InstancesTouching(ctx, tx, tenantID, kind, ref)
			return err
		})
		return out, err
	}
	for _, probe := range []runtime.FingerprintComponent{
		{Kind: runtime.FingerprintPlan, Ref: pf.Plan.Digest()},
		{Kind: runtime.FingerprintRuntime, Ref: runtime.RuntimeVersion},
		{Kind: runtime.FingerprintWorkflow, Ref: fmt.Sprintf("%s@v%d", pf.Plan.WorkflowID, pf.Plan.Version)},
	} {
		got, err := query(probe.Kind, probe.Ref)
		if err != nil || len(got) != 1 || got[0] != started.InstanceID {
			t.Errorf("InstancesTouching(%s=%s) = %v, %v; want [%s]", probe.Kind, probe.Ref, got, err, started.InstanceID)
		}
	}
	if got, err := query(runtime.FingerprintRuntime, "hcmnext.workflow.runtime/v0"); err != nil || len(got) != 0 {
		t.Errorf("an unused version touched %v (%v)", got, err)
	}
	for name, probe := range map[string]runtime.FingerprintComponent{
		"undeclared kind": {Kind: "MODEL_GUESS", Ref: "x"},
		"empty ref":       {Kind: runtime.FingerprintRuntime, Ref: " "},
	} {
		if _, err := query(probe.Kind, probe.Ref); runtimeCode(err) != runtime.CodeInvalidRecord {
			t.Errorf("%s = %v, want %s", name, err, runtime.CodeInvalidRecord)
		}
	}
	if _, err := runtime.InstancesTouching(ctx, conn, uuid.Nil, runtime.FingerprintRuntime, "x"); runtimeCode(err) != runtime.CodeInvalidRecord {
		t.Errorf("nil tenant = %v, want %s", err, runtime.CodeInvalidRecord)
	}

	startNode := pf.Plan.StartNodeID
	t.Run("the row is append-only", func(t *testing.T) {
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE workflow_execution_fingerprint SET runtime_version = 'forged' WHERE tenant_id = $1`, tenantID)
			return err
		})
		if err == nil {
			t.Error("the application role updated a fingerprint")
		}
		if err := db.ExecErr(`DELETE FROM workflow_execution_fingerprint WHERE tenant_id = $1`, tenantID); err == nil {
			t.Error("even the owner deleted a fingerprint past the forbid_mutation trigger")
		}
	})

	t.Run("a tampered row is refused", func(t *testing.T) {
		db.Exec(t, `ALTER TABLE workflow_execution_fingerprint DISABLE TRIGGER workflow_execution_fingerprint_append_only`)
		db.Exec(t, `UPDATE workflow_execution_fingerprint SET components = array_append(components, 'POLICY=forged')
			WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3`, tenantID, started.InstanceID, startNode)
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, _, err := runtime.LoadNodeFingerprint(ctx, tx, tenantID, started.InstanceID, startNode, 1)
			return err
		})
		if runtimeCode(err) != runtime.CodeFingerprintDrift {
			t.Errorf("load of a tampered fingerprint = %v, want %s", err, runtime.CodeFingerprintDrift)
		}
		db.Exec(t, `UPDATE workflow_execution_fingerprint SET components = array_append(components, 'not-a-token')
			WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3`, tenantID, started.InstanceID, startNode)
		err = inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, _, err := runtime.LoadNodeFingerprint(ctx, tx, tenantID, started.InstanceID, startNode, 1)
			return err
		})
		if runtimeCode(err) != runtime.CodeFingerprintDrift {
			t.Errorf("load of a malformed component = %v, want %s", err, runtime.CodeFingerprintDrift)
		}
		db.Exec(t, `ALTER TABLE workflow_execution_fingerprint ENABLE TRIGGER workflow_execution_fingerprint_append_only`)
	})

	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		_, found, err := runtime.LoadNodeFingerprint(ctx, tx, tenantID, started.InstanceID, startNode, 99)
		if found {
			t.Error("an attempt that never ran reported a fingerprint")
		}
		return err
	})
}

// TestTodo_WF_RUN_038_Integration proves the blast-radius index across
// instances and tenants: a capability version bound only by a node deep in
// the walk touches the instance that reached it and not the one that only
// started, the plan digest touches both, and another tenant's scope sees
// none of them.
func TestTodo_WF_RUN_038_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun038i")
	otherTenant := insertTenant(t, db, "wfrun038i-other")
	pf := newPromotionFixture(t, values.TenantId("wfrun038i-tenant"), "intent:wf-run-038-i")

	walked := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "wfrun038i-walked"))
	runPromotionWalk(t, conn, tenantID, pf.Plan, walked.InstanceID, walked.InstanceVersion, runtime.NewMemorySink(), promotionExceedsThresholdWalk)
	idle := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "wfrun038i-idle"))

	startFP, err := runtime.DeriveNodeFingerprint(pf.Plan, pf.Plan.StartNodeID, runtime.RuntimeVersion)
	if err != nil {
		t.Fatal(err)
	}
	var deep runtime.FingerprintComponent
	for _, step := range promotionExceedsThresholdWalk[1:] {
		node, _ := pf.Plan.Node(step.NodeID)
		if node.Capability == nil {
			continue
		}
		c := runtime.FingerprintComponent{Kind: runtime.FingerprintCapability, Ref: fmt.Sprintf("%s@v%d", node.Capability.ID, node.Capability.Version)}
		if !startFP.Touches(c.Kind, c.Ref) {
			deep = c
			break
		}
	}
	if deep.Ref == "" {
		t.Fatal("the promotion walk binds no capability beyond the start node; the fixture no longer exercises the index")
	}

	touching := func(scope uuid.UUID, c runtime.FingerprintComponent) []uuid.UUID {
		var out []uuid.UUID
		inTenantTx(t, conn, scope, func(tx dbport.Tx) error {
			var err error
			out, err = runtime.InstancesTouching(ctx, tx, scope, c.Kind, c.Ref)
			return err
		})
		return out
	}
	if got := touching(tenantID, deep); len(got) != 1 || got[0] != walked.InstanceID {
		t.Errorf("%s=%s touched %v, want only the walked instance %s", deep.Kind, deep.Ref, got, walked.InstanceID)
	}
	plan := runtime.FingerprintComponent{Kind: runtime.FingerprintPlan, Ref: pf.Plan.Digest()}
	if got := touching(tenantID, plan); len(got) != 2 || !containsID(got, walked.InstanceID) || !containsID(got, idle.InstanceID) {
		t.Errorf("plan digest touched %v, want both %s and %s", got, walked.InstanceID, idle.InstanceID)
	}
	if got := touching(otherTenant, plan); len(got) != 0 {
		t.Errorf("another tenant's index answered %v", got)
	}
	// The application role under the other tenant's scope cannot read this
	// tenant's rows even when it names this tenant: RLS, not the predicate,
	// holds the line.
	var leaked []uuid.UUID
	inTenantTx(t, conn, otherTenant, func(tx dbport.Tx) error {
		var err error
		leaked, err = runtime.InstancesTouching(ctx, tx, tenantID, plan.Kind, plan.Ref)
		return err
	})
	if len(leaked) != 0 {
		t.Errorf("cross-tenant query under RLS leaked %v", leaked)
	}
}

func containsID(ids []uuid.UUID, want uuid.UUID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
