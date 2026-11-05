package rewards

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// WF-CAP-010 implements the native severance calculation behind the
// `rewards.calculate_severance` capability: a published severance plan
// (tenure, pay basis, caps) applied in exact decimals with an explanation.
//
// The calculation is kernel-pure: it reads a declared plan and a declared
// input and returns a typed result. It writes nothing, reserves nothing and
// calls out to nothing; this file owns the arithmetic the registered
// capability contract invokes.
const (
	// CalculateSeveranceIntentType is the catalog identifier.
	CalculateSeveranceIntentType = "hcmnext.rewards.calculate_severance"
	// CalculateSeveranceIntentVersion is the served contract version.
	CalculateSeveranceIntentVersion = "v1"
)

var (
	// ErrSeveranceInvalid is returned for an unusable plan or input.
	ErrSeveranceInvalid = errors.New("rewards: severance plan or input is invalid")
	// ErrSeveranceCurrencyMismatch is returned instead of making an FX
	// decision from an ambient worker or plan currency.
	ErrSeveranceCurrencyMismatch = errors.New("rewards: severance currency mismatch")
)

// severanceWeekScale is the declared scale of the credited-weeks product.
// Tenure and the weeks-per-year factor may arrive at different declared
// scales, and reconciling them is a schema decision owned here, not by the
// caller: the product is always computed at this scale with the plan's
// rounding mode.
const severanceWeekScale int32 = 4

// SeverancePlan is the published employer policy a severance calculation
// applies. WeeksPerYear prices each year of tenure in weeks of pay;
// MinWeeks and MaxWeeks bound the credited weeks from below and above.
type SeverancePlan struct {
	Version       string
	WeeksPerYear  values.Decimal
	MinWeeks      values.Decimal
	MaxWeeks      values.Decimal
	Currency      string
	MoneyScale    int32
	MoneyRounding values.RoundingMode
}

// Validate reports whether the plan is usable on its own terms.
func (p SeverancePlan) Validate() error {
	if p.Version == "" || p.Currency == "" {
		return fmt.Errorf("%w: version and currency are required", ErrSeveranceInvalid)
	}
	// NewMoney is used only as the kernel's public currency-shape validator.
	if _, err := values.NewMoney("0", p.Currency, 0, values.RoundingHalfEven); err != nil {
		return fmt.Errorf("%w: currency: %w", ErrSeveranceInvalid, err)
	}
	for _, factor := range []struct {
		name  string
		value values.Decimal
	}{
		{name: "weeks_per_year", value: p.WeeksPerYear},
		{name: "min_weeks", value: p.MinWeeks},
		{name: "max_weeks", value: p.MaxWeeks},
	} {
		if err := factor.value.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrSeveranceInvalid, factor.name, err)
		}
		if factor.value.Sign() < 0 {
			return fmt.Errorf("%w: %s must not be negative", ErrSeveranceInvalid, factor.name)
		}
	}
	if p.WeeksPerYear.Sign() == 0 {
		return fmt.Errorf("%w: weeks_per_year must be positive", ErrSeveranceInvalid)
	}
	if p.MinWeeks.Cmp(p.MaxWeeks) > 0 {
		return fmt.Errorf("%w: min_weeks exceeds max_weeks", ErrSeveranceInvalid)
	}
	if p.MoneyScale < 0 || p.MoneyScale > values.MaxScale {
		return fmt.Errorf("%w: money scale %d", ErrSeveranceInvalid, p.MoneyScale)
	}
	if !p.MoneyRounding.Valid() || p.MoneyRounding == values.RoundingUnspecified {
		return fmt.Errorf("%w: money rounding is unspecified", ErrSeveranceInvalid)
	}
	return nil
}

// SeveranceInput is the declared per-worker input: one week of pay in the
// plan currency and the worker's tenure in years. Neither is inferred.
type SeveranceInput struct {
	WeeklyPay   values.Money
	TenureYears values.Decimal
}

// Validate reports whether the input is usable on its own terms.
func (in SeveranceInput) Validate() error {
	if err := in.WeeklyPay.Validate(); err != nil {
		return fmt.Errorf("%w: weekly pay: %w", ErrSeveranceInvalid, err)
	}
	if err := in.TenureYears.Validate(); err != nil {
		return fmt.Errorf("%w: tenure: %w", ErrSeveranceInvalid, err)
	}
	if in.TenureYears.Sign() < 0 {
		return fmt.Errorf("%w: tenure must not be negative", ErrSeveranceInvalid)
	}
	return nil
}

// SeveranceResult is the typed calculation outcome. Explanation is the
// deterministic, human-readable derivation of Amount from the plan and the
// input; it carries no new inputs.
type SeveranceResult struct {
	IntentType    string
	IntentVersion string

	PlanVersion   string
	CreditedWeeks values.Decimal
	Amount        values.Money
	Explanation   string

	InputsDigest string
	ResultDigest string
	Effects      evidence.EffectCounters
}

