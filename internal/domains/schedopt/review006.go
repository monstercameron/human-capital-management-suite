// SCHED-OPT-006: human-review, modify and approve candidate schedules.
//
// ApplyReview takes one candidate schedule revision and a batch of manual
// changes, revalidates every change against the pinned hard rules, and —
// only when all changes hold — seals an approval binding the exact
// schedule revision. Every manual change records its reason and
// authority. The function is kernel-pure: it creates no work item and
// publishes nothing.
package schedopt

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ReviewVersion is the rejection version for schedule review.
const ReviewVersion = "schedopt-review/v1"

var (
	// ErrReviewRejected is the SCHED-OPT-006 sentinel. A manual change
	// that violates a hard rule, or lacks reason or authority, fails with
	// this error; nothing is approved.
	ErrReviewRejected = errors.New("SCHED_OPT_006_REJECTED")
)

// ReviewRejection is the stable SCHED-OPT-006 failure shape.
type ReviewRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *ReviewRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrReviewRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the SCHED_OPT_006_REJECTED sentinel to errors.Is.
func (r *ReviewRejection) Unwrap() error { return ErrReviewRejected }

func reviewReject(field, state, reason string) error {
	return &ReviewRejection{Field: field, State: state, Version: ReviewVersion, Reason: reason}
}

// ReviewAssignment is one candidate demand-to-worker binding.
type ReviewAssignment struct {
	AssignmentID string
	WorkerRef    string
	DemandRef    string
	WindowRef    string
}

// ReviewRule is one pinned hard rule. MaxPerWindow caps assignments
// sharing a window; BannedPair forbids one worker on one demand.
type ReviewRule struct {
	Kind      string
	WindowRef string
	WorkerRef string
	DemandRef string
	Max       int
}

// CandidateSchedule is the exact revision under review.
type CandidateSchedule struct {
	Tenant          string
	Revision        string
	RuleDigest      string
	Assignments     []ReviewAssignment
	Rules           []ReviewRule
	ReviewLifecycle ReviewLifecycle
	FairWorkweek    *FairWorkweekReviewContext
}

type ReviewLifecycle string

const (
	ReviewPrepublication ReviewLifecycle = "PREPUBLICATION"
	ReviewPublished      ReviewLifecycle = "PUBLISHED"
)

// FairWorkweekReviewContext binds edits to their existing publication and an
// evidence-bearing legal rule port. A missing rule stays unresolved.
type FairWorkweekReviewContext struct {
	Publication  Publication
	Jurisdiction string
	Rules        map[string]FairWorkweekRule
	RegularRates map[string]values.Rate
}

// ChangeKind is the closed manual-change vocabulary.
type ChangeKind string

const (
	ChangeAdd    ChangeKind = "ADD"
	ChangeRemove ChangeKind = "REMOVE"
	ChangeMove   ChangeKind = "MOVE"
)

// ManualChange is one human edit with its reason and authority.
type ManualChange struct {
	Kind         ChangeKind
	AssignmentID string
	WorkerRef    string
	DemandRef    string
	WindowRef    string
	Reason       string
	AuthorityRef string
	ChangedAt    time.Time
}

// ApprovedSchedule binds the exact reviewed revision.
type ApprovedSchedule struct {
	Tenant                  string
	Revision                string
	RuleDigest              string
	Assignments             []ReviewAssignment
	Changes                 []ManualChange
	ApprovedBy              string
	ApprovedAt              time.Time
	BoundDigest             string
	FairWorkweekAssessments []FairWorkweekAssessment
	PremiumObligations      []PremiumObligation
	ReviewLifecycle         ReviewLifecycle
}

