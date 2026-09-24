// Package settlement owns payment-instruction identity and the pure,
// append-only settlement lifecycle. It never accepts raw bank details and has
// no database or provider side effects.
package settlement

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 2

// Version reports this package's stable contract version.
func Version() int { return schemaVersion }

// SettlementRail is a closed vocabulary. Provider-specific details remain
// outside this domain package.
type SettlementRail string

const (
	RailACH      SettlementRail = "ACH"
	RailWire     SettlementRail = "WIRE"
	RailSEPA     SettlementRail = "SEPA"
	RailRTP      SettlementRail = "RTP"
	RailFedNow   SettlementRail = "FEDNOW"
	RailInternal SettlementRail = "INTERNAL"
	RailCheck    SettlementRail = "CHECK"
	RailPayCard  SettlementRail = "PAYCARD"

	ACH      = RailACH
	Wire     = RailWire
	SEPA     = RailSEPA
	RTP      = RailRTP
	FedNow   = RailFedNow
	Internal = RailInternal
	Check    = RailCheck
	PayCard  = RailPayCard
)

type ElectionMethod string

const (
	MethodDirectDeposit ElectionMethod = "DIRECT_DEPOSIT"
	MethodPaperCheck    ElectionMethod = "PAPER_CHECK"
	MethodPayCard       ElectionMethod = "PAY_CARD"
)

var ErrInvalidElection = errors.New("settlement: invalid payment-method election")

// PaymentMethodElection is the consent record bound to each payroll payment.
// It carries opaque evidence and destination references only.
type PaymentMethodElection struct {
	ElectionID         string
	WorkerRef          string
	Method             ElectionMethod
	Consented          bool
	ConsentEvidenceRef string
	DestinationRef     string
	InstrumentRef      string
	Jurisdiction       string
	RulePackRef        string
	FeeDisclosureRef   string
}

// SettlementPolicy is a versioned jurisdiction rule snapshot supplied by the
// governed legal rule resolver. It makes no independent legal determination.
type SettlementPolicy struct {
	Jurisdiction                 string
	RulePackRef                  string
	DirectDepositRequired        bool
	PaperCheckAllowed            bool
	PayCardAllowed               bool
	PayCardFeeDisclosureRequired bool
}

type PaymentMethodResolution struct {
	ElectionDigest string
	PolicyDigest   string
	Rail           SettlementRail
	DestinationRef string
	InstrumentRef  string
}

func (p SettlementPolicy) Validate() error {
	if strings.TrimSpace(p.Jurisdiction) == "" || strings.TrimSpace(p.RulePackRef) == "" {
		return fmt.Errorf("%w: jurisdiction rule snapshot is incomplete", ErrInvalidElection)
	}
	return nil
}

