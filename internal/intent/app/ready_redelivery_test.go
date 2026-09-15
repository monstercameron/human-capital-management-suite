package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// plainExecutor predates WF-RUN-003 and cannot redeliver; redeliveryExecutor
// can, and records whether a redelivery ever reached it.
type plainExecutor struct{}

func (plainExecutor) Execute(context.Context, runtime.StartRequest) (ExecutionResult, error) {
	return ExecutionResult{}, errors.New("not used")
}

func (plainExecutor) Resume(context.Context, ExecutionResumeRequest) (ExecutionResult, error) {
	return ExecutionResult{}, errors.New("not used")
}

func (plainExecutor) ResumeTimer(context.Context, ExecutionTimerResumeRequest) (ExecutionResult, error) {
	return ExecutionResult{}, errors.New("not used")
}

type redeliveryExecutor struct {
	plainExecutor
	calls *int
}

func (e redeliveryExecutor) RedeliverReady(context.Context, ExecutionRedeliveryRequest) (ExecutionResult, error) {
	*e.calls++
	return ExecutionResult{}, nil
}

// refusingBeginner is shared with signal_resume_test.go.

type stubResolver struct{}

func (stubResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return runtime.WorkflowSelection{}, errors.New("not used")
}

// TestCellRedeliverReadyRefusesBeforeTouchingTheExecutor walks every guard in
// front of a redelivery: a cell without an execution journey, an executor
// that cannot redeliver, a malformed instance or version, a missing or nil
// tenant, and an unreachable database all refuse before the executor runs.
func TestCellRedeliverReadyRefusesBeforeTouchingTheExecutor(t *testing.T) {
	calls := 0
	tenantCtx := WithResumeTenant(context.Background(), "tenant-a")
	instance := uuid.NewString()
	service := func(executor ProposalExecutor, tenant uuid.UUID) *IntentService {
		return &IntentService{
			executor: executor, executionResolver: stubResolver{},
			executionVersions: workflowversion.NewRegistry(),
			tenantUUID:        func(values.TenantId) uuid.UUID { return tenant },
		}
	}
	redeliverer := redeliveryExecutor{calls: &calls}
	journey := &journeyEngine{db: refusingBeginner{}}

	for _, tc := range []struct {
		name     string
		cell     *Cell
		ctx      context.Context
		instance string
		version  int64
		want     string
	}{
		{"nil cell", nil, tenantCtx, instance, 1, "no intent service"},
		{"no journey", &Cell{Service: service(redeliverer, uuid.New())}, tenantCtx, instance, 1, "no execution journey"},
		{"executor cannot redeliver", &Cell{Service: service(plainExecutor{}, uuid.New()), Journey: journey}, tenantCtx, instance, 1, "incomplete execution authority"},
		{"no version", &Cell{Service: service(redeliverer, uuid.New()), Journey: journey}, tenantCtx, instance, 0, "positive expected instance version"},
		{"bad instance", &Cell{Service: service(redeliverer, uuid.New()), Journey: journey}, tenantCtx, "not-a-uuid", 1, "instance id"},
		{"no tenant", &Cell{Service: service(redeliverer, uuid.New()), Journey: journey}, context.Background(), instance, 1, "explicit tenant"},
		{"nil tenant", &Cell{Service: service(redeliverer, uuid.Nil), Journey: journey}, tenantCtx, instance, 1, "nil tenant"},
		{"database down", &Cell{Service: service(redeliverer, uuid.New()), Journey: journey}, tenantCtx, instance, 1, "begin ready redelivery"},
	} {
		_, err := tc.cell.RedeliverReady(tc.ctx, tc.instance, tc.version)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: RedeliverReady = %v, want %q", tc.name, err, tc.want)
		}
	}
	if calls != 0 {
		t.Fatalf("a refused redelivery reached the executor %d times", calls)
	}
}
