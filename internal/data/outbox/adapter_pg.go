package outbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// BrokerStore is the minimal database capability the Postgres broker
// backend needs. A dbport.Conn, a dbport.Tx and the pgtest handle all
// satisfy it.
//
// Production DDL for broker_record and broker_checkpoint is intentionally
// not shipped here: table creation is a migration owned by the
// orchestrator (with RLS and storage-disposition rows, which a lane must
// not author). Setup issues the same statements inside the caller's own
// schema for tests and operators.
type BrokerStore interface {
	dbport.Execer
	dbport.Querier
}

// PostgresBroker is the durable Broker backend over PostgreSQL. It
// implements the identical neutral port as MemoryBroker: per-partition
// offsets, effect-identity dedupe, monotonic checkpoints, bounded
// retries with dead-letter parking, and replay. It is safe for
// concurrent use over any store: one mutex multiplexes single
// connections (a bare dbport.Conn is not goroutine-safe), while a
// pool-backed store still gets connection-level parallelism across
// broker instances. Concurrent publishers on one partition retry the
// offset assignment a bounded number of times.
type PostgresBroker struct {
	mu          sync.Mutex
	db          BrokerStore
	maxAttempts int
	maxPoll     int
	clock       func() time.Time
}

// NewPostgresBroker validates one broker policy. Call Setup once before
// publishing.
func NewPostgresBroker(db BrokerStore, policy BrokerPolicy) (*PostgresBroker, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: database is required", ErrBrokerInvalid)
	}
	if policy.MaxAttempts < 1 || policy.MaxPollLimit < 1 {
		return nil, fmt.Errorf("outbox: NewPostgresBroker: %w", ErrBrokerPolicy)
	}
	clock := policy.Clock
	if clock == nil {
		clock = time.Now
	}
	return &PostgresBroker{db: db, maxAttempts: policy.MaxAttempts, maxPoll: policy.MaxPollLimit, clock: clock}, nil
}

