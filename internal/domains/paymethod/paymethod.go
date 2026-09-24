// Package paymethod owns pure payment-destination, verification, split, and
// dual-control semantics. It stores only governed references and display
// hints; provider calls and durable storage are outside this package.
package paymethod

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/settlement"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

func Version() int { return schemaVersion }

var (
	ErrInvalidDestination       = errors.New("paymethod: invalid payment destination")
	ErrInvalidChallenge         = errors.New("paymethod: invalid verification challenge")
	ErrInvalidVerification      = errors.New("paymethod: invalid verification event")
	ErrInvalidSplit             = errors.New("paymethod: invalid split priority")
	ErrDistinctApproverRequired = errors.New("paymethod: destination change requires a distinct approver")
	ErrDestinationNotVerified   = errors.New("paymethod: destination is not verified")
	ErrChallengeExpired         = errors.New("paymethod: verification challenge is expired")
	ErrAttemptBudgetExceeded    = errors.New("paymethod: verification attempt budget exceeded")
	ErrRawBankDetailProhibited  = errors.New("paymethod: raw bank detail is prohibited")
)

// Rail reuses settlement's governed rail vocabulary; the alias prevents two
// domain packages from silently disagreeing about the wire token.
type Rail = settlement.SettlementRail

const (
	RailACH      = settlement.RailACH
	RailWire     = settlement.RailWire
	RailSEPA     = settlement.RailSEPA
	RailRTP      = settlement.RailRTP
	RailFedNow   = settlement.RailFedNow
	RailInternal = settlement.RailInternal
	RailCheck    = settlement.RailCheck
	RailPayCard  = settlement.RailPayCard
	ACH          = RailACH
	Wire         = RailWire
	SEPA         = RailSEPA
	RTP          = RailRTP
	FedNow       = RailFedNow
	Internal     = RailInternal
	Check        = RailCheck
	PayCard      = RailPayCard
)

// ElectionMethod records an employee's affirmative choice of payment
// instrument. Consent evidence is a governed reference, never a checkbox
// inferred from a missing direct-deposit destination.
type ElectionMethod = settlement.ElectionMethod

const (
	MethodDirectDeposit = settlement.MethodDirectDeposit
	MethodPaperCheck    = settlement.MethodPaperCheck
	MethodPayCard       = settlement.MethodPayCard
)

var ErrInvalidElection = settlement.ErrInvalidElection

type PaymentMethodElection = settlement.PaymentMethodElection
type SettlementPolicy = settlement.SettlementPolicy
type PaymentMethodResolution = settlement.PaymentMethodResolution

// ResolvePaymentMethod preserves the paymethod domain API while sharing the
// validation implementation with the settlement release boundary.
func ResolvePaymentMethod(e PaymentMethodElection, policy SettlementPolicy) (PaymentMethodResolution, error) {
	return settlement.ResolvePaymentMethod(e, policy)
}

// RiskClass is a closed classification for destination handling risk.
type RiskClass string

const (
	RiskLow        RiskClass = "LOW"
	RiskMedium     RiskClass = "MEDIUM"
	RiskHigh       RiskClass = "HIGH"
	RiskRestricted RiskClass = "RESTRICTED"
	Low                      = RiskLow
	Medium                   = RiskMedium
	High                     = RiskHigh
	Restricted               = RiskRestricted
)

func (r RiskClass) Valid() bool {
	switch r {
	case RiskLow, RiskMedium, RiskHigh, RiskRestricted:
		return true
	default:
		return false
	}
}

// DestinationState is the lifecycle of a destination revision.
type DestinationState string

const (
	DestinationActive     DestinationState = "ACTIVE"
	DestinationRetired    DestinationState = "RETIRED"
	DestinationSuperseded DestinationState = "SUPERSEDED"
	Active                                 = DestinationActive
	Retired                                = DestinationRetired
	Superseded                             = DestinationSuperseded
)

func (s DestinationState) Valid() bool {
	return s == DestinationActive || s == DestinationRetired || s == DestinationSuperseded
}

// VerificationState is deliberately distinct from provider settlement state.
type VerificationState string

const (
	VerificationUnverified VerificationState = "UNVERIFIED"
	VerificationPending    VerificationState = "PENDING"
	VerificationVerified   VerificationState = "VERIFIED"
	VerificationExpired    VerificationState = "EXPIRED"
	VerificationFailed     VerificationState = "FAILED"
	Unverified                               = VerificationUnverified
	Pending                                  = VerificationPending
	Verified                                 = VerificationVerified
	Expired                                  = VerificationExpired
	Failed                                   = VerificationFailed
)

