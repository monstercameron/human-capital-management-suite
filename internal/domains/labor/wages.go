package labor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrWageRejected is the LABOR-003 refusal boundary. An unknown
// jurisdiction, rate or rule blocks authoritative cost; it is never
// defaulted to zero.
var ErrWageRejected = errors.New("LABOR_003_REJECTED")

// WageRequest asks for exact wages, overtime and differentials under one
// bound rule version. RuleID and RuleVersion must name req.Rule exactly;
// otherwise the request references a stale or unknown rule.
type WageRequest struct {
	CalculationID     string
	Rule              LaborRule
	RuleID            string
	RuleVersion       string
	Jurisdiction      string
	JurisdictionRules RuleRef
	RegularHours      values.Decimal
	OvertimeHours     values.Decimal
	DifferentialHours values.Decimal
	AmountScale       int32
	Rounding          values.RoundingMode
}

// WageCalculation is the immutable component trace of one wage calculation.
type WageCalculation struct {
	CalculationID     string
	RuleID            string
	RuleVersion       string
	Jurisdiction      string
	JurisdictionRules RuleRef
	RegularHours      values.Decimal
	OvertimeHours     values.Decimal
	DifferentialHours values.Decimal
	RegularPay        values.Decimal
	OvertimePay       values.Decimal
	DifferentialPay   values.Decimal
	TotalPay          values.Decimal
	AmountScale       int32
	Rounding          values.RoundingMode
	Digest            string
}

// CalculateWages returns the exact component trace for one governed input.
// Exact inputs yield exact amounts; anything unknown is rejected.
func CalculateWages(req WageRequest) (WageCalculation, error) {
	fail := func(format string, args ...any) (WageCalculation, error) {
		return WageCalculation{}, errors.Join(ErrWageRejected, fmt.Errorf(format, args...))
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
	if strings.TrimSpace(req.Jurisdiction) == "" {
		return fail("jurisdiction is required")
	}
	if err := req.JurisdictionRules.Validate(); err != nil {
		return fail("jurisdiction rules: %v", err)
	}
	hours := []struct {
		name  string
		value values.Decimal
	}{
		{"regular_hours", req.RegularHours},
		{"overtime_hours", req.OvertimeHours},
		{"differential_hours", req.DifferentialHours},
	}
	scale := req.RegularHours.Scale()
	for _, h := range hours {
		if err := h.value.Validate(); err != nil {
			return fail("%s: %v", h.name, err)
		}
		if h.value.Scale() != scale {
			return fail("%s precision differs", h.name)
		}
		if h.value.Sign() < 0 {
			return fail("%s cannot be negative", h.name)
		}
	}
	if req.AmountScale < 0 || req.AmountScale > values.MaxScale {
		return fail("amount scale %d is out of range", req.AmountScale)
	}
	if !req.Rounding.Valid() || req.Rounding == values.RoundingUnspecified {
		return fail("rounding mode must be declared")
	}
	regular, err := req.RegularHours.Mul(req.Rule.BaseRate, req.AmountScale, req.Rounding)
	if err != nil {
		return fail("regular pay: %v", err)
	}
	otBase, err := req.OvertimeHours.Mul(req.Rule.BaseRate, req.AmountScale, req.Rounding)
	if err != nil {
		return fail("overtime base: %v", err)
	}
	overtime, err := otBase.Mul(req.Rule.OvertimeMultiplier, req.AmountScale, req.Rounding)
	if err != nil {
		return fail("overtime pay: %v", err)
	}
	differential, err := req.DifferentialHours.Mul(req.Rule.DifferentialRate, req.AmountScale, req.Rounding)
	if err != nil {
		return fail("differential pay: %v", err)
	}
	total, err := regular.Add(overtime)
	if err != nil {
		return fail("total: %v", err)
	}
	total, err = total.Add(differential)
	if err != nil {
		return fail("total: %v", err)
	}
	out := WageCalculation{
		CalculationID:     req.CalculationID,
		RuleID:            req.Rule.ID,
		RuleVersion:       req.Rule.Version,
		Jurisdiction:      req.Jurisdiction,
		JurisdictionRules: req.JurisdictionRules,
		RegularHours:      req.RegularHours,
		OvertimeHours:     req.OvertimeHours,
		DifferentialHours: req.DifferentialHours,
		RegularPay:        regular,
		OvertimePay:       overtime,
		DifferentialPay:   differential,
		TotalPay:          total,
		AmountScale:       req.AmountScale,
		Rounding:          req.Rounding,
	}
	out.Digest = canonicalbytes.Digest(out.body())
	return out, nil
}

func (c WageCalculation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.labor.WageCalculation", 1).
		String("calculation_id", c.CalculationID).
		String("rule_id", c.RuleID).String("rule_version", c.RuleVersion).
		String("jurisdiction", c.Jurisdiction).Value("jurisdiction_rules", c.JurisdictionRules).
		Value("regular_hours", c.RegularHours).Value("overtime_hours", c.OvertimeHours).
		Value("differential_hours", c.DifferentialHours).
		Value("regular_pay", c.RegularPay).Value("overtime_pay", c.OvertimePay).
		Value("differential_pay", c.DifferentialPay).Value("total_pay", c.TotalPay).
		String("rounding", c.Rounding.String())
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Validate rechecks component arithmetic and digest binding.
func (c WageCalculation) Validate() error {
	fail := func(format string, args ...any) error {
		return errors.Join(ErrWageRejected, fmt.Errorf(format, args...))
	}
	sum, err := c.RegularPay.Add(c.OvertimePay)
	if err != nil {
		return fail("components: %v", err)
	}
	sum, err = sum.Add(c.DifferentialPay)
	if err != nil {
		return fail("components: %v", err)
	}
	if !sum.Equal(c.TotalPay) {
		return fail("components do not sum to total")
	}
	if c.Digest != canonicalbytes.Digest(c.body()) {
		return fail("canonical digest mismatch")
	}
	return nil
}

// Explain renders a reference-only trace: jurisdictions, rules and digests,
// never raw worker identity (the calculation carries none).
func (c WageCalculation) Explain() string {
	return "labor wage calculation " + c.CalculationID +
		" jurisdiction=" + c.Jurisdiction +
		" rule=" + c.RuleID + "/" + c.RuleVersion +
		" regular=" + c.RegularPay.String() +
		" overtime=" + c.OvertimePay.String() +
		" differential=" + c.DifferentialPay.String() +
		" total=" + c.TotalPay.String() +
		" digest=" + c.Digest
}
