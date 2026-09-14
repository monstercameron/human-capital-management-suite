// Workforce, headcount and cost impact: SCENARIO-003 calculates the
// headcount delta between a baseline and a hypothetical target and
// prices it with a cited rate source.
//
// The output binds the formula, the rate provenance, the population
// snapshot, the horizon and the exact assumptions behind it. A missing
// rate source produces an explicit unknown with a reason — never a
// fabricated zero or guess. A ranged rate source produces a cost range.
package scenario

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ImpactFormula is the only formula this calculation applies.
const ImpactFormula = "headcount_delta * burdened_rate"

// RateSource cites the provenance of the burdened rate behind a cost
// impact. Exactly one of an exact rate or a low/high range is carried.
type RateSource struct {
	Ref      string
	Exact    values.Decimal
	HasExact bool
	Low      values.Decimal
	High     values.Decimal
	HasRange bool
}

// Validate implements validation.
func (r RateSource) Validate() error {
	if strings.TrimSpace(r.Ref) == "" {
		return fmt.Errorf("%w: rate source ref is required", ErrInvalidScenario)
	}
	if r.HasExact == r.HasRange {
		return fmt.Errorf("%w: rate source carries exactly one of an exact rate or a range", ErrInvalidScenario)
	}
	if r.HasExact {
		if err := r.Exact.Validate(); err != nil {
			return fmt.Errorf("%w: exact rate: %v", ErrInvalidScenario, err)
		}
		return nil
	}
	if err := r.Low.Validate(); err != nil {
		return fmt.Errorf("%w: range low: %v", ErrInvalidScenario, err)
	}
	if err := r.High.Validate(); err != nil {
		return fmt.Errorf("%w: range high: %v", ErrInvalidScenario, err)
	}
	if r.Low.Cmp(r.High) > 0 {
		return fmt.Errorf("%w: rate range low exceeds high", ErrInvalidScenario)
	}
	return nil
}

// ImpactRequest is the fully bound input to one impact calculation.
type ImpactRequest struct {
	HeadcountBaseline values.Decimal
	HeadcountTarget   values.Decimal
	Rate              *RateSource
	PopulationRef     string
	Horizon           values.EffectiveInterval
	AssumptionKeys    []string
}