func (s VerificationState) Valid() bool {
	switch s {
	case VerificationUnverified, VerificationPending, VerificationVerified, VerificationExpired, VerificationFailed:
		return true
	default:
		return false
	}
}

// Destination is an immutable, effective-dated destination revision. The
// governed reference is opaque: account and routing numbers are not fields in
// the canonical record and the raw compatibility probes are always rejected.
type Destination struct {
	ID                 string
	DestinationID      string
	WorkerRef          string
	Rail               Rail
	Risk               RiskClass
	RiskClass          RiskClass
	GovernedRef        string
	ProviderRef        string
	BankDetailRef      string
	TokenRef           string
	DisplayHint        string
	Currency           string
	CountryCode        string
	Verification       VerificationState
	VerificationState  VerificationState
	VerificationDigest string
	Effective          values.EffectiveInterval
	State              DestinationState
	Status             DestinationState
	Revision           uint64
	SupersedesRevision uint64
	SupersedesDigest   string
	CanonicalDigest    string
	AccountNumber      string
	RoutingNumber      string
	RawAccountNumber   string
	RawBankDetail      string
}

type PaymentDestination = Destination

func (d Destination) id() string {
	if d.DestinationID != "" {
		return d.DestinationID
	}
	return d.ID
}

func (d Destination) risk() RiskClass {
	if d.Risk != "" {
		return d.Risk
	}
	return d.RiskClass
}

func (d Destination) governedRef() string {
	for _, ref := range []string{d.GovernedRef, d.ProviderRef, d.BankDetailRef, d.TokenRef} {
		if strings.TrimSpace(ref) != "" {
			return ref
		}
	}
	return ""
}

func (d Destination) verification() VerificationState {
	if d.Verification != "" {
		return d.Verification
	}
	return d.VerificationState
}

func (d Destination) state() DestinationState {
	if d.State != "" {
		return d.State
	}
	return d.Status
}

func (d Destination) Validate() error {
	if strings.TrimSpace(d.id()) == "" {
		return fieldError(ErrInvalidDestination, "id", errors.New("destination id is required"))
	}
	if strings.TrimSpace(d.WorkerRef) == "" {
		return fieldError(ErrInvalidDestination, "worker_ref", errors.New("worker reference is required"))
	}
	if !d.Rail.Valid() {
		return fieldError(ErrInvalidDestination, "rail", errors.New("rail is not declared"))
	}
	if !d.risk().Valid() {
		return fieldError(ErrInvalidDestination, "risk", errors.New("risk is not declared"))
	}
	ref := d.governedRef()
	if strings.TrimSpace(ref) == "" || strings.TrimSpace(ref) != ref {
		return fieldError(ErrInvalidDestination, "governed_ref", errors.New("governed reference is required and may not be padded"))
	}
	if d.GovernedRef != "" && d.ProviderRef != "" && d.GovernedRef != d.ProviderRef {
		return fieldError(ErrInvalidDestination, "governed_ref", errors.New("reference aliases disagree"))
	}
	if strings.TrimSpace(d.DisplayHint) == "" {
		return fieldError(ErrInvalidDestination, "display_hint", errors.New("masked display hint is required"))
	}
	if d.AccountNumber != "" || d.RoutingNumber != "" || d.RawAccountNumber != "" || d.RawBankDetail != "" {
		return fmt.Errorf("%w: %w", ErrInvalidDestination, ErrRawBankDetailProhibited)
	}
	if strings.TrimSpace(d.Currency) == "" {
		return fieldError(ErrInvalidDestination, "currency", errors.New("currency is required"))
	}
	if strings.TrimSpace(d.CountryCode) == "" {
		return fieldError(ErrInvalidDestination, "country_code", errors.New("country code is required"))
	}
	verification := d.verification()
	if !verification.Valid() {
		return fieldError(ErrInvalidDestination, "verification", errors.New("verification state is not declared"))
	}
	if verification == VerificationVerified && strings.TrimSpace(d.VerificationDigest) == "" {
		return fieldError(ErrInvalidDestination, "verification_digest", errors.New("verified destination requires verification event digest"))
	}
	if err := d.Effective.Validate(); err != nil {
		return fieldError(ErrInvalidDestination, "effective", err)
	}
	if !d.state().Valid() {
		return fieldError(ErrInvalidDestination, "state", errors.New("destination state is not declared"))
	}
	if d.Revision == 0 {
		return fieldError(ErrInvalidDestination, "revision", errors.New("positive revision is required"))
	}
	if d.Revision == 1 && (d.SupersedesRevision != 0 || d.SupersedesDigest != "") {
		return fieldError(ErrInvalidDestination, "supersedes", errors.New("first revision cannot have a predecessor"))
	}
	if d.Revision > 1 && (d.SupersedesRevision == 0 || strings.TrimSpace(d.SupersedesDigest) == "") {
		return fieldError(ErrInvalidDestination, "supersedes", errors.New("successor requires predecessor lineage"))
	}
	if d.CanonicalDigest != "" && d.CanonicalDigest != d.computedDigest() {
		return fieldError(ErrInvalidDestination, "canonical_digest", errors.New("digest mismatch"))
	}
	return nil
}

