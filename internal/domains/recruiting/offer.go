// Immutable offer revisions: RECRUIT-003 owns the offer truth and the
// explicit accepted-offer to Hire intent boundary.
//
// One OfferLedger holds immutable OfferRevisions over referenced
// candidacies: a command validates all of its inputs first, then appends
// exactly one revision plus one event to both the event log and the
// outbox. Acceptance additionally emits exactly one deterministic Hire
// child intent. Worker creation is separately governed: this ledger has
// no worker store and no CreateWorker path, and every HireIntent carries
// WorkerCreated=false.
//
// Terms are mutable only while DRAFT. Approval binds the proposal digest;
// signature must apply to the exact current canonical digest; acceptance
// is a compare-and-swap over the current revision, the approval and
// signature digests, the candidate/candidacy binding and the injected
// acceptance time against expiry. A stale revision, a post-approval term
// change, a foreign signature, an expired or rescinded offer, a stale
// candidacy and any provider acceptance are refused with a typed
// OfferError (matchable with errors.Is(err, ErrOfferRefused)) and append
// nothing.
//
// The clock is injected by the caller, so the ledger is pure: no
// database, no wall clock, no network.
package recruiting

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrOfferRefused is the sentinel for refused offer commands. Match it
// with errors.Is rather than parsing the code.
var ErrOfferRefused = errors.New("recruiting: offer command refused")

// Offer refusal codes. Every refused offer command returns exactly one
// of these.
const (
	CodeOfferConflict       = "OFFER_CONFLICT"
	CodeApprovalMismatch    = "APPROVAL_MISMATCH"
	CodeSignatureMismatch   = "SIGNATURE_MISMATCH"
	CodeOfferExpired        = "OFFER_EXPIRED"
	CodeOfferRescinded      = "OFFER_RESCINDED"
	CodeCandidacyNotCurrent = "CANDIDACY_NOT_CURRENT"
	CodeOfferUnauthorized   = "OFFER_UNAUTHORIZED"
	CodeProviderAcceptance  = "PROVIDER_ACCEPTANCE_REFUSED"
)

// offerCodes are the refusals owned by this file.
func offerCodes(code string) bool {
	switch code {
	case CodeOfferConflict, CodeApprovalMismatch, CodeSignatureMismatch,
		CodeOfferExpired, CodeOfferRescinded, CodeCandidacyNotCurrent,
		CodeOfferUnauthorized, CodeProviderAcceptance:
		return true
	default:
		return false
	}
}

// OfferError is the typed offer refusal. Field names the offending
// input and State the offending condition; compensation figures and
// candidate content are never echoed.
type OfferError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *OfferError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// Is reports ErrOfferRefused for the offer refusals owned by this file.
func (e *OfferError) Is(target error) bool {
	return target == ErrOfferRefused && e != nil && offerCodes(e.Code)
}

// AsOfferError unwraps a typed offer refusal.
func AsOfferError(err error) (*OfferError, bool) {
	if err == nil {
		return nil, false
	}
	var refused *OfferError
	if errors.As(err, &refused) && refused.Code != "" {
		return refused, true
	}
	return nil, false
}

// offerCodeOf reports the refusal code of err, or "" when err is not a
// typed offer refusal.
func offerCodeOf(err error) string {
	if refused, ok := AsOfferError(err); ok {
		return refused.Code
	}
	return ""
}

func offerConflict(field, state string) *OfferError {
	return &OfferError{Code: CodeOfferConflict, Field: field, State: state, Version: schemaVersion}
}

func approvalMismatch(field, state string) *OfferError {
	return &OfferError{Code: CodeApprovalMismatch, Field: field, State: state, Version: schemaVersion}
}

func signatureMismatch(field, state string) *OfferError {
	return &OfferError{Code: CodeSignatureMismatch, Field: field, State: state, Version: schemaVersion}
}

func offerExpired(field, state string) *OfferError {
	return &OfferError{Code: CodeOfferExpired, Field: field, State: state, Version: schemaVersion}
}

