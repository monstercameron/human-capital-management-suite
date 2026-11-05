package custom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Operation is the closed custom-object operation vocabulary. Read and query
// are exposed in the capability surface but are not mutations accepted by
// Commit.
type Operation string

const (
	OperationCreate  Operation = "CREATE"
	OperationRead    Operation = "READ"
	OperationChange  Operation = "CHANGE"
	OperationCorrect Operation = "CORRECT"
	OperationRetire  Operation = "RETIRE"
	OperationQuery   Operation = "QUERY"
)

func (o Operation) valid() bool {
	return o == OperationCreate || o == OperationRead || o == OperationChange || o == OperationCorrect || o == OperationRetire || o == OperationQuery
}

// Valid reports whether o belongs to the closed lifecycle vocabulary.
func (o Operation) Valid() bool { return o.valid() }

// EventType is the canonical event vocabulary projected by this package.
type EventType string

const (
	EventObjectCreated   EventType = "CUSTOM_OBJECT_CREATED"
	EventObjectChanged   EventType = "CUSTOM_OBJECT_CHANGED"
	EventObjectCorrected EventType = "CUSTOM_OBJECT_CORRECTED"
	EventObjectRetired   EventType = "CUSTOM_OBJECT_RETIRED"
)

// Event is a complete rebuildable custom-object fact. A mutation carries a
// complete revision rather than a patch, so replay never depends on mutable
// live state or map iteration order.
type Event struct {
	Tenant            values.TenantId
	ObjectID          string
	Kind              string
	Namespace         string
	DefinitionDigest  string
	DefinitionVersion uint64
	Operation         Operation
	Type              EventType
	Revision          uint64
	RecordRevision    uint64
	Record            CustomRecordRevision
	Retired           bool
	EventID           string
	PreviousEventID   string
	OccurredAt        time.Time
	EffectiveAt       time.Time
	EvidenceDigest    string
	Digest            string
}

// OutboxEntry is the durable intent emitted in the same commit as an event
// and its projection. It contains identifiers and a digest, never raw field
// values.
type OutboxEntry struct {
	ID            string
	Tenant        values.TenantId
	ObjectID      string
	EventID       string
	Effect        string
	OrderingKey   string
	SchemaRef     string
	PayloadDigest string
	Status        string
}

const (
	OutboxPending        = "PENDING"
	CustomEventSchemaRef = "hcmnext.domains.custom.object-event/v1"
	CustomProjectionName = "custom_object"
)

// MutationRequest is the caller input to one governed custom-object event.
// Authorization, purpose and evidence are explicit; none is inferred from
// the fact that a caller can reach this package.
type MutationRequest struct {
	Tenant           values.TenantId
	ObjectID         string
	Definition       CustomObjectDefinition
	Record           CustomRecordRevision
	Operation        Operation
	ExpectedHead     int64
	ExpectedRevision uint64
	Actor            string
	Purpose          string
	EvidenceDigest   string
	GrantedDomains   map[string]bool
	OccurredAt       time.Time
	EffectiveAt      time.Time
}

// CommitReceipt names every durable result of one atomic event commit.
type CommitReceipt struct {
	Event    Event
	Outbox   OutboxEntry
	Head     int64
	Replayed bool
}

// DefinitionPort is the additive persistence seam for the customstore lane.
// Implementations return an immutable definition revision for a tenant.
type DefinitionPort interface {
	LoadObjectDefinition(context.Context, string, string, string, uint64) (CustomObjectDefinition, error)
}

// LoadDefinition reads a tenant-bound definition through the persistence port.
// Keeping this adapter at the domain boundary lets the customstore lane
// provide PostgreSQL without coupling lifecycle code to a database package.
func LoadDefinition(ctx context.Context, source DefinitionPort, tenant, kind, namespace string, version uint64) (CustomObjectDefinition, error) {
	if source == nil {
		return CustomObjectDefinition{}, fmt.Errorf("%w: definition port is required", ErrInvalidMutation)
	}
	return source.LoadObjectDefinition(ctx, tenant, kind, namespace, version)
}

// EventProjectionPort is the persistence contract consumed by workflow and
// serving callers. A database adapter can implement it with ledger.AppendMulti
// and the projection read-barrier; MemoryStore is the reference fake.
type EventProjectionPort interface {
	Commit(context.Context, MutationRequest) (CommitReceipt, error)
	Rebuild(context.Context, values.TenantId, string, string) (ProjectionReport, error)
	Search(context.Context, SearchRequest) (SearchReport, error)
}

