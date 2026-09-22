package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func sampleWorkflowInputs(tenantID, instanceID uuid.UUID, planDigest string) runtime.WorkflowInputArtifact {
	return runtime.WorkflowInputArtifact{
		TenantID: tenantID, InstanceID: instanceID, PlanDigest: planDigest,
		Inputs: []runtime.TypedArtifactValue{
			{Path: "worker_id", Type: workflow.ValueType{Kind: workflow.KindString, Brand: "WorkerID"}, Value: "worker:1"},
			{Path: "proposal_digest", Type: workflow.ValueType{Kind: workflow.KindString}, Value: "sha256:proposal"},
		},
		RecordedAt: fixedInstant,
	}
}

func sampleNodeOutputs(tenantID, instanceID uuid.UUID, nodeID, planDigest string) runtime.NodeOutputArtifact {
	return runtime.NodeOutputArtifact{
		TenantID: tenantID, InstanceID: instanceID, NodeID: nodeID, Attempt: 1,
		StepType: workflow.StepTransform, PlanDigest: planDigest,
		Outputs: []runtime.TypedArtifactValue{
			{Path: "score", Type: workflow.ValueType{Kind: workflow.KindDecimal}, Value: "1.5000"},
			{Path: "label", Type: workflow.ValueType{Kind: workflow.KindString}, Value: "STRONG"},
		},
		RecordedAt: fixedInstant,
	}
}

func TestWorkflowInputArtifactDigestIsCanonical(t *testing.T) {
	a := sampleWorkflowInputs(uuid.New(), uuid.New(), "sha256:plan")
	if err := a.Validate(); err != nil {
		t.Fatalf("valid artifact refused: %v", err)
	}
	if len(a.Digest()) < 7 || a.Digest()[:7] != "sha256:" {
		t.Fatalf("digest %q is not a sha256 reference", a.Digest())
	}
	reordered := a.Clone()
	reordered.Inputs[0], reordered.Inputs[1] = reordered.Inputs[1], reordered.Inputs[0]
	if reordered.Digest() != a.Digest() {
		t.Fatal("input order changed the digest")
	}
	changed := a.Clone()
	changed.Inputs[0].Value = "changed"
	if changed.Digest() == a.Digest() {
		t.Fatal("a changed input value kept the digest")
	}
	if v, ok := a.Input("worker_id"); !ok || v.Value != "worker:1" {
		t.Fatalf("Input = %+v, %v", v, ok)
	}
	if _, ok := a.Input("absent"); ok {
		t.Fatal("an absent input was found")
	}
}

func TestWorkflowInputArtifactValidateRefusesIncompleteArtifacts(t *testing.T) {
	base := sampleWorkflowInputs(uuid.New(), uuid.New(), "sha256:plan")
	for name, mutate := range map[string]func(*runtime.WorkflowInputArtifact){
		"tenant":         func(a *runtime.WorkflowInputArtifact) { a.TenantID = uuid.Nil },
		"plan":           func(a *runtime.WorkflowInputArtifact) { a.PlanDigest = "" },
		"no inputs":      func(a *runtime.WorkflowInputArtifact) { a.Inputs = nil },
		"instant":        func(a *runtime.WorkflowInputArtifact) { a.RecordedAt = time.Time{} },
		"duplicate path": func(a *runtime.WorkflowInputArtifact) { a.Inputs = append(a.Inputs, a.Inputs[0]) },
	} {
		a := base.Clone()
		mutate(&a)
		if code := runtimeCode(a.Validate()); code != runtime.CodeInvalidRecord {
			t.Errorf("%s: code %q, want %s", name, code, runtime.CodeInvalidRecord)
		}
	}
}

