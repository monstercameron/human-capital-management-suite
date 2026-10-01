package timeclockstore

import (
	"context"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

var _ clockservice.PunchCommitter = Adapter{}
var _ clockservice.PunchReceiptReader = Adapter{}

// CommitPunch commits a human punch through the timestore's single
// observation/session/outbox transaction. Device identity and counters are
// intentionally left empty for this first-party human path.
func (a Adapter) CommitPunch(ctx context.Context, tenant string, work clockservice.PunchWork) (clockservice.PunchResult, error) {
	if a.Store == nil {
		return clockservice.PunchResult{}, clockservice.ErrUnavailable
	}
	if len(work.SessionEvents) != 1 {
		return clockservice.PunchResult{}, clockservice.ErrInvalidRequest
	}
	entry := punchEntry(work)
	got, err := a.Store.CommitPunchEffect(ctx, tenant, entry)
	if err != nil {
		return clockservice.PunchResult{}, err
	}
	return clockservice.PunchResult{
		Session:     punchSessionRecord(got.Session),
		Observation: punchObservationRecord(got.Observation),
		Duplicate:   got.Duplicate,
	}, nil
}

// LoadPunchResult recovers the immutable result of a committed human punch.
func (a Adapter) LoadPunchResult(ctx context.Context, tenant, observationID string) (clockservice.PunchResult, bool, error) {
	if a.Store == nil {
		return clockservice.PunchResult{}, false, clockservice.ErrUnavailable
	}
	got, found, err := a.Store.LoadPunchEffect(ctx, tenant, observationID)
	if err != nil || !found {
		return clockservice.PunchResult{}, found, err
	}
	return clockservice.PunchResult{Session: punchSessionRecord(got.Session), Observation: punchObservationRecord(got.Observation), Duplicate: true}, true, nil
}

func punchEntry(work clockservice.PunchWork) timestore.AtomicBatchEntry {
	e := work.SessionEvents[0]
	return timestore.AtomicBatchEntry{
		Receipt:                    timestore.ReceiptRow{ObservationID: work.Observation.ID},
		ExpectedProjectionRevision: int64(work.ExpectedProjectionRevision),
		SessionIsNew:               work.SessionIsNew,
		ExpectedRevision:           int64(work.ExpectedRevision),
		Session:                    &timestore.SessionRow{ID: work.Session.ID, TenantID: work.Session.TenantID, WorkerRef: work.Session.WorkerRef, AssignmentRef: work.Session.AssignmentRef, Status: work.Session.Status, Source: work.Session.Source, ProjectRef: work.Session.ProjectRef, Revision: int64(work.Session.Revision), OpenedAt: work.Session.OpenedAt, ClosedAt: work.Session.ClosedAt, Payload: json.RawMessage(work.Session.Payload)},
		Event:                      &timestore.EventRow{SessionID: work.Session.ID, Kind: e.Kind, ActorRef: e.ActorRef, IdempotencyKey: e.IdempotencyKey, Digest: e.Digest, Payload: json.RawMessage(e.Payload)},
		Observation:                &timestore.ObservationRow{ID: work.Observation.ID, TenantID: work.Observation.TenantID, WorkerRef: work.Observation.WorkerRef, AssignmentRef: work.Observation.AssignmentRef, DeviceRef: work.Observation.DeviceRef, Source: work.Observation.Source, EventType: work.Observation.EventType, ProjectRef: work.Observation.ProjectRef, Timezone: work.Observation.Timezone, IdempotencyKey: work.Observation.IdempotencyKey, Digest: work.Observation.Digest, CorrectsID: work.Observation.CorrectsID, OccurredAt: work.Observation.OccurredAt, ReceivedAt: work.Observation.ReceivedAt, Payload: json.RawMessage(work.Observation.Payload)},
	}
}

func punchSessionRecord(row timestore.SessionRow) clockservice.SessionRecord {
	return clockservice.SessionRecord{ID: row.ID, TenantID: row.TenantID, WorkerRef: row.WorkerRef, AssignmentRef: row.AssignmentRef, Status: row.Status, Source: row.Source, ProjectRef: row.ProjectRef, Revision: uint64(row.Revision), OpenedAt: row.OpenedAt, ClosedAt: row.ClosedAt, Payload: append([]byte(nil), row.Payload...)}
}

func punchObservationRecord(row timestore.ObservationRow) clockservice.ObservationRecord {
	return clockservice.ObservationRecord{ID: row.ID, TenantID: row.TenantID, WorkerRef: row.WorkerRef, AssignmentRef: row.AssignmentRef, DeviceRef: row.DeviceRef, Source: row.Source, EventType: row.EventType, ProjectRef: row.ProjectRef, Timezone: row.Timezone, IdempotencyKey: row.IdempotencyKey, Digest: row.Digest, CorrectsID: row.CorrectsID, OccurredAt: row.OccurredAt, ReceivedAt: row.ReceivedAt, Payload: append([]byte(nil), row.Payload...)}
}
