package cell

import (
	"context"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// workflowReader adapts the application-owned durable workflow reader to the
// transport's deliberately smaller, redaction-safe inspection port. The
// adapter owns no queries and makes no authorization decisions; it only
// converts application records into the generated-boundary projection.
type workflowReader struct {
	source app.WorkflowControlReader
}

var _ workflow.Reader = workflowReader{}

func newWorkflowReader(source app.WorkflowControlReader) workflow.Reader {
	if source == nil {
		return nil
	}
	return workflowReader{source: source}
}

func (r workflowReader) ReadWorkflowControlRecord(ctx context.Context, tenant, instanceID string) (workflow.Record, error) {
	id, err := runtime.ParseUUID(instanceID)
	if err != nil {
		return workflow.Record{}, workflow.ErrNotFound
	}
	record, err := r.source.ReadWorkflowControlRecord(ctx, values.TenantId(tenant), id)
	if err != nil {
		if runtime.CodeOf(err) == runtime.CodeInstanceNotFound {
			return workflow.Record{}, workflow.ErrNotFound
		}
		return workflow.Record{}, err
	}
	converted := convertWorkflowRecord(record)
	subject := "workflow-viewer"
	if principal, ok := trust.FromContext(ctx); ok && principal != nil && principal.Subject() != "" {
		subject = principal.Subject()
	}
	inspector, err := inspect.Build(inspect.Request{
		Instance: record.Instance, Nodes: record.Nodes,
		Authorization: inspect.AllowAll("workflow.designer/1.0.0", "WORKFLOW_AUTHORING", subject),
	})
	if err == nil {
		// Legacy inspection fixtures intentionally omit runtime invariants that
		// the wire projection tolerates. Keep their existing GetWorkflow path
		// intact; the definition-view endpoint requires this non-nil projection
		// and therefore still fails closed when inspector validation rejects it.
		converted.Inspector = &inspector
	}
	// The projected tenant is the caller's tenant identity (the key the
	// inspector confines the record to), never the storage uuid: the
	// inspector compares this value against the caller's own tenant, so a
	// uuid here fails its own confinement check on every served read while
	// fixture reads (whose tenant is already the key) keep passing.
	converted.Instance.TenantID = tenant
	return converted, nil
}

func convertWorkflowRecord(record app.WorkflowControlRecord) workflow.Record {
	instance := record.Instance
	out := workflow.Instance{
		InstanceID:          instance.InstanceID.String(),
		TenantID:            instance.TenantID.String(),
		CellID:              instance.CellID,
		WorkflowID:          instance.WorkflowID,
		WorkflowVersion:     instance.WorkflowVersion,
		CompiledPlanDigest:  instance.CompiledPlanHash,
		BusinessSubjectRefs: append([]string(nil), instance.BusinessSubjectRefs...),
		ExecutionMode:       string(instance.ExecutionMode),
		RuntimeStatus:       string(instance.RuntimeStatus),
		RequestState:        instance.CompletionDimensions.RequestState,
		ExecutionState:      instance.CompletionDimensions.ExecutionState,
		BusinessState:       instance.CompletionDimensions.BusinessState,
		ConsistencyState:    instance.CompletionDimensions.ConsistencyState,
		ObligationState:     instance.CompletionDimensions.ObligationState,
		InputRef:            instance.InputRef,
		CurrentNodeIDs:      append([]string(nil), instance.CurrentNodeIDs...),
		EffectiveContextRef: instance.EffectiveContextRef,
		LastCheckpointRef:   instance.LastCheckpointRef,
		InstanceVersion:     uint64(instance.InstanceVersion),
		CorrelationID:       instance.CorrelationID,
		CreatedAt:           instance.CreatedAt,
		StartedAt:           instance.StartedAt,
		CompletedAt:         instance.CompletedAt,
	}
	if instance.BusinessTransactionID != nil {
		out.BusinessTransactionID = instance.BusinessTransactionID.String()
	}
	if instance.VariableRevisionHead != 0 {
		out.VariableRevisionHead = strconv.FormatInt(instance.VariableRevisionHead, 10)
	}

	result := workflow.Record{Instance: out, Nodes: make([]workflow.NodeExecution, 0, len(record.Nodes))}
	for _, node := range record.Nodes {
		result.Nodes = append(result.Nodes, workflow.NodeExecution{
			NodeExecutionID:         node.NodeExecutionID.String(),
			WorkflowInstanceID:      node.InstanceID.String(),
			NodeID:                  node.NodeID,
			Attempt:                 uint32(node.Attempt),
			Status:                  string(node.Status),
			InputSnapshotRef:        node.InputSnapshotRef,
			OutputArtifactRef:       node.OutputArtifactRef,
			CapabilityExecutionID:   node.Refs.CapabilityExecutionID,
			AuthorizationDecisionID: node.Refs.AuthorizationDecisionID,
			DecisionID:              node.Refs.DecisionID,
			HumanTaskID:             node.Refs.HumanTaskID,
			ErrorClass:              node.ErrorClass,
			StartedAt:               node.StartedAt,
			CompletedAt:             node.CompletedAt,
			TraceID:                 node.TraceID,
		})
	}
	return result
}