func (p SettlementPolicy) Digest() string {
	w := canonicalbytes.New("hcmnext.domains.settlement.SettlementPolicy", schemaVersion).
		String("jurisdiction", p.Jurisdiction).String("rule_pack_ref", p.RulePackRef).
		Bool("direct_deposit_required", p.DirectDepositRequired).Bool("paper_check_allowed", p.PaperCheckAllowed).
		Bool("pay_card_allowed", p.PayCardAllowed).Bool("pay_card_fee_disclosure_required", p.PayCardFeeDisclosureRequired)
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

func (e PaymentMethodElection) Digest() string {
	w := canonicalbytes.New("hcmnext.domains.settlement.PaymentMethodElection", schemaVersion).
		String("election_id", e.ElectionID).String("worker_ref", e.WorkerRef).String("method", string(e.Method)).
		Bool("consented", e.Consented).String("consent_evidence_ref", e.ConsentEvidenceRef).
		String("destination_ref", e.DestinationRef).String("instrument_ref", e.InstrumentRef).
		String("jurisdiction", e.Jurisdiction).String("rule_pack_ref", e.RulePackRef).
		String("fee_disclosure_ref", e.FeeDisclosureRef)
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// ResolvePaymentMethod enforces explicit consent and the exact rule snapshot
// before selecting a settlement rail and opaque destination.
func ResolvePaymentMethod(e PaymentMethodElection, p SettlementPolicy) (PaymentMethodResolution, error) {
	if err := p.Validate(); err != nil {
		return PaymentMethodResolution{}, err
	}
	if strings.TrimSpace(e.ElectionID) == "" || strings.TrimSpace(e.WorkerRef) == "" || !e.Consented || strings.TrimSpace(e.ConsentEvidenceRef) == "" {
		return PaymentMethodResolution{}, fmt.Errorf("%w: worker consent evidence is required", ErrInvalidElection)
	}
	if e.Jurisdiction != p.Jurisdiction || e.RulePackRef != p.RulePackRef {
		return PaymentMethodResolution{}, fmt.Errorf("%w: election and jurisdiction rule snapshot differ", ErrInvalidElection)
	}
	r := PaymentMethodResolution{ElectionDigest: e.Digest(), PolicyDigest: p.Digest()}
	switch e.Method {
	case MethodDirectDeposit:
		if strings.TrimSpace(e.DestinationRef) == "" {
			return PaymentMethodResolution{}, fmt.Errorf("%w: governed deposit destination is required", ErrInvalidElection)
		}
		r.Rail, r.DestinationRef = RailACH, e.DestinationRef
	case MethodPaperCheck:
		if p.DirectDepositRequired || !p.PaperCheckAllowed {
			return PaymentMethodResolution{}, fmt.Errorf("%w: paper check is prohibited by resolved rule pack", ErrInvalidElection)
		}
		if strings.TrimSpace(e.InstrumentRef) == "" {
			return PaymentMethodResolution{}, fmt.Errorf("%w: check delivery reference is required", ErrInvalidElection)
		}
		r.Rail, r.InstrumentRef = RailCheck, e.InstrumentRef
	case MethodPayCard:
		if p.DirectDepositRequired || !p.PayCardAllowed {
			return PaymentMethodResolution{}, fmt.Errorf("%w: pay card is prohibited by resolved rule pack", ErrInvalidElection)
		}
		if strings.TrimSpace(e.InstrumentRef) == "" {
			return PaymentMethodResolution{}, fmt.Errorf("%w: governed pay-card reference is required", ErrInvalidElection)
		}
		if p.PayCardFeeDisclosureRequired && strings.TrimSpace(e.FeeDisclosureRef) == "" {
			return PaymentMethodResolution{}, fmt.Errorf("%w: required fee disclosure evidence is missing", ErrInvalidElection)
		}
		r.Rail, r.InstrumentRef = RailPayCard, e.InstrumentRef
	default:
		return PaymentMethodResolution{}, fmt.Errorf("%w: method %q is not declared", ErrInvalidElection, e.Method)
	}
	return r, nil
}

func (r SettlementRail) Valid() bool {
	switch r {
	case RailACH, RailWire, RailSEPA, RailRTP, RailFedNow, RailInternal, RailCheck, RailPayCard:
		return true
	default:
		return false
	}
}

// SettlementState is the immutable instruction lifecycle. Provider
// acceptance is ACKNOWLEDGED; it is not evidence of SETTLED funds.
type SettlementState string

const (
	StateInstructed   SettlementState = "INSTRUCTED"
	StateSubmitted    SettlementState = "SUBMITTED"
	StateAcknowledged SettlementState = "ACKNOWLEDGED"
	StateSettled      SettlementState = "SETTLED"
	StateReturned     SettlementState = "RETURNED"
	StateReversed     SettlementState = "REVERSED"

	Instructed   = StateInstructed
	Submitted    = StateSubmitted
	Acknowledged = StateAcknowledged
	Settled      = StateSettled
	Returned     = StateReturned
	Reversed     = StateReversed
)

func (s SettlementState) Valid() bool {
	switch s {
	case StateInstructed, StateSubmitted, StateAcknowledged, StateSettled, StateReturned, StateReversed:
		return true
	default:
		return false
	}
}

var (
	ErrInvalidPaymentInstruction = errors.New("settlement: invalid payment instruction")
	ErrSettlementRejected        = errors.New("SETTLE_001_REJECTED")
	ErrNoReleasedPayrollRun      = errors.New("settlement: payment requires a released payroll run")
	ErrSettlementTransition      = errors.New("settlement: lifecycle transition is not allowed")
	ErrIdempotencyConflict       = errors.New("settlement: natural-key idempotency conflict")
)

// PaymentInstructionSpec contains the governed input to an instruction. A
// bank detail is represented only by BankDetailRef; RawBankDetail is a
// deliberate rejection probe and is never retained in a valid instruction.
type PaymentInstructionSpec struct {
	InstructionID         string
	PayeeRef              string
	Amount                values.Decimal
	Currency              string
	FundingSourceRef      string
	Rail                  SettlementRail
	BankDetailRef         string
	ScheduleRef           string
	ValueDate             values.LocalDate
	RawBankDetail         string
	PaymentMethodElection PaymentMethodElection
	SettlementPolicy      SettlementPolicy
	InstrumentRef         string
}

// PaymentInstruction is one immutable revision of a payment instruction.
// Every lifecycle operation returns another revision and leaves the receiver
// untouched.
type PaymentInstruction struct {
	InstructionID               string
	PayrollRunRef               string
	PayrollRunID                string
	PayrollRunRevision          uint64
	PopulationBinding           payroll.PopulationBindingRef
	PayeeRef                    string
	Amount                      values.Decimal
	Currency                    string
	FundingSourceRef            string
	Rail                        SettlementRail
	BankDetailRef               string
	ScheduleRef                 string
	ValueDate                   values.LocalDate
	PaymentMethodElectionDigest string
	SettlementPolicyDigest      string
	PaymentMethodElection       PaymentMethodElection
	SettlementPolicy            SettlementPolicy
	InstrumentRef               string

	State              SettlementState
	Revision           uint64
	SupersedesRevision uint64
	EvidenceRef        string
	CanonicalDigest    string
}

// NewPaymentInstruction creates revision one only when run is released (or a
// later immutable run state that still carries release evidence).
func NewPaymentInstruction(run payroll.PayrollRun, spec PaymentInstructionSpec) (PaymentInstruction, error) {
	if err := run.Validate(); err != nil {
		return PaymentInstruction{}, err
	}
	if run.State != payroll.PayrollRunStateReleased && run.State != payroll.PayrollRunStateSettled && run.State != payroll.PayrollRunStateReversed {
		return PaymentInstruction{}, fmt.Errorf("%w: run state %s", ErrNoReleasedPayrollRun, run.State)
	}
	if strings.TrimSpace(run.CanonicalDigest) == "" {
		return PaymentInstruction{}, fmt.Errorf("%w: released run digest is required", ErrNoReleasedPayrollRun)
	}
	if err := spec.validate(); err != nil {
		return PaymentInstruction{}, err
	}
	resolution, err := ResolvePaymentMethod(spec.PaymentMethodElection, spec.SettlementPolicy)
	if err != nil {
		return PaymentInstruction{}, fmt.Errorf("%w: %v", ErrInvalidPaymentInstruction, err)
	}
	i := PaymentInstruction{
		InstructionID: spec.InstructionID, PayrollRunRef: run.CanonicalDigest,
		PayrollRunID: run.RunID, PayrollRunRevision: run.Revision, PopulationBinding: run.Population,
		PayeeRef: spec.PayeeRef, Amount: spec.Amount, Currency: spec.Currency,
		FundingSourceRef: spec.FundingSourceRef, Rail: spec.Rail, BankDetailRef: spec.BankDetailRef,
		ScheduleRef: spec.ScheduleRef, ValueDate: spec.ValueDate, State: StateInstructed, Revision: 1,
		PaymentMethodElectionDigest: resolution.ElectionDigest, SettlementPolicyDigest: resolution.PolicyDigest,
		PaymentMethodElection: spec.PaymentMethodElection, SettlementPolicy: spec.SettlementPolicy,
		InstrumentRef: spec.InstrumentRef,
	}
	i.CanonicalDigest = i.computedDigest()
	return i, nil
}

// NewInstruction is a concise alias for NewPaymentInstruction.
func NewInstruction(run payroll.PayrollRun, spec PaymentInstructionSpec) (PaymentInstruction, error) {
	return NewPaymentInstruction(run, spec)
}

func (s PaymentInstructionSpec) validate() error {
	if strings.TrimSpace(s.InstructionID) == "" || strings.TrimSpace(s.PayeeRef) == "" {
		return fmt.Errorf("%w: instruction and payee references are required", ErrInvalidPaymentInstruction)
	}
	if err := s.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: amount: %v", ErrInvalidPaymentInstruction, err)
	}
	if s.Amount.Sign() <= 0 {
		return fmt.Errorf("%w: amount must be positive", ErrInvalidPaymentInstruction)
	}
	if strings.TrimSpace(s.Currency) == "" {
		return fmt.Errorf("%w: currency is required", ErrInvalidPaymentInstruction)
	}
	if strings.TrimSpace(s.FundingSourceRef) == "" {
		return fmt.Errorf("%w: funding source reference is required", ErrInvalidPaymentInstruction)
	}
	if !s.Rail.Valid() {
		return fmt.Errorf("%w: settlement rail %q is not declared", ErrInvalidPaymentInstruction, s.Rail)
	}
	if strings.TrimSpace(s.BankDetailRef) == "" {
		if s.Rail != RailCheck && s.Rail != RailPayCard {
			return fmt.Errorf("%w: governed destination reference is required", ErrInvalidPaymentInstruction)
		}
	}
	if s.Rail == RailCheck || s.Rail == RailPayCard {
		if strings.TrimSpace(s.InstrumentRef) == "" {
			return fmt.Errorf("%w: check/card requires a governed instrument reference", ErrInvalidPaymentInstruction)
		}
	}
	resolution, err := ResolvePaymentMethod(s.PaymentMethodElection, s.SettlementPolicy)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPaymentInstruction, err)
	}
	if s.PaymentMethodElection.WorkerRef != s.PayeeRef {
		return fmt.Errorf("%w: election worker does not match payment payee", ErrInvalidPaymentInstruction)
	}
	if resolution.Rail != s.Rail {
		return fmt.Errorf("%w: rail does not match governed election", ErrInvalidPaymentInstruction)
	}
	if s.Rail == RailACH && s.BankDetailRef != resolution.DestinationRef {
		return fmt.Errorf("%w: deposit destination does not match governed election", ErrInvalidPaymentInstruction)
	}
	if (s.Rail == RailCheck || s.Rail == RailPayCard) && s.InstrumentRef != resolution.InstrumentRef {
		return fmt.Errorf("%w: instrument does not match governed election", ErrInvalidPaymentInstruction)
	}
	if s.RawBankDetail != "" {
		return fmt.Errorf("%w: raw bank detail is prohibited", ErrInvalidPaymentInstruction)
	}
	if s.ValueDate.IsSet() {
		if err := s.ValueDate.Validate(); err != nil {
			return fmt.Errorf("%w: value date: %v", ErrInvalidPaymentInstruction, err)
		}
	}
	return nil
}

