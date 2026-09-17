// Privacy-scoped process-mining event log (PROCESS-001).
//
// The projector appends telemetry as canonical, read-only case
// projections: every emitted event carries case, activity, lifecycle,
// timestamp and resource with its scope, redaction marker and watermark,
// plus the case completeness state. Raw payloads never enter the log —
// only their presence and digest — resources pseudonymize deterministically
// under the scope salt, and missing telemetry marks a case PARTIAL rather
// than completing it by assumption. The log rebuilds by replaying the same
// appends in any order.
package privacy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Process-log errors.
var (
	ErrInvalidProcessEvent = errors.New("privacy: invalid process event")
	ErrProcessScope        = errors.New("privacy: process event is outside the log scope")
)

// EventLifecycle is the closed activity-lifecycle vocabulary.
type EventLifecycle string

const (
	LifecycleStarted   EventLifecycle = "STARTED"
	LifecycleCompleted EventLifecycle = "COMPLETED"
	LifecycleSuspended EventLifecycle = "SUSPENDED"
	LifecycleResumed   EventLifecycle = "RESUMED"
	LifecycleCancelled EventLifecycle = "CANCELLED"
	LifecycleObserved  EventLifecycle = "OBSERVED"
)

func (l EventLifecycle) valid() bool {
	switch l {
	case LifecycleStarted, LifecycleCompleted, LifecycleSuspended,
		LifecycleResumed, LifecycleCancelled, LifecycleObserved:
		return true
	default:
		return false
	}
}

// terminal reports whether the lifecycle can close a case.
func (l EventLifecycle) terminal() bool {
	return l == LifecycleCompleted || l == LifecycleCancelled
}

// Completeness is the closed case-completeness vocabulary. A gap in the
// case sequence is PARTIAL; an empty case is UNKNOWN; only a contiguous
// sequence ending in a terminal lifecycle is COMPLETE.
type Completeness string

const (
	CompletenessComplete Completeness = "COMPLETE"
	CompletenessPartial  Completeness = "PARTIAL"
	CompletenessUnknown  Completeness = "UNKNOWN"
)

// ProcessScope binds the projector to one tenant, purpose and workflow
// set. Events outside it are refused, never filtered silently.
type ProcessScope struct {
	TenantID         string
	Purpose          string
	AllowedWorkflows []string
	RedactResources  bool
	RedactionSalt    string
}

// ProcessEvent is one raw telemetry event. Payload carries raw attribute
// values the projection must never repeat; CausedBy names the parent
// event when the activity was caused by one.
type ProcessEvent struct {
	EventID      string
	TenantID     string
	WorkflowID   string
	CaseID       string
	CaseSequence uint64
	Activity     string
	Lifecycle    EventLifecycle
	At           time.Time
	Resource     string
	CausedBy     string
	Payload      map[string]string
}

// ProjectedEvent is the canonical emitted tuple: no payload values, only
// presence and digest; the resource pseudonymized when the scope demands
// it; causation preserved by event id.
type ProjectedEvent struct {
	CaseID         string
	CaseSequence   uint64
	Activity       string
	Lifecycle      EventLifecycle
	Timestamp      string
	Resource       string
	Redacted       bool
	CausedBy       string
	PayloadPresent bool
	PayloadDigest  string
	ScopeTenant    string
	ScopePurpose   string
	WorkflowID     string
}

// CaseProjection is the read-only rebuildable view of one case.
type CaseProjection struct {
	CaseID           string
	WorkflowID       string
	ScopeTenant      string
	ScopePurpose     string
	Events           []ProjectedEvent
	Completeness     Completeness
	MissingSequences []uint64
	Watermark        string
	CanonicalDigest  string
}

// ProcessProjector is the tenant- and purpose-scoped append-only log. It
// is safe for concurrent use.
type ProcessProjector struct {
	mu       sync.Mutex
	scope    ProcessScope
	allowed  map[string]struct{}
	events   map[string]ProcessEvent
	byCase   map[string][]string
	sequence map[string]map[uint64]struct{}
}

// NewProcessProjector binds one log to its privacy scope.
func NewProcessProjector(scope ProcessScope) (*ProcessProjector, error) {
	if strings.TrimSpace(scope.TenantID) == "" || strings.TrimSpace(scope.Purpose) == "" {
		return nil, fmt.Errorf("%w: tenant and purpose are required", ErrInvalidProcessEvent)
	}
	if len(scope.AllowedWorkflows) == 0 {
		return nil, fmt.Errorf("%w: at least one workflow is required", ErrInvalidProcessEvent)
	}
	if scope.RedactResources && strings.TrimSpace(scope.RedactionSalt) == "" {
		return nil, fmt.Errorf("%w: redaction salt is required when resources redact", ErrInvalidProcessEvent)
	}
	allowed := make(map[string]struct{}, len(scope.AllowedWorkflows))
	for _, workflow := range scope.AllowedWorkflows {
		if strings.TrimSpace(workflow) == "" {
			return nil, fmt.Errorf("%w: empty workflow in scope", ErrInvalidProcessEvent)
		}
		allowed[workflow] = struct{}{}
	}
	return &ProcessProjector{
		scope:    scope,
		allowed:  allowed,
		events:   make(map[string]ProcessEvent),
		byCase:   make(map[string][]string),
		sequence: make(map[string]map[uint64]struct{}),
	}, nil
}

