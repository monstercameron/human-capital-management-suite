// Package subscription owns the typed vocabulary for tenant-scoped event
// subscriptions. It validates revisions and matching decisions without
// storing payloads, contacting providers, or making an authorization
// decision on behalf of a caller.
package subscription

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

const version = 1

// Version is the subscription domain contract version.
func Version() int { return version }

var (
	ErrInvalidSubscription = errors.New("subscription: invalid event subscription")
	ErrInvalidRevision     = errors.New("subscription: invalid revision")
	ErrInvalidEventKind    = errors.New("subscription: event kind is not in the closed vocabulary")
	ErrUndeclaredField     = errors.New("subscription: filter names an undeclared field")
	ErrInvalidTransition   = errors.New("subscription: invalid lifecycle transition")
	ErrApprovalRequired    = errors.New("subscription: activation requires a distinct approver")
	ErrInvalidEventDigest  = errors.New("subscription: invalid event digest")
)

// EventKind is the closed event vocabulary available to a subscription.
type EventKind string

const (
	EventWorkerCreated         EventKind = "WORKER_CREATED"
	EventWorkerChanged         EventKind = "WORKER_CHANGED"
	EventEmploymentChanged     EventKind = "EMPLOYMENT_CHANGED"
	EventAssignmentChanged     EventKind = "ASSIGNMENT_CHANGED"
	EventOrganizationChanged   EventKind = "ORGANIZATION_CHANGED"
	EventCompensationChanged   EventKind = "COMPENSATION_CHANGED"
	EventApplicationRevoked    EventKind = "PARTNER_APPLICATION_REVOKED"
	EventCustomObjectCreated   EventKind = "CUSTOM_OBJECT_CREATED"
	EventCustomObjectChanged   EventKind = "CUSTOM_OBJECT_CHANGED"
	EventCustomObjectCorrected EventKind = "CUSTOM_OBJECT_CORRECTED"
	EventCustomObjectRetired   EventKind = "CUSTOM_OBJECT_RETIRED"
	// EventSecurityAlert is the versioned alert projection exported from
	// security-evidence detection. Its payload schema is owned by the
	// application SIEM adapter; the subscription envelope carries only its
	// digest and opaque evidence reference.
	EventSecurityAlert EventKind = "SECURITY_ALERT"
	// EventSecurityAudit is the minimized DLP, access, and administrative
	// security evidence projection exported beside detector alerts.
	EventSecurityAudit EventKind = "SECURITY_AUDIT"
)

// CustomObjectKindField is the sole non-payload selector exposed by the
// custom-object event bridge. Values enter matching only through DigestValue.
const CustomObjectKindField = "custom_object.kind"

// Compatibility aliases keep the vocabulary readable at common call sites;
// they do not add values to the closed set.
const (
	EventWorkerUpdated           = EventWorkerChanged
	EventWorkerEmploymentChanged = EventEmploymentChanged
)

// Valid reports whether k is a declared event kind.
func (k EventKind) Valid() bool {
	switch k {
	case EventWorkerCreated, EventWorkerChanged, EventEmploymentChanged,
		EventAssignmentChanged, EventOrganizationChanged, EventCompensationChanged,
		EventApplicationRevoked, EventCustomObjectCreated, EventCustomObjectChanged,
		EventCustomObjectCorrected, EventCustomObjectRetired, EventSecurityAlert, EventSecurityAudit:
		return true
	default:
		return false
	}
}

// Subscriber identifies the one principal or partner allowed to receive an
// event. Exactly one reference is required; an empty pair means "anyone" and
// is rejected.
type Subscriber struct {
	PrincipalRef string `json:"principal_ref,omitempty"`
	PartnerRef   string `json:"partner_ref,omitempty"`
}

func (s Subscriber) validate() error {
	if (strings.TrimSpace(s.PrincipalRef) == "") == (strings.TrimSpace(s.PartnerRef) == "") {
		return fmt.Errorf("%w: exactly one principal or partner reference is required", ErrInvalidSubscription)
	}
	for name, value := range map[string]string{"principal_ref": s.PrincipalRef, "partner_ref": s.PartnerRef} {
		if value != "" && strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is padded", ErrInvalidSubscription, name)
		}
	}
	return nil
}

