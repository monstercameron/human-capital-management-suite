package clockservice

import (
	"context"
	"github.com/google/uuid"
)

// WorkflowBatchExecutor owns the synchronous first workflow node for a
// batch. It must durably commit accepted observations before returning them
// as accepted receipts; the application service does not write around it.
type WorkflowBatchExecutor interface {
	ExecutePunchBatch(context.Context, string, string, []BatchCommitEntry) (BatchCommitResult, error)
}

// WorkflowReceiptBinding proves which durable workflow instance accepted one
// prepared punch. Sequence and observation/session identifiers are immutable
// correlations and must be returned by the workflow transaction.
type WorkflowReceiptBinding struct {
	TenantID        string
	DeviceSequence  int64
	ObservationID   string
	SessionID       string
	InstanceID      string
	WorkflowID      string
	PlanDigest      string
	StartKey        string
	Committed       bool
	NodeID          string
	Attempt         int
	InstanceVersion int64
	TraceID         string
}

// WorkflowReceiptLookup retrieves durable workflow evidence for an exact
// replay without re-executing the workflow.
type WorkflowReceiptLookup interface {
	LookupWorkflowReceipt(context.Context, string, string, int64) (WorkflowReceiptBinding, bool, error)
}

func validWorkflowBinding(binding WorkflowReceiptBinding, tenant string) bool {
	_, uuidErr := uuid.Parse(binding.InstanceID)
	return binding.TenantID == tenant && binding.DeviceSequence > 0 && binding.ObservationID != "" && binding.SessionID != "" && uuidErr == nil && binding.WorkflowID != "" && binding.PlanDigest != "" && binding.StartKey != "" && binding.Committed && binding.NodeID != "" && binding.Attempt > 0 && binding.InstanceVersion > 0 && binding.TraceID != ""
}
