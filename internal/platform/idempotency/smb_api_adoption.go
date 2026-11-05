package idempotency

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SMB-CORE-002 adoption conformance. Expansion modules adopt the shared
// idempotency kernel through typed test-only descriptors and durable
// conformance fixtures. These fixtures are not production CRM or stock
// implementations; production consumers own their own adoption.

var (
	ErrSMBDescriptorInvalid = errors.New("smb adoption: descriptor is invalid")
	ErrSMBCommandInvalid    = errors.New("smb adoption: command is invalid")
	ErrSMBRevisionConflict  = errors.New("smb adoption: revision conflict, reload and review again")
	ErrSMBCursorRejected    = errors.New("smb adoption: query scope does not match")
	ErrSMBFilterRejected    = errors.New("smb adoption: filter or order field is not allowlisted")
	ErrSMBTokenRevoked      = errors.New("smb adoption: machine token is not authorized")
	ErrSMBEpochFenced       = errors.New("smb adoption: command epoch is stale and fenced for repair")
	ErrSMBSnapshotInvalid   = errors.New("smb adoption: snapshot is invalid")
	ErrSMBBaseNotRestored   = errors.New("smb adoption: revision base is not restored; import a snapshot first")
)

// SMBAPIAdoptionDescriptor maps the exact schema, capability, scope,
// concurrency, pagination, batch, event and retention contract one typed
// fixture adopts. It is test-only conformance evidence.
type SMBAPIAdoptionDescriptor struct {
	Name             string
	SchemaVersion    string
	Capability       string
	TenantID         string
	ResourceScope    string
	IdempotencyScope string
	FilterAllowlist  []string
	OrderAllowlist   []string
	MaxBatchItems    int
	MaxPayloadBytes  int
	PageSize         int
	EventFamily      string
	EventSchema      string
	RetryHorizon     time.Duration
	ControlEpoch     uint64
}

// Validate refuses descriptors that cannot bind exactly one typed fixture.
func (d SMBAPIAdoptionDescriptor) Validate() error {
	switch {
	case strings.TrimSpace(d.Name) == "":
		return fmt.Errorf("%w: fixture name is required", ErrSMBDescriptorInvalid)
	case !strings.Contains(d.SchemaVersion, "/v"):
		return fmt.Errorf("%w: schema version %q must be name/vN", ErrSMBDescriptorInvalid, d.SchemaVersion)
	case !strings.Contains(d.Capability, "@"):
		return fmt.Errorf("%w: capability %q must be versioned", ErrSMBDescriptorInvalid, d.Capability)
	case strings.TrimSpace(d.TenantID) == "":
		return fmt.Errorf("%w: tenant scope is required", ErrSMBDescriptorInvalid)
	case strings.TrimSpace(d.ResourceScope) == "" || strings.TrimSpace(d.IdempotencyScope) == "":
		return fmt.Errorf("%w: resource and idempotency scopes are required", ErrSMBDescriptorInvalid)
	case d.MaxBatchItems < 1 || d.MaxPayloadBytes < 1 || d.PageSize < 1:
		return fmt.Errorf("%w: batch, payload and page limits must be positive", ErrSMBDescriptorInvalid)
	case strings.TrimSpace(d.EventFamily) == "" || strings.TrimSpace(d.EventSchema) == "":
		return fmt.Errorf("%w: event family and schema are required", ErrSMBDescriptorInvalid)
	case d.RetryHorizon <= 0:
		return fmt.Errorf("%w: retry horizon must be positive", ErrSMBDescriptorInvalid)
	}
	return nil
}

// SMBOperationCommand is one typed mutation through the conformance adapter.
// The machine principal and the delegated human actor travel separately.
type SMBOperationCommand struct {
	Descriptor       SMBAPIAdoptionDescriptor
	Key              string
	Canonical        []byte
	ExpectedRevision int64
	MachineToken     string
	DelegatedActor   string
	ControlEpoch     uint64
	Now              time.Time
}

