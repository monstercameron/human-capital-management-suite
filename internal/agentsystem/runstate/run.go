package runstate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

var (
	ErrInvalid        = errors.New("runstate: invalid execution state")
	ErrConflict       = errors.New("runstate: revision conflict")
	ErrLease          = errors.New("runstate: worker lease is not current")
	ErrTerminal       = errors.New("runstate: execution is terminal")
	ErrProgressDenied = errors.New("runstate: progress is not visible to this actor")
)

// State is the durable execution outcome. RECONCILING is nonterminal and
// holds no worker lease; terminal values never return to RUNNING.
type State string

const (
	StateReady       State = "READY"
	StateRunning     State = "RUNNING"
	StateWaiting     State = "WAITING"
	StateReconciling State = "RECONCILING"
	StateCompleted   State = "COMPLETED"
	StateFailed      State = "FAILED"
	StateCancelled   State = "CANCELLED"
	StateExpired     State = "EXPIRED"
	StateNeedsRepair State = "NEEDS_REPAIR"
)

// Phase is a durable, typed boundary in one agent execution.
type Phase string

const (
	PhaseAdmission  Phase = "ADMISSION"
	PhaseContext    Phase = "CONTEXT"
	PhaseModelCall  Phase = "MODEL_CALL"
	PhaseToolCall   Phase = "TOOL_CALL"
	PhaseValidation Phase = "VALIDATION"
	PhaseDelivery   Phase = "DELIVERY"
)

// WaitKind classifies a durable wait without exposing its payload.
type WaitKind string

const (
	WaitSignal   WaitKind = "SIGNAL"
	WaitTimer    WaitKind = "TIMER"
	WaitApproval WaitKind = "APPROVAL"
	WaitUser     WaitKind = "USER_INPUT"
	WaitWorkflow WaitKind = "WORKFLOW"
)

// Checkpoint records only opaque references and digests; prompt and business
// data remain in their owning stores.
type Checkpoint struct {
	Sequence uint64    `json:"sequence"`
	Phase    Phase     `json:"phase"`
	Attempt  uint32    `json:"attempt"`
	Ref      string    `json:"ref,omitempty"`
	Digest   string    `json:"digest,omitempty"`
	At       time.Time `json:"at"`
}

// Lease fences a worker by a monotonically increasing token.
type Lease struct {
	Owner string    `json:"owner"`
	Fence uint64    `json:"fence"`
	Until time.Time `json:"until"`
}

// EffectStatus distinguishes an effect whose outcome is unknown from one
// proven applied or not applied by its owning capability.
type EffectStatus string

const (
	EffectUnknown    EffectStatus = "UNKNOWN"
	EffectApplied    EffectStatus = "APPLIED"
	EffectNotApplied EffectStatus = "NOT_APPLIED"
)

// Effect is a durable intent for an admitted business capability call.
type Effect struct {
	ID              string       `json:"id"`
	IdempotencyKey  string       `json:"idempotency_key"`
	ArgumentsDigest string       `json:"arguments_digest"`
	Status          EffectStatus `json:"status"`
	ResultRef       string       `json:"result_ref,omitempty"`
	ResultDigest    string       `json:"result_digest,omitempty"`
	StartedAt       time.Time    `json:"started_at"`
	ResolvedAt      time.Time    `json:"resolved_at,omitempty"`
}

