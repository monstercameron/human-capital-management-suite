package crewshift

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// AuthorityPathCrewShiftPublish is the single path every publication goes
// through, whatever proposed the shift. There is deliberately no second,
// looser path for an optimizer-sourced proposal: PublishInput.AuthorityPath
// must equal this constant for either ProposalSource, and Publish is the
// only function that turns a draft into a published shift.
const AuthorityPathCrewShiftPublish = "crewshift.publish.v1"

// PublishRejection is the stable FTIME-006 failure shape.
type PublishRejection struct {
	Field  string
	State  string
	Reason string
}

func (r *PublishRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s: %s", ErrPublishRejected, r.Field, r.State, r.Reason)
}

// Unwrap exposes the CREWSHIFT_PUBLISH_REJECTED sentinel to errors.Is.
func (r *PublishRejection) Unwrap() error { return ErrPublishRejected }

func publishReject(field, state, reason string) error {
	return &PublishRejection{Field: field, State: state, Reason: reason}
}

// EligibilityFacts is the caller-supplied standing of the worker being
// scheduled. It is evidence, never inferred: a missing fact fails closed.
type EligibilityFacts struct {
	Active  bool
	Revoked bool
}

// NoticePolicy is the configured minimum lead time between approval and
// shift start. It is data, never a hard-coded constant: a jurisdiction or
// tenant that requires no notice supplies a zero duration explicitly.
type NoticePolicy struct {
	MinimumNotice time.Duration
}

// RestPolicy is the configured minimum gap required between two shifts for
// the same worker.
type RestPolicy struct {
	MinimumRest time.Duration
}

// PublishInput is everything Publish needs to decide one draft. Existing
// carries every other shift -- across every project -- the worker already
// holds in PUBLISHED status, so overlap and rest are checked against the
// worker's whole schedule, not just this project's.
type PublishInput struct {
	Shift                  Shift
	Existing               []Shift
	Eligibility            EligibilityFacts
	HeldQualifications     []values.EntityRef
	RequiredQualifications []values.EntityRef
	ProjectAccess          map[string]bool
	Notice                 NoticePolicy
	Rest                   RestPolicy
	Approver               values.EntityRef
	AuthorityPath          string
	Now                    time.Time
}

// PublishCheck is one pluggable publication rule. Rules outside this
// package's scope -- minors' hours, EU rest periods -- register by
// implementing this interface and appending to the slice Publish is called
// with; they never need to edit this package.
type PublishCheck interface {
	Name() string
	Check(input PublishInput) error
}

type publishCheckFunc struct {
	name string
	fn   func(PublishInput) error
}

func (f publishCheckFunc) Name() string                { return f.name }
func (f publishCheckFunc) Check(in PublishInput) error { return f.fn(in) }

// NewPublishCheck adapts a plain function into a PublishCheck.
func NewPublishCheck(name string, fn func(PublishInput) error) PublishCheck {
	return publishCheckFunc{name: name, fn: fn}
}

// DefaultPublishChecks returns the built-in FTIME-006 checks in the order
// Publish runs them: authority path, eligibility, qualifications, project
// access, overlap, then rest. A caller assembles the final slice with
// append(DefaultPublishChecks(), pluggedInChecks...); order among the
// plugged-in checks is the caller's choice.
func DefaultPublishChecks() []PublishCheck {
	return []PublishCheck{
		NewPublishCheck("authority_path", authorityPathCheck),
		NewPublishCheck("eligibility", eligibilityCheck),
		NewPublishCheck("qualification", qualificationCheck),
		NewPublishCheck("project_access", projectAccessCheck),
		NewPublishCheck("overlap", overlapCheck),
		NewPublishCheck("rest", restCheck),
		NewPublishCheck("notice", noticeCheck),
	}
}

func authorityPathCheck(in PublishInput) error {
	if in.AuthorityPath != AuthorityPathCrewShiftPublish {
		return publishReject("authority_path", "UNAUTHORIZED",
			fmt.Sprintf("source %s must publish through %s", in.Shift.Source, AuthorityPathCrewShiftPublish))
	}
	return nil
}

func eligibilityCheck(in PublishInput) error {
	if in.Eligibility.Revoked {
		return publishReject("eligibility", "REVOKED", "worker's eligibility has been revoked")
	}
	if !in.Eligibility.Active {
		return publishReject("eligibility", "INACTIVE", "worker is not currently eligible")
	}
	return nil
}

func qualificationCheck(in PublishInput) error {
	held := make(map[string]struct{}, len(in.HeldQualifications))
	for _, q := range in.HeldQualifications {
		held[q.String()] = struct{}{}
	}
	for _, required := range in.RequiredQualifications {
		if _, ok := held[required.String()]; !ok {
			return publishReject("qualifications", "MISSING", "worker lacks required qualification "+required.String())
		}
	}
	return nil
}

