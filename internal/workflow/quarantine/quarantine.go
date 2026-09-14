// Package quarantine records governed, immutable decisions about a published
// workflow version. It does not edit a version or a running instance: callers
// use the returned decision and event to drive the version and runtime owners.
package quarantine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrationpreview"
)

const (
	contractVersion = 1
	recordProfile   = "hcmnext.workflow.quarantine.Record/v1"
	eventProfile    = "hcmnext.workflow.quarantine.Event/v1"
)

// Version reports this package's contract version.
func Version() int { return contractVersion }

// Action is an append-only lifecycle action for one exact workflow version.
type Action string

const (
	ActionQuarantine Action = "QUARANTINE"
	ActionLift       Action = "LIFT"
)

func (a Action) Valid() bool { return a == ActionQuarantine || a == ActionLift }

// Request is the complete evidence for opening a quarantine. The declared
// principal and approver are deliberately separate fields: a quarantine is a
// two-person action even when the caller is an automated operator.
type Request struct {
	WorkflowID         string
	WorkflowVersion    uint32
	CompiledPlanDigest string
	Reason             string
	EvidenceRef        string
	DeclaredBy         string
	ApprovedBy         string
	EffectiveAt        time.Time
}

// LiftRequest supplies the new evidence for lifting a quarantine. A lift is
// a new record and never changes the old quarantine record.
type LiftRequest struct {
	Reason      string
	EvidenceRef string
	ReviewedBy  string
	EffectiveAt time.Time
}

// Event is the digested audit event emitted for every quarantine action.
type Event struct {
	Action       Action    `json:"action"`
	RecordDigest string    `json:"record_digest"`
	Previous     string    `json:"previous_record_digest,omitempty"`
	EffectiveAt  time.Time `json:"effective_at"`
	digest       string
}

// Digest returns the event's canonical content identity.
func (e Event) Digest() string { return e.digest }

// Record is one immutable quarantine or lift decision. Status is represented
// by Action; a lift therefore remains visible as a separate historical record.
type Record struct {
	WorkflowID         string    `json:"workflow_id"`
	WorkflowVersion    uint32    `json:"workflow_version"`
	CompiledPlanDigest string    `json:"compiled_plan_digest"`
	Action             Action    `json:"action"`
	Reason             string    `json:"reason"`
	EvidenceRef        string    `json:"evidence_ref"`
	DeclaredBy         string    `json:"declared_by"`
	ApprovedBy         string    `json:"approved_by"`
	EffectiveAt        time.Time `json:"effective_at"`
	PreviousDigest     string    `json:"previous_digest,omitempty"`
	Event              Event     `json:"event"`
	digest             string
}

// Digest returns the record's canonical content identity.
func (r Record) Digest() string { return r.digest }

// Verify confirms that neither the record nor its action event was changed.
func (r Record) Verify() error {
	if r.digest == "" || r.digest != recordDigest(r) {
		return refuse(CodeRecordMutated, r.CompiledPlanDigest, "quarantine record content does not match its digest")
	}
	if r.Event.Digest() == "" || r.Event.Digest() != eventDigest(r.Event) {
		return refuse(CodeEventMutated, r.CompiledPlanDigest, "quarantine event content does not match its digest")
	}
	return nil
}

// Explain returns a stable human-readable summary for an operator or audit
// log. Programmatic consumers should use the typed fields and error codes.
func (r Record) Explain() string {
	return fmt.Sprintf("workflow %s version %d (%s): %s by %s, approved by %s at %s",
		r.WorkflowID, r.WorkflowVersion, r.Action, r.Reason, r.DeclaredBy, r.ApprovedBy,
		r.EffectiveAt.UTC().Format(time.RFC3339Nano))
}

// Explain is the package-level form for callers that do not need a method
// expression.
func Explain(r Record) string { return r.Explain() }