func (d Destination) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.paymethod.Destination", schemaVersion).
		String("id", d.id()).String("worker_ref", d.WorkerRef).String("rail", string(d.Rail)).
		String("risk", string(d.risk())).String("governed_ref", d.governedRef()).
		String("display_hint", d.DisplayHint).String("currency", d.Currency).String("country_code", d.CountryCode).
		String("verification", string(d.verification())).String("verification_digest", d.VerificationDigest).
		Value("effective", d.Effective).String("state", string(d.state())).Int("revision", int64(d.Revision)).
		Int("supersedes_revision", int64(d.SupersedesRevision)).String("supersedes_digest", d.SupersedesDigest)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (d Destination) computedDigest() string { return canonicalbytes.Digest(d.body()) }

// NewDestination validates and digests a destination. Empty lifecycle and
// verification fields receive safe initial values for constructor callers.
func NewDestination(d Destination) (Destination, error) {
	if d.ID == "" {
		d.ID = d.DestinationID
	}
	if d.DestinationID == "" {
		d.DestinationID = d.ID
	}
	if d.Risk == "" {
		d.Risk = d.RiskClass
	}
	if d.RiskClass == "" {
		d.RiskClass = d.Risk
	}
	if d.GovernedRef == "" {
		d.GovernedRef = d.ProviderRef
	}
	if d.Verification == "" {
		d.Verification = d.VerificationState
	}
	if d.VerificationState == "" {
		d.VerificationState = d.Verification
	}
	if d.State == "" {
		d.State = d.Status
	}
	if d.Status == "" {
		d.Status = d.State
	}
	if d.State == "" {
		d.State, d.Status = DestinationActive, DestinationActive
	}
	if d.Verification == "" {
		d.Verification, d.VerificationState = VerificationUnverified, VerificationUnverified
	}
	if d.Revision == 0 {
		d.Revision = 1
	}
	d.CanonicalDigest = ""
	if err := d.Validate(); err != nil {
		return Destination{}, err
	}
	d.CanonicalDigest = d.computedDigest()
	return d, nil
}

// NewRevision makes an immutable successor. It is useful for provider
// rotation and verification state changes; the receiver is never mutated.
func (d Destination) NewRevision(next Destination) (Destination, error) {
	if err := d.Validate(); err != nil {
		return Destination{}, err
	}
	next.ID, next.DestinationID = d.ID, d.DestinationID
	next.WorkerRef = d.WorkerRef
	next.Revision = d.Revision + 1
	next.SupersedesRevision, next.SupersedesDigest = d.Revision, d.CanonicalDigest
	next.CanonicalDigest = ""
	return NewDestination(next)
}

// VerificationMethod is the closed challenge mechanism vocabulary.
type VerificationMethod string

const (
	MethodMicroDeposit        VerificationMethod = "MICRO_DEPOSIT"
	MethodInstantVerification VerificationMethod = "INSTANT_VERIFICATION"
	MicroDeposit                                 = MethodMicroDeposit
	InstantVerification                          = MethodInstantVerification
)

func (m VerificationMethod) Valid() bool {
	return m == MethodMicroDeposit || m == MethodInstantVerification
}

// VerificationChallenge is an immutable challenge envelope. It carries no
// secret answer; provider evidence is represented only by a digest.
type VerificationChallenge struct {
	ID              string
	ChallengeID     string
	DestinationID   string
	Method          VerificationMethod
	IssuedAt        values.Instant
	ExpiresAt       values.Instant
	TTLSeconds      int64
	AttemptBudget   int
	AttemptsUsed    int
	State           VerificationState
	CanonicalDigest string
}

func (c VerificationChallenge) id() string {
	if c.ChallengeID != "" {
		return c.ChallengeID
	}
	return c.ID
}

