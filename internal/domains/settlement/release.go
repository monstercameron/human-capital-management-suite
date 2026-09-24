package settlement

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/sod"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

const releaseSchemaVersion = 2

// PaymentReleaseOperation is the stable sensitive operation bound by the
// release authorization. A release is an approval of a fixed batch, not a
// permission to submit arbitrary payment instructions.
const PaymentReleaseOperation = stepup.ActionApprove

// PaymentReleaseCapability and PaymentReleasePurpose are the governance
// coordinates expected in the step-up obligation for a money-moving release.
const (
	PaymentReleaseCapability = "payment.release"
	PaymentReleasePurpose    = stepup.PurposeHCMOperations
)

var (
	ErrInvalidReleaseBatch   = errors.New("settlement: invalid payment release batch")
	ErrInvalidReleaseBinding = errors.New("settlement: invalid payment release binding")
	ErrReleaseRejected       = errors.New("SETTLE_003_REJECTED")
	ErrInstructionChanged    = errors.New("settlement: instruction set changed")
	ErrFundingChanged        = errors.New("settlement: funding changed")
	ErrPayeeChanged          = errors.New("settlement: payee changed")
	ErrLimitsChanged         = errors.New("settlement: release limits changed")
	ErrSelfApproval          = errors.New("settlement: self-approval is prohibited")
	ErrApprovalRequired      = errors.New("settlement: approved dual-control decision is required")
	ErrReleaseStepUpRequired = errors.New("settlement: sufficient release step-up is required")
)

// ReleaseLimits are the server-held money and currency limits applied to one
// release batch. They are included in both the batch and its authorization
// decision so a later change cannot silently widen the release.
type ReleaseLimits struct {
	MaxBatchAmount       values.Decimal
	MaxInstructionAmount values.Decimal
	Currency             string
}

// NewReleaseLimits creates validated release limits.
func NewReleaseLimits(maxBatchAmount, maxInstructionAmount values.Decimal, currency string) (ReleaseLimits, error) {
	l := ReleaseLimits{MaxBatchAmount: maxBatchAmount, MaxInstructionAmount: maxInstructionAmount, Currency: currency}
	if err := l.Validate(); err != nil {
		return ReleaseLimits{}, err
	}
	return l, nil
}

// Validate checks that every limit is an exact positive monetary value.
func (l ReleaseLimits) Validate() error {
	if err := l.MaxBatchAmount.Validate(); err != nil || l.MaxBatchAmount.Sign() <= 0 {
		return fmt.Errorf("%w: max batch amount must be positive and valid", ErrInvalidReleaseBatch)
	}
	if err := l.MaxInstructionAmount.Validate(); err != nil || l.MaxInstructionAmount.Sign() <= 0 {
		return fmt.Errorf("%w: max instruction amount must be positive and valid", ErrInvalidReleaseBatch)
	}
	if strings.TrimSpace(l.Currency) == "" || l.Currency != strings.ToUpper(l.Currency) {
		return fmt.Errorf("%w: release currency is required and uppercase", ErrInvalidReleaseBatch)
	}
	return nil
}

