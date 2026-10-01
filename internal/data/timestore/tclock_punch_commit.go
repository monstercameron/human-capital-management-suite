package timestore

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PunchEffectResult is the durable result of one human punch effect.
// Human punches deliberately have no device or device sequence; their stable
// identity is the observation idempotency key.
type PunchEffectResult struct {
	Session     SessionRow
	Observation ObservationRow
	Duplicate   bool
}

// LookupPunchObservation finds one immutable observation by the indexed
// tenant/source/idempotency identity. It does not apply an occurrence-time
// window, so recovery remains correct for delayed offline submissions.
func (s *Store) LookupPunchObservation(ctx context.Context, tenant, source, idempotencyKey string) (ObservationRow, bool, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(source) == "" || strings.TrimSpace(idempotencyKey) == "" {
		return ObservationRow{}, false, ErrInvalid
	}
	var result ObservationRow
	found := false
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `SELECT `+observationColumns+` FROM time_observation WHERE tenant_id=$1 AND source=$2 AND idempotency_key=$3`, tenant, source, idempotencyKey)
		got, err := scanObservationRow(row)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		result, found = got, true
		return nil
	})
	if err != nil {
		return ObservationRow{}, false, err
	}
	return result, found, nil
}

// CommitPunchEffect atomically commits one observation, its session
// transition, and the corresponding outbox rows. An identical replay returns
// the original rows without adding any rows; a changed replay conflicts.
func (s *Store) CommitPunchEffect(ctx context.Context, tenant string, entry AtomicBatchEntry) (PunchEffectResult, error) {
	if strings.TrimSpace(tenant) == "" || entry.Observation == nil || entry.Session == nil || entry.Event == nil ||
		entry.Observation.TenantID != tenant || entry.Session.TenantID != tenant || entry.Event.SessionID != entry.Session.ID ||
		entry.Observation.ID == "" || entry.Session.ID == "" {
		return PunchEffectResult{}, ErrInvalid
	}
	entry.Receipt.ObservationID = entry.Observation.ID
	result := PunchEffectResult{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		// Serialize retries for one source/key. This closes the race where two
		// transactions both observe no prior observation before the unique index
		// arbitrates one of them.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+"|"+entry.Observation.Source+"|"+entry.Observation.IdempotencyKey); err != nil {
			return err
		}
		var priorID, priorDigest string
		err := tx.QueryRow(ctx, `SELECT id,digest FROM time_observation WHERE tenant_id=$1 AND source=$2 AND idempotency_key=$3`, tenant, entry.Observation.Source, entry.Observation.IdempotencyKey).Scan(&priorID, &priorDigest)
		if err == nil {
			if priorDigest != entry.Observation.Digest || priorID != entry.Observation.ID {
				return ErrIdempotencyConflict
			}
			result.Duplicate = true
		} else if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if !result.Duplicate {
			projectionRevision, err := preparePunchProjection(ctx, tx, tenant, *entry.Observation, entry.ExpectedProjectionRevision)
			if err != nil {
				return err
			}
			if err := commitBatchObservation(ctx, tx, tenant, *entry.Observation); err != nil {
				return err
			}
			if err := commitBatchSession(ctx, tx, tenant, entry); err != nil {
				return err
			}
			if projectionRevision > 0 {
				if _, err := BumpSelfProjection(ctx, tx, tenant, entry.Observation.WorkerRef, entry.Observation.AssignmentRef, projectionRevision, punchProjectionStatus(entry.Observation.EventType, entry.Session.Status), entry.Observation.OccurredAt); err != nil {
					return err
				}
			}
		}
		obs, err := readPunchObservation(ctx, tx, tenant, entry.Observation.ID)
		if err != nil {
			return err
		}
		session, err := readPunchSession(ctx, tx, tenant, entry.Session.ID)
		if err != nil {
			return err
		}
		if session.WorkerRef != entry.Session.WorkerRef || session.AssignmentRef != entry.Session.AssignmentRef {
			return ErrIdempotencyConflict
		}
		result.Observation, result.Session = obs, session
		return nil
	})
	if err != nil {
		return PunchEffectResult{}, err
	}
	return result, nil
}

func preparePunchProjection(ctx context.Context, tx dbport.Tx, tenant string, obs ObservationRow, expected int64) (int64, error) {
	projection, err := LoadSelfProjection(ctx, tx, tenant, obs.WorkerRef, obs.AssignmentRef)
	if err == nil {
		if expected > 0 && projection.Revision != expected {
			return 0, ErrRevisionConflict
		}
		return projection.Revision, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return 0, err
	}
	if expected > 0 {
		return 0, ErrNotFound
	}
	if obs.Source != "WORKER_SELF" {
		return 0, nil
	}
	return 0, ErrNotFound
}

func punchProjectionStatus(eventType, sessionStatus string) string {
	switch strings.ToUpper(strings.TrimSpace(eventType)) {
	case "IN":
		return "CLOCKED_IN"
	case "BREAK_START":
		return "ON_BREAK"
	case "BREAK_END":
		return "CLOCKED_IN"
	case "OUT", "AUTO_OUT":
		return "CLOCKED_OUT"
	}
	switch strings.ToUpper(strings.TrimSpace(sessionStatus)) {
	case "OPEN":
		return "CLOCKED_IN"
	case "ON_BREAK":
		return "ON_BREAK"
	case "CLOSED", "AUTO_CLOSED":
		return "CLOCKED_OUT"
	default:
		return "CLOCKED_OUT"
	}
}

func readPunchObservation(ctx context.Context, tx dbport.Tx, tenant, id string) (ObservationRow, error) {
	row := tx.QueryRow(ctx, `SELECT `+observationColumns+` FROM time_observation WHERE tenant_id=$1 AND id=$2`, tenant, id)
	result, err := scanObservationRow(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return ObservationRow{}, ErrNotFound
	}
	return result, err
}

func readPunchSession(ctx context.Context, tx dbport.Tx, tenant, id string) (SessionRow, error) {
	row := tx.QueryRow(ctx, `SELECT `+sessionColumns+` FROM time_session WHERE tenant_id=$1 AND id=$2`, tenant, id)
	result, err := scanSessionRow(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return SessionRow{}, ErrNotFound
	}
	return result, err
}

// LoadPunchEffect recovers a human punch by tenant and immutable observation
// id. The session id is read from the outbox event written with the session
// transition, so a response lost after commit can be retried safely.
func (s *Store) LoadPunchEffect(ctx context.Context, tenant, observationID string) (PunchEffectResult, bool, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(observationID) == "" {
		return PunchEffectResult{}, false, ErrInvalid
	}
	result := PunchEffectResult{}
	found := false
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		obs, err := readPunchObservation(ctx, tx, tenant, observationID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var sessionID string
		err = tx.QueryRow(ctx, `SELECT payload->>'session_id' FROM time_outbox WHERE tenant_id=$1 AND payload->>'observation_id'=$2 AND payload ? 'session_id' ORDER BY sequence LIMIT 1`, tenant, observationID).Scan(&sessionID)
		if errors.Is(err, dbport.ErrNoRows) || strings.TrimSpace(sessionID) == "" {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		session, err := readPunchSession(ctx, tx, tenant, sessionID)
		if err != nil {
			return err
		}
		result = PunchEffectResult{Session: session, Observation: obs, Duplicate: true}
		found = true
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return PunchEffectResult{}, false, nil
	}
	if err != nil {
		return PunchEffectResult{}, false, err
	}
	return result, found, nil
}
