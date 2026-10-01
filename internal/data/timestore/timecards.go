package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TimecardState is the timecard aggregate's lifecycle state (FTIME-004).
type TimecardState string

const (
	TimecardOpen      TimecardState = "OPEN"
	TimecardSubmitted TimecardState = "SUBMITTED"
	TimecardApproved  TimecardState = "APPROVED"
	TimecardRejected  TimecardState = "REJECTED"
	TimecardReopened  TimecardState = "REOPENED"
)

// TimecardEventKind discriminates one append-only timecard_event row.
type TimecardEventKind string

const (
	EventLine        TimecardEventKind = "LINE"
	EventCorrection  TimecardEventKind = "CORRECTION"
	EventAttestation TimecardEventKind = "ATTESTATION"
	EventApproval    TimecardEventKind = "APPROVAL"
	EventRejection   TimecardEventKind = "REJECTION"
	EventReopen      TimecardEventKind = "REOPEN"
)

// Timecard is the aggregate row: revision, state and a payload the caller
// fills with paired intervals, breaks, schedule comparison and exceptions.
// The append-only history that justifies the current state lives in
// TimecardEvent rows, never inside this payload.
type Timecard struct {
	TenantID    string
	ID          string
	WorkerRef   string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Revision    int64
	State       TimecardState
	Payload     json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TimecardEvent is one line, correction, attestation, approval, rejection or
// reopen entry. Once written it is immutable (time_forbid_mutation).
type TimecardEvent struct {
	TenantID       string
	ID             string
	TimecardID     string
	Sequence       int64
	Revision       int64
	Kind           TimecardEventKind
	ActorID        string
	IdempotencyKey string
	Payload        json.RawMessage
	CreatedAt      time.Time
}

// CreateTimecard opens a new timecard aggregate at revision 1.
func (s *Store) CreateTimecard(ctx context.Context, tenant string, tc Timecard) error {
	if tenant == "" || tc.TenantID != tenant || tc.ID == "" || tc.WorkerRef == "" || tc.State != TimecardOpen ||
		tc.PeriodStart.IsZero() || tc.PeriodEnd.IsZero() || !tc.PeriodEnd.After(tc.PeriodStart) {
		return ErrInvalid
	}
	payload := tc.Payload
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO timecard(tenant_id,id,worker_ref,period_start,period_end,revision,state,payload) VALUES($1,$2,$3,$4,$5,1,$6,$7::jsonb)`,
			tenant, tc.ID, tc.WorkerRef, tc.PeriodStart, tc.PeriodEnd, string(TimecardOpen), []byte(payload))
		return err
	})
}

// GetTimecard reads the current aggregate row.
func (s *Store) GetTimecard(ctx context.Context, tenant, id string) (Timecard, error) {
	if tenant == "" || id == "" {
		return Timecard{}, ErrInvalid
	}
	var tc Timecard
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var state string
		var payload []byte
		row := tx.QueryRow(ctx, `SELECT tenant_id,id,worker_ref,period_start,period_end,revision,state,payload,created_at,updated_at FROM timecard WHERE tenant_id=$1 AND id=$2`, tenant, id)
		if err := row.Scan(&tc.TenantID, &tc.ID, &tc.WorkerRef, &tc.PeriodStart, &tc.PeriodEnd, &tc.Revision, &state, &payload, &tc.CreatedAt, &tc.UpdatedAt); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		tc.State = TimecardState(state)
		tc.Payload = append(json.RawMessage(nil), payload...)
		return nil
	})
	if err != nil {
		return Timecard{}, err
	}
	return tc, nil
}

// AppendTimecardEvent appends one event and, when nextState is non-empty,
// advances the aggregate's state and revision together in the same
// transaction so a reader of the aggregate always sees a revision whose
// event exists. expectedRevision guards every append: a concurrent
// correction and approval racing on the same timecard can only have one
// winner (FTIME-004 Race).
func (s *Store) AppendTimecardEvent(ctx context.Context, tenant string, expectedRevision int64, nextState TimecardState, ev TimecardEvent) (Timecard, error) {
	if tenant == "" || ev.TenantID != tenant || ev.TimecardID == "" || ev.ActorID == "" || ev.IdempotencyKey == "" || expectedRevision <= 0 {
		return Timecard{}, ErrInvalid
	}
	if !validTimecardEventKind(ev.Kind) {
		return Timecard{}, ErrInvalid
	}
	payload := ev.Payload
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}
	var out Timecard
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var state string
		var currentPayload []byte
		row := tx.QueryRow(ctx, `SELECT revision,state,payload FROM timecard WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, ev.TimecardID)
		var revision int64
		if err := row.Scan(&revision, &state, &currentPayload); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		// Idempotent replay: the same actor and key at the same kind returns
		// the already-recorded event rather than erroring or duplicating.
		var existingSeq int64
		err := tx.QueryRow(ctx, `SELECT sequence FROM timecard_event WHERE tenant_id=$1 AND timecard_id=$2 AND kind=$3 AND actor_id=$4 AND idempotency_key=$5`,
			tenant, ev.TimecardID, string(ev.Kind), ev.ActorID, ev.IdempotencyKey).Scan(&existingSeq)
		if err == nil {
			return s.loadTimecardLocked(ctx, tx, tenant, ev.TimecardID, &out)
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if revision != expectedRevision {
			return ErrRevisionConflict
		}
		newRevision := revision + 1
		resultState := TimecardState(state)
		if nextState != "" {
			resultState = nextState
		}
		var newSeq int64
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(sequence),0)+1 FROM timecard_event WHERE tenant_id=$1 AND timecard_id=$2`, tenant, ev.TimecardID).Scan(&newSeq); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO timecard_event(tenant_id,id,timecard_id,sequence,revision,kind,actor_id,idempotency_key,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`,
			tenant, uuid.NewString(), ev.TimecardID, newSeq, newRevision, string(ev.Kind), ev.ActorID, ev.IdempotencyKey, []byte(payload)); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE timecard SET revision=$1,state=$2,updated_at=now() WHERE tenant_id=$3 AND id=$4 AND revision=$5`,
			newRevision, string(resultState), tenant, ev.TimecardID, revision)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrRevisionConflict
		}
		return s.loadTimecardLocked(ctx, tx, tenant, ev.TimecardID, &out)
	})
	if err != nil {
		return Timecard{}, err
	}
	return out, nil
}

func (s *Store) loadTimecardLocked(ctx context.Context, tx dbport.Tx, tenant, id string, out *Timecard) error {
	var state string
	var payload []byte
	row := tx.QueryRow(ctx, `SELECT tenant_id,id,worker_ref,period_start,period_end,revision,state,payload,created_at,updated_at FROM timecard WHERE tenant_id=$1 AND id=$2`, tenant, id)
	if err := row.Scan(&out.TenantID, &out.ID, &out.WorkerRef, &out.PeriodStart, &out.PeriodEnd, &out.Revision, &state, &payload, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return err
	}
	out.State = TimecardState(state)
	out.Payload = append(json.RawMessage(nil), payload...)
	return nil
}

func validTimecardEventKind(k TimecardEventKind) bool {
	switch k {
	case EventLine, EventCorrection, EventAttestation, EventApproval, EventRejection, EventReopen:
		return true
	}
	return false
}

// TimecardHistory returns every event recorded for id, oldest first.
func (s *Store) TimecardHistory(ctx context.Context, tenant, id string) ([]TimecardEvent, error) {
	if tenant == "" || id == "" {
		return nil, ErrInvalid
	}
	out := make([]TimecardEvent, 0)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,id,timecard_id,sequence,revision,kind,actor_id,idempotency_key,payload,created_at FROM timecard_event WHERE tenant_id=$1 AND timecard_id=$2 ORDER BY sequence`, tenant, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ev TimecardEvent
			var kind string
			var payload []byte
			if err := rows.Scan(&ev.TenantID, &ev.ID, &ev.TimecardID, &ev.Sequence, &ev.Revision, &kind, &ev.ActorID, &ev.IdempotencyKey, &payload, &ev.CreatedAt); err != nil {
				return err
			}
			ev.Kind = TimecardEventKind(kind)
			ev.Payload = append(json.RawMessage(nil), payload...)
			out = append(out, ev)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
