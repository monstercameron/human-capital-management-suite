package configbundle

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrKillSwitchInvalid    = errors.New("configbundle: invalid kill switch")
	ErrKillSwitchSigner     = errors.New("configbundle: kill switch signer is required")
	ErrKillSwitchConflict   = errors.New("configbundle: kill switch identity conflicts")
	ErrKillSwitchNotApplied = errors.New("configbundle: kill switch has not been applied")
	ErrKillSwitchReceipt    = errors.New("configbundle: invalid kill switch receipt")
)

// KillSwitchTarget is an exact-or-wildcard target. Empty dimensions are
// wildcards; the target itself must still contain at least one dimension.
type KillSwitchTarget struct {
	TenantID   string
	CellID     string
	Service    string
	Capability string
	Model      string
	Connector  string
}

// Target is a concise compatibility alias.
type Target = KillSwitchTarget

// Matches reports whether a target applies to a runtime subject.
func (t KillSwitchTarget) Matches(subject KillSwitchTarget) bool {
	return matchesDimension(t.TenantID, subject.TenantID) && matchesDimension(t.CellID, subject.CellID) &&
		matchesDimension(t.Service, subject.Service) && matchesDimension(t.Capability, subject.Capability) &&
		matchesDimension(t.Model, subject.Model) && matchesDimension(t.Connector, subject.Connector)
}

func matchesDimension(pattern, value string) bool { return pattern == "" || pattern == value }

// Specificity is used after explicit Priority to make ties deterministic.
func (t KillSwitchTarget) Specificity() int {
	count := 0
	for _, value := range []string{t.TenantID, t.CellID, t.Service, t.Capability, t.Model, t.Connector} {
		if value != "" {
			count++
		}
	}
	return count
}

// KillSwitchRequest contains the dual-control and incident evidence needed to
// issue an emergency revocation. References are opaque and never dereferenced
// by this pure package.
type KillSwitchRequest struct {
	SwitchID       string
	Target         KillSwitchTarget
	Priority       uint32
	Reason         string
	IncidentRef    string
	Operator       string
	Approver       string
	EvidenceRef    string
	IssuedAt       time.Time
	ExpiresAt      time.Time
	PropagationSLO time.Duration
}

// EmergencyKillSwitch is a descriptive alias for the request.
type EmergencyKillSwitch = KillSwitchRequest

// SignedKillSwitch is immutable, signed revocation evidence.
type SignedKillSwitch struct {
	Request   KillSwitchRequest
	Digest    string
	Signature BundleSignature
}

// KillSwitch is a concise compatibility alias.
type KillSwitch = SignedKillSwitch

// AppliedKillSwitchReceipt records application and propagation timing.
type AppliedKillSwitchReceipt struct {
	SwitchID     string
	SwitchDigest string
	Target       KillSwitchTarget
	AppliedAt    time.Time
	WithinSLO    bool
	Status       string
	Digest       string
	Signature    BundleSignature
}

// KillSwitchReceipt is the concise spelling for an applied receipt.
type KillSwitchReceipt = AppliedKillSwitchReceipt

// KillDecision is the local fail-closed result for one runtime subject.
type KillDecision struct {
	Disabled     bool
	SwitchID     string
	SwitchDigest string
	Priority     uint32
	Reason       string
	ExpiresAt    time.Time
}

// KillSwitchStore is a concurrency-safe, in-memory control-plane and local
// receiver. It retains switches and applied receipts; it never deletes them.
type KillSwitchStore struct {
	signer         ReceiptSigner
	publicKey      ed25519.PublicKey
	now            func() time.Time
	mu             sync.RWMutex
	switches       map[string]SignedKillSwitch
	receipts       map[string]AppliedKillSwitchReceipt
	persistApplied func(SignedKillSwitch, AppliedKillSwitchReceipt) error
}