// PredicateOperator is the closed set of digest-only filter operations.
type PredicateOperator string

const (
	OperatorEquals    PredicateOperator = "EQUALS"
	OperatorNotEquals PredicateOperator = "NOT_EQUALS"
	OperatorExists    PredicateOperator = "EXISTS"
)

func (o PredicateOperator) valid() bool {
	return o == OperatorEquals || o == OperatorNotEquals || o == OperatorExists
}

// Predicate names one declared event field. Value is never applied to a raw
// payload: matching compares a digest of Value with EventDigest.FieldDigests.
// ExpectedDigest can be supplied when the caller already has a canonical
// digest and is preferred for sensitive values.
type Predicate struct {
	Field          string            `json:"field"`
	Operator       PredicateOperator `json:"operator"`
	Value          string            `json:"value,omitempty"`
	ExpectedDigest string            `json:"expected_digest,omitempty"`
}

// Filter is an AND of predicates. An empty filter matches every event kind
// selected by the subscription.
type Filter struct {
	Predicates []Predicate `json:"predicates,omitempty"`
}

// LifecycleState is the immutable revision state.
type LifecycleState string

const (
	StateDraft   LifecycleState = "DRAFT"
	StateActive  LifecycleState = "ACTIVE"
	StatePaused  LifecycleState = "PAUSED"
	StateRevoked LifecycleState = "REVOKED"
)

func (s LifecycleState) valid() bool {
	return s == StateDraft || s == StateActive || s == StatePaused || s == StateRevoked
}

// EventSubscription is one immutable subscription revision. A lifecycle
// operation returns a new value with Revision incremented; it never edits the
// source revision.
type EventSubscription struct {
	SubscriptionID       string                 `json:"subscription_id"`
	Revision             uint64                 `json:"revision"`
	State                LifecycleState         `json:"state"`
	Requester            string                 `json:"requester"`
	Approver             string                 `json:"approver,omitempty"`
	Subscriber           Subscriber             `json:"subscriber"`
	EventKinds           []EventKind            `json:"event_kinds"`
	DeclaredFields       map[EventKind][]string `json:"declared_fields"`
	Filter               Filter                 `json:"filter"`
	DeliveryEndpointRef  string                 `json:"delivery_endpoint_ref"`
	DeliveryGuarantee    DeliveryGuarantee      `json:"delivery_guarantee"`
	TenantScope          string                 `json:"tenant_scope"`
	OrganizationScopeRef string                 `json:"organization_scope_ref,omitempty"`
	PopulationScopeRef   string                 `json:"population_scope_ref,omitempty"`
	Digest               string                 `json:"digest"`
}

// DeliveryGuarantee is the closed delivery guarantee vocabulary. Exactly-once
// is an application-level outcome; the subscription only declares the class
// expected from its delivery runtime.
type DeliveryGuarantee string

const (
	GuaranteeAtMostOnce  DeliveryGuarantee = "AT_MOST_ONCE"
	GuaranteeAtLeastOnce DeliveryGuarantee = "AT_LEAST_ONCE"
	GuaranteeExactlyOnce DeliveryGuarantee = "EFFECTIVELY_ONCE"
)

func (g DeliveryGuarantee) valid() bool {
	return g == GuaranteeAtMostOnce || g == GuaranteeAtLeastOnce || g == GuaranteeExactlyOnce
}

// RevisionRequest contains the content of a new draft revision.
type RevisionRequest struct {
	SubscriptionID       string
	Requester            string
	Subscriber           Subscriber
	EventKinds           []EventKind
	DeclaredFields       map[EventKind][]string
	Filter               Filter
	DeliveryEndpointRef  string
	DeliveryGuarantee    DeliveryGuarantee
	TenantScope          string
	OrganizationScopeRef string
	PopulationScopeRef   string
}

