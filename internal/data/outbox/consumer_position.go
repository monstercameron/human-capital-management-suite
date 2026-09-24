package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// ConsumerEvent is the source identity a consumer must admit exactly once.
// EventID is the semantic dedupe key; stream and sequence are the ordering
// fence used to reject gaps and reordered delivery.
type ConsumerEvent struct {
	Tenant        uuid.UUID
	ConsumerGroup string
	StreamKey     string
	Partition     string
	Sequence      int64
	EventID       uuid.UUID
	// CompensatesEventID links a compensating event to the original event
	// whose effect it reverses. Process admits the compensation only after
	// that original is durably present in consumer_dedupe for this tenant
	// and consumer group.
	CompensatesEventID uuid.UUID
	SchemaRef          string
	Digest             string
	SourceWatermark    int64
}

// Position is the durable progress of one consumer group on one source.
type Position struct {
	Tenant          uuid.UUID
	ConsumerGroup   string
	StreamKey       string
	Partition       string
	Sequence        int64
	Digest          string
	SchemaRef       string
	SourceWatermark int64
	Status          string
	UpdatedAt       time.Time
}

// ProcessResult distinguishes a redelivery no-op from a newly admitted event.
type ProcessResult struct {
	Position  Position
	Duplicate bool
}

var (
	ErrConsumerInvalid = errors.New("outbox: invalid consumer event")
	ErrConsumerGap     = errors.New("outbox: consumer position gap")
	ErrConsumerOrder   = errors.New("outbox: consumer position out of order")
	// ErrConsumerCompensationPending means the original event has not yet
	// been admitted. The transaction is rolled back, leaving the compensation
	// eligible for redelivery after its original is processed.
	ErrConsumerCompensationPending = errors.New("outbox: compensating event is waiting for its original")
)

// ConsumerHandler applies the event's projection/effect inside the same
// transaction as the dedupe row and position. It must not call Commit or
// Rollback on tx.
type ConsumerHandler func(context.Context, dbport.Tx, ConsumerEvent) error

