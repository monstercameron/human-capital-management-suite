package clockservice

// TCLOCK-012 provides the clock event schema and the application-side
// projection used by webhook and pull transports. It deliberately owns no
// delivery loop and never emits an event directly: the only source is the
// committed EventOutbox port.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
)

const (
	ClockPunchAccepted    = "clock.punch.accepted"
	ClockPunchRejected    = "clock.punch.rejected"
	ClockSessionOpened    = "clock.session.opened"
	ClockSessionClosed    = "clock.session.closed"
	ClockExceptionRaised  = "clock.exception.raised"
	ClockTimecardApproved = "clock.timecard.approved"
	ClockTimecardReopened = "clock.timecard.reopened"
	ClockDeviceOffline    = "clock.device.offline"
)

var (
	ErrClockEventsUnavailable = errors.New("clock events: feed is unavailable")
	ErrClockEventInvalid      = errors.New("clock events: invalid committed event")
	ErrClockCursorInvalid     = errors.New("clock events: invalid cursor")
	ErrClockScopeDenied       = errors.New("clock events: scope denied")
)

// ClockEventSchemas returns a fresh immutable schema registry for the eight
// clock event kinds. The registry is value-owned so package state cannot be
// mutated by one tenant or test.
func ClockEventSchemas() *subscription.SchemaRegistry {
	r := subscription.NewSchemaRegistry()
	for _, spec := range clockSchemaSpecs() {
		s, err := subscription.NewEventSchema(spec.kind, 1, spec.fields)
		if err != nil || r.Register(s) != nil {
			return nil
		}
	}
	return r
}

type clockSchemaSpec struct {
	kind   subscription.EventKind
	fields []subscription.SchemaField
}

func clockSchemaSpecs() []clockSchemaSpec {
	field := func(name string, typ subscription.FieldType) subscription.SchemaField {
		return subscription.SchemaField{Name: name, Type: typ, Classification: subscription.ClassificationConfidential}
	}
	ref := subscription.TypeRef
	str := subscription.TypeString
	instant := subscription.TypeInstant
	integer := subscription.TypeInteger
	return []clockSchemaSpec{
		{subscription.EventClockPunchAccepted, []subscription.SchemaField{field("receipt_id", ref), field("device_id", ref), field("worker_id", ref), field("session_id", ref), field("event_type", str), field("occurred_at", instant), field("device_sequence", integer)}},
		{subscription.EventClockPunchRejected, []subscription.SchemaField{field("device_id", ref), field("worker_credential_ref", ref), field("device_sequence", integer), field("reason", str), field("detail_ref", ref)}},
		{subscription.EventClockSessionOpened, []subscription.SchemaField{field("session_id", ref), field("worker_id", ref), field("opened_at", instant), field("source_ref", ref)}},
		{subscription.EventClockSessionClosed, []subscription.SchemaField{field("session_id", ref), field("worker_id", ref), field("closed_at", instant), field("worked_seconds", integer)}},
		{subscription.EventClockExceptionRaised, []subscription.SchemaField{field("subject_id", ref), field("subject_kind", str), field("kind", str), field("detail_ref", ref)}},
		{subscription.EventClockTimecardApproved, []subscription.SchemaField{field("timecard_id", ref), field("worker_id", ref), field("approved_by", ref), field("period_start", str), field("period_end", str)}},
		{subscription.EventClockTimecardReopened, []subscription.SchemaField{field("timecard_id", ref), field("worker_id", ref), field("reopened_by", ref), field("reason", str)}},
		{subscription.EventClockDeviceOffline, []subscription.SchemaField{field("device_id", ref), field("site_id", ref), field("last_seen_at", instant)}},
	}
}

// ClockEventAuthorizer is called before the outbox is read. Implementations
// must validate the current tenant and organization/worker scope and fail
// closed.
type ClockEventAuthorizer interface {
	AuthorizeClockEvents(context.Context, string, string, string) error
}

