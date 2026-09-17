package filing

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var filing002At = time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC)

func filing002Package(t *testing.T) FilingPackage {
	t.Helper()
	pkg, err := BuildFilingPackage(filing001Input())
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func filing002Submitted(t *testing.T) FilingRecord {
	t.Helper()
	rec, err := OpenFiling(filing002Package(t), values.MustDecimal("5000.00", 2, values.RoundingHalfUp), filing002At)
	if err != nil {
		t.Fatal(err)
	}
	sender := &filing001Sender{state: DispatchAccepted}
	outcome, _, err := SubmitFiling(filing002Package(t), sender, map[string]DispatchOutcome{}, filing002At)
	if err != nil {
		t.Fatal(err)
	}
	rec, err = MarkSubmitted(rec, outcome, filing002At)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func filing002Ack() AckObservation {
	return AckObservation{
		Status:     "ACCEPTED",
		AmountDue:  values.MustDecimal("5000.00", 2, values.RoundingHalfUp),
		AmountPaid: values.MustDecimal("5000.00", 2, values.RoundingHalfUp),
		ObservedAt: filing002At.Add(48 * time.Hour), ReferenceHash: "sha256:gov-receipt-1",
	}
}

// TestTodo_FILING_002 is the PRIMARY contract: the lifecycle
// distinguishes prepared, submitted, accepted, rejected, paid, amended
// and repair-required; a rejected, partial, late or payment-mismatched
// filing is never complete; and an amendment references its original
// and balances to source facts without overwriting it.
func TestTodo_FILING_002(t *testing.T) {
	rec, err := ReconcileAcknowledgement(filing002Submitted(t), filing002Ack())
	if err != nil {
		t.Fatalf("ReconcileAcknowledgement: %v", err)
	}
	if rec.State != FilingPaid || rec.AmountPaid.String() != "5000.00" {
		t.Fatalf("matching acknowledgement must pay: %+v", rec)
	}

	t.Run("rejected is never complete", func(t *testing.T) {
		ack := filing002Ack()
		ack.Status = "REJECTED"
		rec, err := ReconcileAcknowledgement(filing002Submitted(t), ack)
		if err != nil {
			t.Fatal(err)
		}
		if rec.State != FilingRejected {
			t.Fatalf("rejection must record REJECTED: %+v", rec)
		}
	})

	t.Run("partial late and mismatched never complete", func(t *testing.T) {
		partial := filing002Ack()
		partial.Partial = true
		rec, err := ReconcileAcknowledgement(filing002Submitted(t), partial)
		if err != nil {
			t.Fatal(err)
		}
		if rec.State != FilingRepairRequired {
			t.Fatalf("partial must need repair: %+v", rec)
		}
		late := filing002Ack()
		late.Late = true
		rec, err = ReconcileAcknowledgement(filing002Submitted(t), late)
		if err != nil {
			t.Fatal(err)
		}
		if rec.State != FilingAccepted {
			t.Fatalf("late payment stays accepted, never paid: %+v", rec)
		}
		mismatch := filing002Ack()
		mismatch.AmountDue = values.MustDecimal("4999.99", 2, values.RoundingHalfUp)
		if _, err := ReconcileAcknowledgement(filing002Submitted(t), mismatch); !errors.Is(err, ErrLifecycleRejected) {
			t.Fatalf("payment mismatch must be FILING_002_REJECTED, got %v", err)
		}
	})

	t.Run("amendment references the original and preserves it", func(t *testing.T) {
		paid, err := ReconcileAcknowledgement(filing002Submitted(t), filing002Ack())
		if err != nil {
			t.Fatal(err)
		}
		in := filing001Input()
		in.ReportVersion = "v2026.01-amended"
		in.SubmissionHash = "sha256:submission-amended"
		in.IdempotencyKey = "idem-941-q1-amend"
		amendment, err := BuildFilingPackage(in)
		if err != nil {
			t.Fatal(err)
		}
		superseded, next, err := ApplyAmendment(paid, amendment, values.MustDecimal("5050.00", 2, values.RoundingHalfUp), filing002At.Add(72*time.Hour))
		if err != nil {
			t.Fatalf("ApplyAmendment: %v", err)
		}
		if superseded.State != FilingAmended {
			t.Fatalf("original must resolve AMENDED: %+v", superseded)
		}
		if next.AmendmentOf != paid.PackageDigest || next.State != FilingPrepared {
			t.Fatalf("amendment must reference the original: %+v", next)
		}
		if next.AmountDue.String() != "5050.00" {
			t.Fatalf("amendment must balance to source facts: %+v", next)
		}
		if _, _, err := ApplyAmendment(paid, filing002Package(t), values.MustDecimal("5050.00", 2, values.RoundingHalfUp), filing002At); !errors.Is(err, ErrLifecycleRejected) {
			t.Fatalf("identical amendment must be FILING_002_REJECTED")
		}
	})

	t.Run("unsubmitted filings take no acknowledgement", func(t *testing.T) {
		rec, err := OpenFiling(filing002Package(t), values.MustDecimal("5000.00", 2, values.RoundingHalfUp), filing002At)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ReconcileAcknowledgement(rec, filing002Ack()); !errors.Is(err, ErrLifecycleRejected) {
			t.Fatalf("prepared filing must refuse acknowledgement, got %v", err)
		}
	})
}

func TestTodo_FILING_002_Property(t *testing.T) {
	a, err := OpenFiling(filing002Package(t), values.MustDecimal("5000.00", 2, values.RoundingHalfUp), filing002At)
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenFiling(filing002Package(t), values.MustDecimal("5000.00", 2, values.RoundingHalfUp), filing002At)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("identical filings must open identically")
	}
	// Lifecycle order binds the seal: states advance forward only.
	submitted, err := MarkSubmitted(a, DispatchOutcome{State: DispatchAccepted, PackageDigest: a.PackageDigest}, filing002At)
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Digest == a.Digest || submitted.State != FilingSubmitted {
		t.Fatalf("submission must advance and move the seal: %+v", submitted)
	}
	if _, err := MarkSubmitted(submitted, DispatchOutcome{State: DispatchAccepted, PackageDigest: submitted.PackageDigest}, filing002At); !errors.Is(err, ErrLifecycleRejected) {
		t.Fatalf("double submission must be FILING_002_REJECTED")
	}
	ambiguous, err := OpenFiling(filing002Package(t), values.MustDecimal("5000.00", 2, values.RoundingHalfUp), filing002At)
	if err != nil {
		t.Fatal(err)
	}
	ambiguous, err = MarkSubmitted(ambiguous, DispatchOutcome{State: DispatchAmbiguous, PackageDigest: ambiguous.PackageDigest}, filing002At)
	if err != nil {
		t.Fatal(err)
	}
	if ambiguous.State != FilingRepairRequired {
		t.Fatalf("ambiguity must hold repair, never guess submitted: %+v", ambiguous)
	}
}