// Create constructs one quarantine record without storage or a clock.
func Create(req Request) (Record, error) {
	if err := validateRequest(req); err != nil {
		return Record{}, err
	}
	r := Record{
		WorkflowID: req.WorkflowID, WorkflowVersion: req.WorkflowVersion,
		CompiledPlanDigest: req.CompiledPlanDigest, Action: ActionQuarantine,
		Reason: req.Reason, EvidenceRef: req.EvidenceRef,
		DeclaredBy: req.DeclaredBy, ApprovedBy: req.ApprovedBy,
		EffectiveAt: req.EffectiveAt.UTC(),
	}
	return seal(r), nil
}

// Quarantine is an alias for Create using the domain action's name.
func Quarantine(req Request) (Record, error) { return Create(req) }

// Lift creates a new record proving that a distinct reviewer lifted previous.
func Lift(previous Record, req LiftRequest) (Record, error) {
	if err := previous.Verify(); err != nil {
		return Record{}, err
	}
	if previous.Action != ActionQuarantine {
		return Record{}, refuse(CodeNotQuarantined, previous.CompiledPlanDigest, "only a quarantine record can be lifted")
	}
	if req.Reason == "" || req.EvidenceRef == "" || req.ReviewedBy == "" || req.EffectiveAt.IsZero() {
		return Record{}, refuse(CodeInvalidRequest, previous.CompiledPlanDigest, "lift requires reason, evidence ref, reviewer and effective instant")
	}
	if req.ReviewedBy == previous.DeclaredBy || req.ReviewedBy == previous.ApprovedBy {
		return Record{}, refuse(CodeSeparationOfDuties, previous.CompiledPlanDigest, "lift reviewer must be distinct from the quarantine declarant and approver")
	}
	r := Record{
		WorkflowID: previous.WorkflowID, WorkflowVersion: previous.WorkflowVersion,
		CompiledPlanDigest: previous.CompiledPlanDigest, Action: ActionLift,
		Reason: req.Reason, EvidenceRef: req.EvidenceRef,
		DeclaredBy: req.ReviewedBy, ApprovedBy: req.ReviewedBy,
		EffectiveAt: req.EffectiveAt.UTC(), PreviousDigest: previous.Digest(),
	}
	return seal(r), nil
}

// BlocksNewStart reports whether this record's exact version is currently
// blocked. A lift record is an explicit release and does not block starts.
func (r Record) BlocksNewStart(workflowID string, workflowVersion uint32, compiledPlanDigest string) bool {
	return r.Action == ActionQuarantine && r.WorkflowID == workflowID &&
		r.WorkflowVersion == workflowVersion && r.CompiledPlanDigest == compiledPlanDigest
}

// AdmitStart is the typed start-boundary refusal for a quarantined exact
// version. A caller may use it before invoking runtime.Start; no runtime row
// is touched here.
func (r Record) AdmitStart(workflowID string, workflowVersion uint32, compiledPlanDigest string) error {
	if r.BlocksNewStart(workflowID, workflowVersion, compiledPlanDigest) {
		return refuse(CodeStartQuarantined, compiledPlanDigest, "new instance starts are refused for quarantined workflow version")
	}
	return nil
}

// LiveInstance identifies the minimum runtime projection needed to mark an
// instance as awaiting a migration-preview decision.
type LiveInstance struct {
	InstanceID         string `json:"instance_id"`
	WorkflowID         string `json:"workflow_id"`
	WorkflowVersion    uint32 `json:"workflow_version"`
	CompiledPlanDigest string `json:"compiled_plan_digest"`
	StepID             string `json:"step_id"`
	Stage              string `json:"stage"`
}

// MigrationDecision is the quarantine disposition for a live instance.
type MigrationDecision string

const (
	MigrationPreviewRequired MigrationDecision = "MIGRATION_PREVIEW_REQUIRED"
)

// InstanceDisposition marks a quarantined live instance without changing it.
type InstanceDisposition struct {
	Instance         LiveInstance      `json:"instance"`
	Decision         MigrationDecision `json:"decision"`
	QuarantineDigest string            `json:"quarantine_digest"`
}

