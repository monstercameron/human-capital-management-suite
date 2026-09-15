package runtime_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func sampleNodeInputs(tenantID, instanceID uuid.UUID, planDigest string) runtime.NodeInputArtifact {
	return runtime.NodeInputArtifact{
		TenantID: tenantID, InstanceID: instanceID,
		NodeID: workflow.PromotionNodeRaiseThreshold, Attempt: 1, StepType: workflow.StepDecision,
		PlanDigest: planDigest,
		Inputs: []runtime.NodeInputValue{
			{Path: "increase_percent", Value: "15.0000"},
			{Path: "band_position", Value: "IN_BAND"},
		},
		Versions: []runtime.PinnedArtifactVersion{{
			Kind: runtime.PinnedVersionRuleTable, Ref: "hcmnext.rules.promotion_approval_threshold",
			Version: "2026.1", Digest: "sha256:" + strings.Repeat("a", 64),
		}},
		RecordedAt: fixedInstant,
	}
}

func TestNodeInputArtifactDigestIsCanonical(t *testing.T) {
	a := sampleNodeInputs(uuid.New(), uuid.New(), "sha256:plan")
	if err := a.Validate(); err != nil {
		t.Fatalf("valid artifact refused: %v", err)
	}
	if !strings.HasPrefix(a.Digest(), "sha256:") {
		t.Fatalf("digest %q is not a sha256 reference", a.Digest())
	}
	reordered := a.Clone()
	reordered.Inputs[0], reordered.Inputs[1] = reordered.Inputs[1], reordered.Inputs[0]
	if reordered.Digest() != a.Digest() {
		t.Fatal("input order changed the digest")
	}
	changed := a.Clone()
	changed.Inputs[0].Value = "5.0000"
	if changed.Digest() == a.Digest() {
		t.Fatal("a changed input value kept the digest")
	}
	if a.Inputs[0].Value != "15.0000" {
		t.Fatal("Clone shared the inputs backing array")
	}
	if v, ok := a.Input("band_position"); !ok || v != "IN_BAND" {
		t.Fatalf("Input = %q, %v", v, ok)
	}
	if _, ok := a.Input("absent"); ok {
		t.Fatal("an absent input was found")
	}
	if v, ok := a.Version(runtime.PinnedVersionRuleTable); !ok || v.Version != "2026.1" {
		t.Fatalf("Version = %+v, %v", v, ok)
	}
	if _, ok := a.Version("TRANSFORM"); ok {
		t.Fatal("an absent version kind was found")
	}
}

func TestNodeInputArtifactValidateRefusesIncompleteArtifacts(t *testing.T) {
	base := sampleNodeInputs(uuid.New(), uuid.New(), "sha256:plan")
	for name, mutate := range map[string]func(*runtime.NodeInputArtifact){
		"tenant":         func(a *runtime.NodeInputArtifact) { a.TenantID = uuid.Nil },
		"node":           func(a *runtime.NodeInputArtifact) { a.NodeID = " " },
		"attempt":        func(a *runtime.NodeInputArtifact) { a.Attempt = 0 },
		"step type":      func(a *runtime.NodeInputArtifact) { a.StepType = "" },
		"plan":           func(a *runtime.NodeInputArtifact) { a.PlanDigest = "" },
		"no inputs":      func(a *runtime.NodeInputArtifact) { a.Inputs = nil },
		"instant":        func(a *runtime.NodeInputArtifact) { a.RecordedAt = time.Time{} },
		"duplicate path": func(a *runtime.NodeInputArtifact) { a.Inputs = append(a.Inputs, a.Inputs[0]) },
		"bad version":    func(a *runtime.NodeInputArtifact) { a.Versions[0].Digest = "" },
	} {
		a := base.Clone()
		mutate(&a)
		if code := runtimeCode(a.Validate()); code != runtime.CodeInvalidRecord {
			t.Errorf("%s: code %q, want %s", name, code, runtime.CodeInvalidRecord)
		}
	}
}