// inputDigest returns the canonical digest of one severance evaluation's
// inputs: the plan and the per-worker input that produced it. Two identical
// evaluations produce the same digest in every process.
func (r SeveranceResult) inputDigest(plan SeverancePlan, in SeveranceInput) (string, error) {
	return canonicalbytes.New("hcmnext.domains.rewards.CalculateSeveranceInput", rewardsSchemaVer).
		String("intent_type", CalculateSeveranceIntentType).
		String("intent_version", CalculateSeveranceIntentVersion).
		String("plan_version", plan.Version).
		Value("weeks_per_year", plan.WeeksPerYear).
		Value("min_weeks", plan.MinWeeks).
		Value("max_weeks", plan.MaxWeeks).
		String("currency", plan.Currency).
		Int("money_scale", int64(plan.MoneyScale)).
		String("money_rounding", plan.MoneyRounding.String()).
		Value("weekly_pay", in.WeeklyPay).
		Value("tenure_years", in.TenureYears).
		Digest()
}

// canonicalBody encodes everything the result digest covers.
func (r SeveranceResult) canonicalBody() ([]byte, error) {
	return canonicalbytes.New("hcmnext.domains.rewards.SeveranceResult", rewardsSchemaVer).
		String("intent_type", r.IntentType).
		String("intent_version", r.IntentVersion).
		String("plan_version", r.PlanVersion).
		Value("credited_weeks", r.CreditedWeeks).
		Value("amount", r.Amount).
		String("explanation", r.Explanation).
		Value("effects", r.Effects).
		Bytes()
}

// Canonical returns the canonical byte encoding, or nil when incoherent.
func (r SeveranceResult) Canonical() []byte {
	body, err := r.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.SeveranceResultEnvelope", rewardsSchemaVer).
		Field("body", body).
		String("inputs_digest", r.InputsDigest).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// CalculateSeverance applies plan to in: raw weeks are tenure times the
// weeks-per-year price, credited weeks clamp the raw product into the
// [min, max] bounds, and the amount is one week of pay times credited weeks
// in exact decimals.
func CalculateSeverance(plan SeverancePlan, in SeveranceInput) (SeveranceResult, error) {
	if err := plan.Validate(); err != nil {
		return SeveranceResult{}, err
	}
	if err := in.Validate(); err != nil {
		return SeveranceResult{}, err
	}
	if in.WeeklyPay.Currency() != plan.Currency {
		return SeveranceResult{}, fmt.Errorf("%w: pay is %s, plan is %s",
			ErrSeveranceCurrencyMismatch, in.WeeklyPay.Currency(), plan.Currency)
	}
	raw, err := in.TenureYears.Mul(plan.WeeksPerYear, severanceWeekScale, plan.MoneyRounding)
	if err != nil {
		return SeveranceResult{}, fmt.Errorf("%w: raw weeks: %w", ErrSeveranceInvalid, err)
	}
	credited := raw
	bound := ""
	switch {
	case credited.Cmp(plan.MinWeeks) < 0:
		credited, bound = plan.MinWeeks, "floor"
	case credited.Cmp(plan.MaxWeeks) > 0:
		credited, bound = plan.MaxWeeks, "cap"
	}
	// Normalize the credited weeks to the calculation scale so the result
	// type is stable however the bound was declared.
	credited, err = credited.Mul(mustSeveranceOne(), severanceWeekScale, plan.MoneyRounding)
	if err != nil {
		return SeveranceResult{}, fmt.Errorf("%w: credited weeks: %w", ErrSeveranceInvalid, err)
	}
	amount, err := in.WeeklyPay.MulDecimal(credited, plan.MoneyScale, plan.MoneyRounding)
	if err != nil {
		return SeveranceResult{}, fmt.Errorf("%w: amount: %w", ErrSeveranceInvalid, err)
	}
	applied := "raw"
	if bound != "" {
		applied = bound + " applied"
	}
	explanation := fmt.Sprintf("severance plan %s: %s years x %s weeks/year = %s weeks; bounds [%s, %s] -> %s weeks (%s); %s weeks x %s/week = %s",
		plan.Version,
		in.TenureYears.String(),
		plan.WeeksPerYear.String(),
		raw.String(),
		plan.MinWeeks.String(),
		plan.MaxWeeks.String(),
		credited.String(),
		applied,
		credited.String(),
		in.WeeklyPay.String(),
		amount.String(),
	)
	result := SeveranceResult{
		IntentType:    CalculateSeveranceIntentType,
		IntentVersion: CalculateSeveranceIntentVersion,
		PlanVersion:   plan.Version,
		CreditedWeeks: credited,
		Amount:        amount,
		Explanation:   explanation,
		Effects:       evidence.ZeroEffects(),
	}
	inputsDigest, err := result.inputDigest(plan, in)
	if err != nil {
		return SeveranceResult{}, fmt.Errorf("%w: inputs digest: %w", ErrSeveranceInvalid, err)
	}
	result.InputsDigest = inputsDigest
	body, err := result.canonicalBody()
	if err != nil {
		return SeveranceResult{}, fmt.Errorf("%w: result digest: %w", ErrSeveranceInvalid, err)
	}
	result.ResultDigest = canonicalbytes.Digest(body)
	return result, nil
}

// mustSeveranceOne is the multiplicative identity at scale zero. It cannot
// fail: the literal is fixed and the scale and mode are in range.
func mustSeveranceOne() values.Decimal {
	one, err := values.NewDecimal("1", 0, values.RoundingHalfEven)
	if err != nil {
		panic(err)
	}
	return one
}