// MarkLiveInstances returns stable dispositions for instances pinned to this
// quarantined version. It intentionally does not mark unrelated versions.
func (r Record) MarkLiveInstances(instances []LiveInstance) []InstanceDisposition {
	if r.Action != ActionQuarantine {
		return nil
	}
	out := make([]InstanceDisposition, 0)
	for _, instance := range instances {
		if r.BlocksNewStart(instance.WorkflowID, instance.WorkflowVersion, instance.CompiledPlanDigest) {
			out = append(out, InstanceDisposition{Instance: instance, Decision: MigrationPreviewRequired, QuarantineDigest: r.Digest()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Instance.InstanceID < out[j].Instance.InstanceID })
	return out
}

// Preview runs the existing pure migration-preview contract after proving the
// supplied source plan is the quarantined version.
func (r Record) Preview(source, target *workflow.CompiledWorkflow, instances []migrationpreview.LiveInstanceState, bridges []migrationpreview.Bridge) (migrationpreview.Result, error) {
	if r.Action != ActionQuarantine {
		return migrationpreview.Result{}, refuse(CodeNotQuarantined, r.CompiledPlanDigest, "migration preview is only required for a quarantine record")
	}
	if source == nil || source.Digest() != r.CompiledPlanDigest {
		return migrationpreview.Result{}, refuse(CodePlanMismatch, r.CompiledPlanDigest, "source plan does not match the quarantined compiled-plan digest")
	}
	return migrationpreview.Preview(migrationpreview.Request{Source: source, Target: target, Instances: instances, Bridges: bridges})
}

// Registry is an append-only in-memory adapter for quarantine records. It is a
// kernel test adapter, not a durable store.
type Registry struct {
	mu      sync.RWMutex
	current map[string]Record
	history map[string][]Record
}

func NewRegistry() *Registry {
	return &Registry{current: make(map[string]Record), history: make(map[string][]Record)}
}

func key(workflowID string, workflowVersion uint32, digest string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", workflowID, workflowVersion, digest)
}

// Quarantine records a new quarantine action and refuses a duplicate active
// quarantine rather than manufacturing a second current state.
func (s *Registry) Quarantine(req Request) (Record, error) {
	r, err := Create(req)
	if err != nil {
		return Record{}, err
	}
	k := key(r.WorkflowID, r.WorkflowVersion, r.CompiledPlanDigest)
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.current[k]; ok && current.Action == ActionQuarantine {
		return Record{}, refuse(CodeAlreadyQuarantined, r.CompiledPlanDigest, "version is already quarantined by record %s", current.Digest())
	}
	s.current[k] = r
	s.history[k] = append(s.history[k], r)
	return r, nil
}

// Lift records a new release action for the registry's current quarantine.
func (s *Registry) Lift(workflowID string, workflowVersion uint32, digest string, req LiftRequest) (Record, error) {
	k := key(workflowID, workflowVersion, digest)
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.current[k]
	if !ok {
		return Record{}, refuse(CodeNotQuarantined, digest, "no current quarantine exists for this version")
	}
	r, err := Lift(previous, req)
	if err != nil {
		return Record{}, err
	}
	s.current[k] = r
	s.history[k] = append(s.history[k], r)
	return r, nil
}

// Current returns the latest action for an exact version.
func (s *Registry) Current(workflowID string, workflowVersion uint32, digest string) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.current[key(workflowID, workflowVersion, digest)]
	return r, ok
}

// AllowsNewStart is the admission read used by a runtime start boundary.
func (s *Registry) AllowsNewStart(workflowID string, workflowVersion uint32, digest string) bool {
	r, ok := s.Current(workflowID, workflowVersion, digest)
	return !ok || r.Action != ActionQuarantine
}

// AdmitStart is the registry-backed form of the start-boundary refusal.
func (s *Registry) AdmitStart(workflowID string, workflowVersion uint32, digest string) error {
	r, ok := s.Current(workflowID, workflowVersion, digest)
	if !ok {
		return nil
	}
	return r.AdmitStart(workflowID, workflowVersion, digest)
}

// History returns a copy of every action for an exact version.
func (s *Registry) History(workflowID string, workflowVersion uint32, digest string) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Record(nil), s.history[key(workflowID, workflowVersion, digest)]...)
}