// SMBOperationResult is the single logical operation identity. Submitted is
// true only while domain completion is still unproven.
type SMBOperationResult struct {
	OperationRef     string
	Decision         Decision
	Revision         int64
	EventID          string
	Submitted        bool
	MachinePrincipal string
	DelegatedActor   string
}

// SMBBatchItemResult carries per-item status; a batch may partially complete.
type SMBBatchItemResult struct {
	Key    string
	Result SMBOperationResult
	Err    string
}

// SMBLogicalEvent is the one logical outbox event per accepted operation. It
// references safe record identities, never cross-tenant payloads.
type SMBLogicalEvent struct {
	EventID           string
	TenantID          string
	Family            string
	Schema            string
	AggregateRef      string
	AggregateRevision int64
	ControlEpoch      uint64
}

// SMBReadQuery pages fixture history over opaque tenant/filter-bound cursors.
type SMBReadQuery struct {
	Descriptor   SMBAPIAdoptionDescriptor
	Cursor       string
	Filter       map[string]string
	Order        string
	MachineToken string
	Now          time.Time
}

// SMBFixtureItem is one authorized revision mark.
type SMBFixtureItem struct {
	Revision int64
	Ref      string
}

// SMBReadPage is one bounded page with its continuation cursor.
type SMBReadPage struct {
	Items      []SMBFixtureItem
	NextCursor string
}

// SMBDeliveryReport proves duplicate/out-of-order tolerance without rollback.
type SMBDeliveryReport struct {
	Accepted    int
	Duplicates  int
	MaxRevision int64
}

// SMB view statuses. The server stays authoritative; loading is bounded and
// every terminal state names its explicit next action.
type SMBViewStatus string

const (
	SMBViewPending       SMBViewStatus = "Pending"
	SMBViewSubmitted     SMBViewStatus = "Submitted"
	SMBViewCompleted     SMBViewStatus = "Completed"
	SMBViewConflict      SMBViewStatus = "Conflict"
	SMBViewStaleRevision SMBViewStatus = "StaleRevision"
	SMBViewFenced        SMBViewStatus = "Fenced"
	SMBViewDisconnected  SMBViewStatus = "Disconnected"
)

// SMBOperationView is the served truth for one operation key: status, safe
// references, changed fields and the explicit actions the server will honor.
type SMBOperationView struct {
	Status        SMBViewStatus
	OperationRef  string
	SafeReference string
	Revision      int64
	ChangedFields []string
	Actions       []string
	Freshness     time.Time
	Note          string
}

type smbFixture struct {
	descriptor SMBAPIAdoptionDescriptor
	revision   int64
	mutations  int
	fields     map[string]string
	history    []SMBFixtureItem
}

type smbObservation struct {
	Kind          SMBViewStatus
	ChangedFields []string
	At            time.Time
}

type smbCursor struct {
	Tenant string `json:"tenant"`
	Scope  string `json:"scope"`
	Filter string `json:"filter"`
	Offset int    `json:"offset"`
}

// SMBConformanceHarness hosts typed fixtures behind the shared idempotency
// kernel. The registry may be pure (bounded tests) or durable (a store-backed
// registry converges retries across processes). A store-backed harness must Import
// a snapshot before new work so the revision base is never guessed.
type SMBConformanceHarness struct {
	mu           sync.Mutex
	registry     *Registry
	epoch        uint64
	lastContact  time.Time
	connected    bool
	fixtures     map[string]*smbFixture
	tokens       map[string]bool
	results      map[string]SMBOperationResult
	records      map[string]Record
	events       []SMBLogicalEvent
	seenEvents   map[string]bool
	subscriber   map[string]int64
	observed     map[string]smbObservation
	baseRestored bool
	fences       []string
}

// NewSMBConformanceHarness binds typed fixtures to one idempotency registry.
func NewSMBConformanceHarness(registry *Registry, now time.Time, epoch uint64) *SMBConformanceHarness {
	if registry == nil {
		registry = NewRegistry()
	}
	return &SMBConformanceHarness{
		registry: registry, epoch: epoch, lastContact: now.UTC(), connected: true,
		fixtures: make(map[string]*smbFixture), tokens: make(map[string]bool),
		results: make(map[string]SMBOperationResult), records: make(map[string]Record),
		seenEvents: make(map[string]bool), subscriber: make(map[string]int64),
		observed: make(map[string]smbObservation),
	}
}

