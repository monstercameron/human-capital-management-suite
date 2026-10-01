package paygl

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServingContractID identifies the pure payroll-to-GL contract composed by
// the shipped application service. It grants no authority and performs no
// durable writes.
const ServingContractID = "hcmnext.conformance.payroll-gl/v1"

// ValidateServingContract exercises the exact pure path that the serving
// composition exposes. Keeping this check in the semantic owner makes the
// binary dependency intentional without giving startup any persistence or
// publication authority.
func ValidateServingContract() error {
	start, err := values.NewLocalDate(2026, time.January, 1)
	if err != nil {
		return fmt.Errorf("paygl: serving contract start date: %w", err)
	}
	end, err := values.NewLocalDate(2027, time.January, 1)
	if err != nil {
		return fmt.Errorf("paygl: serving contract end date: %w", err)
	}
	effective, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "calendar.us", Version: "v1"})
	if err != nil {
		return fmt.Errorf("paygl: serving contract effective interval: %w", err)
	}
	run, err := payroll.NewPayrollRun(
		"serving-paygl-run", "monthly",
		payroll.PeriodRef{ID: "serving-period", Version: "v1", Digest: "sha256:serving-period"},
		payroll.PopulationBindingRef{DefinitionID: "serving-population", RevisionVersion: "v1", Digest: "sha256:serving-population"},
		"sha256:serving-inputs",
	)
	if err != nil {
		return fmt.Errorf("paygl: serving contract payroll run: %w", err)
	}
	run, err = run.Calculate("sha256:serving-calculation")
	if err != nil {
		return fmt.Errorf("paygl: serving contract calculate run: %w", err)
	}
	rule, err := NewAccountingRule(AccountingRule{
		ID: "serving-salary", Version: "v1", Effective: effective,
		ComponentKind: ComponentEarning, ComponentCode: "SALARY",
		Dimension:    labor.Dimension{Kind: labor.DimensionCostCenter, Value: "serving-cc", Version: "v1"},
		DebitAccount: "6000", CreditAccount: "2100", Currency: "USD",
		Rounding: values.RoundingExactRequired, SuspensePolicy: SuspensePolicyReject,
	})
	if err != nil {
		return fmt.Errorf("paygl: serving contract accounting rule: %w", err)
	}
	amount, err := values.NewDecimal("1.00", 2, values.RoundingExactRequired)
	if err != nil {
		return fmt.Errorf("paygl: serving contract amount: %w", err)
	}
	derivation, err := DerivePostings(run, []AccountingRule{rule}, []CalculatedLine{{
		ID: "serving-line", ComponentKind: ComponentEarning, ComponentCode: "SALARY",
		Amount: amount, Currency: "USD", Dimension: rule.Dimension,
	}})
	if err != nil {
		return fmt.Errorf("paygl: serving contract derivation: %w", err)
	}
	if err := derivation.Validate(); err != nil {
		return fmt.Errorf("paygl: serving contract validation: %w", err)
	}
	if len(derivation.Postings) != 2 || !derivation.Balanced || !derivation.TotalDebits.Equal(derivation.TotalCredits) {
		return fmt.Errorf("paygl: serving contract derivation is not a balanced debit/credit pair")
	}
	return nil
}
