package execution

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	transactioncancel "github.com/monstercameron/human-capital-management-suite/internal/transaction/cancel"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/compensate"
)

type rev010Capability struct {
	calls int
	ref   string
}

func (c *rev010Capability) Manifest(context.Context) (compensate.CapabilityManifest, error) {
	ref := c.ref
	if ref == "" {
		ref = "inverse/v1"
	}
	return compensate.CapabilityManifest{CapabilityRef: ref, Digest: "manifest-digest", Idempotent: true}, nil
}

func TestTodo_WF_REV_010_GovernedCancelIntegration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := ext001SeedTenant(t, db)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	scope := idempotency.Scope{Tenant: tenantID, Capability: "transaction.commit", EffectScope: "promotion-plan-1", Key: "promotion-commit-key"}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		store := idempotency.PostgresStore{}
		if _, created, err := store.Reserve(ctx, tx, scope, strings.Repeat("d", 64), idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 24 * time.Hour}, now); err != nil || !created {
			return fmt.Errorf("reserve transaction commit scope: created=%v err=%w", created, err)
		}
		_, err := store.Complete(ctx, tx, scope, idempotency.ResultIdentity{ResultRef: "commit-result", EventRef: "commit-event"}, now)
		return err
	})
	capability := &rev010Capability{ref: ServedHoldReleaseCapability}
	served, err := ComposeServedCompensation(ServedCompensationOptions{
		Capability: capability, Authorizer: rev010Authorizer{}, Observer: rev010Observer{now: now}, Clock: func() time.Time { return now }, DB: db.Conn,
		IntentForPlan: func(context.Context, uuid.UUID, string) (uuid.UUID, error) { return uuid.New(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	launch := served.GovernedCompensator()
	request := transactioncancel.CompensationRequest{CommitIdentity: "commit-identity-1", Request: transactioncancel.Request{
		Tenant: tenantID, PlanID: scope.EffectScope, PlanDigest: strings.Repeat("e", 64), IdempotencyKey: scope.Key,
	}}
	if err := launch(ctx, request); err != nil {
		t.Fatalf("served governed cancellation: %v", err)
	}
	var closed idempotency.Record
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		var found bool
		var err error
		closed, found, err = (idempotency.PostgresStore{}).Lookup(ctx, tx, scope)
		if err != nil {
			return err
		}
		if !found {
			return idempotency.ErrIdempotency
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM workflow_compensation_event WHERE tenant_id=$1`, tenantID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("governed compensation event count=%d", count)
		}
		return nil
	})
	if closed.Status != idempotency.StatusCompensated || capability.calls != 1 {
		t.Fatalf("governed closure=%+v capability calls=%d", closed, capability.calls)
	}
}

func TestTodo_WF_REV_010_PromotionHoldReleaseIntegration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := ext001SeedTenant(t, db)
	intentID, proposalID, instanceID := uuid.New(), uuid.New(), uuid.New()
	materialDigest := strings.Repeat("f", 64)
	ext001SeedHold(t, db, tenantID, intentID, proposalID, materialDigest)
	ext001RecordDelegation(t, db, tenantID, instanceID)
	req := ext001CompensateRequest(t, tenantID, instanceID, intentID.String(), proposalID.String(), materialDigest)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	served, err := ComposeServedCompensation(ServedCompensationOptions{DB: db.Conn, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	release := func(composition *ServedCompensation) promotionsteps.HoldReleaseResult {
		t.Helper()
		var result promotionsteps.HoldReleaseResult
		ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
			var err error
			result, err = composition.ReleaseHoldViaExecutor(withStepTx(ctx, tx), req)
			return err
		})
		return result
	}
	first := release(served)
	if first.Status != "COMPENSATED" || served.CapabilityCalls() != 1 {
		t.Fatalf("promotion hold output=%+v calls=%d", first, served.CapabilityCalls())
	}
	restarted, err := ComposeServedCompensation(ServedCompensationOptions{DB: db.Conn, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	second := release(restarted)
	if second.Status != "COMPENSATED" || restarted.CapabilityCalls() != 0 {
		t.Fatalf("promotion hold replay=%+v calls=%d", second, restarted.CapabilityCalls())
	}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM workflow_compensation_event WHERE tenant_id=$1`, tenantID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("hold release persisted %d compensation events", count)
		}
		return nil
	})
}
func (c *rev010Capability) Compensate(context.Context, compensate.CapabilityRequest) (compensate.CapabilityReceipt, error) {
	c.calls++
	return compensate.CapabilityReceipt{Accepted: true, Applied: true, EvidenceRef: "inverse-evidence"}, nil
}

type rev010Authorizer struct{}

func (rev010Authorizer) Authorize(_ context.Context, r compensate.Request) (compensate.AuthorizationDecision, error) {
	return compensate.AuthorizationDecision{Allowed: true, TenantID: r.TenantID, ActorID: r.ActorID, TargetEffectRef: r.TargetEffectRef, CapabilityRef: r.CompensationCapabilityRef, PayloadDigest: r.PayloadDigest, PolicyFingerprint: r.AuthorityPolicyFingerprint, ApprovalRef: r.ApprovalRef, EvidenceRef: "authorization-evidence"}, nil
}

type rev010Observer struct{ now time.Time }

func (o rev010Observer) ObserveCompensation(_ context.Context, r compensate.ObservationRequest) (compensate.Observation, error) {
	return compensate.Observation{Match: true, ObservedAt: o.now, TenantID: r.Request.TenantID, TargetEffectRef: r.Request.TargetEffectRef, CorrectionEvidenceRef: r.CorrectionEvidenceRef, EvidenceRef: "observation-evidence"}, nil
}

func TestTodo_WF_REV_010_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := ext001SeedTenant(t, db)
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	scope := idempotency.Scope{Tenant: tenantID, Capability: "workflow.promotion.execute", EffectScope: "workflow-node:payroll", Key: "original-step-key"}
	digest := strings.Repeat("a", 64)
	policy := idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 24 * time.Hour}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		store := idempotency.PostgresStore{}
		if _, created, err := store.Reserve(ctx, tx, scope, digest, policy, now); err != nil || !created {
			t.Fatalf("reserve original: created=%v err=%v", created, err)
		}
		_, err := store.Complete(ctx, tx, scope, idempotency.ResultIdentity{ResultRef: "original-result", EventRef: "original-event"}, now)
		return err
	})
	request := compensate.Request{TenantID: tenantID.String(), ActorID: "principal:worker", TargetExecutionRef: "payroll#1", TargetEffectRef: "payroll#1", CompensationCapabilityRef: "inverse/v1", VerificationObservationRef: "observe.inverse/v1", Reason: "verified-correction", ApprovalPolicy: "workflow-cancellation/v1", ApprovalRef: "obligation-1", AuthorityPolicyFingerprint: "authority-fingerprint", CapabilityManifestDigest: "manifest-digest", PayloadDigest: strings.Repeat("b", 64), IdempotencyKey: "compensation-key", OriginalHistoryRef: "workflow-effect:payroll#1", Strategy: compensate.StrategyCorrection, ObservationMaxAge: time.Hour, WorkflowID: scope.Capability, InstanceID: uuid.New(), PlanDigest: strings.Repeat("c", 64), OriginalScope: scope, OriginalEffectKind: compensate.OriginalEffectIdempotencyScope, OriginalEffectRefs: []string{"payroll#1"}}
	capability := &rev010Capability{}
	compose := func() *ServedCompensation {
		served, err := ComposeServedCompensation(ServedCompensationOptions{Capability: capability, Authorizer: rev010Authorizer{}, Observer: rev010Observer{now: now}, Clock: func() time.Time { return now }, DB: db.Conn})
		if err != nil {
			t.Fatalf("compose compensation: %v", err)
		}
		return served
	}
	served := compose()
	missingBinding := request
	missingBinding.OriginalScope = idempotency.Scope{}
	missingBinding.PlanDigest = ""
	missingBinding.OriginalEffectKind = ""
	if _, err := served.Executor().Execute(ctx, missingBinding); err == nil || capability.calls != 0 {
		t.Fatalf("durable compensation accepted missing original binding: err=%v calls=%d", err, capability.calls)
	}
	rollbackCapability := &rev010Capability{}
	rollbackComposition, err := ComposeServedCompensation(ServedCompensationOptions{
		Capability: rollbackCapability, Authorizer: rev010Authorizer{}, Observer: rev010Observer{now: now},
		Clock: func() time.Time { return now }, DB: db.Conn,
	})
	if err != nil {
		t.Fatal(err)
	}
	rollbackTx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = tenancy.WithTenant(ctx, rollbackTx, tenantID); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := rollbackComposition.Executor().Execute(withStepTx(ctx, rollbackTx), request)
	if err != nil || rolledBack.Status != compensate.StatusCompensated {
		_ = rollbackTx.Rollback(ctx)
		t.Fatalf("pre-rollback compensation=%+v err=%v", rolledBack, err)
	}
	if err = rollbackTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if rollbackCapability.calls != 1 {
		t.Fatalf("rollback attempt capability calls=%d, want 1", rollbackCapability.calls)
	}
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		original, found, lookupErr := (idempotency.PostgresStore{}).Lookup(ctx, tx, scope)
		if lookupErr != nil {
			return lookupErr
		}
		if !found || original.Status != idempotency.StatusCompleted {
			return fmt.Errorf("rollback left original record %+v found=%v, want COMPLETED", original, found)
		}
		var events, operations int
		if lookupErr = tx.QueryRow(ctx, `SELECT count(*) FROM workflow_compensation_event WHERE tenant_id=$1`, tenantID).Scan(&events); lookupErr != nil {
			return lookupErr
		}
		if lookupErr = tx.QueryRow(ctx, `SELECT count(*) FROM workflow_compensation_operation WHERE tenant_id=$1`, tenantID).Scan(&operations); lookupErr != nil {
			return lookupErr
		}
		if events != 0 || operations != 0 {
			return fmt.Errorf("rollback left %d events and %d operations", events, operations)
		}
		return nil
	})
	var first compensate.Result
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		first, err = served.Executor().Execute(withStepTx(ctx, tx), request)
		return err
	})
	if first.Status != compensate.StatusCompensated || first.Event.EventRef == "" || capability.calls != 1 {
		t.Fatalf("first=%+v capability calls=%d", first, capability.calls)
	}
	var closed idempotency.Record
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		var found bool
		var err error
		closed, found, err = (idempotency.PostgresStore{}).Lookup(ctx, tx, scope)
		if err != nil {
			return err
		}
		if !found {
			return idempotency.ErrIdempotency
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM workflow_compensation_event WHERE tenant_id=$1 AND event_ref=$2`, tenantID, first.Event.EventRef).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("persisted compensation event count=%d", count)
		}
		return nil
	})
	if closed.Status != idempotency.StatusCompensated || closed.CompensatedByRef != first.Event.EventRef {
		t.Fatalf("original idempotency record=%+v", closed)
	}
	// A new composition has no process memory from the first run. The same
	// compensation identity replays its durable result without calling the
	// corrective capability again.
	restarted := compose()
	var replay compensate.Result
	ext001InTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		replay, err = restarted.Executor().Execute(withStepTx(ctx, tx), request)
		return err
	})
	if !replay.Replayed || replay.Event.EventRef != first.Event.EventRef || capability.calls != 1 {
		t.Fatalf("restart replay=%+v capability calls=%d", replay, capability.calls)
	}
}