func TestNodeOutputArtifactDigestIsCanonical(t *testing.T) {
	a := sampleNodeOutputs(uuid.New(), uuid.New(), "compute", "sha256:plan")
	if err := a.Validate(); err != nil {
		t.Fatalf("valid artifact refused: %v", err)
	}
	reordered := a.Clone()
	reordered.Outputs[0], reordered.Outputs[1] = reordered.Outputs[1], reordered.Outputs[0]
	if reordered.Digest() != a.Digest() {
		t.Fatal("output order changed the digest")
	}
	changed := a.Clone()
	changed.Outputs[0].Value = "9.9999"
	if changed.Digest() == a.Digest() {
		t.Fatal("a changed output value kept the digest")
	}
	if v, ok := a.Output("label"); !ok || v.Value != "STRONG" {
		t.Fatalf("Output = %+v, %v", v, ok)
	}
}

func TestNodeOutputArtifactValidateRefusesIncompleteArtifacts(t *testing.T) {
	base := sampleNodeOutputs(uuid.New(), uuid.New(), "compute", "sha256:plan")
	for name, mutate := range map[string]func(*runtime.NodeOutputArtifact){
		"tenant":         func(a *runtime.NodeOutputArtifact) { a.TenantID = uuid.Nil },
		"node":           func(a *runtime.NodeOutputArtifact) { a.NodeID = " " },
		"attempt":        func(a *runtime.NodeOutputArtifact) { a.Attempt = 0 },
		"step type":      func(a *runtime.NodeOutputArtifact) { a.StepType = "" },
		"plan":           func(a *runtime.NodeOutputArtifact) { a.PlanDigest = "" },
		"no outputs":     func(a *runtime.NodeOutputArtifact) { a.Outputs = nil },
		"instant":        func(a *runtime.NodeOutputArtifact) { a.RecordedAt = time.Time{} },
		"duplicate path": func(a *runtime.NodeOutputArtifact) { a.Outputs = append(a.Outputs, a.Outputs[0]) },
	} {
		a := base.Clone()
		mutate(&a)
		if code := runtimeCode(a.Validate()); code != runtime.CodeInvalidRecord {
			t.Errorf("%s: code %q, want %s", name, code, runtime.CodeInvalidRecord)
		}
	}
}

