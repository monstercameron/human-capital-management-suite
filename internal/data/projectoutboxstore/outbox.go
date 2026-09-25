// Package projectoutboxstore provides bounded, ordered pull and durable
// acknowledgement for project events. Callers supply a tenant-scoped
// transaction so cursor movement and any consumer effects can be atomic.
package projectoutboxstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

const MaxBatchSize = 100

var (
	ErrInvalidRequest   = errors.New("invalid project outbox request")
	ErrSequenceConflict = errors.New("project outbox sequence conflict")
)

type Event struct {
	ID             string
	TenantID       string
	ProjectID      string
	Sequence       int64
	Type           string
	SchemaVersion  int
	SourceRevision int64
	Classification string
	Payload        json.RawMessage
}

// Envelope is the stable JSON contract stored in each outbox payload.
type Envelope struct {
	EventID         string          `json:"eventId"`
	TenantID        string          `json:"tenantId"`
	ProjectID       string          `json:"projectId"`
	AggregateID     string          `json:"aggregateId"`
	EventType       string          `json:"eventType"`
	SourceRevision  int64           `json:"sourceRevision"`
	ProjectSequence int64           `json:"projectSequence"`
	SchemaVersion   int             `json:"schemaVersion"`
	Classification  string          `json:"classification"`
	Value           json.RawMessage `json:"value"`
}

// Pull returns events after the caller's exclusive sequence, in project
// order. The caller must persist its cursor with Ack after processing.
func Pull(ctx context.Context, tx dbport.Tx, tenantID, projectID string, after int64, limit int) ([]Event, error) {
	if tx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(projectID) == "" || after < 0 || limit < 1 || limit > MaxBatchSize {
		return nil, ErrInvalidRequest
	}
	rows, err := tx.Query(ctx, `SELECT event_id,tenant_id,project_id,project_sequence,event_type,schema_version,source_revision,classification,payload
		FROM project_outbox WHERE tenant_id=$1 AND project_id=$2 AND project_sequence>$3
		ORDER BY project_sequence LIMIT $4`, tenantID, projectID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.TenantID, &event.ProjectID, &event.Sequence, &event.Type, &event.SchemaVersion, &event.SourceRevision, &event.Classification, &event.Payload); err != nil {
			return nil, err
		}
		if event.Sequence != after+int64(len(out))+1 || event.SchemaVersion < 1 || !json.Valid(event.Payload) {
			return nil, ErrSequenceConflict
		}
		var envelope Envelope
		if err := json.Unmarshal(event.Payload, &envelope); err != nil {
			return nil, ErrSequenceConflict
		}
		if envelope.EventID != event.ID || envelope.TenantID != event.TenantID || envelope.ProjectID != event.ProjectID || envelope.AggregateID == "" || envelope.EventType != event.Type || envelope.SourceRevision != event.SourceRevision || envelope.ProjectSequence != event.Sequence || envelope.SchemaVersion != event.SchemaVersion || envelope.Classification != event.Classification || !json.Valid(envelope.Value) {
			return nil, ErrSequenceConflict
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Ack advances one consumer's durable project cursor by exactly one event.
// Repeating an already committed acknowledgement is safe and has no effect.
func Ack(ctx context.Context, tx dbport.Tx, tenantID, projectID, consumer string, event Event) error {
	if tx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(consumer) == "" || event.ID == "" || event.TenantID != tenantID || event.ProjectID != projectID || event.Sequence < 1 {
		return ErrInvalidRequest
	}
	var storedID string
	if err := tx.QueryRow(ctx, `SELECT event_id FROM project_outbox WHERE tenant_id=$1 AND project_id=$2 AND project_sequence=$3`, tenantID, projectID, event.Sequence).Scan(&storedID); err != nil {
		return err
	}
	if storedID != event.ID {
		return ErrSequenceConflict
	}
	var last int64
	err := tx.QueryRow(ctx, `INSERT INTO project_outbox_cursor(tenant_id,project_id,consumer,last_sequence) VALUES($1,$2,$3,0)
		ON CONFLICT(tenant_id,project_id,consumer) DO UPDATE SET updated_at=now() RETURNING last_sequence`, tenantID, projectID, consumer).Scan(&last)
	if err != nil {
		return err
	}
	if event.Sequence <= last {
		return nil
	}
	if event.Sequence != last+1 {
		return ErrSequenceConflict
	}
	_, err = tx.Exec(ctx, `UPDATE project_outbox_cursor SET last_sequence=$1,updated_at=now() WHERE tenant_id=$2 AND project_id=$3 AND consumer=$4 AND last_sequence=$5`, event.Sequence, tenantID, projectID, consumer, last)
	return err
}

// Cursor returns the committed position, or zero before the first ack.
func Cursor(ctx context.Context, tx dbport.Tx, tenantID, projectID, consumer string) (int64, error) {
	if tx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(consumer) == "" {
		return 0, ErrInvalidRequest
	}
	var sequence int64
	err := tx.QueryRow(ctx, `SELECT last_sequence FROM project_outbox_cursor WHERE tenant_id=$1 AND project_id=$2 AND consumer=$3`, tenantID, projectID, consumer).Scan(&sequence)
	if errors.Is(err, dbport.ErrNoRows) {
		return 0, nil
	}
	return sequence, err
}
