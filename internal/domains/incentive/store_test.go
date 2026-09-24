package incentive

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMemoryStorePreservesTenantScopedRevisionRules(t *testing.T) {
	plan := testMemoryPlan(t)
	store := NewMemoryStore()
	ctx := context.Background()
	if err := store.SavePlan(ctx, "tenant-a", plan); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(ctx, "tenant-a", plan); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate plan error = %v, want ErrStoreDuplicate", err)
	}
	if _, err := store.LoadPlan(ctx, "tenant-b", plan.PlanID, plan.Revision); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant plan load = %v, want ErrStoreNotFound", err)
	}
}

func TestMemoryStoreCoversObservationAndAwardLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	plan := testMemoryPlan(t)
	if err := store.SavePlan(ctx, "tenant", plan); err != nil {
		t.Fatal(err)
	}

	date, err := values.ParseLocalDate("2026-06-30")
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	observation, err := NewAttainmentObservation(AttainmentObservation{
		ObservationID: "observation", PlanID: plan.PlanID, PlanRevision: plan.Revision,
		MeasureID: "measure", WorkerRef: "worker", Value: testMemoryDecimal(t, "125.00"),
		AsOfEffective: date, AsKnownAt: known, SourceRef: "crm", Watermark: "wm-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendObservation(ctx, "tenant", observation, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendObservation(ctx, "tenant", observation, 1); !errors.Is(err, ErrStoreSequence) {
		t.Fatalf("duplicate observation = %v", err)
	}
	observations, err := store.ListObservations(ctx, "tenant", "worker", plan.PlanID)
	if err != nil || len(observations) != 1 || observations[0].Digest != observation.Digest {
		t.Fatalf("observations = %#v, err = %v", observations, err)
	}
	other, err := store.ListObservations(ctx, "other", "worker", plan.PlanID)
	if err != nil || len(other) != 0 {
		t.Fatalf("cross-tenant observations = %#v, err = %v", other, err)
	}

	award, err := NewAwardCalculation(AwardCalculation{
		CalculationID: "award", WorkerRef: "worker", PlanDigest: plan.Digest, PlanRevision: plan.Revision,
		PeriodRef: plan.PeriodRef, EligibilityRef: plan.EligibilityRef, FormulaRef: plan.FormulaRef,
		Inputs: []AwardInput{{Name: "measure", MeasureID: "measure", ObservationDigest: observation.Digest, Watermark: observation.Watermark, Attainment: observation.Value, Target: testMemoryDecimal(t, "100.00"), Weight: testMemoryDecimal(t, "100.00")}},
		Amount: testMemoryDecimal(t, "125.00"), Currency: plan.Currency, State: AwardCalculated, Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAward(ctx, "tenant", award); err != nil {
		t.Fatal(err)
	}
	approved, err := award.Approve("approval", storeApprovalVerifier{claim: award.approvalClaim(), ref: "approval"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAward(ctx, "tenant", approved); err != nil {
		t.Fatalf("save approved successor: %v", err)
	}
	loaded, err := store.LoadAward(ctx, "tenant", award.CalculationID, approved.Revision)
	if err != nil || loaded.State != AwardApproved || loaded.Digest != approved.Digest {
		t.Fatalf("loaded award = %#v, err = %v", loaded, err)
	}
	if _, err := store.LoadAward(ctx, "other", award.CalculationID, approved.Revision); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant award load = %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	finalized, err := approved.Finalize("finalization")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- store.SaveAward(ctx, "tenant", finalized) }()
	}
	wg.Wait()
	close(results)
	var successes, duplicates int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrStoreDuplicate):
			duplicates++
		default:
			t.Fatalf("concurrent save = %v", err)
		}
	}
	if successes != 1 || duplicates != 1 {
		t.Fatalf("successes=%d duplicates=%d", successes, duplicates)
	}
}

type storeApprovalVerifier struct {
	claim ApprovalClaim
	ref   string
}

