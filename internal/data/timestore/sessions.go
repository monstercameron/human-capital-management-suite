package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrSessionAlreadyOpen is returned when a worker/assignment already has an
// open session: the FTIME-002 RACE guard. It comes from time_session's
// partial unique index, so two concurrent OpenSession calls for the same
// worker/assignment can never both succeed.
var ErrSessionAlreadyOpen = errors.New("time session already open for worker assignment")

// SessionRow is the current (or last-closed) state of one punch session.
type SessionRow struct {
	ID, TenantID, WorkerRef, AssignmentRef, Status, Source, ProjectRef string
	Revision                                                           int64
	OpenedAt, ClosedAt                                                 time.Time
	Payload                                                            json.RawMessage
	CreatedAt, UpdatedAt                                               time.Time
}

// EventRow is one entry in a session's append-only transition journal.
type EventRow struct {
	ID, TenantID, SessionID, Kind, ActorRef, IdempotencyKey, Digest string
	Sequence, Revision                                              int64
	Payload                                                         json.RawMessage
	CreatedAt                                                       time.Time
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func scanSessionRow(row dbport.Row) (SessionRow, error) {
	var r SessionRow
	var b []byte
	var closedAt *time.Time
	if err := row.Scan(&r.ID, &r.TenantID, &r.WorkerRef, &r.AssignmentRef, &r.Status, &r.Source, &r.ProjectRef,
		&r.Revision, &r.OpenedAt, &closedAt, &b, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return SessionRow{}, err
	}
	if closedAt != nil {
		r.ClosedAt = *closedAt
	}
	r.Payload = append(json.RawMessage(nil), b...)
	return r, nil
}

const sessionColumns = `id,tenant_id,worker_ref,assignment_ref,status,source,project_ref,revision,opened_at,closed_at,payload,created_at,updated_at`

// OpenSession creates a new open session and its opening event and outbox
// row in one tenant transaction. session.ID must be a caller-chosen
// deterministic identity: a retry that reuses the same ID and the same
// event digest replays the original result instead of erroring, and a
// worker/assignment that already has a different open session fails with
// ErrSessionAlreadyOpen from the unique index rather than a race.
func (s *Store) OpenSession(ctx context.Context, tenant string, session SessionRow, event EventRow) (SessionRow, error) {
	if tenant == "" || session.TenantID != tenant || session.ID == "" || session.WorkerRef == "" ||
		session.AssignmentRef == "" || session.Status != "OPEN" || session.Source == "" || session.OpenedAt.IsZero() ||
		event.SessionID != session.ID || event.Kind == "" || event.ActorRef == "" ||
		event.IdempotencyKey == "" || event.Digest == "" {
		return SessionRow{}, ErrInvalid
	}
	session.Revision = 1
	event.Sequence = 1
	event.Revision = 1
	payload := session.Payload
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	eventPayload := event.Payload
	if eventPayload == nil {
		eventPayload = json.RawMessage("{}")
	}

	var result SessionRow
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var priorDigest string
		err := tx.QueryRow(ctx, `SELECT digest FROM time_session_event WHERE tenant_id=$1 AND session_id=$2 AND sequence=1`, tenant, session.ID).Scan(&priorDigest)
		if err == nil {
			if priorDigest != event.Digest {
				return ErrIdempotencyConflict
			}
			row := tx.QueryRow(ctx, `SELECT `+sessionColumns+` FROM time_session WHERE tenant_id=$1 AND id=$2`, tenant, session.ID)
			result, err = scanSessionRow(row)
			return err
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO time_session(tenant_id,id,worker_ref,assignment_ref,status,source,project_ref,revision,opened_at,payload) VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$9::jsonb)`,
			tenant, session.ID, session.WorkerRef, session.AssignmentRef, session.Status, session.Source, session.ProjectRef, session.OpenedAt, []byte(payload))
		if err != nil {
			if isUniqueViolation(err) {
				return ErrSessionAlreadyOpen
			}
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO time_session_event(tenant_id,id,session_id,sequence,revision,kind,actor_ref,idempotency_key,digest,payload) VALUES($1,gen_random_uuid()::text,$2,1,1,$3,$4,$5,$6,$7::jsonb)`,
			tenant, session.ID, event.Kind, event.ActorRef, event.IdempotencyKey, event.Digest, []byte(eventPayload))
		if err != nil {
			return err
		}
		if err := appendOutbox(ctx, tx, tenant, "clock.session.opened", 1, eventPayload); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `SELECT `+sessionColumns+` FROM time_session WHERE tenant_id=$1 AND id=$2`, tenant, session.ID)
		result, err = scanSessionRow(row)
		return err
	})
	return result, err
}

