package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func TestTodo_WTIME_MissingPunchExecutorComposition(t *testing.T) {
	if requestIdentity("tenant-a", "same") != requestIdentity("tenant-a", "same") {
		t.Fatal("request identity is not deterministic")
	}
	if requestIdentity("tenant-a", "same") == requestIdentity("tenant-b", "same") {
		t.Fatal("request identity is not tenant scoped")
	}
	advance := runtime.AdvanceReceipt{NodeID: "supervisor_approval", Attempt: 1}
	if got, ok := findAdvance([]runtime.AdvanceReceipt{{NodeID: "other"}, advance}, "supervisor_approval"); !ok || got.NodeID != advance.NodeID {
		t.Fatalf("findAdvance = %+v/%v", got, ok)
	}
	if _, ok := findAdvance(nil, "missing"); ok {
		t.Fatal("findAdvance found a missing node")
	}
	inputs := missingPunchInputs(clockservice.MissingPunchWorkflowRequest{SessionID: "s", OriginalObservationID: "o", ExpectedRevision: 7, ClaimedOutAt: time.Unix(8, 0).UTC(), Reason: "forgot"}, "workflow-instance")
	if len(inputs) != 6 || inputs[0].Path != "session_id" || inputs[2].Value.Text != "7" || inputs[5].Value.Text != "workflow-instance" {
		t.Fatalf("workflow inputs = %+v", inputs)
	}
}

func TestTodo_WTIME_MissingPunchExecutorRejectsIncompleteComposition(t *testing.T) {
	if _, err := NewMissingPunchWorkflowExecutor(MissingPunchWorkflowExecutorOptions{}); err == nil {
		t.Fatal("incomplete composition was accepted")
	}
	if err := (&MissingPunchWorkflowExecutor{}).validateRequest(clockservice.MissingPunchWorkflowRequest{}); err == nil {
		t.Fatal("empty request was accepted")
	}
}

func TestTodo_WTIME_MissingPunchExecutorRejectsUnknownAction(t *testing.T) {
	e := &MissingPunchWorkflowExecutor{
		driver: &execute.Driver{},
		tenant: func(string) (uuid.UUID, error) { return uuid.New(), nil },
		clock:  func() time.Time { return time.Unix(10, 0).UTC() },
	}
	_, err := e.ExecuteMissingPunch(context.Background(), clockservice.MissingPunchWorkflowRequest{Action: "NOPE", TenantID: "tenant", IdempotencyKey: "key", At: time.Unix(10, 0)})
	if err == nil || !errors.Is(err, errMissingPunchInvalidAction) {
		t.Fatalf("unknown action error = %v", err)
	}
}

// Compile-time checks keep the test doubles below aligned with the public
// composition boundary without attempting to fake the workflow engine.
var _ execute.StepRunner = nil
var _ runtime.WorkflowResolver = nil
var _ version.Store = nil
var _ workflow.ExecutionMode = workflow.ModeExecute
var _ frontier.NodeOutcome
