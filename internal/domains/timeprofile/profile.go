// Package timeprofile owns the per-assignment time profile: how a worker's
// time is captured, which rules apply to it and where approved time goes
// (WTIME-001), how that profile resolves to exactly one workflow template
// (the pure half of WTIME-002) and the capability-class constraints a
// compiled plan must and must not carry for that profile's category
// (WTIME-006).
//
// The package is pure: no clock, no storage, no network, no package-level
// mutable state. Every function that needs "now" takes a values.Instant
// argument instead of reading the wall clock.
package timeprofile

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// TimeProfile is the versioned, effective-dated record of how one
// assignment's time is captured, paid, classified and delivered. It is
// definitions data resolved by an EligibilityRule; adapters never read or
// branch on assignment fields directly (WTIME-001 REFACTOR).
type TimeProfile struct {
	// Identity and effective dating. A tenant may hold many versions of the
	// same logical profile; Version distinguishes them and EffectiveFrom/To
	// bounds when each version applies. EffectiveTo unset means open-ended.
	ID            string
	Version       uint64
	TenantRef     values.TenantId
	EffectiveFrom values.Instant
	EffectiveTo   values.Instant

	// Independent axes (WTIME-001: no field is derived from another).
	Capture   CaptureMode
	PayBasis  PayBasis
	Exemption ExemptionStatus
	Category  WorkerCategory

	// Overtime.
	OvertimeJurisdictions []string // e.g. "US-FED", "US-CA"; order-insensitive
	OvertimeMethod        OvertimeMethod
	PieceRate             bool
	// AggregationKey is the explicit worker key concurrent assignments
	// aggregate under for overtime. It is never inferred from the worker id.
	AggregationKey string

	// Eligibility flags.
	OnCallEligible        bool
	ReportingTimeEligible bool
	SplitShiftEligible    bool
	DifferentialEligible  bool

	// Minor workers.
	MinorAgeBand        MinorAgeBand
	MinorPermitVerified bool

	// Travel.
	TravelClassificationPolicyRef string

	// EU/UK working-time duties. These are two independent flags: a
	// jurisdiction can impose one without the other.
	RestPeriodDutyRequired     bool // EU Directive 2003/88/EC, UK WTR 1998
	DailyRecordingDutyRequired bool // CJEU C-55/18, Spain RD-ley 8/2019, BAG 1 ABR 22/21

	// Government contract and grant funding.
	GovernmentContract GovernmentContractFlags
	Grant              GrantFlags

	// Coding.
	Taxonomy          ProjectTaxonomy
	ApprovalChainRefs []string

	// Destination is where approved time is delivered.
	Destination Destination
}

// Validate enforces WTIME-001's cross-field law. It never applies a silent
// default: every field that participates in a rule must already hold a
// declared value, or Validate rejects the profile.
func (p TimeProfile) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return invalidProfile("id", "profile id is required")
	}
	if p.Version == 0 {
		return invalidProfile("version", "profile version must be at least 1")
	}
	if err := p.TenantRef.Validate(); err != nil {
		return invalidProfile("tenant_ref", err.Error())
	}
	if err := p.EffectiveFrom.Validate(); err != nil {
		return invalidProfile("effective_from", err.Error())
	}
	if p.EffectiveTo.IsSet() && !p.EffectiveTo.After(p.EffectiveFrom) {
		return invalidProfile("effective_to", "effective_to must be strictly after effective_from")
	}
	if !p.Capture.Valid() {
		return invalidProfile("capture", "capture mode is not declared")
	}
	if !p.PayBasis.Valid() {
		return invalidProfile("pay_basis", "pay basis is not declared")
	}
	if !p.Exemption.Valid() {
		return invalidProfile("exemption", "exemption status is not declared")
	}
	if !p.Category.Valid() {
		return invalidProfile("category", "worker category is not declared")
	}
	if !p.Destination.Valid() {
		return invalidProfile("destination", "destination is not declared")
	}
	if p.OvertimeMethod != "" && !p.OvertimeMethod.Valid() {
		return invalidProfile("overtime_method", "overtime method is not declared")
	}
	if !p.MinorAgeBand.Valid() {
		return invalidProfile("minor_age_band", "minor age band is not declared")
	}
	if strings.TrimSpace(p.AggregationKey) == "" {
		return invalidProfile("aggregation_key", "the worker overtime aggregation key must be explicit")
	}
	for i, j := range p.OvertimeJurisdictions {
		if strings.TrimSpace(j) == "" {
			return invalidProfile("overtime_jurisdictions", fmt.Sprintf("jurisdiction %d is empty", i))
		}
	}

	isContractorLike := p.Category == CategoryContractor || p.Category == CategoryAgencyTemp
	if isContractorLike {
		if p.Exemption != NotApplicable {
			return invalidProfile("exemption", "contractor and agency-temp profiles must be NOT_APPLICABLE")
		}
	} else if p.Exemption == NotApplicable {
		return invalidProfile("exemption", "NOT_APPLICABLE is reserved for contractor and agency-temp profiles")
	}

	switch p.Category {
	case CategoryContractor:
		if p.Destination != DestinationInvoice {
			return invalidProfile("destination", "contractor profiles must deliver to CONTRACTOR_INVOICE")
		}
	case CategoryAgencyTemp:
		if p.Destination != DestinationAgency {
			return invalidProfile("destination", "agency-temp profiles must deliver to AGENCY_EXPORT")
		}
	}
	if isContractorLike && p.Destination == DestinationPayroll {
		return invalidProfile("destination", "contractor and agency-temp time never reaches PAYROLL")
	}

	if p.Exemption == SalariedNonExempt && p.PayBasis != PaySalary {
		return invalidProfile("pay_basis", "SALARIED_NON_EXEMPT requires a SALARY pay basis")
	}

	nonExempt := p.Exemption == NonExempt || p.Exemption == SalariedNonExempt
	if p.Capture == CaptureException {
		if p.DailyRecordingDutyRequired {
			return invalidProfile("capture", "exception-only capture is forbidden when the daily-recording duty applies")
		}
		if nonExempt {
			return invalidProfile("capture", "exception-only capture is forbidden for a non-exempt or salaried-non-exempt profile")
		}
	}
	if p.Capture == CaptureNone && nonExempt {
		return invalidProfile("capture", "no-capture is forbidden for a non-exempt or salaried-non-exempt profile")
	}

	if p.MinorAgeBand.IsMinor() && !isContractorLike && p.Exemption != NonExempt {
		return invalidProfile("exemption", "a minor employee or platform-worker profile must be NON_EXEMPT")
	}

	if p.Grant.ActivityReportRequired && p.Grant.SemiAnnualCertification {
		return invalidProfile("grant", "activity reporting and semi-annual certification are mutually exclusive")
	}
	if !p.Taxonomy.valid() && (p.GovernmentContract.DCAATotalTimeAccounting || p.Grant.ActivityReportRequired || p.Grant.SemiAnnualCertification) {
		return invalidProfile("taxonomy", "a government-contract or grant profile requires a primary project taxonomy")
	}

	return nil
}