// Validate implements validation.
func (r ImpactRequest) Validate() error {
	if err := r.HeadcountBaseline.Validate(); err != nil {
		return fmt.Errorf("%w: headcount baseline: %v", ErrInvalidScenario, err)
	}
	if err := r.HeadcountTarget.Validate(); err != nil {
		return fmt.Errorf("%w: headcount target: %v", ErrInvalidScenario, err)
	}
	if r.Rate != nil {
		if err := r.Rate.Validate(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(r.PopulationRef) == "" {
		return fmt.Errorf("%w: population ref is required", ErrInvalidScenario)
	}
	if err := r.Horizon.Validate(); err != nil {
		return fmt.Errorf("%w: horizon: %v", ErrInvalidScenario, err)
	}
	if len(r.AssumptionKeys) == 0 {
		return fmt.Errorf("%w: at least one assumption key is required", ErrInvalidScenario)
	}
	seen := make(map[string]struct{}, len(r.AssumptionKeys))
	for _, key := range r.AssumptionKeys {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%w: empty assumption key", ErrInvalidScenario)
		}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate assumption key %q", ErrInvalidScenario, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// WorkforceImpact is the bound result. CostLow and CostHigh are equal
// for an exact rate and meaningless unless CostKnown; an unknown cost
// always carries CostUnknownReason instead of numbers.
type WorkforceImpact struct {
	HeadcountDelta    values.Decimal
	CostLow           values.Decimal
	CostHigh          values.Decimal
	CostKnown         bool
	CostUnknownReason string
	Formula           string
	RateRef           string
	PopulationRef     string
	Horizon           string
	AssumptionKeys    []string
	CanonicalDigest   string
}

// Digest is the stable alias used by audit consumers.
func (w WorkforceImpact) Digest() string { return w.CanonicalDigest }

// Validate implements validation.
func (w WorkforceImpact) Validate() error {
	if err := w.HeadcountDelta.Validate(); err != nil {
		return fmt.Errorf("%w: headcount delta: %v", ErrInvalidScenario, err)
	}
	if w.Formula != ImpactFormula {
		return fmt.Errorf("%w: unknown impact formula %q", ErrInvalidScenario, w.Formula)
	}
	if strings.TrimSpace(w.PopulationRef) == "" || strings.TrimSpace(w.Horizon) == "" || len(w.AssumptionKeys) == 0 {
		return fmt.Errorf("%w: impact bindings are required", ErrInvalidScenario)
	}
	if w.CostKnown {
		if strings.TrimSpace(w.CostUnknownReason) != "" || strings.TrimSpace(w.RateRef) == "" {
			return fmt.Errorf("%w: known cost needs a rate ref and no unknown reason", ErrInvalidScenario)
		}
		if err := w.CostLow.Validate(); err != nil {
			return fmt.Errorf("%w: cost low: %v", ErrInvalidScenario, err)
		}
		if err := w.CostHigh.Validate(); err != nil {
			return fmt.Errorf("%w: cost high: %v", ErrInvalidScenario, err)
		}
		if w.CostLow.Cmp(w.CostHigh) > 0 {
			return fmt.Errorf("%w: cost low exceeds high", ErrInvalidScenario)
		}
	} else if strings.TrimSpace(w.CostUnknownReason) == "" {
		return fmt.Errorf("%w: unknown cost needs a reason", ErrInvalidScenario)
	}
	if w.CanonicalDigest != "" && w.CanonicalDigest != w.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidScenario)
	}
	return nil
}

func (w WorkforceImpact) body() []byte {
	keys := append([]string(nil), w.AssumptionKeys...)
	sort.Strings(keys)
	b := canonicalbytes.New("hcmnext.domains.scenario.WorkforceImpact", schemaVersion).
		Value("headcount_delta", w.HeadcountDelta).Value("cost_low", w.CostLow).Value("cost_high", w.CostHigh).
		Bool("cost_known", w.CostKnown).String("cost_unknown_reason", w.CostUnknownReason).
		String("formula", w.Formula).String("rate_ref", w.RateRef).
		String("population_ref", w.PopulationRef).String("horizon", w.Horizon).
		SortedStrings("assumption_key", keys)
	raw, err := b.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (w WorkforceImpact) computedDigest() string {
	raw := w.body()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Canonical returns the canonical bytes of a valid impact.
func (w WorkforceImpact) Canonical() []byte {
	if w.Validate() != nil {
		return nil
	}
	return w.body()
}

// CalculateImpact prices the headcount delta between baseline and
// target with the cited rate source. It is pure: no rows, events,
// effects or writes.
func CalculateImpact(request ImpactRequest) (WorkforceImpact, error) {
	if err := request.Validate(); err != nil {
		return WorkforceImpact{}, err
	}
	delta, err := request.HeadcountTarget.Sub(request.HeadcountBaseline)
	if err != nil {
		return WorkforceImpact{}, fmt.Errorf("%w: headcount delta: %v", ErrInvalidScenario, err)
	}
	keys := append([]string(nil), request.AssumptionKeys...)
	sort.Strings(keys)
	impact := WorkforceImpact{
		HeadcountDelta: delta,
		CostLow:        values.MustDecimal("0", 0, values.RoundingHalfEven),
		CostHigh:       values.MustDecimal("0", 0, values.RoundingHalfEven),
		Formula:        ImpactFormula,
		PopulationRef:  request.PopulationRef,
		Horizon:        request.Horizon.String(),
		AssumptionKeys: keys,
	}
	if request.Rate == nil {
		impact.CostUnknownReason = "no rate source: cost unknown, never estimated"
		impact.CanonicalDigest = impact.computedDigest()
		return impact, nil
	}
	impact.RateRef = request.Rate.Ref
	impact.CostKnown = true
	if request.Rate.HasExact {
		cost, err := delta.Mul(request.Rate.Exact, request.Rate.Exact.Scale(), values.RoundingHalfEven)
		if err != nil {
			return WorkforceImpact{}, fmt.Errorf("%w: exact cost: %v", ErrInvalidScenario, err)
		}
		impact.CostLow, impact.CostHigh = cost, cost
	} else {
		low, err := delta.Mul(request.Rate.Low, request.Rate.Low.Scale(), values.RoundingHalfEven)
		if err != nil {
			return WorkforceImpact{}, fmt.Errorf("%w: range low cost: %v", ErrInvalidScenario, err)
		}
		high, err := delta.Mul(request.Rate.High, request.Rate.High.Scale(), values.RoundingHalfEven)
		if err != nil {
			return WorkforceImpact{}, fmt.Errorf("%w: range high cost: %v", ErrInvalidScenario, err)
		}
		if low.Cmp(high) > 0 {
			low, high = high, low
		}
		impact.CostLow, impact.CostHigh = low, high
	}
	impact.CanonicalDigest = impact.computedDigest()
	if err := impact.Validate(); err != nil {
		return WorkforceImpact{}, err
	}
	return impact, nil
}