// NewDraft creates and digests the first DRAFT revision.
func NewDraft(req RevisionRequest) (EventSubscription, error) {
	return newRevision(req, 1, StateDraft, "")
}

// NewSubscription is the vocabulary-oriented alias for NewDraft.
func NewSubscription(req RevisionRequest) (EventSubscription, error) { return NewDraft(req) }

func newRevision(req RevisionRequest, number uint64, state LifecycleState, approver string) (EventSubscription, error) {
	s := EventSubscription{
		SubscriptionID: req.SubscriptionID, Revision: number, State: state,
		Requester: req.Requester, Approver: approver, Subscriber: cloneSubscriber(req.Subscriber),
		EventKinds: append([]EventKind(nil), req.EventKinds...), DeclaredFields: cloneFields(req.DeclaredFields),
		Filter:              Filter{Predicates: append([]Predicate(nil), req.Filter.Predicates...)},
		DeliveryEndpointRef: req.DeliveryEndpointRef, DeliveryGuarantee: req.DeliveryGuarantee,
		TenantScope: req.TenantScope, OrganizationScopeRef: req.OrganizationScopeRef, PopulationScopeRef: req.PopulationScopeRef,
	}
	if err := s.validateContent(); err != nil {
		return EventSubscription{}, err
	}
	s.Digest = computeDigest(s)
	return s, nil
}

// NewRevision creates the next DRAFT revision from a previous revision's
// identity and supplied content. The previous value remains unchanged.
func NewRevision(previous EventSubscription, req RevisionRequest) (EventSubscription, error) {
	if err := previous.Verify(); err != nil {
		return EventSubscription{}, fmt.Errorf("%w: previous revision: %v", ErrInvalidRevision, err)
	}
	if previous.State == StateRevoked {
		return EventSubscription{}, fmt.Errorf("%w: revoked subscriptions cannot be revised", ErrInvalidTransition)
	}
	if req.SubscriptionID == "" {
		req.SubscriptionID = previous.SubscriptionID
	}
	if req.SubscriptionID != previous.SubscriptionID {
		return EventSubscription{}, fmt.Errorf("%w: subscription identity changed", ErrInvalidRevision)
	}
	return newRevision(req, previous.Revision+1, StateDraft, "")
}

// Transition creates a new lifecycle revision. Activation requires an
// approver distinct from the requester; all other transitions still require
// an attributed requester.
func (s EventSubscription) Transition(state LifecycleState, requester, approver string) (EventSubscription, error) {
	if err := s.Verify(); err != nil {
		return EventSubscription{}, fmt.Errorf("%w: source: %v", ErrInvalidRevision, err)
	}
	if !state.valid() || strings.TrimSpace(requester) == "" || strings.TrimSpace(requester) != requester {
		return EventSubscription{}, fmt.Errorf("%w: state and requester are required", ErrInvalidTransition)
	}
	if s.State == StateRevoked || state == StateDraft || state == s.State {
		return EventSubscription{}, fmt.Errorf("%w: %s to %s", ErrInvalidTransition, s.State, state)
	}
	if state == StateActive && (strings.TrimSpace(approver) == "" || approver == requester || strings.TrimSpace(approver) != approver) {
		return EventSubscription{}, fmt.Errorf("%w: requester and approver must be distinct", ErrApprovalRequired)
	}
	req := RevisionRequest{
		SubscriptionID: s.SubscriptionID, Requester: requester, Subscriber: s.Subscriber,
		EventKinds: s.EventKinds, DeclaredFields: s.DeclaredFields, Filter: s.Filter,
		DeliveryEndpointRef: s.DeliveryEndpointRef, DeliveryGuarantee: s.DeliveryGuarantee,
		TenantScope: s.TenantScope, OrganizationScopeRef: s.OrganizationScopeRef, PopulationScopeRef: s.PopulationScopeRef,
	}
	return newRevision(req, s.Revision+1, state, approver)
}

// Activate creates an ACTIVE revision with two-person approval evidence.
func (s EventSubscription) Activate(requester, approver string) (EventSubscription, error) {
	return s.Transition(StateActive, requester, approver)
}

