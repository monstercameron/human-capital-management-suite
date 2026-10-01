package payinput

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServingContractID identifies the read-only payroll-input contract composed
// by the shipped application cell. It grants no authority and performs no
// durable writes.
const ServingContractID = "hcmnext.conformance.payroll-inputs/v1"

// ValidateServingContract exercises definitions, exact calculation, append-only
// correction planning, and complete snapshot freezing through the same pure
// symbols used by the payroll-input domain. The fixture is ephemeral; its
// purpose is to keep the semantic owner in the serving-binary dependency
// closure without introducing persistence or startup side effects.
func ValidateServingContract() error {
	effective, err := servingEffective("2026-01-01", "2027-01-01")
	if err != nil {
		return fmt.Errorf("payinput: serving effective interval: %w", err)
	}
	definition, err := NewDefinition(Definition{
		DefinitionID: "serving-earning", Code: "SERVING_EARNING", Kind: KindEarning,
		Currency: "USD", Taxability: map[JurisdictionClass]bool{JurisdictionFederal: true},
		CalculationBasis: BasisFlatAmount, Limits: DecimalLimits{Maximum: values.MustDecimal("100.00", 2, values.RoundingExactRequired)},
		AccountingCode: "SERVING_EARNING", OwnerRef: "payinput", Effective: effective,
		Version: "v1", State: StatePublished,
	})
	if err != nil {
		return fmt.Errorf("payinput: serving definition: %w", err)
	}
	assignment, err := NewWorkerAssignment(definition, WorkerAssignment{
		AssignmentID: "serving-assignment", WorkerRef: "worker-serving", Effective: effective,
		Amount:     values.MustDecimal("25.00", 2, values.RoundingExactRequired),
		Recurrence: RecurrencePerPayroll, RecurrenceRule: "PAY_PERIOD",
	})
	if err != nil {
		return fmt.Errorf("payinput: serving assignment: %w", err)
	}
	if err := ValidateAssignments([]WorkerAssignment{assignment}); err != nil {
		return fmt.Errorf("payinput: serving assignments: %w", err)
	}
	period, err := servingEffective("2026-01-01", "2026-02-01")
	if err != nil {
		return fmt.Errorf("payinput: serving period: %w", err)
	}
	calculation, err := Calculate(CalculationInput{
		Definition: definition, Assignment: assignment,
		Period: CalculationPeriod{ID: "serving-period", Currency: "USD", Effective: period},
	})
	if err != nil {
		return fmt.Errorf("payinput: serving calculation: %w", err)
	}
	if len(calculation.Lines) != 1 || calculation.Lines[0].Applied.String() != "25.00" ||
		!calculation.Lines[0].Taxable[JurisdictionFederal] || calculation.ReplayDigest == "" {
		return fmt.Errorf("payinput: serving calculation lost exact applied amount, taxability, or replay digest")
	}

	now := values.NewInstant(time.Date(2026, time.February, 28, 23, 0, 0, 0, time.UTC))
	run, err := NewPayRun(PayRun{RunID: "serving-run", PeriodID: "serving-period", State: PayRunFinalized, AssignmentDigests: []string{assignment.CanonicalDigest}})
	if err != nil {
		return fmt.Errorf("payinput: serving pay run: %w", err)
	}
	plan, err := PlanCorrection([]PayRun{run}, CorrectionRequest{
		CorrectionID: "serving-correction", AssignmentDigest: assignment.CanonicalDigest,
		DependentPeriods: []string{"serving-period"}, PriorAmount: values.MustDecimal("25.00", 2, values.RoundingExactRequired),
		CorrectedAmount: values.MustDecimal("27.50", 2, values.RoundingExactRequired), Reason: "serving correction", RequestedAt: now,
	}, now)
	if err != nil || len(plan.AffectedRuns) != 1 || plan.SupersedesDigest != assignment.CanonicalDigest {
		return fmt.Errorf("payinput: serving correction plan: %v", err)
	}

	watermark := values.NewInstant(time.Date(2026, time.February, 27, 23, 0, 0, 0, time.UTC))
	snapshotInputs := []SnapshotInput{
		{Source: SourceTime, Revision: "time-v1", Digest: "sha256:time", Present: true, Watermark: watermark},
		{Source: SourceBenefit, Revision: "benefit-v1", Digest: "sha256:benefit", Present: true, Watermark: watermark},
		{Source: SourceTax, Revision: "tax-v1", Digest: "sha256:tax", Present: true, Watermark: watermark},
		{Source: SourceGarnishment, Revision: "garnishment-v1", Digest: "sha256:garnishment", Present: true, Watermark: watermark},
	}
	snapshot, err := FreezeSnapshot("serving-snapshot", "serving-period", snapshotInputs, now)
	if err != nil {
		return fmt.Errorf("payinput: serving snapshot: %w", err)
	}
	if err := VerifySnapshot(snapshot, snapshotInputs, now); err != nil {
		return fmt.Errorf("payinput: serving snapshot verification: %w", err)
	}
	return nil
}

func servingEffective(start, end string) (values.EffectiveInterval, error) {
	from, err := values.ParseLocalDate(start)
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	to, err := values.ParseLocalDate(end)
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	return values.NewLocalDateInterval(from, to, values.CalendarRef{Ref: "calendar.us", Version: "v1"})
}
