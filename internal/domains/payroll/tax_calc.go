package payroll

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrTaxRejected is the TAX-001 typed refusal: a tax profile,
	// registration, wage basis, election or pinned rule version is missing, or
	// current rules are offered for a historical fixture. It carries no net
	// pay and no payable output.
	ErrTaxRejected = errors.New("TAX_001_REJECTED")
)

// TaxError reports the offending field without creating a payable side
// effect and without echoing governed amounts.
type TaxError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *TaxError) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", e.Code, e.Field, e.Reason)
}

// Is reports the typed TAX-001 boundary and any wrapped cause.
func (e *TaxError) Is(target error) bool {
	return target == ErrTaxRejected || target == e.Cause
}

// Unwrap returns the wrapped cause, if any.
func (e *TaxError) Unwrap() error { return e.Cause }

func taxRefusal(field, reason string, cause error) error {
	return &TaxError{Code: ErrTaxRejected.Error(), Field: field, Reason: reason, Cause: cause}
}

// moneyScale is the declared cent scale every TAX-001 money amount carries.
// rateScale is the declared scale of version-pinned tax rates.
const (
	taxMoneyScale  = 2
	taxRateScale   = 4
	taxWorkScale   = 4
	taxTracePrefix = ": "
)

// TaxRulePack is one version-pinned tax rule release. The version is the pin:
// a calculation binds exactly one pack version and a historical fixture is
// never repriced under a newer pack.
type TaxRulePack struct {
	Version      string
	Authority    string
	EmployeeRate values.Decimal
	EmployerRate values.Decimal
	WageBaseCap  values.Decimal
	CapApplies   bool
	Rounding     values.RoundingMode
}

// TaxInput is the complete governed question: the wages to price plus the
// profile, registration, election and wage-basis references that authorize
// the calculation. All arithmetic is decimal; floats never enter.
type TaxInput struct {
	Tenant           string
	WorkerRef        string
	PeriodStart      time.Time
	PeriodEnd        time.Time
	GrossWages       values.Decimal
	PreTaxDeductions values.Decimal
	TaxProfileRef    string
	RegistrationRef  string
	ElectionRef      string
	WageBasisRef     string
	RuleVersion      string
}

// TaxAccountingDelta is the balanced books movement one calculation emits:
// debit and credit carry the same total liability.
type TaxAccountingDelta struct {
	DebitAccount  string
	CreditAccount string
	Amount        values.Decimal
	Balanced      bool
}

// TaxResult is the exact TAX-001 answer with its rule trace and digest. It is
// a value: corrections chain new results and never mutate a prior one.
type TaxResult struct {
	TaxableWages     values.Decimal
	TaxBase          values.Decimal
	EmployeeTax      values.Decimal
	EmployerTax      values.Decimal
	TotalLiability   values.Decimal
	NewAccumulator   values.Decimal
	Authority        string
	RuleVersion      string
	RuleTrace        []string
	Delta            TaxAccountingDelta
	SupersedesDigest string
	Digest           string
}

// TaxCorrection appends one corrected result to history: the original stays
// immutable and the delta prices exactly what changed.
type TaxCorrection struct {
	Original      TaxResult
	Corrected     TaxResult
	DeltaEmployee values.Decimal
	DeltaEmployer values.Decimal
	DeltaTotal    values.Decimal
	Reason        string
	Authority     string
	CorrectedAt   time.Time
}

func (p TaxRulePack) validate() error {
	if strings.TrimSpace(p.Version) == "" {
		return taxRefusal("pack_version", "pinned rule version is required", nil)
	}
	if strings.TrimSpace(p.Authority) == "" {
		return taxRefusal("authority", "issuing authority is required", nil)
	}
	for name, rate := range map[string]values.Decimal{"employee_rate": p.EmployeeRate, "employer_rate": p.EmployerRate} {
		if err := rate.Validate(); err != nil {
			return taxRefusal(name, "rate is required", err)
		}
		if rate.Scale() != taxRateScale {
			return taxRefusal(name, fmt.Sprintf("rate must carry scale %d", taxRateScale), nil)
		}
		if rate.Sign() <= 0 {
			return taxRefusal(name, "rate must be positive", nil)
		}
	}
	if p.CapApplies {
		if err := p.WageBaseCap.Validate(); err != nil {
			return taxRefusal("wage_base_cap", "cap is required when it applies", err)
		}
		if p.WageBaseCap.Scale() != taxMoneyScale || p.WageBaseCap.Sign() <= 0 {
			return taxRefusal("wage_base_cap", "cap must be a positive cent amount", nil)
		}
	}
	if p.Rounding == values.RoundingUnspecified {
		return taxRefusal("rounding", "rounding mode is required", nil)
	}
	return nil
}