// RegisterFixture adopts one typed descriptor. Scopes must stay distinct.
func (h *SMBConformanceHarness) RegisterFixture(d SMBAPIAdoptionDescriptor) error {
	if err := d.Validate(); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.fixtures[d.ResourceScope]; exists {
		return fmt.Errorf("%w: duplicate resource scope %q", ErrSMBDescriptorInvalid, d.ResourceScope)
	}
	for _, existing := range h.fixtures {
		if existing.descriptor.EventFamily == d.EventFamily {
			return fmt.Errorf("%w: duplicate event family %q", ErrSMBDescriptorInvalid, d.EventFamily)
		}
	}
	h.fixtures[d.ResourceScope] = &smbFixture{descriptor: d, fields: make(map[string]string)}
	return nil
}

// IssueMachineToken authorizes a machine principal; RevokeMachineToken ends it.
func (h *SMBConformanceHarness) IssueMachineToken(token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tokens[token] = true
}

// RevokeMachineToken ends a machine principal immediately.
func (h *SMBConformanceHarness) RevokeMachineToken(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.tokens, token)
}

// DisconnectStream freezes stream contact; ReconnectStream refetches truth.
func (h *SMBConformanceHarness) DisconnectStream() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connected = false
}

// ReconnectStream refetches the authorized current revision per fixture.
func (h *SMBConformanceHarness) ReconnectStream() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connected = true
	for scope, fixture := range h.fixtures {
		if fixture.revision > h.subscriber[scope] {
			h.subscriber[scope] = fixture.revision
		}
	}
}

// identity binds the kernel identity from the REGISTERED descriptor: the
// caller names only the scope, never its own capability namespace or limits.
func (h *SMBConformanceHarness) identity(registered SMBAPIAdoptionDescriptor, cmd SMBOperationCommand) Identity {
	return Identity{
		Tenant:      registered.TenantID,
		Capability:  registered.Capability,
		EffectScope: registered.IdempotencyScope,
		Key:         cmd.Key,
		Principal:   cmd.MachineToken,
	}
}

// smbEffectRevision recovers the fixture revision a completed operation
// carried into its durable record, so store-backed replays report the original
// revision instead of the local fixture's.
func smbEffectRevision(effectRef string) (int64, bool) {
	rest, ok := strings.CutPrefix(effectRef, "effect://rev:")
	if !ok {
		return 0, false
	}
	head, _, _ := strings.Cut(rest, ":")
	rev, err := strconv.ParseInt(head, 10, 64)
	if err != nil || rev < 0 {
		return 0, false
	}
	return rev, true
}

func smbOperationRef(tenant, capability, scope, key, principal string) string {
	return "op://" + digestText(tenant+"|"+capability+"|"+scope+"|"+key+"|"+principal)
}