// ClockEventSubscriptionSource provides the current immutable subscription
// revisions and their separate authorization grants.
type ClockEventSubscriptionSource interface {
	ActiveClockSubscriptions(context.Context, string) ([]subscription.EventSubscription, map[string]subscription.ScopeGrant, error)
}

// ScopedClockEventSource supplies trusted organization and worker bindings
// captured with the committed outbox row. A source lacking this capability
// cannot serve a request that asks for either scope.
type ScopedClockEventSource interface {
	ListScopedEvents(context.Context, string, string, string, int64, int) ([]ScopedOutboxEvent, error)
}

// ScopedOutboxEvent extends the existing clock outbox row with trusted scope
// metadata. The metadata is source supplied and is never taken from payload.
type ScopedOutboxEvent struct {
	OutboxEvent
	Organization string
	Worker       string
}

// ClockEventFeedConfig wires the feed without changing Service.Outbox or the
// managed subscription delivery loop.
type ClockEventFeedConfig struct {
	Source        EventOutbox
	Authorizer    ClockEventAuthorizer
	Subscriptions ClockEventSubscriptionSource
	Revisions     []subscription.EventSubscription
	Grants        map[string]subscription.ScopeGrant
	Signer        *subscription.CredentialRing
	CursorKey     []byte
	Destination   string
	Now           func() time.Time
	CursorTTL     time.Duration
}

// ClockEventFeed projects committed clock outbox entries for pull and
// webhook transports. It contains no goroutines and is safe for concurrent
// calls when its injected ports are safe.
type ClockEventFeed struct {
	source        EventOutbox
	authorizer    ClockEventAuthorizer
	subscriptions ClockEventSubscriptionSource
	revisions     []subscription.EventSubscription
	grants        map[string]subscription.ScopeGrant
	signer        *subscription.CredentialRing
	cursorKey     []byte
	destination   string
	now           func() time.Time
	cursorTTL     time.Duration
}

// NewClockEventFeed validates and copies all configuration. At least one
// subscription source (dynamic or fixed) and a dedicated cursor key are
// required; authorization is mandatory before any outbox read.
func NewClockEventFeed(c ClockEventFeedConfig) (*ClockEventFeed, error) {
	if c.Source == nil || c.Authorizer == nil || c.Signer == nil || len(c.CursorKey) < 32 {
		return nil, ErrClockEventsUnavailable
	}
	if c.Subscriptions == nil && c.Revisions == nil {
		return nil, ErrClockEventsUnavailable
	}
	if strings.TrimSpace(c.Destination) == "" {
		return nil, ErrClockEventsUnavailable
	}
	now := c.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	ttl := c.CursorTTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	g := make(map[string]subscription.ScopeGrant, len(c.Grants))
	for k, v := range c.Grants {
		v.Fields = cloneGrantFields(v.Fields)
		v.EventKinds = append([]subscription.EventKind(nil), v.EventKinds...)
		v.Resources = append([]string(nil), v.Resources...)
		g[k] = v
	}
	return &ClockEventFeed{source: c.Source, authorizer: c.Authorizer, subscriptions: c.Subscriptions,
		revisions: cloneClockRevisions(c.Revisions), grants: g,
		signer: c.Signer, cursorKey: append([]byte(nil), c.CursorKey...), destination: c.Destination,
		now: now, cursorTTL: ttl}, nil
}

// ClockEventCursor binds sequence to tenant and scope. It is opaque and
// authenticated; callers cannot move a cursor across tenant or scope.
type ClockEventCursor struct {
	Tenant       string `json:"tenant"`
	Organization string `json:"organization,omitempty"`
	Worker       string `json:"worker,omitempty"`
	Sequence     int64  `json:"sequence"`
	ExpiresAt    int64  `json:"expires_at"`
}

// IssueClockEventCursor mints a tenant-and-scope-bound cursor.
func (f *ClockEventFeed) IssueClockEventCursor(c ClockEventCursor) (string, error) {
	if f == nil || strings.TrimSpace(c.Tenant) == "" || c.Sequence < 0 {
		return "", ErrClockCursorInvalid
	}
	if c.ExpiresAt == 0 {
		c.ExpiresAt = f.now().Add(f.cursorTTL).Unix()
	}
	b, err := json.Marshal(c)
	if err != nil {
		return "", ErrClockCursorInvalid
	}
	p := base64.RawURLEncoding.EncodeToString(b)
	return p + "." + clockCursorMAC(f.cursorKey, p), nil
}