func (c VerificationChallenge) Validate() error {
	if strings.TrimSpace(c.id()) == "" {
		return fieldError(ErrInvalidChallenge, "id", errors.New("challenge id is required"))
	}
	if strings.TrimSpace(c.DestinationID) == "" {
		return fieldError(ErrInvalidChallenge, "destination_id", errors.New("destination id is required"))
	}
	if !c.Method.Valid() {
		return fieldError(ErrInvalidChallenge, "method", errors.New("verification method is not declared"))
	}
	if err := c.IssuedAt.Validate(); err != nil {
		return fieldError(ErrInvalidChallenge, "issued_at", err)
	}
	if err := c.ExpiresAt.Validate(); err != nil {
		return fieldError(ErrInvalidChallenge, "expires_at", err)
	}
	if !c.IssuedAt.Before(c.ExpiresAt) {
		return fieldError(ErrInvalidChallenge, "ttl", errors.New("expires_at must be after issued_at"))
	}
	if c.TTLSeconds <= 0 {
		return fieldError(ErrInvalidChallenge, "ttl_seconds", errors.New("positive TTL is required"))
	}
	if c.AttemptBudget <= 0 || c.AttemptsUsed < 0 || c.AttemptsUsed > c.AttemptBudget {
		return fieldError(ErrInvalidChallenge, "attempt_budget", errors.New("attempts must fit the positive budget"))
	}
	if !c.State.Valid() || c.State == VerificationVerified {
		return fieldError(ErrInvalidChallenge, "state", errors.New("challenge state must be pending or failed/expired"))
	}
	if c.CanonicalDigest != "" && c.CanonicalDigest != c.computedDigest() {
		return fieldError(ErrInvalidChallenge, "canonical_digest", errors.New("digest mismatch"))
	}
	return nil
}

func (c VerificationChallenge) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.paymethod.VerificationChallenge", schemaVersion).
		String("id", c.id()).String("destination_id", c.DestinationID).String("method", string(c.Method)).
		Value("issued_at", c.IssuedAt).Value("expires_at", c.ExpiresAt).Int("ttl_seconds", c.TTLSeconds).
		Int("attempt_budget", int64(c.AttemptBudget)).Int("attempts_used", int64(c.AttemptsUsed)).String("state", string(c.State))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (c VerificationChallenge) computedDigest() string { return canonicalbytes.Digest(c.body()) }

// NewVerificationChallenge validates and digests a pending challenge.
func NewVerificationChallenge(destination Destination, id string, method VerificationMethod, issuedAt, expiresAt values.Instant, attemptBudget int) (VerificationChallenge, error) {
	if err := destination.Validate(); err != nil {
		return VerificationChallenge{}, err
	}
	if strings.TrimSpace(id) == "" {
		return VerificationChallenge{}, fieldError(ErrInvalidChallenge, "id", errors.New("challenge id is required"))
	}
	seconds := expiresAt.Time().Unix() - issuedAt.Time().Unix()
	c := VerificationChallenge{ID: id, ChallengeID: id, DestinationID: destination.id(), Method: method, IssuedAt: issuedAt, ExpiresAt: expiresAt, TTLSeconds: seconds, AttemptBudget: attemptBudget, State: VerificationPending}
	if err := c.Validate(); err != nil {
		return VerificationChallenge{}, err
	}
	c.CanonicalDigest = c.computedDigest()
	return c, nil
}

// ChallengeSpec is the structured form for integrations that prefer one
// request value.
type ChallengeSpec struct {
	ID            string
	Method        VerificationMethod
	IssuedAt      values.Instant
	ExpiresAt     values.Instant
	TTLSeconds    int64
	AttemptBudget int
}

func NewChallenge(destination Destination, spec ChallengeSpec) (VerificationChallenge, error) {
	if spec.TTLSeconds == 0 && spec.IssuedAt.Validate() == nil && spec.ExpiresAt.Validate() == nil {
		spec.TTLSeconds = spec.ExpiresAt.Time().Unix() - spec.IssuedAt.Time().Unix()
	}
	c, err := NewVerificationChallenge(destination, spec.ID, spec.Method, spec.IssuedAt, spec.ExpiresAt, spec.AttemptBudget)
	if err != nil {
		return VerificationChallenge{}, err
	}
	if spec.TTLSeconds != c.TTLSeconds {
		return VerificationChallenge{}, fieldError(ErrInvalidChallenge, "ttl_seconds", errors.New("TTL does not match challenge window"))
	}
	return c, nil
}

// VerificationEvent records successful verification as a digest-only event.
type VerificationEvent struct {
	ChallengeID     string
	DestinationID   string
	Method          VerificationMethod
	Attempt         int
	VerifiedAt      values.Instant
	EvidenceDigest  string
	CanonicalDigest string
}

type VerificationChallengeEvent = VerificationEvent

