// Return-to-Work readiness evaluates whether a worker may return, with
// what restrictions, or not at all (LEAVE-012).
//
// It is pure Leave-domain readiness: it reads clearance, restriction, job,
// schedule, access and qualification facts plus the five bound revisions,
// and returns exactly one of READY, READY_WITH_RESTRICTIONS, NOT_READY or
// UNKNOWN. It never mutates an assignment: a structured restriction yields
// a governed EstablishWorkRestriction follow-up intent (plus
// accommodation/reassignment when qualification is conditional), while
// free-text restriction input is refused outright.
package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Readiness results: the closed LEAVE-012 vocabulary.
const (
	ReadinessReady                 = "READY"
	ReadinessReadyWithRestrictions = "READY_WITH_RESTRICTIONS"
	ReadinessNotReady              = "NOT_READY"
	ReadinessUnknown               = "UNKNOWN"
)

// Clearance states: missing, expired or quarantined clearance never returns ready.
const (
	ClearanceValid       = "VALID"
	ClearanceMissing     = "MISSING"
	ClearanceExpired     = "EXPIRED"
	ClearanceQuarantined = "QUARANTINED"
	ClearanceUnknown     = "UNKNOWN"
)

// Restriction states: an unresolved (blocking) restriction never returns ready.
const (
	RestrictionNone        = "NONE"
	RestrictionCleared     = "CLEARED"
	RestrictionConditional = "CONDITIONAL"
	RestrictionBlocking    = "BLOCKING"
	RestrictionUnknown     = "UNKNOWN"
)

// Job-requirement states: an unavailable job requirement never returns ready.
const (
	JobAvailable   = "AVAILABLE"
	JobUnavailable = "UNAVAILABLE"
	JobUnknown     = "UNKNOWN"
)

// Schedule states.
const (
	ScheduleConfirmed = "CONFIRMED"
	ScheduleUnknown   = "UNKNOWN"
)

// Access states.
const (
	AccessRestored = "RESTORED"
	AccessUnknown  = "UNKNOWN"
)

// Qualification states (aligned with QUAL-004's typed qualification).
const (
	QualificationQualified    = "QUALIFIED"
	QualificationConditional  = "CONDITIONAL"
	QualificationNotQualified = "NOT_QUALIFIED"
	QualificationUnknown      = "UNKNOWN"
)

// Follow-up intent kinds: the only governed intents this evaluation emits.
// They are requests to be planned elsewhere, never executions performed here.
const (
	FollowUpEstablishWorkRestriction = "EstablishWorkRestriction"
	FollowUpAccommodation            = "Accommodation"
	FollowUpReassignment             = "Reassignment"
)

// StructuredRestriction is one governed capability constraint. Code names a
// capability limit (for example LIFT-10KG); Source names its origin
// (MEDICAL or SAFETY). Free-text notes are never carried here.
type StructuredRestriction struct {
	ID     string
	Code   string
	Source string
}

// FollowUpIntent is one governed follow-up this evaluation requests.
type FollowUpIntent struct {
	Kind          string
	RestrictionID string
	Reason        string
}

// ReturnToWorkInput carries the readiness facts plus the five bound
// revisions. Assignment state is deliberately absent: this function cannot
// mutate what it is never given.
type ReturnToWorkInput struct {
	LeaveRevision    string
	EvidenceRevision string
	JobRevision      string
	ScheduleRevision string
	AccessRevision   string
	Clearance        string
	RestrictionState string
	Restrictions     []StructuredRestriction
	JobRequirement   string
	Schedule         string
	Access           string
	Qualification    string
	// FreeTextRestriction must always be empty: any free-text restriction
	// is refused with an error and produces no result.
	FreeTextRestriction string
}

// ReturnToWorkResult is the evaluated readiness with its bound revisions,
// closed-vocabulary reasons, governed follow-ups and integrity digest.
type ReturnToWorkResult struct {
	Result           string
	LeaveRevision    string
	EvidenceRevision string
	JobRevision      string
	ScheduleRevision string
	AccessRevision   string
	Reasons          []string
	FollowUps        []FollowUpIntent
	Digest           string
}