func offerRescinded(field, state string) *OfferError {
	return &OfferError{Code: CodeOfferRescinded, Field: field, State: state, Version: schemaVersion}
}

func candidacyNotCurrent(field, state string) *OfferError {
	return &OfferError{Code: CodeCandidacyNotCurrent, Field: field, State: state, Version: schemaVersion}
}

func offerUnauthorized(field, state string) *OfferError {
	return &OfferError{Code: CodeOfferUnauthorized, Field: field, State: state, Version: schemaVersion}
}

func providerAcceptance(field, state string) *OfferError {
	return &OfferError{Code: CodeProviderAcceptance, Field: field, State: state, Version: schemaVersion}
}

func offerMissing(field, state string) *OfferError {
	return &OfferError{Code: CodeOfferConflict, Field: field, State: state, Version: schemaVersion}
}

// isProviderRef reports whether ref cites an external provider rather
// than governance authority. Provider results are observations only and
// can never approve, sign or accept an offer.
func isProviderRef(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), "provider:")
}

// isAuthorityRef reports whether ref cites governance authority.
func isAuthorityRef(ref string) bool {
	return strings.HasPrefix(strings.TrimSpace(ref), "authority:")
}

// OfferStatus is the offer lifecycle vocabulary.
type OfferStatus string

// The offer lifecycle: DRAFT approves, APPROVED signs, SIGNED accepts;
// RESCINDED ends an APPROVED or SIGNED offer. Terminal states have no
// exits.
const (
	OfferDraft     OfferStatus = "DRAFT"
	OfferApproved  OfferStatus = "APPROVED"
	OfferSigned    OfferStatus = "SIGNED"
	OfferAccepted  OfferStatus = "ACCEPTED"
	OfferRescinded OfferStatus = "RESCINDED"
)

func (s OfferStatus) valid() bool {
	switch s {
	case OfferDraft, OfferApproved, OfferSigned, OfferAccepted, OfferRescinded:
		return true
	default:
		return false
	}
}

// OfferTerms are the material terms bound by approval: position, salary,
// start date and offer expiry.
type OfferTerms struct {
	PositionRef string
	Salary      values.Decimal
	StartDate   values.Instant
	ExpiresAt   values.Instant
}

func (tm OfferTerms) validate() error {
	if !validID(tm.PositionRef) {
		return offerMissing("position", "missing-id")
	}
	if err := tm.Salary.Validate(); err != nil {
		return offerMissing("salary", "invalid")
	}
	if tm.Salary.Cmp(values.MustDecimal("0", 2, values.RoundingHalfUp)) <= 0 {
		return offerMissing("salary", "non-positive")
	}
	if tm.StartDate.Validate() != nil {
		return offerMissing("start-date", "invalid")
	}
	if tm.ExpiresAt.Validate() != nil {
		return offerMissing("expiry", "invalid")
	}
	return nil
}

// OfferRevision is one immutable offer revision. ProposalDigest binds
// the approved terms, ApprovalDigest binds the proposal plus the
// approval, and SignatureDigest must equal the canonical digest the
// signer actually signed.
type OfferRevision struct {
	OfferID           string
	Revision          uint64
	Status            OfferStatus
	CandidacyID       string
	CandidateID       string
	CandidacyRevision uint64
	PositionRef       string
	Salary            values.Decimal
	StartDate         values.Instant
	ExpiresAt         values.Instant
	ProposalDigest    string
	ApprovalDigest    string
	SignatureDigest   string
	EffectiveAt       values.Instant
	KnownAt           values.KnownAt
	CanonicalDigest   string
}

