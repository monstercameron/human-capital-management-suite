// BEN-007: calculate payroll benefit deductions.
//
// ComputeDeduction turns one election's plan rate, pay frequency, coverage
// span, arrears and retro corrections into exact employee/employer amounts
// with a trace. Retro corrections are separate deltas: totals are never
// overwritten. The function is kernel-pure.
package benefits

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrDeductionRejected is the BEN-007 sentinel for inexact inputs.
	ErrDeductionRejected = errors.New("BEN_007_REJECTED")
)

// DeductionRejection is the stable BEN-007 failure shape.
type DeductionRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *DeductionRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrDeductionRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the BEN_007_REJECTED sentinel to errors.Is.
func (r *DeductionRejection) Unwrap() error { return ErrDeductionRejected }

func deductionReject(field, state, reason string) error {
	return &DeductionRejection{Field: field, State: state, Version: ElectionVersion, Reason: reason}
}

// PayFrequency is the closed BEN-007 payroll frequency vocabulary.
type PayFrequency string

const (
	FrequencyWeekly      PayFrequency = "WEEKLY"
	FrequencyBiweekly    PayFrequency = "BI_WEEKLY"
	FrequencySemiMonthly PayFrequency = "SEMI_MONTHLY"
	FrequencyMonthly     PayFrequency = "MONTHLY"
)

// PeriodsPerYear returns the exact annual period count.
func (f PayFrequency) PeriodsPerYear() (int64, bool) {
	switch f {
	case FrequencyWeekly:
		return 52, true
	case FrequencyBiweekly:
		return 26, true
	case FrequencySemiMonthly:
		return 24, true
	case FrequencyMonthly:
		return 12, true
	default:
		return 0, false
	}
}

// DeductionInput is one payroll-period benefit deduction request. Rates
// are annual exact decimals; Arrears collects previously unbilled amounts;
// RetroCorrects names the prior period a correction adjusts.
type DeductionInput struct {
	Tenant         string
	WorkerRef      string
	ElectionDigest string
	RateVersion    string
	AnnualEmployee values.Decimal
	AnnualEmployer values.Decimal
	Frequency      PayFrequency
	PeriodStart    time.Time
	Arrears        values.Decimal
	RetroAmount    values.Decimal
	RetroCorrects  string
}

// DeductionResult is the exact period calculation with its trace. Retro
// corrections ride in RetroDelta; Base amounts are never rewritten.
type DeductionResult struct {
	EmployeeBase   values.Decimal
	EmployerBase   values.Decimal
	ArrearsApplied values.Decimal
	RetroDelta     values.Decimal
	EmployeeTotal  values.Decimal
	EmployerTotal  values.Decimal
	Trace          []string
	Digest         string
}