func (i PaymentInstruction) Validate() error {
	if strings.TrimSpace(i.InstructionID) == "" || strings.TrimSpace(i.PayrollRunRef) == "" || strings.TrimSpace(i.PayrollRunID) == "" || i.PayrollRunRevision == 0 || strings.TrimSpace(i.PayeeRef) == "" {
		return fmt.Errorf("%w: instruction, released run and payee references are required", ErrInvalidPaymentInstruction)
	}
	if err := i.PopulationBinding.Validate(); err != nil {
		return fmt.Errorf("%w: population binding: %v", ErrInvalidPaymentInstruction, err)
	}
	if err := (PaymentInstructionSpec{InstructionID: i.InstructionID, PayeeRef: i.PayeeRef, Amount: i.Amount, Currency: i.Currency, FundingSourceRef: i.FundingSourceRef, Rail: i.Rail, BankDetailRef: i.BankDetailRef, ScheduleRef: i.ScheduleRef, ValueDate: i.ValueDate, PaymentMethodElection: i.PaymentMethodElection, SettlementPolicy: i.SettlementPolicy, InstrumentRef: i.InstrumentRef}).validate(); err != nil {
		return err
	}
	resolution, err := ResolvePaymentMethod(i.PaymentMethodElection, i.SettlementPolicy)
	if err != nil || resolution.ElectionDigest != i.PaymentMethodElectionDigest || resolution.PolicyDigest != i.SettlementPolicyDigest {
		return fmt.Errorf("%w: bound election or jurisdiction policy digest mismatch", ErrInvalidPaymentInstruction)
	}
	if !i.State.Valid() || i.Revision == 0 {
		return fmt.Errorf("%w: state and revision are required", ErrInvalidPaymentInstruction)
	}
	if i.SupersedesRevision >= i.Revision && i.SupersedesRevision != 0 {
		return fmt.Errorf("%w: successor revision must supersede an earlier revision", ErrInvalidPaymentInstruction)
	}
	if i.State != StateInstructed && strings.TrimSpace(i.EvidenceRef) == "" {
		return fmt.Errorf("%w: lifecycle evidence is required after instruction", ErrInvalidPaymentInstruction)
	}
	if i.CanonicalDigest == "" || i.CanonicalDigest != i.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidPaymentInstruction)
	}
	return nil
}