func (i TaxInput) validate() error {
	if strings.TrimSpace(i.Tenant) == "" {
		return taxRefusal("tenant", "tenant is required", nil)
	}
	if strings.TrimSpace(i.WorkerRef) == "" {
		return taxRefusal("worker_ref", "worker reference is required", nil)
	}
	if i.PeriodStart.IsZero() || i.PeriodEnd.IsZero() || !i.PeriodStart.Before(i.PeriodEnd) {
		return taxRefusal("period", "a valid pay period is required", nil)
	}
	for name, amount := range map[string]values.Decimal{"gross_wages": i.GrossWages, "pretax_deductions": i.PreTaxDeductions} {
		if err := amount.Validate(); err != nil {
			return taxRefusal(name, "amount is required", err)
		}
		if amount.Scale() != taxMoneyScale {
			return taxRefusal(name, fmt.Sprintf("amount must carry scale %d", taxMoneyScale), nil)
		}
		if amount.Sign() < 0 {
			return taxRefusal(name, "amount cannot be negative", nil)
		}
	}
	for name, ref := range map[string]string{
		"tax_profile_ref": i.TaxProfileRef, "registration_ref": i.RegistrationRef,
		"election_ref": i.ElectionRef, "wage_basis_ref": i.WageBasisRef,
		"rule_version": i.RuleVersion,
	} {
		if strings.TrimSpace(ref) == "" {
			return taxRefusal(name, "governed reference is required before net pay", nil)
		}
	}
	return nil
}

func taxDigest(input TaxInput, pack TaxRulePack, result TaxResult, prior values.Decimal) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		input.Tenant, input.WorkerRef,
		input.PeriodStart.UTC().Format(time.RFC3339Nano), input.PeriodEnd.UTC().Format(time.RFC3339Nano),
		input.GrossWages.String(), input.PreTaxDeductions.String(),
		input.TaxProfileRef, input.RegistrationRef, input.ElectionRef, input.WageBasisRef,
		pack.Version, pack.Authority, pack.EmployeeRate.String(), pack.EmployerRate.String(),
		result.TaxableWages.String(), result.TaxBase.String(),
		result.EmployeeTax.String(), result.EmployerTax.String(), result.TotalLiability.String(),
		prior.String(), result.NewAccumulator.String(),
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CalculateTax prices one worker's period taxes under exactly one pinned rule
// pack. Missing governed input, an unpinned pack version, or any inexact
// money state refuses with TAX_001_REJECTED before any payable output exists.
// priorAccumulator is the year-to-date liability the period accrues into.
func CalculateTax(input TaxInput, pack TaxRulePack, priorAccumulator values.Decimal) (TaxResult, error) {
	if err := input.validate(); err != nil {
		return TaxResult{}, err
	}
	if err := pack.validate(); err != nil {
		return TaxResult{}, err
	}
	if pack.Version != input.RuleVersion {
		return TaxResult{}, taxRefusal("rule_version", fmt.Sprintf("pack %q cannot price input pinned to %q", pack.Version, input.RuleVersion), nil)
	}
	if err := priorAccumulator.Validate(); err != nil {
		return TaxResult{}, taxRefusal("prior_accumulator", "accumulator is required", err)
	}
	if priorAccumulator.Scale() != taxMoneyScale || priorAccumulator.Sign() < 0 {
		return TaxResult{}, taxRefusal("prior_accumulator", "accumulator must be a non-negative cent amount", nil)
	}
	taxable, err := input.GrossWages.Sub(input.PreTaxDeductions)
	if err != nil {
		return TaxResult{}, taxRefusal("gross_wages", "taxable wages do not reconcile", err)
	}
	if taxable.Sign() < 0 {
		return TaxResult{}, taxRefusal("taxable_wages", "pretax deductions cannot exceed gross wages", nil)
	}
	base := taxable
	if pack.CapApplies && taxable.Cmp(pack.WageBaseCap) > 0 {
		base = pack.WageBaseCap
	}
	price := func(rate values.Decimal) (values.Decimal, error) {
		raw, err := base.Mul(rate, taxWorkScale, pack.Rounding)
		if err != nil {
			return values.Decimal{}, err
		}
		return raw.Quantize(taxMoneyScale, pack.Rounding)
	}
	employee, err := price(pack.EmployeeRate)
	if err != nil {
		return TaxResult{}, taxRefusal("employee_rate", "employee tax does not price", err)
	}
	employer, err := price(pack.EmployerRate)
	if err != nil {
		return TaxResult{}, taxRefusal("employer_rate", "employer tax does not price", err)
	}
	total, err := employee.Add(employer)
	if err != nil {
		return TaxResult{}, taxRefusal("total_liability", "liability does not reconcile", err)
	}
	accumulator, err := priorAccumulator.Add(total)
	if err != nil {
		return TaxResult{}, taxRefusal("prior_accumulator", "accumulator does not reconcile", err)
	}
	trace := func(format string, args ...any) string {
		return pack.Version + taxTracePrefix + fmt.Sprintf(format, args...)
	}
	result := TaxResult{
		TaxableWages: taxable, TaxBase: base,
		EmployeeTax: employee, EmployerTax: employer, TotalLiability: total,
		NewAccumulator: accumulator, Authority: pack.Authority, RuleVersion: pack.Version,
		RuleTrace: []string{
			trace("taxable wages %s less pretax %s", input.GrossWages, input.PreTaxDeductions),
			trace("base %s at employee %s and employer %s", base, pack.EmployeeRate, pack.EmployerRate),
			trace("authority %s accrues liability %s", pack.Authority, total),
		},
		Delta: TaxAccountingDelta{
			DebitAccount: "payroll-tax-expense", CreditAccount: "payroll-tax-liability",
			Amount: total, Balanced: true,
		},
	}
	result.Digest = taxDigest(input, pack, result, priorAccumulator)
	return result, nil
}

