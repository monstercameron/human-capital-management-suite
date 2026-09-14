package shadow

import (
	"context"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"sync"
	"time"
)

type AttemptKind string

const (
	AttemptRead       AttemptKind = "GOVERNED_READ"
	AttemptCapability AttemptKind = "CAPABILITY_EFFECT"
	AttemptDomain     AttemptKind = "DOMAIN_EFFECT"
	AttemptExternal   AttemptKind = "EXTERNAL_EFFECT"
	AttemptMessage    AttemptKind = "MESSAGE_SEND"
	AttemptBilling    AttemptKind = "BILLING"
	AttemptApproval   AttemptKind = "APPROVAL_CONSUMPTION"
)

type EffectAttempt struct {
	NodeID    string      `json:"node_id"`
	Kind      AttemptKind `json:"kind"`
	Reference string      `json:"reference,omitempty"`
	Allowed   bool        `json:"allowed"`
	At        time.Time   `json:"at"`
}

type RefusalError struct{ Attempt EffectAttempt }

func (e *RefusalError) Error() string {
	return fmt.Sprintf("shadow: %s at %s refused", e.Attempt.Kind, e.Attempt.NodeID)
}
func (e *RefusalError) Unwrap() error { return ErrEffectForbidden }

// Recorder replaces both capability and effect ports. Reads are served from
// supplied copied references; every effect attempt is recorded and refused.
type Recorder struct {
	mu       sync.Mutex
	reads    State
	attempts []EffectAttempt
}

func NewRecorder(reads State) *Recorder { return &Recorder{reads: reads.Clone()} }

func (r *Recorder) Read(_ context.Context, nodeID, reference string, at time.Time) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts = append(r.attempts, EffectAttempt{NodeID: nodeID, Kind: AttemptRead, Reference: reference, Allowed: true, At: at.UTC()})
	value, ok := r.reads[reference]
	if !ok {
		return nil, fmt.Errorf("shadow: governed read %q is unavailable", reference)
	}
	return append([]byte(nil), value...), nil
}

func (r *Recorder) Refuse(_ context.Context, nodeID string, kind AttemptKind, reference string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	attempt := EffectAttempt{NodeID: nodeID, Kind: kind, Reference: reference, At: at.UTC()}
	r.attempts = append(r.attempts, attempt)
	return &RefusalError{Attempt: attempt}
}

func (r *Recorder) Invoke(ctx context.Context, nodeID string, kind AttemptKind, reference string, at time.Time) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.shadow.invoke", observe.Attrs{observe.KeyNode: nodeID}, kind)
	defer func() { observe.DoneWith(obsOp, retErr) }()
	return r.Refuse(ctx, nodeID, kind, reference, at)
}

func (r *Recorder) Attempts() []EffectAttempt {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]EffectAttempt(nil), r.attempts...)
}

type WorkItem struct {
	NodeID string `json:"node_id"`
	ID     string `json:"id"`
	Ref    string `json:"ref,omitempty"`
}

type WorkItemRecorder struct {
	mu    sync.Mutex
	items []WorkItem
}

func (r *WorkItemRecorder) Schedule(item WorkItem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, item)
}
func (r *WorkItemRecorder) Items() []WorkItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]WorkItem(nil), r.items...)
}

type Timer struct {
	NodeID string    `json:"node_id"`
	ID     string    `json:"id"`
	DueAt  time.Time `json:"due_at"`
}

type TimerRecorder struct {
	mu     sync.Mutex
	timers []Timer
}

func (r *TimerRecorder) Schedule(timer Timer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.timers = append(r.timers, timer)
}
func (r *TimerRecorder) Timers() []Timer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Timer(nil), r.timers...)
}

type TerminalRequest struct {
	TenantID       string `json:"tenant_id"`
	InstanceID     string `json:"instance_id"`
	WorkflowID     string `json:"workflow_id"`
	PlanDigest     string `json:"plan_digest"`
	BusinessCode   string `json:"business_code"`
	OutputDigest   string `json:"output_digest,omitempty"`
	CorrelationID  string `json:"correlation_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type TerminalRecord struct {
	Code         string `json:"code"`
	BusinessCode string `json:"business_code"`
	Digest       string `json:"digest"`
}

type TerminalRecorder struct {
	mu     sync.Mutex
	record *TerminalRecord
}

func (r *TerminalRecorder) Record(req TerminalRequest) TerminalRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.record != nil {
		return *r.record
	}
	b, _ := marshalCanonical(req)
	r.record = &TerminalRecord{Code: TerminalNotExecuted, BusinessCode: req.BusinessCode, Digest: digestBytes("hcmnext.workflow.shadow.Terminal/v1", b)}
	return *r.record
}
func (r *TerminalRecorder) RecordValue() (TerminalRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.record == nil {
		return TerminalRecord{}, false
	}
	return *r.record, true
}

type Ports struct {
	Capabilities *Recorder
	Effects      *Recorder
	WorkItems    *WorkItemRecorder
	Timers       *TimerRecorder
	Terminal     *TerminalRecorder
}