// SetAppliedWriter installs the durable commit boundary. Apply publishes the
// signed switch and receipt to this writer before exposing them as applied in
// memory. Composition must install it before serving requests.
func (s *KillSwitchStore) SetAppliedWriter(writer func(SignedKillSwitch, AppliedKillSwitchReceipt) error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.persistApplied = writer
	s.mu.Unlock()
}

// RestoreApplied rehydrates a durable applied record after verifying both
// signatures and their shared identity. Conflicting records are rejected.
func (s *KillSwitchStore) RestoreApplied(switchValue SignedKillSwitch, receipt AppliedKillSwitchReceipt) error {
	if s == nil || s.signer == nil || switchValue.Verify(s.publicKey) != nil || receipt.Verify(s.publicKey) != nil ||
		receipt.SwitchID != switchValue.Request.SwitchID || receipt.SwitchDigest != switchValue.Digest || receipt.Target != switchValue.Request.Target {
		return ErrKillSwitchReceipt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.switches[switchValue.Request.SwitchID]; ok && prior.Digest != switchValue.Digest {
		return ErrKillSwitchConflict
	}
	if prior, ok := s.receipts[switchValue.Request.SwitchID]; ok && prior.Digest != receipt.Digest {
		return ErrKillSwitchConflict
	}
	s.switches[switchValue.Request.SwitchID] = cloneSignedKillSwitch(switchValue)
	s.receipts[switchValue.Request.SwitchID] = cloneKillSwitchReceipt(receipt)
	return nil
}

// NewKillSwitchStore creates a pure store. The public key is optional for
// local issue-and-apply tests; when supplied, Apply verifies the switch.
func NewKillSwitchStore(signer ReceiptSigner, publicKeys ...ed25519.PublicKey) *KillSwitchStore {
	now := func() time.Time { return time.Now().UTC() }
	var publicKey ed25519.PublicKey
	if len(publicKeys) > 0 {
		publicKey = append(ed25519.PublicKey(nil), publicKeys[0]...)
	}
	return &KillSwitchStore{signer: signer, publicKey: publicKey, now: now, switches: make(map[string]SignedKillSwitch), receipts: make(map[string]AppliedKillSwitchReceipt)}
}

// NewKillSwitchManager is an explicit lifecycle constructor alias.
func NewKillSwitchManager(signer ReceiptSigner, publicKeys ...ed25519.PublicKey) *KillSwitchStore {
	return NewKillSwitchStore(signer, publicKeys...)
}

// SetClock is test-only composition without introducing a clock dependency.
func (s *KillSwitchStore) SetClock(clock func() time.Time) {
	if s == nil || clock == nil {
		return
	}
	s.mu.Lock()
	s.now = clock
	s.mu.Unlock()
}

// Issue validates, digests, signs, and retains one emergency switch.
func (s *KillSwitchStore) Issue(request KillSwitchRequest) (SignedKillSwitch, error) {
	if s == nil || s.signer == nil {
		return SignedKillSwitch{}, ErrKillSwitchSigner
	}
	if err := validateKillSwitchRequest(request); err != nil {
		return SignedKillSwitch{}, err
	}
	result := SignedKillSwitch{Request: cloneKillSwitchRequest(request)}
	digest, err := result.DigestValue()
	if err != nil {
		return SignedKillSwitch{}, err
	}
	result.Digest = digest
	signature, err := s.signer.SignDigest(digest)
	if err != nil {
		return SignedKillSwitch{}, fmt.Errorf("%w: %v", ErrKillSwitchSigner, err)
	}
	if err := validateSignatureIdentity(signature); err != nil {
		return SignedKillSwitch{}, err
	}
	result.Signature = signature
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.switches[request.SwitchID]; ok {
		if prior.Digest == result.Digest {
			return cloneSignedKillSwitch(prior), nil
		}
		return SignedKillSwitch{}, ErrKillSwitchConflict
	}
	s.switches[request.SwitchID] = cloneSignedKillSwitch(result)
	return cloneSignedKillSwitch(result), nil
}

// Publish is the publication-oriented alias for Issue.
func (s *KillSwitchStore) Publish(request KillSwitchRequest) (SignedKillSwitch, error) {
	return s.Issue(request)
}

// Apply verifies and applies a switch locally, then emits a signed receipt.
// A late application is still applied fail-closed, but the receipt marks the
// propagation SLO miss so operations can create or escalate an incident.
func (s *KillSwitchStore) Apply(switchValue SignedKillSwitch, appliedAt ...time.Time) (AppliedKillSwitchReceipt, error) {
	if s == nil || s.signer == nil {
		return AppliedKillSwitchReceipt{}, ErrKillSwitchSigner
	}
	if s.publicKey != nil {
		if err := switchValue.Verify(s.publicKey); err != nil {
			return AppliedKillSwitchReceipt{}, err
		}
	} else if err := switchValue.Validate(); err != nil {
		return AppliedKillSwitchReceipt{}, err
	}
	at := s.now().UTC()
	if len(appliedAt) > 0 && !appliedAt[0].IsZero() {
		at = appliedAt[0].UTC()
	}
	if !at.Before(switchValue.Request.ExpiresAt) {
		return AppliedKillSwitchReceipt{}, ErrKillSwitchInvalid
	}
	within := !at.Before(switchValue.Request.IssuedAt) && at.Sub(switchValue.Request.IssuedAt) <= switchValue.Request.PropagationSLO
	receipt := AppliedKillSwitchReceipt{SwitchID: switchValue.Request.SwitchID, SwitchDigest: switchValue.Digest, Target: switchValue.Request.Target, AppliedAt: at, WithinSLO: within, Status: "APPLIED"}
	if !within {
		receipt.Status = "APPLIED_LATE"
	}
	digest, err := receipt.DigestValue()
	if err != nil {
		return AppliedKillSwitchReceipt{}, err
	}
	receipt.Digest = digest
	receipt.Signature, err = s.signer.SignDigest(digest)
	if err != nil {
		return AppliedKillSwitchReceipt{}, fmt.Errorf("%w: %v", ErrKillSwitchSigner, err)
	}
	if err := validateSignatureIdentity(receipt.Signature); err != nil {
		return AppliedKillSwitchReceipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.receipts[switchValue.Request.SwitchID]; ok {
		return cloneKillSwitchReceipt(prior), nil
	}
	if s.persistApplied != nil {
		if err := s.persistApplied(cloneSignedKillSwitch(switchValue), cloneKillSwitchReceipt(receipt)); err != nil {
			return AppliedKillSwitchReceipt{}, fmt.Errorf("configbundle: persist applied kill switch: %w", err)
		}
	}
	s.switches[switchValue.Request.SwitchID] = cloneSignedKillSwitch(switchValue)
	s.receipts[switchValue.Request.SwitchID] = cloneKillSwitchReceipt(receipt)
	return cloneKillSwitchReceipt(receipt), nil
}

// ApplySwitch is an explicit alias for Apply.
func (s *KillSwitchStore) ApplySwitch(switchValue SignedKillSwitch, appliedAt ...time.Time) (AppliedKillSwitchReceipt, error) {
	return s.Apply(switchValue, appliedAt...)
}

// Evaluate returns the highest-precedence currently applied switch. Expired
// switches are ignored, while a missing applied switch is fail-open only for
// that local subject and never reports a false revocation.
func (s *KillSwitchStore) Evaluate(subject KillSwitchTarget, now time.Time) KillDecision {
	if s == nil {
		return KillDecision{}
	}
	if now.IsZero() {
		now = s.now().UTC()
	} else {
		now = now.UTC()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.evaluateLocked(subject, now)
}

// Guard evaluates the applied CP-008 state and runs proceed while the
// switch state is read-locked. Applying a switch therefore cannot race past
// a guarded stage transition.
func (s *KillSwitchStore) Guard(subject KillSwitchTarget, proceed func(KillDecision) error) error {
	if s == nil || proceed == nil {
		return ErrKillSwitchInvalid
	}
	s.mu.RLock()
	now := s.now().UTC()
	defer s.mu.RUnlock()
	return proceed(s.evaluateLocked(subject, now))
}

func (s *KillSwitchStore) evaluateLocked(subject KillSwitchTarget, now time.Time) KillDecision {
	var candidates []SignedKillSwitch
	for id, switchValue := range s.switches {
		if _, applied := s.receipts[id]; !applied || !switchValue.Request.Target.Matches(subject) || !now.Before(switchValue.Request.ExpiresAt) {
			continue
		}
		candidates = append(candidates, switchValue)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i].Request, candidates[j].Request
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		if left.Target.Specificity() != right.Target.Specificity() {
			return left.Target.Specificity() > right.Target.Specificity()
		}
		if !left.IssuedAt.Equal(right.IssuedAt) {
			return left.IssuedAt.After(right.IssuedAt)
		}
		return candidates[i].Digest < candidates[j].Digest
	})
	if len(candidates) == 0 {
		return KillDecision{Reason: "NO_ACTIVE_SWITCH"}
	}
	selected := candidates[0]
	return KillDecision{Disabled: true, SwitchID: selected.Request.SwitchID, SwitchDigest: selected.Digest, Priority: selected.Request.Priority, Reason: selected.Request.Reason, ExpiresAt: selected.Request.ExpiresAt}
}

// Active is a descriptive alias for Evaluate.
func (s *KillSwitchStore) Active(subject KillSwitchTarget, now time.Time) (KillDecision, bool) {
	decision := s.Evaluate(subject, now)
	return decision, decision.Disabled
}

// Get and Receipt return defensive evidence copies.
func (s *KillSwitchStore) Get(id string) (SignedKillSwitch, bool) {
	if s == nil {
		return SignedKillSwitch{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.switches[id]
	return cloneSignedKillSwitch(v), ok
}

func (s *KillSwitchStore) Receipt(id string) (AppliedKillSwitchReceipt, bool) {
	if s == nil {
		return AppliedKillSwitchReceipt{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.receipts[id]
	return cloneKillSwitchReceipt(v), ok
}

// DigestValue computes the switch digest without trusting signed fields.
func (s SignedKillSwitch) DigestValue() (string, error) {
	if err := validateKillSwitchRequest(s.Request); err != nil {
		return "", err
	}
	t := s.Request.Target
	w := canonicalbytes.New("hcmnext.platform.configbundle.SignedKillSwitch", 1).
		String("switch_id", s.Request.SwitchID).
		String("tenant_id", t.TenantID).String("cell_id", t.CellID).
		String("service", t.Service).String("capability", t.Capability).
		String("model", t.Model).String("connector", t.Connector).
		Int("priority", int64(s.Request.Priority)).String("reason", s.Request.Reason).
		String("incident_ref", s.Request.IncidentRef).String("operator", s.Request.Operator).
		String("approver", s.Request.Approver).String("evidence_ref", s.Request.EvidenceRef).
		String("issued_at", s.Request.IssuedAt.UTC().Format(time.RFC3339Nano)).
		String("expires_at", s.Request.ExpiresAt.UTC().Format(time.RFC3339Nano)).
		Int("propagation_slo_ns", int64(s.Request.PropagationSLO))
	return w.Digest()
}

// Validate checks switch structure and digest identity.
func (s SignedKillSwitch) Validate() error {
	if err := validateKillSwitchRequest(s.Request); err != nil {
		return err
	}
	if s.Digest == "" || s.Signature.Algorithm == "" {
		return ErrKillSwitchInvalid
	}
	return nil
}

// Verify checks switch structure, digest, and detached signature.
func (s SignedKillSwitch) Verify(publicKey ed25519.PublicKey) error {
	if err := validateSignatureIdentity(s.Signature); err != nil {
		return err
	}
	digest, err := s.DigestValue()
	if err != nil {
		return err
	}
	if digest != s.Digest {
		return ErrKillSwitchReceipt
	}
	raw, err := digestBytes(s.Digest)
	if err != nil {
		return err
	}
	signature, err := decodeSignature(s.Signature.Value)
	if err != nil || !ed25519.Verify(publicKey, raw, signature) {
		return ErrKillSwitchReceipt
	}
	return nil
}

// Explain returns audit-safe switch metadata.
func (s SignedKillSwitch) Explain() string {
	return fmt.Sprintf("kill switch id=%s priority=%d tenant=%s cell=%s digest=%s", s.Request.SwitchID, s.Request.Priority, s.Request.Target.TenantID, s.Request.Target.CellID, s.Digest)
}

// DigestValue computes the applied-receipt digest without trusting its
// signature.
func (r AppliedKillSwitchReceipt) DigestValue() (string, error) {
	t := r.Target
	w := canonicalbytes.New("hcmnext.platform.configbundle.AppliedKillSwitchReceipt", 1).
		String("switch_id", r.SwitchID).String("switch_digest", r.SwitchDigest).
		String("tenant_id", t.TenantID).String("cell_id", t.CellID).
		String("service", t.Service).String("capability", t.Capability).
		String("model", t.Model).String("connector", t.Connector).
		String("applied_at", r.AppliedAt.UTC().Format(time.RFC3339Nano)).
		Bool("within_slo", r.WithinSLO).String("status", r.Status)
	return w.Digest()
}

// Verify checks an applied receipt's digest and signature.
func (r AppliedKillSwitchReceipt) Verify(publicKey ed25519.PublicKey) error {
	if err := validateSignatureIdentity(r.Signature); err != nil {
		return err
	}
	digest, err := r.DigestValue()
	if err != nil {
		return err
	}
	if digest != r.Digest {
		return ErrKillSwitchReceipt
	}
	raw, err := digestBytes(r.Digest)
	if err != nil {
		return err
	}
	signature, err := decodeSignature(r.Signature.Value)
	if err != nil || !ed25519.Verify(publicKey, raw, signature) {
		return ErrKillSwitchReceipt
	}
	return nil
}

// Explain returns applied propagation facts without sensitive evidence.
func (r AppliedKillSwitchReceipt) Explain() string {
	return fmt.Sprintf("kill switch receipt id=%s status=%s within_slo=%t digest=%s", r.SwitchID, r.Status, r.WithinSLO, r.Digest)
}

func validateKillSwitchRequest(r KillSwitchRequest) error {
	if strings.TrimSpace(r.SwitchID) == "" || strings.TrimSpace(r.SwitchID) != r.SwitchID || strings.TrimSpace(r.Reason) == "" || strings.TrimSpace(r.Reason) != r.Reason || strings.TrimSpace(r.IncidentRef) == "" || strings.TrimSpace(r.Operator) == "" || strings.TrimSpace(r.Approver) == "" || r.Operator == r.Approver || strings.TrimSpace(r.EvidenceRef) == "" || r.IssuedAt.IsZero() || r.ExpiresAt.IsZero() || !r.ExpiresAt.After(r.IssuedAt) || r.PropagationSLO <= 0 || r.Target.Specificity() == 0 {
		return ErrKillSwitchInvalid
	}
	for _, value := range []string{r.IncidentRef, r.Operator, r.Approver, r.EvidenceRef, r.Target.TenantID, r.Target.CellID, r.Target.Service, r.Target.Capability, r.Target.Model, r.Target.Connector} {
		if value != "" && (strings.TrimSpace(value) != value || value == "*") {
			return ErrKillSwitchInvalid
		}
	}
	return nil
}

func cloneKillSwitchRequest(r KillSwitchRequest) KillSwitchRequest               { return r }
func cloneSignedKillSwitch(s SignedKillSwitch) SignedKillSwitch                  { return s }
func cloneKillSwitchReceipt(r AppliedKillSwitchReceipt) AppliedKillSwitchReceipt { return r }
