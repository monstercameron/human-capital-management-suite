package correction

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServingContractID identifies the payroll-correction lifecycle contract
// composed by the shipped application service. It grants no authority and
// performs no durable writes: the ledger below is in-memory only.
const ServingContractID = "hcmnext.conformance.payroll-correction/v1"

// ValidateServingContract walks the exact golden CONF-006 lifecycle the
// serving composition exposes: reproduce the original under its pinned rule
// version, compute the exact balanced delta, simulate with zero effect,
// approve, and append reconciled evidence that supersedes the original.
// Keeping this check in the semantic owner makes the binary dependency
// intentional while history stays immutable.
func ValidateServingContract() error {
	rounding, err := values.ParseRoundingMode("HALF_UP")
	if err != nil {
		return fmt.Errorf("correction: serving contract rounding: %w", err)
	}
	decimal := func(s string) (values.Decimal, error) {
		return values.NewDecimal(s, 2, rounding)
	}
	multA, err := decimal("1.00")
	if err != nil {
		return fmt.Errorf("correction: serving contract multiplier: %w", err)
	}
	multB, err := decimal("1.50")
	if err != nil {
		return fmt.Errorf("correction: serving contract multiplier: %w", err)
	}
	taxRate, err := decimal("0.20")
	if err != nil {
		return fmt.Errorf("correction: serving contract tax rate: %w", err)
	}
	regular, err := decimal("80")
	if err != nil {
		return fmt.Errorf("correction: serving contract hours: %w", err)
	}
	overtime, err := decimal("10")
	if err != nil {
		return fmt.Errorf("correction: serving contract hours: %w", err)
	}
	rate, err := decimal("25.00")
	if err != nil {
		return fmt.Errorf("correction: serving contract rate: %w", err)
	}
	registry := NewRegistry(
		RuleVersion{ID: "2026-A", OvertimeMultiplier: multA, TaxRate: taxRate},
		RuleVersion{ID: "2026-B", OvertimeMultiplier: multB, TaxRate: taxRate},
	)
	inputs := Inputs{EmployeeID: "serving-contract", RegularHours: regular, OvertimeHours: overtime, HourlyRate: rate}
	effective := "2026-01-15"
	originalKnown := time.Date(2026, time.January, 16, 9, 0, 0, 0, time.UTC)
	correctedKnown := time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)

	original, err := Calculate("run/serving", inputs, RuleVersion{ID: "2026-A", OvertimeMultiplier: multA, TaxRate: taxRate}, effective, originalKnown)
	if err != nil {
		return fmt.Errorf("correction: serving contract original: %w", err)
	}
	if got := original.Gross.String(); got != "2250.00" {
		return fmt.Errorf("correction: serving contract original gross = %s, want 2250.00", got)
	}
	if err := Reproduce(original, inputs, registry); err != nil {
		return fmt.Errorf("correction: serving contract reproduce: %w", err)
	}
	corrected, err := Calculate("run/serving/corrected", inputs, RuleVersion{ID: "2026-B", OvertimeMultiplier: multB, TaxRate: taxRate}, effective, correctedKnown)
	if err != nil {
		return fmt.Errorf("correction: serving contract corrected: %w", err)
	}
	if got := corrected.Gross.String(); got != "2375.00" {
		return fmt.Errorf("correction: serving contract corrected gross = %s, want 2375.00", got)
	}
	delta, err := ComputeDelta(original, corrected)
	if err != nil {
		return fmt.Errorf("correction: serving contract delta: %w", err)
	}
	if got := delta.DGross.String(); got != "125.00" {
		return fmt.Errorf("correction: serving contract delta gross = %s, want 125.00", got)
	}
	if got := delta.DTax.String(); got != "25.00" {
		return fmt.Errorf("correction: serving contract delta tax = %s, want 25.00", got)
	}
	if got := delta.DNet.String(); got != "100.00" {
		return fmt.Errorf("correction: serving contract delta net = %s, want 100.00", got)
	}
	simulated, err := Simulate(Propose("corr/serving", original, corrected, delta), original)
	if err != nil {
		return fmt.Errorf("correction: serving contract simulate: %w", err)
	}
	approved, err := Approve(simulated, "serving-contract")
	if err != nil {
		return fmt.Errorf("correction: serving contract approve: %w", err)
	}
	store := NewStore()
	if err := store.AppendRun(original); err != nil {
		return fmt.Errorf("correction: serving contract append run: %w", err)
	}
	evidence, err := store.AppendCorrection(approved)
	if err != nil {
		return fmt.Errorf("correction: serving contract append correction: %w", err)
	}
	if evidence.FilingRef == "" || evidence.PayDigest == "" {
		return fmt.Errorf("correction: serving contract evidence is missing pay/filing refs")
	}
	stored, ok := store.Original(original.RunID)
	if !ok || stored.Digest != original.Digest {
		return fmt.Errorf("correction: serving contract original did not survive supersession")
	}
	return nil
}