// canonicalBody writes the profile through the shared canonicalbytes framing
// so two structurally identical profiles always produce the same bytes,
// independent of slice order or the process that built them.
func (p TimeProfile) canonicalBody() []byte {
	jurisdictions := append([]string(nil), p.OvertimeJurisdictions...)
	approvals := append([]string(nil), p.ApprovalChainRefs...)
	w := canonicalbytes.New("hcmnext.domains.timeprofile.TimeProfile", schemaVersion).
		String("id", p.ID).Int("version", int64(p.Version)).String("tenant_ref", string(p.TenantRef)).
		Value("effective_from", p.EffectiveFrom).
		Optional("effective_to", p.EffectiveTo.IsSet(), p.EffectiveTo).
		String("capture", string(p.Capture)).String("pay_basis", string(p.PayBasis)).
		String("exemption", string(p.Exemption)).String("category", string(p.Category)).
		SortedStrings("overtime_jurisdictions", jurisdictions).
		String("overtime_method", string(p.OvertimeMethod)).Bool("piece_rate", p.PieceRate).
		String("aggregation_key", p.AggregationKey).
		Bool("on_call_eligible", p.OnCallEligible).Bool("reporting_time_eligible", p.ReportingTimeEligible).
		Bool("split_shift_eligible", p.SplitShiftEligible).Bool("differential_eligible", p.DifferentialEligible).
		String("minor_age_band", string(p.MinorAgeBand)).Bool("minor_permit_verified", p.MinorPermitVerified).
		String("travel_classification_policy_ref", p.TravelClassificationPolicyRef).
		Bool("rest_period_duty_required", p.RestPeriodDutyRequired).
		Bool("daily_recording_duty_required", p.DailyRecordingDutyRequired).
		Bool("gc_dcaa_total_time_accounting", p.GovernmentContract.DCAATotalTimeAccounting).
		Bool("gc_uncompensated_overtime_tracked", p.GovernmentContract.UncompensatedOvertimeTracked).
		Bool("grant_activity_report_required", p.Grant.ActivityReportRequired).
		Bool("grant_semi_annual_certification", p.Grant.SemiAnnualCertification).
		String("taxonomy_primary", p.Taxonomy.Primary).String("taxonomy_secondary", p.Taxonomy.Secondary).
		SortedStrings("approval_chain_refs", approvals).
		String("destination", string(p.Destination))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Canonical returns the profile's canonical byte encoding, or nil when the
// profile fails Validate.
func (p TimeProfile) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.canonicalBody()
}

// Digest returns the profile's canonical digest. It fails the same way
// Validate does.
func (p TimeProfile) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return canonicalbytes.Digest(p.canonicalBody()), nil
}

// coversInstant reports whether at falls within the profile's effective
// window: [EffectiveFrom, EffectiveTo) when EffectiveTo is set, or
// [EffectiveFrom, +inf) otherwise.
func (p TimeProfile) coversInstant(at values.Instant) bool {
	if at.Compare(p.EffectiveFrom) < 0 {
		return false
	}
	if p.EffectiveTo.IsSet() && at.Compare(p.EffectiveTo) >= 0 {
		return false
	}
	return true
}
