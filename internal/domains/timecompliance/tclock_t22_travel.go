package timecompliance

import (
	"fmt"
	"time"
)

// TravelKind is a session-transfer classification, not a second time record.
type TravelKind string

const (
	TravelHomeCommute        TravelKind = "HOME_TO_WORK_COMMUTE"
	TravelBetweenSites       TravelKind = "BETWEEN_SITES"
	TravelSpecialAssignment  TravelKind = "SPECIAL_ONE_DAY_ASSIGNMENT"
	TravelOvernightNormal    TravelKind = "OVERNIGHT_WITHIN_NORMAL_HOURS"
	TravelWorkPerformed      TravelKind = "WORK_WHILE_TRAVELLING"
	TravelEmployerControlled TravelKind = "EMPLOYER_CONTROLLED_TRAVEL"
)

// TravelSegment is a transfer in the worker's session workflow. TravelKind
// is intentionally derived from the factual flags, so a caller cannot forge
// paid treatment by supplying a desired classification.
type TravelSegment struct {
	TenantID                  string
	WorkerID                  string
	Interval                  Interval
	HomeCommute               bool
	BetweenSites              bool
	SpecialOneDayAssignment   bool
	Overnight                 bool
	WorkPerformed             bool
	EmployerRequiredTransport bool
	NormalWorkStartMinute     int
	NormalWorkEndMinute       int
}

type TravelPolicy struct {
	Jurisdiction                       string
	CaliforniaEmployerControlledTravel bool
}

type TravelClassification struct {
	Kind              TravelKind
	Paid              bool
	CountsTowardHours bool
	Minutes           int
}

func (s TravelSegment) validate() error {
	if s.TenantID == "" || s.WorkerID == "" {
		return ErrMissingScope
	}
	if err := s.Interval.validate(); err != nil {
		return err
	}
	if s.NormalWorkStartMinute < 0 || s.NormalWorkStartMinute >= 24*60 || s.NormalWorkEndMinute < 0 || s.NormalWorkEndMinute > 24*60 {
		return fmt.Errorf("%w: invalid normal-hours window", ErrInvalidEvidence)
	}
	return nil
}

func normalMinuteWindowOverlap(interval Interval, startMinute, endMinute int) bool {
	if endMinute <= startMinute {
		return false
	}
	for day := calendarDay(interval.Start).Add(-24 * time.Hour); !day.After(interval.End); day = day.Add(24 * time.Hour) {
		windowStart := day.Add(time.Duration(startMinute) * time.Minute)
		windowEnd := day.Add(time.Duration(endMinute) * time.Minute)
		if interval.Start.Before(windowEnd) && interval.End.After(windowStart) {
			return true
		}
	}
	return false
}

// ClassifyTravel separates compensable travel minutes from mileage. Required
// California transport is paid; ordinary commute and mileage remain outside
// worked hours. All paid classifications carry the exact transfer duration.
func ClassifyTravel(segment TravelSegment, policy TravelPolicy) (TravelClassification, error) {
	if err := segment.validate(); err != nil {
		return TravelClassification{}, err
	}
	minutes := segment.Interval.minutes()
	paid := func(kind TravelKind) TravelClassification {
		return TravelClassification{Kind: kind, Paid: true, CountsTowardHours: true, Minutes: minutes}
	}
	unpaid := func(kind TravelKind) TravelClassification {
		return TravelClassification{Kind: kind, Paid: false, CountsTowardHours: false, Minutes: minutes}
	}

	if segment.WorkPerformed {
		return paid(TravelWorkPerformed), nil
	}
	if segment.BetweenSites {
		return paid(TravelBetweenSites), nil
	}
	if segment.SpecialOneDayAssignment {
		return paid(TravelSpecialAssignment), nil
	}
	if segment.HomeCommute {
		if policy.CaliforniaEmployerControlledTravel && segment.EmployerRequiredTransport {
			return paid(TravelEmployerControlled), nil
		}
		return unpaid(TravelHomeCommute), nil
	}
	if segment.Overnight {
		if normalMinuteWindowOverlap(segment.Interval, segment.NormalWorkStartMinute, segment.NormalWorkEndMinute) {
			return paid(TravelOvernightNormal), nil
		}
		return unpaid(TravelOvernightNormal), nil
	}
	return unpaid(TravelHomeCommute), nil
}

// MileageExpense is deliberately not an interval and has no hours field. A
// payroll/expense adapter can price it without ever adding it to worked time.
type MileageExpense struct {
	TenantID        string
	WorkerID        string
	SegmentStart    time.Time
	SegmentEnd      time.Time
	MilesHundredths int64
}

func (m MileageExpense) Validate() error {
	if m.TenantID == "" || m.WorkerID == "" || m.SegmentStart.IsZero() || m.SegmentEnd.IsZero() || !m.SegmentEnd.After(m.SegmentStart) || m.MilesHundredths < 0 {
		return fmt.Errorf("%w: invalid mileage expense", ErrInvalidEvidence)
	}
	return nil
}