// TestRecordWorkflowInputsIsPinnedAndImmutable proves the durable half of
// WF-EXT-004's workflow-input contract: an artifact is bound to the
// instance's compiled plan, re-recording the identical document is
// idempotent, a different document for the same instance is refused, a
// tampered row is refused on load, and the table refuses UPDATE and DELETE.
func TestRecordWorkflowInputsIsPinnedAndImmutable(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfext004inputs")
	pf := newPromotionFixture(t, values.TenantId("wfext004inputs-tenant"), "intent:wf-ext-004-inputs")
	started := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "wfext004inputs"))

	artifact := sampleWorkflowInputs(tenantID, started.InstanceID, pf.Plan.Digest())
	var digest string
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		digest, err = runtime.RecordWorkflowInputs(ctx, tx, artifact)
		return err
	})
	if digest != artifact.Digest() {
		t.Fatalf("recorded digest %s, want %s", digest, artifact.Digest())
	}

	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		again, err := runtime.RecordWorkflowInputs(ctx, tx, artifact)
		if err == nil && again != digest {
			t.Errorf("identical re-record returned %s, want %s", again, digest)
		}
		return err
	})

	loadOne := func(tenant, instance uuid.UUID) (runtime.WorkflowInputArtifact, bool, error) {
		var (
			out   runtime.WorkflowInputArtifact
			found bool
		)
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			var err error
			out, found, err = runtime.LoadWorkflowInputs(ctx, tx, tenant, instance)
			return err
		})
		return out, found, err
	}

	loaded, found, err := loadOne(tenantID, started.InstanceID)
	if err != nil || !found || loaded.Digest() != digest {
		t.Fatalf("loaded = %+v, found=%v, %v", loaded, found, err)
	}

	for name, tc := range map[string]struct {
		mutate func(*runtime.WorkflowInputArtifact)
		want   string
	}{
		"different inputs for a recorded instance": {func(a *runtime.WorkflowInputArtifact) { a.Inputs[0].Value = "worker:2" }, runtime.CodeWorkflowInputConflict},
		"a plan the instance does not pin":         {func(a *runtime.WorkflowInputArtifact) { a.PlanDigest = "sha256:other" }, runtime.CodeAdvancePlanMismatch},
		"an instance that does not exist":          {func(a *runtime.WorkflowInputArtifact) { a.InstanceID = uuid.New() }, runtime.CodeInstanceNotFound},
		"an invalid artifact":                      {func(a *runtime.WorkflowInputArtifact) { a.Inputs = nil }, runtime.CodeInvalidRecord},
	} {
		a := artifact.Clone()
		tc.mutate(&a)
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, err := runtime.RecordWorkflowInputs(ctx, tx, a)
			return err
		})
		if code := runtimeCode(err); code != tc.want {
			t.Errorf("%s: code %q (%v), want %s", name, code, err, tc.want)
		}
	}

	// An instance that never recorded any document reports found=false.
	otherTenant := insertTenant(t, db, "wfext004inputs-empty")
	otherPF := newPromotionFixture(t, values.TenantId("wfext004inputs-empty-tenant"), "intent:wf-ext-004-inputs-empty")
	otherStarted := startPromotionInstance(t, conn, otherTenant, otherPF.baseStartRequest(otherTenant, "wfext004inputs-empty"))
	_, found, err = loadOne(otherTenant, otherStarted.InstanceID)
	if err != nil || found {
		t.Fatalf("load of an instance with no recorded document: found=%v, err=%v, want found=false", found, err)
	}

	// Historical evidence is immutable, even to the owner role.
	for _, stmt := range []string{
		`UPDATE workflow_input_artifact SET plan_digest = 'sha256:x' WHERE tenant_id = $1`,
		`DELETE FROM workflow_input_artifact WHERE tenant_id = $1`,
	} {
		if err := db.ExecErr(stmt, tenantID); err == nil {
			t.Errorf("%q succeeded against append-only evidence", stmt)
		}
	}

	// A row whose content no longer digests to its recorded digest is refused.
	db.Exec(t, `ALTER TABLE workflow_input_artifact DISABLE TRIGGER workflow_input_artifact_append_only`)
	db.Exec(t, `UPDATE workflow_input_artifact SET artifact = jsonb_set(artifact, '{inputs,0,value}', '"tampered"') WHERE tenant_id = $1`, tenantID)
	db.Exec(t, `ALTER TABLE workflow_input_artifact ENABLE TRIGGER workflow_input_artifact_append_only`)
	if _, _, err := loadOne(tenantID, started.InstanceID); runtimeCode(err) != runtime.CodeInvalidRecord {
		t.Fatalf("load of a tampered artifact = %v, want %s", err, runtime.CodeInvalidRecord)
	}
}

