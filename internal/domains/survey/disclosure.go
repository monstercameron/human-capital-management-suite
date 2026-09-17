// SURVEY-005: minimum-cohort and re-identification defenses for aggregate
// release. EvaluateRelease is the disclosure gate every survey aggregate
// passes before it may be shown: small cohorts suppress, small cells and
// rare attributes generalize, differencing and repeated filters cannot
// isolate a respondent, and free-text verbatim needs human review.
//
// The gate is kernel-pure: it reads no database, clock, or network. The
// decision instant is injected by the caller (DecidedAt), and requests carry
// only counts and fixed codes — never answer values, member references, or
// response content — so decisions and explanations cannot leak payload.
package survey

import (
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// MinReleaseCohort is the absolute floor for any aggregate release. It
// mirrors the SURVEY-001 anonymity minimum: a lower campaign threshold is
// invalid, not silently honored.
const MinReleaseCohort = 5

// RepeatReleaseLimit is the number of prior identical-cohort releases
// tolerated before a further identical re-release needs human review.
const RepeatReleaseLimit = 2

// ReleaseOutcome is the disclosure fate of one requested aggregate.
type ReleaseOutcome string

const (
	ReleaseReleased    ReleaseOutcome = "RELEASED"
	ReleaseSuppressed  ReleaseOutcome = "SUPPRESSED"
	ReleaseGeneralized ReleaseOutcome = "GENERALIZED"
	ReleaseNeedsReview ReleaseOutcome = "NEEDS_REVIEW"
)

// ReleaseReason names one fired re-identification defense.
type ReleaseReason string

const (
	ReleaseReasonSmallCohort   ReleaseReason = "SMALL_COHORT"
	ReleaseReasonSmallCell     ReleaseReason = "SMALL_CELL"
	ReleaseReasonDifferencing  ReleaseReason = "DIFFERENCING"
	ReleaseReasonRepeatedQuery ReleaseReason = "REPEATED_QUERY"
	ReleaseReasonFreeText      ReleaseReason = "FREE_TEXT"
	ReleaseReasonRareAttribute ReleaseReason = "RARE_ATTRIBUTE"
)

// ErrInvalidReleaseRequest is returned for malformed disclosure requests.
// Every such error matches with errors.Is; the message carries counts and
// fixed codes only, never respondent content.
var ErrInvalidReleaseRequest = fmt.Errorf("survey: invalid aggregate release request")

// PriorRelease describes one previously disclosed overlapping aggregate so
// the gate can refuse differencing and repeated-filter probing. OverlapCount
// is the number of respondents shared with the requested cohort.
type PriorRelease struct {
	CohortCount  int
	OverlapCount int
}

// ReleaseRequest asks whether one aggregate may be disclosed. CellCounts
// holds per-cell respondent counts for the requested breakdown; an empty
// slice requests a cohort-level total only.
type ReleaseRequest struct {
	CohortCount      int
	MinimumCohort    int
	CellCounts       []int
	HasFreeText      bool
	HasRareAttribute bool
	PriorReleases    []PriorRelease
	DecidedAt        values.Instant
}

// ReleaseDecision is the deterministic disclosure fate of a request.
// Reasons accumulate every fired defense; Outcome follows deny-dominance:
// SUPPRESSED beats NEEDS_REVIEW beats GENERALIZED beats RELEASED.
type ReleaseDecision struct {
	Outcome       ReleaseOutcome
	Reasons       []ReleaseReason
	CohortCount   int
	MinimumCohort int
	DecidedAt     values.Instant
	Digest        string
}

// Releasable reports whether the aggregate may be disclosed as requested.
func (d ReleaseDecision) Releasable() bool { return d.Outcome == ReleaseReleased }

// Explain returns a DLP-safe decision summary: outcome, counts, and fixed
// reason codes only. It never contains response content, answer values, or
// member references because the request never carried any.
func (d ReleaseDecision) Explain() string {
	if len(d.Reasons) == 0 {
		return fmt.Sprintf("Aggregate release %s: cohort %d meets minimum %d with no defense triggered; no response content, values, or member references are disclosed.", d.Outcome, d.CohortCount, d.MinimumCohort)
	}
	codes := make([]string, len(d.Reasons))
	for i, r := range d.Reasons {
		codes[i] = string(r)
	}
	return fmt.Sprintf("Aggregate release %s: cohort %d against minimum %d triggers %d defense(s) (%s); no response content, values, or member references are disclosed.", d.Outcome, d.CohortCount, d.MinimumCohort, len(d.Reasons), joinCodes(codes))
}

func joinCodes(codes []string) string {
	out := ""
	for i, c := range codes {
		if i > 0 {
			out += ","
		}
		out += c
	}
	return out
}

// EvaluateRelease applies the SURVEY-005 disclosure defenses to one request.
// Malformed requests fail closed with ErrInvalidReleaseRequest (a below-floor
// threshold additionally matches ErrAnonymityThresholdTooLow); well-formed
// requests always yield a decision, never an error.
func EvaluateRelease(req ReleaseRequest) (ReleaseDecision, error) {
	if err := req.DecidedAt.Validate(); err != nil {
		return ReleaseDecision{}, fmt.Errorf("%w: decision instant: %v", ErrInvalidReleaseRequest, err)
	}
	if req.MinimumCohort < MinReleaseCohort {
		return ReleaseDecision{}, fmt.Errorf("%w: minimum cohort %d below floor %d: %w", ErrInvalidReleaseRequest, req.MinimumCohort, MinReleaseCohort, ErrAnonymityThresholdTooLow)
	}
	if req.CohortCount < 0 {
		return ReleaseDecision{}, fmt.Errorf("%w: cohort count %d is negative", ErrInvalidReleaseRequest, req.CohortCount)
	}
	for i, cell := range req.CellCounts {
		if cell < 0 {
			return ReleaseDecision{}, fmt.Errorf("%w: cell %d count %d is negative", ErrInvalidReleaseRequest, i, cell)
		}
	}
	for i, prior := range req.PriorReleases {
		if prior.CohortCount < 0 || prior.OverlapCount < 0 ||
			prior.OverlapCount > prior.CohortCount || prior.OverlapCount > req.CohortCount {
			return ReleaseDecision{}, fmt.Errorf("%w: prior release %d has impossible overlap", ErrInvalidReleaseRequest, i)
		}
	}

	minimum := req.MinimumCohort
	var reasons []ReleaseReason
	add := func(r ReleaseReason) {
		for _, have := range reasons {
			if have == r {
				return
			}
		}
		reasons = append(reasons, r)
	}

	if req.CohortCount < minimum {
		add(ReleaseReasonSmallCohort)
	}
	for _, prior := range req.PriorReleases {
		if remainder := req.CohortCount - prior.OverlapCount; remainder > 0 && remainder < minimum {
			add(ReleaseReasonDifferencing)
			break
		}
		if remainder := prior.CohortCount - prior.OverlapCount; remainder > 0 && remainder < minimum {
			add(ReleaseReasonDifferencing)
			break
		}
	}
	if identical := countIdenticalReleases(req); identical >= RepeatReleaseLimit {
		add(ReleaseReasonRepeatedQuery)
	}
	for _, cell := range req.CellCounts {
		if cell < minimum {
			add(ReleaseReasonSmallCell)
			break
		}
	}
	if req.HasRareAttribute {
		add(ReleaseReasonRareAttribute)
	}
	if req.HasFreeText {
		add(ReleaseReasonFreeText)
	}

	slices.Sort(reasons)
	decision := ReleaseDecision{
		Outcome:       releaseOutcome(reasons),
		Reasons:       reasons,
		CohortCount:   req.CohortCount,
		MinimumCohort: minimum,
		DecidedAt:     req.DecidedAt,
	}
	decision.Digest = releaseDigest(decision)
	return decision, nil
}

// countIdenticalReleases counts prior disclosures over exactly the requested
// cohort. Re-pulling the same aggregate is harmless once, but repeated
// identical re-releases signal filter probing and need review.
func countIdenticalReleases(req ReleaseRequest) int {
	count := 0
	for _, prior := range req.PriorReleases {
		if prior.CohortCount == req.CohortCount && prior.OverlapCount == req.CohortCount {
			count++
		}
	}
	return count
}

func releaseOutcome(reasons []ReleaseReason) ReleaseOutcome {
	outcome := ReleaseReleased
	for _, r := range reasons {
		switch r {
		case ReleaseReasonSmallCohort, ReleaseReasonDifferencing:
			return ReleaseSuppressed
		case ReleaseReasonRepeatedQuery, ReleaseReasonFreeText:
			outcome = ReleaseNeedsReview
		case ReleaseReasonSmallCell, ReleaseReasonRareAttribute:
			if outcome == ReleaseReleased {
				outcome = ReleaseGeneralized
			}
		}
	}
	return outcome
}

func releaseDigest(d ReleaseDecision) string {
	sec, nsec := d.DecidedAt.Unix()
	w := canonicalbytes.New("hcmnext.domains.survey.ReleaseDecision", 1).
		String("outcome", string(d.Outcome)).
		Int("cohort_count", int64(d.CohortCount)).
		Int("minimum_cohort", int64(d.MinimumCohort)).
		Int("decided_at_sec", sec).
		Int("decided_at_nsec", int64(nsec))
	for _, r := range d.Reasons {
		w.String("reason", string(r))
	}
	digest, _ := w.Digest()
	return digest
}
