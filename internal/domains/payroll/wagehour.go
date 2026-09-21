package payroll

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal/stateparams"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrWageInput rejects wage evaluation without a complete governed
	// input: workweek, timezone, approved time, exemption, minimum, overtime
	// and break policy, rates and rounding are all required before any pay
	//able output exists.
	ErrWageInput = errors.New("payroll: invalid wage-hour input")
)

// Daily and weekly overtime thresholds for non-exempt time.
const (
	dailyOTThreshold     = "8"
	dailyDoubleThreshold = "12"
	weeklyOTThreshold    = "40"
	mealPremiumHours     = "1"
)

// ExemptionStatus is the closed WAGE-001 exemption vocabulary. UNDECLARED
// never defaults: it evaluates to UNKNOWN with zero pay.
type ExemptionStatus string

const (
	ExemptionExempt     ExemptionStatus = "EXEMPT"
	ExemptionNonexempt  ExemptionStatus = "NONEXEMPT"
	ExemptionUndeclared ExemptionStatus = "UNDECLARED"
)

// WageStatus is the closed result vocabulary.
type WageStatus string

const (
	WageOK             WageStatus = "OK"
	WageReviewRequired WageStatus = "REVIEW_REQUIRED"
	WageUnknown        WageStatus = "UNKNOWN"
)

// WageInput is the complete governed question: an approved workweek of daily
// hours plus the versioned rule parameters that price it. All arithmetic is
// decimal; floats never enter this contract.
type WageInput struct {
	Tenant             string
	WorkerRef          string
	WorkweekStart      time.Time
	Timezone           string
	DailyHours         []values.Decimal
	Approved           bool
	ApprovedBy         string
	Exemption          ExemptionStatus
	RegularRate        values.Decimal
	MinimumWage        values.Decimal
	OvertimeMultiple   values.Decimal
	DoubleTimeMultiple values.Decimal
	UnpaidBreakHours   values.Decimal
	BreaksSet          bool
	MissedMealPeriods  int
	Rounding           values.RoundingMode
	RuleVersion        string
	Overtime           *stateparams.OvertimeThreshold
}

// WageResult is the exact WAGE-001 answer with its rule trace.
type WageResult struct {
	Status            WageStatus
	RegularHours      values.Decimal
	OvertimeHours     values.Decimal
	DoubleTimeHours   values.Decimal
	PremiumHours      values.Decimal
	UnpaidBreakDeduct values.Decimal
	MinimumWageTopUp  values.Decimal
	GrossPay          values.Decimal
	RuleTrace         []string
	RuleVersion       string
	Digest            string
}

func (i WageInput) validate() error {
	if strings.TrimSpace(i.Tenant) == "" || strings.TrimSpace(i.WorkerRef) == "" {
		return fmt.Errorf("%w: tenant and worker are required", ErrWageInput)
	}
	if i.WorkweekStart.IsZero() || strings.TrimSpace(i.Timezone) == "" {
		return fmt.Errorf("%w: workweek start and timezone are required", ErrWageInput)
	}
	if len(i.DailyHours) == 0 {
		return fmt.Errorf("%w: approved daily time is required", ErrWageInput)
	}
	for d, hours := range i.DailyHours {
		if err := hours.Validate(); err != nil {
			return fmt.Errorf("%w: day %d: %v", ErrWageInput, d, err)
		}
		if hours.Sign() < 0 {
			return fmt.Errorf("%w: day %d is negative", ErrWageInput, d)
		}
	}
	if !i.Approved {
		return fmt.Errorf("%w: time must be approved before pricing", ErrWageInput)
	}
	if strings.TrimSpace(i.ApprovedBy) == "" {
		return fmt.Errorf("%w: approver is required", ErrWageInput)
	}
	if i.ApprovedBy == i.WorkerRef {
		return fmt.Errorf("%w: self-approved time is never payable", ErrWageInput)
	}
	switch i.Exemption {
	case ExemptionExempt, ExemptionNonexempt, ExemptionUndeclared:
	default:
		return fmt.Errorf("%w: exemption is not declared", ErrWageInput)
	}
	for name, amount := range map[string]values.Decimal{
		"regular rate": i.RegularRate, "minimum wage": i.MinimumWage,
		"overtime multiple": i.OvertimeMultiple, "double-time multiple": i.DoubleTimeMultiple,
	} {
		if err := amount.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrWageInput, name, err)
		}
		if amount.Sign() <= 0 {
			return fmt.Errorf("%w: %s must be positive", ErrWageInput, name)
		}
	}
	if i.BreaksSet {
		if err := i.UnpaidBreakHours.Validate(); err != nil {
			return fmt.Errorf("%w: break hours: %v", ErrWageInput, err)
		}
		if i.UnpaidBreakHours.Sign() < 0 {
			return fmt.Errorf("%w: break hours cannot be negative", ErrWageInput)
		}
	}
	if i.MissedMealPeriods < 0 {
		return fmt.Errorf("%w: missed meal periods cannot be negative", ErrWageInput)
	}
	if i.Rounding == values.RoundingUnspecified {
		return fmt.Errorf("%w: rounding mode is required", ErrWageInput)
	}
	if strings.TrimSpace(i.RuleVersion) == "" {
		return fmt.Errorf("%w: rule version is required", ErrWageInput)
	}
	return nil
}

