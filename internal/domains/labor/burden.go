package labor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrBurdenRejected is the LABOR-004 refusal boundary. A missing currency,
// component, basis, rate, cap binding or period blocks authoritative cost;
// totals alone never pass.
var ErrBurdenRejected = errors.New("LABOR_004_REJECTED")

// BurdenKind is the closed vocabulary of employer-burden components.
type BurdenKind string

const (
	BurdenTax       BurdenKind = "TAX"
	BurdenBenefit   BurdenKind = "BENEFIT"
	BurdenAllowance BurdenKind = "ALLOWANCE"
	BurdenOverhead  BurdenKind = "OVERHEAD"
)

func (k BurdenKind) Valid() bool {
	switch k {
	case BurdenTax, BurdenBenefit, BurdenAllowance, BurdenOverhead:
		return true
	default:
		return false
	}
}

// BurdenItem is one employer-burden component. When Capped, Cap binds the
// maximum assessable basis; when uncapped, the exposure above the stated
// basis is an unknown range, never an implicit zero.
type BurdenItem struct {
	Kind   BurdenKind
	Name   string
	Basis  values.Decimal
	Rate   values.Decimal
	Capped bool
	Cap    values.Decimal
	Period string
	Amount values.Decimal
}

// BurdenRequest asks for employer burden and total labor cost under one
// bound rule version. Every closed burden kind must appear at least once;
// an omitted kind is rejected even when the remaining totals look exact.
type BurdenRequest struct {
	CalculationID string
	Rule          LaborRule
	RuleID        string
	RuleVersion   string
	Currency      string
	Wages         values.Decimal
	AmountScale   int32
	Rounding      values.RoundingMode
	Items         []BurdenItem
}

// BurdenCalculation is the immutable result: exact component totals plus
// the unknown ranges where inputs are incomplete.
type BurdenCalculation struct {
	CalculationID string
	RuleID        string
	RuleVersion   string
	Currency      string
	Wages         values.Decimal
	Items         []BurdenItem
	TotalBurden   values.Decimal
	TotalCost     values.Decimal
	UnknownRanges []string
	Complete      bool
	AmountScale   int32
	Rounding      values.RoundingMode
	Digest        string
}

