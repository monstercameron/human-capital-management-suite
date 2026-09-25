package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/workorder"
)

func TestCandidateWorkOrderRegistrationIsPinnedButNotAdmitted(t *testing.T) {
	const digest = "sha256:published-template"
	reg, err := CandidateWorkOrderRegistration(WorkOrderRegistrationSpec{
		Template: workorder.DefaultTemplate(),
		Pin:      workorder.TemplatePin{TemplateID: "IRONRIDGE_FIELD_WORK", Version: "1.0.0", Digest: digest},
		Match:    func(runtime.StartRequest) bool { return true },
	})
	if err != nil {
		t.Fatalf("CandidateWorkOrderRegistration: %v", err)
	}
	if reg.Plan == nil || reg.Pin.CompiledPlanDigest != reg.Plan.Digest() {
		t.Fatalf("plan/pin = %v / %v, want the exact compiled plan digest", reg.Plan, reg.Pin)
	}
	if len(reg.Registration.AdmittedIntentTypes) != 0 {
		t.Fatalf("candidate admits intents without signed authority: %v", reg.Registration.AdmittedIntentTypes)
	}
	intent := runtime.StartRequest{Source: &runtime.StartSource{
		Kind: runtime.StartSourceProposal, IntentType: "WORK_ORDER_EXECUTE", Proposal: &runtime.ProposalBinding{},
	}}
	if reg.Match == nil || !reg.Match(intent) {
		t.Fatal("candidate should match a proposal of its workflow intent when its current-authority matcher accepts")
	}
	intent.Source.IntentType = "hcmnext.people.promote_worker/v1"
	if reg.Match(intent) {
		t.Fatal("candidate matched a different intent type")
	}
	intent.Source.IntentType = "WORK_ORDER_EXECUTE"
	intent.PinnedCompiledPlanDigest = "sha256:another-plan"
	if reg.Match(intent) {
		t.Fatal("candidate matched a continuation pinned to a different plan")
	}
	intent.PinnedCompiledPlanDigest = ""
	intent.Source.Proposal = nil
	if reg.Match(intent) {
		t.Fatal("candidate matched a proposal source without a bound proposal")
	}

	handler := reg.Registration.StepHandlers[workorder.CapabilitySubmitRequest]
	_, _, err = handler.Run(context.Background(), nil, execute.StepRequest{})
	if err == nil {
		t.Fatal("unbound work-order capability succeeded without an authorized domain adapter")
	}
}

type workOrderRuntimeReaderFake struct {
	instance runtime.Instance
	nodes    []runtime.NodeExecution
	instErr  error
	nodeErr  error
}

type workOrderNodeBindingFake bool

func (f workOrderNodeBindingFake) BindsWorkOrderRevision(_ context.Context, _ runtime.NodeExecution, _ WorkOrderRuntimePin) (bool, error) {
	return bool(f), nil
}

func (f workOrderRuntimeReaderFake) LoadInstance(context.Context, runtime.Executor, uuid.UUID, uuid.UUID) (runtime.Instance, error) {
	return f.instance, f.instErr
}

func (f workOrderRuntimeReaderFake) LoadNodeExecutions(context.Context, runtime.Executor, uuid.UUID, uuid.UUID) ([]runtime.NodeExecution, error) {
	return f.nodes, f.nodeErr
}

func TestRequireWorkOrderNodeCompletedChecksExactRuntimePin(t *testing.T) {
	tenant, instanceID := uuid.New(), uuid.New()
	completedAt := time.Now().UTC()
	pin := WorkOrderRuntimePin{
		TenantID: tenant, InstanceID: instanceID, WorkOrderID: "wo-riverside-repair", OrderRevision: 12,
		WorkflowID: "hcmnext.workflows.work_order.ironridge_field_work", WorkflowVersion: 1, CompiledPlanHash: "sha256:compiled-plan",
	}
	reader := workOrderRuntimeReaderFake{
		instance: runtime.Instance{TenantID: tenant, InstanceID: instanceID, WorkflowID: pin.WorkflowID,
			WorkflowVersion: 1, CompiledPlanHash: pin.CompiledPlanHash},
		nodes: []runtime.NodeExecution{{TenantID: tenant, InstanceID: instanceID, NodeID: "approve_budget",
			Status: runtime.NodeSucceeded, CompletedAt: &completedAt}},
	}
	if err := RequireWorkOrderNodeCompleted(context.Background(), reader, workOrderNodeBindingFake(true), emptyRuntimeExecutor{}, pin, "approve_budget"); err != nil {
		t.Fatalf("RequireWorkOrderNodeCompleted: %v", err)
	}

	wrongPin := pin
	wrongPin.CompiledPlanHash = "sha256:substituted-plan"
	if err := RequireWorkOrderNodeCompleted(context.Background(), reader, workOrderNodeBindingFake(true), emptyRuntimeExecutor{}, wrongPin, "approve_budget"); !errors.Is(err, ErrWorkOrderCompletionProof) {
		t.Fatalf("wrong compiled plan proof error = %v, want ErrWorkOrderCompletionProof", err)
	}
	if err := RequireWorkOrderNodeCompleted(context.Background(), reader, workOrderNodeBindingFake(true), emptyRuntimeExecutor{}, pin, "release_work"); !errors.Is(err, ErrWorkOrderCompletionProof) {
		t.Fatalf("missing node proof error = %v, want ErrWorkOrderCompletionProof", err)
	}
	failed := reader
	failed.nodes = []runtime.NodeExecution{{TenantID: tenant, InstanceID: instanceID, NodeID: "approve_budget",
		Status: runtime.NodeFailed, CompletedAt: &completedAt}}
	if err := RequireWorkOrderNodeCompleted(context.Background(), failed, workOrderNodeBindingFake(true), emptyRuntimeExecutor{}, pin, "approve_budget"); !errors.Is(err, ErrWorkOrderCompletionProof) {
		t.Fatalf("failed-node proof error = %v, want ErrWorkOrderCompletionProof", err)
	}
	if err := RequireWorkOrderNodeCompleted(context.Background(), reader, nil, emptyRuntimeExecutor{}, pin, "approve_budget"); !errors.Is(err, ErrWorkOrderCompletionProof) {
		t.Fatalf("missing reader error = %v, want ErrWorkOrderCompletionProof", err)
	}
	if err := RequireWorkOrderNodeCompleted(context.Background(), reader, workOrderNodeBindingFake(false), emptyRuntimeExecutor{}, pin, "approve_budget"); !errors.Is(err, ErrWorkOrderCompletionProof) {
		t.Fatalf("unbound order revision error = %v, want ErrWorkOrderCompletionProof", err)
	}
}

type emptyRuntimeExecutor struct{}

func (emptyRuntimeExecutor) Exec(context.Context, string, ...any) (int64, error) { return 0, nil }
func (emptyRuntimeExecutor) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (emptyRuntimeExecutor) QueryRow(context.Context, string, ...any) dbport.Row { return nil }
