package crewshift

import (
	"fmt"
	"sort"
	"time"
)

// ExceptionKind is the closed vocabulary of findings Reconcile can report.
// It deliberately mirrors internal/domains/attendance's LATE, EARLY, MISSING
// and UNSCHEDULED kinds so the same word means the same thing across both
// packages; BREAK is added here for a shift's own planned break exceptions,
// which attendance's punch/schedule evaluation does not model.
type ExceptionKind string

const (
	ExceptionLate        ExceptionKind = "LATE"
	ExceptionEarly       ExceptionKind = "EARLY"
	ExceptionMissed      ExceptionKind = "MISSED"
	ExceptionUnscheduled ExceptionKind = "UNSCHEDULED"
	ExceptionBreak       ExceptionKind = "BREAK"
)

// Tolerance is the configured grace window either side of the shift before
// a punch is treated as late or early. It is data, never a constant.
type Tolerance struct {
	Late  time.Duration
	Early time.Duration
}

// PunchInterval is one observed attendance interval, already captured
// elsewhere (internal/domains/clock, internal/domains/attendance). Reconcile
// only reads these values; it never mutates or persists a punch, and a
// schedule edit produced through Publish or ApplyLifecycle never reaches
// back into this slice.
type PunchInterval struct {
	ID    string
	Start time.Time
	End   time.Time // zero when the worker has not yet clocked out
}

// Exception is one finding produced by comparing punches against a
// published shift.
type Exception struct {
	Kind     ExceptionKind
	PunchID  string
	Interval Interval
	Minutes  int64
	Reason   string
}

// ReconcileResult pins the shift revision it was computed against, so a
// caller can tell a stale comparison from a current one.
type ReconcileResult struct {
	ShiftID    string
	Revision   int64
	Exceptions []Exception
}

func exceptionMinutes(i Interval) int64 {
	d := i.End.Sub(i.Start)
	if d <= 0 {
		return 0
	}
	return int64((d + time.Minute - 1) / time.Minute)
}

// Reconcile compares observed punches against one published shift and
// returns every late, early, missed, unscheduled and break exception. The
// shift and its breaks are read-only inputs: this function returns findings,
// never a rewritten shift or a rewritten punch, so a reconciliation can never
// be mistaken for the record it is checking.
func Reconcile(shift Shift, tolerance Tolerance, punches []PunchInterval) (ReconcileResult, error) {
	if shift.Status != StatusPublished {
		return ReconcileResult{}, fmt.Errorf("%w: only a published shift has punches to reconcile", ErrReconcileRejected)
	}
	if tolerance.Late < 0 || tolerance.Early < 0 {
		return ReconcileResult{}, fmt.Errorf("%w: tolerance must not be negative", ErrReconcileRejected)
	}
	for _, p := range punches {
		if p.ID == "" || p.Start.IsZero() {
			return ReconcileResult{}, fmt.Errorf("%w: every punch requires an id and a start instant", ErrReconcileRejected)
		}
		if !p.End.IsZero() && !p.End.After(p.Start) {
			return ReconcileResult{}, fmt.Errorf("%w: punch %s end must be after its start", ErrReconcileRejected, p.ID)
		}
	}

	ordered := append([]PunchInterval(nil), punches...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Start.Before(ordered[j].Start) })

	res := ReconcileResult{ShiftID: shift.ID, Revision: shift.Revision}
	windowStart := shift.Work.Start.Add(-tolerance.Late)
	windowEnd := shift.Work.End.Add(tolerance.Early)

	var withinShift []PunchInterval
	for _, p := range ordered {
		end := p.effectiveEnd()
		// A punch is within the tolerated window when its observed span
		// touches [windowStart, windowEnd] at all; a punch entirely before
		// or entirely after that span never happened during this shift.
		inWindow := !end.Before(windowStart) && !p.Start.After(windowEnd)
		if !inWindow {
			punchWindow := Interval{Start: p.Start, End: end}
			if punchWindow.End.Equal(punchWindow.Start) {
				punchWindow.End = punchWindow.Start.Add(time.Minute)
			}
			res.Exceptions = append(res.Exceptions, Exception{
				Kind: ExceptionUnscheduled, PunchID: p.ID, Interval: punchWindow,
				Minutes: exceptionMinutes(punchWindow), Reason: "punch falls outside the scheduled shift window",
			})
			continue
		}
		withinShift = append(withinShift, p)
	}

	if len(withinShift) == 0 {
		res.Exceptions = append(res.Exceptions, Exception{
			Kind: ExceptionMissed, Interval: shift.Work, Minutes: exceptionMinutes(shift.Work),
			Reason: "no punch evidence for a published shift",
		})
	} else {
		first := withinShift[0]
		last := withinShift[len(withinShift)-1]
		if late := first.Start.Sub(shift.Work.Start); late > tolerance.Late {
			res.Exceptions = append(res.Exceptions, Exception{
				Kind: ExceptionLate, PunchID: first.ID,
				Interval: Interval{Start: shift.Work.Start.Add(tolerance.Late), End: first.Start},
				Minutes:  exceptionMinutes(Interval{Start: shift.Work.Start.Add(tolerance.Late), End: first.Start}),
				Reason:   "arrival is outside the late tolerance",
			})
		}
		lastEnd := last.End
		if lastEnd.IsZero() {
			lastEnd = last.Start
		}
		if early := shift.Work.End.Sub(lastEnd); early > tolerance.Early {
			res.Exceptions = append(res.Exceptions, Exception{
				Kind: ExceptionEarly, PunchID: last.ID,
				Interval: Interval{Start: lastEnd, End: shift.Work.End.Add(-tolerance.Early)},
				Minutes:  exceptionMinutes(Interval{Start: lastEnd, End: shift.Work.End.Add(-tolerance.Early)}),
				Reason:   "departure is outside the early tolerance",
			})
		}
	}

	for _, seg := range shift.Breaks.Segments {
		if seg.Paid {
			continue
		}
		covered := false
		for i := 1; i < len(withinShift); i++ {
			gap := Interval{Start: withinShift[i-1].effectiveEnd(), End: withinShift[i].Start}
			if !gap.Start.Before(seg.Interval.Start) && !gap.End.After(seg.Interval.End) {
				covered = true
				break
			}
		}
		if !covered {
			res.Exceptions = append(res.Exceptions, Exception{
				Kind: ExceptionBreak, Interval: seg.Interval, Minutes: exceptionMinutes(seg.Interval),
				Reason: "no punch gap covers the planned unpaid break",
			})
		}
	}

	sort.Slice(res.Exceptions, func(i, j int) bool {
		if res.Exceptions[i].Interval.Start.Equal(res.Exceptions[j].Interval.Start) {
			return res.Exceptions[i].Kind < res.Exceptions[j].Kind
		}
		return res.Exceptions[i].Interval.Start.Before(res.Exceptions[j].Interval.Start)
	})
	return res, nil
}

// effectiveEnd returns End when the worker has clocked out, otherwise Start:
// an open punch covers no gap for break detection.
func (p PunchInterval) effectiveEnd() time.Time {
	if p.End.IsZero() {
		return p.Start
	}
	return p.End
}
