package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The human-work read side as an application port.
//
// WorkService.ListWorkItems and GetWorkItem (EP-WORK-001) read the work_item
// store the same way the workflow-inspection port does: an application-owned
// reader over the pool, because package-dependency-policy forbids transport
// packages importing the data layer. The reader answers with the store's own
// types unprojected; membership, visibility classification and permitted
// actions are the workitem package's read rules (view.go), applied by the
// transport against the caller's principal.

// WorkItemQueueReader loads queue and item records for a caller acting in
// one tenant. An item that does not exist, or is not this tenant's, is
// reported with the store's own [workitem.CodeWorkItemNotFound] refusal
// (readable through workitem.CodeOf), so a transport projects NOT_FOUND
// without this package inventing a second sentinel for the same fact.
type WorkItemQueueReader interface {
	// ListWorkItemQueue is the coarse, already membership-filtered queue:
	// every live item the named principal may act on, in the store's stable
	// deadline/identity order.
	ListWorkItemQueue(ctx context.Context, tenant values.TenantId, principalRef string, now time.Time) ([]workitem.WorkItem, error)
	// ReadWorkItem loads one item for the detail endpoint. Membership is
	// not applied here -- the transport decides visibility after loading,
	// because an invisible item and an absent item project identically.
	ReadWorkItem(ctx context.Context, tenant values.TenantId, workItemID uuid.UUID) (workitem.WorkItem, error)
}

// workItemQueueReader implements [WorkItemQueueReader] over a pool.
type workItemQueueReader struct {
	db         dbport.Beginner
	tenantUUID func(values.TenantId) uuid.UUID
}

// NewWorkItemQueueReader builds the reader a composition root hands the
// human-work transport. db is the pool the frontier writes through
// (internal/data/pgxadapter.Pool in every real composition) and tenantUUID
// the same tenant-key-to-uuid mapping [CellConfig.TenantUUID] carries.
// Either nil yields a reader that refuses every read.
func NewWorkItemQueueReader(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) WorkItemQueueReader {
	return workItemQueueReader{db: db, tenantUUID: tenantUUID}
}

// ErrWorkItemQueueReaderUnconfigured reports a reader built with no
// database or no tenant mapping.
var ErrWorkItemQueueReaderUnconfigured = errors.New("app: the work item queue reader is not configured")

// scoped opens a tenant-scoped transaction that is always rolled back, the
// same read discipline [workflowControlReader.ReadWorkflowControlRecord]
// documents: every table here is row-level-security protected, so the tenant
// has to be established on the session before the first SELECT, and a read
// that committed would be claiming to have changed something.
func (r workItemQueueReader) scoped(ctx context.Context, tenant values.TenantId, fn func(workitem.Executor, uuid.UUID) error) error {
	if r.db == nil || r.tenantUUID == nil {
		return ErrWorkItemQueueReaderUnconfigured
	}
	tenantID := r.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return fmt.Errorf("app: work item queue read: tenant %q maps to no id", tenant)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("app: work item queue read: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return fmt.Errorf("app: work item queue read: scope tenant: %w", err)
	}
	return fn(tx, tenantID)
}

// ListWorkItemQueue implements [WorkItemQueueReader].
func (r workItemQueueReader) ListWorkItemQueue(
	ctx context.Context, tenant values.TenantId, principalRef string, now time.Time,
) ([]workitem.WorkItem, error) {
	var items []workitem.WorkItem
	err := r.scoped(ctx, tenant, func(ex workitem.Executor, tenantID uuid.UUID) error {
		listed, err := workitem.Store{}.ListQueue(ctx, ex, tenantID, principalRef, now)
		if err != nil {
			return fmt.Errorf("list the work item queue: %w", err)
		}
		items = listed
		return nil
	})
	return items, err
}

// ReadWorkItem implements [WorkItemQueueReader].
func (r workItemQueueReader) ReadWorkItem(
	ctx context.Context, tenant values.TenantId, workItemID uuid.UUID,
) (workitem.WorkItem, error) {
	var item workitem.WorkItem
	err := r.scoped(ctx, tenant, func(ex workitem.Executor, tenantID uuid.UUID) error {
		loaded, err := workitem.Store{}.Load(ctx, ex, tenantID, workItemID)
		if err != nil {
			return fmt.Errorf("load the work item: %w", err)
		}
		item = loaded
		return nil
	})
	return item, err
}
