package timeclockstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// ObservationAdapter maps immutable punch observations to timestore.
type ObservationAdapter struct{ Store *timestore.Store }

var _ clockservice.ObservationStore = ObservationAdapter{}

// AppendObservation persists or replays an idempotent observation.
func (a ObservationAdapter) AppendObservation(ctx context.Context, tenant string, obs clockservice.ObservationRecord) (clockservice.ObservationRecord, bool, error) {
	if a.Store == nil {
		return clockservice.ObservationRecord{}, false, clockservice.ErrUnavailable
	}
	r := timestore.ObservationRow{ID: obs.ID, TenantID: obs.TenantID, WorkerRef: obs.WorkerRef, AssignmentRef: obs.AssignmentRef, DeviceRef: obs.DeviceRef, Source: obs.Source, EventType: obs.EventType, ProjectRef: obs.ProjectRef, Timezone: obs.Timezone, IdempotencyKey: obs.IdempotencyKey, Digest: obs.Digest, CorrectsID: obs.CorrectsID, OccurredAt: obs.OccurredAt, ReceivedAt: obs.ReceivedAt, Payload: json.RawMessage(append([]byte(nil), obs.Payload...))}
	got, duplicate, err := a.Store.AppendObservation(ctx, tenant, r)
	return clockservice.ObservationRecord{ID: got.ID, TenantID: got.TenantID, WorkerRef: got.WorkerRef, AssignmentRef: got.AssignmentRef, DeviceRef: got.DeviceRef, Source: got.Source, EventType: got.EventType, ProjectRef: got.ProjectRef, Timezone: got.Timezone, IdempotencyKey: got.IdempotencyKey, Digest: got.Digest, CorrectsID: got.CorrectsID, OccurredAt: got.OccurredAt, ReceivedAt: got.ReceivedAt, Payload: append([]byte(nil), got.Payload...)}, duplicate, err
}

// ListObservations reads a bounded tenant-scoped observation page.
func (a ObservationAdapter) ListObservations(ctx context.Context, tenant, worker string, from, to time.Time, cursor string, limit int) ([]clockservice.ObservationRecord, string, error) {
	if a.Store == nil {
		return nil, "", clockservice.ErrUnavailable
	}
	rows, next, err := a.Store.ListObservations(ctx, tenant, worker, from, to, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	out := make([]clockservice.ObservationRecord, len(rows))
	for i, r := range rows {
		out[i] = clockservice.ObservationRecord{ID: r.ID, TenantID: r.TenantID, WorkerRef: r.WorkerRef, AssignmentRef: r.AssignmentRef, DeviceRef: r.DeviceRef, Source: r.Source, EventType: r.EventType, ProjectRef: r.ProjectRef, Timezone: r.Timezone, IdempotencyKey: r.IdempotencyKey, Digest: r.Digest, CorrectsID: r.CorrectsID, OccurredAt: r.OccurredAt, ReceivedAt: r.ReceivedAt, Payload: append([]byte(nil), r.Payload...)}
	}
	return out, next, nil
}