// Append validates and records one event. Scope violations, unknown
// causation, missing effective time and replays are refused.
func (p *ProcessProjector) Append(event ProcessEvent) error {
	if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.CaseID) == "" ||
		strings.TrimSpace(event.Activity) == "" || strings.TrimSpace(event.Resource) == "" {
		return fmt.Errorf("%w: event, case, activity and resource are required", ErrInvalidProcessEvent)
	}
	if event.CaseSequence == 0 {
		return fmt.Errorf("%w: case sequence starts at one", ErrInvalidProcessEvent)
	}
	if !event.Lifecycle.valid() {
		return fmt.Errorf("%w: unsupported lifecycle %q", ErrInvalidProcessEvent, event.Lifecycle)
	}
	if event.At.IsZero() {
		return fmt.Errorf("%w: effective time is required", ErrInvalidProcessEvent)
	}
	if event.TenantID != p.scope.TenantID {
		return fmt.Errorf("%w: tenant %q is outside log tenant %q",
			ErrProcessScope, event.TenantID, p.scope.TenantID)
	}
	if _, ok := p.allowed[event.WorkflowID]; !ok {
		return fmt.Errorf("%w: workflow %q is outside the log scope", ErrProcessScope, event.WorkflowID)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.events[event.EventID]; exists {
		return fmt.Errorf("%w: event %q is already logged", ErrInvalidProcessEvent, event.EventID)
	}
	if event.CausedBy != "" {
		if _, ok := p.events[event.CausedBy]; !ok {
			return fmt.Errorf("%w: causation parent %q is unknown", ErrInvalidProcessEvent, event.CausedBy)
		}
	}
	if p.sequence[event.CaseID] == nil {
		p.sequence[event.CaseID] = make(map[uint64]struct{})
	}
	if _, exists := p.sequence[event.CaseID][event.CaseSequence]; exists {
		return fmt.Errorf("%w: case %q already carries sequence %d",
			ErrInvalidProcessEvent, event.CaseID, event.CaseSequence)
	}
	p.events[event.EventID] = event
	p.byCase[event.CaseID] = append(p.byCase[event.CaseID], event.EventID)
	p.sequence[event.CaseID][event.CaseSequence] = struct{}{}
	return nil
}

// Project emits the canonical read-only projection of one case. Event
// order follows case sequence, never append order, so replay rebuilds the
// identical projection.
func (p *ProcessProjector) Project(caseID string) (CaseProjection, error) {
	p.mu.Lock()
	ids := append([]string(nil), p.byCase[caseID]...)
	events := make([]ProcessEvent, 0, len(ids))
	for _, id := range ids {
		events = append(events, p.events[id])
	}
	scope := p.scope
	p.mu.Unlock()
	if len(events) == 0 {
		return CaseProjection{
			CaseID: caseID, ScopeTenant: scope.TenantID, ScopePurpose: scope.Purpose,
			Completeness: CompletenessUnknown,
		}, nil
	}
	sort.Slice(events, func(i, j int) bool { return events[i].CaseSequence < events[j].CaseSequence })
	projection := CaseProjection{
		CaseID: caseID, WorkflowID: events[0].WorkflowID,
		ScopeTenant: scope.TenantID, ScopePurpose: scope.Purpose,
	}
	var watermark time.Time
	for _, event := range events {
		if event.WorkflowID != projection.WorkflowID {
			return CaseProjection{}, fmt.Errorf("%w: case %q mixes workflows", ErrProcessScope, caseID)
		}
		projection.Events = append(projection.Events, scope.project(event))
		if event.At.After(watermark) {
			watermark = event.At
		}
	}
	projection.Watermark = watermark.UTC().Format(time.RFC3339)
	projection.Completeness, projection.MissingSequences = completeness(events)
	projection.CanonicalDigest = projection.digest()
	return projection, nil
}

func completeness(events []ProcessEvent) (Completeness, []uint64) {
	var missing []uint64
	for i, event := range events {
		want := uint64(i + 1)
		if event.CaseSequence != want {
			for seq := want; seq < event.CaseSequence; seq++ {
				missing = append(missing, seq)
			}
		}
	}
	if len(missing) > 0 {
		return CompletenessPartial, missing
	}
	if events[len(events)-1].Lifecycle.terminal() {
		return CompletenessComplete, nil
	}
	return CompletenessPartial, nil
}

func (s ProcessScope) project(event ProcessEvent) ProjectedEvent {
	resource, redacted := event.Resource, false
	if s.RedactResources {
		sum := sha256.Sum256([]byte(s.RedactionSalt + "\x00" + event.Resource))
		resource, redacted = "pseudonym:"+hex.EncodeToString(sum[:])[:16], true
	}
	return ProjectedEvent{
		CaseID: event.CaseID, CaseSequence: event.CaseSequence, Activity: event.Activity,
		Lifecycle: event.Lifecycle, Timestamp: event.At.UTC().Format(time.RFC3339),
		Resource: resource, Redacted: redacted, CausedBy: event.CausedBy,
		PayloadPresent: len(event.Payload) > 0, PayloadDigest: digestPayload(event.Payload),
		ScopeTenant: s.TenantID, ScopePurpose: s.Purpose, WorkflowID: event.WorkflowID,
	}
}

func digestPayload(payload map[string]string) string {
	if len(payload) == 0 {
		return ""
	}
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sum := sha256.New()
	for _, key := range keys {
		sum.Write([]byte(key + "\x00" + payload[key] + "\x00"))
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

func (c CaseProjection) digest() string {
	sum := sha256.New()
	fmt.Fprintf(sum, "%s\x00%s\x00%s\x00%s\x00%s\x00",
		c.CaseID, c.WorkflowID, c.ScopeTenant, c.ScopePurpose, c.Completeness)
	for _, event := range c.Events {
		fmt.Fprintf(sum, "%d\x00%s\x00%s\x00%s\x00%s\x00%v\x00%s\x00%v\x00%s\x00",
			event.CaseSequence, event.Activity, event.Lifecycle, event.Timestamp,
			event.Resource, event.Redacted, event.CausedBy, event.PayloadPresent, event.PayloadDigest)
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}