// TestRecordNodeInputsIsPinnedAndImmutable proves the durable half of
// WF-RUN-013's pinned-input contract: an artifact is bound to the instance's
// plan and execution context, re-recording it is idempotent, a different
// artifact for the same attempt is refused, a tampered row is refused on load,
// and the table itself refuses UPDATE and DELETE.
func TestRecordNodeInputsIsPinnedAndImmutable(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun013inputs")
	pf := newPromotionFixture(t, values.TenantId("wfrun013inputs-tenant"), "intent:wf-run-013-inputs")
	started := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "wfrun013inputs"))
	pinnedContext := started.ExecutionContext.Digest()

	artifact := sampleNodeInputs(tenantID, started.InstanceID, pf.Plan.Digest())
	var digest string
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		digest, err = runtime.RecordNodeInputs(ctx, tx, artifact)
		return err
	})
	adopted := artifact.Clone()
	adopted.ExecutionContextDigest = pinnedContext
	if digest != adopted.Digest() {
		t.Fatalf("recorded digest %s, want the artifact under the pinned context %s", digest, adopted.Digest())
	}

	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		again, err := runtime.RecordNodeInputs(ctx, tx, adopted)
		if err == nil && again != digest {
			t.Errorf("identical re-record returned %s, want %s", again, digest)
		}
		return err
	})

	loadAll := func() ([]runtime.NodeInputArtifact, error) {
		var out []runtime.NodeInputArtifact
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			var err error
			out, err = runtime.LoadNodeInputs(ctx, tx, tenantID, started.InstanceID)
			return err
		})
		return out, err
	}
	loaded, err := loadAll()
	if err != nil || len(loaded) != 1 || loaded[0].Digest() != digest || loaded[0].ExecutionContextDigest != pinnedContext {
		t.Fatalf("loaded = %+v, %v", loaded, err)
	}

	for name, tc := range map[string]struct {
		mutate func(*runtime.NodeInputArtifact)
		want   string
	}{
		"different inputs for a recorded attempt": {func(a *runtime.NodeInputArtifact) { a.Inputs[0].Value = "5.0000" }, runtime.CodeNodeInputConflict},
		"a plan the instance does not pin":        {func(a *runtime.NodeInputArtifact) { a.PlanDigest = "sha256:other" }, runtime.CodeAdvancePlanMismatch},
		"a context the instance did not pin":      {func(a *runtime.NodeInputArtifact) { a.ExecutionContextDigest = "sha256:other" }, runtime.CodeContextDrift},
		"an instance that does not exist":         {func(a *runtime.NodeInputArtifact) { a.InstanceID = uuid.New() }, runtime.CodeInstanceNotFound},
		"an invalid artifact":                     {func(a *runtime.NodeInputArtifact) { a.Inputs = nil }, runtime.CodeInvalidRecord},
	} {
		a := artifact.Clone()
		tc.mutate(&a)
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, err := runtime.RecordNodeInputs(ctx, tx, a)
			return err
		})
		if code := runtimeCode(err); code != tc.want {
			t.Errorf("%s: code %q (%v), want %s", name, code, err, tc.want)
		}
	}

	// Another tenant sees nothing.
	other := insertTenant(t, db, "wfrun013inputs-other")
	if err := inTenantTxErr(conn, other, func(tx dbport.Tx) error {
		rows, err := runtime.LoadNodeInputs(ctx, tx, tenantID, started.InstanceID)
		if err == nil && len(rows) != 0 {
			t.Errorf("tenant isolation leaked %d artifacts", len(rows))
		}
		return err
	}); err != nil {
		t.Fatalf("load as another tenant: %v", err)
	}

	// Historical evidence is immutable, even to the owner role.
	for _, stmt := range []string{
		`UPDATE workflow_node_input_artifact SET plan_digest = 'sha256:x' WHERE tenant_id = $1`,
		`DELETE FROM workflow_node_input_artifact WHERE tenant_id = $1`,
	} {
		if err := db.ExecErr(stmt, tenantID); err == nil {
			t.Errorf("%q succeeded against append-only evidence", stmt)
		}
	}

	// A row whose content no longer digests to its recorded digest is refused.
	db.Exec(t, `ALTER TABLE workflow_node_input_artifact DISABLE TRIGGER workflow_node_input_artifact_append_only`)
	db.Exec(t, `UPDATE workflow_node_input_artifact SET artifact = jsonb_set(artifact, '{inputs,0,value}', '"99.0000"') WHERE tenant_id = $1`, tenantID)
	db.Exec(t, `ALTER TABLE workflow_node_input_artifact ENABLE TRIGGER workflow_node_input_artifact_append_only`)
	if _, err := loadAll(); runtimeCode(err) != runtime.CodeInvalidRecord {
		t.Fatalf("load of a tampered artifact = %v, want %s", err, runtime.CodeInvalidRecord)
	}
}
