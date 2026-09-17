package settlement

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrFundingRejected is the SETTLE-002 typed refusal: worker, instruction
	// and control totals do not reconcile exactly, or the funding source is
	// missing, insufficient or late. It carries no payable output.
	ErrFundingRejected = errors.New("SETTLE_002_REJECTED")
)

// Funding obligation kinds. A blocked funding requirement names the exact
// obligation a release must wait for instead of failing silently.
const (
	FundingMissing      = "FUNDING_MISSING"
	FundingInsufficient = "FUNDING_INSUFFICIENT"
	FundingLate         = "FUNDING_LATE"
)

// FundingSource is the governed account that must cover a funding
// requirement. Raw account detail never appears here, only the governed
// reference, and availability is an explicit dated amount.
type FundingSource struct {
	Ref             string
	Entity          string
	Currency        string
	AvailableAmount values.Decimal
	AvailableBy     values.LocalDate
}

// WorkerFundingTotal is one worker's share of the funding requirement as
// priced by payroll.
type WorkerFundingTotal struct {
	WorkerRef string
	Amount    values.Decimal
}

// FundingRequest is the complete governed question: the exact instruction
// set to fund, the worker shares and control total it must reconcile with,
// and the source that must cover it. All dates are explicit; there is no
// clock read.
type FundingRequest struct {
	Entity       string
	Currency     string
	ValueDate    values.LocalDate
	Instructions []PaymentInstruction
	WorkerTotals []WorkerFundingTotal
	ControlTotal values.Decimal
	Source       FundingSource
}

// FundingObligation is the explicit release-blocking obligation returned
// when the funding source is missing, insufficient or late.
type FundingObligation struct {
	Kind   string
	Field  string
	Detail string
}

// Required reports whether the obligation blocks release.
func (o FundingObligation) Required() bool { return o.Kind != "" }

// FundingRequirement is the verified answer: reconciled totals bound to one
// entity, currency, value date and funding source under a canonical digest.
type FundingRequirement struct {
	Entity            string
	Currency          string
	ValueDate         values.LocalDate
	InstructionCount  int
	InstructionDigest string
	WorkerTotal       values.Decimal
	InstructionTotal  values.Decimal
	ControlTotal      values.Decimal
	SourceRef         string
	RuleTrace         []string
	CanonicalDigest   string
}

func fundingRefusal(field, reason string, cause error) error {
	if cause != nil {
		return fmt.Errorf("%w: %s: %s: %v", ErrFundingRejected, field, reason, cause)
	}
	return fmt.Errorf("%w: %s: %s", ErrFundingRejected, field, reason)
}

