package timeclock

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Capability identities the time templates bind. Every one is listed in
// [Manifests] with its effect class and the timeprofile control or decision
// classes it carries, which is what CheckCompiled evaluates.
const (
	CapCommitPunch            = "hcmnext.time.commit_punch_observation"
	CapReadPunchFacts         = "hcmnext.time.read_punch_facts"
	CapOpenDurationTimesheet  = "hcmnext.time.open_duration_timesheet"
	CapRecordDurationLines    = "hcmnext.time.record_duration_lines"
	CapSeedExpectedPattern    = "hcmnext.time.seed_expected_pattern"
	CapRecordExceptionLine    = "hcmnext.time.record_exception_line"
	CapCollectPeriodInputs    = "hcmnext.time.collect_period_obligations"
	CapComputePremiums        = "hcmnext.time.compute_premiums"
	CapLockPeriodTimecard     = "hcmnext.time.lock_period_timecard"
	CapDispatchApprovedTime   = "hcmnext.time.dispatch_approved_time"
	CapObserveDestination     = "hcmnext.time.observe_destination_acceptance"
	CapContractorOpenPeriod   = "hcmnext.time.contractor.open_invoice_period"
	CapContractorRecordEntry  = "hcmnext.time.contractor.record_entry"
	CapContractorSubmitToAP   = "hcmnext.time.contractor.submit_invoice"
	CapContractorObserveAP    = "hcmnext.time.contractor.observe_invoice_acceptance"
	CapAgencyOpenTimesheet    = "hcmnext.time.agency.open_timesheet"
	CapAgencyExportToVMS      = "hcmnext.time.agency.export_to_vms"
	CapAgencyObserveVMS       = "hcmnext.time.agency.observe_vms_acceptance"
	CapEvaluateGeofence       = "hcmnext.time.evaluate_geofence"
	CapCapturePunchPhoto      = "hcmnext.time.capture_punch_photo"
	CapEnforceScheduleLockout = "hcmnext.time.enforce_schedule_lockout"
	CapRequireClockIn         = "hcmnext.time.require_clock_in"
	CapAssignShift            = "hcmnext.time.assign_shift"
)

// Rule references the DECISION nodes cite. RULE is not a step type; each is a
// DECISION with this rule_ref, evaluated by the matching entry in
// [DefaultEvaluators].
const (
	RuleClassifyPunch           = "rules.time.classify_punch/v1"
	RuleSessionEventKind        = "rules.time.session_event_kind/v1"
	RuleSessionNextStep         = "rules.time.session_next_step/v1"
	RuleValidateDurationLines   = "rules.time.validate_duration_lines/v1"
	RuleTimesheetComplete       = "rules.time.timesheet_complete/v1"
	RuleClassifyException       = "rules.time.classify_exception/v1"
	RulePeriodReady             = "rules.time.period_ready/v1"
	RuleDestinationPermitted    = "rules.time.destination_permitted/v1"
	RuleContractorValidateSOW   = "rules.time.contractor.validate_against_sow/v1"
	RuleAgencyValidateTimesheet = "rules.time.agency.validate_timesheet/v1"
)

// Form references the TASK nodes bind as their output schema. A TASK has no
// capability, so its class is keyed on the form it collects, which is part of
// the plan digest.
const (
	FormReviewPunch             = "hcmnext.forms.time.review_punch"
	FormCorrectDurationLines    = "hcmnext.forms.time.correct_duration_lines"
	FormReviewException         = "hcmnext.forms.time.review_exception"
	FormResolvePeriodExceptions = "hcmnext.forms.time.resolve_period_exceptions"
	FormAttestTimecard          = "hcmnext.forms.time.attest_timecard_and_breaks"
	FormReviewContractorEntry   = "hcmnext.forms.time.contractor.review_entry"
	FormReviewAgencyTimesheet   = "hcmnext.forms.time.agency.review_timesheet"
)