var (
	ErrInvalidMutation = errors.New("custom: invalid mutation")
	ErrStaleHead       = errors.New("custom: stale stream head")
	ErrStaleRevision   = errors.New("custom: stale object revision")
	ErrProjectionStale = errors.New("custom: projection is stale")
	ErrAlreadyRetired  = errors.New("custom: object already retired")
	ErrNotFound        = errors.New("custom: object not found")
	ErrDuplicateEvent  = errors.New("custom: duplicate event")
)

// MemoryStore is a concurrency-safe, tenant-isolated, atomic reference
// implementation. The lock is the transaction boundary: validation happens
// before state is changed, then the event, projection, and outbox are
// published together.
type EventStore struct {
	mu       sync.RWMutex
	heads    map[string]int64
	events   map[string][]Event
	records  map[string]Event
	outbox   []OutboxEntry
	seenKeys map[string]CommitReceipt
}

var _ EventProjectionPort = (*EventStore)(nil)

func NewEventStore() *EventStore {
	return &EventStore{
		heads: make(map[string]int64), events: make(map[string][]Event),
		records: make(map[string]Event), seenKeys: make(map[string]CommitReceipt),
	}
}

// NewMemoryEventStore is the explicit test-double spelling used by callers
// that also use the definition MemoryStore from the persistence port.
func NewMemoryEventStore() *EventStore { return NewEventStore() }

// Commit appends one canonical event and applies its projection atomically.
func (s *EventStore) Commit(ctx context.Context, req MutationRequest) (CommitReceipt, error) {
	if err := ctx.Err(); err != nil {
		return CommitReceipt{}, err
	}
	if s == nil {
		return CommitReceipt{}, fmt.Errorf("%w: nil store", ErrInvalidMutation)
	}
	if err := validateMutation(req); err != nil {
		return CommitReceipt{}, err
	}
	stream := streamKey(req.Tenant, req.Kind(), req.ObjectID)
	key := idempotencyKey(req)

	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.seenKeys[key]; ok {
		return cloneReceipt(prior, true), nil
	}
	head := s.heads[stream]
	if head != req.ExpectedHead {
		return CommitReceipt{}, fmt.Errorf("%w: stream=%s expected=%d actual=%d", ErrStaleHead, stream, req.ExpectedHead, head)
	}
	current, exists := s.records[eventRecordKey(req.Tenant, req.Kind(), req.ObjectID)]
	if req.Operation == OperationCreate {
		if exists {
			return CommitReceipt{}, fmt.Errorf("%w: object %s", ErrInvalidMutation, req.ObjectID)
		}
	} else {
		if !exists {
			return CommitReceipt{}, fmt.Errorf("%w: object %s", ErrNotFound, req.ObjectID)
		}
		if current.Revision != req.ExpectedRevision {
			return CommitReceipt{}, fmt.Errorf("%w: object=%s expected=%d actual=%d", ErrStaleRevision, req.ObjectID, req.ExpectedRevision, current.Revision)
		}
		if current.Retired {
			return CommitReceipt{}, fmt.Errorf("%w: object %s", ErrAlreadyRetired, req.ObjectID)
		}
	}

	revision := uint64(1)
	if exists {
		revision = current.RecordRevision + 1
	}
	event := eventFrom(req, head+1, revision)
	if priorEvents := s.events[stream]; len(priorEvents) > 0 {
		event.PreviousEventID = priorEvents[len(priorEvents)-1].EventID
	}
	event.Digest = eventDigest(event)
	outbox := OutboxEntry{
		ID: event.EventID, Tenant: event.Tenant, ObjectID: event.ObjectID,
		EventID: event.EventID, Effect: "custom-object." + strings.ToLower(string(event.Operation)),
		OrderingKey: stream, SchemaRef: CustomEventSchemaRef, PayloadDigest: event.Digest,
		Status: OutboxPending,
	}
	// The only mutations below are the commit point. All values are detached.
	s.events[stream] = append(s.events[stream], cloneEvent(event))
	s.heads[stream] = head + 1
	s.records[eventRecordKey(req.Tenant, req.Kind(), req.ObjectID)] = cloneEvent(event)
	s.outbox = append(s.outbox, outbox)
	receipt := CommitReceipt{Event: cloneEvent(event), Outbox: outbox, Head: head + 1}
	s.seenKeys[key] = receipt
	return receipt, nil
}