// Setup creates the broker tables when they are absent. It is
// test and operator tooling, not a migration.
func (b *PostgresBroker) Setup(ctx context.Context) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS broker_record (
			tenant_id uuid NOT NULL,
			stream_key text NOT NULL,
			stream_partition text NOT NULL,
			partition_offset bigint NOT NULL,
			effect_identity text NOT NULL,
			ordering_key text NOT NULL DEFAULT '',
			payload bytea NOT NULL,
			schema_ref text NOT NULL DEFAULT '',
			attempts int NOT NULL DEFAULT 0,
			dlq boolean NOT NULL DEFAULT FALSE,
			dlq_reason text NOT NULL DEFAULT '',
			enqueued_at timestamptz NOT NULL,
			PRIMARY KEY (tenant_id, stream_key, stream_partition, partition_offset),
			UNIQUE (tenant_id, stream_key, effect_identity)
		)`,
		`CREATE TABLE IF NOT EXISTS broker_checkpoint (
			tenant_id uuid NOT NULL,
			consumer text NOT NULL,
			stream_key text NOT NULL,
			stream_partition text NOT NULL,
			partition_offset bigint NOT NULL,
			updated_at timestamptz NOT NULL,
			PRIMARY KEY (tenant_id, consumer, stream_key, stream_partition)
		)`,
	} {
		if _, err := b.db.Exec(ctx, statement); err != nil {
			return fmt.Errorf("outbox: broker setup: %w", err)
		}
	}
	return nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key value")
}

const brokerRecordColumns = `partition_offset, effect_identity, ordering_key, payload, schema_ref, attempts, dlq, dlq_reason, enqueued_at`

// Publish appends one record, assigning the next per-partition offset.
// Republishing an effect identity converges on the recorded offset with
// Duplicate set. Concurrent publishers on one partition retry the
// assignment; anything else fails fast.
func (b *PostgresBroker) Publish(ctx context.Context, request PublishRequest) (StreamRecord, error) {
	if err := validatePublish(request); err != nil {
		return StreamRecord{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock().UTC()
	for attempt := 0; attempt < 8; attempt++ {
		var offset int64
		err := b.db.QueryRow(ctx, `WITH next AS (
				SELECT COALESCE(MAX(partition_offset), 0) + 1 AS o FROM broker_record
				WHERE tenant_id = $1 AND stream_key = $2 AND stream_partition = $3
			)
			INSERT INTO broker_record (tenant_id, stream_key, stream_partition, partition_offset, effect_identity, ordering_key, payload, schema_ref, enqueued_at)
			SELECT $1, $2, $3, o, $4, $5, $6, $7, $8 FROM next
			ON CONFLICT (tenant_id, stream_key, effect_identity) DO NOTHING
			RETURNING partition_offset`,
			request.Tenant, request.Stream, request.Partition,
			request.EffectIdentity, request.OrderingKey, request.Payload, request.SchemaRef, now).Scan(&offset)
		if err == nil {
			return sealRecord(StreamRecord{
				Tenant: request.Tenant, Stream: request.Stream, Partition: request.Partition,
				Offset: offset, EffectIdentity: request.EffectIdentity, OrderingKey: request.OrderingKey,
				Payload: append([]byte(nil), request.Payload...), SchemaRef: request.SchemaRef, EnqueuedAt: now,
			}), nil
		}
		if errors.Is(err, dbport.ErrNoRows) {
			return b.byEffect(ctx, request.Tenant, request.Stream, request.EffectIdentity, true)
		}
		if !isUniqueViolation(err) {
			return StreamRecord{}, fmt.Errorf("outbox: broker publish: %w", err)
		}
	}
	return StreamRecord{}, fmt.Errorf("outbox: broker publish: %w", ErrBrokerOffset)
}

func (b *PostgresBroker) byEffect(ctx context.Context, tenant uuid.UUID, stream, identity string, duplicate bool) (StreamRecord, error) {
	var record StreamRecord
	var offset int64
	var attempts int
	var payload []byte
	var enqueued time.Time
	record.Tenant, record.Stream = tenant, stream
	err := b.db.QueryRow(ctx, `SELECT partition_offset, stream_partition, effect_identity, ordering_key, payload, schema_ref, attempts, dlq, dlq_reason, enqueued_at
		FROM broker_record WHERE tenant_id = $1 AND stream_key = $2 AND effect_identity = $3`,
		tenant, stream, identity).Scan(&offset, &record.Partition, &record.EffectIdentity, &record.OrderingKey, &payload, &record.SchemaRef, &attempts, &record.DLQ, &record.DLQReason, &enqueued)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return StreamRecord{}, fmt.Errorf("outbox: broker publish race: %w", ErrBrokerOffset)
		}
		return StreamRecord{}, fmt.Errorf("outbox: broker publish: %w", err)
	}
	record.Offset, record.Attempts = offset, attempts
	record.Payload = append([]byte(nil), payload...)
	record.EnqueuedAt = enqueued.UTC()
	record.Duplicate = duplicate
	return sealRecord(record), nil
}

func (b *PostgresBroker) watermark(ctx context.Context, tenant uuid.UUID, stream, partition string) (int64, error) {
	var high *int64
	if err := b.db.QueryRow(ctx, `SELECT MAX(partition_offset) FROM broker_record
		WHERE tenant_id = $1 AND stream_key = $2 AND stream_partition = $3`, tenant, stream, partition).Scan(&high); err != nil {
		return 0, fmt.Errorf("outbox: broker watermark: %w", err)
	}
	if high == nil {
		return 0, nil
	}
	return *high, nil
}

func (b *PostgresBroker) readRange(ctx context.Context, tenant uuid.UUID, stream, partition string, from int64, limit int) ([]StreamRecord, error) {
	if err := validateScope(tenant, stream, partition); err != nil {
		return nil, err
	}
	if from < 0 {
		return nil, fmt.Errorf("%w: negative offset", ErrBrokerInvalid)
	}
	if limit < 1 || limit > b.maxPoll {
		return nil, fmt.Errorf("%w: limit is outside the poll bound", ErrBrokerInvalid)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	rows, err := b.db.Query(ctx, `SELECT `+brokerRecordColumns+` FROM broker_record
		WHERE tenant_id = $1 AND stream_key = $2 AND stream_partition = $3 AND partition_offset > $4
		ORDER BY partition_offset LIMIT $5`, tenant, stream, partition, from, limit)
	if err != nil {
		return nil, fmt.Errorf("outbox: broker poll: %w", err)
	}
	defer rows.Close()
	var out []StreamRecord
	for rows.Next() {
		var offset int64
		var record StreamRecord
		var payload []byte
		var enqueued time.Time
		record.Tenant, record.Stream, record.Partition = tenant, stream, partition
		if err := rows.Scan(&offset, &record.EffectIdentity, &record.OrderingKey, &payload, &record.SchemaRef, &record.Attempts, &record.DLQ, &record.DLQReason, &enqueued); err != nil {
			return nil, fmt.Errorf("outbox: broker poll: %w", err)
		}
		record.Offset = offset
		record.Payload = append([]byte(nil), payload...)
		record.EnqueuedAt = enqueued.UTC()
		out = append(out, sealRecord(record))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("outbox: broker poll: %w", err)
	}
	if len(out) == 0 && from > 0 {
		high, err := b.watermark(ctx, tenant, stream, partition)
		if err != nil {
			return nil, err
		}
		if from > high {
			return nil, fmt.Errorf("%w: %d beyond watermark %d", ErrBrokerOffset, from, high)
		}
	}
	return out, nil
}

// Poll reads the records after one offset, in partition order.
func (b *PostgresBroker) Poll(ctx context.Context, tenant uuid.UUID, consumer, stream, partition string, after int64, limit int) ([]StreamRecord, error) {
	if strings.TrimSpace(consumer) == "" {
		return nil, fmt.Errorf("%w: consumer is required", ErrBrokerInvalid)
	}
	return b.readRange(ctx, tenant, stream, partition, after, limit)
}

// Replay rereads from an earlier offset for reprocessing.
func (b *PostgresBroker) Replay(ctx context.Context, tenant uuid.UUID, consumer, stream, partition string, from int64, limit int) ([]StreamRecord, error) {
	if strings.TrimSpace(consumer) == "" {
		return nil, fmt.Errorf("%w: consumer is required", ErrBrokerInvalid)
	}
	return b.readRange(ctx, tenant, stream, partition, from, limit)
}

// CommitCheckpoint records consumer progress monotonically: committing
// behind the recorded offset is a rewind and is refused, and no
// checkpoint passes the partition high-watermark.
func (b *PostgresBroker) CommitCheckpoint(ctx context.Context, checkpoint ConsumerCheckpoint) (ConsumerCheckpoint, error) {
	if err := validateScope(checkpoint.Tenant, checkpoint.Stream, checkpoint.Partition); err != nil {
		return ConsumerCheckpoint{}, err
	}
	if strings.TrimSpace(checkpoint.Consumer) == "" || checkpoint.Consumer != strings.TrimSpace(checkpoint.Consumer) {
		return ConsumerCheckpoint{}, fmt.Errorf("%w: consumer is required exact, without padding", ErrBrokerInvalid)
	}
	if checkpoint.Offset < 0 {
		return ConsumerCheckpoint{}, fmt.Errorf("%w: negative offset", ErrBrokerInvalid)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	high, err := b.watermark(ctx, checkpoint.Tenant, checkpoint.Stream, checkpoint.Partition)
	if err != nil {
		return ConsumerCheckpoint{}, err
	}
	if checkpoint.Offset > high {
		return ConsumerCheckpoint{}, fmt.Errorf("%w: %d beyond watermark %d", ErrBrokerOffset, checkpoint.Offset, high)
	}
	now := b.clock().UTC()
	var storedOffset int64
	var updated time.Time
	err = b.db.QueryRow(ctx, `INSERT INTO broker_checkpoint (tenant_id, consumer, stream_key, stream_partition, partition_offset, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, consumer, stream_key, stream_partition) DO UPDATE
		SET partition_offset = EXCLUDED.partition_offset, updated_at = EXCLUDED.updated_at
		WHERE broker_checkpoint.partition_offset <= EXCLUDED.partition_offset
		RETURNING partition_offset, updated_at`,
		checkpoint.Tenant, checkpoint.Consumer, checkpoint.Stream, checkpoint.Partition, checkpoint.Offset, now).Scan(&storedOffset, &updated)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			var current int64
			lookup := b.db.QueryRow(ctx, `SELECT partition_offset FROM broker_checkpoint
				WHERE tenant_id = $1 AND consumer = $2 AND stream_key = $3 AND stream_partition = $4`,
				checkpoint.Tenant, checkpoint.Consumer, checkpoint.Stream, checkpoint.Partition)
			if scanErr := lookup.Scan(&current); scanErr != nil {
				return ConsumerCheckpoint{}, fmt.Errorf("outbox: broker checkpoint: %w", scanErr)
			}
			return ConsumerCheckpoint{}, fmt.Errorf("%w: %d behind %d", ErrCheckpointRewind, checkpoint.Offset, current)
		}
		return ConsumerCheckpoint{}, fmt.Errorf("outbox: broker checkpoint: %w", err)
	}
	return ConsumerCheckpoint{Tenant: checkpoint.Tenant, Consumer: checkpoint.Consumer, Stream: checkpoint.Stream, Partition: checkpoint.Partition, Offset: storedOffset, UpdatedAt: updated.UTC()}, nil
}

// ReadCheckpoint reads progress without moving it.
func (b *PostgresBroker) ReadCheckpoint(ctx context.Context, tenant uuid.UUID, consumer, stream, partition string) (ConsumerCheckpoint, bool, error) {
	if strings.TrimSpace(consumer) == "" {
		return ConsumerCheckpoint{}, false, fmt.Errorf("%w: consumer is required", ErrBrokerInvalid)
	}
	if err := validateScope(tenant, stream, partition); err != nil {
		return ConsumerCheckpoint{}, false, err
	}
	var offset int64
	var updated time.Time
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.db.QueryRow(ctx, `SELECT partition_offset, updated_at FROM broker_checkpoint
		WHERE tenant_id = $1 AND consumer = $2 AND stream_key = $3 AND stream_partition = $4`,
		tenant, consumer, stream, partition).Scan(&offset, &updated)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ConsumerCheckpoint{}, false, nil
		}
		return ConsumerCheckpoint{}, false, fmt.Errorf("outbox: broker checkpoint: %w", err)
	}
	return ConsumerCheckpoint{Tenant: tenant, Consumer: consumer, Stream: stream, Partition: partition, Offset: offset, UpdatedAt: updated.UTC()}, true, nil
}

// Fail records one failed delivery, parking the record once it exhausts
// the attempt budget. A parked record stays parked without new attempts.
func (b *PostgresBroker) Fail(ctx context.Context, tenant uuid.UUID, stream, effectIdentity, reason string) (bool, error) {
	if tenant == uuid.Nil || strings.TrimSpace(stream) == "" || strings.TrimSpace(effectIdentity) == "" {
		return false, fmt.Errorf("%w: tenant, stream and effect identity are required", ErrBrokerInvalid)
	}
	if strings.TrimSpace(reason) == "" {
		return false, fmt.Errorf("%w: failure reason is required", ErrBrokerInvalid)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var attempts int
	var dlq bool
	err := b.db.QueryRow(ctx, `UPDATE broker_record
		SET attempts = attempts + 1,
			dlq = attempts + 1 >= $5,
			dlq_reason = CASE WHEN attempts + 1 >= $5 THEN $4 ELSE dlq_reason END
		WHERE tenant_id = $1 AND stream_key = $2 AND effect_identity = $3 AND NOT dlq
		RETURNING attempts, dlq`,
		tenant, stream, effectIdentity, reason, b.maxAttempts).Scan(&attempts, &dlq)
	if err != nil {
		if !errors.Is(err, dbport.ErrNoRows) {
			return false, fmt.Errorf("outbox: broker fail: %w", err)
		}
		var parked bool
		lookup := b.db.QueryRow(ctx, `SELECT dlq FROM broker_record
			WHERE tenant_id = $1 AND stream_key = $2 AND effect_identity = $3`, tenant, stream, effectIdentity)
		if scanErr := lookup.Scan(&parked); scanErr != nil {
			if errors.Is(scanErr, dbport.ErrNoRows) {
				return false, fmt.Errorf("%w: %s", ErrBrokerUnknown, effectIdentity)
			}
			return false, fmt.Errorf("outbox: broker fail: %w", scanErr)
		}
		return false, nil
	}
	_ = attempts
	return dlq, nil
}

// DeadLetters lists parked records in partition order.
func (b *PostgresBroker) DeadLetters(ctx context.Context, tenant uuid.UUID, stream, partition string, limit int) ([]StreamRecord, error) {
	if err := validateScope(tenant, stream, partition); err != nil {
		return nil, err
	}
	if limit < 1 || limit > b.maxPoll {
		return nil, fmt.Errorf("%w: limit is outside the poll bound", ErrBrokerInvalid)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	rows, err := b.db.Query(ctx, `SELECT `+brokerRecordColumns+` FROM broker_record
		WHERE tenant_id = $1 AND stream_key = $2 AND stream_partition = $3 AND dlq
		ORDER BY partition_offset LIMIT $4`, tenant, stream, partition, limit)
	if err != nil {
		return nil, fmt.Errorf("outbox: broker dead letters: %w", err)
	}
	defer rows.Close()
	var out []StreamRecord
	for rows.Next() {
		var offset int64
		var record StreamRecord
		var payload []byte
		var enqueued time.Time
		record.Tenant, record.Stream, record.Partition = tenant, stream, partition
		if err := rows.Scan(&offset, &record.EffectIdentity, &record.OrderingKey, &payload, &record.SchemaRef, &record.Attempts, &record.DLQ, &record.DLQReason, &enqueued); err != nil {
			return nil, fmt.Errorf("outbox: broker dead letters: %w", err)
		}
		record.Offset = offset
		record.Payload = append([]byte(nil), payload...)
		record.EnqueuedAt = enqueued.UTC()
		out = append(out, sealRecord(record))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("outbox: broker dead letters: %w", err)
	}
	return out, nil
}