// Pause creates a PAUSED revision.
func (s EventSubscription) Pause(requester string) (EventSubscription, error) {
	return s.Transition(StatePaused, requester, "")
}

// Revoke creates a terminal REVOKED revision.
func (s EventSubscription) Revoke(requester string) (EventSubscription, error) {
	return s.Transition(StateRevoked, requester, "")
}

// Revise creates the next DRAFT revision. It is the method form of
// NewRevision for callers that work from a value.
func (s EventSubscription) Revise(req RevisionRequest) (EventSubscription, error) {
	return NewRevision(s, req)
}

// NextRevision is an alias for Revise.
func (s EventSubscription) NextRevision(req RevisionRequest) (EventSubscription, error) {
	return s.Revise(req)
}

func (s EventSubscription) validateContent() error {
	if strings.TrimSpace(s.SubscriptionID) == "" || strings.TrimSpace(s.SubscriptionID) != s.SubscriptionID {
		return fmt.Errorf("%w: subscription id is required", ErrInvalidSubscription)
	}
	if s.Revision == 0 || !s.State.valid() {
		return fmt.Errorf("%w: revision and state are required", ErrInvalidRevision)
	}
	if strings.TrimSpace(s.Requester) == "" || strings.TrimSpace(s.Requester) != s.Requester {
		return fmt.Errorf("%w: requester is required", ErrInvalidSubscription)
	}
	if err := s.Subscriber.validate(); err != nil {
		return err
	}
	if len(s.EventKinds) == 0 {
		return fmt.Errorf("%w: at least one event kind is required", ErrInvalidSubscription)
	}
	seenKinds := make(map[EventKind]struct{}, len(s.EventKinds))
	for _, kind := range s.EventKinds {
		if !kind.Valid() {
			return fmt.Errorf("%w: %q", ErrInvalidEventKind, kind)
		}
		if _, ok := seenKinds[kind]; ok {
			return fmt.Errorf("%w: duplicate event kind %q", ErrInvalidSubscription, kind)
		}
		seenKinds[kind] = struct{}{}
	}
	if strings.TrimSpace(s.TenantScope) == "" || strings.TrimSpace(s.TenantScope) != s.TenantScope {
		return fmt.Errorf("%w: tenant scope is required", ErrInvalidSubscription)
	}
	if strings.TrimSpace(s.DeliveryEndpointRef) == "" || strings.TrimSpace(s.DeliveryEndpointRef) != s.DeliveryEndpointRef {
		return fmt.Errorf("%w: delivery endpoint reference is required", ErrInvalidSubscription)
	}
	if !s.DeliveryGuarantee.valid() {
		return fmt.Errorf("%w: delivery guarantee %q is not declared", ErrInvalidSubscription, s.DeliveryGuarantee)
	}
	for name, value := range map[string]string{"organization_scope_ref": s.OrganizationScopeRef, "population_scope_ref": s.PopulationScopeRef} {
		if value != "" && strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is padded", ErrInvalidSubscription, name)
		}
	}
	for kind, fields := range s.DeclaredFields {
		if !kind.Valid() {
			return fmt.Errorf("%w: declaration has unknown event kind %q", ErrInvalidEventKind, kind)
		}
		seen := make(map[string]struct{}, len(fields))
		for _, field := range fields {
			if strings.TrimSpace(field) == "" || strings.TrimSpace(field) != field {
				return fmt.Errorf("%w: declared field is empty or padded", ErrInvalidSubscription)
			}
			if _, ok := seen[field]; ok {
				return fmt.Errorf("%w: duplicate declared field %q", ErrInvalidSubscription, field)
			}
			seen[field] = struct{}{}
		}
	}
	for _, predicate := range s.Filter.Predicates {
		if strings.TrimSpace(predicate.Field) == "" || strings.TrimSpace(predicate.Field) != predicate.Field || !predicate.Operator.valid() {
			return fmt.Errorf("%w: invalid filter predicate", ErrInvalidSubscription)
		}
		if predicate.Operator != OperatorExists && predicate.Value == "" && predicate.ExpectedDigest == "" {
			return fmt.Errorf("%w: predicate %q has no expected value", ErrInvalidSubscription, predicate.Field)
		}
		for _, kind := range s.EventKinds {
			if !containsField(s.DeclaredFields[kind], predicate.Field) {
				return fmt.Errorf("%w: %q for %s", ErrUndeclaredField, predicate.Field, kind)
			}
		}
	}
	if s.State == StateActive && (s.Approver == "" || s.Approver == s.Requester) {
		return ErrApprovalRequired
	}
	return nil
}