func readinessDigest(res ReturnToWorkResult) string {
	parts := []string{"leave-readiness", res.Result,
		res.LeaveRevision, res.EvidenceRevision, res.JobRevision, res.ScheduleRevision, res.AccessRevision}
	parts = append(parts, res.Reasons...)
	for _, fu := range res.FollowUps {
		parts = append(parts, fu.Kind+"\x01"+fu.RestrictionID+"\x01"+fu.Reason)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Verify recomputes the result seal.
func (r ReturnToWorkResult) Verify() error {
	if r.Digest == "" || readinessDigest(r) != r.Digest {
		return fmt.Errorf("leave: readiness seal is broken")
	}
	return nil
}

// EvaluateReturnToWorkReadiness evaluates domain-specific return readiness.
// Blocking facts (bad clearance, unresolved restriction, unavailable job,
// failed qualification) yield NOT_READY; unknown facts yield UNKNOWN;
// conditional facts yield READY_WITH_RESTRICTIONS with governed follow-ups;
// otherwise READY. Revisions are required and bound onto the result.
// Free-text restriction input is refused and mutates nothing.
func EvaluateReturnToWorkReadiness(in ReturnToWorkInput) (ReturnToWorkResult, error) {
	if strings.TrimSpace(in.LeaveRevision) == "" || strings.TrimSpace(in.EvidenceRevision) == "" ||
		strings.TrimSpace(in.JobRevision) == "" || strings.TrimSpace(in.ScheduleRevision) == "" ||
		strings.TrimSpace(in.AccessRevision) == "" {
		return ReturnToWorkResult{}, fmt.Errorf("leave: readiness binds leave, evidence, job, schedule and access revisions")
	}
	switch in.Clearance {
	case ClearanceValid, ClearanceMissing, ClearanceExpired, ClearanceQuarantined, ClearanceUnknown:
	default:
		return ReturnToWorkResult{}, fmt.Errorf("leave: clearance %q is not a readiness state", in.Clearance)
	}
	switch in.RestrictionState {
	case RestrictionNone, RestrictionCleared, RestrictionConditional, RestrictionBlocking, RestrictionUnknown:
	default:
		return ReturnToWorkResult{}, fmt.Errorf("leave: restriction state %q is not a readiness state", in.RestrictionState)
	}
	switch in.JobRequirement {
	case JobAvailable, JobUnavailable, JobUnknown:
	default:
		return ReturnToWorkResult{}, fmt.Errorf("leave: job requirement %q is not a readiness state", in.JobRequirement)
	}
	switch in.Schedule {
	case ScheduleConfirmed, ScheduleUnknown:
	default:
		return ReturnToWorkResult{}, fmt.Errorf("leave: schedule %q is not a readiness state", in.Schedule)
	}
	switch in.Access {
	case AccessRestored, AccessUnknown:
	default:
		return ReturnToWorkResult{}, fmt.Errorf("leave: access %q is not a readiness state", in.Access)
	}
	switch in.Qualification {
	case QualificationQualified, QualificationConditional, QualificationNotQualified, QualificationUnknown:
	default:
		return ReturnToWorkResult{}, fmt.Errorf("leave: qualification %q is not a readiness state", in.Qualification)
	}
	if strings.TrimSpace(in.FreeTextRestriction) != "" {
		return ReturnToWorkResult{}, fmt.Errorf("leave: free-text restriction never mutates an assignment: restate it as a structured restriction")
	}
	for _, r := range in.Restrictions {
		if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Code) == "" {
			return ReturnToWorkResult{}, fmt.Errorf("leave: structured restriction carries an id and a capability code")
		}
		switch strings.TrimSpace(r.Source) {
		case "MEDICAL", "SAFETY":
		default:
			return ReturnToWorkResult{}, fmt.Errorf("leave: restriction source %q is not governed", r.Source)
		}
	}

	bind := func(result string, reasons []string, followUps []FollowUpIntent) ReturnToWorkResult {
		out := ReturnToWorkResult{
			Result: result, LeaveRevision: in.LeaveRevision, EvidenceRevision: in.EvidenceRevision,
			JobRevision: in.JobRevision, ScheduleRevision: in.ScheduleRevision, AccessRevision: in.AccessRevision,
			Reasons: reasons, FollowUps: followUps,
		}
		out.Digest = readinessDigest(out)
		return out
	}

	switch in.Clearance {
	case ClearanceMissing, ClearanceExpired, ClearanceQuarantined:
		return bind(ReadinessNotReady, []string{"clearance_" + strings.ToLower(in.Clearance)}, nil), nil
	}
	if in.RestrictionState == RestrictionBlocking {
		return bind(ReadinessNotReady, []string{"restriction_blocking"}, nil), nil
	}
	if in.JobRequirement == JobUnavailable {
		return bind(ReadinessNotReady, []string{"job_unavailable"}, nil), nil
	}
	if in.Qualification == QualificationNotQualified {
		return bind(ReadinessNotReady, []string{"qualification_not_qualified"}, nil), nil
	}
	if in.Clearance == ClearanceUnknown || in.RestrictionState == RestrictionUnknown ||
		in.JobRequirement == JobUnknown || in.Schedule == ScheduleUnknown ||
		in.Access == AccessUnknown || in.Qualification == QualificationUnknown {
		return bind(ReadinessUnknown, []string{"unknown_state"}, nil), nil
	}
	if in.RestrictionState == RestrictionConditional || in.Qualification == QualificationConditional {
		if len(in.Restrictions) == 0 {
			return ReturnToWorkResult{}, fmt.Errorf("leave: conditional readiness carries at least one structured restriction")
		}
		followUps := make([]FollowUpIntent, 0, len(in.Restrictions)+2)
		for _, r := range in.Restrictions {
			followUps = append(followUps, FollowUpIntent{
				Kind: FollowUpEstablishWorkRestriction, RestrictionID: r.ID, Reason: "restriction_" + strings.ToLower(r.Code),
			})
		}
		followUps = append(followUps, FollowUpIntent{Kind: FollowUpAccommodation, Reason: "accommodation_review"})
		if in.Qualification == QualificationConditional {
			followUps = append(followUps, FollowUpIntent{Kind: FollowUpReassignment, Reason: "reassignment_review"})
		}
		return bind(ReadinessReadyWithRestrictions, []string{"conditional"}, followUps), nil
	}
	return bind(ReadinessReady, []string{"ready"}, nil), nil
}