func (f *ClockEventFeed) decodeCursor(raw, tenant, organization, worker string) (ClockEventCursor, error) {
	if raw == "" {
		return ClockEventCursor{Tenant: tenant, Organization: organization, Worker: worker}, nil
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(clockCursorMAC(f.cursorKey, parts[0]))) {
		return ClockEventCursor{}, ErrClockCursorInvalid
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ClockEventCursor{}, ErrClockCursorInvalid
	}
	var c ClockEventCursor
	if json.Unmarshal(b, &c) != nil || c.Tenant != tenant || c.Organization != organization || c.Worker != worker || c.Sequence < 0 || c.ExpiresAt <= f.now().Unix() {
		return ClockEventCursor{}, ErrClockCursorInvalid
	}
	return c, nil
}

func clockCursorMAC(key []byte, payload string) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("hcmnext.clock-event-cursor/v1\x00" + payload))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// ClockEventRequest is the pull feed request. Organization and Worker are
// explicit scope inputs and are included in cursor binding.
type ClockEventRequest struct {
	Tenant, Organization, Worker, Cursor string
	Limit                                int
}

// ClockEvent is a minimally disclosed, signed event projection.
type ClockEvent struct {
	Tenant        string                         `json:"tenant"`
	Sequence      int64                          `json:"sequence"`
	EventType     string                         `json:"event_type"`
	SchemaVersion int                            `json:"schema_version"`
	Payload       map[string]any                 `json:"payload"`
	Envelope      subscription.CanonicalEnvelope `json:"envelope"`
	Signature     subscription.SignedDelivery    `json:"signature"`
}