// Validate checks the revision content but does not require a digest. Use
// Verify when checking a revision that claims to have been minted already.
func (s EventSubscription) Validate() error { return s.validateContent() }

// Verify detects mutation of the exported value after it was minted.
func (s EventSubscription) Verify() error {
	if err := s.validateContent(); err != nil {
		return err
	}
	if s.Digest == "" || computeDigest(s) != s.Digest {
		return fmt.Errorf("%w: digest does not match revision %d", ErrInvalidRevision, s.Revision)
	}
	return nil
}

// EventDigest is the payload-free input to matching. FieldDigests are
// precomputed by the event ingress boundary; this package never receives or
// evaluates the raw event payload.
type EventDigest struct {
	TenantScope  string            `json:"tenant_scope"`
	Kind         EventKind         `json:"kind"`
	Digest       string            `json:"digest"`
	FieldDigests map[string]string `json:"field_digests,omitempty"`
}

func (e EventDigest) validate() error {
	if strings.TrimSpace(e.TenantScope) == "" || e.TenantScope != strings.TrimSpace(e.TenantScope) || e.Digest == "" || !e.Kind.Valid() {
		return ErrInvalidEventDigest
	}
	return nil
}

// DigestValue returns the canonical digest used by Predicate.Value matching.
func DigestValue(value string) string {
	h := sha256.New()
	_, _ = h.Write([]byte("hcmnext.domains.subscription.filter-value/v1\x00"))
	_, _ = h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}

// CustomObjectEventDigest projects a custom-object outbox entry into the
// payload-free subscription contract. Tenant and object-kind scoping are
// bound into the event digest, while the filterable kind is itself digested.
func CustomObjectEventDigest(tenantScope, objectKind string, eventKind EventKind, payloadDigest string) (EventDigest, error) {
	if strings.TrimSpace(tenantScope) == "" || tenantScope != strings.TrimSpace(tenantScope) ||
		strings.TrimSpace(objectKind) == "" || objectKind != strings.TrimSpace(objectKind) ||
		strings.TrimSpace(payloadDigest) == "" || payloadDigest != strings.TrimSpace(payloadDigest) {
		return EventDigest{}, ErrInvalidEventDigest
	}
	switch eventKind {
	case EventCustomObjectCreated, EventCustomObjectChanged, EventCustomObjectCorrected, EventCustomObjectRetired:
	default:
		return EventDigest{}, ErrInvalidEventKind
	}
	h := sha256.New()
	for _, part := range []string{"hcmnext.domains.subscription.custom-object-event/v1", tenantScope, objectKind, string(eventKind), payloadDigest} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return EventDigest{
		TenantScope: tenantScope,
		Kind:        eventKind,
		Digest:      "sha256:" + hex.EncodeToString(h.Sum(nil)),
		FieldDigests: map[string]string{
			CustomObjectKindField: DigestValue(objectKind),
		},
	}, nil
}

// SubscriptionMatch identifies a matched active revision without exposing an
// event payload.
type SubscriptionMatch struct {
	SubscriptionID string `json:"subscription_id"`
	Revision       uint64 `json:"revision"`
	RevisionDigest string `json:"revision_digest"`
}

