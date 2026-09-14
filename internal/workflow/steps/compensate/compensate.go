package compensate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"strings"
	"time"
)

type Status string

const (
	StatusCompensated    Status = "COMPENSATED"
	StatusPartial        Status = "PARTIAL"
	StatusFailed         Status = "FAILED"
	StatusRepairRequired Status = "REPAIR_REQUIRED"
)

func (s Status) Valid() bool {
	return s == StatusCompensated || s == StatusPartial || s == StatusFailed || s == StatusRepairRequired
}

type Strategy string

const (
	StrategyCorrection          Strategy = "CORRECTION"
	StrategySupersedingRevision Strategy = "SUPERSEDING_REVISION"
	StrategyRepairPlan          Strategy = "REPAIR_PLAN"
	StrategyIrreversible        Strategy = "IRREVERSIBLE"
)

func (s Strategy) Valid() bool {
	return s == StrategyCorrection || s == StrategySupersedingRevision || s == StrategyRepairPlan || s == StrategyIrreversible
}

type Request struct {
	TenantID, ActorID, TargetExecutionRef, TargetEffectRef, CompensationCapabilityRef, VerificationObservationRef, Reason, ApprovalPolicy, ApprovalRef, AuthorityPolicyFingerprint, CapabilityManifestDigest, PayloadDigest, IdempotencyKey, OriginalHistoryRef string
	Strategy                                                                                                                                                                                                                                                    Strategy
	RepairRef                                                                                                                                                                                                                                                   string
	ObservationMaxAge                                                                                                                                                                                                                                           time.Duration
}

func (r Request) Validate() error {
	for _, v := range []string{r.TenantID, r.ActorID, r.TargetExecutionRef, r.TargetEffectRef, r.CompensationCapabilityRef, r.VerificationObservationRef, r.Reason, r.ApprovalPolicy, r.ApprovalRef, r.AuthorityPolicyFingerprint, r.CapabilityManifestDigest, r.PayloadDigest, r.IdempotencyKey, r.OriginalHistoryRef} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%w: required binding is empty", ErrInvalidRequest)
		}
	}
	if !r.Strategy.Valid() {
		return fmt.Errorf("%w: unknown strategy", ErrInvalidRequest)
	}
	if r.Strategy == StrategyRepairPlan && r.RepairRef == "" {
		return fmt.Errorf("%w: repair ref required", ErrInvalidRequest)
	}
	if r.ObservationMaxAge <= 0 {
		return fmt.Errorf("%w: observation age required", ErrInvalidRequest)
	}
	return nil
}

type AuthorizationDecision struct {
	Allowed                                                                                                       bool
	TenantID, ActorID, TargetEffectRef, CapabilityRef, PayloadDigest, PolicyFingerprint, ApprovalRef, EvidenceRef string
}
type Authorizer interface {
	Authorize(context.Context, Request) (AuthorizationDecision, error)
}
type CapabilityManifest struct {
	CapabilityRef, Digest string
	Idempotent            bool
}
type CapabilityRequest struct {
	Request       Request
	RequestDigest string
}
type CapabilityReceipt struct {
	Accepted, Applied, Ambiguous bool
	EvidenceRef, Detail          string
}
type Capability interface {
	Manifest(context.Context) (CapabilityManifest, error)
	Compensate(context.Context, CapabilityRequest) (CapabilityReceipt, error)
}
type ObservationRequest struct {
	Request               Request
	CorrectionEvidenceRef string
}
type Observation struct {
	Match, Partial, Unknown                                               bool
	ObservedAt                                                            time.Time
	TenantID, TargetEffectRef, CorrectionEvidenceRef, EvidenceRef, Detail string
}
type Observer interface {
	ObserveCompensation(context.Context, ObservationRequest) (Observation, error)
}
type OperationState string

const (
	OperationReserved       OperationState = "RESERVED"
	OperationEffectRecorded OperationState = "EFFECT_RECORDED"
	OperationCompleted      OperationState = "COMPLETED"
)

