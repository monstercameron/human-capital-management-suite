// GARN-003: calculate disposable earnings and legal limits.
//
// ComputeWithholdingLimit turns exact gross earnings, itemized mandatory
// deductions and a pinned jurisdiction rule pack into disposable earnings
// and a bounded withholding trace. A missing fact or rule blocks: it
// never resolves to zero or a default. The function is kernel-pure.
package garnishment

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// LimitVersion is the rejection version carried by LimitRejection.
const LimitVersion = "garnishment-limit/v1"

var (
	// ErrLimitBlocked is the GARN-003 sentinel. Missing earnings,
	// deductions, jurisdiction or rule input blocks with this error; it
	// never resolves to a zero or default withholding.
	ErrLimitBlocked = errors.New("GARN_003_BLOCKED")
)

// LimitRejection is the stable GARN-003 failure shape.
type LimitRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *LimitRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrLimitBlocked, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the GARN_003_BLOCKED sentinel to errors.Is.
func (r *LimitRejection) Unwrap() error { return ErrLimitBlocked }

func limitBlocked(field, state, reason string) error {
	return &LimitRejection{Field: field, State: state, Version: LimitVersion, Reason: reason}
}

// MandatoryDeduction is one exact pre-withholding deduction with its
// legal basis. Only listed deductions reduce disposable earnings.
type MandatoryDeduction struct {
	Kind     string
	Amount   values.Decimal
	BasisRef string
}

// EarningsInput is one pay-period garnishment calculation request.
// Amounts are exact decimals; LimitBps is the legal cap in basis points
// of disposable earnings from the pinned rule pack.
type EarningsInput struct {
	Tenant          string
	WorkerRef       string
	OrderRef        string
	OrderType       OrderType
	Jurisdiction    string
	RulePackRef     string
	RulePackVersion string
	RulePackDigest  string
	Gross           values.Decimal
	Deductions      []MandatoryDeduction
	LimitBps        int64
	ProtectedFloor  values.Decimal
	OrderedAmount   values.Decimal
}

// WithholdingLimit is the bounded calculation result with its trace.
type WithholdingLimit struct {
	Disposable      values.Decimal
	MaxWithholding  values.Decimal
	OrderedCapped   values.Decimal
	Trace           []string
	CanonicalDigest string
}

