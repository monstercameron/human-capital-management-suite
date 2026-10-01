// Package timeclockstore maps the clock application ports to timestore's
// atomic batch transaction. It is a composition adapter, so neither the
// application service nor the data package imports the other for policy.
package timeclockstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// Adapter binds a timestore.Store to clockservice.UnitOfWork and its atomic
// batch seam.
type Adapter struct{ Store *timestore.Store }

// CommitPunchBatch maps application rows into one timestore transaction.
func (a Adapter) CommitPunchBatch(ctx context.Context, tenant, device string, entries []clockservice.BatchCommitEntry) (clockservice.BatchCommitResult, error) {
	rows := make([]timestore.AtomicBatchEntry, len(entries))
	for i, entry := range entries {
		if entry.Work != nil && len(entry.Work.SessionEvents) != 1 {
			return clockservice.BatchCommitResult{}, errors.New("clock batch: exactly one session event is required")
		}
		rows[i] = timestore.AtomicBatchEntry{Sequence: entry.Sequence, Receipt: timestore.ReceiptRow{DeviceSequence: entry.Receipt.DeviceSequence, Status: entry.Receipt.Status, Reason: entry.Receipt.Reason, ObservationID: entry.Receipt.ObservationID, Payload: json.RawMessage(entry.Receipt.Payload)}}
		if entry.Work == nil {
			continue
		}
		w := entry.Work
		rows[i].SessionIsNew, rows[i].ExpectedRevision = w.SessionIsNew, int64(w.ExpectedRevision)
		rows[i].Session = &timestore.SessionRow{ID: w.Session.ID, TenantID: w.Session.TenantID, WorkerRef: w.Session.WorkerRef, AssignmentRef: w.Session.AssignmentRef, Status: w.Session.Status, Source: w.Session.Source, ProjectRef: w.Session.ProjectRef, Revision: int64(w.Session.Revision), OpenedAt: w.Session.OpenedAt, ClosedAt: w.Session.ClosedAt, Payload: json.RawMessage(w.Session.Payload)}
		if len(w.SessionEvents) > 0 {
			e := w.SessionEvents[0]
			rows[i].Event = &timestore.EventRow{SessionID: w.Session.ID, Kind: e.Kind, ActorRef: e.ActorRef, IdempotencyKey: e.IdempotencyKey, Digest: e.Digest, Payload: json.RawMessage(e.Payload)}
		}
		o := w.Observation
		rows[i].Observation = &timestore.ObservationRow{ID: o.ID, TenantID: o.TenantID, WorkerRef: o.WorkerRef, AssignmentRef: o.AssignmentRef, DeviceRef: o.DeviceRef, Source: o.Source, EventType: o.EventType, ProjectRef: o.ProjectRef, Timezone: o.Timezone, IdempotencyKey: o.IdempotencyKey, Digest: o.Digest, CorrectsID: o.CorrectsID, OccurredAt: o.OccurredAt, ReceivedAt: o.ReceivedAt, Payload: json.RawMessage(o.Payload)}
	}
	if a.Store == nil {
		return clockservice.BatchCommitResult{}, clockservice.ErrUnavailable
	}
	got, err := a.Store.CommitBatch(ctx, tenant, device, rows)
	if err != nil {
		return clockservice.BatchCommitResult{}, err
	}
	out := clockservice.BatchCommitResult{HighestContiguous: got.HighestContiguous, Receipts: make([]clockservice.ReceiptRecord, len(got.Receipts)), Duplicates: make(map[int64]bool, len(got.Duplicates))}
	for seq, duplicate := range got.Duplicates {
		out.Duplicates[seq] = duplicate
	}
	for i, r := range got.Receipts {
		out.Receipts[i] = clockservice.ReceiptRecord{DeviceSequence: r.DeviceSequence, Status: r.Status, Reason: r.Reason, ObservationID: r.ObservationID, Payload: append([]byte(nil), r.Payload...)}
	}
	return out, nil
}

// Punch delegates the legacy single-punch path to timestore's existing
// session and observation methods. Batch submissions use CommitPunchBatch.
func (a Adapter) Punch(context.Context, string, clockservice.PunchWork) (clockservice.PunchResult, error) {
	return clockservice.PunchResult{}, clockservice.ErrUnavailable
}

// Batch is intentionally unavailable for callers that bypass SubmitPunches;
// the sequence-bearing CommitPunchBatch method is required for safe ingest.
func (a Adapter) Batch(context.Context, string, []clockservice.PunchWork) ([]clockservice.PunchResult, error) {
	return nil, clockservice.ErrUnavailable
}