// ApplySessionTransition applies a revision-checked update to an existing
// session together with the events that explain it, in one tenant
// transaction. A stale expectedRevision returns ErrRevisionConflict. A
// retry sharing events[0]'s idempotency key with a previously recorded
// transition replays that prior result when the digests match, and
// ErrIdempotencyConflict when they do not.
func (s *Store) ApplySessionTransition(ctx context.Context, tenant, sessionID string, expectedRevision int64, next SessionRow, events []EventRow) (SessionRow, error) {
	if tenant == "" || sessionID == "" || expectedRevision <= 0 || next.TenantID != tenant || next.ID != sessionID ||
		next.Status == "" || next.Source == "" || len(events) == 0 {
		return SessionRow{}, ErrInvalid
	}
	for i := range events {
		if events[i].SessionID != sessionID || events[i].Kind == "" || events[i].ActorRef == "" ||
			events[i].IdempotencyKey == "" || events[i].Digest == "" {
			return SessionRow{}, ErrInvalid
		}
	}
	key := events[0].IdempotencyKey
	digest := events[0].Digest

	payload := next.Payload
	if payload == nil {
		payload = json.RawMessage("{}")
	}

	var result SessionRow
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `SELECT `+sessionColumns+` FROM time_session WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, sessionID)
		current, err := scanSessionRow(row)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		var priorDigest string
		err = tx.QueryRow(ctx, `SELECT digest FROM time_session_event WHERE tenant_id=$1 AND session_id=$2 AND idempotency_key=$3`, tenant, sessionID, key).Scan(&priorDigest)
		if err == nil {
			if priorDigest != digest {
				return ErrIdempotencyConflict
			}
			result = current
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}

		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}

		sequence := expectedRevision
		for i := range events {
			sequence++
			events[i].Sequence = sequence
			events[i].Revision = sequence
			eventPayload := events[i].Payload
			if eventPayload == nil {
				eventPayload = json.RawMessage("{}")
			}
			_, err = tx.Exec(ctx,
				`INSERT INTO time_session_event(tenant_id,id,session_id,sequence,revision,kind,actor_ref,idempotency_key,digest,payload) VALUES($1,gen_random_uuid()::text,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`,
				tenant, sessionID, events[i].Sequence, events[i].Revision, events[i].Kind, events[i].ActorRef, events[i].IdempotencyKey, events[i].Digest, []byte(eventPayload))
			if err != nil {
				return err
			}
			if err := appendOutbox(ctx, tx, tenant, "clock.session."+strings.ToLower(events[i].Kind), 1, eventPayload); err != nil {
				return err
			}
		}

		newRevision := sequence
		var closedAt any
		if !next.ClosedAt.IsZero() {
			closedAt = next.ClosedAt
		}
		tag, err := tx.Exec(ctx,
			`UPDATE time_session SET status=$1,project_ref=$2,revision=$3,closed_at=$4,payload=$5::jsonb,updated_at=now() WHERE tenant_id=$6 AND id=$7 AND revision=$8`,
			next.Status, next.ProjectRef, newRevision, closedAt, []byte(payload), tenant, sessionID, current.Revision)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrRevisionConflict
		}
		row = tx.QueryRow(ctx, `SELECT `+sessionColumns+` FROM time_session WHERE tenant_id=$1 AND id=$2`, tenant, sessionID)
		result, err = scanSessionRow(row)
		return err
	})
	return result, err
}

// CurrentSession returns the active session for a worker's assignment, or
// ErrNotFound when none is active. OPEN and ON_BREAK are both active states.
func (s *Store) CurrentSession(ctx context.Context, tenant, worker, assignment string) (SessionRow, error) {
	if tenant == "" || worker == "" || assignment == "" {
		return SessionRow{}, ErrInvalid
	}
	var result SessionRow
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `SELECT `+sessionColumns+` FROM time_session WHERE tenant_id=$1 AND worker_ref=$2 AND assignment_ref=$3 AND status IN ('OPEN','ON_BREAK')`, tenant, worker, assignment)
		var err error
		result, err = scanSessionRow(row)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return result, err
}

// ListSessions returns a bounded, id-ordered page of a worker's sessions
// opened within [from, to). An empty cursor starts at the first page; the
// returned cursor is empty once the last page has been read.
func (s *Store) ListSessions(ctx context.Context, tenant, worker string, from, to time.Time, cursor string, limit int) ([]SessionRow, string, error) {
	if tenant == "" || worker == "" || limit <= 0 || limit > 500 {
		return nil, "", ErrInvalid
	}
	items := make([]SessionRow, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT `+sessionColumns+` FROM time_session WHERE tenant_id=$1 AND worker_ref=$2 AND ($3::timestamptz IS NULL OR opened_at>=$3) AND ($4::timestamptz IS NULL OR opened_at<$4) AND id>$5 ORDER BY id LIMIT $6`,
			tenant, worker, nullableTime(from), nullableTime(to), cursor, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			row, err := scanSessionRow(rows)
			if err != nil {
				return err
			}
			items = append(items, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, "", err
	}
	if len(items) <= limit {
		return items, "", nil
	}
	items = items[:limit]
	return items, items[len(items)-1].ID, nil
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