// MatchActive returns active revisions whose tenant, event kind and digest-only
// filter match event. The input slice should contain the current active
// revisions; inactive revisions are ignored and never treated as matches.
func MatchActive(event EventDigest, revisions []EventSubscription) ([]SubscriptionMatch, error) {
	if err := event.validate(); err != nil {
		return nil, err
	}
	out := make([]SubscriptionMatch, 0)
	for _, revision := range revisions {
		if revision.State != StateActive || revision.TenantScope != event.TenantScope || !containsKind(revision.EventKinds, event.Kind) {
			continue
		}
		if err := revision.Verify(); err != nil {
			return nil, fmt.Errorf("%w: candidate %s: %v", ErrInvalidRevision, revision.SubscriptionID, err)
		}
		matched := true
		for _, predicate := range revision.Filter.Predicates {
			fieldDigest, exists := event.FieldDigests[predicate.Field]
			switch predicate.Operator {
			case OperatorExists:
				matched = matched && exists
			case OperatorEquals:
				expected := predicate.ExpectedDigest
				if expected == "" {
					expected = DigestValue(predicate.Value)
				}
				matched = matched && exists && fieldDigest == expected
			case OperatorNotEquals:
				expected := predicate.ExpectedDigest
				if expected == "" {
					expected = DigestValue(predicate.Value)
				}
				matched = matched && (!exists || fieldDigest != expected)
			}
			if !matched {
				break
			}
		}
		if matched {
			out = append(out, SubscriptionMatch{SubscriptionID: revision.SubscriptionID, Revision: revision.Revision, RevisionDigest: revision.Digest})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SubscriptionID == out[j].SubscriptionID {
			return out[i].Revision < out[j].Revision
		}
		return out[i].SubscriptionID < out[j].SubscriptionID
	})
	return out, nil
}

// Registry is a concurrent in-memory collection of immutable revisions. It
// is a test/design adapter, not durable authority.
type Registry struct {
	mu        sync.RWMutex
	revisions map[string][]EventSubscription
}

// NewRegistry creates an empty design-time registry.
func NewRegistry() *Registry { return &Registry{revisions: make(map[string][]EventSubscription)} }

// Append records one revision without replacing an existing revision.
func (r *Registry) Append(revision EventSubscription) error {
	if r == nil {
		return ErrInvalidRevision
	}
	if err := revision.Verify(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	history := r.revisions[revision.SubscriptionID]
	if len(history) == 0 && revision.Revision != 1 {
		return fmt.Errorf("%w: first revision must be 1", ErrInvalidRevision)
	}
	if len(history) > 0 && revision.Revision != history[len(history)-1].Revision+1 {
		return fmt.Errorf("%w: revision must append in order", ErrInvalidRevision)
	}
	r.revisions[revision.SubscriptionID] = append(history, cloneSubscription(revision))
	return nil
}

// Register is an alias for Append.
func (r *Registry) Register(revision EventSubscription) error { return r.Append(revision) }

// Revisions returns a copy of one subscription's immutable history.
func (r *Registry) Revisions(id string) []EventSubscription {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	history := r.revisions[id]
	out := make([]EventSubscription, len(history))
	for i := range history {
		out[i] = cloneSubscription(history[i])
	}
	return out
}

// Active returns the latest ACTIVE revision for every subscription.
func (r *Registry) Active() []EventSubscription {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]EventSubscription, 0)
	for _, history := range r.revisions {
		if len(history) > 0 && history[len(history)-1].State == StateActive {
			out = append(out, cloneSubscription(history[len(history)-1]))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SubscriptionID < out[j].SubscriptionID })
	return out
}

// Match evaluates only the registry's latest active revisions.
func (r *Registry) Match(event EventDigest) ([]SubscriptionMatch, error) {
	return MatchActive(event, r.Active())
}

type digestPredicate struct {
	Field, Operator, Value, ExpectedDigest string
}

type digestView struct {
	SubscriptionID string
	Revision       uint64
	State          LifecycleState
	Requester      string
	Approver       string
	PrincipalRef   string
	PartnerRef     string
	EventKinds     []EventKind
	Fields         []fieldView
	Predicates     []digestPredicate
	Endpoint       string
	Guarantee      DeliveryGuarantee
	Tenant         string
	Organization   string
	Population     string
}

