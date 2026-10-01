package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

type missingPunchApprovalAuthorityStub struct{}

func (missingPunchApprovalAuthorityStub) Recheck(context.Context, workitem.Executor, execute.CurrentApprovalAuthorityRequest) (execute.CurrentApprovalAuthorityDecision, error) {
	return execute.CurrentApprovalAuthorityDecision{Allowed: true, DecisionRef: "authority:stub"}, nil
}

type missingPunchApprovalLoaderStub struct{}

func (missingPunchApprovalLoaderStub) LoadMissingPunchApproval(context.Context, clockservice.MissingPunchWorkflowRequest) (MissingPunchApprovalBinding, error) {
	return MissingPunchApprovalBinding{}, errors.New("loader should not be called")
}

func TestTodo_TCLOCK011_DurableApprovalStoreRequiresConcreteComposition(t *testing.T) {
	clock := func() time.Time { return time.Unix(100, 0).UTC() }
	if _, err := NewDurableMissingPunchApprovalStore(nil, missingPunchApprovalLoaderStub{}, missingPunchApprovalAuthorityStub{}, clock); err == nil {
		t.Fatal("nil driver was accepted")
	}
	if _, err := NewDurableMissingPunchApprovalStore(&execute.Driver{}, nil, missingPunchApprovalAuthorityStub{}, clock); err == nil {
		t.Fatal("nil durable loader was accepted")
	}
	if _, err := NewDurableMissingPunchApprovalStore(&execute.Driver{}, missingPunchApprovalLoaderStub{}, nil, clock); err == nil {
		t.Fatal("nil current authority was accepted")
	}
	if _, err := NewDurableMissingPunchApprovalStoreFactory(nil, missingPunchApprovalLoaderStub{}, missingPunchApprovalAuthorityStub{}, clock); err == nil {
		t.Fatal("nil request-scoped driver factory was accepted")
	}
}

func TestTodo_TCLOCK011_DurableApprovalStoreRejectsCallerAsBinding(t *testing.T) {
	store := &DurableMissingPunchApprovalStore{Driver: &execute.Driver{}, Loader: missingPunchApprovalLoaderStub{}, Authority: missingPunchApprovalAuthorityStub{}, Clock: func() time.Time { return time.Unix(100, 0).UTC() }}
	_, err := store.CompleteApproval(context.Background(), clockservice.MissingPunchWorkflowRequest{Action: "REQUEST", TenantID: "tenant", RequestID: "request", Actor: "supervisor", At: time.Unix(100, 0).UTC()})
	if !errors.Is(err, errMissingPunchApprovalBinding) {
		t.Fatalf("invalid action error=%v, want binding refusal", err)
	}
}