// Manifest is one capability's registry facts as the time templates need them:
// the effect class the compiler proves, the scope a node's authority must
// carry and the policy classes CheckCompiled reads.
type Manifest struct {
	ID          string
	OwnerDomain string
	Effect      capability.EffectClass
	Scope       string
	Classes     []string
}

func classes(values ...string) []string { return values }

func control(c timeprofile.ControlClass) string   { return string(c) }
func decision(c timeprofile.DecisionClass) string { return string(c) }

// Manifests returns every capability the time templates and their overlays
// may bind, sorted by ID. The control classes are policy data (WTIME-006
// REFACTOR): a new control capability is a new row here, never a new branch
// in CheckCompiled.
func Manifests() []Manifest {
	out := []Manifest{
		{CapCommitPunch, "time", capability.EffectInternalMutation, "scope:time.punch.write",
			classes(control(timeprofile.ControlMandatoryClockIn), decision(timeprofile.DecisionDailyRecording))},
		{CapReadPunchFacts, "time", capability.EffectReadOnly, "scope:time.punch.read",
			classes(control(timeprofile.ControlGeofence))},
		{CapOpenDurationTimesheet, "time", capability.EffectInternalMutation, "scope:time.timesheet.write", nil},
		{CapRecordDurationLines, "time", capability.EffectInternalMutation, "scope:time.timesheet.write",
			classes(decision(timeprofile.DecisionDailyRecording))},
		{CapSeedExpectedPattern, "time", capability.EffectInternalMutation, "scope:time.exception.write", nil},
		{CapRecordExceptionLine, "time", capability.EffectInternalMutation, "scope:time.exception.write", nil},
		{CapCollectPeriodInputs, "time", capability.EffectReadOnly, "scope:time.period.read", nil},
		{CapComputePremiums, "time", capability.EffectReadOnly, "scope:time.period.read", nil},
		{CapLockPeriodTimecard, "time", capability.EffectInternalMutation, "scope:time.period.write", nil},
		{CapDispatchApprovedTime, "time", capability.EffectExternalMutation, "scope:time.period.dispatch", nil},
		{CapObserveDestination, "time", capability.EffectReadOnly, "scope:time.period.read", nil},
		{CapContractorOpenPeriod, "time.contractor", capability.EffectInternalMutation, "scope:time.contractor.write", nil},
		{CapContractorRecordEntry, "time.contractor", capability.EffectInternalMutation, "scope:time.contractor.write", nil},
		{CapContractorSubmitToAP, "time.contractor", capability.EffectExternalMutation, "scope:time.contractor.dispatch", nil},
		{CapContractorObserveAP, "time.contractor", capability.EffectReadOnly, "scope:time.contractor.read", nil},
		{CapAgencyOpenTimesheet, "time.agency", capability.EffectInternalMutation, "scope:time.agency.write", nil},
		{CapAgencyExportToVMS, "time.agency", capability.EffectExternalMutation, "scope:time.agency.dispatch", nil},
		{CapAgencyObserveVMS, "time.agency", capability.EffectReadOnly, "scope:time.agency.read", nil},
		{CapEvaluateGeofence, "time", capability.EffectReadOnly, "scope:time.punch.read", classes(control(timeprofile.ControlGeofence))},
		{CapCapturePunchPhoto, "time", capability.EffectReadOnly, "scope:time.punch.read", classes(control(timeprofile.ControlPunchPhoto))},
		{CapEnforceScheduleLockout, "time", capability.EffectReadOnly, "scope:time.punch.read", classes(control(timeprofile.ControlScheduleLockout))},
		{CapRequireClockIn, "time", capability.EffectReadOnly, "scope:time.punch.read", classes(control(timeprofile.ControlMandatoryClockIn))},
		{CapAssignShift, "time", capability.EffectReadOnly, "scope:time.schedule.read", classes(control(timeprofile.ControlShiftAssignment))},
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ManifestFor returns one capability's manifest.
func ManifestFor(id string) (Manifest, bool) {
	for _, m := range Manifests() {
		if m.ID == id {
			return m, true
		}
	}
	return Manifest{}, false
}

// RuleClasses returns the decision and control classes a DECISION rule_ref
// carries. The punch classifier is the minor-hours, rest-period and early
// lockout decision in one rule pack; a plan that omits it omits all three.
func RuleClasses() map[string][]string {
	return map[string][]string{
		// These rules are part of the published time-rule registry but do not
		// carry a profile control or required-decision class. Keeping an
		// explicit empty entry lets the compiler distinguish a known neutral
		// rule from an unmanifested tenant overlay.
		RuleSessionEventKind:                      nil,
		RuleSessionNextStep:                       nil,
		RuleTimesheetComplete:                     nil,
		RulePeriodReady:                           nil,
		RuleDestinationPermitted:                  nil,
		RuleContractorValidateSOW:                 nil,
		"rules.time.classify_exception_period/v1": classes(decision(timeprofile.DecisionRestPeriod)),
		"rules.time.exception_resolution/v1":      nil,
		RuleClassifyPunch: classes(control(timeprofile.ControlScheduleLockout),
			decision(timeprofile.DecisionMinorHours), decision(timeprofile.DecisionRestPeriod)),
		RuleValidateDurationLines: classes(decision(timeprofile.DecisionDailyRecording),
			decision(timeprofile.DecisionMinorHours), decision(timeprofile.DecisionRestPeriod)),
		RuleClassifyException: classes(decision(timeprofile.DecisionRestPeriod)),
		RuleAgencyValidateTimesheet: classes(decision(timeprofile.DecisionDailyRecording),
			decision(timeprofile.DecisionMinorHours), decision(timeprofile.DecisionRestPeriod)),
	}
}

// FormClasses returns the classes a TASK form carries. Attesting breaks is a
// control indicator, so the period timecard's attestation form is one.
func FormClasses() map[string][]string {
	return map[string][]string{
		// Review/correction forms are published time forms without a
		// profile-control class. Explicit neutral entries keep unknown tenant
		// forms fail-closed while allowing the shipped templates to compile.
		FormReviewPunch:             nil,
		FormCorrectDurationLines:    nil,
		FormReviewException:         nil,
		FormResolvePeriodExceptions: nil,
		FormReviewContractorEntry:   nil,
		FormReviewAgencyTimesheet:   nil,
		FormAttestTimecard:          classes(control(timeprofile.ControlBreakAttestation)),
	}
}

// capabilityTable resolves the manifests for the compiler, exactly as the
// work-order template's static table does.
type capabilityTable map[capability.Key]capability.Record

func (t capabilityTable) Lookup(key capability.Key) (capability.Record, bool) {
	record, ok := t[key]
	return record, ok
}

// Capabilities returns the capability resolver every time template compiles
// against. A publisher must recompile with these exact records.
func Capabilities() workflow.CapabilityResolver {
	manifests := Manifests()
	table := make(capabilityTable, len(manifests))
	for _, m := range manifests {
		table[capability.Key{ID: m.ID, Version: 1}] = capability.Record{
			Definition: capability.Definition{
				ID: m.ID, Version: 1, OwnerDomain: m.OwnerDomain,
				RequestSchema:  capabilitySchemaRef(m.ID, "request"),
				ResponseSchema: capabilitySchemaRef(m.ID, "response"),
				ErrorSchema:    capabilitySchemaRef(m.ID, "error"),
				EffectClass:    m.Effect, RiskClass: "MEDIUM",
				IdempotencyPolicyRef: "idempotency.time/v1", AuthZScopeRef: m.Scope,
				LegalBasisRef: "legal.time.recordkeeping/v1", EntitlementRef: "entitlement.time/v1",
				SLOClassRef: "slo.time.punch.p95-300ms/v1", TestRef: "conformance:" + m.ID + "/v1",
			},
			Status: capability.StatusActive, Digest: "sha256:time:" + m.ID,
		}
	}
	return table
}

func capabilitySchemaRef(id, slot string) capability.SchemaRef {
	return capability.SchemaRef{SchemaID: id + "." + slot + "/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}
}
