package outbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrBrokerInvalid reports a broker request that cannot be recorded:
	// a missing tenant, stream, partition or effect identity, padded
	// identifiers, an empty payload, or an unusable offset or limit.
	ErrBrokerInvalid = errors.New("outbox: invalid broker record")

	// ErrBrokerOffset reports a poll or replay beyond the durable
	// high-watermark or with a negative offset.
	ErrBrokerOffset = errors.New("outbox: broker offset out of range")

	// ErrCheckpointRewind reports a checkpoint behind the recorded
	// offset: consumer progress never rewinds through commit.
	ErrCheckpointRewind = errors.New("outbox: consumer checkpoint rewind refused")

	// ErrBrokerUnknown reports a retry or checkpoint against an identity
	// the broker never recorded.
	ErrBrokerUnknown = errors.New("outbox: unknown broker identity")

	// ErrBrokerPolicy reports a broker policy that cannot bound work: a
	// non-positive attempt budget or poll limit.
	ErrBrokerPolicy = errors.New("outbox: invalid broker policy")
)

// BrokerPolicy bounds one broker backend. MaxAttempts caps deliveries
// per record before it parks in the dead-letter queue; MaxPollLimit
// caps one poll or replay. No broker-specific offset or topic semantics
// appear here: domain and workflow code programs this port only, so a
// migration between backends cannot change ordering or dedupe results.
type BrokerPolicy struct {
	MaxAttempts  int
	MaxPollLimit int
	Clock        func() time.Time
}

// PublishRequest is one logical event. Partition is the partition/order
// key: records sharing it stay ordered; records on different partitions
// are independent. EffectIdentity dedupes: republishing it returns the
// recorded offset with Duplicate set instead of a second record.
type PublishRequest struct {
	Tenant         uuid.UUID
	Stream         string
	Partition      string
	EffectIdentity string
	OrderingKey    string
	Payload        []byte
	SchemaRef      string
}

// StreamRecord is one durable logical event. Offset is the per-partition
// sequence, starting at 1. Attempts counts deliveries; DLQ parks the
// record once attempts exhaust the policy. Duplicate is transport state
// set on redelivery and never part of the seal.
type StreamRecord struct {
	Tenant         uuid.UUID
	Stream         string
	Partition      string
	Offset         int64
	EffectIdentity string
	OrderingKey    string
	Payload        []byte
	SchemaRef      string
	Attempts       int
	DLQ            bool
	DLQReason      string
	EnqueuedAt     time.Time
	Duplicate      bool
	Digest         string
}

// ConsumerCheckpoint is one consumer's durable progress on one
// partition: every offset at or below Offset is done.
type ConsumerCheckpoint struct {
	Tenant    uuid.UUID
	Consumer  string
	Stream    string
	Partition string
	Offset    int64
	UpdatedAt time.Time
}

// Broker is the broker-neutral event port. MemoryBroker implements it
// hermetically with OSS-broker partitioned-log semantics, and
// PostgresBroker implements it durably over PostgreSQL; both pass the
// identical conformance suite, so neither broker's native semantics can
// leak into domain or workflow code.
type Broker interface {
	Publish(ctx context.Context, record PublishRequest) (StreamRecord, error)
	Poll(ctx context.Context, tenant uuid.UUID, consumer, stream, partition string, after int64, limit int) ([]StreamRecord, error)
	CommitCheckpoint(ctx context.Context, checkpoint ConsumerCheckpoint) (ConsumerCheckpoint, error)
	ReadCheckpoint(ctx context.Context, tenant uuid.UUID, consumer, stream, partition string) (ConsumerCheckpoint, bool, error)
	// Replay rereads from an earlier offset: at or behind the committed
	// checkpoint for reprocessing, never a checkpoint rewind.
	Replay(ctx context.Context, tenant uuid.UUID, consumer, stream, partition string, from int64, limit int) ([]StreamRecord, error)
	// Fail records one failed delivery. It reports moved=true once the
	// record exhausts the attempt budget and parks in the dead-letter
	// queue; further failures stay parked without new attempts.
	Fail(ctx context.Context, tenant uuid.UUID, stream, effectIdentity, reason string) (moved bool, err error)
	DeadLetters(ctx context.Context, tenant uuid.UUID, stream, partition string, limit int) ([]StreamRecord, error)
}