// Execute runs one typed command: validate, authorize, fence stale epochs,
// converge retries through the shared kernel, then apply exactly one fixture
// mutation and one logical event. Every refusal lands before effects.
func (h *SMBConformanceHarness) Execute(cmd SMBOperationCommand) (SMBOperationResult, error) {
	if err := cmd.Descriptor.Validate(); err != nil {
		return SMBOperationResult{}, err
	}
	if strings.TrimSpace(cmd.Key) == "" || len(cmd.Canonical) == 0 || strings.TrimSpace(cmd.DelegatedActor) == "" || cmd.Now.IsZero() {
		return SMBOperationResult{}, fmt.Errorf("%w: key, bounded canonical payload, delegated actor and time are required", ErrSMBCommandInvalid)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.tokens[cmd.MachineToken] {
		return SMBOperationResult{}, ErrSMBTokenRevoked
	}
	if cmd.ControlEpoch != h.epoch {
		h.fences = append(h.fences, cmd.Descriptor.ResourceScope+"|"+cmd.Key)
		h.observed[cmd.Descriptor.ResourceScope+"|"+cmd.Key] = smbObservation{Kind: SMBViewFenced, At: cmd.Now.UTC()}
		return SMBOperationResult{}, ErrSMBEpochFenced
	}
	fixture, ok := h.fixtures[cmd.Descriptor.ResourceScope]
	if !ok || fixture.descriptor.TenantID != cmd.Descriptor.TenantID {
		return SMBOperationResult{}, ErrSMBCursorRejected
	}
	registered := fixture.descriptor
	if len(cmd.Canonical) > registered.MaxPayloadBytes {
		return SMBOperationResult{}, fmt.Errorf("%w: canonical payload exceeds the registered %d-byte bound", ErrSMBCommandInvalid, registered.MaxPayloadBytes)
	}
	identity := h.identity(registered, cmd)
	opKey := identityKey(identity)
	wantDigest := hashBytes(cmd.Canonical)
	if old, err := h.registry.Lookup(identity); err == nil {
		if old.RequestDigest != wantDigest {
			h.observed[cmd.Descriptor.ResourceScope+"|"+cmd.Key] = smbObservation{Kind: SMBViewConflict, At: cmd.Now.UTC()}
			return SMBOperationResult{}, ErrConflict
		}
		if old.State == Tombstone || old.State == Expired || (!cmd.Now.Before(old.ExpiresAt) && (old.Tombstone || old.ExpiryMode == RejectReuse)) {
			return SMBOperationResult{}, ErrExpired
		}
		if !cmd.Now.Before(old.ExpiresAt) && old.ExpiryMode == AllowReuse && !old.Tombstone {
			// Allow-reuse horizons fall through to a fresh reservation below,
			// matching the shared kernel's delete-and-reserve semantics.
		} else if old.State == Completed {
			if prior, found := h.results[opKey]; found {
				prior.Decision = Replay
				return prior, nil
			}
			revision := fixture.revision
			if durable, ok := smbEffectRevision(old.EffectRef); ok {
				revision = durable
			}
			return SMBOperationResult{OperationRef: smbOperationRef(identity.Tenant, identity.Capability, identity.EffectScope, identity.Key, identity.Principal), Decision: Replay, Revision: revision}, nil
		} else {
			return SMBOperationResult{OperationRef: smbOperationRef(identity.Tenant, identity.Capability, identity.EffectScope, identity.Key, identity.Principal), Decision: InFlight, Revision: fixture.revision, Submitted: true, MachinePrincipal: cmd.MachineToken, DelegatedActor: cmd.DelegatedActor}, nil
		}
	} else if !errors.Is(err, ErrNotFound) {
		return SMBOperationResult{}, err
	}
	if h.registry.store != nil && !h.baseRestored {
		return SMBOperationResult{}, ErrSMBBaseNotRestored
	}
	if cmd.ExpectedRevision != fixture.revision {
		h.observed[cmd.Descriptor.ResourceScope+"|"+cmd.Key] = smbObservation{Kind: SMBViewStaleRevision, ChangedFields: smbChangedFields(fixture.fields, cmd.Canonical), At: cmd.Now.UTC()}
		return SMBOperationResult{}, ErrSMBRevisionConflict
	}
	resolution, err := h.registry.Reserve(Request{
		Identity: identity, Layer: "smb-conformance", Canonical: cmd.Canonical, Now: cmd.Now,
		Retention: RetentionPolicy{ExpiresAt: cmd.Now.Add(registered.RetryHorizon), Mode: RejectReuse, Tombstone: true},
	})
	if err != nil {
		if errors.Is(err, ErrConflict) {
			h.observed[cmd.Descriptor.ResourceScope+"|"+cmd.Key] = smbObservation{Kind: SMBViewConflict, At: cmd.Now.UTC()}
		}
		return SMBOperationResult{}, err
	}
	if resolution.Decision != Reserved {
		return SMBOperationResult{OperationRef: smbOperationRef(identity.Tenant, identity.Capability, identity.EffectScope, identity.Key, identity.Principal), Decision: resolution.Decision, Revision: fixture.revision, Submitted: true, MachinePrincipal: cmd.MachineToken, DelegatedActor: cmd.DelegatedActor}, nil
	}
	fixture.revision++
	fixture.mutations++
	fixture.fields = smbPayloadFields(cmd.Canonical)
	opRef := smbOperationRef(identity.Tenant, identity.Capability, identity.EffectScope, identity.Key, identity.Principal)
	fixture.history = append(fixture.history, SMBFixtureItem{Revision: fixture.revision, Ref: opRef})
	event := SMBLogicalEvent{
		EventID:  "evt://" + digestText(fixture.descriptor.ResourceScope+"|"+cmd.Key+"|"+strconv.FormatInt(fixture.revision, 10)),
		TenantID: identity.Tenant, Family: fixture.descriptor.EventFamily, Schema: fixture.descriptor.EventSchema,
		AggregateRef: opRef, AggregateRevision: fixture.revision, ControlEpoch: h.epoch,
	}
	h.events = append(h.events, event)
	effectRef := "effect://rev:" + strconv.FormatInt(fixture.revision, 10) + ":" + digestText(opRef)
	completed, err := h.registry.Complete(identity, resolution.Record.RequestDigest, opRef, effectRef, cmd.Now)
	if err != nil {
		return SMBOperationResult{}, err
	}
	result := SMBOperationResult{OperationRef: opRef, Decision: Reserved, Revision: fixture.revision, EventID: event.EventID, MachinePrincipal: cmd.MachineToken, DelegatedActor: cmd.DelegatedActor}
	h.results[opKey] = result
	h.records[opKey] = completed
	h.lastContact = cmd.Now.UTC()
	return result, nil
}

// batchLimit resolves the REGISTERED batch bound for a scope.
func (h *SMBConformanceHarness) batchLimit(scope string) (int, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fixture, ok := h.fixtures[scope]
	if !ok {
		return 0, false
	}
	return fixture.descriptor.MaxBatchItems, true
}

// ExecuteBatch runs each command for its own per-item status. Accepted items
// apply; refused items never do. There is no all-or-nothing claim.
func (h *SMBConformanceHarness) ExecuteBatch(cmds []SMBOperationCommand) []SMBBatchItemResult {
	items := make([]SMBBatchItemResult, 0, len(cmds))
	for _, cmd := range cmds {
		if limit, ok := h.batchLimit(cmd.Descriptor.ResourceScope); ok && len(cmds) > limit && limit > 0 {
			items = append(items, SMBBatchItemResult{Key: cmd.Key, Err: fmt.Errorf("%w: batch of %d exceeds registered limit %d", ErrSMBCommandInvalid, len(cmds), limit).Error()})
			continue
		}
		result, err := h.Execute(cmd)
		item := SMBBatchItemResult{Key: cmd.Key, Result: result}
		if err != nil {
			item.Err = err.Error()
		}
		items = append(items, item)
	}
	return items
}

// Read pages authorized revision marks. Cursors are opaque and bound to one
// tenant, scope and filter digest; anything else fails with no records.
func (h *SMBConformanceHarness) Read(query SMBReadQuery) (SMBReadPage, error) {
	if err := query.Descriptor.Validate(); err != nil {
		return SMBReadPage{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.tokens[query.MachineToken] {
		return SMBReadPage{}, ErrSMBTokenRevoked
	}
	fixture, ok := h.fixtures[query.Descriptor.ResourceScope]
	if !ok || fixture.descriptor.TenantID != query.Descriptor.TenantID {
		return SMBReadPage{}, ErrSMBCursorRejected
	}
	for field := range query.Filter {
		if !containsString(fixture.descriptor.FilterAllowlist, field) {
			return SMBReadPage{}, fmt.Errorf("%w: %q", ErrSMBFilterRejected, field)
		}
	}
	if query.Order != "" && !containsString(fixture.descriptor.OrderAllowlist, query.Order) {
		return SMBReadPage{}, fmt.Errorf("%w: %q", ErrSMBFilterRejected, query.Order)
	}
	wantFilter := smbFilterDigest(query.Filter)
	offset := 0
	if query.Cursor != "" {
		cursor, err := smbDecodeCursor(query.Cursor)
		if err != nil || cursor.Tenant != query.Descriptor.TenantID || cursor.Scope != query.Descriptor.ResourceScope || cursor.Filter != wantFilter || cursor.Offset < 0 {
			return SMBReadPage{}, ErrSMBCursorRejected
		}
		offset = cursor.Offset
	}
	if offset > len(fixture.history) {
		return SMBReadPage{}, ErrSMBCursorRejected
	}
	end := offset + fixture.descriptor.PageSize
	if end > len(fixture.history) {
		end = len(fixture.history)
	}
	page := SMBReadPage{Items: append([]SMBFixtureItem(nil), fixture.history[offset:end]...)}
	if end < len(fixture.history) {
		page.NextCursor = smbEncodeCursor(query.Descriptor.TenantID, query.Descriptor.ResourceScope, wantFilter, end)
	}
	return page, nil
}

// Events returns the durable logical outbox in append order.
func (h *SMBConformanceHarness) Events() []SMBLogicalEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]SMBLogicalEvent(nil), h.events...)
}