func (r OfferRevision) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.recruiting.OfferRevision", schemaVersion).
		String("offer_id", r.OfferID).Int("revision", int64(r.Revision)).
		String("status", string(r.Status)).
		String("candidacy_id", r.CandidacyID).String("candidate_id", r.CandidateID).
		Int("candidacy_revision", int64(r.CandidacyRevision)).
		String("position_ref", r.PositionRef).
		Value("salary", r.Salary).Value("start_date", r.StartDate).Value("expires_at", r.ExpiresAt).
		String("proposal_digest", r.ProposalDigest).String("approval_digest", r.ApprovalDigest).
		String("signature_digest", r.SignatureDigest).
		Value("effective_at", r.EffectiveAt).Value("known_at", r.KnownAt).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r OfferRevision) withDigest() OfferRevision {
	r.CanonicalDigest = canonicalbytes.Digest(r.body())
	return r
}

func proposalDigest(offerID, candidacyID, candidateID string, candidacyRev uint64, terms OfferTerms) string {
	b, err := canonicalbytes.New("hcmnext.domains.recruiting.OfferProposal", schemaVersion).
		String("offer_id", offerID).
		String("candidacy_id", candidacyID).String("candidate_id", candidateID).
		Int("candidacy_revision", int64(candidacyRev)).
		String("position_ref", terms.PositionRef).
		Value("salary", terms.Salary).Value("start_date", terms.StartDate).
		Value("expires_at", terms.ExpiresAt).Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

func approvalDigest(proposal, approver, authorityRef string) string {
	b, err := canonicalbytes.New("hcmnext.domains.recruiting.OfferApproval", schemaVersion).
		String("proposal_digest", proposal).String("approver", approver).
		String("authority_ref", authorityRef).Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// HireIntent is the single deterministic child intent emitted by one
// acceptance. WorkerCreated is always false: worker creation remains
// separately governed and never happens inside offer acceptance.
type HireIntent struct {
	IntentID      string
	OfferID       string
	OfferRevision uint64
	CandidacyID   string
	CandidateID   string
	PositionRef   string
	StartDate     values.Instant
	WorkerCreated bool
}

// CreateOfferCmd opens one DRAFT offer bound to an exact candidacy
// revision.
type CreateOfferCmd struct {
	OfferID           string
	CandidacyID       string
	CandidateID       string
	CandidacyRevision uint64
	Terms             OfferTerms
	EffectiveAt       values.Instant
	KnownAt           values.KnownAt
}

// AcceptOfferCmd accepts one SIGNED offer. AcceptedAt is the injected
// clock against which expiry is evaluated.
type AcceptOfferCmd struct {
	OfferID           string
	ExpectedRevision  uint64
	CandidacyID       string
	CandidacyRevision uint64
	CandidateID       string
	SignatureDigest   string
	AcceptedAt        values.Instant
	KnownAt           values.KnownAt
}

// OfferLedger is the governed offer boundary: current revisions, the
// append-only event log and outbox mirror, and the emitted Hire intents.
// The mutex serializes concurrent acceptances so one compare-and-swap
// wins and the losers report OFFER_CONFLICT with zero effect.
type OfferLedger struct {
	mu     sync.Mutex
	offers map[string]OfferRevision
	events []RecruitingEvent
	outbox []RecruitingEvent
	hires  []HireIntent
}

// NewOfferLedger opens an empty governed offer boundary.
func NewOfferLedger() *OfferLedger {
	return &OfferLedger{offers: make(map[string]OfferRevision)}
}

func (l *OfferLedger) append(event RecruitingEvent) {
	l.events = append(l.events, event)
	l.outbox = append(l.outbox, event)
}

// CreateOffer opens offer id as DRAFT at revision 1, binding the exact
// candidacy revision and computing the proposal digest.
func (l *OfferLedger) CreateOffer(cmd CreateOfferCmd) (OfferRevision, error) {
	if l == nil {
		return OfferRevision{}, offerMissing("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !validID(cmd.OfferID) {
		return OfferRevision{}, offerMissing("offer", "missing-id")
	}
	if _, exists := l.offers[cmd.OfferID]; exists {
		return OfferRevision{}, offerConflict("offer", "already-exists")
	}
	if !validID(cmd.CandidacyID) {
		return OfferRevision{}, offerMissing("candidacy", "missing-id")
	}
	if !validID(cmd.CandidateID) {
		return OfferRevision{}, offerMissing("candidate", "missing-id")
	}
	if cmd.CandidacyRevision == 0 {
		return OfferRevision{}, offerMissing("candidacy", "missing-revision")
	}
	if err := cmd.Terms.validate(); err != nil {
		return OfferRevision{}, err
	}
	if cmd.Terms.ExpiresAt.Compare(cmd.EffectiveAt) <= 0 {
		return OfferRevision{}, offerMissing("expiry", "not-after-effective")
	}
	if !validTimes(cmd.EffectiveAt, cmd.KnownAt) {
		return OfferRevision{}, offerMissing("effective-time", "invalid")
	}
	revision := OfferRevision{
		OfferID: cmd.OfferID, Revision: 1, Status: OfferDraft,
		CandidacyID: cmd.CandidacyID, CandidateID: cmd.CandidateID,
		CandidacyRevision: cmd.CandidacyRevision,
		PositionRef:       cmd.Terms.PositionRef, Salary: cmd.Terms.Salary,
		StartDate: cmd.Terms.StartDate, ExpiresAt: cmd.Terms.ExpiresAt,
		ProposalDigest: proposalDigest(cmd.OfferID, cmd.CandidacyID, cmd.CandidateID, cmd.CandidacyRevision, cmd.Terms),
		EffectiveAt:    cmd.EffectiveAt, KnownAt: cmd.KnownAt,
	}.withDigest()
	l.offers[cmd.OfferID] = revision
	l.append(RecruitingEvent{Kind: "OFFER_CREATED", AggregateID: cmd.OfferID, Revision: 1, EffectiveAt: cmd.EffectiveAt, KnownAt: cmd.KnownAt})
	return revision, nil
}

// UpdateOfferTerms replaces the terms of a DRAFT offer. Any attempt to
// change terms after approval is APPROVAL_MISMATCH: post-approval
// corrections create a successor offer instead.
func (l *OfferLedger) UpdateOfferTerms(offerID string, expectedRevision uint64, terms OfferTerms, effective values.Instant, known values.KnownAt) error {
	if l == nil {
		return offerMissing("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.offers[offerID]
	if !ok {
		return offerMissing("offer", "unknown")
	}
	if current.Revision != expectedRevision {
		return offerConflict("offer", "revision-mismatch")
	}
	if current.Status != OfferDraft {
		return approvalMismatch("terms", "change-after-"+string(current.Status))
	}
	if err := terms.validate(); err != nil {
		return err
	}
	if terms.ExpiresAt.Compare(effective) <= 0 {
		return offerMissing("expiry", "not-after-effective")
	}
	if !validTimes(effective, known) {
		return offerMissing("effective-time", "invalid")
	}
	current.Revision++
	current.PositionRef = terms.PositionRef
	current.Salary = terms.Salary
	current.StartDate = terms.StartDate
	current.ExpiresAt = terms.ExpiresAt
	current.ProposalDigest = proposalDigest(current.OfferID, current.CandidacyID, current.CandidateID, current.CandidacyRevision, terms)
	current.EffectiveAt = effective
	current.KnownAt = known
	l.offers[offerID] = current.withDigest()
	l.append(RecruitingEvent{Kind: "OFFER_TERMS_UPDATED", AggregateID: offerID, Revision: current.Revision, EffectiveAt: effective, KnownAt: known})
	return nil
}

// ApproveOffer approves a DRAFT offer, binding the proposal digest plus
// the authorized approver. Provider authority is never approval.
func (l *OfferLedger) ApproveOffer(offerID string, expectedRevision uint64, approver, authorityRef string, effective values.Instant, known values.KnownAt) error {
	if l == nil {
		return offerMissing("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.offers[offerID]
	if !ok {
		return offerMissing("offer", "unknown")
	}
	if !validID(approver) {
		return offerUnauthorized("approver", "missing-id")
	}
	if !isAuthorityRef(authorityRef) {
		return offerUnauthorized("authority", "not-governance-authority")
	}
	if current.Revision != expectedRevision {
		return offerConflict("offer", "revision-mismatch")
	}
	if current.Status != OfferDraft {
		return approvalMismatch("offer", "approve-from-"+string(current.Status))
	}
	if !validTimes(effective, known) {
		return offerMissing("effective-time", "invalid")
	}
	current.Revision++
	current.Status = OfferApproved
	current.ApprovalDigest = approvalDigest(current.ProposalDigest, approver, authorityRef)
	current.EffectiveAt = effective
	current.KnownAt = known
	l.offers[offerID] = current.withDigest()
	l.append(RecruitingEvent{Kind: "OFFER_APPROVED", AggregateID: offerID, Revision: current.Revision, EffectiveAt: effective, KnownAt: known})
	return nil
}

// SignOffer records the candidate signature over the exact current
// canonical digest. A signature over any other digest is
// SIGNATURE_MISMATCH. Provider signatures are refused unconditionally:
// a document provider can evidence a signature, never accept an offer.
func (l *OfferLedger) SignOffer(offerID string, expectedRevision uint64, signer, signatureRef, signatureDigest string, effective values.Instant, known values.KnownAt) error {
	if l == nil {
		return offerMissing("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.offers[offerID]
	if !ok {
		return offerMissing("offer", "unknown")
	}
	if !validID(signer) {
		return signatureMismatch("signer", "missing-id")
	}
	if isProviderRef(signatureRef) {
		return providerAcceptance("signature", "provider-cannot-sign")
	}
	if !validID(signatureRef) {
		return signatureMismatch("signature", "missing-ref")
	}
	if current.Revision != expectedRevision {
		return offerConflict("offer", "revision-mismatch")
	}
	if current.Status != OfferApproved {
		return signatureMismatch("offer", "sign-from-"+string(current.Status))
	}
	if signatureDigest == "" || signatureDigest != current.CanonicalDigest {
		return signatureMismatch("signature", "digest-mismatch")
	}
	if !validTimes(effective, known) {
		return offerMissing("effective-time", "invalid")
	}
	current.Revision++
	current.Status = OfferSigned
	current.SignatureDigest = signatureDigest
	current.EffectiveAt = effective
	current.KnownAt = known
	l.offers[offerID] = current.withDigest()
	l.append(RecruitingEvent{Kind: "OFFER_SIGNED", AggregateID: offerID, Revision: current.Revision, EffectiveAt: effective, KnownAt: known})
	return nil
}

// RescindOffer ends an APPROVED or SIGNED offer. Acceptance afterwards
// is OFFER_RESCINDED.
func (l *OfferLedger) RescindOffer(offerID string, expectedRevision uint64, reason string, effective values.Instant, known values.KnownAt) error {
	if l == nil {
		return offerMissing("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.offers[offerID]
	if !ok {
		return offerMissing("offer", "unknown")
	}
	if current.Revision != expectedRevision {
		return offerConflict("offer", "revision-mismatch")
	}
	if current.Status != OfferApproved && current.Status != OfferSigned {
		return offerConflict("offer", "rescind-from-"+string(current.Status))
	}
	if !validID(reason) {
		return offerMissing("reason", "missing")
	}
	if !validTimes(effective, known) {
		return offerMissing("effective-time", "invalid")
	}
	current.Revision++
	current.Status = OfferRescinded
	current.EffectiveAt = effective
	current.KnownAt = known
	l.offers[offerID] = current.withDigest()
	l.append(RecruitingEvent{Kind: "OFFER_RESCINDED", AggregateID: offerID, Revision: current.Revision, EffectiveAt: effective, KnownAt: known})
	return nil
}

// AcceptOffer accepts one SIGNED offer. The compare-and-swap binds the
// current revision, the stored approval/signature digests, the
// candidate/candidacy binding and expiry against the injected
// acceptance time. On success it appends exactly one OFFER_ACCEPTED
// event and emits exactly one deterministic Hire child intent; worker
// creation stays separately governed.
func (l *OfferLedger) AcceptOffer(cmd AcceptOfferCmd) (HireIntent, error) {
	if l == nil {
		return HireIntent{}, offerMissing("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.offers[cmd.OfferID]
	if !ok {
		return HireIntent{}, offerMissing("offer", "unknown")
	}
	if current.Revision != cmd.ExpectedRevision {
		return HireIntent{}, offerConflict("offer", "revision-mismatch")
	}
	if current.Status == OfferRescinded {
		return HireIntent{}, offerRescinded("offer", "rescinded")
	}
	if current.Status != OfferSigned {
		return HireIntent{}, signatureMismatch("offer", "accept-from-"+string(current.Status))
	}
	if cmd.AcceptedAt.Compare(current.ExpiresAt) >= 0 {
		return HireIntent{}, offerExpired("offer", "past-expiry")
	}
	if cmd.SignatureDigest == "" || cmd.SignatureDigest != current.SignatureDigest {
		return HireIntent{}, signatureMismatch("signature", "digest-mismatch")
	}
	if cmd.CandidacyID != current.CandidacyID || cmd.CandidacyRevision != current.CandidacyRevision {
		return HireIntent{}, candidacyNotCurrent("candidacy", "stale-or-foreign")
	}
	if cmd.CandidateID != current.CandidateID {
		return HireIntent{}, candidacyNotCurrent("candidate", "mismatch")
	}
	if cmd.AcceptedAt.Validate() != nil || cmd.KnownAt.Canonical() == nil {
		return HireIntent{}, offerMissing("accepted-time", "invalid")
	}
	acceptedRevision := current.Revision
	current.Revision++
	current.Status = OfferAccepted
	current.EffectiveAt = cmd.AcceptedAt
	current.KnownAt = cmd.KnownAt
	current = current.withDigest()
	l.offers[cmd.OfferID] = current
	l.append(RecruitingEvent{Kind: "OFFER_ACCEPTED", AggregateID: cmd.OfferID, Revision: current.Revision, EffectiveAt: cmd.AcceptedAt, KnownAt: cmd.KnownAt})
	hire := HireIntent{
		IntentID: "hire:" + current.CanonicalDigest,
		OfferID:  current.OfferID, OfferRevision: acceptedRevision,
		CandidacyID: current.CandidacyID, CandidateID: current.CandidateID,
		PositionRef: current.PositionRef, StartDate: current.StartDate,
		WorkerCreated: false,
	}
	l.hires = append(l.hires, hire)
	return hire, nil
}

// Offer returns a copy of the current revision of one offer.
func (l *OfferLedger) Offer(id string) (OfferRevision, bool) {
	if l == nil {
		return OfferRevision{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	revision, ok := l.offers[id]
	return revision, ok
}

// HireIntents returns the emitted Hire child intents, oldest first.
func (l *OfferLedger) HireIntents() []HireIntent {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]HireIntent(nil), l.hires...)
}

// EventKinds returns the ordered event-kind trace for audit and tests.
func (l *OfferLedger) EventKinds() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	kinds := make([]string, 0, len(l.events))
	for _, event := range l.events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

// SortedOfferIDs returns the sorted offer IDs for deterministic
// inspection.
func (l *OfferLedger) SortedOfferIDs() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ids := make([]string, 0, len(l.offers))
	for id := range l.offers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Explain renders the bounded human-readable account: entity and event
// counts only — never candidate, compensation or signature content.
func (l *OfferLedger) Explain() string {
	if l == nil {
		return "offers: missing ledger"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return fmt.Sprintf("offers offers=%d hires=%d events=%d outbox=%d worker_creation=separately-governed",
		len(l.offers), len(l.hires), len(l.events), len(l.outbox))
}
