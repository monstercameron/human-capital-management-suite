package crewshift

import (
	"errors"
	"testing"
	"time"
)

// TestTodo_FTIME_007_Reconcile is part of FTIME-007's primary proof: actual
// punches compared against a published shift surface late, early, missed,
// unscheduled and break exceptions, and the comparison never mutates the
// punches or the shift it was given.
func TestTodo_FTIME_007_Reconcile(t *testing.T) {
	shift := mustPublish(t, fixtureDraft("recon-1"))
	shift.Breaks = BreakPlan{Segments: []BreakSegment{
		{Kind: BreakMeal, Interval: Interval{Start: shift.Work.Start.Add(4 * time.Hour), End: shift.Work.Start.Add(4*time.Hour + 30*time.Minute)}},
	}}
	tol := Tolerance{Late: 5 * time.Minute, Early: 5 * time.Minute}

	t.Run("on time with the break covered", func(t *testing.T) {
		punches := []PunchInterval{
			{ID: "p1", Start: shift.Work.Start, End: shift.Work.Start.Add(4 * time.Hour)},
			{ID: "p2", Start: shift.Work.Start.Add(4*time.Hour + 30*time.Minute), End: shift.Work.End},
		}
		before := append([]PunchInterval(nil), punches...)
		res, err := Reconcile(shift, tol, punches)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if len(res.Exceptions) != 0 {
			t.Fatalf("exceptions for a compliant, break-covered shift = %+v, want none", res.Exceptions)
		}
		for i := range punches {
			if punches[i] != before[i] {
				t.Fatalf("Reconcile mutated the punch slice at %d: %+v != %+v", i, punches[i], before[i])
			}
		}
	})

	t.Run("late arrival, early departure, uncovered break", func(t *testing.T) {
		punches := []PunchInterval{
			{ID: "p3", Start: shift.Work.Start.Add(20 * time.Minute), End: shift.Work.End.Add(-20 * time.Minute)},
		}
		res, err := Reconcile(shift, tol, punches)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		kinds := map[ExceptionKind]bool{}
		for _, e := range res.Exceptions {
			kinds[e.Kind] = true
		}
		for _, want := range []ExceptionKind{ExceptionLate, ExceptionEarly, ExceptionBreak} {
			if !kinds[want] {
				t.Fatalf("exceptions = %+v, want %s among them", res.Exceptions, want)
			}
		}
	})

	t.Run("no punch at all is missed", func(t *testing.T) {
		res, err := Reconcile(shift, tol, nil)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		// The whole shift is missed, and its unpaid break is separately
		// unaccounted for since there is no punch gap to cover it.
		kinds := map[ExceptionKind]int{}
		for _, e := range res.Exceptions {
			kinds[e.Kind]++
		}
		if kinds[ExceptionMissed] != 1 || kinds[ExceptionBreak] != 1 || len(res.Exceptions) != 2 {
			t.Fatalf("exceptions for no punches = %+v, want exactly one MISSED and one BREAK", res.Exceptions)
		}
	})

	t.Run("a punch far outside the shift is unscheduled", func(t *testing.T) {
		stray := PunchInterval{ID: "stray", Start: shift.Work.Start.Add(-48 * time.Hour), End: shift.Work.Start.Add(-47 * time.Hour)}
		res, err := Reconcile(shift, tol, []PunchInterval{stray})
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		found := false
		for _, e := range res.Exceptions {
			if e.Kind == ExceptionUnscheduled && e.PunchID == "stray" {
				found = true
			}
		}
		if !found {
			t.Fatalf("exceptions = %+v, want an UNSCHEDULED finding for the stray punch", res.Exceptions)
		}
		// The stray punch leaves the shift itself with no covering punch,
		// which also reports as missed.
		missed := false
		for _, e := range res.Exceptions {
			if e.Kind == ExceptionMissed {
				missed = true
			}
		}
		if !missed {
			t.Fatalf("exceptions = %+v, want a MISSED finding once the only punch is unscheduled", res.Exceptions)
		}
	})

	t.Run("a draft shift cannot be reconciled", func(t *testing.T) {
		draft := fixtureDraft("recon-draft")
		if _, err := Reconcile(draft, tol, nil); !errors.Is(err, ErrReconcileRejected) {
			t.Fatalf("Reconcile a draft = %v, want ErrReconcileRejected", err)
		}
	})
}
