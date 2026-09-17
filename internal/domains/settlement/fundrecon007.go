// SETTLE-007: reconcile funding, instructions and settlements.
//
// ReconcileFunding compares funding lines, payment instructions and rail
// settlements per instruction. Totals and per-payment states classify
// exact settled, rejected, returned, pending and unknown deltas, and
// payroll completion stays blocked until the policy permits. Acceptance
// is never settlement: an acceptance observation cannot settle an
// instruction. The function is kernel-pure.
package settlement

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// FundReconVersion is the rejection version for funding reconciliation.
const FundReconVersion = "settlement-fundrecon/v1"

var (
	// ErrFundReconRejected is the SETTLE-007 sentinel. Calling acceptance
	// settlement, or funding and return totals that mismatch, fails with
	// this error carrying the offending field, state and version, and
	// nothing is persisted.
	ErrFundReconRejected = errors.New("SETTLE_007_REJECTED")
)

// FundReconRejection is the stable SETTLE-007 failure shape.
type FundReconRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *FundReconRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrFundReconRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the SETTLE_007_REJECTED sentinel to errors.Is.
func (r *FundReconRejection) Unwrap() error { return ErrFundReconRejected }

func fundReconReject(field, state, reason string) error {
	return &FundReconRejection{Field: field, State: state, Version: FundReconVersion, Reason: reason}
}

// PaymentFate is the closed SETTLE-007 per-payment vocabulary.
type PaymentFate string

const (
	FateSettled  PaymentFate = "SETTLED"
	FateRejected PaymentFate = "REJECTED"
	FateReturned PaymentFate = "RETURNED"
	FatePending  PaymentFate = "PENDING"
	FateUnknown  PaymentFate = "UNKNOWN"
)

// InstructionLine is one funded payment instruction.
type InstructionLine struct {
	Ref    string
	Amount values.Decimal
}

// SettlementLine is one rail observation for an instruction. Kind must be
// a terminal rail fact: ACCEPTANCE never settles.
type SettlementLine struct {
	Ref    string
	Kind   ObservationKind
	Amount values.Decimal
}

// FundReconPolicy gates payroll completion. Unknown or pending payments
// block completion unless the policy explicitly tolerates them.
type FundReconPolicy struct {
	AllowPendingCompletion bool
	AllowUnknownCompletion bool
}

// FundReconInput reconciles one payroll run.
type FundReconInput struct {
	Tenant       string
	RunRef       string
	FundingTotal values.Decimal
	Instructions []InstructionLine
	Settlements  []SettlementLine
	Policy       FundReconPolicy
	Now          time.Time
}

// PaymentRecon is the classified fate of one instruction.
type PaymentRecon struct {
	Ref    string
	Amount values.Decimal
	Fate   PaymentFate
	Detail string
}

// FundReconResult carries totals, per-payment fates and the completion
// gate.
type FundReconResult struct {
	Payments          []PaymentRecon
	InstructionTotal  values.Decimal
	SettledTotal      values.Decimal
	RejectedTotal     values.Decimal
	ReturnedTotal     values.Decimal
	PendingTotal      values.Decimal
	UnknownTotal      values.Decimal
	FundingDelta      values.Decimal
	CompletionAllowed bool
	Digest            string
}