func projectAccessCheck(in PublishInput) error {
	if !in.ProjectAccess[in.Shift.ProjectRef.String()] {
		return publishReject("project_access", "DENIED", "worker has no access to project "+in.Shift.ProjectRef.String())
	}
	return nil
}

// otherPublished returns every existing shift for the same worker that is
// currently published, excluding the shift being published itself (by ID).
func otherPublished(in PublishInput) []Shift {
	out := make([]Shift, 0, len(in.Existing))
	for _, other := range in.Existing {
		if other.ID == in.Shift.ID {
			continue
		}
		if other.Status != StatusPublished {
			continue
		}
		if other.WorkerRef.String() != in.Shift.WorkerRef.String() {
			continue
		}
		out = append(out, other)
	}
	return out
}

func overlapCheck(in PublishInput) error {
	for _, other := range otherPublished(in) {
		if in.Shift.Work.Overlaps(other.Work) {
			return publishReject("overlap", "DOUBLE_BOOKED",
				fmt.Sprintf("worker is already published on project %s during this window", other.ProjectRef.String()))
		}
	}
	return nil
}

func restCheck(in PublishInput) error {
	if in.Rest.MinimumRest <= 0 {
		return nil
	}
	for _, other := range otherPublished(in) {
		var gap time.Duration
		switch {
		case in.Shift.Work.Start.After(other.Work.End) || in.Shift.Work.Start.Equal(other.Work.End):
			gap = in.Shift.Work.Start.Sub(other.Work.End)
		case other.Work.Start.After(in.Shift.Work.End) || other.Work.Start.Equal(in.Shift.Work.End):
			gap = other.Work.Start.Sub(in.Shift.Work.End)
		default:
			continue // overlap is caught by overlapCheck
		}
		if gap < in.Rest.MinimumRest {
			return publishReject("rest", "INSUFFICIENT",
				fmt.Sprintf("only %s rest before/after an existing shift; %s required", gap, in.Rest.MinimumRest))
		}
	}
	return nil
}

func noticeCheck(in PublishInput) error {
	if in.Notice.MinimumNotice <= 0 {
		return nil
	}
	if in.Now.IsZero() {
		return publishReject("notice.now", "MISSING", "publication instant is required to check notice")
	}
	lead := in.Shift.Work.Start.Sub(in.Now)
	if lead < in.Notice.MinimumNotice {
		return publishReject("notice", "TOO_LATE",
			fmt.Sprintf("only %s notice before shift start; %s required", lead, in.Notice.MinimumNotice))
	}
	return nil
}

// PublishOutcome is the result of one Publish attempt: either a published
// Shift with Rejection nil, or a zero Shift with the rejection that stopped
// it. Keeping both on one struct lets Explain describe either outcome
// without the caller re-deriving it from an error type switch.
type PublishOutcome struct {
	Shift     Shift
	Rejection *PublishRejection
}

// Publish evaluates every check against the draft in input.Shift and, if all
// of them pass, returns the same shift advanced to PUBLISHED with its
// revision incremented and its approver and instants recorded. Approval is
// per-change: every call to Publish that succeeds records who approved that
// specific revision.
func Publish(input PublishInput, checks []PublishCheck) (PublishOutcome, error) {
	if input.Shift.Status != StatusDraft {
		err := publishReject("shift.status", "NOT_DRAFT", "only a draft shift can be published")
		return PublishOutcome{Rejection: err.(*PublishRejection)}, err
	}
	if err := input.Shift.Validate(); err != nil {
		return PublishOutcome{}, err
	}
	if err := input.Approver.Validate(); err != nil {
		err := publishReject("approver", "MISSING", "publication requires a valid approver reference")
		return PublishOutcome{Rejection: err.(*PublishRejection)}, err
	}
	if input.Approver.Tenant != input.Shift.Tenant {
		err := publishReject("approver", "CROSS_TENANT", "approver must belong to the shift's tenant")
		return PublishOutcome{Rejection: err.(*PublishRejection)}, err
	}
	for _, check := range checks {
		if err := check.Check(input); err != nil {
			var rej *PublishRejection
			if r, ok := err.(*PublishRejection); ok {
				rej = r
			}
			return PublishOutcome{Rejection: rej}, err
		}
	}
	published := input.Shift
	published.Status = StatusPublished
	published.ApprovedBy = input.Approver
	published.ApprovedAt = input.Now
	published.PublishedAt = input.Now
	published.Revision++
	return PublishOutcome{Shift: published}, nil
}
