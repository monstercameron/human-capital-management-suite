package merit

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServingContractID identifies the pure merit-cycle contract linked by the
// serving composition. It does not grant compensation authority or persist
// cycle state.
const ServingContractID = "hcmnext.conformance.merit/v1"

// ValidateServingContract exercises the merit path that a serving composition
// must retain: frozen population facts and guideline rules produce an exact,
// explainable recommendation, while manager-only calibration is rejected.
func ValidateServingContract() error {
	at := values.NewInstant(time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC))
	money, err := values.NewMoney("100.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		return fmt.Errorf("merit: serving base pay: %w", err)
	}
	watermark, err := values.NewSequenceRevision("merit-serving-population", 1)
	if err != nil {
		return fmt.Errorf("merit: serving population revision: %w", err)
	}
	decimal := func(text string) values.Decimal {
		return values.MustDecimal(text, 2, values.RoundingHalfEven)
	}
	population, err := NewPopulationSnapshot(PopulationSnapshot{
		SnapshotID: "population-serving", Revision: 1, Members: []PopulationMember{{
			ParticipantID: "worker-serving", ManagerID: "manager-serving", BasePay: money,
			PerformanceRating: decimal("4.00"), BandPosition: decimal("0.50"),
			SalaryRevisionRef: "salary-serving", PerformanceRef: "performance-serving",
			EffectiveAt: at, KnownAt: at,
		}}, Watermark: watermark, Frozen: true, FrozenAt: at,
	})
	if err != nil {
		return fmt.Errorf("merit: serving population: %w", err)
	}
	guidelines, err := NewGuidelineMatrix(GuidelineMatrix{
		MatrixID: "guidelines-serving", Version: "1",
		Rules: []GuidelineRule{{RatingMin: decimal("4.00"), RatingMax: decimal("4.00"), BandPositionMin: decimal("0.00"), BandPositionMax: decimal("1.00"), MinimumRate: decimal("0.05"), MaximumRate: decimal("0.10")}},
	})
	if err != nil {
		return fmt.Errorf("merit: serving guidelines: %w", err)
	}
	cycle, err := NewMeritCycle(MeritCycle{
		CycleID: "cycle-serving", Revision: 1, Population: population, Guidelines: guidelines,
		Budget: decimal("10.00"), Currency: "USD", State: CycleDraft, EffectiveAt: at, KnownAt: at,
	})
	if err != nil {
		return fmt.Errorf("merit: serving cycle: %w", err)
	}
	next, recommendation, err := cycle.Propose("worker-serving", decimal("0.05"), "manager-serving")
	if err != nil {
		return fmt.Errorf("merit: serving recommendation: %w", err)
	}
	if recommendation.State != RecommendationProposed || recommendation.Amount.String() != "5.00" {
		return fmt.Errorf("merit: serving recommendation lost exact proposal semantics: %+v", recommendation)
	}
	explanation, err := next.Explain()
	if err != nil {
		return fmt.Errorf("merit: serving explanation: %w", err)
	}
	if explanation.RecommendationCount != 1 || explanation.ConsumedBudget.String() != "5.00" {
		return fmt.Errorf("merit: serving explanation lost deterministic totals: %+v", explanation)
	}
	adjustment, err := NewCalibrationAdjustment(CalibrationAdjustment{
		ParticipantID: recommendation.ParticipantID, From: recommendation.Amount, To: decimal("6.00"),
		Reason: performance.CalibrationReasonEvidence, AdjusterID: "manager-serving",
	})
	if err != nil {
		return fmt.Errorf("merit: serving calibration fixture: %w", err)
	}
	if _, err := next.Calibrate(recommendation.ParticipantID, adjustment); err != ErrCalibrationSeparation {
		return fmt.Errorf("merit: serving manager calibration error = %v, want %v", err, ErrCalibrationSeparation)
	}
	return nil
}