// BuildFundingRequirement reconciles worker, instruction and control totals
// exactly by entity, currency and value date, then proves the funding source
// covers the total on time. Only INSTRUCTED instructions fund: provider
// acceptance is not funding, settlement is not funding, and returns or
// reversals never fund. The function is pure: it persists nothing and mutates
// nothing.
func BuildFundingRequirement(req FundingRequest) (FundingRequirement, FundingObligation, error) {
	var trace []string
	record := func(format string, args ...any) {
		trace = append(trace, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(req.Entity) == "" {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("entity", "entity is required", nil)
	}
	if strings.TrimSpace(req.Currency) == "" {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("currency", "currency is required", nil)
	}
	if !req.ValueDate.IsSet() {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("value_date", "value date is required", nil)
	}
	if err := req.ValueDate.Validate(); err != nil {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("value_date", "value date is invalid", err)
	}
	if len(req.Instructions) == 0 {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("instructions", "at least one instruction is required", nil)
	}
	if len(req.WorkerTotals) == 0 {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("worker_totals", "at least one worker total is required", nil)
	}
	scale := req.Instructions[0].Amount.Scale()
	seenID := make(map[string]bool, len(req.Instructions))
	seenKey := make(map[string]string, len(req.Instructions))
	instructionTotal := req.Instructions[0].Amount
	for n, instruction := range req.Instructions {
		if err := instruction.Validate(); err != nil {
			return FundingRequirement{}, FundingObligation{}, fundingRefusal(fmt.Sprintf("instructions[%d]", n), "instruction is invalid", err)
		}
		if instruction.State != StateInstructed {
			return FundingRequirement{}, FundingObligation{}, fundingRefusal(fmt.Sprintf("instructions[%d]", n), fmt.Sprintf("state %s is not fundable; only INSTRUCTED instructions fund", instruction.State), nil)
		}
		if instruction.Amount.Scale() != scale {
			return FundingRequirement{}, FundingObligation{}, fundingRefusal(fmt.Sprintf("instructions[%d]", n), "amount scale must match the requirement scale", nil)
		}
		if instruction.Currency != req.Currency {
			return FundingRequirement{}, FundingObligation{}, fundingRefusal(fmt.Sprintf("instructions[%d]", n), fmt.Sprintf("currency %s differs from requirement currency %s", instruction.Currency, req.Currency), nil)
		}
		if instruction.ValueDate.IsSet() && instruction.ValueDate.Compare(req.ValueDate) != 0 {
			return FundingRequirement{}, FundingObligation{}, fundingRefusal(fmt.Sprintf("instructions[%d]", n), "value date differs from requirement value date", nil)
		}
		if seenID[instruction.InstructionID] {
			return FundingRequirement{}, FundingObligation{}, fundingRefusal(fmt.Sprintf("instructions[%d]", n), "duplicate instruction identity would fund twice", nil)
		}
		seenID[instruction.InstructionID] = true
		key := instruction.NaturalKey()
		if prior, dup := seenKey[key]; dup {
			return FundingRequirement{}, FundingObligation{}, fundingRefusal(fmt.Sprintf("instructions[%d]", n), fmt.Sprintf("natural-key conflict with %s; refusing merge", prior), ErrIdempotencyConflict)
		}
		seenKey[key] = instruction.InstructionID
		if n > 0 {
			var err error
			instructionTotal, err = instructionTotal.Add(instruction.Amount)
			if err != nil {
				return FundingRequirement{}, FundingObligation{}, fundingRefusal("instruction_total", "instruction amounts do not sum", err)
			}
		}
	}
	record("instructions: %d fundable at %s on %s", len(req.Instructions), req.Currency, req.ValueDate.String())
	workerTotal := req.WorkerTotals[0].Amount
	if strings.TrimSpace(req.WorkerTotals[0].WorkerRef) == "" {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("worker_totals[0]", "worker reference is required", nil)
	}
	if err := workerTotal.Validate(); err != nil {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("worker_totals[0]", "worker amount is invalid", err)
	}
	if workerTotal.Scale() != scale {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("worker_totals[0]", "amount scale must match the requirement scale", nil)
	}
	for n := 1; n < len(req.WorkerTotals); n++ {
		line := req.WorkerTotals[n]
		if strings.TrimSpace(line.WorkerRef) == "" {
			return FundingRequirement{}, FundingObligation{}, fundingRefusal(fmt.Sprintf("worker_totals[%d]", n), "worker reference is required", nil)
		}
		if err := line.Amount.Validate(); err != nil {
			return FundingRequirement{}, FundingObligation{}, fundingRefusal(fmt.Sprintf("worker_totals[%d]", n), "worker amount is invalid", err)
		}
		if line.Amount.Scale() != scale {
			return FundingRequirement{}, FundingObligation{}, fundingRefusal(fmt.Sprintf("worker_totals[%d]", n), "amount scale must match the requirement scale", nil)
		}
		var err error
		workerTotal, err = workerTotal.Add(line.Amount)
		if err != nil {
			return FundingRequirement{}, FundingObligation{}, fundingRefusal("worker_total", "worker amounts do not sum", err)
		}
	}
	if err := req.ControlTotal.Validate(); err != nil {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("control_total", "control total is invalid", err)
	}
	if req.ControlTotal.Scale() != scale {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("control_total", "amount scale must match the requirement scale", nil)
	}
	if workerTotal.Cmp(instructionTotal) != 0 {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("worker_total", fmt.Sprintf("worker total %s does not equal instruction total %s", workerTotal.String(), instructionTotal.String()), nil)
	}
	record("worker total %s reconciles with instruction total", workerTotal.String())
	if req.ControlTotal.Cmp(instructionTotal) != 0 {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("control_total", fmt.Sprintf("control total %s does not equal instruction total %s", req.ControlTotal.String(), instructionTotal.String()), nil)
	}
	record("control total %s reconciles by %s/%s/%s", req.ControlTotal.String(), req.Entity, req.Currency, req.ValueDate.String())
	source := req.Source
	if strings.TrimSpace(source.Ref) == "" {
		return FundingRequirement{}, FundingObligation{
			Kind: FundingMissing, Field: "source.ref",
			Detail: fmt.Sprintf("no funding source covers %s %s for %s on %s", instructionTotal.String(), req.Currency, req.Entity, req.ValueDate.String()),
		}, fundingRefusal("source.ref", "funding source is missing", nil)
	}
	if source.Entity != req.Entity {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("source.entity", fmt.Sprintf("source entity %s differs from requirement entity %s", source.Entity, req.Entity), nil)
	}
	if source.Currency != req.Currency {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("source.currency", fmt.Sprintf("source currency %s differs from requirement currency %s", source.Currency, req.Currency), nil)
	}
	if err := source.AvailableAmount.Validate(); err != nil {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("source.available", "available amount is invalid", err)
	}
	if source.AvailableAmount.Scale() != scale {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("source.available", "amount scale must match the requirement scale", nil)
	}
	if !source.AvailableBy.IsSet() {
		return FundingRequirement{}, FundingObligation{
			Kind: FundingLate, Field: "source.available_by",
			Detail: fmt.Sprintf("funding source %s has no availability date for value date %s", source.Ref, req.ValueDate.String()),
		}, fundingRefusal("source.available_by", "funding availability date is required", nil)
	}
	if err := source.AvailableBy.Validate(); err != nil {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("source.available_by", "funding availability date is invalid", err)
	}
	if source.AvailableAmount.Cmp(instructionTotal) < 0 {
		return FundingRequirement{}, FundingObligation{
			Kind: FundingInsufficient, Field: "source.available",
			Detail: fmt.Sprintf("source %s covers %s of required %s %s", source.Ref, source.AvailableAmount.String(), instructionTotal.String(), req.Currency),
		}, fundingRefusal("source.available", "funding is insufficient", nil)
	}
	if source.AvailableBy.Compare(req.ValueDate) > 0 {
		return FundingRequirement{}, FundingObligation{
			Kind: FundingLate, Field: "source.available_by",
			Detail: fmt.Sprintf("source %s is available %s, after value date %s", source.Ref, source.AvailableBy.String(), req.ValueDate.String()),
		}, fundingRefusal("source.available_by", "funding is late", nil)
	}
	record("source %s covers %s %s by %s", source.Ref, instructionTotal.String(), req.Currency, source.AvailableBy.String())
	digests := make([]string, 0, len(req.Instructions))
	amounts := make([]values.Decimal, 0, len(req.Instructions))
	for _, instruction := range req.Instructions {
		digests = append(digests, instruction.CanonicalDigest)
		amounts = append(amounts, instruction.Amount)
	}
	sort.Strings(digests)
	instructionDigest := instructionSetDigest(digests, amounts)
	result := FundingRequirement{
		Entity: req.Entity, Currency: req.Currency, ValueDate: req.ValueDate,
		InstructionCount: len(req.Instructions), InstructionDigest: instructionDigest,
		WorkerTotal: workerTotal, InstructionTotal: instructionTotal,
		ControlTotal: req.ControlTotal, SourceRef: source.Ref, RuleTrace: trace,
	}
	w := canonicalbytes.New("hcmnext.domains.settlement.FundingRequirement", schemaVersion).
		String("entity", result.Entity).String("currency", result.Currency).
		Value("value_date", result.ValueDate).Int("instruction_count", int64(result.InstructionCount)).
		String("instruction_digest", result.InstructionDigest).
		Value("worker_total", result.WorkerTotal).Value("instruction_total", result.InstructionTotal).
		Value("control_total", result.ControlTotal).String("source_ref", result.SourceRef)
	raw, err := w.Bytes()
	if err != nil {
		return FundingRequirement{}, FundingObligation{}, fundingRefusal("digest", "canonical bytes are unavailable", err)
	}
	result.CanonicalDigest = canonicalbytes.Digest(raw)
	return result, FundingObligation{}, nil
}