func mustDecimal(text string) values.Decimal {
	return values.MustDecimal(text, 4, values.RoundingHalfUp)
}

func wageDigest(input WageInput, result WageResult) string {
	days := make([]string, 0, len(input.DailyHours))
	for _, day := range input.DailyHours {
		days = append(days, day.String())
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		input.Tenant, input.WorkerRef, input.WorkweekStart.UTC().Format(time.RFC3339Nano),
		input.Timezone, strings.Join(days, ","), string(input.Exemption),
		input.RegularRate.String(), input.MinimumWage.String(),
		result.RegularHours.String(), result.OvertimeHours.String(), result.DoubleTimeHours.String(),
		result.PremiumHours.String(), result.GrossPay.String(), string(result.Status), input.RuleVersion,
		overtimeThresholdIdentity(input.Overtime),
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func overtimeThresholdIdentity(threshold *stateparams.OvertimeThreshold) string {
	if threshold == nil {
		return "legacy"
	}
	daily := "none"
	if threshold.DailyThresholdHours != nil {
		daily = fmt.Sprint(*threshold.DailyThresholdHours)
	}
	return strings.Join([]string{
		threshold.ID,
		daily,
		fmt.Sprint(threshold.WeeklyThresholdHours),
		fmt.Sprint(threshold.ConsecutiveDayTrigger),
		threshold.Multiplier.String(),
	}, ":")
}

func resolveOvertimeParameters(input WageInput) (WageInput, error) {
	threshold := input.Overtime
	if threshold == nil {
		return input, nil
	}
	if strings.TrimSpace(threshold.ID) == "" || threshold.WeeklyThresholdHours <= 0 ||
		(threshold.DailyThresholdHours != nil && *threshold.DailyThresholdHours <= 0) {
		return WageInput{}, fmt.Errorf("%w: overtime threshold is incomplete", ErrWageInput)
	}
	if err := threshold.Multiplier.Validate(); err != nil || threshold.Multiplier.Sign() <= 0 {
		return WageInput{}, fmt.Errorf("%w: overtime threshold multiplier is invalid", ErrWageInput)
	}
	if err := input.OvertimeMultiple.Validate(); err == nil && input.OvertimeMultiple.Sign() > 0 && input.OvertimeMultiple.Cmp(threshold.Multiplier) != 0 {
		return WageInput{}, fmt.Errorf("%w: overtime multiple contradicts the resolved threshold", ErrWageInput)
	}
	if err := input.DoubleTimeMultiple.Validate(); err == nil && input.DoubleTimeMultiple.Sign() > 0 {
		return WageInput{}, fmt.Errorf("%w: double-time multiple is not declared by the resolved threshold", ErrWageInput)
	}
	input.OvertimeMultiple = threshold.Multiplier
	// The resolved threshold has no double-time bucket. A valid multiplier is
	// still needed by the common exact-decimal pricing path, where its hours
	// remain zero by construction.
	input.DoubleTimeMultiple = threshold.Multiplier
	return input, nil
}

// EvaluateWages prices one approved workweek deterministically. Missing or
// unapproved input returns an error — never payable output. An undeclared
// exemption returns UNKNOWN with zero pay. Minimum-wage floors and meal
// premiums price exactly but flag REVIEW_REQUIRED.
func EvaluateWages(input WageInput) (WageResult, error) {
	var err error
	input, err = resolveOvertimeParameters(input)
	if err != nil {
		return WageResult{}, err
	}
	if err := input.validate(); err != nil {
		return WageResult{}, err
	}
	zero := mustDecimal("0")
	quantize := func(d values.Decimal) (values.Decimal, error) { return d.Quantize(2, input.Rounding) }
	hourScale := input.DailyHours[0].Scale()
	for d, hours := range input.DailyHours {
		if hours.Scale() != hourScale {
			return WageResult{}, fmt.Errorf("%w: day %d scale %d does not match workweek scale %d", ErrWageInput, d, hours.Scale(), hourScale)
		}
	}
	if input.BreaksSet && input.UnpaidBreakHours.Scale() != hourScale {
		return WageResult{}, fmt.Errorf("%w: break hours must share the workweek scale", ErrWageInput)
	}
	constAt := func(text string) (values.Decimal, error) {
		return mustDecimal(text).Quantize(hourScale, values.RoundingExactRequired)
	}
	if input.Exemption == ExemptionUndeclared {
		regular, _ := quantize(zero)
		unknown := WageResult{
			Status: WageUnknown, RegularHours: regular, OvertimeHours: regular,
			DoubleTimeHours: regular, PremiumHours: regular, MinimumWageTopUp: regular,
			UnpaidBreakDeduct: regular, GrossPay: regular, RuleVersion: input.RuleVersion,
			RuleTrace: []string{input.RuleVersion + ": exemption undeclared; no payable output"},
		}
		unknown.Digest = wageDigest(input, unknown)
		return unknown, nil
	}
	eight, err := constAt(dailyOTThreshold)
	if err != nil {
		return WageResult{}, err
	}
	twelve, err := constAt(dailyDoubleThreshold)
	if err != nil {
		return WageResult{}, err
	}
	forty, err := constAt(weeklyOTThreshold)
	if err != nil {
		return WageResult{}, err
	}
	one, err := constAt(mealPremiumHours)
	if err != nil {
		return WageResult{}, err
	}
	zero, err = constAt("0")
	if err != nil {
		return WageResult{}, err
	}
	var trace []string
	record := func(format string, args ...any) {
		trace = append(trace, input.RuleVersion+": "+fmt.Sprintf(format, args...))
	}

	regular, overtime, double := zero, zero, zero
	for d, hours := range input.DailyHours {
		dayRegular, dayOT, dayDouble := hours, zero, zero
		if input.Exemption == ExemptionNonexempt {
			if input.Overtime != nil {
				if input.Overtime.ConsecutiveDayTrigger && d >= 5 && hours.Sign() > 0 {
					dayRegular = zero
					dayOT = hours
				} else if input.Overtime.DailyThresholdHours != nil {
					daily, thresholdErr := constAt(fmt.Sprint(*input.Overtime.DailyThresholdHours))
					if thresholdErr != nil {
						return WageResult{}, thresholdErr
					}
					if hours.Cmp(daily) > 0 {
						dayOT, err = hours.Sub(daily)
						if err != nil {
							return WageResult{}, err
						}
						dayRegular = daily
					}
				}
			} else if hours.Cmp(twelve) > 0 {
				var err error
				dayDouble, err = hours.Sub(twelve)
				if err != nil {
					return WageResult{}, err
				}
				dayRegular = twelve
			}
			if input.Overtime == nil && dayRegular.Cmp(eight) > 0 {
				var err error
				excess, err := dayRegular.Sub(eight)
				if err != nil {
					return WageResult{}, err
				}
				dayOT = excess
				dayRegular = eight
			}
		}
		if !dayDouble.IsZero() {
			record("day %d: %s past twelve prices at double time", d, dayDouble)
		} else if !dayOT.IsZero() {
			record("day %d: %s prices at overtime", d, dayOT)
		}
		var err error
		if regular, err = regular.Add(dayRegular); err != nil {
			return WageResult{}, err
		}
		if overtime, err = overtime.Add(dayOT); err != nil {
			return WageResult{}, err
		}
		if double, err = double.Add(dayDouble); err != nil {
			return WageResult{}, err
		}
	}
	// Weekly overtime prices non-exempt hours past forty that daily rules
	// have not already moved to premium rates.
	if input.Exemption == ExemptionNonexempt {
		weeklyThreshold := forty
		if input.Overtime != nil {
			weeklyThreshold, err = constAt(fmt.Sprint(input.Overtime.WeeklyThresholdHours))
			if err != nil {
				return WageResult{}, err
			}
		}
		classified, err := regular.Add(overtime)
		if err != nil {
			return WageResult{}, err
		}
		total, err := classified.Add(double)
		if err != nil {
			return WageResult{}, err
		}
		if total.Cmp(weeklyThreshold) > 0 {
			excess, err := total.Sub(weeklyThreshold)
			if err != nil {
				return WageResult{}, err
			}
			already, err := overtime.Add(double)
			if err != nil {
				return WageResult{}, err
			}
			additional, err := excess.Sub(already)
			if err != nil {
				return WageResult{}, err
			}
			if additional.Sign() > 0 {
				move := additional
				if move.Cmp(regular) > 0 {
					move = regular
				}
				regular, err = regular.Sub(move)
				if err != nil {
					return WageResult{}, err
				}
				overtime, err = overtime.Add(move)
				if err != nil {
					return WageResult{}, err
				}
				record("weekly: %s past %s prices at overtime", move, weeklyThreshold)
			}
		}
	} else {
		record("exempt: straight time only")
	}
	// Unpaid breaks reduce regular hours first, then overtime.
	breaks := zero
	if input.BreaksSet {
		breaks = input.UnpaidBreakHours
		remaining := breaks
		if remaining.Cmp(regular) > 0 {
			remaining = regular
		}
		var err error
		regular, err = regular.Sub(remaining)
		if err != nil {
			return WageResult{}, err
		}
		leftover, err := breaks.Sub(remaining)
		if err != nil {
			return WageResult{}, err
		}
		if leftover.Cmp(overtime) > 0 {
			return WageResult{}, fmt.Errorf("%w: breaks exceed payable hours", ErrWageInput)
		}
		overtime, err = overtime.Sub(leftover)
		if err != nil {
			return WageResult{}, err
		}
		record("breaks: %s unpaid hours deducted", breaks)
	}
	// Missed meal periods accrue one premium hour each at the regular rate.
	premium := zero
	for m := 0; m < input.MissedMealPeriods; m++ {
		var err error
		premium, err = premium.Add(one)
		if err != nil {
			return WageResult{}, err
		}
	}
	if !premium.IsZero() {
		record("meals: %d missed periods accrue %s premium hours", input.MissedMealPeriods, premium)
	}
	otRate, err := input.RegularRate.Mul(input.OvertimeMultiple, 4, input.Rounding)
	if err != nil {
		return WageResult{}, err
	}
	dtRate, err := input.RegularRate.Mul(input.DoubleTimeMultiple, 4, input.Rounding)
	if err != nil {
		return WageResult{}, err
	}
	price := func(hours, rate values.Decimal) (values.Decimal, error) {
		return hours.Mul(rate, 4, input.Rounding)
	}
	regularPay, err := price(regular, input.RegularRate)
	if err != nil {
		return WageResult{}, err
	}
	overtimePay, err := price(overtime, otRate)
	if err != nil {
		return WageResult{}, err
	}
	doublePay, err := price(double, dtRate)
	if err != nil {
		return WageResult{}, err
	}
	premiumPay, err := price(premium, input.RegularRate)
	if err != nil {
		return WageResult{}, err
	}
	gross, err := regularPay.Add(overtimePay)
	if err != nil {
		return WageResult{}, err
	}
	if gross, err = gross.Add(doublePay); err != nil {
		return WageResult{}, err
	}
	if gross, err = gross.Add(premiumPay); err != nil {
		return WageResult{}, err
	}
	// The jurisdiction floor applies to every payable hour.
	payable, err := regular.Add(overtime)
	if err != nil {
		return WageResult{}, err
	}
	if payable, err = payable.Add(double); err != nil {
		return WageResult{}, err
	}
	floor, err := payable.Mul(input.MinimumWage, 4, input.Rounding)
	if err != nil {
		return WageResult{}, err
	}
	topUp := zero
	status := WageOK
	if gross.Cmp(floor) < 0 {
		topUp, err = floor.Sub(gross)
		if err != nil {
			return WageResult{}, err
		}
		gross = floor
		status = WageReviewRequired
		record("minimum wage: floor %s applied with top-up %s", floor, topUp)
	}
	if !premium.IsZero() {
		status = WageReviewRequired
	}
	result := WageResult{
		Status: status, RuleVersion: input.RuleVersion, RuleTrace: trace,
		UnpaidBreakDeduct: breaks, MinimumWageTopUp: topUp,
	}
	for _, field := range []struct {
		value *values.Decimal
		raw   values.Decimal
	}{
		{&result.RegularHours, regular}, {&result.OvertimeHours, overtime},
		{&result.DoubleTimeHours, double}, {&result.PremiumHours, premium},
		{&result.MinimumWageTopUp, topUp}, {&result.UnpaidBreakDeduct, breaks},
	} {
		*field.value, err = field.raw.Quantize(2, input.Rounding)
		if err != nil {
			return WageResult{}, err
		}
	}
	if result.GrossPay, err = gross.Quantize(2, input.Rounding); err != nil {
		return WageResult{}, err
	}
	result.Digest = wageDigest(input, result)
	return result, nil
}