// Run is the persisted projection of one accepted admission and its ordered
// execution evidence. Tenant and agent identity are pinned from admission.
type Run struct {
	ID               string           `json:"id"`
	TenantID         string           `json:"tenant_id"`
	AdmissionID      string           `json:"admission_id"`
	PrincipalMode    agentrun.RunMode `json:"principal_mode"`
	ActorID          string           `json:"actor_id"`
	RequestDigest    string           `json:"request_digest"`
	AgentID          string           `json:"agent_id"`
	AgentVersion     string           `json:"agent_version"`
	AgentDigest      string           `json:"agent_digest"`
	ContextDigest    string           `json:"context_digest"`
	Deadline         time.Time        `json:"deadline"`
	State            State            `json:"state"`
	Version          uint64           `json:"version"`
	Fence            uint64           `json:"fence"`
	Lease            *Lease           `json:"lease,omitempty"`
	Checkpoints      []Checkpoint     `json:"checkpoints"`
	Effects          []Effect         `json:"effects,omitempty"`
	CancelRequested  bool             `json:"cancel_requested,omitempty"`
	ExpireRequested  bool             `json:"expire_requested,omitempty"`
	FailureRequested bool             `json:"failure_requested,omitempty"`
	Retryable        bool             `json:"retryable,omitempty"`
	WaitKind         WaitKind         `json:"wait_kind,omitempty"`
	WaitRef          string           `json:"wait_ref,omitempty"`
	TerminalCode     string           `json:"terminal_code,omitempty"`
	FailureGate      string           `json:"failure_gate,omitempty"`
	FailureOwner     string           `json:"failure_owner,omitempty"`
	FailureLocation  string           `json:"failure_location,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// FailureGate is the closed operational vocabulary for why a run stopped.
type FailureGate string

const (
	FailureGateAuthority        FailureGate = "authority"
	FailureGateGrant            FailureGate = "grant"
	FailureGateInstallation     FailureGate = "installation"
	FailureGateAudience         FailureGate = "audience"
	FailureGateBudget           FailureGate = "budget"
	FailureGateModelRoute       FailureGate = "model_route"
	FailureGateModelCall        FailureGate = "model_call"
	FailureGateModelOutput      FailureGate = "model_output"
	FailureGateToolScope        FailureGate = "tool_scope"
	FailureGateToolCall         FailureGate = "tool_call"
	FailureGateOutputGrounding  FailureGate = "output_grounding"
	FailureGateOutputSchema     FailureGate = "output_schema"
	FailureGateDeliveryAudience FailureGate = "delivery_audience"
	FailureGateDeliveryWrite    FailureGate = "delivery_write"
	FailureGateDeadline         FailureGate = "deadline"
	FailureGateStopped          FailureGate = "stopped"
)

// FailureRefusal carries only closed-list operational fields. It must never
// contain prompts, document text, model text, provider errors, or request body.
type FailureRefusal struct {
	Gate     FailureGate `json:"gate"`
	Owner    string      `json:"owner"`
	Location string      `json:"location"`
}

func NewFailureRefusal(gate FailureGate, owner string) FailureRefusal {
	_, file, line, ok := runtime.Caller(1)
	location := "unknown:0"
	if ok {
		location = fmt.Sprintf("%s:%d", filepath.Base(file), line)
	}
	return FailureRefusal{Gate: gate, Owner: owner, Location: location}
}

// Store persists execution snapshots with compare-and-swap revisions.
type Store interface {
	Create(context.Context, Run) error
	Get(context.Context, string) (Run, error)
	Save(context.Context, Run, uint64) error
}

// AdmissionRechecker reloads current grants, stop controls and pinned context
// by admission ID. It runs before claiming work and before model/tool/delivery
// boundaries; the owner gateway still reauthorizes the actual effect.
type AdmissionRechecker interface {
	Recheck(context.Context, string, string) error
}

// Service coordinates durable execution transitions over a tenant-scoped
// store. It does not invoke models, tools, or delivery adapters itself.
type Service struct {
	store   Store
	recheck AdmissionRechecker
}

// New constructs a service with durable state storage.
func New(store Store, recheck AdmissionRechecker) (*Service, error) {
	if store == nil || recheck == nil {
		return nil, fmt.Errorf("%w: durable store and current-authority rechecker are required", ErrInvalid)
	}
	return &Service{store: store, recheck: recheck}, nil
}

// Start creates execution only from a valid accepted admission record. The
// admission ID is the stable run ID across duplicate source delivery.
func (s *Service) Start(ctx context.Context, admission agentrun.Record) (Run, error) {
	if s == nil || s.store == nil || agentrun.ValidateAdmissionRecord(admission) != nil || admission.Decision != agentrun.DecisionAccepted {
		return Run{}, fmt.Errorf("%w: accepted admission is required", ErrInvalid)
	}
	now := admission.AdmittedAt.UTC()
	run := Run{
		ID: admission.ID, AdmissionID: admission.ID, TenantID: admission.Request.Source.TenantID,
		PrincipalMode: admission.Request.Principal.Mode, ActorID: admission.Request.Principal.InvokerID,
		RequestDigest: admission.RequestDigest, AgentID: admission.Authority.Agent.AgentID,
		AgentVersion: admission.Authority.Agent.Version, AgentDigest: admission.Authority.Agent.Digest,
		ContextDigest: admission.Authority.Context.Digest, Deadline: admission.Request.Deadline.UTC(),
		State: StateReady, Version: 1, CreatedAt: now, UpdatedAt: now,
		Checkpoints: []Checkpoint{{Sequence: 1, Phase: PhaseAdmission, Digest: "sha256:" + admission.RequestDigest, At: now}},
	}
	if run.PrincipalMode == agentrun.ModeSponsored {
		run.ActorID = admission.Request.Principal.SponsorID
	}
	if err := s.store.Create(ctx, cloneRun(run)); err != nil {
		return Run{}, err
	}
	return run, nil
}

// Claim grants a fenced execution lease to one worker.
func (s *Service) Claim(ctx context.Context, id, owner string, now time.Time, ttl time.Duration) (Run, error) {
	run, err := s.get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if !clean(owner) || now.IsZero() || ttl <= 0 || run.State != StateReady || !run.Deadline.After(now) {
		return Run{}, fmt.Errorf("%w: run cannot be claimed", ErrInvalid)
	}
	if err := s.recheck.Recheck(ctx, run.TenantID, run.AdmissionID); err != nil {
		return Run{}, fmt.Errorf("runstate: current authority recheck: %w", err)
	}
	prior := run.Version
	run.Fence++
	run.Lease = &Lease{Owner: owner, Fence: run.Fence, Until: now.UTC().Add(ttl)}
	run.State, run.Version, run.UpdatedAt = StateRunning, run.Version+1, now.UTC()
	if err := s.store.Save(ctx, run, prior); err != nil {
		return Run{}, err
	}
	return run, nil
}

// Checkpoint appends a non-effect execution boundary under the current lease.
func (s *Service) Checkpoint(ctx context.Context, id, owner string, fence, expected uint64, phase Phase, attempt uint32, ref, digest string, now time.Time) (Run, error) {
	run, err := s.currentLease(ctx, id, owner, fence, expected, now)
	if err != nil {
		return Run{}, err
	}
	if !validPhase(phase) || phase == PhaseAdmission || phase == PhaseToolCall || !cleanOptional(ref) || !cleanOptional(digest) || (ref == "" && digest == "") {
		return Run{}, fmt.Errorf("%w: checkpoint fields", ErrInvalid)
	}
	if phase == PhaseModelCall || phase == PhaseDelivery {
		if err := s.recheck.Recheck(ctx, run.TenantID, run.AdmissionID); err != nil {
			return Run{}, fmt.Errorf("runstate: current authority recheck: %w", err)
		}
	}
	prior := run.Version
	run.append(Checkpoint{Phase: phase, Attempt: attempt, Ref: ref, Digest: digest, At: now.UTC()})
	run.Version++
	run.UpdatedAt = now.UTC()
	if phase == PhaseDelivery {
		if hasUnknownEffect(run.Effects) {
			return Run{}, fmt.Errorf("%w: unresolved effect blocks delivery", ErrInvalid)
		}
		run.State, run.TerminalCode, run.Lease = StateCompleted, "DELIVERED", nil
	}
	if err := s.store.Save(ctx, run, prior); err != nil {
		return Run{}, err
	}
	return run, nil
}

// BeginEffect persists intent before a capability call. A crash from this
// point onward is conservatively ambiguous until the owning capability is
// queried, even if the worker never reached the network call.
func (s *Service) BeginEffect(ctx context.Context, id, owner, effectID, key, argsDigest string, fence, expected uint64, now time.Time) (Run, error) {
	run, err := s.currentLease(ctx, id, owner, fence, expected, now)
	if err != nil {
		return Run{}, err
	}
	if !clean(effectID) || !clean(key) || !digest(argsDigest) || findEffect(run.Effects, effectID) >= 0 {
		return Run{}, fmt.Errorf("%w: effect identity", ErrInvalid)
	}
	if err := s.recheck.Recheck(ctx, run.TenantID, run.AdmissionID); err != nil {
		return Run{}, fmt.Errorf("runstate: current authority recheck: %w", err)
	}
	for _, effect := range run.Effects {
		if effect.IdempotencyKey == key {
			return Run{}, fmt.Errorf("%w: duplicate effect key", ErrInvalid)
		}
	}
	prior := run.Version
	run.Effects = append(run.Effects, Effect{ID: effectID, IdempotencyKey: key, ArgumentsDigest: argsDigest, Status: EffectUnknown, StartedAt: now.UTC()})
	run.append(Checkpoint{Phase: PhaseToolCall, Attempt: 1, Ref: effectID, Digest: argsDigest, At: now.UTC()})
	run.Version++
	run.UpdatedAt = now.UTC()
	if err := s.store.Save(ctx, run, prior); err != nil {
		return Run{}, err
	}
	return run, nil
}

// ResolveEffect records the owner's authoritative observation; UNKNOWN is
// never accepted as a resolution.
func (s *Service) ResolveEffect(ctx context.Context, id, owner, effectID string, fence, expected uint64, status EffectStatus, resultRef, resultDigest string, now time.Time) (Run, error) {
	run, err := s.currentLease(ctx, id, owner, fence, expected, now)
	if err != nil {
		return Run{}, err
	}
	idx := findEffect(run.Effects, effectID)
	if idx < 0 || run.Effects[idx].Status != EffectUnknown || (status != EffectApplied && status != EffectNotApplied) || !cleanOptional(resultRef) || !cleanOptional(resultDigest) {
		return Run{}, fmt.Errorf("%w: effect observation", ErrInvalid)
	}
	if status == EffectApplied && (!clean(resultRef) || !digest(resultDigest)) {
		return Run{}, fmt.Errorf("%w: applied effect needs owner evidence", ErrInvalid)
	}
	prior := run.Version
	effect := &run.Effects[idx]
	effect.Status, effect.ResultRef, effect.ResultDigest, effect.ResolvedAt = status, resultRef, resultDigest, now.UTC()
	run.append(resolvedEffectCheckpoint(*effect, now))
	run.Version++
	run.UpdatedAt = now.UTC()
	run.finishRequestedTerminal()
	if err := s.store.Save(ctx, run, prior); err != nil {
		return Run{}, err
	}
	return run, nil
}

// Recover releases an expired worker lease. An effect with no owner outcome
// enters RECONCILING, never READY, so recovery cannot replay it implicitly.
func (s *Service) Recover(ctx context.Context, id string, expected uint64, now time.Time) (Run, error) {
	run, err := s.get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if run.Version != expected {
		return Run{}, ErrConflict
	}
	if run.State != StateRunning || run.Lease == nil || run.Lease.Until.After(now) || now.IsZero() {
		return Run{}, fmt.Errorf("%w: no expired lease", ErrInvalid)
	}
	prior := run.Version
	run.Lease = nil
	if !run.Deadline.After(now) {
		run.ExpireRequested = true
	}
	if hasUnknownEffect(run.Effects) {
		run.State = StateReconciling
	} else {
		run.State = StateReady
		run.finishRequestedTerminal()
	}
	run.Version++
	run.UpdatedAt = now.UTC()
	if err := s.store.Save(ctx, run, prior); err != nil {
		return Run{}, err
	}
	return run, nil
}

// Park releases the worker lease while execution waits for an external signal.
func (s *Service) Park(ctx context.Context, id, owner string, fence, expected uint64, kind WaitKind, ref string, now time.Time) (Run, error) {
	run, err := s.currentLease(ctx, id, owner, fence, expected, now)
	if err != nil {
		return Run{}, err
	}
	if hasUnknownEffect(run.Effects) || !validWaitKind(kind) || !clean(ref) {
		return Run{}, fmt.Errorf("%w: unresolved effect or invalid wait condition", ErrInvalid)
	}
	prior := run.Version
	run.WaitKind, run.WaitRef = kind, ref
	run.State, run.Lease, run.Version, run.UpdatedAt = StateWaiting, nil, run.Version+1, now.UTC()
	if err := s.store.Save(ctx, run, prior); err != nil {
		return Run{}, err
	}
	return run, nil
}

// Resume makes a durably waiting run eligible for a new worker lease.
func (s *Service) Resume(ctx context.Context, id string, expected uint64, now time.Time) (Run, error) {
	run, err := s.get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if run.Version != expected {
		return Run{}, ErrConflict
	}
	if run.State != StateWaiting || now.IsZero() || !run.Deadline.After(now) {
		return Run{}, fmt.Errorf("%w: waiting run or deadline invalid", ErrInvalid)
	}
	prior := run.Version
	run.WaitKind, run.WaitRef = "", ""
	run.State, run.Version, run.UpdatedAt = StateReady, run.Version+1, now.UTC()
	if err := s.store.Save(ctx, run, prior); err != nil {
		return Run{}, err
	}
	return run, nil
}

// Fail records a typed non-sensitive failure outcome. Ambiguous effects remain
// in RECONCILING until their owner supplies authoritative evidence.
func (s *Service) Fail(ctx context.Context, id, owner, code string, retryable bool, fence, expected uint64, now time.Time) (Run, error) {
	return s.FailWithRefusal(ctx, id, owner, code, retryable, FailureRefusal{}, fence, expected, now)
}

// FailWithRefusal records a typed non-sensitive failure with closed-list
// operator diagnostics. Ambiguous effects remain in RECONCILING until their
// owner supplies authoritative evidence.
func (s *Service) FailWithRefusal(ctx context.Context, id, owner, code string, retryable bool, refusal FailureRefusal, fence, expected uint64, now time.Time) (Run, error) {
	run, err := s.currentLease(ctx, id, owner, fence, expected, now)
	if err != nil {
		return Run{}, err
	}
	if !cleanCode(code) || !validFailureRefusal(refusal) {
		return Run{}, fmt.Errorf("%w: failure code", ErrInvalid)
	}
	prior := run.Version
	run.FailureRequested, run.TerminalCode, run.Retryable = true, code, retryable
	run.FailureGate, run.FailureOwner, run.FailureLocation = string(refusal.Gate), refusal.Owner, refusal.Location
	run.Lease, run.Version, run.UpdatedAt = nil, run.Version+1, now.UTC()
	run.finishRequestedTerminal()
	if err := s.store.Save(ctx, run, prior); err != nil {
		return Run{}, err
	}
	return run, nil
}

// ReconcileEffect resolves an ambiguous capability outcome after a restart.
// An unresolved or still-unknown result cannot advance the run.
func (s *Service) ReconcileEffect(ctx context.Context, id, effectID string, expected uint64, status EffectStatus, resultRef, resultDigest string, now time.Time) (Run, error) {
	run, err := s.get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if run.Version != expected {
		return Run{}, ErrConflict
	}
	idx := findEffect(run.Effects, effectID)
	if run.State != StateReconciling || idx < 0 || run.Effects[idx].Status != EffectUnknown || now.IsZero() || (status != EffectApplied && status != EffectNotApplied) {
		return Run{}, fmt.Errorf("%w: reconciliation evidence", ErrInvalid)
	}
	if status == EffectApplied && (!clean(resultRef) || !digest(resultDigest)) {
		return Run{}, fmt.Errorf("%w: applied effect needs owner evidence", ErrInvalid)
	}
	prior := run.Version
	run.Effects[idx].Status, run.Effects[idx].ResultRef = status, resultRef
	run.Effects[idx].ResultDigest, run.Effects[idx].ResolvedAt = resultDigest, now.UTC()
	run.append(resolvedEffectCheckpoint(run.Effects[idx], now))
	run.Version++
	run.UpdatedAt = now.UTC()
	if !hasUnknownEffect(run.Effects) {
		run.finishRequestedTerminal()
		if run.State == StateReconciling {
			run.State = StateReady
		}
	}
	if err := s.store.Save(ctx, run, prior); err != nil {
		return Run{}, err
	}
	return run, nil
}

// A declined effect has no owner result. Its checkpoint names the admitted
// effect and arguments, while the effect record retains NOT_APPLIED.
func resolvedEffectCheckpoint(effect Effect, now time.Time) Checkpoint {
	ref, digest := effect.ResultRef, effect.ResultDigest
	if effect.Status == EffectNotApplied && ref == "" && digest == "" {
		ref, digest = effect.ID, effect.ArgumentsDigest
	}
	return Checkpoint{Phase: PhaseToolCall, Attempt: 2, Ref: ref, Digest: digest, At: now.UTC()}
}

// Cancel requests cancellation. An unresolved external effect must be
// reconciled before the run can become terminally CANCELLED.
func (s *Service) Cancel(ctx context.Context, id string, expected uint64, now time.Time) (Run, error) {
	return s.requestTerminal(ctx, id, expected, now, true)
}

// Expire applies the pinned admission deadline as a typed terminal outcome.
func (s *Service) Expire(ctx context.Context, id string, expected uint64, now time.Time) (Run, error) {
	run, err := s.get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if now.IsZero() || now.Before(run.Deadline) {
		return Run{}, fmt.Errorf("%w: deadline has not elapsed", ErrInvalid)
	}
	return s.requestTerminal(ctx, id, expected, now, false)
}

func (s *Service) requestTerminal(ctx context.Context, id string, expected uint64, now time.Time, cancel bool) (Run, error) {
	run, err := s.get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if run.Version != expected {
		return Run{}, ErrConflict
	}
	if terminal(run.State) {
		return Run{}, ErrTerminal
	}
	if now.IsZero() {
		return Run{}, fmt.Errorf("%w: time required", ErrInvalid)
	}
	prior := run.Version
	if cancel {
		run.CancelRequested = true
	} else {
		run.ExpireRequested = true
	}
	run.Lease = nil
	run.Version++
	run.UpdatedAt = now.UTC()
	run.finishRequestedTerminal()
	if err := s.store.Save(ctx, run, prior); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (r *Run) finishRequestedTerminal() {
	if hasUnknownEffect(r.Effects) {
		r.State = StateReconciling
		return
	}
	switch {
	case r.CancelRequested:
		r.State, r.TerminalCode = StateCancelled, "CANCELLED"
	case r.ExpireRequested:
		r.State, r.TerminalCode = StateExpired, "EXPIRED"
	case r.FailureRequested:
		r.State = StateFailed
	}
}

func (s *Service) currentLease(ctx context.Context, id, owner string, fence, expected uint64, now time.Time) (Run, error) {
	run, err := s.get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if run.Version != expected {
		return Run{}, ErrConflict
	}
	if run.State != StateRunning || run.Lease == nil || run.Lease.Owner != owner || run.Lease.Fence != fence || fence != run.Fence || !run.Lease.Until.After(now) {
		return Run{}, ErrLease
	}
	return run, nil
}

func (s *Service) get(ctx context.Context, id string) (Run, error) {
	if s == nil || s.store == nil || !clean(id) {
		return Run{}, ErrInvalid
	}
	return s.store.Get(ctx, id)
}

func (r *Run) append(checkpoint Checkpoint) {
	checkpoint.Sequence = uint64(len(r.Checkpoints) + 1)
	r.Checkpoints = append(r.Checkpoints, checkpoint)
}

func cloneRun(r Run) Run {
	r.Checkpoints = append([]Checkpoint(nil), r.Checkpoints...)
	r.Effects = append([]Effect(nil), r.Effects...)
	if r.Lease != nil {
		lease := *r.Lease
		r.Lease = &lease
	}
	return r
}

func findEffect(effects []Effect, id string) int {
	for i := range effects {
		if effects[i].ID == id {
			return i
		}
	}
	return -1
}
func hasUnknownEffect(effects []Effect) bool {
	for _, effect := range effects {
		if effect.Status == EffectUnknown {
			return true
		}
	}
	return false
}
func terminal(state State) bool {
	return state == StateCompleted || state == StateFailed || state == StateCancelled || state == StateExpired || state == StateNeedsRepair
}
func validPhase(phase Phase) bool {
	return phase == PhaseAdmission || phase == PhaseContext || phase == PhaseModelCall || phase == PhaseToolCall || phase == PhaseValidation || phase == PhaseDelivery
}
func validWaitKind(kind WaitKind) bool {
	return kind == WaitSignal || kind == WaitTimer || kind == WaitApproval || kind == WaitUser || kind == WaitWorkflow
}
func clean(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsRune(value, 0)
}
func cleanOptional(value string) bool { return value == "" || clean(value) }
func digest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[7:] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
func cleanCode(value string) bool {
	if value == "" || len(value) > 96 {
		return false
	}
	for _, char := range value {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func validFailureRefusal(refusal FailureRefusal) bool {
	if refusal == (FailureRefusal{}) {
		return true
	}
	if !validFailureGate(refusal.Gate) || !clean(refusal.Owner) || !clean(refusal.Location) || strings.ContainsAny(refusal.Owner+refusal.Location, "\r\n") {
		return false
	}
	return len(refusal.Owner) <= 96 && len(refusal.Location) <= 160
}

func validFailureGate(gate FailureGate) bool {
	switch gate {
	case FailureGateAuthority, FailureGateGrant, FailureGateInstallation, FailureGateAudience, FailureGateBudget,
		FailureGateModelRoute, FailureGateModelCall, FailureGateModelOutput, FailureGateToolScope, FailureGateToolCall,
		FailureGateOutputGrounding, FailureGateOutputSchema, FailureGateDeliveryAudience, FailureGateDeliveryWrite,
		FailureGateDeadline, FailureGateStopped:
		return true
	default:
		return false
	}
}