func (r DeductionResult) computedDigest(in DeductionInput) string {
	w := canonicalbytes.New("hcmnext.domains.benefits.DeductionResult", 1).
		String("tenant", in.Tenant).
		String("worker_ref", in.WorkerRef).
		String("election_digest", in.ElectionDigest).
		String("rate_version", in.RateVersion).
		String("frequency", string(in.Frequency)).
		String("period_start", in.PeriodStart.UTC().Format(time.RFC3339)).
		Value("employee_base", r.EmployeeBase).
		Value("employer_base", r.EmployerBase).
		Value("arrears", r.ArrearsApplied).
		Value("retro", r.RetroDelta).
		Value("employee_total", r.EmployeeTotal).
		Value("employer_total", r.EmployerTotal)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func nonNegative(d values.Decimal) bool {
	return d.Validate() == nil && d.Sign() >= 0
}

// ComputeDeduction calculates one period's exact benefit deduction.
func ComputeDeduction(in DeductionInput) (DeductionResult, error) {
	if strings.TrimSpace(in.Tenant) == "" {
		return DeductionResult{}, deductionReject("deduction.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(in.WorkerRef) == "" {
		return DeductionResult{}, deductionReject("deduction.worker_ref", "MISSING", "worker ref is required")
	}
	if strings.TrimSpace(in.ElectionDigest) == "" {
		return DeductionResult{}, deductionReject("deduction.election_digest", "MISSING", "election digest is required")
	}
	if strings.TrimSpace(in.RateVersion) == "" {
		return DeductionResult{}, deductionReject("deduction.rate_version", "STALE", "rate version is required")
	}
	periods, ok := in.Frequency.PeriodsPerYear()
	if !ok {
		return DeductionResult{}, deductionReject("deduction.frequency", "UNDECLARED", fmt.Sprintf("frequency %q is not declared", in.Frequency))
	}
	if in.PeriodStart.IsZero() {
		return DeductionResult{}, deductionReject("deduction.period_start", "MISSING", "period start is required")
	}
	if !nonNegative(in.AnnualEmployee) || !nonNegative(in.AnnualEmployer) {
		return DeductionResult{}, deductionReject("deduction.annual_rate", "INVALID", "annual rates must be valid non-negative decimals")
	}
	if !nonNegative(in.Arrears) {
		return DeductionResult{}, deductionReject("deduction.arrears", "INVALID", "arrears must be a valid non-negative decimal")
	}
	if err := in.RetroAmount.Validate(); err != nil {
		return DeductionResult{}, deductionReject("deduction.retro_amount", "INVALID", "retro amount is not a valid decimal")
	}
	if !in.RetroAmount.IsZero() && strings.TrimSpace(in.RetroCorrects) == "" {
		return DeductionResult{}, deductionReject("deduction.retro_corrects", "MISSING", "retro correction must name the corrected period")
	}
	divisor, err := values.NewDecimal(fmt.Sprintf("%d.00", periods), 2, values.RoundingHalfUp)
	if err != nil {
		return DeductionResult{}, deductionReject("deduction.frequency", "INVALID", "period count is not representable")
	}
	empBase, err := in.AnnualEmployee.Div(divisor, 2, values.RoundingHalfUp)
	if err != nil {
		return DeductionResult{}, deductionReject("deduction.employee_base", "INEXACT", fmt.Sprintf("employee base is not computable: %v", err))
	}
	erBase, err := in.AnnualEmployer.Div(divisor, 2, values.RoundingHalfUp)
	if err != nil {
		return DeductionResult{}, deductionReject("deduction.employer_base", "INEXACT", fmt.Sprintf("employer base is not computable: %v", err))
	}
	empTotal, err := empBase.Add(in.Arrears)
	if err != nil {
		return DeductionResult{}, deductionReject("deduction.employee_total", "INEXACT", fmt.Sprintf("employee total is not computable: %v", err))
	}
	empTotal, err = empTotal.Add(in.RetroAmount)
	if err != nil {
		return DeductionResult{}, deductionReject("deduction.employee_total", "INEXACT", fmt.Sprintf("retro delta is not applicable: %v", err))
	}
	if empTotal.Sign() < 0 {
		return DeductionResult{}, deductionReject("deduction.employee_total", "NEGATIVE", "period total must not go negative")
	}
	res := DeductionResult{
		EmployeeBase: empBase, EmployerBase: erBase,
		ArrearsApplied: in.Arrears, RetroDelta: in.RetroAmount,
		EmployeeTotal: empTotal, EmployerTotal: erBase,
		Trace: []string{
			fmt.Sprintf("annual employee %s / %d periods", in.AnnualEmployee.String(), periods),
			fmt.Sprintf("annual employer %s / %d periods", in.AnnualEmployer.String(), periods),
			fmt.Sprintf("arrears %s applied to employee share", in.Arrears.String()),
			fmt.Sprintf("retro delta %s corrects %s", in.RetroAmount.String(), in.RetroCorrects),
		},
	}
	res.Digest = res.computedDigest(in)
	return res, nil
}