func (r FundReconResult) computedDigest(in FundReconInput) string {
	payments := make([]string, 0, len(r.Payments))
	for _, p := range r.Payments {
		payments = append(payments, strings.Join([]string{p.Ref, p.Amount.String(), string(p.Fate)}, "\x00"))
	}
	sort.Strings(payments)
	w := canonicalbytes.New("hcmnext.domains.settlement.FundRecon", 1).
		String("tenant", in.Tenant).
		String("run", in.RunRef).
		Value("funding", in.FundingTotal).
		Value("instructions", r.InstructionTotal).
		Value("settled", r.SettledTotal).
		Value("rejected", r.RejectedTotal).
		Value("returned", r.ReturnedTotal).
		Value("pending", r.PendingTotal).
		Value("unknown", r.UnknownTotal).
		Value("funding_delta", r.FundingDelta).
		Bool("completion", r.CompletionAllowed).
		SortedStrings("payments", payments)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func addTotal(base, delta values.Decimal, field string) (values.Decimal, error) {
	out, err := base.Add(delta)
	if err != nil {
		return values.Decimal{}, fundReconReject(field, "INEXACT", fmt.Sprintf("total is not computable: %v", err))
	}
	return out, nil
}

// ReconcileFunding reconciles funding, instructions and settlements for
// one run. Seeded defects — acceptance presented as settlement, or
// funding and return totals that mismatch — are refused with
// SETTLE_007_REJECTED naming field, state and version.
func ReconcileFunding(in FundReconInput) (FundReconResult, error) {
	if strings.TrimSpace(in.Tenant) == "" {
		return FundReconResult{}, fundReconReject("fundrecon.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(in.RunRef) == "" {
		return FundReconResult{}, fundReconReject("fundrecon.run_ref", "MISSING", "run ref is required")
	}
	if err := in.FundingTotal.Validate(); err != nil || in.FundingTotal.Sign() < 0 {
		return FundReconResult{}, fundReconReject("fundrecon.funding_total", "INVALID", "funding total must be a valid non-negative decimal")
	}
	if len(in.Instructions) == 0 {
		return FundReconResult{}, fundReconReject("fundrecon.instructions", "MISSING", "at least one instruction is required")
	}
	if in.Now.IsZero() {
		return FundReconResult{}, fundReconReject("fundrecon.now", "MISSING", "reconciliation instant is required")
	}
	settledByRef := make(map[string][]SettlementLine, len(in.Settlements))
	for i, s := range in.Settlements {
		if strings.TrimSpace(s.Ref) == "" {
			return FundReconResult{}, fundReconReject(fmt.Sprintf("fundrecon.settlements[%d].ref", i), "MISSING", "settlement ref is required")
		}
		if err := s.Amount.Validate(); err != nil || s.Amount.Sign() < 0 {
			return FundReconResult{}, fundReconReject(fmt.Sprintf("fundrecon.settlements[%d].amount", i), "INVALID", "settlement amount must be valid and non-negative")
		}
		// Acceptance is an observation, never settlement.
		if s.Kind == ObservationAcceptance {
			return FundReconResult{}, fundReconReject(fmt.Sprintf("fundrecon.settlements[%d].kind", i), "ACCEPTANCE_NOT_SETTLEMENT", "acceptance cannot settle an instruction")
		}
		if !s.Kind.valid() {
			return FundReconResult{}, fundReconReject(fmt.Sprintf("fundrecon.settlements[%d].kind", i), "UNDECLARED", fmt.Sprintf("kind %q is not declared", s.Kind))
		}
		settledByRef[s.Ref] = append(settledByRef[s.Ref], s)
	}
	res := FundReconResult{}
	zero := values.MustDecimal("0.00", 2, values.RoundingHalfUp)
	res.InstructionTotal, res.SettledTotal, res.RejectedTotal, res.ReturnedTotal, res.PendingTotal, res.UnknownTotal = zero, zero, zero, zero, zero, zero
	seen := make(map[string]struct{}, len(in.Instructions))
	var err error
	for _, line := range in.Instructions {
		if strings.TrimSpace(line.Ref) == "" {
			return FundReconResult{}, fundReconReject("fundrecon.instruction.ref", "MISSING", "instruction ref is required")
		}
		if _, dup := seen[line.Ref]; dup {
			return FundReconResult{}, fundReconReject("fundrecon.instruction.ref", "DUPLICATE", fmt.Sprintf("instruction %s repeats", line.Ref))
		}
		seen[line.Ref] = struct{}{}
		if err := line.Amount.Validate(); err != nil || line.Amount.Sign() <= 0 {
			return FundReconResult{}, fundReconReject("fundrecon.instruction.amount", "INVALID", "instruction amount must be valid and positive")
		}
		res.InstructionTotal, err = addTotal(res.InstructionTotal, line.Amount, "fundrecon.instruction_total")
		if err != nil {
			return FundReconResult{}, err
		}
		payment := PaymentRecon{Ref: line.Ref, Amount: line.Amount}
		obs := settledByRef[line.Ref]
		switch len(obs) {
		case 0:
			payment.Fate = FatePending
			payment.Detail = "no rail observation yet"
			res.PendingTotal, err = addTotal(res.PendingTotal, line.Amount, "fundrecon.pending_total")
		case 1:
			switch obs[0].Kind {
			case ObservationSettlement:
				if !obs[0].Amount.Equal(line.Amount) {
					return FundReconResult{}, fundReconReject("fundrecon.settlements.amount", "MISMATCH", fmt.Sprintf("settled amount diverges for %s", line.Ref))
				}
				payment.Fate = FateSettled
				payment.Detail = "rail settlement matches the instruction"
				res.SettledTotal, err = addTotal(res.SettledTotal, line.Amount, "fundrecon.settled_total")
			case ObservationRejection:
				payment.Fate = FateRejected
				payment.Detail = "rail rejected the instruction"
				res.RejectedTotal, err = addTotal(res.RejectedTotal, line.Amount, "fundrecon.rejected_total")
			case ObservationReturn:
				if !obs[0].Amount.Equal(line.Amount) {
					return FundReconResult{}, fundReconReject("fundrecon.settlements.amount", "MISMATCH", fmt.Sprintf("return amount diverges for %s", line.Ref))
				}
				payment.Fate = FateReturned
				payment.Detail = "rail returned the payment"
				res.ReturnedTotal, err = addTotal(res.ReturnedTotal, line.Amount, "fundrecon.returned_total")
			default:
				payment.Fate = FateUnknown
				payment.Detail = "indeterminate rail evidence"
				res.UnknownTotal, err = addTotal(res.UnknownTotal, line.Amount, "fundrecon.unknown_total")
			}
		default:
			return FundReconResult{}, fundReconReject("fundrecon.settlements.ref", "DUPLICATE_OBSERVATION", fmt.Sprintf("instruction %s carries competing observations", line.Ref))
		}
		if err != nil {
			return FundReconResult{}, err
		}
		res.Payments = append(res.Payments, payment)
	}
	res.FundingDelta, err = in.FundingTotal.Sub(res.InstructionTotal)
	if err != nil {
		return FundReconResult{}, fundReconReject("fundrecon.funding_delta", "INEXACT", fmt.Sprintf("funding delta is not computable: %v", err))
	}
	if res.FundingDelta.Sign() != 0 {
		return FundReconResult{}, fundReconReject("fundrecon.funding_total", "MISMATCH", fmt.Sprintf("funding %s does not cover instructions %s", in.FundingTotal.String(), res.InstructionTotal.String()))
	}
	blocked := false
	for _, p := range res.Payments {
		switch p.Fate {
		case FatePending:
			if !in.Policy.AllowPendingCompletion {
				blocked = true
			}
		case FateUnknown:
			if !in.Policy.AllowUnknownCompletion {
				blocked = true
			}
		case FateRejected, FateReturned:
			blocked = true
		case FateSettled:
		}
	}
	res.CompletionAllowed = !blocked
	res.Digest = res.computedDigest(in)
	return res, nil
}