func (e VerificationEvent) Validate() error {
	if strings.TrimSpace(e.ChallengeID) == "" || strings.TrimSpace(e.DestinationID) == "" {
		return fieldError(ErrInvalidVerification, "references", errors.New("challenge and destination references are required"))
	}
	if !e.Method.Valid() || e.Attempt <= 0 {
		return fieldError(ErrInvalidVerification, "method_or_attempt", errors.New("method and positive attempt are required"))
	}
	if err := e.VerifiedAt.Validate(); err != nil {
		return fieldError(ErrInvalidVerification, "verified_at", err)
	}
	if strings.TrimSpace(e.EvidenceDigest) == "" {
		return fieldError(ErrInvalidVerification, "evidence_digest", errors.New("evidence must be a digest"))
	}
	if e.CanonicalDigest != "" && e.CanonicalDigest != e.computedDigest() {
		return fieldError(ErrInvalidVerification, "canonical_digest", errors.New("digest mismatch"))
	}
	return nil
}

func (e VerificationEvent) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.paymethod.VerificationEvent", schemaVersion).
		String("challenge_id", e.ChallengeID).String("destination_id", e.DestinationID).
		String("method", string(e.Method)).Int("attempt", int64(e.Attempt)).Value("verified_at", e.VerifiedAt).
		String("evidence_digest", e.EvidenceDigest)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (e VerificationEvent) computedDigest() string { return canonicalbytes.Digest(e.body()) }

// Verify accepts a digest-only provider result at a declared instant and
// returns an immutable verified event. It never mutates the challenge.
func (c VerificationChallenge) Verify(at values.Instant, evidenceDigest string) (VerificationEvent, error) {
	if err := c.Validate(); err != nil {
		return VerificationEvent{}, err
	}
	if err := at.Validate(); err != nil {
		return VerificationEvent{}, fieldError(ErrInvalidVerification, "verified_at", err)
	}
	if at.Before(c.IssuedAt) || !at.Before(c.ExpiresAt) {
		return VerificationEvent{}, ErrChallengeExpired
	}
	if c.AttemptsUsed >= c.AttemptBudget {
		return VerificationEvent{}, ErrAttemptBudgetExceeded
	}
	e := VerificationEvent{ChallengeID: c.id(), DestinationID: c.DestinationID, Method: c.Method, Attempt: c.AttemptsUsed + 1, VerifiedAt: at, EvidenceDigest: evidenceDigest}
	if err := e.Validate(); err != nil {
		return VerificationEvent{}, err
	}
	e.CanonicalDigest = e.computedDigest()
	return e, nil
}

// RecordAttempt returns an immutable challenge successor with one consumed
// attempt. It is useful when a provider reports a failed answer; the answer
// itself is never retained.
func (c VerificationChallenge) RecordAttempt() (VerificationChallenge, error) {
	if err := c.Validate(); err != nil {
		return VerificationChallenge{}, err
	}
	if c.AttemptsUsed >= c.AttemptBudget {
		return VerificationChallenge{}, ErrAttemptBudgetExceeded
	}
	next := c
	next.AttemptsUsed++
	if next.AttemptsUsed == next.AttemptBudget {
		next.State = VerificationFailed
	}
	next.CanonicalDigest = ""
	if err := next.Validate(); err != nil {
		return VerificationChallenge{}, err
	}
	next.CanonicalDigest = next.computedDigest()
	return next, nil
}

// ApplyVerification returns a new destination revision bound to the digest of
// the verification event.
func ApplyVerification(destination Destination, event VerificationEvent) (Destination, error) {
	if err := destination.Validate(); err != nil {
		return Destination{}, err
	}
	if err := event.Validate(); err != nil {
		return Destination{}, err
	}
	if event.DestinationID != destination.id() {
		return Destination{}, fmt.Errorf("%w: event destination differs", ErrInvalidVerification)
	}
	next := destination
	next.Verification, next.VerificationState = VerificationVerified, VerificationVerified
	next.VerificationDigest = event.CanonicalDigest
	return destination.NewRevision(next)
}

// SplitKind is the closed fixed/percentage allocation vocabulary.
type SplitKind string

const (
	SplitFixedAmount SplitKind = "FIXED_AMOUNT"
	SplitPercentage  SplitKind = "PERCENTAGE"
	FixedAmount                = SplitFixedAmount
	Percentage                 = SplitPercentage
)

func (k SplitKind) Valid() bool { return k == SplitFixedAmount || k == SplitPercentage }

// SplitPriority is one ordered allocation rule. Value is an exact decimal.
type SplitPriority struct {
	Priority       int
	DestinationRef string
	Kind           SplitKind
	Value          values.Decimal
	Remainder      bool
}

type Split = SplitPriority
type SplitRule = SplitPriority