func validatePublish(request PublishRequest) error {
	if request.Tenant == uuid.Nil {
		return fmt.Errorf("%w: tenant is required", ErrBrokerInvalid)
	}
	for _, field := range []struct {
		name, value string
	}{{"stream", request.Stream}, {"partition", request.Partition}, {"effect_identity", request.EffectIdentity}} {
		if strings.TrimSpace(field.value) == "" || field.value != strings.TrimSpace(field.value) {
			return fmt.Errorf("%w: %s is required exact, without padding", ErrBrokerInvalid, field.name)
		}
	}
	if len(request.Payload) == 0 {
		return fmt.Errorf("%w: payload is required", ErrBrokerInvalid)
	}
	return nil
}

func validateScope(tenant uuid.UUID, stream, partition string) error {
	if tenant == uuid.Nil {
		return fmt.Errorf("%w: tenant is required", ErrBrokerInvalid)
	}
	if strings.TrimSpace(stream) == "" || stream != strings.TrimSpace(stream) {
		return fmt.Errorf("%w: stream is required exact, without padding", ErrBrokerInvalid)
	}
	if strings.TrimSpace(partition) == "" || partition != strings.TrimSpace(partition) {
		return fmt.Errorf("%w: partition is required exact, without padding", ErrBrokerInvalid)
	}
	return nil
}

func sealRecord(record StreamRecord) StreamRecord {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		record.Tenant.String(), record.Stream, record.Partition,
		fmt.Sprintf("%d", record.Offset), record.EffectIdentity, record.OrderingKey,
		hex.EncodeToString(record.Payload), record.SchemaRef,
	}, "\x00")))
	record.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return record
}

// VerifyRecord checks one record's seal. Duplicate and Attempts are
// transport and delivery state, never covered by the seal; DLQ parking
// reseals through Fail, so a parked record verifies under its own seal.
func VerifyRecord(record StreamRecord) error {
	if record.Digest == "" {
		return fmt.Errorf("%w: record is not sealed", ErrBrokerInvalid)
	}
	quiet := record
	quiet.Duplicate = false
	quiet.Attempts = 0
	if sealRecord(quiet).Digest != record.Digest {
		return fmt.Errorf("%w: record seal mismatch", ErrBrokerInvalid)
	}
	return nil
}

func copyRecord(record StreamRecord) StreamRecord {
	out := record
	out.Payload = append([]byte(nil), record.Payload...)
	return out
}

// MemoryBroker is the hermetic Broker backend: a partitioned in-process
// log with OSS-broker semantics (per-partition offsets, consumer-group
// checkpoints, bounded retries, dead-letter queue, replay). It is the
// conformance test double every durable backend must agree with. It is
// safe for concurrent use.
type MemoryBroker struct {
	mu          sync.Mutex
	maxAttempts int
	maxPoll     int
	clock       func() time.Time
	log         map[string][]StreamRecord
	byEffect    map[string]effectRef
	checkpoints map[string]ConsumerCheckpoint
}

// effectRef locates one record by partition and index. The log slices
// reallocate on append, so the index deliberately stores no pointer.
type effectRef struct {
	partition string
	index     int
}

func brokerPartitionKey(tenant uuid.UUID, stream, partition string) string {
	return tenant.String() + "\x00" + stream + "\x00" + partition
}

func brokerEffectKey(tenant uuid.UUID, stream, identity string) string {
	return tenant.String() + "\x00" + stream + "\x00" + identity
}

func brokerCheckpointKey(tenant uuid.UUID, consumer, stream, partition string) string {
	return tenant.String() + "\x00" + consumer + "\x00" + stream + "\x00" + partition
}

