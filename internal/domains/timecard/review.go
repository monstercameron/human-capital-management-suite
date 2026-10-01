// FTIME-004: review a timecard's paired intervals, breaks, schedule
// comparison, exceptions and correction history without rewriting punch
// history. A correction is new signed evidence appended to a history, never
// a rewrite of what came before.
package timecard

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/attendance"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

// ErrReviewRejected is the FTIME-004 seeded-defect sentinel.
var ErrReviewRejected = errors.New("FTIME_004_REJECTED")

// PairedInterval is one IN/OUT punch pair as review evidence. Complete is
// false when the OUT side is missing: Minutes is then exactly zero by
// construction, and the caller must treat that as an open, unresolved
// interval, never as a reported zero-hour session (see Line.Open).
type PairedInterval struct {
	InID, OutID string
	InAt, OutAt time.Time
	Minutes     int
	Complete    bool
}

// PairPunches pairs signed, accepted observations for one worker
// chronologically into IN/OUT intervals. Every punch must carry an explicit
// IN or OUT direction: pairing never guesses a missing side from a
// directionless event.
func PairPunches(punches []clock.TimeObservation) ([]PairedInterval, error) {
	if len(punches) == 0 {
		return nil, fmt.Errorf("%w: at least one punch is required", ErrReviewRejected)
	}
	ordered := append([]clock.TimeObservation(nil), punches...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].OccurredAt.Equal(ordered[j].OccurredAt) {
			return ordered[i].Digest < ordered[j].Digest
		}
		return ordered[i].OccurredAt.Before(ordered[j].OccurredAt)
	})
	var pairs []PairedInterval
	var open *clock.TimeObservation
	for i, p := range ordered {
		if !p.Accepted || p.Digest == "" {
			return nil, fmt.Errorf("%w: punch %d is not accepted evidence", ErrReviewRejected, i)
		}
		switch p.EventType {
		case clock.EventClockIn:
			if open != nil {
				// A second IN before an OUT leaves the first interval open;
				// it is reported as evidence, not discarded.
				pairs = append(pairs, PairedInterval{InID: open.Digest, InAt: open.OccurredAt, Complete: false})
			}
			o := p
			open = &o
		case clock.EventClockOut:
			if open == nil {
				return nil, fmt.Errorf("%w: punch %d is an OUT with no matching IN", ErrReviewRejected, i)
			}
			minutes := int((p.OccurredAt.Sub(open.OccurredAt) + time.Minute - 1) / time.Minute)
			pairs = append(pairs, PairedInterval{InID: open.Digest, OutID: p.Digest, InAt: open.OccurredAt, OutAt: p.OccurredAt, Minutes: minutes, Complete: true})
			open = nil
		default:
			// Meal/break events are reviewed separately from the paired
			// session intervals.
		}
	}
	if open != nil {
		pairs = append(pairs, PairedInterval{InID: open.Digest, InAt: open.OccurredAt, Complete: false})
	}
	return pairs, nil
}

// LineFromPairedInterval turns one paired interval into a WF-CAP-007
// timecard line. An incomplete pair becomes an open line with zero
// minutes, excluded from the total until its OUT side is resolved.
func LineFromPairedInterval(id string, p PairedInterval) Line {
	if !p.Complete {
		return Line{ID: id, Kind: LineSession, Minutes: 0, Open: true, SourceRefs: []string{p.InID}}
	}
	return Line{ID: id, Kind: LineSession, Minutes: p.Minutes, Open: false, SourceRefs: []string{p.InID, p.OutID}}
}

// ReviewView is the FTIME-004 read model: everything a worker or
// supervisor sees when reviewing a timecard. It is evidence, not a
// command: building one never writes state.
type ReviewView struct {
	WorkerID           string
	Source             string
	Timezone           string
	PairedIntervals    []PairedInterval
	Breaks             []attendance.Break
	ScheduleComparison attendance.Result
	Exceptions         []attendance.Finding
	Corrections        []clock.PunchCorrection
}

// BuildReview assembles the FTIME-004 review from pinned evidence. Every
// punch must belong to the worker under review: a punch recorded for a
// different worker is refused rather than silently mixed into the view.
func BuildReview(workerID, source, timezone string, punches []clock.TimeObservation, breaks []attendance.Break, comparison attendance.Result, corrections []clock.PunchCorrection) (ReviewView, error) {
	if strings.TrimSpace(workerID) == "" || strings.TrimSpace(source) == "" || strings.TrimSpace(timezone) == "" {
		return ReviewView{}, fmt.Errorf("%w: worker, source and timezone are required", ErrReviewRejected)
	}
	for i, p := range punches {
		if p.WorkerRef != "" && p.WorkerRef != workerID {
			return ReviewView{}, fmt.Errorf("%w: punch %d belongs to worker %q, not %q", ErrReviewRejected, i, p.WorkerRef, workerID)
		}
	}
	pairs, err := PairPunches(punches)
	if err != nil {
		return ReviewView{}, err
	}
	if err := comparison.Validate(); err != nil {
		return ReviewView{}, fmt.Errorf("%w: schedule comparison: %v", ErrReviewRejected, err)
	}
	return ReviewView{
		WorkerID: workerID, Source: source, Timezone: timezone,
		PairedIntervals:    pairs,
		Breaks:             append([]attendance.Break(nil), breaks...),
		ScheduleComparison: comparison,
		Exceptions:         append([]attendance.Finding(nil), comparison.Exceptions...),
		Corrections:        append([]clock.PunchCorrection(nil), corrections...),
	}, nil
}

// AppendCorrection appends one already-signed correction to a review's
// correction history. The prior corrections are never rewritten: the
// result is a new slice with the addition on the end, and the caller's
// history argument is left untouched.
func AppendCorrection(history []clock.PunchCorrection, next clock.PunchCorrection) ([]clock.PunchCorrection, error) {
	if err := clock.ExplainCorrection(next); err != nil {
		return nil, fmt.Errorf("%w: correction evidence: %v", ErrReviewRejected, err)
	}
	out := append([]clock.PunchCorrection(nil), history...)
	out = append(out, next)
	return out, nil
}

// exceptionKey identifies one finding for resolution tracking.
func exceptionKey(f attendance.Finding) string {
	return string(f.Kind) + "|" + f.ShiftID + "|" + f.PunchID
}

// OpenExceptions returns the review's findings that are not yet resolved.
// resolved names each finding already cleared through a separation-of-
// duties resolution (attendance.ResolveExceptionWork); every other finding
// still blocks approval, whether it is a schedule, break or overlap
// exception.
func OpenExceptions(view ReviewView, resolved map[string]bool) []attendance.Finding {
	if len(view.Exceptions) == 0 {
		return nil
	}
	out := make([]attendance.Finding, 0, len(view.Exceptions))
	for _, f := range view.Exceptions {
		if resolved[exceptionKey(f)] {
			continue
		}
		out = append(out, f)
	}
	return out
}
