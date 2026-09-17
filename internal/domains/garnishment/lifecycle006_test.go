package garnishment

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func garn006Active(t *testing.T) AttachmentOrder {
	t.Helper()
	order, err := IntakeOrder(orderInput())
	if err != nil {
		t.Fatal(err)
	}
	order, err = order.Verify(orderEvidence())
	if err != nil {
		t.Fatal(err)
	}
	order, err = order.Activate("clerk:payroll-2")
	if err != nil {
		t.Fatal(err)
	}
	return order
}

func garn006Notice(effective, noticed time.Time) DatedNotice {
	return DatedNotice{
		EffectiveAt: effective,
		NoticedAt:   noticed,
		Actor:       "clerk:payroll-2",
		Reason:      "court order satisfied",
	}
}

// TestTodo_GARN_006 is the PRIMARY contract: effective-dated amendments and
// releases preserve the prior order, deductions outside validity are
// prevented, and late notice produces a correction obligation.
func TestTodo_GARN_006(t *testing.T) {
	t.Run("timely release preserves lineage without correction", func(t *testing.T) {
		order := garn006Active(t)
		effective := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		released, receipt, err := ApplyRelease(order, garn006Notice(effective, effective))
		if err != nil {
			t.Fatalf("ApplyRelease: %v", err)
		}
		if released.Status != OrderReleased {
			t.Fatalf("released status = %v", released.Status)
		}
		if receipt.PriorDigest != order.Digest || receipt.NewDigest != released.Digest {
			t.Fatalf("receipt must chain prior and new digests: %+v", receipt)
		}
		if receipt.LateNotice || receipt.Correction != nil {
			t.Fatalf("timely notice must not raise a correction: %+v", receipt)
		}
		if receipt.EffectiveAt != effective {
			t.Fatalf("receipt must carry the effective date: %+v", receipt)
		}
		if err := receipt.Verify(); err != nil {
			t.Fatalf("sealed receipt Verify: %v", err)
		}
		if IsDeductible(released, effective.Add(-time.Hour)) {
			t.Fatal("released order must not remain deductible")
		}
	})

	t.Run("amendment versions and preserves lineage", func(t *testing.T) {
		order := garn006Active(t)
		effective := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		amended, receipt, err := ApplyAmendment(order, garn006Notice(effective, effective), AmendRequest{
			ClaimedBalance: values.MustDecimal("900.00", 2, values.RoundingHalfUp),
			Reason:         "arrears payment posted",
			AmendedBy:      "clerk:payroll-2",
		})
		if err != nil {
			t.Fatalf("ApplyAmendment: %v", err)
		}
		if amended.Version != 2 || amended.Status != OrderPendingVerification {
			t.Fatalf("amendment must be version 2 pending verification: %+v", amended)
		}
		if amended.DocumentDigest != order.DocumentDigest || receipt.PriorDigest != order.Digest {
			t.Fatalf("amendment must preserve lineage: %+v %+v", amended, receipt)
		}
		if err := receipt.Verify(); err != nil {
			t.Fatalf("sealed receipt Verify: %v", err)
		}
	})

	t.Run("late notice produces a correction obligation", func(t *testing.T) {
		order := garn006Active(t)
		effective := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		noticed := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
		_, receipt, err := ApplyRelease(order, garn006Notice(effective, noticed))
		if err != nil {
			t.Fatalf("ApplyRelease: %v", err)
		}
		if !receipt.LateNotice || receipt.Correction == nil {
			t.Fatalf("late notice must raise a correction: %+v", receipt)
		}
		if !receipt.Correction.PeriodFrom.Equal(effective) || !receipt.Correction.PeriodTo.Equal(noticed) {
			t.Fatalf("correction must cover the late window: %+v", receipt.Correction)
		}
		if err := receipt.Verify(); err != nil {
			t.Fatalf("sealed receipt Verify: %v", err)
		}
	})

	t.Run("deductions outside validity are prevented", func(t *testing.T) {
		order := garn006Active(t)
		if IsDeductible(order, time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)) {
			t.Fatal("order must not be deductible before its effective date")
		}
		if !IsDeductible(order, time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)) {
			t.Fatal("active order must be deductible inside its validity")
		}
		effective := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
		if _, _, err := ApplyRelease(order, garn006Notice(effective, effective)); !errors.Is(err, ErrLifecycleRejected) {
			t.Fatalf("effective date outside validity must be rejected, got %v", err)
		}
	})

	t.Run("termination ends withholding permanently", func(t *testing.T) {
		order := garn006Active(t)
		effective := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
		terminated, receipt, err := ApplyTermination(order, garn006Notice(effective, effective))
		if err != nil {
			t.Fatalf("ApplyTermination: %v", err)
		}
		if terminated.Status != OrderTerminated {
			t.Fatalf("terminated status = %v", terminated.Status)
		}
		if receipt.PriorDigest != order.Digest || receipt.NewDigest != terminated.Digest {
			t.Fatalf("receipt must chain prior and new digests: %+v", receipt)
		}
		if IsDeductible(terminated, effective) {
			t.Fatal("terminated order must not be deductible")
		}
		if err := receipt.Verify(); err != nil {
			t.Fatalf("sealed receipt Verify: %v", err)
		}
	})

	t.Run("rejection names field state version", func(t *testing.T) {
		order := garn006Active(t)
		released, _, err := ApplyRelease(order, garn006Notice(
			time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		))
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = ApplyRelease(released, garn006Notice(
			time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		))
		var rej *LifecycleRejection
		if !errors.As(err, &rej) {
			t.Fatalf("expected *LifecycleRejection, got %v", err)
		}
		if !errors.Is(err, ErrLifecycleRejected) {
			t.Fatalf("expected GARN_006_REJECTED, got %v", err)
		}
		if rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version: %+v", rej)
		}
	})
}

func TestTodo_GARN_006_Race(t *testing.T) {
	order := garn006Active(t)
	notice := garn006Notice(
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	)
	const racers = 16
	digests := make([]string, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, receipt, err := ApplyRelease(order, notice)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = receipt.CanonicalDigest
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: %v", i, err)
		}
		if digests[i] != digests[0] || digests[0] == "" {
			t.Fatalf("concurrent releases diverged: %v", digests)
		}
	}
}

func TestTodo_GARN_006_Mutation(t *testing.T) {
	order := garn006Active(t)
	_, receipt, err := ApplyRelease(order, garn006Notice(
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	))
	if err != nil {
		t.Fatal(err)
	}
	dropped := receipt
	dropped.Correction = nil
	if err := dropped.Verify(); !errors.Is(err, ErrLifecycleRejected) {
		t.Fatalf("dropped-correction mutant must fail Verify, got %v", err)
	}
	retimed := receipt
	retimed.EffectiveAt = retimed.EffectiveAt.Add(24 * time.Hour)
	if err := retimed.Verify(); !errors.Is(err, ErrLifecycleRejected) {
		t.Fatalf("retimed mutant must fail Verify, got %v", err)
	}
	unmarked := receipt
	unmarked.LateNotice = false
	if err := unmarked.Verify(); !errors.Is(err, ErrLifecycleRejected) {
		t.Fatalf("unmarked-late mutant must fail Verify, got %v", err)
	}
}