// Rebuild replays one object stream and verifies the live projection digest.
func (s *EventStore) Rebuild(ctx context.Context, tenant values.TenantId, kind, objectID string) (ProjectionReport, error) {
	if err := ctx.Err(); err != nil {
		return ProjectionReport{}, err
	}
	if tenant == "" || kind == "" || objectID == "" {
		return ProjectionReport{}, fmt.Errorf("%w: tenant, kind and object id are required", ErrInvalidMutation)
	}
	stream := streamKey(tenant, kind, objectID)
	s.mu.RLock()
	events := append([]Event(nil), s.events[stream]...)
	live, ok := s.records[eventRecordKey(tenant, kind, objectID)]
	s.mu.RUnlock()
	rebuilt, err := rebuildEvents(events, tenant, kind, objectID)
	if err != nil {
		return ProjectionReport{}, err
	}
	if !ok {
		return ProjectionReport{}, fmt.Errorf("%w: object %s", ErrNotFound, objectID)
	}
	if rebuilt.Digest != live.Digest {
		return ProjectionReport{}, fmt.Errorf("custom: projection divergence for %s", objectID)
	}
	return rebuilt, nil
}

// Events returns detached canonical events for inspection and replay tests.
func (s *EventStore) Events(tenant values.TenantId, kind, objectID string) []Event {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := s.events[streamKey(tenant, kind, objectID)]
	out := make([]Event, len(items))
	for i := range items {
		out[i] = cloneEvent(items[i])
	}
	return out
}

// Outbox returns a detached view of all pending effect intents.
func (s *EventStore) Outbox() []OutboxEntry {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]OutboxEntry(nil), s.outbox...)
}

func validateMutation(req MutationRequest) error {
	if req.Tenant == "" || req.ObjectID == "" || !req.Operation.valid() || req.Actor == "" || req.Purpose == "" || req.EvidenceDigest == "" {
		return fmt.Errorf("%w: tenant, object, operation, actor, purpose and evidence are required", ErrInvalidMutation)
	}
	if req.Operation == OperationRead || req.Operation == OperationQuery {
		return fmt.Errorf("%w: read and query are not mutations", ErrInvalidMutation)
	}
	if err := req.Definition.Validate(); err != nil {
		return err
	}
	policy, err := ResolvePolicy(req.Definition)
	if err != nil || !policy.CanAuthorize(req.GrantedDomains) {
		return fmt.Errorf("%w: actor lacks every field authz domain", ErrInvalidMutation)
	}
	if req.Record.ObjectID != req.ObjectID {
		return fmt.Errorf("%w: record object id mismatch", ErrInvalidMutation)
	}
	if err := req.Record.Validate(req.Definition); err != nil {
		return err
	}
	if req.ExpectedHead < 0 {
		return fmt.Errorf("%w: expected head cannot be negative", ErrInvalidMutation)
	}
	if req.Operation == OperationCreate && req.ExpectedRevision != 0 {
		return fmt.Errorf("%w: create expected revision must be zero", ErrInvalidMutation)
	}
	if req.Operation != OperationCreate && req.ExpectedRevision == 0 {
		return fmt.Errorf("%w: existing object expected revision is required", ErrInvalidMutation)
	}
	if req.OccurredAt.IsZero() || req.EffectiveAt.IsZero() {
		return fmt.Errorf("%w: occurred and effective times are required", ErrInvalidMutation)
	}
	return nil
}

func eventFrom(req MutationRequest, sequence int64, revision uint64) Event {
	typ := EventObjectChanged
	if req.Operation == OperationCreate {
		typ = EventObjectCreated
	}
	if req.Operation == OperationCorrect {
		typ = EventObjectCorrected
	}
	if req.Operation == OperationRetire {
		typ = EventObjectRetired
	}
	record := req.Record
	return Event{Tenant: req.Tenant, ObjectID: req.ObjectID, Kind: req.Definition.Kind,
		Namespace: req.Definition.Namespace, DefinitionDigest: req.Definition.Digest(),
		DefinitionVersion: req.Definition.Version, Operation: req.Operation, Type: typ,
		Revision: revision, RecordRevision: revision, Record: cloneEventRecord(record), Retired: req.Operation == OperationRetire,
		EventID: eventIDFor(req, sequence), OccurredAt: req.OccurredAt.UTC(), EffectiveAt: req.EffectiveAt.UTC(),
		EvidenceDigest: req.EvidenceDigest, Digest: fmt.Sprintf("sequence:%d", sequence)}
}