type OperationKey struct{ TenantID, CapabilityRef, TargetEffectRef, IdempotencyKey string }
type OperationRecord struct {
	Key           OperationKey
	RequestDigest string
	State         OperationState
	Receipt       CapabilityReceipt
	Result        Result
}
type OperationOwner interface {
	Reserve(context.Context, OperationKey, string) (OperationRecord, bool, error)
	RecordEffect(context.Context, OperationKey, string, CapabilityReceipt) error
	Complete(context.Context, OperationKey, string, Result) error
}
type Event struct {
	Request                                                                                                                        Request
	Status                                                                                                                         Status
	AuthorizationEvidenceRef, CapabilityEvidenceRef, ObservationEvidenceRef, RemainingDriftRef, OriginalHistoryRef, Detail, Digest string
}
type Ledger interface {
	AppendCompensation(context.Context, Event) error
}
type Result struct {
	Status   Status
	Event    Event
	Replayed bool
}
type Executor struct {
	Capability Capability
	Authorizer Authorizer
	Observer   Observer
	Operations OperationOwner
	Ledger     Ledger
	Now        func() time.Time
}

func (e *Executor) Execute(ctx context.Context, r Request) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.steps.compensate.execute", r)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := r.Validate(); err != nil {
		return Result{}, err
	}
	if e == nil || e.Capability == nil || e.Authorizer == nil || e.Observer == nil || e.Operations == nil || e.Ledger == nil || e.Now == nil {
		return Result{}, fmt.Errorf("%w: coordinator ports required", ErrInvalidRequest)
	}
	a, err := e.Authorizer.Authorize(ctx, r)
	if err != nil || !authMatches(a, r) {
		return Result{}, fmt.Errorf("%w: unbound decision", ErrAuthorizationRequired)
	}
	m, err := e.Capability.Manifest(ctx)
	if err != nil || !m.Idempotent || m.CapabilityRef != r.CompensationCapabilityRef || m.Digest != r.CapabilityManifestDigest {
		return Result{}, fmt.Errorf("%w: manifest mismatch", ErrIdempotencyRequired)
	}
	d := requestDigest(r)
	k := OperationKey{r.TenantID, r.CompensationCapabilityRef, r.TargetEffectRef, r.IdempotencyKey}
	rec, created, err := e.Operations.Reserve(ctx, k, d)
	if err != nil || (!created && rec.RequestDigest != d) {
		return Result{}, fmt.Errorf("%w: reservation conflict", ErrIdempotencyRequired)
	}
	if !created && rec.Key != k {
		return Result{}, fmt.Errorf("%w: operation key binding mismatch", ErrIdempotencyRequired)
	}
	if !created && rec.State == OperationCompleted {
		if !validReplay(rec.Result, r, d, a.EvidenceRef, rec.Receipt.EvidenceRef) {
			return Result{}, fmt.Errorf("%w: corrupt replay", ErrIdempotencyRequired)
		}
		out := rec.Result
		out.Replayed = true
		return out, nil
	}
	if !created && rec.State == OperationReserved {
		return Result{}, fmt.Errorf("%w: operation is already reserved", ErrIdempotencyRequired)
	}
	if !created && rec.State != OperationEffectRecorded {
		return Result{}, fmt.Errorf("%w: unknown operation state %q", ErrIdempotencyRequired, rec.State)
	}
	if r.Strategy == StrategyIrreversible {
		return e.finish(ctx, k, d, r, a.EvidenceRef, CapabilityReceipt{}, Observation{}, StatusRepairRequired, "irreversible effect")
	}
	receipt := rec.Receipt
	if created {
		receipt, err = e.Capability.Compensate(ctx, CapabilityRequest{r, d})
		if err != nil {
			return e.finish(ctx, k, d, r, a.EvidenceRef, CapabilityReceipt{}, Observation{}, StatusRepairRequired, "capability failed")
		}
		if receipt.EvidenceRef == "" {
			return e.finish(ctx, k, d, r, a.EvidenceRef, receipt, Observation{}, StatusRepairRequired, "missing effect evidence")
		}
		if err = e.Operations.RecordEffect(ctx, k, d, receipt); err != nil {
			return e.finish(ctx, k, d, r, a.EvidenceRef, receipt, Observation{}, StatusRepairRequired, "effect outcome was not durably recorded")
		}
	}
	if !receipt.Accepted || (!receipt.Applied && !receipt.Ambiguous) {
		return e.finish(ctx, k, d, r, a.EvidenceRef, receipt, Observation{}, StatusFailed, "corrective effect was not accepted")
	}
	o, oe := e.Observer.ObserveCompensation(ctx, ObservationRequest{r, receipt.EvidenceRef})
	if oe != nil {
		return e.finish(ctx, k, d, r, a.EvidenceRef, receipt, Observation{}, StatusRepairRequired, "observation failed")
	}
	if !validObs(o, r, receipt, e.Now().UTC()) {
		return e.finish(ctx, k, d, r, a.EvidenceRef, receipt, o, StatusRepairRequired, "stale, contradictory, or unbound observation")
	}
	s := StatusFailed
	if o.Match && receipt.Applied {
		s = StatusCompensated
	} else if o.Unknown {
		s = StatusRepairRequired
	} else if o.Partial || receipt.Ambiguous {
		s = StatusPartial
	}
	return e.finish(ctx, k, d, r, a.EvidenceRef, receipt, o, s, verifiedDetail(s))
}