// CurrentRevision reports the server-authorized revision for a scope.
func (h *SMBConformanceHarness) CurrentRevision(scope string) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if fixture, ok := h.fixtures[scope]; ok {
		return fixture.revision
	}
	return 0
}

// FixtureMutations counts applied fixture mutations for a scope.
func (h *SMBConformanceHarness) FixtureMutations(scope string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if fixture, ok := h.fixtures[scope]; ok {
		return fixture.mutations
	}
	return 0
}

// SubscriberRevision reports the event subscriber's converged revision.
func (h *SMBConformanceHarness) SubscriberRevision(scope string) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.subscriber[scope]
}

// DeliverEvents applies duplicate/out-of-order delivery. Redeliveries are
// deduplicated by stable event ID and the subscriber never moves backward.
func (h *SMBConformanceHarness) DeliverEvents(events []SMBLogicalEvent) SMBDeliveryReport {
	h.mu.Lock()
	defer h.mu.Unlock()
	var report SMBDeliveryReport
	for _, event := range events {
		scope := ""
		for candidate, fixture := range h.fixtures {
			if fixture.descriptor.EventFamily == event.Family {
				scope = candidate
				break
			}
		}
		if h.seenEvents[event.EventID] {
			report.Duplicates++
		} else {
			h.seenEvents[event.EventID] = true
			report.Accepted++
			if scope != "" && event.AggregateRevision > h.subscriber[scope] {
				h.subscriber[scope] = event.AggregateRevision
			}
		}
		if scope != "" && h.subscriber[scope] > report.MaxRevision {
			report.MaxRevision = h.subscriber[scope]
		}
	}
	return report
}