// CalculateBurden returns exact burden components and total labor cost.
func CalculateBurden(req BurdenRequest) (BurdenCalculation, error) {
	fail := func(format string, args ...any) (BurdenCalculation, error) {
		return BurdenCalculation{}, errors.Join(ErrBurdenRejected, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(req.CalculationID) == "" {
		return fail("calculation id is required")
	}
	if err := req.Rule.Validate(); err != nil {
		return fail("rule: %v", err)
	}
	if req.RuleID != req.Rule.ID || req.RuleVersion != req.Rule.Version {
		return fail("request binds rule %s/%s, have %s/%s", req.RuleID, req.RuleVersion, req.Rule.ID, req.Rule.Version)
	}
	if strings.TrimSpace(req.Currency) == "" {
		return fail("currency is required")
	}
	if req.Currency != req.Rule.Currency {
		return fail("currency %q does not match rule currency %q", req.Currency, req.Rule.Currency)
	}
	if err := req.Wages.Validate(); err != nil {
		return fail("wages: %v", err)
	}
	if req.Wages.Sign() < 0 {
		return fail("wages cannot be negative")
	}
	if req.AmountScale < 0 || req.AmountScale > values.MaxScale {
		return fail("amount scale %d is out of range", req.AmountScale)
	}
	if !req.Rounding.Valid() || req.Rounding == values.RoundingUnspecified {
		return fail("rounding mode must be declared")
	}
	if len(req.Items) == 0 {
		return fail("at least one burden item is required")
	}
	seenKind := map[BurdenKind]struct{}{}
	seenName := map[string]struct{}{}
	items := make([]BurdenItem, len(req.Items))
	unknown := []string{}
	for i, item := range req.Items {
		where := fmt.Sprintf("item %d", i)
		if !item.Kind.Valid() {
			return fail("%s: kind %q is not declared", where, item.Kind)
		}
		if strings.TrimSpace(item.Name) == "" {
			return fail("%s: name is required", where)
		}
		if strings.TrimSpace(item.Period) == "" {
			return fail("%s %q: period is required", where, item.Name)
		}
		if err := item.Basis.Validate(); err != nil {
			return fail("%s %q basis: %v", where, item.Name, err)
		}
		if item.Basis.Sign() < 0 {
			return fail("%s %q basis cannot be negative", where, item.Name)
		}
		if err := item.Rate.Validate(); err != nil {
			return fail("%s %q rate: %v", where, item.Name, err)
		}
		if item.Rate.Sign() < 0 {
			return fail("%s %q rate cannot be negative", where, item.Name)
		}
		if _, ok := seenName[item.Name]; ok {
			return fail("%s: duplicate item %q", where, item.Name)
		}
		seenName[item.Name] = struct{}{}
		seenKind[item.Kind] = struct{}{}
		basis := item.Basis
		if item.Capped {
			if err := item.Cap.Validate(); err != nil {
				return fail("%s %q cap: %v", where, item.Name, err)
			}
			if item.Cap.Sign() < 0 {
				return fail("%s %q cap cannot be negative", where, item.Name)
			}
			if basis.Cmp(item.Cap) > 0 {
				basis = item.Cap
			}
		} else {
			unknown = append(unknown, "uncapped:"+item.Name)
		}
		amount, err := basis.Mul(item.Rate, req.AmountScale, req.Rounding)
		if err != nil {
			return fail("%s %q amount: %v", where, item.Name, err)
		}
		items[i] = item
		items[i].Amount = amount
	}
	for _, kind := range []BurdenKind{BurdenTax, BurdenBenefit, BurdenAllowance, BurdenOverhead} {
		if _, ok := seenKind[kind]; !ok {
			return fail("burden kind %q is omitted", kind)
		}
	}
	zero, err := values.NewDecimal("0", req.AmountScale, req.Rounding)
	if err != nil {
		return fail("zero: %v", err)
	}
	burden := zero
	for _, item := range items {
		if item.Amount.Scale() != req.AmountScale {
			return fail("item %q precision differs", item.Name)
		}
		burden, err = burden.Add(item.Amount)
		if err != nil {
			return fail("burden total: %v", err)
		}
	}
	wages, err := req.Wages.Quantize(req.AmountScale, req.Rounding)
	if err != nil {
		return fail("wages: %v", err)
	}
	cost, err := wages.Add(burden)
	if err != nil {
		return fail("total cost: %v", err)
	}
	out := BurdenCalculation{
		CalculationID: req.CalculationID,
		RuleID:        req.Rule.ID,
		RuleVersion:   req.Rule.Version,
		Currency:      req.Currency,
		Wages:         wages,
		Items:         items,
		TotalBurden:   burden,
		TotalCost:     cost,
		UnknownRanges: unknown,
		Complete:      len(unknown) == 0,
		AmountScale:   req.AmountScale,
		Rounding:      req.Rounding,
	}
	out.Digest = canonicalbytes.Digest(out.body())
	return out, nil
}

func (c BurdenCalculation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.labor.BurdenCalculation", 1).
		String("calculation_id", c.CalculationID).
		String("rule_id", c.RuleID).String("rule_version", c.RuleVersion).
		String("currency", c.Currency).Value("wages", c.Wages).
		Value("total_burden", c.TotalBurden).Value("total_cost", c.TotalCost).
		Int("amount_scale", int64(c.AmountScale)).String("rounding", c.Rounding.String()).
		Count("items", len(c.Items))
	for _, item := range c.Items {
		w.String("kind", string(item.Kind)).String("name", item.Name).
			Value("basis", item.Basis).Value("rate", item.Rate).
			Bool("capped", item.Capped)
		w.Optional("cap", item.Capped, item.Cap)
		w.String("period", item.Period).Value("amount", item.Amount)
	}
	for _, u := range c.UnknownRanges {
		w.String("unknown_range", u)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Validate recomputes every component amount, rechecks conservation and
// kind coverage, and verifies the digest binding.
func (c BurdenCalculation) Validate() error {
	fail := func(format string, args ...any) error {
		return errors.Join(ErrBurdenRejected, fmt.Errorf(format, args...))
	}
	if !c.Rounding.Valid() || c.Rounding == values.RoundingUnspecified {
		return fail("rounding mode must be declared")
	}
	seenKind := map[BurdenKind]struct{}{}
	seenName := map[string]struct{}{}
	burden, err := values.NewDecimal("0", c.AmountScale, c.Rounding)
	if err != nil {
		return fail("zero: %v", err)
	}
	for i, item := range c.Items {
		where := fmt.Sprintf("item %d", i)
		if !item.Kind.Valid() || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Period) == "" {
			return fail("%s is incomplete", where)
		}
		if _, ok := seenName[item.Name]; ok {
			return fail("%s: duplicate item %q", where, item.Name)
		}
		seenName[item.Name] = struct{}{}
		seenKind[item.Kind] = struct{}{}
		basis := item.Basis
		if item.Capped {
			if err := item.Cap.Validate(); err != nil {
				return fail("%s %q cap: %v", where, item.Name, err)
			}
			if basis.Cmp(item.Cap) > 0 {
				basis = item.Cap
			}
		}
		want, err := basis.Mul(item.Rate, c.AmountScale, c.Rounding)
		if err != nil {
			return fail("%s %q amount: %v", where, item.Name, err)
		}
		if !want.Equal(item.Amount) {
			return fail("%s %q amount %s, recomputed %s", where, item.Name, item.Amount, want)
		}
		burden, err = burden.Add(item.Amount)
		if err != nil {
			return fail("%s: %v", where, err)
		}
	}
	for _, kind := range []BurdenKind{BurdenTax, BurdenBenefit, BurdenAllowance, BurdenOverhead} {
		if _, ok := seenKind[kind]; !ok {
			return fail("burden kind %q is omitted", kind)
		}
	}
	if !burden.Equal(c.TotalBurden) {
		return fail("components sum %s, burden %s", burden, c.TotalBurden)
	}
	total, err := c.Wages.Add(c.TotalBurden)
	if err != nil {
		return fail("total cost: %v", err)
	}
	if !total.Equal(c.TotalCost) {
		return fail("wages plus burden %s, cost %s", total, c.TotalCost)
	}
	if c.Digest != canonicalbytes.Digest(c.body()) {
		return fail("canonical digest mismatch")
	}
	return nil
}
