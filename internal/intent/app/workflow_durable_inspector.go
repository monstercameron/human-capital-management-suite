package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// WorkflowInstanceInspector loads and projects the complete durable record
// behind one workflow instance. Callers provide the authorization decisions
// already evaluated for this request; inspect.Load refuses a non-disclosable
// decision before its first table read.
type WorkflowInstanceInspector interface {
	InspectWorkflowInstance(context.Context, values.TenantId, uuid.UUID, inspect.Authorization, inspect.WorkItemAuthorization) (inspect.DurableView, error)
}

type workflowInstanceInspector struct {
	db         dbport.Beginner
	tenantUUID func(values.TenantId) uuid.UUID
	versions   version.Store
}

var ErrWorkflowInstanceInspectorUnconfigured = errors.New("app: the workflow instance inspector is not configured")

// NewWorkflowInstanceInspector builds the app adapter for inspect.Load. A
// nil database or tenant resolver fails closed, as does an unavailable
// version registry (which the durable manifest then identifies explicitly).
func NewWorkflowInstanceInspector(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID, versions version.Store) WorkflowInstanceInspector {
	return workflowInstanceInspector{db: db, tenantUUID: tenantUUID, versions: versions}
}

func (r workflowInstanceInspector) InspectWorkflowInstance(
	ctx context.Context, tenant values.TenantId, instanceID uuid.UUID,
	authorization inspect.Authorization, workItems inspect.WorkItemAuthorization,
) (inspect.DurableView, error) {
	if r.db == nil || r.tenantUUID == nil {
		return inspect.DurableView{}, ErrWorkflowInstanceInspectorUnconfigured
	}
	tenantID := r.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return inspect.DurableView{}, fmt.Errorf("app: workflow instance inspection: tenant %q maps to no id", tenant)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return inspect.DurableView{}, fmt.Errorf("app: workflow instance inspection: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return inspect.DurableView{}, fmt.Errorf("app: workflow instance inspection: scope tenant: %w", err)
	}
	view, err := inspect.Load(ctx, tx, inspect.LoadRequest{
		TenantID: tenantID, InstanceID: instanceID, Authorization: authorization,
		WorkItems: workItems, Versions: r.versions,
	})
	if err != nil {
		return inspect.DurableView{}, fmt.Errorf("app: workflow instance inspection: durable load: %w", err)
	}
	return view, nil
}