// ResolveOperationView serves the truthful status for one operation key.
func (h *SMBConformanceHarness) ResolveOperationView(tenant, scope, key string, now time.Time) SMBOperationView {
	h.mu.Lock()
	defer h.mu.Unlock()
	fixture, ok := h.fixtures[scope]
	if !ok || fixture.descriptor.TenantID != tenant {
		return SMBOperationView{Status: SMBViewPending, Note: "operation is not visible in this scope"}
	}
	if !h.connected {
		return SMBOperationView{Status: SMBViewDisconnected, Actions: []string{"Refetch"}, Freshness: h.lastContact, Revision: fixture.revision, Note: "event stream is disconnected; showing last authorized state"}
	}
	opKey := scope + "|" + key
	if observation, seen := h.observed[opKey]; seen {
		switch observation.Kind {
		case SMBViewFenced:
			return SMBOperationView{Status: SMBViewFenced, OperationRef: "fence://" + digestText(opKey), Actions: []string{"Review again"}, Freshness: observation.At, Revision: fixture.revision, Note: "stale restored command is fenced for observation and repair"}
		case SMBViewStaleRevision:
			return SMBOperationView{Status: SMBViewStaleRevision, OperationRef: "pending://" + digestText(opKey), ChangedFields: observation.ChangedFields, Actions: []string{"Reload", "Review again"}, Revision: fixture.revision, Note: "resource changed; reload and review again"}
		case SMBViewConflict:
			// The safe reference names the original operation only; the
			// conflicting payload is never exposed to the other party.
			for resultKey, record := range h.records {
				if record.Identity.Key == key && record.Identity.Tenant == tenant && record.Identity.EffectScope == fixture.descriptor.IdempotencyScope {
					if prior, found := h.results[resultKey]; found {
						return SMBOperationView{Status: SMBViewConflict, OperationRef: prior.OperationRef, SafeReference: prior.OperationRef, Actions: []string{"Review again"}, Revision: fixture.revision, Note: "idempotency key was reused with a different payload"}
					}
				}
			}
			return SMBOperationView{Status: SMBViewConflict, SafeReference: "op://" + digestText(opKey), Actions: []string{"Review again"}, Revision: fixture.revision, Note: "idempotency key was reused with a different payload"}
		}
	}
	for resultKey, result := range h.results {
		if record, found := h.records[resultKey]; found && record.Identity.Key == key && record.Identity.Tenant == tenant && record.Identity.EffectScope == fixture.descriptor.IdempotencyScope {
			return SMBOperationView{Status: SMBViewCompleted, OperationRef: result.OperationRef, SafeReference: result.OperationRef, Revision: result.Revision, Actions: []string{"Refresh"}, Freshness: h.lastContact, Note: "domain completion proven"}
		}
	}
	return SMBOperationView{Status: SMBViewPending, OperationRef: "pending://" + digestText(opKey), Actions: []string{"Refresh"}, Revision: fixture.revision, Note: "accepted work is not yet proven complete"}
}