func (l ReleaseLimits) digest() string {
	w := canonicalbytes.New("hcmnext.domains.settlement.ReleaseLimits", releaseSchemaVersion).
		Value("max_batch_amount", l.MaxBatchAmount).
		Value("max_instruction_amount", l.MaxInstructionAmount).
		String("currency", l.Currency)
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Digest returns the protected identity of the release limits.
func (l ReleaseLimits) Digest() string { return l.digest() }

// PaymentReleaseBatch is an immutable, digest-backed set of payment
// instructions. It carries references and amounts only; payment destinations
// remain governed by the instruction and pay-method boundaries.
type PaymentReleaseBatch struct {
	BatchID            string
	TenantID           values.TenantId
	InstructionDigests []string
	InstructionAmounts []values.Decimal
	Instructions       []PaymentInstruction
	PayrollRun         payroll.PayrollRun
	Population         payroll.FrozenPopulation
	InstructionDigest  string
	FundingDigest      string
	PayeeDigest        string
	Total              values.Decimal
	Currency           string
	Limits             ReleaseLimits
	LimitsDigest       string
	CanonicalDigest    string
}

// NewPaymentReleaseBatch seals a batch from already validated payment
// instructions, a frozen affected-worker population, and funding evidence.
func NewPaymentReleaseBatch(batchID string, tenant values.TenantId, instructions []PaymentInstruction, run payroll.PayrollRun, fundingDigest string, population payroll.FrozenPopulation, limits ReleaseLimits) (PaymentReleaseBatch, error) {
	if strings.TrimSpace(batchID) == "" || batchID != strings.TrimSpace(batchID) {
		return PaymentReleaseBatch{}, fmt.Errorf("%w: batch id is required", ErrInvalidReleaseBatch)
	}
	if err := tenant.Validate(); err != nil {
		return PaymentReleaseBatch{}, fmt.Errorf("%w: tenant: %v", ErrInvalidReleaseBatch, err)
	}
	if len(instructions) == 0 {
		return PaymentReleaseBatch{}, fmt.Errorf("%w: at least one instruction is required", ErrInvalidReleaseBatch)
	}
	if strings.TrimSpace(fundingDigest) == "" {
		return PaymentReleaseBatch{}, fmt.Errorf("%w: funding digest is required", ErrInvalidReleaseBatch)
	}
	if err := population.Validate(); err != nil || population.State != payroll.PopulationStateFrozen {
		return PaymentReleaseBatch{}, fmt.Errorf("%w: authoritative frozen worker population is required", ErrInvalidReleaseBatch)
	}
	if err := limits.Validate(); err != nil {
		return PaymentReleaseBatch{}, err
	}
	if err := run.Validate(); err != nil || (run.State != payroll.PayrollRunStateReleased && run.State != payroll.PayrollRunStateSettled && run.State != payroll.PayrollRunStateReversed) {
		return PaymentReleaseBatch{}, fmt.Errorf("%w: released payroll run is required", ErrInvalidReleaseBatch)
	}
	if population.RunID != run.RunID || population.Binding != run.Population {
		return PaymentReleaseBatch{}, fmt.Errorf("%w: frozen worker roster does not match payroll run", ErrInvalidReleaseBatch)
	}

	type instructionEntry struct {
		digest      string
		amount      values.Decimal
		instruction PaymentInstruction
	}
	entries := make([]instructionEntry, 0, len(instructions))
	var total values.Decimal
	for n, instruction := range instructions {
		if err := instruction.Validate(); err != nil {
			return PaymentReleaseBatch{}, fmt.Errorf("%w: instruction %d: %v", ErrInvalidReleaseBatch, n, err)
		}
		if instruction.Currency != limits.Currency {
			return PaymentReleaseBatch{}, fmt.Errorf("%w: instruction %d currency differs from release currency", ErrInvalidReleaseBatch, n)
		}
		if instruction.PayrollRunRef != run.CanonicalDigest || instruction.PayrollRunID != run.RunID || instruction.PayrollRunRevision != run.Revision || instruction.PopulationBinding != run.Population {
			return PaymentReleaseBatch{}, fmt.Errorf("%w: instruction %d is not bound to the released payroll run", ErrInvalidReleaseBatch, n)
		}
		if n == 0 {
			total = instruction.Amount
		} else {
			var err error
			total, err = total.Add(instruction.Amount)
			if err != nil {
				return PaymentReleaseBatch{}, fmt.Errorf("%w: total: %v", ErrInvalidReleaseBatch, err)
			}
		}
		entries = append(entries, instructionEntry{digest: instruction.CanonicalDigest, amount: instruction.Amount, instruction: instruction})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].digest < entries[j].digest })
	digests := make([]string, 0, len(entries))
	amounts := make([]values.Decimal, 0, len(entries))
	orderedInstructions := make([]PaymentInstruction, 0, len(entries))
	for _, entry := range entries {
		digests = append(digests, entry.digest)
		amounts = append(amounts, entry.amount)
		orderedInstructions = append(orderedInstructions, entry.instruction)
	}
	b := PaymentReleaseBatch{
		BatchID: batchID, TenantID: tenant, InstructionDigests: digests,
		InstructionAmounts: amounts, Instructions: orderedInstructions, PayrollRun: run, Population: population,
		FundingDigest: fundingDigest, PayeeDigest: population.Digest,
		Total: total, Currency: limits.Currency, Limits: limits,
		LimitsDigest: limits.digest(),
	}
	b.InstructionDigest = instructionSetDigest(b.InstructionDigests, b.InstructionAmounts)
	b.CanonicalDigest = b.digest()
	if err := b.Validate(); err != nil {
		return PaymentReleaseBatch{}, err
	}
	return b, nil
}