func (s SplitPriority) Validate() error {
	if s.Priority <= 0 {
		return fieldError(ErrInvalidSplit, "priority", errors.New("priority must be positive"))
	}
	if strings.TrimSpace(s.DestinationRef) == "" {
		return fieldError(ErrInvalidSplit, "destination_ref", errors.New("destination reference is required"))
	}
	if !s.Kind.Valid() {
		return fieldError(ErrInvalidSplit, "kind", errors.New("split kind is not declared"))
	}
	if s.Remainder && s.Value.Validate() != nil {
		return nil
	}
	if err := s.Value.Validate(); err != nil {
		return fieldError(ErrInvalidSplit, "value", err)
	}
	if s.Value.Sign() <= 0 {
		return fieldError(ErrInvalidSplit, "value", errors.New("split value must be positive"))
	}
	return nil
}

func (s SplitPriority) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.paymethod.SplitPriority", schemaVersion).
		Int("priority", int64(s.Priority)).String("destination_ref", s.DestinationRef).
		String("kind", string(s.Kind)).Bool("value_present", s.Value.Validate() == nil).Bool("remainder", s.Remainder)
	if s.Value.Validate() == nil {
		w.Value("value", s.Value)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// SplitPlan is a deterministic ordered split set. A remainder destination is
// required whenever the explicit rules do not consume the full total.
type SplitPlan struct {
	Currency                string
	TotalAmount             values.Decimal
	Splits                  []SplitPriority
	RemainderDestinationRef string
	CanonicalDigest         string
}

func exactAdd(a, b values.Decimal) (values.Decimal, error) {
	if err := a.Validate(); err != nil {
		return values.Decimal{}, err
	}
	if err := b.Validate(); err != nil {
		return values.Decimal{}, err
	}
	scale := a.Scale()
	if b.Scale() > scale {
		scale = b.Scale()
	}
	left, err := a.Quantize(scale, values.RoundingExactRequired)
	if err != nil {
		return values.Decimal{}, err
	}
	right, err := b.Quantize(scale, values.RoundingExactRequired)
	if err != nil {
		return values.Decimal{}, err
	}
	return left.Add(right)
}

func (p SplitPlan) Validate() error {
	if strings.TrimSpace(p.Currency) == "" {
		return fieldError(ErrInvalidSplit, "currency", errors.New("currency is required"))
	}
	if len(p.Splits) == 0 {
		return fieldError(ErrInvalidSplit, "splits", errors.New("at least one split is required"))
	}
	if p.TotalAmount.Validate() == nil && p.TotalAmount.Sign() <= 0 {
		return fieldError(ErrInvalidSplit, "total_amount", errors.New("total amount must be positive"))
	}
	copyOf := append([]SplitPriority(nil), p.Splits...)
	sort.Slice(copyOf, func(i, j int) bool { return copyOf[i].Priority < copyOf[j].Priority })
	var percent values.Decimal
	var fixed values.Decimal
	percentSet, fixedSet := false, false
	seen := make(map[string]struct{}, len(copyOf))
	remainderCount := 0
	for i, split := range copyOf {
		if err := split.Validate(); err != nil {
			return err
		}
		if split.Priority != i+1 {
			return fieldError(ErrInvalidSplit, "priority", errors.New("priorities must be contiguous and ordered"))
		}
		if _, ok := seen[split.DestinationRef]; ok {
			return fieldError(ErrInvalidSplit, "destination_ref", errors.New("destination cannot appear twice"))
		}
		seen[split.DestinationRef] = struct{}{}
		if split.Remainder {
			remainderCount++
		}
		if split.Remainder && split.Value.Validate() != nil {
			continue
		}
		switch split.Kind {
		case SplitPercentage:
			if !percentSet {
				percent = split.Value
				percentSet = true
			} else {
				var err error
				percent, err = exactAdd(percent, split.Value)
				if err != nil {
					return fieldError(ErrInvalidSplit, "percentage", err)
				}
			}
		case SplitFixedAmount:
			if !fixedSet {
				fixed = split.Value
				fixedSet = true
			} else {
				var err error
				fixed, err = exactAdd(fixed, split.Value)
				if err != nil {
					return fieldError(ErrInvalidSplit, "fixed_amount", err)
				}
			}
		}
	}
	if remainderCount > 1 {
		return fieldError(ErrInvalidSplit, "remainder", errors.New("only one remainder split is permitted"))
	}
	if remainderCount == 1 && strings.TrimSpace(p.RemainderDestinationRef) != "" {
		for _, split := range copyOf {
			if split.Remainder && split.DestinationRef != p.RemainderDestinationRef {
				return fieldError(ErrInvalidSplit, "remainder_destination_ref", errors.New("remainder marker and destination disagree"))
			}
		}
	}
	if strings.TrimSpace(p.RemainderDestinationRef) != "" {
		if _, exists := seen[p.RemainderDestinationRef]; exists && remainderCount == 0 {
			return fieldError(ErrInvalidSplit, "remainder_destination_ref", errors.New("remainder destination cannot also be an explicit split"))
		}
	}
	hasRemainder := remainderCount > 0 || strings.TrimSpace(p.RemainderDestinationRef) != ""
	if fixedSet && p.TotalAmount.Validate() != nil {
		return fieldError(ErrInvalidSplit, "total_amount", errors.New("fixed-amount splits require a total amount"))
	}
	if percentSet {
		oneHundred, err := values.NewDecimal("100", percent.Scale(), values.RoundingExactRequired)
		if err != nil {
			return err
		}
		if percent.Cmp(oneHundred) > 0 || !hasRemainder && percent.Cmp(oneHundred) != 0 {
			return fieldError(ErrInvalidSplit, "percentage", errors.New("percentages must sum to 100 or leave a declared remainder"))
		}
	}
	if fixedSet && p.TotalAmount.Validate() == nil && fixed.Cmp(p.TotalAmount) > 0 {
		return fieldError(ErrInvalidSplit, "fixed_amount", errors.New("fixed splits exceed total amount"))
	}
	if fixedSet && !percentSet && p.TotalAmount.Validate() == nil && !hasRemainder && !fixed.Equal(p.TotalAmount) {
		return fieldError(ErrInvalidSplit, "fixed_amount", errors.New("fixed splits must sum to the total amount"))
	}
	if fixedSet && percentSet && p.TotalAmount.Validate() == nil {
		hundred, err := values.NewDecimal("100", percent.Scale(), values.RoundingExactRequired)
		if err != nil {
			return err
		}
		portion, err := p.TotalAmount.Mul(percent, p.TotalAmount.Scale(), values.RoundingExactRequired)
		if err != nil {
			return fieldError(ErrInvalidSplit, "percentage", err)
		}
		portion, err = portion.Div(hundred, p.TotalAmount.Scale(), values.RoundingExactRequired)
		if err != nil {
			return fieldError(ErrInvalidSplit, "percentage", err)
		}
		spent, err := exactAdd(fixed, portion)
		if err != nil {
			return fieldError(ErrInvalidSplit, "total_amount", err)
		}
		if !hasRemainder && !spent.Equal(p.TotalAmount) || hasRemainder && spent.Cmp(p.TotalAmount) > 0 {
			return fieldError(ErrInvalidSplit, "splits", errors.New("fixed and percentage splits do not sum within the total"))
		}
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fieldError(ErrInvalidSplit, "canonical_digest", errors.New("digest mismatch"))
	}
	return nil
}

func (p SplitPlan) body() []byte {
	splits := append([]SplitPriority(nil), p.Splits...)
	sort.Slice(splits, func(i, j int) bool { return splits[i].Priority < splits[j].Priority })
	w := canonicalbytes.New("hcmnext.domains.paymethod.SplitPlan", schemaVersion).
		String("currency", p.Currency).Bool("total_amount_present", p.TotalAmount.Validate() == nil).
		String("remainder_destination_ref", p.RemainderDestinationRef).Count("splits", len(splits))
	if p.TotalAmount.Validate() == nil {
		w.Value("total_amount", p.TotalAmount)
	}
	for _, split := range splits {
		w.Value("split", split)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p SplitPlan) computedDigest() string { return canonicalbytes.Digest(p.body()) }

func NewSplitPlan(plan SplitPlan) (SplitPlan, error) {
	plan.CanonicalDigest = ""
	if err := plan.Validate(); err != nil {
		return SplitPlan{}, err
	}
	plan.CanonicalDigest = plan.computedDigest()
	return plan, nil
}

func (p SplitPlan) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.computedDigest(), nil
}

// DestinationChange is the append-only proposal/approval fact for replacing
// a destination. RequestedBy and Approver must be distinct principals.
type DestinationChange struct {
	ID              string
	DestinationID   string
	WorkerRef       string
	PreviousDigest  string
	ProposedDigest  string
	RequestedBy     string
	Approver        string
	Effective       values.EffectiveInterval
	CanonicalDigest string
}

type PaymentDestinationChange = DestinationChange

func (c DestinationChange) Validate() error {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.DestinationID) == "" || strings.TrimSpace(c.WorkerRef) == "" {
		return fieldError(ErrInvalidDestination, "references", errors.New("change references are required"))
	}
	if strings.TrimSpace(c.PreviousDigest) == "" || strings.TrimSpace(c.ProposedDigest) == "" || c.PreviousDigest == c.ProposedDigest {
		return fieldError(ErrInvalidDestination, "digests", errors.New("distinct previous and proposed digests are required"))
	}
	if strings.TrimSpace(c.RequestedBy) == "" {
		return fieldError(ErrInvalidDestination, "requested_by", errors.New("requester is required"))
	}
	if strings.TrimSpace(c.Approver) == "" || c.Approver == c.RequestedBy {
		return ErrDistinctApproverRequired
	}
	if err := c.Effective.Validate(); err != nil {
		return fieldError(ErrInvalidDestination, "effective", err)
	}
	if c.CanonicalDigest != "" && c.CanonicalDigest != c.computedDigest() {
		return fieldError(ErrInvalidDestination, "canonical_digest", errors.New("digest mismatch"))
	}
	return nil
}

