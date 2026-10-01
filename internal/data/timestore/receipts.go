package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// receiptCursorLookahead bounds the single read RecordBatch uses to find
// how far the contiguous-acknowledged run extends past the stored cursor.
// It comfortably covers the largest batch this ingest path is sized for
// (TCLOCK-003's 200-punch benchmark) with headroom for prior partial gaps.
const receiptCursorLookahead = 10000

// ReceiptRow is the immutable per-punch receipt for one device batch entry,
// keyed by (tenant, device, device_sequence) so a retried sequence number
// always resolves to its original receipt rather than a new one.
type ReceiptRow struct {
	TenantID, DeviceID, Status, Reason, ObservationID string
	DeviceSequence                                    int64
	Payload                                           json.RawMessage
	CreatedAt                                         time.Time
}

func scanReceiptRow(row dbport.Row) (ReceiptRow, error) {
	var r ReceiptRow
	var b []byte
	var obs *string
	if err := row.Scan(&r.TenantID, &r.DeviceID, &r.DeviceSequence, &r.Status, &r.Reason, &obs, &b, &r.CreatedAt); err != nil {
		return ReceiptRow{}, err
	}
	if obs != nil {
		r.ObservationID = *obs
	}
	r.Payload = append(json.RawMessage(nil), b...)
	return r, nil
}

const receiptColumns = `tenant_id,device_id,device_sequence,status,reason,observation_id,payload,created_at`

// RecordBatch persists one device's batch of punch receipts and its outbox
// events, then recomputes and returns the highest device_sequence
// contiguously acknowledged from 1 for that device, in one tenant
// transaction. A device_sequence already recorded (a retried batch)
// returns its original receipt unchanged rather than a new row; a device's
// concurrent batches are serialized by an advisory lock so the recomputed
// cursor always reflects every receipt already committed.
func (s *Store) RecordBatch(ctx context.Context, tenant, deviceID string, batch []ReceiptRow) ([]ReceiptRow, int64, error) {
	if tenant == "" || deviceID == "" || len(batch) == 0 {
		return nil, 0, ErrInvalid
	}
	ordered := make([]ReceiptRow, len(batch))
	copy(ordered, batch)
	for i := range ordered {
		if ordered[i].DeviceSequence <= 0 || strings.TrimSpace(ordered[i].Status) == "" {
			return nil, 0, ErrInvalid
		}
		ordered[i].TenantID = tenant
		ordered[i].DeviceID = deviceID
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].DeviceSequence < ordered[j].DeviceSequence })

	out := make([]ReceiptRow, len(ordered))
	var highest int64
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+"|"+deviceID); err != nil {
			return err
		}
		for i, r := range ordered {
			row := tx.QueryRow(ctx, `SELECT `+receiptColumns+` FROM time_receipt WHERE tenant_id=$1 AND device_id=$2 AND device_sequence=$3`, tenant, deviceID, r.DeviceSequence)
			existing, err := scanReceiptRow(row)
			if err == nil {
				out[i] = existing
				continue
			}
			if !errors.Is(err, dbport.ErrNoRows) {
				return err
			}
			payload := r.Payload
			if payload == nil {
				payload = json.RawMessage("{}")
			}
			_, err = tx.Exec(ctx,
				`INSERT INTO time_receipt(tenant_id,device_id,device_sequence,status,reason,observation_id,payload) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`,
				tenant, deviceID, r.DeviceSequence, r.Status, r.Reason, nullIfEmpty(r.ObservationID), []byte(payload))
			if err != nil {
				return err
			}
			r.Payload = payload
			out[i] = r
			eventPayload, err := json.Marshal(r)
			if err != nil {
				return err
			}
			if err := appendOutbox(ctx, tx, tenant, "clock.punch."+strings.ToLower(r.Status), 1, eventPayload); err != nil {
				return err
			}
		}

		var cursor int64
		err := tx.QueryRow(ctx, `SELECT highest_contiguous FROM time_receipt_cursor WHERE tenant_id=$1 AND device_id=$2`, tenant, deviceID).Scan(&cursor)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return err
		}

		rows, err := tx.Query(ctx,
			`SELECT device_sequence FROM time_receipt WHERE tenant_id=$1 AND device_id=$2 AND device_sequence>$3 ORDER BY device_sequence LIMIT $4`,
			tenant, deviceID, cursor, receiptCursorLookahead)
		if err != nil {
			return err
		}
		next := cursor + 1
		for rows.Next() {
			var seq int64
			if err := rows.Scan(&seq); err != nil {
				rows.Close()
				return err
			}
			if seq != next {
				break
			}
			cursor = next
			next++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		highest = cursor

		_, err = tx.Exec(ctx,
			`INSERT INTO time_receipt_cursor(tenant_id,device_id,highest_contiguous) VALUES($1,$2,$3) ON CONFLICT (tenant_id,device_id) DO UPDATE SET highest_contiguous=$3, updated_at=now()`,
			tenant, deviceID, highest)
		return err
	})
	return out, highest, err
}

// DeviceCursor returns the highest device_sequence contiguously
// acknowledged from 1 for a device, without submitting a new batch.
func (s *Store) DeviceCursor(ctx context.Context, tenant, deviceID string) (int64, error) {
	if tenant == "" || deviceID == "" {
		return 0, ErrInvalid
	}
	var cursor int64
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT highest_contiguous FROM time_receipt_cursor WHERE tenant_id=$1 AND device_id=$2`, tenant, deviceID).Scan(&cursor)
		if errors.Is(err, dbport.ErrNoRows) {
			cursor = 0
			return nil
		}
		return err
	})
	return cursor, err
}