// NewReleaseBatch is the concise constructor alias.
func NewReleaseBatch(batchID string, tenant values.TenantId, instructions []PaymentInstruction, run payroll.PayrollRun, fundingDigest string, population payroll.FrozenPopulation, limits ReleaseLimits) (PaymentReleaseBatch, error) {
	return NewPaymentReleaseBatch(batchID, tenant, instructions, run, fundingDigest, population, limits)
}

// Validate verifies the sealed batch and its limit binding.
func (b PaymentReleaseBatch) Validate() error {
	if strings.TrimSpace(b.BatchID) == "" || b.BatchID != strings.TrimSpace(b.BatchID) {
		return fmt.Errorf("%w: batch id is required", ErrInvalidReleaseBatch)
	}
	if err := b.TenantID.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidReleaseBatch, err)
	}
	if len(b.InstructionDigests) == 0 || len(b.InstructionDigests) != len(b.InstructionAmounts) {
		return fmt.Errorf("%w: instruction digests and amounts are incomplete", ErrInvalidReleaseBatch)
	}
	if err := b.Population.Validate(); err != nil || b.Population.State != payroll.PopulationStateFrozen {
		return fmt.Errorf("%w: authoritative frozen worker population is invalid", ErrInvalidReleaseBatch)
	}
	if err := b.PayrollRun.Validate(); err != nil || (b.PayrollRun.State != payroll.PayrollRunStateReleased && b.PayrollRun.State != payroll.PayrollRunStateSettled && b.PayrollRun.State != payroll.PayrollRunStateReversed) {
		return fmt.Errorf("%w: released payroll run is invalid", ErrInvalidReleaseBatch)
	}
	if b.Population.RunID != b.PayrollRun.RunID || b.Population.Binding != b.PayrollRun.Population {
		return fmt.Errorf("%w: worker roster is not bound to the released payroll run", ErrInvalidReleaseBatch)
	}
	if b.PayeeDigest != b.Population.Digest || len(b.Instructions) != len(b.InstructionDigests) || len(b.Instructions) != len(b.Population.Members) {
		return fmt.Errorf("%w: instruction set does not cover the affected-worker roster", ErrInvalidReleaseBatch)
	}
	workers := make(map[string]struct{}, len(b.Instructions))
	for n, instruction := range b.Instructions {
		if err := instruction.Validate(); err != nil {
			return fmt.Errorf("%w: instruction %d: %v", ErrInvalidReleaseBatch, n, err)
		}
		if instruction.CanonicalDigest != b.InstructionDigests[n] || !instruction.Amount.Equal(b.InstructionAmounts[n]) {
			return fmt.Errorf("%w: instruction %d differs from its sealed digest or amount", ErrInvalidReleaseBatch, n)
		}
		if instruction.PayrollRunRef != b.PayrollRun.CanonicalDigest || instruction.PayrollRunID != b.PayrollRun.RunID || instruction.PayrollRunRevision != b.PayrollRun.Revision || instruction.PopulationBinding != b.PayrollRun.Population {
			return fmt.Errorf("%w: instruction %d is bound to another payroll population", ErrInvalidReleaseBatch, n)
		}
		worker := instruction.PaymentMethodElection.WorkerRef
		if _, exists := workers[worker]; exists {
			return fmt.Errorf("%w: duplicate affected-worker instruction %q", ErrInvalidReleaseBatch, worker)
		}
		workers[worker] = struct{}{}
	}
	for _, member := range b.Population.MemberList() {
		worker := member.WorkerRef
		if worker == "" {
			worker = member.MemberRef
		}
		if _, exists := workers[worker]; !exists {
			return fmt.Errorf("%w: affected worker %q has no validated payment instruction", ErrInvalidReleaseBatch, worker)
		}
	}
	if strings.TrimSpace(b.FundingDigest) == "" || strings.TrimSpace(b.PayeeDigest) == "" {
		return fmt.Errorf("%w: funding and payee digests are required", ErrInvalidReleaseBatch)
	}
	if err := b.Limits.Validate(); err != nil {
		return err
	}
	if err := b.Total.Validate(); err != nil || b.Total.Sign() <= 0 || b.Currency != b.Limits.Currency {
		return fmt.Errorf("%w: total and currency are invalid", ErrInvalidReleaseBatch)
	}
	var total values.Decimal
	for _, amount := range b.InstructionAmounts {
		if err := amount.Validate(); err != nil || amount.Sign() <= 0 {
			return fmt.Errorf("%w: instruction amount is invalid", ErrInvalidReleaseBatch)
		}
		if total.IsZero() {
			total = amount
			continue
		}
		var err error
		total, err = total.Add(amount)
		if err != nil {
			return fmt.Errorf("%w: instruction total is invalid", ErrInvalidReleaseBatch)
		}
	}
	if !total.Equal(b.Total) {
		return fmt.Errorf("%w: instruction total does not match batch total", ErrInvalidReleaseBatch)
	}
	if b.InstructionDigest != instructionSetDigest(b.InstructionDigests, b.InstructionAmounts) {
		return fmt.Errorf("%w: instruction digest mismatch", ErrInvalidReleaseBatch)
	}
	if b.LimitsDigest != b.Limits.digest() || b.CanonicalDigest == "" || b.CanonicalDigest != b.digest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidReleaseBatch)
	}
	return nil
}

