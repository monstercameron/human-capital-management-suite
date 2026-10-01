package timeclockstore

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// SelfProjectionStore reads the durable CAS projection created by migration
// 00037. Labels are supplied by the catalog-backed localizer at the boundary.
type SelfProjectionStore struct {
	Store  *timestore.Store
	Labels SelfClockLabels
}

func (s SelfProjectionStore) ReadSelfClock(ctx context.Context, tenant, worker, assignment string) (clockservice.SelfClockStatus, error) {
	if s.Store == nil || s.Labels == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(worker) == "" || strings.TrimSpace(assignment) == "" {
		return clockservice.SelfClockStatus{}, clockservice.ErrUnavailable
	}
	labels, ok := s.Labels.(SelfClockProjectionLabels)
	if !ok {
		return clockservice.SelfClockStatus{}, clockservice.ErrUnavailable
	}
	if err := EnsureSelfClockProjection(ctx, s.Store, tenant, worker, assignment); err != nil {
		return clockservice.SelfClockStatus{}, err
	}
	var projection clockservice.SelfClockStatus
	var statusCode string
	var lastEvent time.Time
	err := s.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT revision, status_code, COALESCE(last_event_at, 'epoch'::timestamptz) FROM time_self_clock_projection WHERE tenant_id=$1 AND worker_ref=$2 AND assignment_ref=$3`, tenant, worker, assignment).Scan(&projection.Revision, &statusCode, &lastEvent)
	})
	if err != nil {
		return clockservice.SelfClockStatus{}, err
	}
	revision := projection.Revision
	projection, err = labels.ProjectionLabels(ctx, tenant, worker, assignment, statusCode, lastEvent, revision)
	if err != nil {
		return clockservice.SelfClockStatus{}, err
	}
	projection.Revision = revision
	projection.StatusCode = statusCode
	return projection, nil
}
