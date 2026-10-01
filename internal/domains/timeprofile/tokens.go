package timeprofile

import "strings"

// This file adds vocabulary the TIMEPROFILE lane needs beyond vocabulary.go,
// which is shared with other lanes and is not edited here. New tokens are
// declared in the open so another lane can find and reuse them instead of
// inventing a second name for the same concept.

// MinorAgeBand is the minor-worker age classification on a profile. The zero
// value means the worker is not a minor; it is not a placeholder for
// "unknown".
type MinorAgeBand string

const (
	MinorAgeBandNone    MinorAgeBand = ""         // not a minor
	MinorAgeBandUnder16 MinorAgeBand = "UNDER_16" // FLSA 29 CFR 570.35 and stricter state floors
	MinorAgeBand16To17  MinorAgeBand = "16_17"
)

// Valid reports whether b is a declared age band, including the not-a-minor
// zero value.
func (b MinorAgeBand) Valid() bool {
	switch b {
	case MinorAgeBandNone, MinorAgeBandUnder16, MinorAgeBand16To17:
		return true
	}
	return false
}

// IsMinor reports whether the band names an actual minor worker.
func (b MinorAgeBand) IsMinor() bool { return b != MinorAgeBandNone }

// DecisionClass tags a workflow DECISION a compiled plan must contain for
// some profile categories. Unlike ControlClass in vocabulary.go, a decision
// class is never forbidden; it can only be required.
type DecisionClass string

const (
	// DecisionMinorHours is required on any plan serving a minor profile
	// (29 CFR 570.35 and state minor-hours rules).
	DecisionMinorHours DecisionClass = "MINOR_HOURS_DECISION"
	// DecisionRestPeriod is required on any plan serving a profile whose
	// jurisdiction imposes a rest-period duty (EU Directive 2003/88/EC,
	// CJEU C-55/18, UK Working Time Regulations 1998).
	DecisionRestPeriod DecisionClass = "REST_PERIOD_DECISION"
	// DecisionDailyRecording is required on any plan serving a profile
	// whose jurisdiction imposes a daily-recording duty (CJEU C-55/18,
	// Spain RD-ley 8/2019, Germany BAG 1 ABR 22/21).
	DecisionDailyRecording DecisionClass = "DAILY_RECORDING"
)

func (c DecisionClass) Valid() bool {
	switch c {
	case DecisionMinorHours, DecisionRestPeriod, DecisionDailyRecording:
		return true
	}
	return false
}

// GovernmentContractFlags carries the DCAA total-time-accounting posture for
// a profile working on a government contract (FAR 31.201-2).
type GovernmentContractFlags struct {
	// DCAATotalTimeAccounting requires daily entry of every hour worked,
	// including leave, indirect and uncompensated overtime.
	DCAATotalTimeAccounting bool
	// UncompensatedOvertimeTracked requires uncompensated overtime on a
	// salaried DCAA profile to be recorded, not dropped.
	UncompensatedOvertimeTracked bool
}

// GrantFlags carries the reporting posture for a profile funded in part or
// whole by a grant or award (2 CFR 200.430(i)).
type GrantFlags struct {
	// ActivityReportRequired requires a periodic personnel activity report.
	ActivityReportRequired bool
	// SemiAnnualCertification requires a semi-annual certification in place
	// of an activity report when the worker is 100% on one award.
	SemiAnnualCertification bool
}

// ProjectTaxonomy codes duration lines to a primary taxonomy (project, cost
// code or award) and an optional, independently coded second taxonomy (for
// example ASC 350-40 capitalization alongside IRC 41 research activity).
type ProjectTaxonomy struct {
	Primary   string
	Secondary string // "" means no second taxonomy is coded
}

func (t ProjectTaxonomy) valid() bool { return strings.TrimSpace(t.Primary) != "" }
