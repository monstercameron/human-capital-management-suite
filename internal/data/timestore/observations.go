package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ObservationRow is one immutable signed punch observation. ReceivedAt is
// always the server's own clock, stamped by the caller from the server's
// Now before this call, never trusted from the device; OccurredAt is the
// device-claimed instant. CorrectsID links a later correction to the
// earlier observation it corrects without ever rewriting that earlier row.
type ObservationRow struct {
	ID, TenantID, WorkerRef, AssignmentRef, DeviceRef, Source, EventType, ProjectRef, Timezone string
	IdempotencyKey, Digest, CorrectsID                                                         string
	OccurredAt, ReceivedAt, CreatedAt                                                          time.Time
	Payload                                                                                    json.RawMessage
}

const observationColumns = `id,tenant_id,worker_ref,assignment_ref,device_ref,source,event_type,project_ref,timezone,idempotency_key,digest,corrects_id,occurred_at,received_at,payload,created_at`

func scanObservationRow(row dbport.Row) (ObservationRow, error) {
	var r ObservationRow
	var b []byte
	var corrects *string
	if err := row.Scan(&r.ID, &r.TenantID, &r.WorkerRef, &r.AssignmentRef, &r.DeviceRef, &r.Source, &r.EventType,
		&r.ProjectRef, &r.Timezone, &r.IdempotencyKey, &r.Digest, &corrects, &r.OccurredAt, &r.ReceivedAt, &b, &r.CreatedAt); err != nil {
		return ObservationRow{}, err
	}
	if corrects != nil {
		r.CorrectsID = *corrects
	}
	r.Payload = append(json.RawMessage(nil), b...)
	return r, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// AppendObservation persists one immutable punch observation and its
// outbox event in one tenant transaction, keyed by (tenant, source,
// idempotency_key). A retry with the same key and the same digest replays
// the original row and reports it as a duplicate rather than inserting a
// second one; the same key with a different digest is ErrIdempotencyConflict.
// A CorrectsID that does not name an existing observation for the tenant is
// ErrNotFound: correction lineage must point at real evidence.
func (s *Store) AppendObservation(ctx context.Context, tenant string, obs ObservationRow) (ObservationRow, bool, error) {
	if tenant == "" || obs.TenantID != tenant || obs.ID == "" || obs.WorkerRef == "" || obs.AssignmentRef == "" ||
		obs.Source == "" || obs.EventType == "" || obs.OccurredAt.IsZero() || obs.ReceivedAt.IsZero() ||
		obs.IdempotencyKey == "" || obs.Digest == "" {
		return ObservationRow{}, false, ErrInvalid
	}
	payload := obs.Payload
	if payload == nil {
		payload = json.RawMessage("{}")
	}

	var result ObservationRow
	duplicate := false
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if obs.CorrectsID != "" {
			var exists string
			err := tx.QueryRow(ctx, `SELECT id FROM time_observation WHERE tenant_id=$1 AND id=$2`, tenant, obs.CorrectsID).Scan(&exists)
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
		}

		var priorID, priorDigest string
		err := tx.QueryRow(ctx, `SELECT id,digest FROM time_observation WHERE tenant_id=$1 AND source=$2 AND idempotency_key=$3`, tenant, obs.Source, obs.IdempotencyKey).Scan(&priorID, &priorDigest)
		if err == nil {
			if priorDigest != obs.Digest {
				return ErrIdempotencyConflict
			}
			duplicate = true
			row := tx.QueryRow(ctx, `SELECT `+observationColumns+` FROM time_observation WHERE tenant_id=$1 AND id=$2`, tenant, priorID)
			result, err = scanObservationRow(row)
			return err
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO time_observation(tenant_id,id,worker_ref,assignment_ref,device_ref,source,event_type,project_ref,timezone,occurred_at,received_at,idempotency_key,digest,corrects_id,payload)
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb)`,
			tenant, obs.ID, obs.WorkerRef, obs.AssignmentRef, obs.DeviceRef, obs.Source, obs.EventType, obs.ProjectRef, obs.Timezone,
			obs.OccurredAt, obs.ReceivedAt, obs.IdempotencyKey, obs.Digest, nullIfEmpty(obs.CorrectsID), []byte(payload))
		if err != nil {
			return err
		}
		if err := appendOutbox(ctx, tx, tenant, "clock.punch.accepted", 1, payload); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `SELECT `+observationColumns+` FROM time_observation WHERE tenant_id=$1 AND id=$2`, tenant, obs.ID)
		result, err = scanObservationRow(row)
		return err
	})
	return result, duplicate, err
}

// ListObservations returns a bounded, id-ordered page of a worker's
// observations in [from, to). Used by recovery checks and by higher layers
// that need the raw evidence behind a session.
func (s *Store) ListObservations(ctx context.Context, tenant, worker string, from, to time.Time, cursor string, limit int) ([]ObservationRow, string, error) {
	if tenant == "" || worker == "" || limit <= 0 || limit > 500 {
		return nil, "", ErrInvalid
	}
	items := make([]ObservationRow, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT `+observationColumns+` FROM time_observation WHERE tenant_id=$1 AND worker_ref=$2 AND ($3::timestamptz IS NULL OR occurred_at>=$3) AND ($4::timestamptz IS NULL OR occurred_at<$4) AND id>$5 ORDER BY id LIMIT $6`,
			tenant, worker, nullableTime(from), nullableTime(to), cursor, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			row, err := scanObservationRow(rows)
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