func (l WithholdingLimit) computedDigest(in EarningsInput) string {
	w := canonicalbytes.New("hcmnext.domains.garnishment.WithholdingLimit", 1).
		String("tenant", in.Tenant).
		String("worker_ref", in.WorkerRef).
		String("order_ref", in.OrderRef).
		String("order_type", string(in.OrderType)).
		String("jurisdiction", in.Jurisdiction).
		String("rule_pack", in.RulePackRef).
		String("rule_version", in.RulePackVersion).
		String("rule_digest", in.RulePackDigest).
		Value("gross", in.Gross).
		Value("disposable", l.Disposable).
		Value("max_withholding", l.MaxWithholding).
		Value("ordered_capped", l.OrderedCapped).
		Int("limit_bps", in.LimitBps)
	for i, d := range in.Deductions {
		w.String(fmt.Sprintf("deduction_%d_kind", i), d.Kind)
		w.Value(fmt.Sprintf("deduction_%d_amount", i), d.Amount)
	}
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func limitScale() values.Decimal {
	return values.MustDecimal("10000.00", 2, values.RoundingHalfUp)
}

// ComputeWithholdingLimit calculates disposable earnings and the legal
// withholding bound for one order and pay period.
func ComputeWithholdingLimit(in EarningsInput) (WithholdingLimit, error) {
	if strings.TrimSpace(in.Tenant) == "" {
		return WithholdingLimit{}, limitBlocked("earnings.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(in.WorkerRef) == "" {
		return WithholdingLimit{}, limitBlocked("earnings.worker_ref", "MISSING", "worker ref is required")
	}
	if strings.TrimSpace(in.OrderRef) == "" {
		return WithholdingLimit{}, limitBlocked("earnings.order_ref", "MISSING", "order ref is required")
	}
	if !in.OrderType.Valid() {
		return WithholdingLimit{}, limitBlocked("earnings.order_type", "UNDECLARED", fmt.Sprintf("order type %q is not declared", in.OrderType))
	}
	if strings.TrimSpace(in.Jurisdiction) == "" {
		return WithholdingLimit{}, limitBlocked("earnings.jurisdiction", "MISSING", "jurisdiction is required")
	}
	if strings.TrimSpace(in.RulePackRef) == "" || strings.TrimSpace(in.RulePackVersion) == "" || strings.TrimSpace(in.RulePackDigest) == "" {
		return WithholdingLimit{}, limitBlocked("earnings.rule_pack", "MISSING", "pinned rule pack ref, version and digest are required")
	}
	if err := in.Gross.Validate(); err != nil || in.Gross.Sign() < 0 {
		return WithholdingLimit{}, limitBlocked("earnings.gross", "INVALID", "gross must be a valid non-negative decimal")
	}
	if err := in.OrderedAmount.Validate(); err != nil || in.OrderedAmount.Sign() < 0 {
		return WithholdingLimit{}, limitBlocked("earnings.ordered_amount", "INVALID", "ordered amount must be a valid non-negative decimal")
	}
	if err := in.ProtectedFloor.Validate(); err != nil {
		return WithholdingLimit{}, limitBlocked("earnings.protected_floor", "INVALID", "protected floor must be a valid decimal")
	}
	if in.LimitBps <= 0 || in.LimitBps > 10000 {
		return WithholdingLimit{}, limitBlocked("earnings.limit_bps", "OUT_OF_RANGE", "legal limit must be within (0, 10000] basis points")
	}
	total := in.Gross
	trace := []string{fmt.Sprintf("gross %s", in.Gross.String())}
	for i, d := range in.Deductions {
		if strings.TrimSpace(d.Kind) == "" || strings.TrimSpace(d.BasisRef) == "" {
			return WithholdingLimit{}, limitBlocked(fmt.Sprintf("earnings.deductions[%d]", i), "MISSING", "each deduction carries kind and legal basis")
		}
		if err := d.Amount.Validate(); err != nil || d.Amount.Sign() < 0 {
			return WithholdingLimit{}, limitBlocked(fmt.Sprintf("earnings.deductions[%d].amount", i), "INVALID", "deduction amounts must be valid non-negative decimals")
		}
		next, err := total.Sub(d.Amount)
		if err != nil {
			return WithholdingLimit{}, limitBlocked("earnings.disposable", "INEXACT", fmt.Sprintf("disposable is not computable: %v", err))
		}
		if next.Sign() < 0 {
			return WithholdingLimit{}, limitBlocked("earnings.disposable", "NEGATIVE", "deductions exceed gross")
		}
		total = next
		trace = append(trace, fmt.Sprintf("less %s %s (%s)", d.Kind, d.Amount.String(), d.BasisRef))
	}
	disposable := total
	rate, err := values.NewDecimal(fmt.Sprintf("%d.00", in.LimitBps), 2, values.RoundingHalfUp)
	if err != nil {
		return WithholdingLimit{}, limitBlocked("earnings.limit_bps", "INVALID", "limit is not representable")
	}
	scaled, err := disposable.Mul(rate, 2, values.RoundingHalfUp)
	if err != nil {
		return WithholdingLimit{}, limitBlocked("earnings.max_withholding", "INEXACT", fmt.Sprintf("legal cap is not computable: %v", err))
	}
	maxWithholding, err := scaled.Div(limitScale(), 2, values.RoundingHalfUp)
	if err != nil {
		return WithholdingLimit{}, limitBlocked("earnings.max_withholding", "INEXACT", fmt.Sprintf("legal cap is not computable: %v", err))
	}
	aboveFloor, err := disposable.Sub(in.ProtectedFloor)
	if err != nil {
		return WithholdingLimit{}, limitBlocked("earnings.protected_floor", "INEXACT", fmt.Sprintf("floor comparison is not computable: %v", err))
	}
	if aboveFloor.Sign() < 0 {
		maxWithholding = values.MustDecimal("0.00", 2, values.RoundingHalfUp)
		trace = append(trace, "disposable below protected floor: cap is zero")
	} else if maxWithholding.Cmp(aboveFloor) > 0 {
		maxWithholding = aboveFloor
		trace = append(trace, "cap clamped to disposable above the protected floor")
	}
	capped := in.OrderedAmount
	if capped.Cmp(maxWithholding) > 0 {
		capped = maxWithholding
		trace = append(trace, fmt.Sprintf("ordered %s capped to legal max %s", in.OrderedAmount.String(), maxWithholding.String()))
	} else {
		trace = append(trace, fmt.Sprintf("ordered %s within legal max %s", in.OrderedAmount.String(), maxWithholding.String()))
	}
	res := WithholdingLimit{Disposable: disposable, MaxWithholding: maxWithholding, OrderedCapped: capped, Trace: trace}
	res.CanonicalDigest = res.computedDigest(in)
	return res, nil
}