func (b PaymentReleaseBatch) digest() string {
	w := canonicalbytes.New("hcmnext.domains.settlement.PaymentReleaseBatch", releaseSchemaVersion).
		String("batch_id", b.BatchID).String("tenant_id", b.TenantID.String()).
		String("instruction_digest", b.InstructionDigest).String("funding_digest", b.FundingDigest).
		String("payroll_run_digest", b.PayrollRun.CanonicalDigest).
		String("payee_digest", b.PayeeDigest).String("population_digest", b.Population.Digest).Value("total", b.Total).
		String("currency", b.Currency).String("limits_digest", b.LimitsDigest)
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// PaymentReleaseBinding is the server-held snapshot an authorization must
// match. Each separate digest is checked so a refusal identifies whether the
// instruction, funding, payee or limit context changed.
type PaymentReleaseBinding struct {
	BatchDigest       string
	InstructionDigest string
	FundingDigest     string
	PayeeDigest       string
	LimitsDigest      string
}

// Binding returns the exact authorization snapshot for b.
func (b PaymentReleaseBatch) Binding() PaymentReleaseBinding {
	return PaymentReleaseBinding{BatchDigest: b.CanonicalDigest, InstructionDigest: b.InstructionDigest, FundingDigest: b.FundingDigest, PayeeDigest: b.PayeeDigest, LimitsDigest: b.LimitsDigest}
}

// ReleaseBinding is an alias kept for callers that use the domain term
// without the longer payment prefix.
type ReleaseBinding = PaymentReleaseBinding

// PaymentReleaseApproval is the proposal-bound approval supplied by the
// separate approver. It is evidence, not an execution command.
type PaymentReleaseApproval struct {
	DecisionID   string
	ProposalID   string
	RequesterID  string
	ApproverID   string
	BatchDigest  string
	LimitsDigest string
	Approved     bool
}

// PaymentReleaseAuthorizationRequest contains the complete decision-time
// control context. All fields are facts to be checked; none widens authority.
type PaymentReleaseAuthorizationRequest struct {
	Batch           PaymentReleaseBatch
	Binding         PaymentReleaseBinding
	ProposalID      string
	RequesterID     string
	ApproverID      string
	ExecutorID      string
	Approval        PaymentReleaseApproval
	Constraints     sod.Constraints
	DecisionContext sod.DecisionContext
	StepUp          stepup.Obligation
	AuthorizedAt    time.Time
}

// ReleaseAuthorizationRequest is a concise alias for the public request.
type ReleaseAuthorizationRequest = PaymentReleaseAuthorizationRequest

// PaymentReleaseDecision is the immutable authorization evidence. It binds
// the exact batch and limits and contains no raw destination or bank data.
type PaymentReleaseDecision struct {
	DecisionID          string
	ProposalID          string
	BatchID             string
	TenantID            values.TenantId
	BatchDigest         string
	InstructionDigest   string
	FundingDigest       string
	PayeeDigest         string
	Limits              ReleaseLimits
	LimitsDigest        string
	RequesterID         string
	ApproverID          string
	ApprovalDecisionID  string
	SODRuleID           string
	StepUpBindingDigest string
	AuthorizedAt        time.Time
	CanonicalDigest     string
}

// Validate verifies the immutable decision evidence.
func (d PaymentReleaseDecision) Validate() error {
	if strings.TrimSpace(d.DecisionID) == "" || strings.TrimSpace(d.ProposalID) == "" || strings.TrimSpace(d.BatchID) == "" {
		return fmt.Errorf("%w: decision identity is incomplete", ErrReleaseRejected)
	}
	if err := d.TenantID.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrReleaseRejected, err)
	}
	if err := d.Limits.Validate(); err != nil {
		return err
	}
	if d.BatchDigest == "" || d.InstructionDigest == "" || d.FundingDigest == "" || d.PayeeDigest == "" || d.LimitsDigest == "" || d.StepUpBindingDigest == "" || d.SODRuleID == "" {
		return fmt.Errorf("%w: decision evidence is incomplete", ErrReleaseRejected)
	}
	if d.LimitsDigest != d.Limits.digest() || d.AuthorizedAt.IsZero() || d.CanonicalDigest == "" || d.CanonicalDigest != d.digest() {
		return fmt.Errorf("%w: decision digest mismatch", ErrReleaseRejected)
	}
	return nil
}

