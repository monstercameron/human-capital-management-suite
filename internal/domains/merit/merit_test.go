package merit

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func meritInstant() values.Instant {
	return values.NewInstant(time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC))
}
func meritDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	return values.MustDecimal(text, 2, values.RoundingHalfEven)
}

func meritPopulation(t *testing.T) PopulationSnapshot {
	t.Helper()
	money, err := values.NewMoney("100.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	wm, err := values.NewSequenceRevision("merit-population", 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPopulationSnapshot(PopulationSnapshot{SnapshotID: "population-1", Revision: 1, Frozen: true, FrozenAt: meritInstant(), Watermark: wm, Members: []PopulationMember{{ParticipantID: "worker-1", ManagerID: "manager-1", BasePay: money, PerformanceRating: meritDecimal(t, "4.00"), BandPosition: meritDecimal(t, "0.50"), SalaryRevisionRef: "salary-1", PerformanceRef: "performance-1", EffectiveAt: meritInstant(), KnownAt: meritInstant()}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func meritMatrix(t *testing.T) GuidelineMatrix {
	t.Helper()
	g, err := NewGuidelineMatrix(GuidelineMatrix{MatrixID: "matrix-1", Version: "1", Rules: []GuidelineRule{{RatingMin: meritDecimal(t, "4.00"), RatingMax: meritDecimal(t, "4.00"), BandPositionMin: meritDecimal(t, "0.00"), BandPositionMax: meritDecimal(t, "1.00"), MinimumRate: meritDecimal(t, "0.05"), MaximumRate: meritDecimal(t, "0.10")}}})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func meritCycle(t *testing.T) MeritCycle {
	t.Helper()
	p := meritPopulation(t)
	g := meritMatrix(t)
	c, err := NewMeritCycle(MeritCycle{CycleID: "cycle-1", Revision: 1, Population: p, Guidelines: g, Budget: meritDecimal(t, "20.00"), Currency: "USD", State: CycleDraft, EffectiveAt: meritInstant(), KnownAt: meritInstant()})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMeritCycleRequiresFrozenPopulationBudgetAndExplainableRecommendation(t *testing.T) {
	c := meritCycle(t)
	next, rec, err := c.Propose("worker-1", meritDecimal(t, "0.10"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Amount.String() != "10.00" || next.Recommendations[0].State != RecommendationProposed {
		t.Fatalf("recommendation = %#v", rec)
	}
	if _, _, err := c.Propose("worker-1", meritDecimal(t, "0.20"), "manager-1"); err == nil {
		t.Fatal("out-of-guideline proposal accepted")
	}
}

func TestTodo_MERIT_001_Property(t *testing.T) {
	c := meritCycle(t)
	next, _, err := c.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	used, err := next.ConsumedBudget()
	if err != nil {
		t.Fatal(err)
	}
	if used.String() != "5.00" {
		t.Fatalf("consumed = %s", used)
	}
}

func TestTodo_MERIT_001_Golden(t *testing.T) {
	c := meritCycle(t)
	next, _, err := c.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	e, err := next.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if e.RecommendationCount != 1 || e.ConsumedBudget.String() != "5.00" {
		t.Fatalf("explanation = %#v", e)
	}
}

func TestTodo_MERIT_001_Race(t *testing.T) {
	c := meritCycle(t)
	store := NewMemoryCycleStore()
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	const readers = 16
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := store.Current(c.CycleID)
			if err != nil {
				errs <- err
				return
			}
			if got.CanonicalDigest != c.CanonicalDigest {
				errs <- fmt.Errorf("current digest = %q, want %q", got.CanonicalDigest, c.CanonicalDigest)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestTodo_MERIT_001_Fault(t *testing.T) {
	c := meritCycle(t)
	c.Budget = values.Decimal{}
	if err := c.Validate(); !strings.Contains(err.Error(), "budget") {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestTodo_MERIT_001_Security(t *testing.T) {
	c := meritCycle(t)
	next, _, err := c.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	e, err := next.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.Digest, "worker-1") {
		t.Fatal("explanation leaked participant")
	}
}

func TestTodo_MERIT_001_Conformance(t *testing.T) {
	c := meritCycle(t)
	next, rec, err := c.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	bad, err := NewCalibrationAdjustment(CalibrationAdjustment{ParticipantID: rec.ParticipantID, From: rec.Amount, To: meritDecimal(t, "6.00"), Reason: performance.CalibrationReasonEvidence, AdjusterID: "manager-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.Calibrate("worker-1", bad); err != ErrCalibrationSeparation {
		t.Fatalf("manager-only calibration error = %v", err)
	}
}

func TestTodo_MERIT_001_Mutation(t *testing.T) {
	c := meritCycle(t)
	next, rec, err := c.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	adjustment, err := NewCalibrationAdjustment(CalibrationAdjustment{ParticipantID: rec.ParticipantID, From: rec.Amount, To: meritDecimal(t, "6.00"), Reason: performance.CalibrationReasonEvidence, AdjusterID: "calibrator-1"})
	if err != nil {
		t.Fatal(err)
	}
	calibrated, err := next.Calibrate("worker-1", adjustment)
	if err != nil {
		t.Fatal(err)
	}
	if calibrated.Recommendations[0].Amount.String() != "6.00" || next.Recommendations[0].Amount.String() != "5.00" {
		t.Fatal("calibration rewrote parent")
	}
}