// NewMemoryBroker validates one broker policy.
func NewMemoryBroker(policy BrokerPolicy) (*MemoryBroker, error) {
	if policy.MaxAttempts < 1 || policy.MaxPollLimit < 1 {
		return nil, fmt.Errorf("outbox: NewMemoryBroker: %w", ErrBrokerPolicy)
	}
	clock := policy.Clock
	if clock == nil {
		clock = time.Now
	}
	return &MemoryBroker{
		maxAttempts: policy.MaxAttempts, maxPoll: policy.MaxPollLimit, clock: clock,
		log: make(map[string][]StreamRecord), byEffect: make(map[string]effectRef),
		checkpoints: make(map[string]ConsumerCheckpoint),
	}, nil
}

// Publish appends one record, assigning the next per-partition offset.
// Republishing an effect identity converges on the recorded offset.
func (b *MemoryBroker) Publish(ctx context.Context, request PublishRequest) (StreamRecord, error) {
	if err := validatePublish(request); err != nil {
		return StreamRecord{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	identity := brokerEffectKey(request.Tenant, request.Stream, request.EffectIdentity)
	if ref, ok := b.byEffect[identity]; ok {
		redelivery := copyRecord(b.log[ref.partition][ref.index])
		redelivery.Duplicate = true
		return redelivery, nil
	}
	partition := brokerPartitionKey(request.Tenant, request.Stream, request.Partition)
	record := sealRecord(StreamRecord{
		Tenant: request.Tenant, Stream: request.Stream, Partition: request.Partition,
		Offset: int64(len(b.log[partition])) + 1, EffectIdentity: request.EffectIdentity,
		OrderingKey: request.OrderingKey, Payload: append([]byte(nil), request.Payload...),
		SchemaRef: request.SchemaRef, EnqueuedAt: b.clock().UTC(),
	})
	b.log[partition] = append(b.log[partition], record)
	b.byEffect[identity] = effectRef{partition: partition, index: len(b.log[partition]) - 1}
	return copyRecord(record), nil
}

func (b *MemoryBroker) readPartition(tenant uuid.UUID, stream, partition string, from int64, limit int) ([]StreamRecord, error) {
	if err := validateScope(tenant, stream, partition); err != nil {
		return nil, err
	}
	if from < 0 {
		return nil, fmt.Errorf("%w: negative offset", ErrBrokerInvalid)
	}
	if limit < 1 || limit > b.maxPoll {
		return nil, fmt.Errorf("%w: limit is outside the poll bound", ErrBrokerInvalid)
	}
	entries := b.log[brokerPartitionKey(tenant, stream, partition)]
	if from > int64(len(entries)) {
		return nil, fmt.Errorf("%w: %d beyond watermark %d", ErrBrokerOffset, from, len(entries))
	}
	out := make([]StreamRecord, 0, limit)
	for _, record := range entries[from:] {
		if len(out) == limit {
			break
		}
		out = append(out, copyRecord(record))
	}
	return out, nil
}

// Poll reads the records after one offset, in partition order.
func (b *MemoryBroker) Poll(ctx context.Context, tenant uuid.UUID, consumer, stream, partition string, after int64, limit int) ([]StreamRecord, error) {
	if strings.TrimSpace(consumer) == "" {
		return nil, fmt.Errorf("%w: consumer is required", ErrBrokerInvalid)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.readPartition(tenant, stream, partition, after, limit)
}

// Replay rereads from an earlier offset for reprocessing.
func (b *MemoryBroker) Replay(ctx context.Context, tenant uuid.UUID, consumer, stream, partition string, from int64, limit int) ([]StreamRecord, error) {
	if strings.TrimSpace(consumer) == "" {
		return nil, fmt.Errorf("%w: consumer is required", ErrBrokerInvalid)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.readPartition(tenant, stream, partition, from, limit)
}

// CommitCheckpoint records consumer progress. Progress is monotonic:
// committing behind the recorded offset is a rewind and is refused.
// Replay, not checkpoint commit, reprocesses history.
func (b *MemoryBroker) CommitCheckpoint(ctx context.Context, checkpoint ConsumerCheckpoint) (ConsumerCheckpoint, error) {
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
	high := int64(len(b.log[brokerPartitionKey(checkpoint.Tenant, checkpoint.Stream, checkpoint.Partition)]))
	if checkpoint.Offset > high {
		return ConsumerCheckpoint{}, fmt.Errorf("%w: %d beyond watermark %d", ErrBrokerOffset, checkpoint.Offset, high)
	}
	key := brokerCheckpointKey(checkpoint.Tenant, checkpoint.Consumer, checkpoint.Stream, checkpoint.Partition)
	if current, ok := b.checkpoints[key]; ok && checkpoint.Offset < current.Offset {
		return ConsumerCheckpoint{}, fmt.Errorf("%w: %d behind %d", ErrCheckpointRewind, checkpoint.Offset, current.Offset)
	}
	stored := ConsumerCheckpoint{Tenant: checkpoint.Tenant, Consumer: checkpoint.Consumer, Stream: checkpoint.Stream, Partition: checkpoint.Partition, Offset: checkpoint.Offset, UpdatedAt: b.clock().UTC()}
	b.checkpoints[key] = stored
	return stored, nil
}

// ReadCheckpoint reads progress without moving it.
func (b *MemoryBroker) ReadCheckpoint(ctx context.Context, tenant uuid.UUID, consumer, stream, partition string) (ConsumerCheckpoint, bool, error) {
	if strings.TrimSpace(consumer) == "" {
		return ConsumerCheckpoint{}, false, fmt.Errorf("%w: consumer is required", ErrBrokerInvalid)
	}
	if err := validateScope(tenant, stream, partition); err != nil {
		return ConsumerCheckpoint{}, false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	checkpoint, ok := b.checkpoints[brokerCheckpointKey(tenant, consumer, stream, partition)]
	return checkpoint, ok, nil
}

// Fail records one failed delivery, parking the record in the
// dead-letter queue once it exhausts the attempt budget. A parked
// record stays parked: further failures report moved=false without
// minting new attempts, so retries are always bounded.
func (b *MemoryBroker) Fail(ctx context.Context, tenant uuid.UUID, stream, effectIdentity, reason string) (bool, error) {
	if tenant == uuid.Nil || strings.TrimSpace(stream) == "" || strings.TrimSpace(effectIdentity) == "" {
		return false, fmt.Errorf("%w: tenant, stream and effect identity are required", ErrBrokerInvalid)
	}
	if strings.TrimSpace(reason) == "" {
		return false, fmt.Errorf("%w: failure reason is required", ErrBrokerInvalid)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	ref, ok := b.byEffect[brokerEffectKey(tenant, stream, effectIdentity)]
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrBrokerUnknown, effectIdentity)
	}
	record := &b.log[ref.partition][ref.index]
	if record.DLQ {
		return false, nil
	}
	record.Attempts++
	if record.Attempts >= b.maxAttempts {
		record.DLQ = true
		record.DLQReason = reason
		*record = sealRecord(*record)
		return true, nil
	}
	return false, nil
}

// DeadLetters lists parked records in partition order.
func (b *MemoryBroker) DeadLetters(ctx context.Context, tenant uuid.UUID, stream, partition string, limit int) ([]StreamRecord, error) {
	if err := validateScope(tenant, stream, partition); err != nil {
		return nil, err
	}
	if limit < 1 || limit > b.maxPoll {
		return nil, fmt.Errorf("%w: limit is outside the poll bound", ErrBrokerInvalid)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []StreamRecord
	for _, record := range b.log[brokerPartitionKey(tenant, stream, partition)] {
		if record.DLQ {
			out = append(out, copyRecord(record))
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// BrokerSnapshot is the durable state a restarted broker resumes from.
type BrokerSnapshot struct {
	Records     []StreamRecord
	Checkpoints []ConsumerCheckpoint
}

// Snapshot exports the durable broker state.
func (b *MemoryBroker) Snapshot() BrokerSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	snapshot := BrokerSnapshot{}
	for _, entries := range b.log {
		for _, record := range entries {
			quiet := copyRecord(record)
			quiet.Duplicate = false
			snapshot.Records = append(snapshot.Records, quiet)
		}
	}
	for _, checkpoint := range b.checkpoints {
		snapshot.Checkpoints = append(snapshot.Checkpoints, checkpoint)
	}
	sort.Slice(snapshot.Records, func(i, j int) bool {
		a, c := snapshot.Records[i], snapshot.Records[j]
		if a.Stream != c.Stream {
			return a.Stream < c.Stream
		}
		if a.Partition != c.Partition {
			return a.Partition < c.Partition
		}
		return a.Offset < c.Offset
	})
	sort.Slice(snapshot.Checkpoints, func(i, j int) bool {
		a, c := snapshot.Checkpoints[i], snapshot.Checkpoints[j]
		if a.Consumer != c.Consumer {
			return a.Consumer < c.Consumer
		}
		if a.Stream != c.Stream {
			return a.Stream < c.Stream
		}
		return a.Partition < c.Partition
	})
	return snapshot
}

// ResumeBroker rebuilds a broker from durable state, verifying every
// seal before trusting it, so a restart resumes offsets, checkpoints,
// retries and dead letters exactly.
func ResumeBroker(snapshot BrokerSnapshot, policy BrokerPolicy) (*MemoryBroker, error) {
	broker, err := NewMemoryBroker(policy)
	if err != nil {
		return nil, err
	}
	for _, record := range snapshot.Records {
		if record.Offset < 1 {
			return nil, fmt.Errorf("%w: stored offset %d", ErrBrokerInvalid, record.Offset)
		}
		if err := VerifyRecord(record); err != nil {
			return nil, err
		}
		partition := brokerPartitionKey(record.Tenant, record.Stream, record.Partition)
		entries := broker.log[partition]
		if int64(len(entries))+1 != record.Offset {
			return nil, fmt.Errorf("%w: stored offset %d after %d records", ErrBrokerOffset, record.Offset, len(entries))
		}
		key := brokerEffectKey(record.Tenant, record.Stream, record.EffectIdentity)
		if _, ok := broker.byEffect[key]; ok {
			return nil, fmt.Errorf("%w: duplicate stored identity %s", ErrBrokerInvalid, record.EffectIdentity)
		}
		stored := copyRecord(record)
		stored.Duplicate = false
		broker.log[partition] = append(entries, stored)
		broker.byEffect[key] = effectRef{partition: partition, index: len(broker.log[partition]) - 1}
	}
	for _, checkpoint := range snapshot.Checkpoints {
		if _, _, err := broker.ReadCheckpoint(context.Background(), checkpoint.Tenant, checkpoint.Consumer, checkpoint.Stream, checkpoint.Partition); err != nil {
			return nil, err
		}
		key := brokerCheckpointKey(checkpoint.Tenant, checkpoint.Consumer, checkpoint.Stream, checkpoint.Partition)
		if _, ok := broker.checkpoints[key]; ok {
			return nil, fmt.Errorf("%w: duplicate stored checkpoint", ErrBrokerInvalid)
		}
		high := int64(len(broker.log[brokerPartitionKey(checkpoint.Tenant, checkpoint.Stream, checkpoint.Partition)]))
		if checkpoint.Offset < 0 || checkpoint.Offset > high {
			return nil, fmt.Errorf("%w: stored checkpoint %d beyond watermark %d", ErrBrokerOffset, checkpoint.Offset, high)
		}
		broker.checkpoints[key] = checkpoint
	}
	return broker, nil
}