// AuthorizePaymentRelease evaluates dual control, exact snapshot binding,
// and the money-moving step-up obligation. It is pure: it writes no rows,
// consumes no proof and submits no provider request.
func AuthorizePaymentRelease(req PaymentReleaseAuthorizationRequest) (PaymentReleaseDecision, error) {
	if err := req.Batch.Validate(); err != nil {
		return PaymentReleaseDecision{}, reject("batch", err)
	}
	if err := validateBinding(req.Binding); err != nil {
		return PaymentReleaseDecision{}, reject("binding", err)
	}
	actual := req.Batch.Binding()
	checks := []struct {
		field string
		got   string
		want  string
		cause error
	}{
		{"instruction_digest", req.Binding.InstructionDigest, actual.InstructionDigest, ErrInstructionChanged},
		{"funding_digest", req.Binding.FundingDigest, actual.FundingDigest, ErrFundingChanged},
		{"payee_digest", req.Binding.PayeeDigest, actual.PayeeDigest, ErrPayeeChanged},
		{"limits_digest", req.Binding.LimitsDigest, actual.LimitsDigest, ErrLimitsChanged},
		{"batch_digest", req.Binding.BatchDigest, actual.BatchDigest, ErrInvalidReleaseBinding},
	}
	for _, check := range checks {
		if check.got != check.want {
			return PaymentReleaseDecision{}, reject(check.field, check.cause)
		}
	}
	for n, amount := range req.Batch.InstructionAmounts {
		if amount.Cmp(req.Batch.Limits.MaxInstructionAmount) > 0 {
			return PaymentReleaseDecision{}, reject(fmt.Sprintf("instruction_amount[%d]", n), ErrLimitsChanged)
		}
	}
	if req.Batch.Total.Cmp(req.Batch.Limits.MaxBatchAmount) > 0 {
		return PaymentReleaseDecision{}, reject("batch_total", ErrLimitsChanged)
	}
	if strings.TrimSpace(req.ProposalID) == "" || strings.TrimSpace(req.RequesterID) == "" || strings.TrimSpace(req.ApproverID) == "" {
		return PaymentReleaseDecision{}, reject("authority", ErrApprovalRequired)
	}
	if req.RequesterID == req.ApproverID {
		return PaymentReleaseDecision{}, reject("approver", ErrSelfApproval)
	}
	if req.DecisionContext.Requester.Subject != req.RequesterID {
		return PaymentReleaseDecision{}, reject("requester", ErrApprovalRequired)
	}
	if !containsActor(req.DecisionContext.Approvers, req.ApproverID) {
		return PaymentReleaseDecision{}, reject("approver", ErrApprovalRequired)
	}
	if req.ExecutorID != "" {
		req.DecisionContext.Executor = sod.Actor{Subject: req.ExecutorID}
	}
	if !req.Constraints.RequesterMayNotApprove {
		return PaymentReleaseDecision{}, reject("sod", ErrSelfApproval)
	}
	sodResult, err := sod.Evaluate(req.DecisionContext, req.Constraints, 1)
	if err != nil || !contains(sodResult.Eligible, req.ApproverID) {
		if err == nil {
			err = ErrSelfApproval
		}
		return PaymentReleaseDecision{}, reject("sod", err)
	}
	if !req.Approval.Approved || req.Approval.DecisionID == "" || req.Approval.ProposalID != req.ProposalID || req.Approval.RequesterID != req.RequesterID || req.Approval.ApproverID != req.ApproverID || req.Approval.BatchDigest != actual.BatchDigest || req.Approval.LimitsDigest != actual.LimitsDigest {
		return PaymentReleaseDecision{}, reject("approval", ErrApprovalRequired)
	}
	if err := validateReleaseStepUp(req.StepUp, req); err != nil {
		return PaymentReleaseDecision{}, reject("step_up", err)
	}
	if req.AuthorizedAt.IsZero() {
		return PaymentReleaseDecision{}, reject("authorized_at", ErrReleaseRejected)
	}

	d := PaymentReleaseDecision{
		DecisionID: req.Approval.DecisionID, ProposalID: req.ProposalID, BatchID: req.Batch.BatchID,
		TenantID: req.Batch.TenantID, BatchDigest: actual.BatchDigest, InstructionDigest: actual.InstructionDigest,
		FundingDigest: actual.FundingDigest, PayeeDigest: actual.PayeeDigest, Limits: req.Batch.Limits,
		LimitsDigest: actual.LimitsDigest, RequesterID: req.RequesterID, ApproverID: req.ApproverID,
		ApprovalDecisionID: req.Approval.DecisionID, SODRuleID: req.Constraints.RuleID,
		StepUpBindingDigest: req.StepUp.BindingDigest, AuthorizedAt: req.AuthorizedAt.UTC(),
	}
	d.CanonicalDigest = d.digest()
	return d, nil
}

