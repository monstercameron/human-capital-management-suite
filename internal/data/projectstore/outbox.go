package projectstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

const ProjectEventSchemaVersion = 1

// OutboxEvent is the versioned event contract shared by project-owned stores.
type OutboxEvent struct {
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

type AppendOutboxEvent struct {
	TenantID, ProjectID, AggregateID, EventType, Classification string
	SourceRevision                                              int64
	Value                                                       any
}

var ErrInvalidOutboxEvent = errors.New("invalid project outbox event")

// AppendOutboxEventTx appends one globally ordered project event within the
// caller's tenant transaction. Its sequence allocation, payload, and row are
// atomic with all other effects in that transaction.
func AppendOutboxEventTx(ctx context.Context, tx dbport.Tx, event AppendOutboxEvent) (OutboxEvent, error) {
	if tx == nil || strings.TrimSpace(event.TenantID) == "" || strings.TrimSpace(event.ProjectID) == "" || strings.TrimSpace(event.AggregateID) == "" || strings.TrimSpace(event.EventType) == "" || strings.TrimSpace(event.Classification) == "" || event.SourceRevision <= 0 {
		return OutboxEvent{}, ErrInvalidOutboxEvent
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `UPDATE project SET event_sequence=event_sequence+1,updated_at=now() WHERE tenant_id=$1 AND id=$2 RETURNING event_sequence`, event.TenantID, event.ProjectID).Scan(&sequence); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return OutboxEvent{}, ErrNotFound
		}
		return OutboxEvent{}, err
	}
	value, err := json.Marshal(event.Value)
	if err != nil {
		return OutboxEvent{}, err
	}
	envelope := OutboxEvent{
		EventID: uuid.NewString(), TenantID: event.TenantID, ProjectID: event.ProjectID,
		AggregateID: event.AggregateID, EventType: event.EventType, SourceRevision: event.SourceRevision,
		ProjectSequence: sequence, SchemaVersion: ProjectEventSchemaVersion,
		Classification: event.Classification, Value: value,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return OutboxEvent{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_outbox(tenant_id,project_id,event_id,project_sequence,event_type,schema_version,source_revision,classification,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`, envelope.TenantID, envelope.ProjectID, envelope.EventID, envelope.ProjectSequence, envelope.EventType, envelope.SchemaVersion, envelope.SourceRevision, envelope.Classification, payload); err != nil {
		return OutboxEvent{}, err
	}
	return envelope, nil
}