func eventDigest(e Event) string {
	w := canonicalbytes.New(CustomEventSchemaRef, 1)
	w.String("tenant", e.Tenant.String()).String("object_id", e.ObjectID).
		String("kind", e.Kind).String("namespace", e.Namespace).
		String("definition_digest", e.DefinitionDigest).Int("definition_version", int64(e.DefinitionVersion)).
		String("operation", string(e.Operation)).String("type", string(e.Type)).Int("revision", int64(e.Revision)).
		String("record_digest", e.Record.Digest()).Bool("retired", e.Retired).
		String("event_id", e.EventID).String("previous_event_id", e.PreviousEventID).
		String("occurred_at", e.OccurredAt.Format(time.RFC3339Nano)).String("effective_at", e.EffectiveAt.Format(time.RFC3339Nano)).
		String("evidence_digest", e.EvidenceDigest)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// ProjectionReport is a deterministic semantic replay result with source
// freshness and schema metadata suitable for read barriers and reports.
type ProjectionReport struct {
	Tenant        values.TenantId
	Kind          string
	ObjectID      string
	SourceHead    int64
	SchemaVersion uint64
	Digest        string
	Retired       bool
	EventCount    int
}

func rebuildEvents(events []Event, tenant values.TenantId, kind, objectID string) (ProjectionReport, error) {
	if len(events) == 0 {
		return ProjectionReport{}, fmt.Errorf("%w: object %s", ErrNotFound, objectID)
	}
	var last Event
	for i, event := range events {
		if event.Tenant != tenant || event.Kind != kind || event.ObjectID != objectID || event.Operation == OperationRead {
			return ProjectionReport{}, fmt.Errorf("%w: foreign or invalid event at %d", ErrInvalidMutation, i)
		}
		if event.Digest == "" || event.Digest != eventDigest(event) {
			return ProjectionReport{}, fmt.Errorf("%w: event %d digest mismatch", ErrInvalidMutation, i)
		}
		if event.Revision == 0 || (i > 0 && event.Revision != last.Revision+1) {
			return ProjectionReport{}, fmt.Errorf("%w: non-contiguous object revision at %d", ErrStaleRevision, i)
		}
		if i > 0 && event.PreviousEventID != last.EventID {
			return ProjectionReport{}, fmt.Errorf("%w: broken event chain", ErrInvalidMutation)
		}
		last = event
	}
	return ProjectionReport{Tenant: tenant, Kind: kind, ObjectID: objectID, SourceHead: int64(len(events)),
		SchemaVersion: last.DefinitionVersion, Digest: last.Digest, Retired: last.Retired, EventCount: len(events)}, nil
}

func streamKey(tenant values.TenantId, kind, objectID string) string {
	return tenant.String() + "/custom/" + kind + "/" + objectID
}
func eventRecordKey(tenant values.TenantId, kind, objectID string) string {
	return tenant.String() + "\x00" + kind + "\x00" + objectID
}
func idempotencyKey(req MutationRequest) string {
	return req.Tenant.String() + "\x00" + req.Kind() + "\x00" + req.ObjectID + "\x00" + string(req.Operation) + "\x00" + fmt.Sprint(req.ExpectedHead) + "\x00" + req.EvidenceDigest + "\x00" + req.Record.Digest()
}
func (r MutationRequest) Kind() string { return r.Definition.Kind }

func eventIDFor(req MutationRequest, sequence int64) string {
	return digestStrings([]string{req.Tenant.String(), req.Kind(), req.ObjectID, string(req.Operation), fmt.Sprint(sequence), req.Record.Digest(), req.EvidenceDigest})
}

func cloneEventRecord(in CustomRecordRevision) CustomRecordRevision {
	fields := make(map[string]TypedValue, len(in.FieldValues))
	for key, value := range in.FieldValues {
		fields[key] = value
	}
	in.FieldValues = fields
	return in
}
func cloneEvent(in Event) Event { in.Record = cloneEventRecord(in.Record); return in }
func cloneReceipt(in CommitReceipt, replayed bool) CommitReceipt {
	in.Event = cloneEvent(in.Event)
	in.Replayed = replayed
	return in
}

// DefinitionDigest is a digest-only helper used by clients and reports.
func DefinitionDigest(def CustomObjectDefinition) string {
	sum := sha256.Sum256(def.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain returns a redaction-safe contract summary.
func Explain() string {
	return "custom lifecycle: canonical tenant-scoped events, atomic rebuildable projection, pending outbox intents, governed mutations"
}

// Version is the lifecycle contract version.
func Version() int { return 1 }
