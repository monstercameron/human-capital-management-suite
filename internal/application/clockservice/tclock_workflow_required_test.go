package clockservice

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type regWorkflowExecutor struct {
	work   *regWork
	fail   error
	called *int
}

func (e regWorkflowExecutor) ExecutePunch(ctx context.Context, tenant string, work PunchWork) (WorkflowPunchResult, error) {
	if e.called != nil {
		*e.called++
	}
	if e.fail != nil {
		return WorkflowPunchResult{}, e.fail
	}
	result, err := e.work.Punch(ctx, tenant, work)
	if err != nil {
		return WorkflowPunchResult{}, err
	}
	return WorkflowPunchResult{PunchResult: result, InstanceID: uuid.MustParse("6e280766-6a9a-4da8-90ce-9802eb3d6945"), WorkflowID: "published-clock", PlanDigest: "sha256:clock-plan", StartKey: work.Observation.IdempotencyKey, TraceID: "durable-test-trace", NodeID: "commit_punch", Attempt: 1, InstanceVersion: 2, Committed: true}, nil
}

func TestTodo_WTIME004_ClockInRequiresWorkflowBeforeAnyWrite(t *testing.T) {
	for _, tc := range []struct {
		name        string
		unavailable bool
		failure     error
	}{
		{name: "unconfigured", unavailable: true},
		{name: "workflow failure", failure: errors.New("workflow unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newRegWork()
			service := regService(&regWorkers{active: true, assign: true}, regAuth{}, store)
			if tc.unavailable {
				service.PunchWorkflow = nil
			} else {
				service.PunchWorkflow = regWorkflowExecutor{work: store, fail: tc.failure}
			}
			_, err := service.ClockIn(context.Background(), regPrincipal(t, regWorker), regPunch("required-workflow", regNow))
			if err == nil {
				t.Fatal("clock-in accepted without successful workflow")
			}
			if store.workCalls != 0 || len(store.observations) != 0 || len(store.records) != 0 {
				t.Fatal("workflow failure left a punch write")
			}
		})
	}
}

func TestTodo_WTIME004_ClockInReturnsOnlyWorkflowCommittedResult(t *testing.T) {
	store := newRegWork()
	service := regService(&regWorkers{active: true, assign: true}, regAuth{}, store)
	calls := 0
	service.PunchWorkflow = regWorkflowExecutor{work: store, called: &calls}
	receipt, err := service.ClockIn(context.Background(), regPrincipal(t, regWorker), regPunch("workflow-success", regNow))
	if err != nil || receipt.Status != ReceiptAccepted || calls != 1 || store.workCalls != 1 || receipt.WorkflowInstanceRef == "" || receipt.WorkflowID != "published-clock" || receipt.WorkflowPlanDigest != "sha256:clock-plan" || receipt.WorkflowStartKey != "workflow-success" || receipt.WorkflowTraceID != "durable-test-trace" || receipt.WorkflowNodeID != "commit_punch" || receipt.WorkflowAttempt != 1 || receipt.WorkflowInstanceVersion != 2 {
		t.Fatalf("receipt=%+v error=%v workflow calls=%d commits=%d", receipt, err, calls, store.workCalls)
	}
}
