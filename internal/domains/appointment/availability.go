// APPT-002: resolve eligible participants and availability.
//
// Resolution is evidence, not surveillance: each candidate signal carries
// only a free/busy verdict, a leave state, a qualification state, a timezone
// and a privacy-consent flag — never calendar detail. The resolution reports
// one verdict per candidate (AVAILABLE, UNAVAILABLE or UNKNOWN) with reason
// codes only. Any unknown dimension stays UNKNOWN; it is never defaulted to
// available. Participant identity never appears in the output.
package appointment

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// AvailabilityVerdict is one candidate's resolved availability.
type AvailabilityVerdict string

// Verdicts.
const (
	VerdictAvailable   AvailabilityVerdict = "AVAILABLE"
	VerdictUnavailable AvailabilityVerdict = "UNAVAILABLE"
	VerdictUnknown     AvailabilityVerdict = "UNKNOWN"
)

func (v AvailabilityVerdict) valid() bool {
	return v == VerdictAvailable || v == VerdictUnavailable || v == VerdictUnknown
}

// PresenceSignal is the free/busy state of one candidate.
type PresenceSignal string

// Presence signals.
const (
	PresenceFree    PresenceSignal = "FREE"
	PresenceBusy    PresenceSignal = "BUSY"
	PresenceUnknown PresenceSignal = "UNKNOWN"
)

// LeaveSignal is the leave state of one candidate.
type LeaveSignal string

// Leave signals.
const (
	LeaveClear   LeaveSignal = "CLEAR"
	LeaveOnLeave LeaveSignal = "ON_LEAVE"
	LeaveUnknown LeaveSignal = "UNKNOWN"
)

// QualificationSignal is the qualification state of one candidate.
type QualificationSignal string

// Qualification signals.
const (
	Qualified            QualificationSignal = "QUALIFIED"
	Unqualified          QualificationSignal = "UNQUALIFIED"
	QualificationUnknown QualificationSignal = "UNKNOWN"
)

// CandidateSignal is the complete, calendar-free input for one candidate:
// free/busy, leave, qualification, timezone and consent to use even the
// free/busy signal. Source refs pin the revision each signal was read from.
type CandidateSignal struct {
	Participant    values.EntityRef
	Role           string
	Busy           PresenceSignal
	BusyRef        string
	Leave          LeaveSignal
	LeaveRef       string
	Qualification  QualificationSignal
	QualRef        string
	Timezone       string
	PrivacyConsent bool
}

// CandidateResolution is one candidate's verdict with reason codes only.
type CandidateResolution struct {
	Role        string
	Verdict     AvailabilityVerdict
	Reasons     []string
	BusyRef     string
	LeaveRef    string
	QualRef     string
	Participant values.EntityRef
}

func (r CandidateResolution) validate() error {
	if !r.Verdict.valid() || r.Role == "" || len(r.Reasons) == 0 {
		return fmt.Errorf("appointment: candidate resolution is incomplete")
	}
	return nil
}

// AvailabilityResolution is the bounded, deterministic availability answer
// for one requirement. It carries verdicts and digests only.
type AvailabilityResolution struct {
	RequirementID string
	Version       string
	Candidates    []CandidateResolution
	Digest        string
}

func rejectAvailability(field, state, version, reason string) error {
	return &Rejection{Code: "APPT_002_REJECTED", Field: field, State: state, Version: version, Reason: reason}
}

