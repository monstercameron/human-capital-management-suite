package cell

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
	kernelworkflow "github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type fakeWorkflowReader struct {
	record app.WorkflowControlRecord
}

func (f fakeWorkflowReader) ReadWorkflowControlRecord(context.Context, values.TenantId, uuid.UUID) (app.WorkflowControlRecord, error) {
	return f.record, nil
}

func TestTodo_EP_WF_001_Integration(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	instanceID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	started := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	source := fakeWorkflowReader{record: app.WorkflowControlRecord{
		Instance: runtime.Instance{
			TenantID: tenant, InstanceID: instanceID, CellID: "cell-a",
			WorkflowID: "promotion", WorkflowVersion: 3, CompiledPlanHash: "plan-digest",
			ExecutionMode: kernelworkflow.ModeExecute, RuntimeStatus: runtime.InstanceRunning,
			CompletionDimensions: runtime.Dimensions{RequestState: "APPROVED", ExecutionState: "RUNNING"},
			InputRef:             "input-ref", VariableRevisionHead: 4, CurrentNodeIDs: []string{"approve"},
			CorrelationID: "corr-1", CreatedAt: started, StartedAt: &started, InstanceVersion: 9,
		},
		Nodes: []runtime.NodeExecution{{
			TenantID: tenant, NodeExecutionID: runtime.NodeExecutionID(tenant, instanceID, "approve", 1),
			InstanceID: instanceID, NodeID: "approve", Attempt: 1, StepType: kernelworkflow.StepApproval,
			Status: runtime.NodeSucceeded, InputSnapshotRef: "snapshot-ref", OutputArtifactRef: "artifact-ref",
			Refs:    runtime.GovernanceRefs{AuthorizationDecisionID: "authz-ref", DecisionID: "decision-ref"},
			TraceID: "trace-ref", StartedAt: &started, CompletedAt: &started,
		}},
	}}

	reader := newWorkflowReader(source)
	if reader == nil {
		t.Fatal("newWorkflowReader returned nil for a configured source")
	}
	// The caller addresses the record by tenant key, and the projection
	// carries that key back: the inspector confines the record against the
	// caller's own tenant, so projecting the storage uuid would fail its
	// own check on every served read.
	record, err := reader.ReadWorkflowControlRecord(t.Context(), "tenant-key-a", instanceID.String())
	if err != nil {
		t.Fatalf("ReadWorkflowControlRecord: %v", err)
	}
	if record.Instance.InstanceID != instanceID.String() || record.Instance.TenantID != "tenant-key-a" || record.Instance.InstanceVersion != 9 {
		t.Fatalf("instance projection = %+v", record.Instance)
	}
	if got := record.Instance.VariableRevisionHead; got != "4" {
		t.Fatalf("variable revision head = %q, want 4", got)
	}
	if len(record.Nodes) != 1 || record.Nodes[0].AuthorizationDecisionID != "authz-ref" || record.Nodes[0].Status != "SUCCEEDED" {
		t.Fatalf("node projection = %+v", record.Nodes)
	}
}

func TestTodo_EP_WF_001_Security(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	instanceID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	reader := newWorkflowReader(fakeWorkflowReader{record: app.WorkflowControlRecord{Instance: runtime.Instance{TenantID: tenant, InstanceID: instanceID}}})
	_, err := reader.ReadWorkflowControlRecord(t.Context(), tenant.String(), "not-a-uuid")
	if err != transportworkflow.ErrNotFound {
		t.Fatalf("malformed instance id error = %v, want non-disclosing ErrNotFound", err)
	}
}
