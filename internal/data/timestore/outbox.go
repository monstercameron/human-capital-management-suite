package timestore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// OutboxEvent is one committed clock/timecard event row. Sequence is a
// per-tenant, globally increasing identity assigned by time_outbox, so a
// subscriber's cursor is a single comparable integer.
type OutboxEvent struct {
	TenantID, EventType string
	Sequence            int64
	SchemaVersion       int
	Payload             json.RawMessage
	CreatedAt           time.Time
}

// appendOutbox inserts one outbox event inside the caller's transaction, so
// it commits atomically with the state change it describes (TCLOCK-012's
// "written in the same tx" requirement). Callers in sessions.go,
// observations.go and receipts.go all route through this one insert.
func appendOutbox(ctx context.Context, tx dbport.Tx, tenant, eventType string, schemaVersion int, payload []byte) error {
	if tenant == "" || eventType == "" || schemaVersion <= 0 {
		return ErrInvalid
	}
	if payload == nil {
		payload = []byte("{}")
	}
	if !json.Valid(payload) {
		return ErrInvalid
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO time_outbox(tenant_id,id,event_type,schema_version,payload) VALUES($1,$2,$3,$4,$5::jsonb)`,
		tenant, uuid.NewString(), eventType, schemaVersion, payload)
	return err
}

// ListEvents returns a bounded, ordered page of a tenant's outbox events
// strictly after afterCursor (0 starts from the beginning). The order is
// stable across calls because sequence only ever increases.
func (s *Store) ListEvents(ctx context.Context, tenant string, afterCursor int64, limit int) ([]OutboxEvent, error) {
	if tenant == "" || afterCursor < 0 || limit <= 0 || limit > 1000 {
		return nil, ErrInvalid
	}
	out := make([]OutboxEvent, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT tenant_id,sequence,event_type,schema_version,payload,created_at FROM time_outbox WHERE tenant_id=$1 AND sequence>$2 ORDER BY sequence LIMIT $3`,
			tenant, afterCursor, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e OutboxEvent
			var b []byte
			if err := rows.Scan(&e.TenantID, &e.Sequence, &e.EventType, &e.SchemaVersion, &b, &e.CreatedAt); err != nil {
				return err
			}
			e.Payload = append(json.RawMessage(nil), b...)
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}