type fieldView struct {
	Kind   EventKind
	Fields []string
}

func computeDigest(s EventSubscription) string {
	kinds := append([]EventKind(nil), s.EventKinds...)
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	fields := make([]fieldView, 0, len(s.DeclaredFields))
	for kind, values := range s.DeclaredFields {
		fields = append(fields, fieldView{Kind: kind, Fields: append([]string(nil), values...)})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Kind < fields[j].Kind })
	for i := range fields {
		sort.Strings(fields[i].Fields)
	}
	predicates := make([]digestPredicate, 0, len(s.Filter.Predicates))
	for _, predicate := range s.Filter.Predicates {
		predicates = append(predicates, digestPredicate{Field: predicate.Field, Operator: string(predicate.Operator), Value: predicate.Value, ExpectedDigest: predicate.ExpectedDigest})
	}
	sort.Slice(predicates, func(i, j int) bool {
		if predicates[i].Field == predicates[j].Field {
			return predicates[i].Operator < predicates[j].Operator
		}
		return predicates[i].Field < predicates[j].Field
	})
	view := digestView{
		SubscriptionID: s.SubscriptionID, Revision: s.Revision, State: s.State, Requester: s.Requester, Approver: s.Approver,
		PrincipalRef: s.Subscriber.PrincipalRef, PartnerRef: s.Subscriber.PartnerRef, EventKinds: kinds,
		Fields: fields, Predicates: predicates, Endpoint: s.DeliveryEndpointRef, Guarantee: s.DeliveryGuarantee,
		Tenant: s.TenantScope, Organization: s.OrganizationScopeRef, Population: s.PopulationScopeRef,
	}
	b, err := json.Marshal(view)
	if err != nil {
		return ""
	}
	h := sha256.New()
	_, _ = h.Write([]byte("hcmnext.domains.subscription.EventSubscription/v1\x00"))
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

type SubscriptionExplanation struct {
	SubscriptionID      string
	Revision            uint64
	State               LifecycleState
	SubscriberKind      string
	EventKinds          []EventKind
	TenantScope         string
	DeliveryEndpointRef string
	DeliveryGuarantee   DeliveryGuarantee
	RevisionDigest      string
}

// Explain returns bounded metadata and the revision digest, never filter
// values or event payloads.
func (s EventSubscription) Explain() SubscriptionExplanation {
	kind := "PARTNER"
	if s.Subscriber.PrincipalRef != "" {
		kind = "PRINCIPAL"
	}
	return SubscriptionExplanation{SubscriptionID: s.SubscriptionID, Revision: s.Revision, State: s.State, SubscriberKind: kind, EventKinds: append([]EventKind(nil), s.EventKinds...), TenantScope: s.TenantScope, DeliveryEndpointRef: s.DeliveryEndpointRef, DeliveryGuarantee: s.DeliveryGuarantee, RevisionDigest: s.Digest}
}

// Explain is the package-level explanation symbol required by the domain
// contract.
func Explain(s EventSubscription) SubscriptionExplanation { return s.Explain() }

func containsField(fields []string, wanted string) bool {
	for _, field := range fields {
		if field == wanted {
			return true
		}
	}
	return false
}

func containsKind(kinds []EventKind, wanted EventKind) bool {
	for _, kind := range kinds {
		if kind == wanted {
			return true
		}
	}
	return false
}

func cloneSubscriber(s Subscriber) Subscriber { return s }

func cloneFields(fields map[EventKind][]string) map[EventKind][]string {
	if fields == nil {
		return nil
	}
	out := make(map[EventKind][]string, len(fields))
	for kind, values := range fields {
		out[kind] = append([]string(nil), values...)
	}
	return out
}

func cloneSubscription(s EventSubscription) EventSubscription {
	s.EventKinds = append([]EventKind(nil), s.EventKinds...)
	s.DeclaredFields = cloneFields(s.DeclaredFields)
	s.Filter.Predicates = append([]Predicate(nil), s.Filter.Predicates...)
	return s
}