type smbSnapshot struct {
	Version     int                           `json:"version"`
	Epoch       uint64                        `json:"epoch"`
	LastContact time.Time                     `json:"last_contact"`
	Fixtures    map[string]smbFixtureSnapshot `json:"fixtures"`
	Results     map[string]SMBOperationResult `json:"results"`
	Records     map[string]Record             `json:"records"`
	Events      []SMBLogicalEvent             `json:"events"`
	Seen        []string                      `json:"seen"`
	Subscriber  map[string]int64              `json:"subscriber"`
	Observed    map[string]smbObservation     `json:"observed"`
	Fences      []string                      `json:"fences"`
}

type smbFixtureSnapshot struct {
	Descriptor SMBAPIAdoptionDescriptor `json:"descriptor"`
	Revision   int64                    `json:"revision"`
	Mutations  int                      `json:"mutations"`
	Fields     map[string]string        `json:"fields"`
	History    []SMBFixtureItem         `json:"history"`
}

// Export captures the full harness state for crash recovery.
func (h *SMBConformanceHarness) Export() ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	snapshot := smbSnapshot{
		Version: 1, Epoch: h.epoch, LastContact: h.lastContact,
		Fixtures: make(map[string]smbFixtureSnapshot), Results: make(map[string]SMBOperationResult),
		Records: make(map[string]Record), Events: append([]SMBLogicalEvent(nil), h.events...),
		Subscriber: make(map[string]int64), Observed: make(map[string]smbObservation),
		Fences: append([]string(nil), h.fences...),
	}
	for scope, fixture := range h.fixtures {
		fields := make(map[string]string, len(fixture.fields))
		for k, v := range fixture.fields {
			fields[k] = v
		}
		snapshot.Fixtures[scope] = smbFixtureSnapshot{Descriptor: fixture.descriptor, Revision: fixture.revision, Mutations: fixture.mutations, Fields: fields, History: append([]SMBFixtureItem(nil), fixture.history...)}
	}
	for k, v := range h.results {
		snapshot.Results[k] = v
	}
	for k, v := range h.records {
		snapshot.Records[k] = v
	}
	for id := range h.seenEvents {
		snapshot.Seen = append(snapshot.Seen, id)
	}
	sort.Strings(snapshot.Seen)
	for k, v := range h.subscriber {
		snapshot.Subscriber[k] = v
	}
	for k, v := range h.observed {
		snapshot.Observed[k] = v
	}
	return json.Marshal(snapshot)
}

