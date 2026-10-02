package agentstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// A model step, once begun, is lasting evidence that a model call may have been
// paid for; a dead worker's open steps are closed so its successor is admitted.
func TestTodo_AGENTRUN_002_ModelStepEvidence(t *testing.T) {
	store, tenantID, lease, at := personaSecurityFixture(t)
	ctx := context.Background()
	started, err := store.PersonaRunModelCallMayHaveStarted(ctx, tenantID, lease.RunID)
	if err != nil || started {
		t.Fatalf("a run with no step reports a model call: %t %v", started, err)
	}
	if err := store.RunPersonaSecurityStep(ctx, tenantID, lease.LeaseID, "persona-context-1", at.Add(time.Second), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if started, err = store.PersonaRunModelCallMayHaveStarted(ctx, tenantID, lease.RunID); err != nil || started {
		t.Fatalf("a context step is not a model call: %t %v", started, err)
	}
	// The worker died inside its model call: the step is still STARTED.
	err = store.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO persona_security_step(tenant_id,lease_id,step_id,state,started_at) VALUES ($1,$2,'persona-model-1','STARTED',$3)`, tenantID, lease.LeaseID, at.Add(2*time.Second))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if started, err = store.PersonaRunModelCallMayHaveStarted(ctx, tenantID, lease.RunID); err != nil || !started {
		t.Fatalf("an open model step is not evidence: %t %v", started, err)
	}
	if other, err := store.PersonaRunModelCallMayHaveStarted(ctx, uuid.New(), lease.RunID); err != nil || other {
		t.Fatalf("another tenant sees the evidence: %t %v", other, err)
	}
	if err := store.RunPersonaSecurityStep(ctx, tenantID, lease.LeaseID, "next", at.Add(3*time.Second), func(context.Context) error { return nil }); !errors.Is(err, ErrPersonaSecurityRecovery) {
		t.Fatalf("a step began over a dead worker's open step: %v", err)
	}
	if n, err := store.RecoverPersonaRunSteps(ctx, tenantID, lease.RunID, at.Add(4*time.Second)); err != nil || n != 1 {
		t.Fatalf("recovered %d steps, %v; want 1", n, err)
	}
	if err := store.RunPersonaSecurityStep(ctx, tenantID, lease.LeaseID, "next", at.Add(5*time.Second), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("the successor was not admitted after recovery: %v", err)
	}
	// The evidence outlives the recovery: the call may still have been paid for.
	if started, err = store.PersonaRunModelCallMayHaveStarted(ctx, tenantID, lease.RunID); err != nil || !started {
		t.Fatalf("recovery erased the evidence: %t %v", started, err)
	}
	if n, err := store.RecoverPersonaRunSteps(ctx, tenantID, "run-without-a-lease", at); err != nil || n != 0 {
		t.Fatalf("a run with no lease recovered %d steps, %v", n, err)
	}
	for _, call := range []func() error{
		func() error {
			_, err := store.PersonaRunModelCallMayHaveStarted(ctx, uuid.Nil, lease.RunID)
			return err
		},
		func() error { _, err := store.PersonaRunModelCallMayHaveStarted(ctx, tenantID, " "); return err },
		func() error {
			_, err := store.RecoverPersonaRunSteps(ctx, tenantID, lease.RunID, time.Time{})
			return err
		},
		func() error {
			_, err := (*Store)(nil).RecoverPersonaRunSteps(ctx, tenantID, lease.RunID, at)
			return err
		},
	} {
		if err := call(); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
}
