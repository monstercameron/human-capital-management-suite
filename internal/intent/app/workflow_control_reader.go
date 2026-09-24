package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// The typed runtime/work-item record used by workflow-control and journey
// stage derivation. The operator-facing admin inspector is separate:
// WorkflowInstanceInspector in workflow_durable_inspector.go calls
// inspect.Load so the user-facing inspection is backed by the full durable
// traversal and its manifest.

// WorkflowControlRecord is the runtime/work-item state needed by
// workflow-control and the journey's business-stage projection. It carries
// store types unprojected and is not the operator inspector response.
type WorkflowControlRecord struct {
	Instance  runtime.Instance
	Nodes     []runtime.NodeExecution
	WorkItems []workitem.WorkItem
	// Transitions is keyed by the work item id's string form, in the order
	// the store returns them.
	Transitions map[string][]workitem.TransitionRecord
}

// WorkflowControlReader loads one instance's control projection for a caller
// acting in one tenant.
//
// An instance that does not exist, or is not this tenant's, is reported with
// the runtime store's own [runtime.CodeInstanceNotFound] refusal (readable
// through runtime.CodeOf), so a transport can project NOT_FOUND without this
// package inventing a second sentinel for the same fact.
type WorkflowControlReader interface {
	ReadWorkflowControlRecord(ctx context.Context, tenant values.TenantId, instanceID uuid.UUID) (WorkflowControlRecord, error)
}

// workflowControlReader implements [WorkflowControlReader] over a pool.
type workflowControlReader struct {
	db         dbport.Beginner
	tenantUUID func(values.TenantId) uuid.UUID
}

// NewWorkflowControlReader builds the control reader over the pool the
// workflow runtime writes through
// (internal/data/pgxadapter.Pool in every real composition) and tenantUUID
// the same tenant-key-to-uuid mapping [CellConfig.TenantUUID] carries.
// Either nil yields a reader that refuses every read, which is what an
// unconfigured port should do rather than answering from nowhere.
func NewWorkflowControlReader(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) WorkflowControlReader {
	return workflowControlReader{db: db, tenantUUID: tenantUUID}
}

// ErrWorkflowControlReaderUnconfigured reports a reader built with no
// database or no tenant mapping.
var ErrWorkflowControlReaderUnconfigured = errors.New("app: the workflow control reader is not configured")

// ReadWorkflowControlRecord implements [WorkflowControlReader].
//
// The read runs inside one tenant-scoped transaction that is always rolled
// back: every table it touches is row-level-security protected, so the tenant
// has to be established on the session (internal/data/tenancy.WithTenant)
// before the first SELECT, and a read that committed would be claiming to
// have changed something.
func (r workflowControlReader) ReadWorkflowControlRecord(
	ctx context.Context, tenant values.TenantId, instanceID uuid.UUID,
) (WorkflowControlRecord, error) {
	if r.db == nil || r.tenantUUID == nil {
		return WorkflowControlRecord{}, ErrWorkflowControlReaderUnconfigured
	}
	tenantID := r.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return WorkflowControlRecord{}, fmt.Errorf("app: workflow control read: tenant %q maps to no id", tenant)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return WorkflowControlRecord{}, fmt.Errorf("app: workflow control read: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return WorkflowControlRecord{}, fmt.Errorf("app: workflow control read: scope tenant: %w", err)
	}
	return loadWorkflowControlRecord(ctx, tx, tenantID, instanceID)
}

// loadWorkflowControlRecord performs the four reads over an executor the
// caller has already scoped to the tenant. Store refusals are wrapped, never
// replaced, so runtime.CodeOf still reads them.
func loadWorkflowControlRecord(
	ctx context.Context, ex workitem.Executor, tenantID, instanceID uuid.UUID,
) (WorkflowControlRecord, error) {
	runtimeStore, itemStore := runtime.Store{}, workitem.Store{}

	instance, err := runtimeStore.LoadInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return WorkflowControlRecord{}, fmt.Errorf("load the workflow instance: %w", err)
	}
	nodes, err := runtimeStore.LoadNodeExecutions(ctx, ex, tenantID, instanceID)
	if err != nil {
		return WorkflowControlRecord{}, fmt.Errorf("load the node executions: %w", err)
	}
	items, err := itemStore.ListForInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return WorkflowControlRecord{}, fmt.Errorf("list the work items: %w", err)
	}
	transitions := make(map[string][]workitem.TransitionRecord, len(items))
	for _, item := range items {
		rows, err := itemStore.LoadTransitions(ctx, ex, tenantID, item.WorkItemID)
		if err != nil {
			return WorkflowControlRecord{}, fmt.Errorf("load work item transitions: %w", err)
		}
		transitions[item.WorkItemID.String()] = rows
	}
	return WorkflowControlRecord{
		Instance: instance, Nodes: nodes, WorkItems: items, Transitions: transitions,
	}, nil
}