// Process admits one event atomically. A crash before Commit rolls back both
// the handler's rows and the dedupe/position update; a redelivery then safely
// retries the whole unit. A duplicate event ID is committed as a no-op.
func Process(ctx context.Context, db dbport.Beginner, event ConsumerEvent, handler ConsumerHandler) (ProcessResult, error) {
	if event.Partition == "" {
		event.Partition = "default"
	}
	if err := validateConsumerEvent(event); err != nil {
		return ProcessResult{}, err
	}
	if db == nil || handler == nil {
		return ProcessResult{}, fmt.Errorf("%w: database and handler are required", ErrConsumerInvalid)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("outbox: consumer begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := tenancy.WithTenant(ctx, tx, event.Tenant); err != nil {
		return ProcessResult{}, fmt.Errorf("outbox: consumer scope: %w", err)
	}
	if err := ensureConsumerPosition(ctx, tx, event); err != nil {
		return ProcessResult{}, err
	}
	position, err := loadConsumerPosition(ctx, tx, event, true)
	if err != nil {
		return ProcessResult{}, err
	}

	var duplicate bool
	var seen uuid.UUID
	err = tx.QueryRow(ctx, `SELECT event_id FROM consumer_dedupe WHERE tenant_id=$1 AND consumer_group=$2 AND event_id=$3`, event.Tenant, event.ConsumerGroup, event.EventID).Scan(&seen)
	if err == nil {
		duplicate = true
	} else if !errors.Is(err, dbport.ErrNoRows) {
		return ProcessResult{}, fmt.Errorf("outbox: consumer dedupe lookup: %w", err)
	}
	if !duplicate {
		if event.CompensatesEventID != uuid.Nil {
			applied, lookupErr := consumerEventApplied(ctx, tx, event.Tenant, event.ConsumerGroup, event.CompensatesEventID)
			if lookupErr != nil {
				return ProcessResult{}, lookupErr
			}
			if !applied {
				return ProcessResult{}, fmt.Errorf("outbox: compensation %s: %w", event.EventID, ErrConsumerCompensationPending)
			}
		}
		if event.Sequence > position.Sequence+1 {
			return ProcessResult{}, fmt.Errorf("%w: stream %s is at %d, received %d", ErrConsumerGap, event.StreamKey, position.Sequence, event.Sequence)
		}
		if event.Sequence <= position.Sequence {
			return ProcessResult{}, fmt.Errorf("%w: stream %s is at %d, received %d", ErrConsumerOrder, event.StreamKey, position.Sequence, event.Sequence)
		}
		if err := handler(ctx, tx, event); err != nil {
			return ProcessResult{}, fmt.Errorf("outbox: consumer handler: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO consumer_dedupe (tenant_id, consumer_group, event_id, stream_key, source_partition, source_sequence, schema_ref, event_digest, processed_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())`, event.Tenant, event.ConsumerGroup, event.EventID, event.StreamKey, event.Partition, event.Sequence, event.SchemaRef, event.Digest); err != nil {
			return ProcessResult{}, fmt.Errorf("outbox: record consumer dedupe: %w", err)
		}
		position, err = updateConsumerPosition(ctx, tx, event)
		if err != nil {
			return ProcessResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ProcessResult{}, fmt.Errorf("outbox: consumer commit: %w", err)
	}
	committed = true
	return ProcessResult{Position: position, Duplicate: duplicate}, nil
}

func validateConsumerEvent(event ConsumerEvent) error {
	if event.Tenant == uuid.Nil || event.ConsumerGroup == "" || event.StreamKey == "" || event.EventID == uuid.Nil || event.SchemaRef == "" || event.Digest == "" {
		return fmt.Errorf("%w: tenant, group, stream, event id, schema and digest are required", ErrConsumerInvalid)
	}
	if event.Sequence < 1 || event.SourceWatermark < event.Sequence {
		return fmt.Errorf("%w: sequence and source watermark are invalid", ErrConsumerInvalid)
	}
	if event.CompensatesEventID != uuid.Nil && event.CompensatesEventID == event.EventID {
		return fmt.Errorf("%w: an event cannot compensate itself", ErrConsumerInvalid)
	}
	return nil
}

func consumerEventApplied(ctx context.Context, q dbport.Querier, tenant uuid.UUID, group string, eventID uuid.UUID) (bool, error) {
	var found uuid.UUID
	err := q.QueryRow(ctx, `SELECT event_id FROM consumer_dedupe WHERE tenant_id=$1 AND consumer_group=$2 AND event_id=$3`, tenant, group, eventID).Scan(&found)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, dbport.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("outbox: compensation original lookup: %w", err)
}

func ensureConsumerPosition(ctx context.Context, tx dbport.Tx, event ConsumerEvent) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO consumer_position (tenant_id, consumer_group, stream_key, source_partition)
		VALUES ($1,$2,$3,$4) ON CONFLICT (tenant_id, consumer_group, stream_key, source_partition) DO NOTHING`,
		event.Tenant, event.ConsumerGroup, event.StreamKey, event.Partition)
	if err != nil {
		return fmt.Errorf("outbox: ensure consumer position: %w", err)
	}
	return nil
}

func loadConsumerPosition(ctx context.Context, q dbport.Querier, event ConsumerEvent, lock bool) (Position, error) {
	query := `SELECT tenant_id, consumer_group, stream_key, source_partition, source_sequence, source_digest, source_schema_ref, source_watermark, status, updated_at FROM consumer_position WHERE tenant_id=$1 AND consumer_group=$2 AND stream_key=$3 AND source_partition=$4`
	if lock {
		query += ` FOR UPDATE`
	}
	var p Position
	var digest, schema *string
	if err := q.QueryRow(ctx, query, event.Tenant, event.ConsumerGroup, event.StreamKey, event.Partition).Scan(&p.Tenant, &p.ConsumerGroup, &p.StreamKey, &p.Partition, &p.Sequence, &digest, &schema, &p.SourceWatermark, &p.Status, &p.UpdatedAt); err != nil {
		return Position{}, fmt.Errorf("outbox: load consumer position: %w", err)
	}
	if digest != nil {
		p.Digest = *digest
	}
	if schema != nil {
		p.SchemaRef = *schema
	}
	return p, nil
}

func updateConsumerPosition(ctx context.Context, tx dbport.Tx, event ConsumerEvent) (Position, error) {
	if _, err := tx.Exec(ctx, `
		UPDATE consumer_position SET source_sequence=$5, source_digest=$6, source_schema_ref=$7, source_watermark=$8, status='CURRENT', updated_at=now()
		WHERE tenant_id=$1 AND consumer_group=$2 AND stream_key=$3 AND source_partition=$4`,
		event.Tenant, event.ConsumerGroup, event.StreamKey, event.Partition, event.Sequence, event.Digest, event.SchemaRef, event.SourceWatermark); err != nil {
		return Position{}, fmt.Errorf("outbox: update consumer position: %w", err)
	}
	return loadConsumerPosition(ctx, tx, event, false)
}

// ReadPosition reads progress without changing it.
func ReadPosition(ctx context.Context, q dbport.Querier, tenant uuid.UUID, group, stream, partition string) (Position, error) {
	if partition == "" {
		partition = "default"
	}
	return loadConsumerPosition(ctx, q, ConsumerEvent{Tenant: tenant, ConsumerGroup: group, StreamKey: stream, Partition: partition}, false)
}