// AppendTaxCorrection chains one recomputed result behind its prior: the
// original is returned untouched, the corrected result must already name the
// prior digest, and the delta prices exactly what changed.
func AppendTaxCorrection(prior, corrected TaxResult, reason, authority string, at time.Time) (TaxCorrection, error) {
	if strings.TrimSpace(prior.Digest) == "" || strings.TrimSpace(corrected.Digest) == "" {
		return TaxCorrection{}, taxRefusal("supersedes_digest", "both results must carry digests", nil)
	}
	if corrected.SupersedesDigest != prior.Digest {
		return TaxCorrection{}, taxRefusal("supersedes_digest", "corrected result must chain the prior digest", nil)
	}
	if strings.TrimSpace(reason) == "" {
		return TaxCorrection{}, taxRefusal("reason", "correction reason is required", nil)
	}
	if strings.TrimSpace(authority) == "" {
		return TaxCorrection{}, taxRefusal("authority", "correction authority is required", nil)
	}
	if at.IsZero() {
		return TaxCorrection{}, taxRefusal("corrected_at", "correction time is required", nil)
	}
	deltaEmployee, err := corrected.EmployeeTax.Sub(prior.EmployeeTax)
	if err != nil {
		return TaxCorrection{}, taxRefusal("employee_tax", "correction delta does not reconcile", err)
	}
	deltaEmployer, err := corrected.EmployerTax.Sub(prior.EmployerTax)
	if err != nil {
		return TaxCorrection{}, taxRefusal("employer_tax", "correction delta does not reconcile", err)
	}
	deltaTotal, err := corrected.TotalLiability.Sub(prior.TotalLiability)
	if err != nil {
		return TaxCorrection{}, taxRefusal("total_liability", "correction delta does not reconcile", err)
	}
	return TaxCorrection{
		Original: prior, Corrected: corrected,
		DeltaEmployee: deltaEmployee, DeltaEmployer: deltaEmployer, DeltaTotal: deltaTotal,
		Reason: reason, Authority: authority, CorrectedAt: at.UTC(),
	}, nil
}