// AuthorizeRelease is an alias for adapters that name the action directly.
func AuthorizeRelease(req ReleaseAuthorizationRequest) (PaymentReleaseDecision, error) {
	return AuthorizePaymentRelease(req)
}

func validateBinding(b PaymentReleaseBinding) error {
	if b.BatchDigest == "" || b.InstructionDigest == "" || b.FundingDigest == "" || b.PayeeDigest == "" || b.LimitsDigest == "" {
		return ErrInvalidReleaseBinding
	}
	return nil
}

func validateReleaseStepUp(ob stepup.Obligation, req PaymentReleaseAuthorizationRequest) error {
	if !ob.Required || !ob.Satisfied || ob.Code != stepup.CodeStepUpSatisfied {
		return ErrReleaseStepUpRequired
	}
	if ob.Action != PaymentReleaseOperation || ob.Capability != PaymentReleaseCapability || ob.Purpose != PaymentReleasePurpose || ob.ProposalID != req.ProposalID || ob.Tenant != req.Batch.TenantID || ob.Subject != req.ApproverID || strings.TrimSpace(ob.SessionRef) == "" || strings.TrimSpace(ob.BindingDigest) == "" {
		return ErrReleaseStepUpRequired
	}
	return nil
}

func (d PaymentReleaseDecision) digest() string {
	w := canonicalbytes.New("hcmnext.domains.settlement.PaymentReleaseDecision", releaseSchemaVersion).
		String("decision_id", d.DecisionID).String("proposal_id", d.ProposalID).String("batch_id", d.BatchID).
		String("tenant_id", d.TenantID.String()).String("batch_digest", d.BatchDigest).
		String("instruction_digest", d.InstructionDigest).String("funding_digest", d.FundingDigest).
		String("payee_digest", d.PayeeDigest).String("limits_digest", d.LimitsDigest).
		String("requester_id", d.RequesterID).String("approver_id", d.ApproverID).
		String("approval_decision_id", d.ApprovalDecisionID).String("sod_rule_id", d.SODRuleID).
		String("step_up_binding_digest", d.StepUpBindingDigest).Int("authorized_at", d.AuthorizedAt.UnixNano())
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Explain is audit-safe and names only protected digests and policy state.
func (d PaymentReleaseDecision) Explain() string {
	return fmt.Sprintf("payment release decision proposal=%s batch=%s limits=%s requester=%s approver=%s sod=%s step_up_bound=%t",
		d.ProposalID, d.BatchDigest, d.LimitsDigest, d.RequesterID, d.ApproverID, d.SODRuleID, d.StepUpBindingDigest != "")
}

// ExplainRelease renders a decision without exposing payment destination data.
func ExplainRelease(d PaymentReleaseDecision) string { return d.Explain() }

func reject(field string, cause error) error {
	return fmt.Errorf("%w: %s: %w", ErrReleaseRejected, field, cause)
}

func containsActor(actors []sod.Actor, subject string) bool {
	for _, actor := range actors {
		if actor.Subject == subject {
			return true
		}
	}
	return false
}

func contains(subjects []string, want string) bool {
	for _, subject := range subjects {
		if subject == want {
			return true
		}
	}
	return false
}

func instructionSetDigest(digests []string, amounts []values.Decimal) string {
	if len(digests) != len(amounts) {
		return ""
	}
	w := canonicalbytes.New("hcmnext.domains.settlement.PaymentReleaseInstructions", releaseSchemaVersion)
	for n := range digests {
		w.String(fmt.Sprintf("digest_%d", n), digests[n]).Value(fmt.Sprintf("amount_%d", n), amounts[n])
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}
