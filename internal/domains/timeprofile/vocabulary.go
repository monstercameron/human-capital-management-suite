// Package timeprofile owns the per-assignment time profile: how a worker's
// time is captured, which rules apply to it and where approved time goes.
// Capture mode, pay basis, exemption status and worker category are four
// independent axes; no field is derived from another.
//
// This file is the shared vocabulary every time-keeping package imports.
// It is pure: no clock, no storage, no network.
package timeprofile

import "strings"

// CaptureMode is how time is recorded for an assignment.
type CaptureMode string

const (
	CapturePunch     CaptureMode = "PUNCH"          // clock in, breaks, clock out
	CaptureDuration  CaptureMode = "DURATION"       // hours per day and project
	CaptureException CaptureMode = "EXCEPTION_ONLY" // scheduled pattern plus deviations
	CaptureNone      CaptureMode = "NONE"           // no time recorded
)

// PayBasis is how the assignment is paid. It says nothing about overtime.
type PayBasis string

const (
	PayHourly    PayBasis = "HOURLY"
	PaySalary    PayBasis = "SALARY"
	PayPieceRate PayBasis = "PIECE_RATE"
	PayDayRate   PayBasis = "DAY_RATE"
	PayContract  PayBasis = "CONTRACT_RATE" // contractor SOW or rate card
)

// ExemptionStatus is the overtime classification under the applicable law.
// SALARIED_NON_EXEMPT is a pay-basis and exemption pair spelled out because
// it is the case most often collapsed into EXEMPT.
type ExemptionStatus string

const (
	NonExempt         ExemptionStatus = "NON_EXEMPT"
	Exempt            ExemptionStatus = "EXEMPT"
	SalariedNonExempt ExemptionStatus = "SALARIED_NON_EXEMPT"
	NotApplicable     ExemptionStatus = "NOT_APPLICABLE" // contractors and agency temps
)

// WorkerCategory is the legal relationship between the worker and the tenant.
type WorkerCategory string

const (
	CategoryEmployee   WorkerCategory = "EMPLOYEE"
	CategoryContractor WorkerCategory = "CONTRACTOR"
	CategoryAgencyTemp WorkerCategory = "AGENCY_TEMP"
	CategoryPlatform   WorkerCategory = "PLATFORM_WORKER"
)

// Destination is where approved time is delivered.
type Destination string

const (
	DestinationPayroll     Destination = "PAYROLL"
	DestinationInvoice     Destination = "CONTRACTOR_INVOICE"
	DestinationAgency      Destination = "AGENCY_EXPORT"
	DestinationCostingOnly Destination = "COSTING_ONLY"
)

// OvertimeMethod is how the regular rate and overtime are computed.
type OvertimeMethod string

const (
	OvertimeNone             OvertimeMethod = "NONE"
	OvertimeSingleRate       OvertimeMethod = "SINGLE_RATE"
	OvertimeWeightedAverage  OvertimeMethod = "WEIGHTED_AVERAGE"   // 29 CFR 778.115
	OvertimeFluctuatingWeek  OvertimeMethod = "FLUCTUATING_WEEK"   // 29 CFR 778.114
	OvertimeHealthcare880    OvertimeMethod = "HEALTHCARE_8_80"    // 29 CFR 778.601
	OvertimePublicCompTime   OvertimeMethod = "PUBLIC_COMP_TIME"   // FLSA 7(o)
	OvertimePublicSafety7k   OvertimeMethod = "PUBLIC_SAFETY_7K"   // FLSA 7(k)
	OvertimePieceRateAverage OvertimeMethod = "PIECE_RATE_AVERAGE" // 29 CFR 778.111
)

// Template names the workflow template a profile resolves to. Exactly one
// template matches a valid profile.
type Template string

const (
	TemplatePunchSession   Template = "time.punch_session"
	TemplateDurationSheet  Template = "time.duration_timesheet"
	TemplateExceptionOnly  Template = "time.exception_period"
	TemplateContractorTime Template = "time.contractor_invoice"
	TemplateAgencyTime     Template = "time.agency_vms"
)

// ControlClass tags a capability that directs when, where or how work is
// done. The ABC test, IR35 and the EU Platform Work Directive treat these
// as indicators of employment, so contractor plans must not contain them.
type ControlClass string

const (
	ControlScheduleLockout  ControlClass = "SCHEDULE_LOCKOUT"
	ControlMandatoryClockIn ControlClass = "MANDATORY_CLOCK_IN"
	ControlGeofence         ControlClass = "GEOFENCE"
	ControlPunchPhoto       ControlClass = "PUNCH_PHOTO"
	ControlBreakAttestation ControlClass = "BREAK_ATTESTATION"
	ControlShiftAssignment  ControlClass = "SHIFT_ASSIGNMENT"
)

// ControlClasses lists every control class in a stable order.
func ControlClasses() []ControlClass {
	return []ControlClass{ControlScheduleLockout, ControlMandatoryClockIn, ControlGeofence,
		ControlPunchPhoto, ControlBreakAttestation, ControlShiftAssignment}
}

func (m CaptureMode) Valid() bool {
	switch m {
	case CapturePunch, CaptureDuration, CaptureException, CaptureNone:
		return true
	}
	return false
}

func (p PayBasis) Valid() bool {
	switch p {
	case PayHourly, PaySalary, PayPieceRate, PayDayRate, PayContract:
		return true
	}
	return false
}

func (e ExemptionStatus) Valid() bool {
	switch e {
	case NonExempt, Exempt, SalariedNonExempt, NotApplicable:
		return true
	}
	return false
}

func (c WorkerCategory) Valid() bool {
	switch c {
	case CategoryEmployee, CategoryContractor, CategoryAgencyTemp, CategoryPlatform:
		return true
	}
	return false
}

func (d Destination) Valid() bool {
	switch d {
	case DestinationPayroll, DestinationInvoice, DestinationAgency, DestinationCostingOnly:
		return true
	}
	return false
}

func (m OvertimeMethod) Valid() bool {
	switch m {
	case OvertimeNone, OvertimeSingleRate, OvertimeWeightedAverage, OvertimeFluctuatingWeek,
		OvertimeHealthcare880, OvertimePublicCompTime, OvertimePublicSafety7k, OvertimePieceRateAverage:
		return true
	}
	return false
}

func (t Template) Valid() bool {
	switch t {
	case TemplatePunchSession, TemplateDurationSheet, TemplateExceptionOnly, TemplateContractorTime, TemplateAgencyTime:
		return true
	}
	return false
}

// ParseCaptureMode normalizes a stored token; unknown tokens return "" and false.
func ParseCaptureMode(token string) (CaptureMode, bool) {
	m := CaptureMode(strings.ToUpper(strings.TrimSpace(token)))
	return m, m.Valid()
}

// ParseWorkerCategory normalizes a stored token; unknown tokens return "" and false.
func ParseWorkerCategory(token string) (WorkerCategory, bool) {
	c := WorkerCategory(strings.ToUpper(strings.TrimSpace(token)))
	return c, c.Valid()
}

// ParseExemptionStatus normalizes a stored token; unknown tokens return "" and false.
func ParseExemptionStatus(token string) (ExemptionStatus, bool) {
	e := ExemptionStatus(strings.ToUpper(strings.TrimSpace(token)))
	return e, e.Valid()
}