// ClockEventPage is a bounded pull result with a signed resume cursor.
type ClockEventPage struct {
	Events     []ClockEvent `json:"events"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

// VerifyClockEvent checks that the delivered payload still matches the
// signed projected digest before a consumer uses it.
func VerifyClockEvent(event ClockEvent, signer *subscription.CredentialRing, at time.Time) error {
	if signer == nil || event.Tenant != event.Envelope.Tenant || event.Sequence <= 0 ||
		uint64(event.Sequence) != event.Envelope.Sequence || event.EventType != string(event.Envelope.Kind) ||
		event.SchemaVersion != event.Envelope.SchemaVersion {
		return ErrClockEventInvalid
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil || subscription.DigestValue(string(payload)) != event.Envelope.PayloadDigest {
		return ErrClockEventInvalid
	}
	if err := event.Envelope.Validate(); err != nil {
		return ErrClockEventInvalid
	}
	if err := signer.Verify(event.Signature, event.Envelope.Digest(), at); err != nil {
		return fmt.Errorf("%w: signature: %v", ErrClockEventInvalid, err)
	}
	return nil
}

// Pull reads only committed outbox rows after authorization, applies current
// managed subscription scope and field masks, and signs each projection.
func (f *ClockEventFeed) Pull(ctx context.Context, req ClockEventRequest) (ClockEventPage, error) {
	if f == nil || strings.TrimSpace(req.Tenant) == "" || req.Limit <= 0 || req.Limit > 1000 {
		return ClockEventPage{}, ErrClockEventsUnavailable
	}
	if err := f.authorizer.AuthorizeClockEvents(ctx, req.Tenant, req.Organization, req.Worker); err != nil {
		return ClockEventPage{}, fmt.Errorf("%w: %v", ErrClockScopeDenied, err)
	}
	c, err := f.decodeCursor(req.Cursor, req.Tenant, req.Organization, req.Worker)
	if err != nil {
		return ClockEventPage{}, err
	}
	revisions, grants, err := f.currentSubscriptions(ctx, req.Tenant)
	if err != nil {
		return ClockEventPage{}, err
	}
	if err := validateFeedSubscriptions(req.Tenant, revisions, grants); err != nil {
		return ClockEventPage{}, err
	}
	// Principal, cursor, and current subscription scope are all resolved
	// before this read. A denied caller cannot probe outbox sequence state.
	var rows []OutboxEvent
	var scoped []ScopedOutboxEvent
	if req.Organization != "" || req.Worker != "" {
		source, ok := f.source.(ScopedClockEventSource)
		if !ok {
			return ClockEventPage{}, fmt.Errorf("%w: trusted organization/worker metadata unavailable", ErrClockScopeDenied)
		}
		scoped, err = source.ListScopedEvents(ctx, req.Tenant, req.Organization, req.Worker, c.Sequence, req.Limit)
		if err != nil {
			return ClockEventPage{}, err
		}
		if len(scoped) > req.Limit {
			return ClockEventPage{}, ErrClockEventInvalid
		}
		rows = make([]OutboxEvent, len(scoped))
		for i := range scoped {
			rows[i] = scoped[i].OutboxEvent
		}
	} else {
		rows, err = f.source.ListEvents(ctx, req.Tenant, c.Sequence, req.Limit)
		if err != nil {
			return ClockEventPage{}, err
		}
	}
	if len(rows) > req.Limit {
		return ClockEventPage{}, ErrClockEventInvalid
	}
	if err := validateOutboxRows(rows, req.Tenant, c.Sequence); err != nil {
		return ClockEventPage{}, err
	}
	page := ClockEventPage{Events: make([]ClockEvent, 0, len(rows))}
	for i, row := range rows {
		var org, worker string
		if len(scoped) > 0 {
			org, worker = scoped[i].Organization, scoped[i].Worker
		}
		e, ok, err := f.project(req.Tenant, row, org, worker, req.Organization, req.Worker, revisions, grants)
		if err != nil {
			return ClockEventPage{}, err
		}
		if ok {
			page.Events = append(page.Events, e)
		}
		c.Sequence = row.Sequence
	}
	c.ExpiresAt = 0
	if c.Sequence > 0 {
		page.NextCursor, err = f.IssueClockEventCursor(c)
	}
	return page, err
}

func (f *ClockEventFeed) currentSubscriptions(ctx context.Context, tenant string) ([]subscription.EventSubscription, map[string]subscription.ScopeGrant, error) {
	if f.subscriptions != nil {
		r, g, err := f.subscriptions.ActiveClockSubscriptions(ctx, tenant)
		return activeClockRevisions(cloneClockRevisions(r)), cloneGrants(g), err
	}
	return activeClockRevisions(f.revisions), cloneGrants(f.grants), nil
}

func (f *ClockEventFeed) project(tenant string, row OutboxEvent, trustedOrganization, trustedWorker, requestedOrganization, requestedWorker string, revisions []subscription.EventSubscription, grants map[string]subscription.ScopeGrant) (ClockEvent, bool, error) {
	schema, ok := schemaFor(row.EventType, row.SchemaVersion)
	if !ok {
		return ClockEvent{}, false, nil
	}
	var raw map[string]any
	if json.Unmarshal(row.Payload, &raw) != nil {
		return ClockEvent{}, false, ErrClockEventInvalid
	}
	payload := make(map[string]any)
	for _, field := range schema.Fields {
		if value, exists := raw[field.Name]; exists {
			if !validClockValue(field.Type, value) {
				return ClockEvent{}, false, ErrClockEventInvalid
			}
			payload[field.Name] = value
		}
	}
	if requestedWorker != "" && (trustedWorker == "" || trustedWorker != requestedWorker) {
		return ClockEvent{}, false, nil
	}
	if requestedOrganization != "" && (trustedOrganization == "" || trustedOrganization != requestedOrganization) {
		return ClockEvent{}, false, nil
	}
	if requestedWorker != "" {
		if w, ok := payload["worker_id"].(string); !ok || w != requestedWorker {
			return ClockEvent{}, false, nil
		}
	}
	fd := make(map[string]string, len(payload))
	for name, value := range payload {
		b, _ := json.Marshal(value)
		fd[name] = subscription.DigestValue(string(b))
	}
	projected, err := json.Marshal(payload)
	if err != nil {
		return ClockEvent{}, false, ErrClockEventInvalid
	}
	digest := subscription.EventDigest{TenantScope: tenant, Kind: subscription.EventKind(row.EventType), Digest: subscription.DigestValue(string(projected)), FieldDigests: fd}
	eligibleRevisions := make([]subscription.EventSubscription, 0, len(revisions))
	eligibleGrants := make(map[string]subscription.ScopeGrant, len(grants))
	for _, revision := range revisions {
		if !clockSubscriptionScopeMatches(revision, trustedOrganization, trustedWorker, requestedOrganization, requestedWorker) {
			continue
		}
		eligibleRevisions = append(eligibleRevisions, revision)
		if grant, exists := grants[revision.SubscriptionID]; exists {
			eligibleGrants[revision.SubscriptionID] = grant
		}
	}
	matches, _, err := subscription.MatchAuthorized(digest, eligibleRevisions, eligibleGrants)
	if err != nil {
		return ClockEvent{}, false, err
	}
	if len(revisions) > 0 && len(matches) == 0 {
		return ClockEvent{}, false, nil
	}
	if len(revisions) > 0 {
		allowed := make(map[string]bool)
		for _, match := range matches {
			for _, revision := range eligibleRevisions {
				if revision.SubscriptionID == match.SubscriptionID && revision.Revision == match.Revision {
					for _, field := range revision.DeclaredFields[subscription.EventKind(row.EventType)] {
						allowed[field] = true
					}
				}
			}
		}
		for field := range payload {
			if !allowed[field] {
				delete(payload, field)
			}
		}
	}
	projected, err = json.Marshal(payload)
	if err != nil {
		return ClockEvent{}, false, ErrClockEventInvalid
	}
	env := subscription.CanonicalEnvelope{Tenant: tenant, Kind: subscription.EventKind(row.EventType), SchemaVersion: row.SchemaVersion, SubjectRefs: subjectRefs(payload), EffectiveAt: row.CreatedAt.UTC(), KnownAt: row.CreatedAt.UTC(), PayloadDigest: subscription.DigestValue(string(projected)), ProvenanceRef: fmt.Sprintf("clock-outbox:%d", row.Sequence), Sequence: uint64(row.Sequence)}
	if err := env.Validate(); err != nil {
		return ClockEvent{}, false, err
	}
	signed, err := f.signer.Sign(env.Digest(), f.now())
	if err != nil {
		return ClockEvent{}, false, err
	}
	return ClockEvent{Tenant: tenant, Sequence: row.Sequence, EventType: row.EventType, SchemaVersion: row.SchemaVersion, Payload: payload, Envelope: env, Signature: signed}, true, nil
}

// clockSubscriptionScopeMatches requires trusted source metadata whenever a
// subscription declares an organization or population scope. A requested
// scope is not evidence by itself: it is only a selector that must agree with
// the metadata captured with the committed outbox row.
func clockSubscriptionScopeMatches(revision subscription.EventSubscription, trustedOrganization, trustedWorker, requestedOrganization, requestedWorker string) bool {
	if revision.OrganizationScopeRef != "" && (trustedOrganization == "" || trustedOrganization != revision.OrganizationScopeRef) {
		return false
	}
	if revision.PopulationScopeRef != "" && (trustedWorker == "" || trustedWorker != revision.PopulationScopeRef) {
		return false
	}
	if requestedOrganization != "" && revision.OrganizationScopeRef != "" && requestedOrganization != revision.OrganizationScopeRef {
		return false
	}
	if requestedWorker != "" && revision.PopulationScopeRef != "" && requestedWorker != revision.PopulationScopeRef {
		return false
	}
	return true
}

func schemaFor(eventType string, version int) (subscription.EventSchema, bool) {
	for _, spec := range clockSchemaSpecs() {
		if string(spec.kind) == eventType {
			s, err := subscription.NewEventSchema(spec.kind, 1, spec.fields)
			return s, err == nil && version == 1
		}
	}
	return subscription.EventSchema{}, false
}

func activeClockRevisions(in []subscription.EventSubscription) []subscription.EventSubscription {
	out := make([]subscription.EventSubscription, 0, len(in))
	for _, s := range in {
		if s.State == subscription.StateActive {
			out = append(out, s)
		}
	}
	return out
}

func cloneGrantFields(in map[subscription.EventKind][]string) map[subscription.EventKind][]string {
	if in == nil {
		return nil
	}
	out := make(map[subscription.EventKind][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func cloneGrants(in map[string]subscription.ScopeGrant) map[string]subscription.ScopeGrant {
	if in == nil {
		return nil
	}
	out := make(map[string]subscription.ScopeGrant, len(in))
	for id, grant := range in {
		grant.EventKinds = append([]subscription.EventKind(nil), grant.EventKinds...)
		grant.Resources = append([]string(nil), grant.Resources...)
		grant.Fields = cloneGrantFields(grant.Fields)
		out[id] = grant
	}
	return out
}

func validateFeedSubscriptions(tenant string, revisions []subscription.EventSubscription, grants map[string]subscription.ScopeGrant) error {
	if len(revisions) == 0 {
		return ErrClockScopeDenied
	}
	for _, s := range revisions {
		if s.TenantScope != tenant {
			return ErrClockScopeDenied
		}
		g, ok := grants[s.SubscriptionID]
		if !ok {
			return ErrClockScopeDenied
		}
		if _, err := subscription.Authorize(s, g); err != nil {
			return fmt.Errorf("%w: %v", ErrClockScopeDenied, err)
		}
	}
	return nil
}

func validateOutboxRows(rows []OutboxEvent, tenant string, after int64) error {
	previous := after
	for _, row := range rows {
		if row.Sequence <= 0 || row.Sequence <= previous || row.SchemaVersion <= 0 {
			return ErrClockEventInvalid
		}
		if row.EventType == "" || row.CreatedAt.IsZero() {
			return ErrClockEventInvalid
		}
		previous = row.Sequence
	}
	_ = tenant // tenant is bound by EventOutbox.ListEvents; scoped sources bind it in their port.
	return nil
}

func validClockValue(typ subscription.FieldType, value any) bool {
	switch typ {
	case subscription.TypeString:
		_, ok := value.(string)
		return ok
	case subscription.TypeRef:
		text, ok := value.(string)
		return ok && strings.TrimSpace(text) != ""
	case subscription.TypeInstant:
		text, ok := value.(string)
		if !ok {
			return false
		}
		instant, err := time.Parse(time.RFC3339Nano, text)
		return err == nil && !instant.IsZero()
	case subscription.TypeInteger:
		number, ok := value.(float64)
		return ok && !math.IsNaN(number) && !math.IsInf(number, 0) && number >= -9007199254740991 && number <= 9007199254740991 && math.Trunc(number) == number
	case subscription.TypeNumber:
		number, ok := value.(float64)
		return ok && !math.IsNaN(number) && !math.IsInf(number, 0)
	case subscription.TypeBoolean:
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}
func subjectRefs(payload map[string]any) []string {
	keys := []string{"worker_id", "session_id", "timecard_id", "device_id", "subject_id"}
	out := []string{}
	for _, k := range keys {
		if v, ok := payload[k].(string); ok && v != "" {
			out = append(out, k+":"+v)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		out = []string{"clock:event"}
	}
	return out
}

func cloneClockRevisions(in []subscription.EventSubscription) []subscription.EventSubscription {
	out := append([]subscription.EventSubscription(nil), in...)
	for i := range out {
		out[i].EventKinds = append([]subscription.EventKind(nil), in[i].EventKinds...)
		out[i].DeclaredFields = cloneGrantFields(in[i].DeclaredFields)
		out[i].Filter.Predicates = append([]subscription.Predicate(nil), in[i].Filter.Predicates...)
	}
	return out
}
