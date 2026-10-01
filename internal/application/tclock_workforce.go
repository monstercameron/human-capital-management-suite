package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

// ClockWorkforceReader is the production, tenant-scoped people reader used by
// clock context resolution. It reads the canonical workforce aggregate and
// never accepts display names, roles, project ids, or site ids from a device.
type ClockWorkforceReader struct {
	Pool  *pgxadapter.Pool
	Clock *timestore.Store
}

var _ interface {
	ResolveWorker(context.Context, string, string) (string, bool, error)
	ResolveAssignment(context.Context, string, string, string) (string, string, bool, error)
	CurrentAssignment(context.Context, string, string, time.Time) (string, string, string, bool, error)
	ResolveWorkerStatus(context.Context, string, string, string) (clockservice.WorkerStatusResult, error)
} = ClockWorkforceReader{}

// ResolveWorker resolves a worker key or id through the RLS-scoped workforce
// table and requires the canonical lifecycle status to be active.
func (r ClockWorkforceReader) ResolveWorker(ctx context.Context, tenant, ref string) (string, bool, error) {
	if r.Pool == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(ref) == "" {
		return "", false, clockservice.ErrUnavailable
	}
	tenantID := pgstore.TenantID(tenant)
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return "", false, err
	}
	row, found, err := (workforce.Store{}).Get(ctx, tx, tenantID, ref)
	if err != nil {
		return "", false, err
	}
	if !found || !strings.EqualFold(row.LifecycleStatus, "active") {
		return "", false, clockservice.ErrWorkerNotEligible
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return row.WorkerID.String(), true, nil
}

// ResolveAssignment accepts only an assignment belonging to the resolved
// worker. Crew shifts do not currently persist project/site assignment refs;
// consequently this method fails closed when the canonical assignment has no
// independently stored project/site binding.
func (r ClockWorkforceReader) ResolveAssignment(ctx context.Context, tenant, worker, assignment string) (string, string, bool, error) {
	if r.Pool == nil || tenant == "" || worker == "" || assignment == "" {
		return "", "", false, clockservice.ErrUnavailable
	}
	workerID, active, err := r.ResolveWorker(ctx, tenant, worker)
	if err != nil || !active {
		return "", "", false, err
	}
	if workerID == "" {
		return "", "", false, clockservice.ErrWorkerNotEligible
	}
	return "", "", false, clockservice.ErrAssignmentNotFound
}

// CurrentAssignment resolves the effective workforce assignment as of the
// event instant. A missing project/site authority is returned as not found;
// location is not guessed to be a site.
func (r ClockWorkforceReader) CurrentAssignment(ctx context.Context, tenant, worker string, at time.Time) (string, string, string, bool, error) {
	if r.Pool == nil || tenant == "" || worker == "" || at.IsZero() {
		return "", "", "", false, clockservice.ErrUnavailable
	}
	row, err := r.readWorker(ctx, tenant, worker)
	if err != nil {
		return "", "", "", false, err
	}
	if row.AssignmentID == "" {
		return "", "", "", false, clockservice.ErrAssignmentNotFound
	}
	// The workforce projection carries the effective assignment id, but the
	// current schema has no authoritative project/site binding for it. Keep
	// the id for diagnostics and fail closed for punch authorization.
	return row.AssignmentID, "", "", false, nil
}

// ResolveWorkerStatus reads the current worker display and open-session state.
func (r ClockWorkforceReader) ResolveWorkerStatus(ctx context.Context, tenant, worker, _ string) (clockservice.WorkerStatusResult, error) {
	if r.Pool == nil || r.Clock == nil || tenant == "" || worker == "" {
		return clockservice.WorkerStatusResult{}, clockservice.ErrUnavailable
	}
	row, err := r.readWorker(ctx, tenant, worker)
	if err != nil {
		return clockservice.WorkerStatusResult{}, err
	}
	status := clockservice.WorkerStatusResult{WorkerID: row.WorkerID.String(), DisplayName: row.DisplayName()}
	// Session lookup requires an assignment ref. The workforce row is the
	// authoritative source for that ref; no client field is consulted.
	if row.AssignmentID != "" {
		session, sessionErr := r.Clock.CurrentSession(ctx, tenant, row.WorkerID.String(), row.AssignmentID)
		if sessionErr != nil && !errors.Is(sessionErr, timestore.ErrNotFound) {
			return clockservice.WorkerStatusResult{}, sessionErr
		}
		if sessionErr == nil {
			status.SessionStatus = session.Status
		}
	}
	return status, nil
}

func (r ClockWorkforceReader) readWorker(ctx context.Context, tenant, ref string) (workforce.WorkerRow, error) {
	tenantID := pgstore.TenantID(tenant)
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return workforce.WorkerRow{}, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return workforce.WorkerRow{}, err
	}
	row, found, err := (workforce.Store{}).Get(ctx, tx, tenantID, ref)
	if err != nil {
		return workforce.WorkerRow{}, err
	}
	if !found || !strings.EqualFold(row.LifecycleStatus, "active") {
		return workforce.WorkerRow{}, clockservice.ErrWorkerNotEligible
	}
	if err := tx.Commit(ctx); err != nil {
		return workforce.WorkerRow{}, err
	}
	return row, nil
}
