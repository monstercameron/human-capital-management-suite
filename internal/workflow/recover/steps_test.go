package recover_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	wfrecover "github.com/monstercameron/human-capital-management-suite/internal/workflow/recover"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// countingStepEffect guards one node and counts the performances that reached
// Perform, optionally failing them.
type countingStepEffect struct {
	node     string
	fail     error
	performs int
}

func (e *countingStepEffect) Guards(node workflow.CompiledNode) bool { return node.ID == e.node }

func (e *countingStepEffect) Perform(_ context.Context, _ dbport.Tx, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	e.performs++
	if e.fail != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, e.fail
	}
	return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:guarded"},
		runtime.GovernanceRefs{CapabilityExecutionID: "cap-exec:1", EffectRefs: []string{"effect:1"}}, nil
}

// innerSteps is the unguarded runner.
type innerSteps struct{ calls int }

func (s *innerSteps) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	s.calls++
	return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded}, runtime.GovernanceRefs{}, nil
}

type guardedStepFixture struct {
	db     *pgtest.DB
	conn   *pgxadapter.Conn
	tenant uuid.UUID
	req    execute.StepRequest
	fence  lease.Fence
}

func newGuardedStepFixture(t *testing.T, key string) guardedStepFixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, key)
	conn := appConn(t, db)
	plan := referencePlan(t)
	node, _ := plan.Node(plan.StartNodeID)
	instanceID := uuid.New()
	f := guardedStepFixture{db: db, conn: conn, tenant: tenant, req: execute.StepRequest{
		TenantID: tenant, InstanceID: instanceID, Attempt: 1, Node: node, Plan: plan,
		CorrelationID: "corr-" + key, RecordedAt: bootAt,
	}}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		grant, err := lease.Manager{}.Acquire(context.Background(), tx, lease.AcquireRequest{
			TenantID: tenant, Resource: lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: instanceID.String()},
			Holder: workerA, Now: bootAt, TTL: time.Hour,
		})
		f.fence = grant.Fence
		return err
	})
	return f
}

func (f guardedStepFixture) steps(effect wfrecover.StepEffect, inner execute.StepRunner) wfrecover.GuardedSteps {
	return wfrecover.GuardedSteps{
		Inner: inner, Effects: effect, DB: f.conn, Store: idempotency.PostgresStore{},
		Retention: retention, Verifier: lease.Fenced{},
	}
}

func (f guardedStepFixture) fenced() context.Context {
	return execute.WithFence(context.Background(), f.fence.RuntimeFence(time.Time{}))
}

// TestTodo_WF_RUN_003_GuardedStep proves a guarded step effect is exactly-once
// per node activation: the first run performs it and stores its outcome, the
// redelivered run of the same activation replays the stored outcome without
// performing it, and a later activation is a new effect.
func TestTodo_WF_RUN_003_GuardedStep(t *testing.T) {
	f := newGuardedStepFixture(t, "wfrun003-guarded-step")
	effect := &countingStepEffect{node: f.req.Node.ID}
	inner := &innerSteps{}
	steps := f.steps(effect, inner)

	first, firstRefs, err := steps.Run(f.fenced(), f.req)
	if err != nil || effect.performs != 1 {
		t.Fatalf("first run = %+v, %v; performs %d, want 1", first, err, effect.performs)
	}
	replayed, replayedRefs, err := steps.Run(f.fenced(), f.req)
	if err != nil || effect.performs != 1 {
		t.Fatalf("redelivered run = %v; performs %d, want the stored result replayed without a second performance", err, effect.performs)
	}
	if !reflect.DeepEqual(first, replayed) || !reflect.DeepEqual(firstRefs, replayedRefs) {
		t.Fatalf("replayed outcome %+v/%+v differs from the performed %+v/%+v", replayed, replayedRefs, first, firstRefs)
	}
	var rec idempotency.Record
	inTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		var found bool
		rec, found, err = idempotency.PostgresStore{}.Lookup(context.Background(), tx, wfrecover.StepEffectRequest(f.req).Scope())
		if !found && err == nil {
			err = errors.New("no idempotency record")
		}
		return err
	})
	if rec.Status != idempotency.StatusCompleted || rec.Scope.Key != wfrecover.StepEffectKey(f.req.InstanceID, f.req.Node.ID, 1) {
		t.Fatalf("stored record = %+v, want COMPLETED under the activation key", rec)
	}

	next := f.req
	next.Attempt = 2
	if _, _, err := steps.Run(f.fenced(), next); err != nil || effect.performs != 2 {
		t.Fatalf("a later activation = %v; performs %d, want a new effect", err, effect.performs)
	}
	if inner.calls != 0 {
		t.Fatalf("a guarded node reached the inner runner %d times", inner.calls)
	}
}