func seal(r Record) Record {
	r.digest = recordDigest(r)
	r.Event = Event{Action: r.Action, RecordDigest: r.digest, Previous: r.PreviousDigest, EffectiveAt: r.EffectiveAt}
	r.Event.digest = eventDigest(r.Event)
	return r
}

type recordIdentity struct {
	WorkflowID         string
	WorkflowVersion    uint32
	CompiledPlanDigest string
	Action             Action
	Reason             string
	EvidenceRef        string
	DeclaredBy         string
	ApprovedBy         string
	EffectiveAt        time.Time
	PreviousDigest     string
}

func recordDigest(r Record) string {
	return digest(recordProfile, recordIdentity{r.WorkflowID, r.WorkflowVersion, r.CompiledPlanDigest, r.Action, r.Reason, r.EvidenceRef, r.DeclaredBy, r.ApprovedBy, r.EffectiveAt, r.PreviousDigest})
}

func eventDigest(e Event) string {
	return digest(eventProfile, struct {
		Action       Action
		RecordDigest string
		Previous     string
		EffectiveAt  time.Time
	}{e.Action, e.RecordDigest, e.Previous, e.EffectiveAt})
}

func digest(profile string, value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	h := sha256.New()
	_, _ = h.Write([]byte(profile))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func validateRequest(req Request) error {
	switch {
	case req.WorkflowID == "":
		return refuse(CodeInvalidRequest, "", "workflow id is required")
	case req.WorkflowVersion == 0:
		return refuse(CodeInvalidRequest, req.WorkflowID, "workflow version must be at least 1")
	case req.CompiledPlanDigest == "":
		return refuse(CodeInvalidRequest, req.WorkflowID, "compiled-plan digest is required")
	case req.Reason == "":
		return refuse(CodeInvalidRequest, req.WorkflowID, "quarantine reason is required")
	case req.EvidenceRef == "":
		return refuse(CodeInvalidRequest, req.WorkflowID, "quarantine evidence ref is required")
	case req.DeclaredBy == "":
		return refuse(CodeInvalidRequest, req.WorkflowID, "quarantine declarant is required")
	case req.ApprovedBy == "":
		return refuse(CodeInvalidRequest, req.WorkflowID, "distinct quarantine approver is required")
	case req.DeclaredBy == req.ApprovedBy:
		return refuse(CodeSeparationOfDuties, req.WorkflowID, "quarantine approver must be distinct from declarant")
	case req.EffectiveAt.IsZero():
		return refuse(CodeInvalidRequest, req.WorkflowID, "effective instant is required")
	default:
		return nil
	}
}

const (
	CodeInvalidRequest     = "INVALID_REQUEST"
	CodeSeparationOfDuties = "SEPARATION_OF_DUTIES"
	CodeRecordMutated      = "RECORD_MUTATED"
	CodeEventMutated       = "EVENT_MUTATED"
	CodeNotQuarantined     = "NOT_QUARANTINED"
	CodeAlreadyQuarantined = "ALREADY_QUARANTINED"
	CodePlanMismatch       = "PLAN_MISMATCH"
	CodeStartQuarantined   = "START_QUARANTINED"
)

var ErrQuarantine = errors.New("workflow/quarantine: rejected")

type Error struct{ Code, Ref, Detail string }

func (e *Error) Error() string {
	if e.Ref == "" {
		return "workflow/quarantine: " + e.Code + ": " + e.Detail
	}
	return "workflow/quarantine: " + e.Code + " [" + e.Ref + "]: " + e.Detail
}

func (e *Error) Unwrap() error { return ErrQuarantine }

func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func refuse(code, ref, format string, args ...any) *Error {
	return &Error{Code: code, Ref: ref, Detail: fmt.Sprintf(format, args...)}
}

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