func (c DestinationChange) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.paymethod.DestinationChange", schemaVersion).
		String("id", c.ID).String("destination_id", c.DestinationID).String("worker_ref", c.WorkerRef).
		String("previous_digest", c.PreviousDigest).String("proposed_digest", c.ProposedDigest).
		String("requested_by", c.RequestedBy).String("approver", c.Approver).Value("effective", c.Effective)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (c DestinationChange) computedDigest() string { return canonicalbytes.Digest(c.body()) }

func NewDestinationChange(current, proposed Destination, id, requestedBy, approver string, effective values.EffectiveInterval) (DestinationChange, error) {
	if err := current.Validate(); err != nil {
		return DestinationChange{}, err
	}
	if err := proposed.Validate(); err != nil {
		return DestinationChange{}, err
	}
	c := DestinationChange{ID: id, DestinationID: current.id(), WorkerRef: current.WorkerRef, PreviousDigest: current.CanonicalDigest, ProposedDigest: proposed.CanonicalDigest, RequestedBy: requestedBy, Approver: approver, Effective: effective}
	if err := c.Validate(); err != nil {
		return DestinationChange{}, err
	}
	c.CanonicalDigest = c.computedDigest()
	return c, nil
}

// RequireDistinctApprover is the small reusable dual-control rule.
func RequireDistinctApprover(requestedBy, approver string) error {
	if strings.TrimSpace(requestedBy) == "" || strings.TrimSpace(approver) == "" || requestedBy == approver {
		return ErrDistinctApproverRequired
	}
	return nil
}