// Import restores harness state and reseeds the idempotency registry from the
// persisted records so post-restore retries converge without duplicate work.
func (h *SMBConformanceHarness) Import(snapshot []byte) error {
	var decoded smbSnapshot
	if err := json.Unmarshal(snapshot, &decoded); err != nil {
		return fmt.Errorf("%w: %v", ErrSMBSnapshotInvalid, err)
	}
	if decoded.Version != 1 {
		return fmt.Errorf("%w: unsupported version %d", ErrSMBSnapshotInvalid, decoded.Version)
	}
	for _, fixture := range decoded.Fixtures {
		if err := fixture.Descriptor.Validate(); err != nil {
			return err
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.epoch = decoded.Epoch
	h.lastContact = decoded.LastContact
	h.connected = true
	h.fixtures = make(map[string]*smbFixture, len(decoded.Fixtures))
	for scope, saved := range decoded.Fixtures {
		fields := make(map[string]string, len(saved.Fields))
		for k, v := range saved.Fields {
			fields[k] = v
		}
		h.fixtures[scope] = &smbFixture{descriptor: saved.Descriptor, revision: saved.Revision, mutations: saved.Mutations, fields: fields, history: append([]SMBFixtureItem(nil), saved.History...)}
	}
	h.results = make(map[string]SMBOperationResult, len(decoded.Results))
	for k, v := range decoded.Results {
		h.results[k] = v
	}
	h.records = make(map[string]Record, len(decoded.Records))
	records := make(map[string]Record, len(decoded.Records))
	for k, v := range decoded.Records {
		h.records[k] = v
		records[identityKey(v.Identity)] = v
	}
	if h.registry == nil {
		h.registry = NewRegistry()
	}
	h.registry.mu.Lock()
	h.registry.records = records
	h.registry.mu.Unlock()
	h.events = append([]SMBLogicalEvent(nil), decoded.Events...)
	h.seenEvents = make(map[string]bool, len(decoded.Seen))
	for _, id := range decoded.Seen {
		h.seenEvents[id] = true
	}
	h.subscriber = make(map[string]int64, len(decoded.Subscriber))
	for k, v := range decoded.Subscriber {
		h.subscriber[k] = v
	}
	h.observed = make(map[string]smbObservation, len(decoded.Observed))
	for k, v := range decoded.Observed {
		h.observed[k] = v
	}
	h.fences = append([]string(nil), decoded.Fences...)
	h.baseRestored = true
	return nil
}

func smbEncodeCursor(tenant, scope, filter string, offset int) string {
	raw, _ := json.Marshal(smbCursor{Tenant: tenant, Scope: scope, Filter: filter, Offset: offset})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func smbDecodeCursor(cursor string) (smbCursor, error) {
	var decoded smbCursor
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return smbCursor{}, err
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return smbCursor{}, err
	}
	return decoded, nil
}

func smbFilterDigest(filter map[string]string) string {
	parts := make([]string, 0, len(filter))
	for k, v := range filter {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return digestText(strings.Join(parts, "\x00"))
}

func smbPayloadFields(canonical []byte) map[string]string {
	var raw map[string]any
	if err := json.Unmarshal(canonical, &raw); err != nil {
		return map[string]string{"_raw": string(canonical)}
	}
	fields := make(map[string]string, len(raw))
	for k, v := range raw {
		fields[k] = fmt.Sprintf("%v", v)
	}
	return fields
}

func smbChangedFields(current map[string]string, canonical []byte) []string {
	incoming := smbPayloadFields(canonical)
	keys := make(map[string]bool, len(current)+len(incoming))
	for k := range current {
		keys[k] = true
	}
	for k := range incoming {
		keys[k] = true
	}
	var changed []string
	for k := range keys {
		if current[k] != incoming[k] {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	if len(changed) == 0 {
		changed = []string{"revision"}
	}
	return changed
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