// ResolveAvailability resolves one availability verdict per candidate
// signal. Unknown busy, leave, qualification, timezone or privacy consent
// resolves UNKNOWN with the dimension named; it never defaults to
// available. Required participant roles without a signal reject the whole
// resolution: a missing candidate is a refusal, not an implicit decline.
func ResolveAvailability(req Requirement, signals []CandidateSignal) (AvailabilityResolution, error) {
	version := req.Version
	if version == "" {
		version = "v1"
	}
	if err := req.Validate(); err != nil {
		return AvailabilityResolution{}, rejectAvailability("requirement", "INVALID", version, err.Error())
	}
	if len(signals) == 0 {
		return AvailabilityResolution{}, rejectAvailability("participants", "EMPTY", version, "availability without candidate signals is not resolvable")
	}
	tenant := signals[0].Participant.Tenant
	seen := make(map[string]struct{}, len(signals))
	covered := make(map[string]int)
	for i, s := range signals {
		if err := s.Participant.Validate(); err != nil {
			return AvailabilityResolution{}, rejectAvailability("participants.ref", fmt.Sprintf("INVALID:%d", i), version, err.Error())
		}
		if s.Participant.Tenant != tenant {
			return AvailabilityResolution{}, rejectAvailability("participants.ref", fmt.Sprintf("CROSS_TENANT:%d", i), version, "candidate signals must share one tenant")
		}
		if s.Role == "" {
			return AvailabilityResolution{}, rejectAvailability("participants.role", fmt.Sprintf("INVALID:%d", i), version, "candidate role is required")
		}
		key := s.Participant.String() + "|" + s.Role
		if _, dup := seen[key]; dup {
			return AvailabilityResolution{}, rejectAvailability("participants.ref", fmt.Sprintf("DUPLICATE:%d", i), version, "duplicate candidate signal")
		}
		seen[key] = struct{}{}
		covered[s.Role]++
	}
	for _, p := range req.Participants {
		if p.Required && covered[p.Role] == 0 {
			return AvailabilityResolution{}, rejectAvailability("participants", "UNCOVERED", version, fmt.Sprintf("required role %q has no candidate signal", p.Role))
		}
	}
	out := AvailabilityResolution{RequirementID: req.ID, Version: req.Version, Candidates: make([]CandidateResolution, 0, len(signals))}
	for _, s := range signals {
		out.Candidates = append(out.Candidates, resolveCandidate(s))
	}
	sort.SliceStable(out.Candidates, func(i, j int) bool { return out.Candidates[i].Role < out.Candidates[j].Role })
	out.Digest = availabilityDigest(out)
	for _, c := range out.Candidates {
		if err := c.validate(); err != nil {
			return AvailabilityResolution{}, rejectAvailability("participants", "INVALID", version, err.Error())
		}
	}
	return out, nil
}

func resolveCandidate(s CandidateSignal) CandidateResolution {
	c := CandidateResolution{Role: s.Role, BusyRef: s.BusyRef, LeaveRef: s.LeaveRef, QualRef: s.QualRef}
	switch {
	case !s.PrivacyConsent:
		c.Verdict, c.Reasons = VerdictUnknown, []string{"privacy-unknown"}
	case s.Busy == PresenceUnknown || s.Busy == "":
		c.Verdict, c.Reasons = VerdictUnknown, []string{"busy-unknown"}
	case s.Leave == LeaveUnknown || s.Leave == "":
		c.Verdict, c.Reasons = VerdictUnknown, []string{"leave-unknown"}
	case s.Qualification == QualificationUnknown || s.Qualification == "":
		c.Verdict, c.Reasons = VerdictUnknown, []string{"qualification-unknown"}
	case s.Timezone == "":
		c.Verdict, c.Reasons = VerdictUnknown, []string{"timezone-unknown"}
	case s.Busy == PresenceBusy:
		c.Verdict, c.Reasons = VerdictUnavailable, []string{"busy"}
	case s.Leave == LeaveOnLeave:
		c.Verdict, c.Reasons = VerdictUnavailable, []string{"on-leave"}
	case s.Qualification == Unqualified:
		c.Verdict, c.Reasons = VerdictUnavailable, []string{"unqualified"}
	default:
		c.Verdict, c.Reasons = VerdictAvailable, []string{"available"}
	}
	return c
}

func availabilityDigest(res AvailabilityResolution) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s@%s", res.RequirementID, res.Version)
	for _, c := range res.Candidates {
		fmt.Fprintf(h, "|%s=%s", c.Role, c.Verdict)
		for _, r := range c.Reasons {
			fmt.Fprintf(h, ",%s", r)
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