func (i PaymentInstruction) naturalKeyBytes() []byte {
	w := canonicalbytes.New("hcmnext.domains.settlement.PaymentInstructionNaturalKey", schemaVersion).
		String("payroll_run_ref", i.PayrollRunRef).String("payroll_run_id", i.PayrollRunID).
		Int("payroll_run_revision", int64(i.PayrollRunRevision)).Value("population_binding", i.PopulationBinding).
		String("payee_ref", i.PayeeRef).
		Value("amount", i.Amount).String("currency", i.Currency).String("funding_source_ref", i.FundingSourceRef).
		String("rail", string(i.Rail)).String("bank_detail_ref", i.BankDetailRef).String("schedule_ref", i.ScheduleRef).
		String("payment_method_election_digest", i.PaymentMethodElectionDigest).String("settlement_policy_digest", i.SettlementPolicyDigest).String("instrument_ref", i.InstrumentRef)
	if i.ValueDate.IsSet() {
		w.Value("value_date", i.ValueDate)
	} else {
		w.Bool("value_date_present", false)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// NaturalKey is the stable idempotency identity, independent of instruction
// ID, lifecycle state, provider evidence and revision.
func (i PaymentInstruction) NaturalKey() string { return canonicalbytes.Digest(i.naturalKeyBytes()) }

// IdempotencyKey is an explicit alias for NaturalKey.
func (i PaymentInstruction) IdempotencyKey() string { return i.NaturalKey() }

func (i PaymentInstruction) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.settlement.PaymentInstruction", schemaVersion).
		String("instruction_id", i.InstructionID).String("payroll_run_ref", i.PayrollRunRef).
		String("payroll_run_id", i.PayrollRunID).Int("payroll_run_revision", int64(i.PayrollRunRevision)).
		Value("population_binding", i.PopulationBinding).
		String("payee_ref", i.PayeeRef).Value("amount", i.Amount).String("currency", i.Currency).
		String("funding_source_ref", i.FundingSourceRef).String("rail", string(i.Rail)).
		String("bank_detail_ref", i.BankDetailRef).String("schedule_ref", i.ScheduleRef).
		String("payment_method_election_digest", i.PaymentMethodElectionDigest).String("settlement_policy_digest", i.SettlementPolicyDigest).String("instrument_ref", i.InstrumentRef).
		Bool("value_date_present", i.ValueDate.IsSet()).
		String("state", string(i.State)).Int("revision", int64(i.Revision)).
		Int("supersedes_revision", int64(i.SupersedesRevision)).String("evidence_ref", i.EvidenceRef)
	if i.ValueDate.IsSet() {
		w.Value("value_date", i.ValueDate)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (i PaymentInstruction) computedDigest() string { return canonicalbytes.Digest(i.body()) }

// FulfillmentAction describes the governed work required by a non-electronic
// rail. It is an instruction to an authorized check or card adapter; this
// package does not print checks or load funds itself.
type FulfillmentAction string

const (
	ActionSubmitPayment    FulfillmentAction = "SUBMIT_PAYMENT"
	ActionPrintCheck       FulfillmentAction = "PRINT_CHECK"
	ActionVoidReissueCheck FulfillmentAction = "VOID_AND_REISSUE_CHECK"
	ActionLoadPayCard      FulfillmentAction = "LOAD_PAYCARD"
)

func (i PaymentInstruction) FulfillmentAction() FulfillmentAction {
	switch i.Rail {
	case RailCheck:
		return ActionPrintCheck
	case RailPayCard:
		return ActionLoadPayCard
	default:
		return ActionSubmitPayment
	}
}

// FulfillmentRequirements returns the actions a handler must satisfy for the
// selected rail. Paper checks require a print path and a controlled void and
// reissue path; neither is sent through an electronic payment provider.
func (i PaymentInstruction) FulfillmentRequirements() []FulfillmentAction {
	switch i.Rail {
	case RailCheck:
		return []FulfillmentAction{ActionPrintCheck, ActionVoidReissueCheck}
	case RailPayCard:
		return []FulfillmentAction{ActionLoadPayCard}
	default:
		return []FulfillmentAction{ActionSubmitPayment}
	}
}

func (i PaymentInstruction) allowed(to SettlementState) bool {
	switch i.State {
	case StateInstructed:
		return to == StateSubmitted
	case StateSubmitted:
		return to == StateAcknowledged || to == StateReturned
	case StateAcknowledged:
		return to == StateSettled || to == StateReturned
	case StateSettled:
		return to == StateReturned || to == StateReversed
	case StateReturned:
		return to == StateReversed
	default:
		return false
	}
}

// Transition appends one lifecycle revision and requires evidence for every
// post-instruction state. The original instruction is never modified.
func (i PaymentInstruction) Transition(to SettlementState, evidence string) (PaymentInstruction, error) {
	if err := i.Validate(); err != nil {
		return PaymentInstruction{}, err
	}
	if !to.Valid() || !i.allowed(to) {
		return PaymentInstruction{}, fmt.Errorf("%w: %s -> %s", ErrSettlementTransition, i.State, to)
	}
	if strings.TrimSpace(evidence) == "" {
		return PaymentInstruction{}, fmt.Errorf("%w: evidence is required", ErrSettlementTransition)
	}
	next := i
	next.State, next.Revision, next.SupersedesRevision, next.EvidenceRef = to, i.Revision+1, i.Revision, evidence
	next.CanonicalDigest = next.computedDigest()
	return next, nil
}

func (i PaymentInstruction) Submit(evidence string) (PaymentInstruction, error) {
	return i.Transition(StateSubmitted, evidence)
}
func (i PaymentInstruction) Acknowledge(evidence string) (PaymentInstruction, error) {
	return i.Transition(StateAcknowledged, evidence)
}
func (i PaymentInstruction) Settle(evidence string) (PaymentInstruction, error) {
	return i.Transition(StateSettled, evidence)
}
func (i PaymentInstruction) Return(evidence string) (PaymentInstruction, error) {
	return i.Transition(StateReturned, evidence)
}
func (i PaymentInstruction) Reverse(evidence string) (PaymentInstruction, error) {
	return i.Transition(StateReversed, evidence)
}

// EnsureIdempotent returns the existing instruction for the same natural key;
// a different natural key is a conflict and must not be silently replaced.
func EnsureIdempotent(existing, candidate PaymentInstruction) (PaymentInstruction, error) {
	if err := existing.Validate(); err != nil {
		return PaymentInstruction{}, err
	}
	if err := candidate.Validate(); err != nil {
		return PaymentInstruction{}, err
	}
	if existing.NaturalKey() != candidate.NaturalKey() {
		return PaymentInstruction{}, ErrIdempotencyConflict
	}
	return existing, nil
}

// Explain renders an audit-safe lifecycle summary.
func (i PaymentInstruction) Explain() string {
	return fmt.Sprintf("payment instruction %s for released run %s: %s revision %d via %s, amount %s %s, natural key %s, digest %s", i.InstructionID, i.PayrollRunRef, i.State, i.Revision, i.Rail, i.Amount.String(), i.Currency, i.NaturalKey(), i.CanonicalDigest)
}

// Explain is the package-level contract spelling.
func Explain(i PaymentInstruction) string { return i.Explain() }
