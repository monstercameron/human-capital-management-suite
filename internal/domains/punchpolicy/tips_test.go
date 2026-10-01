package punchpolicy

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mustDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewDecimal(%q): %v", text, err)
	}
	return d
}

func TestTipDeclaration_And_Adjust(t *testing.T) {
	now := time.Date(2026, 3, 2, 17, 5, 0, 0, time.UTC)
	rec, err := NewTipDeclaration(TipDeclaration{
		WorkerID:   "w-1",
		ShiftID:    "shift-1",
		DeclaredBy: "w-1",
		DeclaredAt: now,
		Amount:     mustDecimal(t, "42.50"),
	})
	if err != nil {
		t.Fatalf("NewTipDeclaration: %v", err)
	}
	if len(rec.Adjustments) != 0 {
		t.Fatalf("a fresh declaration should have no adjustments")
	}

	adjusted, err := Adjust(rec, TipAdjustment{
		Amount:     mustDecimal(t, "5.00"),
		Reason:     "manager correction after receipt review",
		AdjustedBy: "mgr-1",
		AdjustedAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if len(rec.Adjustments) != 0 {
		t.Fatalf("Adjust must not mutate the original record, got %d adjustments on the original", len(rec.Adjustments))
	}
	if len(adjusted.Adjustments) != 1 {
		t.Fatalf("adjusted record should have exactly one adjustment, got %d", len(adjusted.Adjustments))
	}

	total, err := adjusted.Total(2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("Total: %v", err)
	}
	want := mustDecimal(t, "47.50")
	if !total.Equal(want) {
		t.Fatalf("Total: got %s want %s", total, want)
	}

	// A second adjustment appends rather than replacing the first.
	twiceAdjusted, err := Adjust(adjusted, TipAdjustment{
		Amount:     mustDecimal(t, "-2.00"),
		Reason:     "duplicate line removed",
		AdjustedBy: "mgr-1",
		AdjustedAt: now.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if len(twiceAdjusted.Adjustments) != 2 {
		t.Fatalf("second adjustment should append, giving 2 total, got %d", len(twiceAdjusted.Adjustments))
	}
	if len(adjusted.Adjustments) != 1 {
		t.Fatalf("the intermediate record must be unaffected by the later Adjust call")
	}
}

func TestTipDeclaration_Validate(t *testing.T) {
	now := time.Date(2026, 3, 2, 17, 5, 0, 0, time.UTC)
	valid := TipDeclaration{WorkerID: "w-1", ShiftID: "s-1", DeclaredBy: "w-1", DeclaredAt: now, Amount: mustDecimal(t, "10.00")}
	if _, err := NewTipDeclaration(valid); err != nil {
		t.Fatalf("valid declaration should be accepted: %v", err)
	}

	missingWorker := valid
	missingWorker.WorkerID = ""
	if _, err := NewTipDeclaration(missingWorker); !errors.Is(err, ErrInvalidTipDeclaration) {
		t.Fatalf("missing worker id should fail with ErrInvalidTipDeclaration, got %v", err)
	}

	negative := valid
	negative.Amount = mustDecimal(t, "-1.00")
	if _, err := NewTipDeclaration(negative); !errors.Is(err, ErrInvalidTipDeclaration) {
		t.Fatalf("negative declared amount should fail with ErrInvalidTipDeclaration, got %v", err)
	}

	zeroTime := valid
	zeroTime.DeclaredAt = time.Time{}
	if _, err := NewTipDeclaration(zeroTime); !errors.Is(err, ErrInvalidTipDeclaration) {
		t.Fatalf("zero declared-at should fail with ErrInvalidTipDeclaration, got %v", err)
	}
}

func TestAdjust_InvalidAdjustment(t *testing.T) {
	now := time.Date(2026, 3, 2, 17, 5, 0, 0, time.UTC)
	rec, err := NewTipDeclaration(TipDeclaration{WorkerID: "w-1", ShiftID: "s-1", DeclaredBy: "w-1", DeclaredAt: now, Amount: mustDecimal(t, "10.00")})
	if err != nil {
		t.Fatalf("NewTipDeclaration: %v", err)
	}
	blankReason := TipAdjustment{Amount: mustDecimal(t, "1.00"), AdjustedBy: "mgr-1", AdjustedAt: now}
	if _, err := Adjust(rec, blankReason); !errors.Is(err, ErrInvalidTipDeclaration) {
		t.Fatalf("blank reason should fail with ErrInvalidTipDeclaration, got %v", err)
	}
}