// TestRecordNodeOutputsIsPinnedAndImmutable proves the same contract for a
// node attempt's typed output artifact, plus the declared-output validation
// [runtime.RecordNodeOutputs] performs when a caller passes the compiled node.
func TestRecordNodeOutputsIsPinnedAndImmutable(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfext004outputs")
	pf := newPromotionFixture(t, values.TenantId("wfext004outputs-tenant"), "intent:wf-ext-004-outputs")
	started := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "wfext004outputs"))

	node := &workflow.CompiledNode{
		ID:   "compute",
		Type: workflow.StepTransform,
		Outputs: []workflow.Field{
			{Path: "score", Type: workflow.ValueType{Kind: workflow.KindDecimal}},
			{Path: "label", Type: workflow.ValueType{Kind: workflow.KindString}},
		},
	}
	artifact := sampleNodeOutputs(tenantID, started.InstanceID, "compute", pf.Plan.Digest())

	var digest string
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		digest, err = runtime.RecordNodeOutputs(ctx, tx, artifact, node)
		return err
	})
	if digest != artifact.Digest() {
		t.Fatalf("recorded digest %s, want %s", digest, artifact.Digest())
	}

	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		again, err := runtime.RecordNodeOutputs(ctx, tx, artifact, node)
		if err == nil && again != digest {
			t.Errorf("identical re-record returned %s, want %s", again, digest)
		}
		return err
	})

	loadAll := func() ([]runtime.NodeOutputArtifact, error) {
		var out []runtime.NodeOutputArtifact
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			var err error
			out, err = runtime.LoadNodeOutputs(ctx, tx, tenantID, started.InstanceID)
			return err
		})
		return out, err
	}

	loaded, err := loadAll()
	if err != nil || len(loaded) != 1 || loaded[0].Digest() != digest {
		t.Fatalf("loaded = %+v, %v", loaded, err)
	}

	for name, tc := range map[string]struct {
		node   *workflow.CompiledNode
		mutate func(*runtime.NodeOutputArtifact)
		want   string
	}{
		"different outputs for a recorded attempt": {node, func(a *runtime.NodeOutputArtifact) { a.Outputs[0].Value = "9.0000" }, runtime.CodeNodeOutputConflict},
		"a plan the instance does not pin":         {node, func(a *runtime.NodeOutputArtifact) { a.PlanDigest = "sha256:other" }, runtime.CodeAdvancePlanMismatch},
		"an instance that does not exist":          {node, func(a *runtime.NodeOutputArtifact) { a.InstanceID = uuid.New() }, runtime.CodeInstanceNotFound},
		"an invalid artifact":                      {node, func(a *runtime.NodeOutputArtifact) { a.Outputs = nil }, runtime.CodeInvalidRecord},
		"an undeclared output path": {node, func(a *runtime.NodeOutputArtifact) {
			a.Attempt = 2
			a.Outputs = []runtime.TypedArtifactValue{{Path: "undeclared", Type: workflow.ValueType{Kind: workflow.KindString}, Value: "x"}}
		}, runtime.CodeInvalidRecord},
		"a wrong-typed declared output": {node, func(a *runtime.NodeOutputArtifact) {
			a.Attempt = 2
			a.Outputs = []runtime.TypedArtifactValue{{Path: "score", Type: workflow.ValueType{Kind: workflow.KindString}, Value: "not-a-decimal"}}
		}, runtime.CodeInvalidRecord},
	} {
		a := artifact.Clone()
		tc.mutate(&a)
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, err := runtime.RecordNodeOutputs(ctx, tx, a, tc.node)
			return err
		})
		if code := runtimeCode(err); code != tc.want {
			t.Errorf("%s: code %q (%v), want %s", name, code, err, tc.want)
		}
	}

	// A nil node skips the declared-output check entirely.
	untyped := artifact.Clone()
	untyped.NodeID = "no_declared_check"
	untyped.Outputs = []runtime.TypedArtifactValue{{Path: "anything", Type: workflow.ValueType{Kind: workflow.KindString}, Value: "x"}}
	if err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, err := runtime.RecordNodeOutputs(ctx, tx, untyped, nil)
		return err
	}); err != nil {
		t.Fatalf("RecordNodeOutputs with nil node: %v", err)
	}

	// Historical evidence is immutable, even to the owner role.
	for _, stmt := range []string{
		`UPDATE workflow_node_output_artifact SET plan_digest = 'sha256:x' WHERE tenant_id = $1`,
		`DELETE FROM workflow_node_output_artifact WHERE tenant_id = $1`,
	} {
		if err := db.ExecErr(stmt, tenantID); err == nil {
			t.Errorf("%q succeeded against append-only evidence", stmt)
		}
	}

	// A row whose content no longer digests to its recorded digest is refused.
	db.Exec(t, `ALTER TABLE workflow_node_output_artifact DISABLE TRIGGER workflow_node_output_artifact_append_only`)
	db.Exec(t, `UPDATE workflow_node_output_artifact SET artifact = jsonb_set(artifact, '{outputs,0,value}', '"tampered"') WHERE tenant_id = $1 AND node_id = 'compute' AND attempt = 1`, tenantID)
	db.Exec(t, `ALTER TABLE workflow_node_output_artifact ENABLE TRIGGER workflow_node_output_artifact_append_only`)
	if _, err := loadAll(); runtimeCode(err) != runtime.CodeInvalidRecord {
		t.Fatalf("load with a tampered artifact = %v, want %s", err, runtime.CodeInvalidRecord)
	}
}
