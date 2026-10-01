package punchpolicy

import (
	"errors"
	"testing"
	"time"
)

func TestTodo_TCLOCK_009_GraceAndSupervisorEvidence(t *testing.T) {
	policy := fixturePolicy()
	shift := fixtureShift(true)

	within, err := EvaluateGrace(policy, Interval{
		Start: shift.Interval.Start.Add(-5 * time.Minute),
		End:   shift.Interval.End.Add(5 * time.Minute),
	}, shift)
	if err != nil {
		t.Fatalf("EvaluateGrace within: %v", err)
	}
	if within.Status != GraceWithinWindow || within.PolicyID != policy.ID || within.PolicyVersion != policy.Version {
		t.Fatalf("grace result = %+v, want policy-pinned within-window result", within)
	}

	outside, err := EvaluateGrace(policy, Interval{
		Start: shift.Interval.Start.Add(-6 * time.Minute),
		End:   shift.Interval.End.Add(6 * time.Minute),
	}, shift)
	if err != nil {
		t.Fatalf("EvaluateGrace outside: %v", err)
	}
	if outside.Status != GraceEarlyAndLate || outside.EarlyBy != 6*time.Minute || outside.LateBy != 6*time.Minute {
		t.Fatalf("grace result = %+v, want both boundary violations", outside)
	}

	decision, err := CheckClockIn(policy, shift.Interval.Start.Add(-11*time.Minute), shift, Override{Role: "SUPERVISOR"})
	if err != nil {
		t.Fatalf("CheckClockIn without approver: %v", err)
	}
	if decision.Status != RequiresOverride {
		t.Fatalf("override without approver identity must not clear lockout, got %s", decision.Status)
	}

	approved, err := CheckClockIn(policy, shift.Interval.Start.Add(-11*time.Minute), shift, Override{Role: "SUPERVISOR", ApprovedBy: "manager-1"})
	if err != nil {
		t.Fatalf("CheckClockIn approved: %v", err)
	}
	if approved.Status != OverriddenAllowed {
		t.Fatalf("supervisor override with approver identity should clear lockout, got %s", approved.Status)
	}
}

func TestTodo_TCLOCK_009_GraceRejectsMalformedInputs(t *testing.T) {
	policy := fixturePolicy()
	shift := fixtureShift(true)
	if _, err := EvaluateGrace(policy, Interval{}, shift); !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("empty raw interval should fail with ErrInvalidInterval, got %v", err)
	}
	if _, err := EvaluateGrace(policy, Interval{Start: shift.Interval.Start, End: shift.Interval.End}, Shift{ID: shift.ID}); !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("empty shift interval should fail with ErrInvalidInterval, got %v", err)
	}
}