func validReplay(result Result, request Request, digest, authorizationEvidenceRef, capabilityEvidenceRef string) bool {
	event := result.Event
	return result.Status.Valid() && result.Status == event.Status &&
		!result.Replayed && requestDigest(event.Request) == digest &&
		event.Request.IdempotencyKey == request.IdempotencyKey &&
		event.AuthorizationEvidenceRef == authorizationEvidenceRef &&
		event.CapabilityEvidenceRef == capabilityEvidenceRef &&
		event.OriginalHistoryRef == request.OriginalHistoryRef &&
		event.Digest != "" && Digest(event) == event.Digest
}

func verifiedDetail(s Status) string {
	switch s {
	case StatusCompensated:
		return "corrective effect verified"
	case StatusPartial:
		return "corrective effect partially verified"
	case StatusRepairRequired:
		return "corrective effect requires repair"
	default:
		return "corrective effect verification failed"
	}
}
func authMatches(a AuthorizationDecision, r Request) bool {
	return a.Allowed && a.EvidenceRef != "" && a.TenantID == r.TenantID && a.ActorID == r.ActorID && a.TargetEffectRef == r.TargetEffectRef && a.CapabilityRef == r.CompensationCapabilityRef && a.PayloadDigest == r.PayloadDigest && a.PolicyFingerprint == r.AuthorityPolicyFingerprint && a.ApprovalRef == r.ApprovalRef
}
func validObs(o Observation, r Request, c CapabilityReceipt, n time.Time) bool {
	x := 0
	if o.Match {
		x++
	}
	if o.Partial {
		x++
	}
	if o.Unknown {
		x++
	}
	return x == 1 && o.EvidenceRef != "" && !o.ObservedAt.IsZero() && !o.ObservedAt.After(n) && n.Sub(o.ObservedAt) <= r.ObservationMaxAge && o.TenantID == r.TenantID && o.TargetEffectRef == r.TargetEffectRef && o.CorrectionEvidenceRef == c.EvidenceRef
}
func (e *Executor) finish(ctx context.Context, k OperationKey, d string, r Request, a string, c CapabilityReceipt, o Observation, s Status, detail string) (Result, error) {
	ev := Event{Request: r, Status: s, AuthorizationEvidenceRef: a, CapabilityEvidenceRef: c.EvidenceRef, ObservationEvidenceRef: o.EvidenceRef, OriginalHistoryRef: r.OriginalHistoryRef, Detail: detail}
	if s == StatusPartial || s == StatusRepairRequired {
		ev.RemainingDriftRef = r.RepairRef
	}
	ev.Digest = Digest(ev)
	out := Result{Status: s, Event: ev}
	if err := e.Ledger.AppendCompensation(ctx, ev); err != nil {
		return Result{}, err
	}
	if err := e.Operations.Complete(ctx, k, d, out); err != nil {
		return Result{}, err
	}
	return out, nil
}
func requestDigest(r Request) string {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(append([]byte("hcmnext.workflow.steps.compensate.Request/v1\x00"), b...))
	return hex.EncodeToString(h[:])
}
func Digest(e Event) string {
	e.Digest = ""
	b, _ := json.Marshal(e)
	h := sha256.Sum256(append([]byte("hcmnext.workflow.steps.compensate.Event/v2\x00"), b...))
	return hex.EncodeToString(h[:])
}
