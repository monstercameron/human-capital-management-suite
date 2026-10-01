// travel.go implements WTIME-016: classify a travel segment separately
// from mileage. The classification (and the California employer-controlled
// travel flag) come from TravelPolicy, which is data; this package hosts
// no state-specific constant. Paid travel counts toward hours and
// overtime; mileage is never merged into the minutes total — it is a
// distinct MileageExpense record with its own ledger.
package worktimerules

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidTravelSegment reports a segment or policy that cannot be
// classified.
var ErrInvalidTravelSegment = errors.New("worktimerules: invalid travel segment")

// TravelClass is the classification a segment resolves to under
// 29 CFR 785.35-41 and, where the policy flag is set, California's
// employer-controlled-travel rule.
type TravelClass string

const (
	TravelCommute             TravelClass = "COMMUTE"    // ordinary home-to-work, unpaid
	TravelInterSite           TravelClass = "INTER_SITE" // between worksites during the workday, paid
	TravelSpecialOneDay       TravelClass = "SPECIAL_ONE_DAY_ASSIGNMENT"
	TravelOvernightInHours    TravelClass = "OVERNIGHT_WITHIN_NORMAL_HOURS"
	TravelWorkWhileTravelling TravelClass = "WORK_WHILE_TRAVELLING"
	TravelEmployerControlled  TravelClass = "EMPLOYER_CONTROLLED" // California rule: mandatory transport to a distant site
)

// TravelSegment is one candidate travel interval with the facts needed to
// classify it. NormalWorkStart/NormalWorkEnd are the worker's ordinary
// daily hours (minutes since local midnight) used to decide whether
// overnight travel falls inside them.
type TravelSegment struct {
	WorkerID                  string
	Start, End                time.Time
	IsFirstOrLastOfDay        bool // true for the ordinary commute leg
	IsInterSite               bool // travel between two worksites during the day
	IsSpecialAssignment       bool // a one-day assignment to a distant, non-regular site
	IsOvernight               bool
	PerformedWork             bool // e.g. answering calls or driving under employer direction while travelling
	EmployerRequiredTransport bool // California: employer mandates the transport mode/route
	NormalWorkStartMinute     int
	NormalWorkEndMinute       int
}

func (s TravelSegment) validate() error {
	if s.WorkerID == "" {
		return fmt.Errorf("%w: worker_id is required", ErrInvalidTravelSegment)
	}
	if s.Start.IsZero() || s.End.IsZero() || !s.End.After(s.Start) {
		return fmt.Errorf("%w: interval must be non-empty", ErrInvalidTravelSegment)
	}
	return nil
}

func (s TravelSegment) minutes() int { return int(s.End.Sub(s.Start).Minutes()) }

func (s TravelSegment) withinNormalHours() bool {
	startMin, endMin := minuteOfDay(s.Start), minuteOfDay(s.End)
	return startMin >= s.NormalWorkStartMinute && endMin <= s.NormalWorkEndMinute
}

// TravelPolicy carries the one state-specific flag this todo names. Every
// other distinction is drawn directly from the segment's own facts, per
// 29 CFR 785.35-41.
type TravelPolicy struct {
	CaliforniaEmployerControlledTravel bool
}

// TravelClassification is the paid/unpaid answer for one segment. Minutes
// counts toward hours and overtime only when Paid is true; mileage never
// appears here.
type TravelClassification struct {
	Class             TravelClass
	Paid              bool
	CountsTowardHours bool
	Minutes           int
}

// ClassifyTravel resolves one segment's class and pay treatment. Ordinary
// commute is unpaid and excluded from hours unless the California
// employer-controlled-travel flag applies and the segment's transport was
// employer-required, in which case it is reclassified as
// TravelEmployerControlled and paid.
func ClassifyTravel(segment TravelSegment, policy TravelPolicy) (TravelClassification, error) {
	if err := segment.validate(); err != nil {
		return TravelClassification{}, err
	}
	minutes := segment.minutes()

	if segment.IsFirstOrLastOfDay && !segment.IsSpecialAssignment {
		if policy.CaliforniaEmployerControlledTravel && segment.EmployerRequiredTransport {
			return TravelClassification{Class: TravelEmployerControlled, Paid: true, CountsTowardHours: true, Minutes: minutes}, nil
		}
		return TravelClassification{Class: TravelCommute, Paid: false, CountsTowardHours: false, Minutes: minutes}, nil
	}

	if segment.IsSpecialAssignment {
		return TravelClassification{Class: TravelSpecialOneDay, Paid: true, CountsTowardHours: true, Minutes: minutes}, nil
	}

	if segment.IsInterSite {
		return TravelClassification{Class: TravelInterSite, Paid: true, CountsTowardHours: true, Minutes: minutes}, nil
	}

	if segment.PerformedWork {
		return TravelClassification{Class: TravelWorkWhileTravelling, Paid: true, CountsTowardHours: true, Minutes: minutes}, nil
	}

	if segment.IsOvernight {
		if segment.withinNormalHours() {
			return TravelClassification{Class: TravelOvernightInHours, Paid: true, CountsTowardHours: true, Minutes: minutes}, nil
		}
		return TravelClassification{Class: TravelOvernightInHours, Paid: false, CountsTowardHours: false, Minutes: minutes}, nil
	}

	// Default: unclassified travel outside the worker's normal hours and
	// not otherwise flagged is treated as unpaid commute-equivalent travel
	// rather than guessed as compensable.
	return TravelClassification{Class: TravelCommute, Paid: false, CountsTowardHours: false, Minutes: minutes}, nil
}

// MileageExpense is a distance-based reimbursement record, kept separate
// from hours by construction: it has no minutes field and nothing in this
// package folds it into any Windows total. MilesHundredths is distance in
// hundredths of a mile (integer) so that a payroll-side rate computation
// can use exact decimals rather than float64; this package does not itself
// price the expense.
type MileageExpense struct {
	WorkerID                 string
	SegmentStart, SegmentEnd time.Time
	MilesHundredths          int64
}
