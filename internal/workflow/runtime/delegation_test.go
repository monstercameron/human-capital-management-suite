package runtime_test

import (
	"context"
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func validDelegation() runtime.ExecutionDelegation {
	return runtime.ExecutionDelegation{
		Subject: "hc-050-rafael-torres", SubjectKind: "human", TenantKey: "harborcare",
		OrganizationScopeID: "org:harborcare:people-ops", Roles: []string{"promotion_operator", "hr_admin", "promotion_operator"},
		Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
		SessionRef: "session-1", EvidenceRef: "ev:authn:abc",
	}
}

func TestExecutionDelegationValidateRefusesMissingFields(t *testing.T) {
	if err := validDelegation().Validate(); err != nil {
		t.Fatalf("valid delegation refused: %v", err)
	}
	for name, mutate := range map[string]func(*runtime.ExecutionDelegation){
		"subject":       func(d *runtime.ExecutionDelegation) { d.Subject = " " },
		"subject kind":  func(d *runtime.ExecutionDelegation) { d.SubjectKind = "" },
		"tenant":        func(d *runtime.ExecutionDelegation) { d.TenantKey = "" },
		"session":       func(d *runtime.ExecutionDelegation) { d.SessionRef = "" },
		"method":        func(d *runtime.ExecutionDelegation) { d.AuthenticationMethod = "" },
		"assurance":     func(d *runtime.ExecutionDelegation) { d.Assurance = "" },
		"evidence":      func(d *runtime.ExecutionDelegation) { d.EvidenceRef = "" },
		"purpose":       func(d *runtime.ExecutionDelegation) { d.Purposes = nil },
		"blank purpose": func(d *runtime.ExecutionDelegation) { d.Purposes = []string{" "} },
	} {
		d := validDelegation()
		mutate(&d)
		if code := runtimeCode(d.Validate()); code != runtime.CodeInvalidRecord {
			t.Errorf("missing %s: code %q, want %s", name, code, runtime.CodeInvalidRecord)
		}
	}
}

// TestExecutionDelegationIsPinnedAtStartOutsideTheContextDigest proves the
// start transaction records the delegation once, a replay keeps the original,
// the execution-context digest is unchanged by it, an instance started
// without one has none, and the row is append-only.
func TestExecutionDelegationIsPinnedAtStartOutsideTheContextDigest(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun034-delegation")
	pf := newPromotionFixture(t, values.TenantId("wfrun034-tenant"), "intent:wf-run-034-delegation")

	plain := pf.baseStartRequest(tenantID, "wfrun034-plain")
	plainStarted := startPromotionInstance(t, conn, tenantID, plain)

	req := pf.baseStartRequest(tenantID, "wfrun034-delegated")
	delegation := validDelegation()
	req.Delegation = &delegation
	started := startPromotionInstance(t, conn, tenantID, req)
	if started.ExecutionContext.Digest() != runtime.DeriveExecutionContext(pf.baseStartRequest(tenantID, "wfrun034-delegated"), runtime.WorkflowSelection{WorkflowID: pf.Plan.WorkflowID, Plan: pf.Plan}).Digest() {
		t.Fatal("the delegation changed the pinned execution-context digest")
	}

	var loaded runtime.ExecutionDelegation
	var found, plainFound bool
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		if loaded, found, err = runtime.LoadExecutionDelegation(ctx, tx, tenantID, started.InstanceID); err != nil {
			return err
		}
		_, plainFound, err = runtime.LoadExecutionDelegation(ctx, tx, tenantID, plainStarted.InstanceID)
		return err
	})
	if !found || loaded.Subject != delegation.Subject || loaded.SubjectKind != "human" || loaded.OrganizationScopeID != delegation.OrganizationScopeID ||
		!slices.Equal(loaded.Roles, []string{"hr_admin", "promotion_operator"}) || !slices.Equal(loaded.Purposes, delegation.Purposes) ||
		loaded.SessionRef != "session-1" || loaded.EvidenceRef != "ev:authn:abc" || loaded.TenantKey != "harborcare" ||
		loaded.AuthenticationMethod != "bearer_token" || loaded.Assurance != "substantial" || !loaded.RecordedAt.Equal(req.CreatedAt.UTC()) {
		t.Fatalf("loaded delegation = %+v (found %v)", loaded, found)
	}
	if plainFound {
		t.Fatal("an instance started without a delegation loaded one")
	}

	other := validDelegation()
	other.Subject = "hc-999-somebody-else"
	req.Delegation = &other
	if replayed := startPromotionInstance(t, conn, tenantID, req); !replayed.Replay {
		t.Fatalf("second start = %+v, want a replay", replayed)
	}
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		again, _, err := runtime.LoadExecutionDelegation(ctx, tx, tenantID, started.InstanceID)
		if err == nil && again.Subject != delegation.Subject {
			t.Fatalf("a replayed start rewrote the delegation to %q", again.Subject)
		}
		return err
	})

	invalid := pf.baseStartRequest(tenantID, "wfrun034-invalid")
	bad := validDelegation()
	bad.Purposes = nil
	invalid.Delegation = &bad
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, err := runtime.Start(ctx, tx, invalid)
		return err
	})
	if runtimeCode(err) != runtime.CodeInvalidRecord {
		t.Fatalf("start with an invalid delegation = %v, want %s", err, runtime.CodeInvalidRecord)
	}

	if err := db.ExecErr(`UPDATE workflow_execution_delegation SET subject = 'x' WHERE tenant_id = $1`, tenantID); err == nil {
		t.Fatal("workflow_execution_delegation accepted an UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM workflow_execution_delegation WHERE tenant_id = $1`, tenantID); err == nil {
		t.Fatal("workflow_execution_delegation accepted a DELETE")
	}
}
