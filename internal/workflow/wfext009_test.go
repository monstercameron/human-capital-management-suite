package workflow_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func compilePromotionSchema(t *testing.T, schema uint32) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(workflow.PromotionReferenceDefinition(), workflow.Options{
		Phase: workflow.PhaseP1A, Capabilities: promotionRegistry(t), IRSchemaVersion: schema,
	})
	if err != nil {
		t.Fatalf("compile schema v%d: %v", schema, err)
	}
	return plan
}

// TestTodo_WF_EXT_009 proves that a compiled plan selects a supported schema,
// and that the schema participates in the plan digest.
func TestTodo_WF_EXT_009(t *testing.T) {
	v1 := compilePromotionSchema(t, 1)
	v2 := compilePromotionSchema(t, 0)
	if v1.SchemaVersion() != 1 || v2.SchemaVersion() != workflow.CurrentIRSchemaVersion {
		t.Fatalf("schema versions = %d and %d, want 1 and %d", v1.SchemaVersion(), v2.SchemaVersion(), workflow.CurrentIRSchemaVersion)
	}
	if v1.Digest() == v2.Digest() {
		t.Fatal("different IR schemas produced the same plan digest")
	}
	if err := v1.Verify(); err != nil {
		t.Fatalf("v1 Verify: %v", err)
	}
	if err := v2.Verify(); err != nil {
		t.Fatalf("v2 Verify: %v", err)
	}
	state, err := frontier.Seed(v2, "wf-ext-009-v2")
	if err != nil {
		t.Fatalf("seed v2 run: %v", err)
	}
	if _, err := frontier.Advance(v2, state, frontier.NodeOutcome{
		NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded,
		OutputDigest: "sha256:v2-execution",
	}); err != nil {
		t.Fatalf("execute v2 plan: %v", err)
	}
	raw, err := json.Marshal(v2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ir_schema_version":2`) {
		t.Fatalf("v2 canonical plan omitted its IR schema version: %s", raw)
	}
}

// TestTodo_WF_EXT_009_Golden keeps Promotion's frozen v1 digest and wire form.
func TestTodo_WF_EXT_009_Golden(t *testing.T) {
	plan, err := promotionexec.CompileV1_0()
	if err != nil {
		t.Fatal(err)
	}
	const frozenPromotionV1Digest = "655535f1e484991a79562a292eb21374381b1e757e115a3ec53eb25ce61679d7"
	if plan.SchemaVersion() != 1 || plan.Digest() != frozenPromotionV1Digest {
		t.Fatalf("Promotion v1 schema/digest = %d/%s, want 1/%s", plan.SchemaVersion(), plan.Digest(), frozenPromotionV1Digest)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"ir_schema_version"`) {
		t.Fatalf("frozen v1 bytes gained a schema field: %s", raw)
	}
	decoded, err := workflow.DecodeCanonicalPlan(raw)
	if err != nil || decoded.Digest() != frozenPromotionV1Digest {
		t.Fatalf("decode frozen Promotion plan = %v, %v", decoded, err)
	}
}

// TestTodo_WF_EXT_009_Recovery proves a saved in-flight v1 plan remains the
// plan used to advance its run after v2 compilation is available.
func TestTodo_WF_EXT_009_Recovery(t *testing.T) {
	pinned := compilePromotionSchema(t, 1)
	bytes, err := json.Marshal(pinned)
	if err != nil {
		t.Fatal(err)
	}
	upgraded := compilePromotionSchema(t, 0)
	if upgraded.SchemaVersion() != workflow.CurrentIRSchemaVersion {
		t.Fatalf("upgraded schema = %d", upgraded.SchemaVersion())
	}
	recovered, err := workflow.DecodeCanonicalPlan(bytes)
	if err != nil {
		t.Fatalf("decode pinned v1 plan after upgrade: %v", err)
	}
	if recovered.SchemaVersion() != 1 || recovered.Digest() != pinned.Digest() {
		t.Fatalf("recovered plan = schema %d, digest %s; want v1 digest %s", recovered.SchemaVersion(), recovered.Digest(), pinned.Digest())
	}
	state, err := frontier.Seed(recovered, "wf-ext-009-inflight")
	if err != nil {
		t.Fatalf("seed recovered run: %v", err)
	}
	out := frontier.NodeOutcome{NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:recovered-v1"}
	if _, err := frontier.Advance(recovered, state, out); err != nil {
		t.Fatalf("advance with pinned v1 decoder: %v", err)
	}
	if _, err := frontier.Advance(upgraded, state, out); err == nil {
		t.Fatal("run pinned to v1 advanced under the newly compiled v2 plan")
	}
}

// TestTodo_WF_EXT_009_Fault proves unsupported schemas fail closed.
func TestTodo_WF_EXT_009_Fault(t *testing.T) {
	raw := []byte(`{"workflow_id":"workflow.test","ir_schema_version":99}`)
	if _, err := workflow.DecodeCanonicalPlan(raw); err == nil || !strings.Contains(err.Error(), "unsupported IR schema version 99") {
		t.Fatalf("unknown schema refusal = %v, want unsupported-version error", err)
	}
	if _, err := workflow.DecodeCanonicalPlan([]byte(`{"workflow_id":"workflow.test","ir_schema_version":null}`)); err == nil {
		t.Fatal("null schema version was treated as a legacy v1 plan")
	}
	if _, err := workflow.DecodeCanonicalPlan([]byte(`{"workflow_id":"workflow.test","IR_SCHEMA_VERSION":2}`)); err == nil {
		t.Fatal("case-variant schema key was treated as a legacy v1 plan")
	}
	if _, err := workflow.DecodeCanonicalPlan([]byte(`{"workflow_id":"workflow.test","ir_schema_version":1,"ir_schema_version":2}`)); err == nil {
		t.Fatal("duplicate schema keys were accepted")
	}
	plan := compilePromotionSchema(t, 1)
	plan.IRSchemaVersion = 99
	if err := plan.Verify(); err == nil {
		t.Fatal("Verify accepted an unknown schema version")
	}
}