func (v storeApprovalVerifier) VerifyApproval(got ApprovalClaim, ref string) error {
	if ref != v.ref || got.CalculationID != v.claim.CalculationID || got.WorkerRef != v.claim.WorkerRef || got.PeriodRef != v.claim.PeriodRef || got.Currency != v.claim.Currency || got.AwardDigest != v.claim.AwardDigest || got.AwardRevision != v.claim.AwardRevision || !got.Amount.Equal(v.claim.Amount) {
		return errors.New("receipt mismatch")
	}
	return nil
}

func TestMemoryStoreRefusalsAndErrorCodes(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	//lint:ignore SA1012 deliberate nil context: this test proves a nil context fails closed with ErrStoreInvalid. owner=compensation-incentive expires=2027-03-24
	if err := store.SavePlan(nil, "tenant", testMemoryPlan(t)); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("nil context = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.SavePlan(canceled, "tenant", testMemoryPlan(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context = %v", err)
	}
	if _, err := store.LoadPlan(ctx, "tenant", "", 0); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("invalid load plan = %v", err)
	}
	if err := store.AppendObservation(ctx, "tenant", AttainmentObservation{}, 0); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("invalid observation = %v", err)
	}
	if _, err := store.ListObservations(ctx, "tenant", "", ""); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("invalid list = %v", err)
	}
	if _, err := store.LoadAward(ctx, "tenant", "", 0); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("invalid load award = %v", err)
	}
	plan := testMemoryPlan(t)
	if err := store.SavePlan(ctx, "tenant", plan); err != nil {
		t.Fatal(err)
	}
	badAward := AwardCalculation{CalculationID: "bad", WorkerRef: "worker", PlanDigest: "wrong", PlanRevision: 1, PeriodRef: "FY26", EligibilityRef: "eligible", FormulaRef: "formula", Inputs: []AwardInput{{Name: "m", MeasureID: "measure", ObservationDigest: "obs", Watermark: "wm", Attainment: testMemoryDecimal(t, "1.00"), Target: testMemoryDecimal(t, "1.00"), Weight: testMemoryDecimal(t, "100.00")}}, Amount: testMemoryDecimal(t, "1.00"), Currency: "USD", State: AwardCalculated, Revision: 1}
	if err := store.SaveAward(ctx, "tenant", badAward); !errors.Is(err, ErrStorePlanMismatch) {
		t.Fatalf("plan mismatch = %v", err)
	}

	for code, sentinel := range map[StoreCode]error{
		StoreInvalidCode: ErrStoreInvalid, StoreNotFoundCode: ErrStoreNotFound,
		StoreDuplicateCode: ErrStoreDuplicate, StoreStaleCASCode: ErrStoreStaleCAS,
		StoreSequenceCode: ErrStoreSequence, StorePlanMismatchCode: ErrStorePlanMismatch,
		StoreStorageCode: ErrStoreStorage,
	} {
		err := storeError(code, "detail")
		if !errors.Is(err, sentinel) || err.Error() == "" {
			t.Fatalf("code %s: %v", code, err)
		}
	}
	if err := (&StoreError{}).Unwrap(); !errors.Is(err, ErrStoreRefused) {
		t.Fatalf("unknown code unwrap = %v", err)
	}
}

func testMemoryPlan(t *testing.T) IncentivePlanRevision {
	t.Helper()
	plan, err := NewIncentivePlanRevision(IncentivePlanRevision{
		PlanID: "plan", Revision: 1, Name: "Plan", Currency: "USD", PeriodRef: "FY26",
		EligibilityRef: "eligible", FormulaRef: "formula", Measures: []PlanMeasure{
			{ID: "measure", Name: "Measure", Kind: MeasureRevenue, Target: testMemoryDecimal(t, "100.00"), Weight: testMemoryDecimal(t, "100.00")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func testMemoryDecimal(t *testing.T, text string) (d values.Decimal) {
	t.Helper()
	var err error
	d, err = values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
