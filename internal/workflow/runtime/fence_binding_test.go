package runtime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type bindingVerifier struct{ calls int }

func (v *bindingVerifier) VerifyFence(context.Context, runtime.Executor, uuid.UUID, runtime.Fence) error {
	v.calls++
	return errors.New("verified binding; stop before storage")
}

func TestTodo_WF_RUN_041_Integration(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "fence-binding")
	pf := newPromotionFixture(t, values.TenantId("fence-binding"), "intent:fence-binding")
	start := startPromotionInstance(t, conn, tenant, pf.baseStartRequest(tenant, "fence-binding"))
	ctx := context.Background()
	var manager lease.Manager
	for _, resource := range []lease.Resource{
		{Kind: lease.ResourceWorkflowInstance, ID: uuid.NewString()},
		{Kind: lease.ResourceQueue, ID: start.InstanceID.String()},
		{Kind: lease.ResourceWorkflowInstance, ID: start.InstanceID.String()},
	} {
		t.Run(resource.Kind+"/"+resource.ID, func(t *testing.T) {
			var grant lease.Grant
			inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
				var err error
				grant, err = manager.Acquire(ctx, tx, lease.AcquireRequest{TenantID: tenant, Resource: resource, Holder: fencedHolderA, Now: fixedInstant, TTL: time.Minute})
				return err
			})
			sink := runtime.NewMemorySink()
			var statements []string
			err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				recorder := &recordingExecutor{tx: tx}
				_, err := runtime.AdvanceFenced(ctx, recorder, runtime.FencedAdvanceRequest{
					Fence: fenceOf(grant, fixedInstant), Verifier: lease.Fenced{Manager: manager},
					Request: runtime.AdvanceRequest{TenantID: tenant, InstanceID: start.InstanceID, ExpectedInstanceVersion: start.InstanceVersion, Attempt: 1, Plan: pf.Plan,
						Outcome: frontier.NodeOutcome{NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:binding"}, RecordedAt: fixedInstant, Sink: sink},
				})
				statements = recorder.statements()
				return err
			})
			matching := resource.Kind == lease.ResourceWorkflowInstance && resource.ID == start.InstanceID.String()
			if matching {
				if err != nil || len(sink.Records()) == 0 {
					t.Fatalf("matching fence failed to advance: %v records=%d", err, len(sink.Records()))
				}
				return
			}
			if runtime.CodeOf(err) != runtime.CodeFenceRefused || len(statements) != 0 || len(sink.Records()) != 0 {
				t.Fatalf("foreign fence error=%v statements=%v continuations=%v", err, statements, sink.Records())
			}
			inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
				instance, err := (runtime.Store{}).LoadInstance(ctx, tx, tenant, start.InstanceID)
				if err == nil && instance.InstanceVersion != start.InstanceVersion {
					t.Fatalf("foreign fence changed version: %d", instance.InstanceVersion)
				}
				return err
			})
		})
	}
}

func TestTodo_WF_RUN_041(t *testing.T) {
	instance := uuid.New()
	for _, tc := range []struct {
		name, kind, resource, code string
		calls                      int
	}{
		{"matching", "WORKFLOW_INSTANCE", instance.String(), runtime.CodeFenceRefused, 1},
		{"foreign instance", "WORKFLOW_INSTANCE", uuid.NewString(), runtime.CodeFenceRefused, 0},
		{"queue with matching id", "QUEUE", instance.String(), runtime.CodeFenceRefused, 0},
		{"missing resource", "WORKFLOW_INSTANCE", "", runtime.CodeFenceRequired, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifier := &bindingVerifier{}
			_, err := runtime.AdvanceFenced(context.Background(), panicExecutor{t: t}, runtime.FencedAdvanceRequest{
				Fence:    runtime.Fence{ResourceKind: tc.kind, ResourceID: tc.resource, LeaseID: uuid.New(), HolderID: "workload:test#replica:1", Token: 1, At: fixedInstant},
				Verifier: verifier,
				Request:  runtime.AdvanceRequest{TenantID: uuid.New(), InstanceID: instance},
			})
			if runtime.CodeOf(err) != tc.code || verifier.calls != tc.calls {
				t.Fatalf("error=%v verifier calls=%d; want code=%s calls=%d", err, verifier.calls, tc.code, tc.calls)
			}
		})
	}
}