// DestinationExplanation is safe to log: it includes no governed or account
// data, only identifiers, classifications and digests.
type DestinationExplanation struct {
	ID           string
	WorkerRef    string
	Rail         Rail
	Risk         RiskClass
	Verification VerificationState
	State        DestinationState
	Digest       string
}

func (d Destination) Explain() (DestinationExplanation, error) {
	if err := d.Validate(); err != nil {
		return DestinationExplanation{}, err
	}
	return DestinationExplanation{ID: d.id(), WorkerRef: d.WorkerRef, Rail: d.Rail, Risk: d.risk(), Verification: d.verification(), State: d.state(), Digest: d.CanonicalDigest}, nil
}

func Explain(d Destination) (DestinationExplanation, error) { return d.Explain() }

// DestinationCatalog is an in-memory port for domain tests and adapters.
type DestinationCatalog interface {
	Put(Destination) error
	Get(id string) (Destination, error)
	RecordChange(DestinationChange) error
}

type InMemoryCatalog struct {
	mu           sync.RWMutex
	destinations map[string]Destination
	changes      []DestinationChange
}

func NewInMemoryCatalog(destinations []Destination) (*InMemoryCatalog, error) {
	c := &InMemoryCatalog{destinations: make(map[string]Destination)}
	for _, destination := range destinations {
		if err := c.Put(destination); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *InMemoryCatalog) Put(destination Destination) error {
	if err := destination.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.destinations == nil {
		c.destinations = make(map[string]Destination)
	}
	c.destinations[destination.id()] = destination
	return nil
}

func (c *InMemoryCatalog) Get(id string) (Destination, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	destination, ok := c.destinations[id]
	if !ok {
		return Destination{}, fmt.Errorf("%w: destination %s not found", ErrInvalidDestination, id)
	}
	return destination, nil
}

func (c *InMemoryCatalog) RecordChange(change DestinationChange) error {
	if err := change.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	destination, ok := c.destinations[change.DestinationID]
	if !ok || destination.CanonicalDigest != change.PreviousDigest {
		return fmt.Errorf("%w: change is not based on the current destination", ErrInvalidDestination)
	}
	c.changes = append(c.changes, change)
	destination.CanonicalDigest = change.ProposedDigest
	c.destinations[change.DestinationID] = destination
	return nil
}

type ValidationError struct {
	Field string
	Cause error
}

func (e *ValidationError) Error() string { return fmt.Sprintf("%s: %v", e.Field, e.Cause) }
func (e *ValidationError) Unwrap() error { return e.Cause }

func fieldError(root error, field string, cause error) error {
	return fmt.Errorf("%w: %w", root, &ValidationError{Field: field, Cause: cause})
}