// TestTodo_WF_RUN_003_GuardedStepFault proves the guard refuses before any
// effect when it cannot be exactly-once: no fence, a superseded fence, missing
// wiring, a failed performance (which commits nothing) and a stored result
// that is not a step result.
func TestTodo_WF_RUN_003_GuardedStepFault(t *testing.T) {
	f := newGuardedStepFixture(t, "wfrun003-guarded-fault")
	ctx := context.Background()

	inner := &innerSteps{}
	unguarded := &countingStepEffect{node: "some-other-node"}
	if _, _, err := f.steps(unguarded, inner).Run(ctx, f.req); err != nil || inner.calls != 1 {
		t.Fatalf("unguarded node = %v, inner calls %d; want the inner runner", err, inner.calls)
	}
	if _, _, err := f.steps(unguarded, nil).Run(ctx, f.req); !errors.Is(err, wfrecover.ErrInvalid) {
		t.Fatalf("unguarded node with no inner runner = %v, want ErrInvalid", err)
	}

	effect := &countingStepEffect{node: f.req.Node.ID}
	noVerifier := f.steps(effect, inner)
	noVerifier.Verifier = nil
	if _, _, err := noVerifier.Run(f.fenced(), f.req); !errors.Is(err, wfrecover.ErrInvalid) {
		t.Fatalf("no verifier = %v, want ErrInvalid", err)
	}
	if _, _, err := f.steps(effect, inner).Run(ctx, f.req); !errors.Is(err, wfrecover.ErrFenceRefused) {
		t.Fatalf("no fence on the context = %v, want ErrFenceRefused", err)
	}
	zeroInstant := f.req
	zeroInstant.RecordedAt = time.Time{}
	if _, _, err := f.steps(effect, inner).Run(f.fenced(), zeroInstant); !errors.Is(err, wfrecover.ErrInvalid) {
		t.Fatalf("no instant = %v, want ErrInvalid", err)
	}

	// A sweeper takes the instance over; the old fence performs nothing.
	takeoverAt := bootAt.Add(2 * time.Hour)
	inTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		_, err := lease.Manager{}.Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: f.tenant, Resource: f.fence.Resource, Holder: workerB, Now: takeoverAt, TTL: time.Hour,
		})
		return err
	})
	stale := f.req
	stale.RecordedAt = takeoverAt
	if _, _, err := f.steps(effect, inner).Run(f.fenced(), stale); !errors.Is(err, wfrecover.ErrFenceRefused) || lease.CodeOf(err) != lease.CodeFenceStale {
		t.Fatalf("superseded fence = %v, want FENCE_STALE before the effect", err)
	}
	if effect.performs != 0 {
		t.Fatalf("refused runs performed the effect %d times", effect.performs)
	}

	var current lease.Fence
	inTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		obs, err := lease.Manager{}.Observe(ctx, tx, f.tenant, f.fence.Resource, takeoverAt)
		current = lease.Fence{TenantID: f.tenant, Resource: f.fence.Resource, LeaseID: obs.LeaseID, Holder: workerB, Token: obs.Token}
		return err
	})
	fresh := execute.WithFence(ctx, current.RuntimeFence(time.Time{}))
	failing := &countingStepEffect{node: f.req.Node.ID, fail: errors.New("connector refused")}
	if _, _, err := f.steps(failing, inner).Run(fresh, stale); !errors.Is(err, wfrecover.ErrEffect) {
		t.Fatalf("failed performance = %v, want ErrEffect", err)
	}
	inTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		_, found, err := idempotency.PostgresStore{}.Lookup(ctx, tx, wfrecover.StepEffectRequest(stale).Scope())
		if err == nil && found {
			err = errors.New("a failed performance left an idempotency record")
		}
		return err
	})

	// A record under the activation key that is not a step result is refused
	// rather than advanced on.
	foreign := stale
	foreign.Attempt = 3
	scope := wfrecover.StepEffectRequest(foreign)
	inTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		_, err := idempotency.Guard(ctx, tx, idempotency.PostgresStore{}, scope.Scope(), scope.EffectDigest(), retention, takeoverAt,
			func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
				return idempotency.ResultIdentity{ResultRef: "not-a-step-result"}, nil
			})
		return err
	})
	if _, _, err := f.steps(effect, inner).Run(fresh, foreign); !errors.Is(err, wfrecover.ErrEffect) || effect.performs != 0 {
		t.Fatalf("foreign stored result = %v (performs %d), want ErrEffect without a performance", err, effect.performs)
	}
}