func (a ApprovedSchedule) computedDigest() string {
	assignments := make([]string, 0, len(a.Assignments))
	for _, as := range a.Assignments {
		assignments = append(assignments, strings.Join([]string{as.AssignmentID, as.WorkerRef, as.DemandRef, as.WindowRef}, "\x00"))
	}
	sort.Strings(assignments)
	changes := make([]string, 0, len(a.Changes))
	for _, c := range a.Changes {
		changes = append(changes, strings.Join([]string{string(c.Kind), c.AssignmentID, c.WorkerRef, c.DemandRef, c.WindowRef, c.Reason, c.AuthorityRef, c.ChangedAt.UTC().Format(time.RFC3339Nano)}, "\x00"))
	}
	sort.Strings(changes)
	assessments := fairWorkweekAssessmentDigests(a.FairWorkweekAssessments)
	obligations := fairWorkweekObligationDigests(a.PremiumObligations)
	w := canonicalbytes.New("hcmnext.domains.schedopt.ApprovedSchedule", 1).
		String("tenant", a.Tenant).
		String("revision", a.Revision).
		String("rule_digest", a.RuleDigest).
		String("review_lifecycle", string(a.ReviewLifecycle)).
		String("approved_by", a.ApprovedBy).
		String("approved_at", a.ApprovedAt.UTC().Format(time.RFC3339)).
		SortedStrings("assignments", assignments).
		SortedStrings("changes", changes).
		SortedStrings("fair_workweek_assessments", assessments).
		SortedStrings("premium_obligations", obligations)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func checkRules(assignments []ReviewAssignment, rules []ReviewRule) error {
	byWindow := map[string]int{}
	for _, a := range assignments {
		byWindow[a.WindowRef]++
	}
	seen := map[string]struct{}{}
	for _, a := range assignments {
		if strings.TrimSpace(a.AssignmentID) == "" || strings.TrimSpace(a.WorkerRef) == "" ||
			strings.TrimSpace(a.DemandRef) == "" || strings.TrimSpace(a.WindowRef) == "" {
			return reviewReject("review.assignment", "INCOMPLETE", "assignments carry id, worker, demand and window")
		}
		key := a.WorkerRef + "\x00" + a.WindowRef
		if _, dup := seen[key]; dup {
			return reviewReject("review.assignment", "DOUBLE_BOOKED", fmt.Sprintf("worker %s assigned twice in window %s", a.WorkerRef, a.WindowRef))
		}
		seen[key] = struct{}{}
	}
	for _, r := range rules {
		switch r.Kind {
		case "MAX_PER_WINDOW":
			if byWindow[r.WindowRef] > r.Max {
				return reviewReject("review.window", "OVER_CAPACITY", fmt.Sprintf("window %s holds %d assignments over max %d", r.WindowRef, byWindow[r.WindowRef], r.Max))
			}
		case "BANNED_PAIR":
			for _, a := range assignments {
				if a.WorkerRef == r.WorkerRef && a.DemandRef == r.DemandRef {
					return reviewReject("review.assignment", "BANNED_PAIR", fmt.Sprintf("worker %s is banned from demand %s", r.WorkerRef, r.DemandRef))
				}
			}
		default:
			return reviewReject("review.rule", "UNDECLARED", fmt.Sprintf("rule kind %q is not declared", r.Kind))
		}
	}
	return nil
}

// ApplyReview revalidates manual changes against the pinned hard rules
// and seals an approval binding the exact schedule revision.
// ApplyReview handles reviews without an explicit lifecycle. It permits
// no-change approvals; manual edits must use ApplyPrepublicationReview or
// ApplyPublishedReview so notice assessment cannot be skipped by omission.
func ApplyReview(schedule CandidateSchedule, changes []ManualChange, approvedBy string, now time.Time) (ApprovedSchedule, error) {
	if len(changes) > 0 && schedule.FairWorkweek == nil {
		return ApprovedSchedule{}, reviewReject("review.fair_workweek", "UNKNOWN_REVIEW", "manual changes require ApplyPrepublicationReview or ApplyPublishedReview")
	}
	return applyReview(schedule, changes, approvedBy, now)
}

func applyReview(schedule CandidateSchedule, changes []ManualChange, approvedBy string, now time.Time) (ApprovedSchedule, error) {
	if strings.TrimSpace(schedule.Tenant) == "" {
		return ApprovedSchedule{}, reviewReject("review.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(schedule.Revision) == "" {
		return ApprovedSchedule{}, reviewReject("review.revision", "MISSING", "candidate revision is required")
	}
	if strings.TrimSpace(schedule.RuleDigest) == "" {
		return ApprovedSchedule{}, reviewReject("review.rule_digest", "MISSING", "pinned rule digest is required")
	}
	if len(changes) > 0 {
		if schedule.FairWorkweek == nil && schedule.ReviewLifecycle != ReviewPrepublication {
			return ApprovedSchedule{}, reviewReject("review.fair_workweek", "UNKNOWN_REVIEW", "manual changes require explicit prepublication state or published legal review context")
		}
		if schedule.FairWorkweek != nil && schedule.ReviewLifecycle != ReviewPublished {
			return ApprovedSchedule{}, reviewReject("review.fair_workweek", "LIFECYCLE_MISMATCH", "publication context requires published review lifecycle")
		}
	}
	if strings.TrimSpace(approvedBy) == "" {
		return ApprovedSchedule{}, reviewReject("review.approved_by", "MISSING", "approver is required")
	}
	if now.IsZero() {
		return ApprovedSchedule{}, reviewReject("review.approved_at", "MISSING", "approval instant is required")
	}
	if err := checkRules(schedule.Assignments, schedule.Rules); err != nil {
		return ApprovedSchedule{}, err
	}
	if schedule.FairWorkweek != nil {
		if err := validateFairWorkweekReviewBinding(schedule); err != nil {
			return ApprovedSchedule{}, err
		}
	}
	assignments := append([]ReviewAssignment(nil), schedule.Assignments...)
	var fairWorkweekAssessments []FairWorkweekAssessment
	var premiumObligations []PremiumObligation
	byID := make(map[string]int, len(assignments))
	for i, a := range assignments {
		byID[a.AssignmentID] = i
	}
	for i, c := range changes {
		if strings.TrimSpace(c.Reason) == "" {
			return ApprovedSchedule{}, reviewReject(fmt.Sprintf("review.changes[%d].reason", i), "MISSING", "manual change requires a reason")
		}
		if strings.TrimSpace(c.AuthorityRef) == "" {
			return ApprovedSchedule{}, reviewReject(fmt.Sprintf("review.changes[%d].authority_ref", i), "MISSING", "manual change requires an authority")
		}
		switch c.Kind {
		case ChangeAdd:
			if _, dup := byID[c.AssignmentID]; dup {
				return ApprovedSchedule{}, reviewReject(fmt.Sprintf("review.changes[%d]", i), "DUPLICATE", fmt.Sprintf("assignment %s already exists", c.AssignmentID))
			}
			if strings.TrimSpace(c.WorkerRef) == "" || strings.TrimSpace(c.DemandRef) == "" || strings.TrimSpace(c.WindowRef) == "" {
				return ApprovedSchedule{}, reviewReject(fmt.Sprintf("review.changes[%d]", i), "INCOMPLETE", "added assignment requires worker, demand and window")
			}
			byID[c.AssignmentID] = len(assignments)
			assignments = append(assignments, ReviewAssignment{AssignmentID: c.AssignmentID, WorkerRef: c.WorkerRef, DemandRef: c.DemandRef, WindowRef: c.WindowRef})
		case ChangeRemove:
			idx, ok := byID[c.AssignmentID]
			if !ok {
				return ApprovedSchedule{}, reviewReject(fmt.Sprintf("review.changes[%d]", i), "NOT_FOUND", fmt.Sprintf("assignment %s does not exist", c.AssignmentID))
			}
			assignments = append(assignments[:idx], assignments[idx+1:]...)
			byID = make(map[string]int, len(assignments))
			for j, a := range assignments {
				byID[a.AssignmentID] = j
			}
		case ChangeMove:
			idx, ok := byID[c.AssignmentID]
			if !ok {
				return ApprovedSchedule{}, reviewReject(fmt.Sprintf("review.changes[%d]", i), "NOT_FOUND", fmt.Sprintf("assignment %s does not exist", c.AssignmentID))
			}
			if strings.TrimSpace(c.WorkerRef) == "" || strings.TrimSpace(c.WindowRef) == "" {
				return ApprovedSchedule{}, reviewReject(fmt.Sprintf("review.changes[%d]", i), "INCOMPLETE", "moved assignment requires worker and window")
			}
			assignments[idx].WorkerRef = c.WorkerRef
			assignments[idx].WindowRef = c.WindowRef
			if strings.TrimSpace(c.DemandRef) != "" {
				assignments[idx].DemandRef = c.DemandRef
			}
		default:
			return ApprovedSchedule{}, reviewReject(fmt.Sprintf("review.changes[%d].kind", i), "UNDECLARED", fmt.Sprintf("change kind %q is not declared", c.Kind))
		}
		if err := checkRules(assignments, schedule.Rules); err != nil {
			return ApprovedSchedule{}, err
		}
		if schedule.FairWorkweek != nil {
			assessment, err := assessManualChange(*schedule.FairWorkweek, c, assignments)
			if err != nil {
				return ApprovedSchedule{}, err
			}
			if assessment.State == "UNKNOWN_REVIEW" {
				return ApprovedSchedule{}, reviewReject(fmt.Sprintf("review.changes[%d].fair_workweek", i), "UNKNOWN_REVIEW", assessment.ReviewCode)
			}
			fairWorkweekAssessments = append(fairWorkweekAssessments, assessment)
			if assessment.Obligation != nil {
				premiumObligations = append(premiumObligations, *assessment.Obligation)
			}
		}
	}
	approved := ApprovedSchedule{
		Tenant: schedule.Tenant, Revision: schedule.Revision, RuleDigest: schedule.RuleDigest,
		Assignments: assignments, Changes: append([]ManualChange(nil), changes...),
		ApprovedBy: approvedBy, ApprovedAt: now.UTC(),
		FairWorkweekAssessments: fairWorkweekAssessments,
		PremiumObligations:      premiumObligations,
		ReviewLifecycle:         schedule.ReviewLifecycle,
	}
	approved.BoundDigest = approved.computedDigest()
	return approved, nil
}

// ApplyPrepublicationReview makes the no-posting lifecycle explicit for a
// manual edit to a candidate that has not yet been published.
func ApplyPrepublicationReview(schedule CandidateSchedule, changes []ManualChange, approvedBy string, now time.Time) (ApprovedSchedule, error) {
	if schedule.FairWorkweek != nil {
		return ApprovedSchedule{}, reviewReject("review.fair_workweek", "LIFECYCLE_MISMATCH", "prepublication review cannot carry a publication context")
	}
	schedule.ReviewLifecycle = ReviewPrepublication
	return applyReview(schedule, changes, approvedBy, now)
}

// ApplyPublishedReview is the required review entry point for edits to an
// already posted revision. It binds the posting instant and legal inputs into
// ApplyReview so missed-notice edits cannot silently use the prepublication path.
func ApplyPublishedReview(schedule CandidateSchedule, publication Publication, jurisdiction string, rules map[string]FairWorkweekRule, regularRates map[string]values.Rate, changes []ManualChange, approvedBy string, now time.Time) (ApprovedSchedule, error) {
	schedule.ReviewLifecycle = ReviewPublished
	schedule.FairWorkweek = &FairWorkweekReviewContext{
		Publication: publication, Jurisdiction: jurisdiction,
		Rules: rules, RegularRates: regularRates,
	}
	return applyReview(schedule, changes, approvedBy, now)
}
