package schedopt

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// FairWorkweekRule is supplied by a reviewed legal rule source. Schedopt
// deliberately carries no default jurisdiction rates or notice periods.
type FairWorkweekRule struct {
	Jurisdiction  string
	RuleRef       string
	RuleVersion   string
	CitationRef   string
	MinimumNotice time.Duration
	PremiumHours  values.Quantity
}

// PremiumBasis identifies how the obligation amount was priced.
type PremiumBasis string

const (
	PremiumBasisRegularRateHours PremiumBasis = "REGULAR_RATE_X_PREMIUM_HOURS"
)

// PremiumObligation is a separate statutory obligation, not ordinary wages.
type PremiumObligation struct {
	Kind           string
	Amount         values.Money
	Basis          PremiumBasis
	PremiumHours   values.Quantity
	Rate           values.Rate
	Jurisdiction   string
	RuleRef        string
	RuleVersion    string
	CitationRef    string
	ChangeDigest   string
	PostingInstant time.Time
	ChangeInstant  time.Time
}

type FairWorkweekAssessment struct {
	State        string // COMPLIANT, PREMIUM, or UNKNOWN_REVIEW
	Notice       time.Duration
	Jurisdiction string
	RuleRef      string
	RuleVersion  string
	CitationRef  string
	ChangeDigest string
	Obligation   *PremiumObligation
	ReviewCode   string
}

// AssessPublishedScheduleChange compares a manual change instant against the
// publication instant and an explicitly supplied jurisdiction rule. Missing
// or mismatched legal scope is UNKNOWN_REVIEW; it is never treated as compliant.
func AssessPublishedScheduleChange(pub Publication, change ManualChange, changedAt time.Time, jurisdiction string, rules map[string]FairWorkweekRule, regularRate values.Rate) (FairWorkweekAssessment, error) {
	if pub.PublishedAt.IsZero() {
		return FairWorkweekAssessment{}, publishReject("fair_workweek.posting_instant", "MISSING", "publication instant is required")
	}
	if pub.Digest == "" || pub.Digest != pub.computedDigest() {
		return FairWorkweekAssessment{}, publishReject("fair_workweek.publication", "UNBOUND", "publication digest must bind the posting instant")
	}
	if changedAt.IsZero() || changedAt.Before(pub.PublishedAt) {
		return FairWorkweekAssessment{}, publishReject("fair_workweek.change_instant", "INVALID", "change instant must be at or after publication")
	}
	if strings.TrimSpace(string(change.Kind)) == "" || strings.TrimSpace(change.AssignmentID) == "" || strings.TrimSpace(change.Reason) == "" || strings.TrimSpace(change.AuthorityRef) == "" {
		return FairWorkweekAssessment{}, publishReject("fair_workweek.manual_change", "INCOMPLETE", "manual change kind, assignment, reason and authority are required")
	}
	if strings.TrimSpace(jurisdiction) == "" {
		return FairWorkweekAssessment{State: "UNKNOWN_REVIEW", ReviewCode: "JURISDICTION_MISSING"}, nil
	}
	rule, ok := rules[jurisdiction]
	if !ok || rule.Jurisdiction != jurisdiction || strings.TrimSpace(rule.RuleRef) == "" || strings.TrimSpace(rule.RuleVersion) == "" || strings.TrimSpace(rule.CitationRef) == "" || rule.MinimumNotice < 0 {
		return FairWorkweekAssessment{State: "UNKNOWN_REVIEW", ReviewCode: "RULE_UNRESOLVED"}, nil
	}
	notice := changedAt.Sub(pub.PublishedAt)
	result := FairWorkweekAssessment{
		State: "COMPLIANT", Notice: notice, Jurisdiction: jurisdiction,
		RuleRef: rule.RuleRef, RuleVersion: rule.RuleVersion,
		CitationRef: rule.CitationRef, ChangeDigest: manualChangeDigest(change, changedAt),
	}
	if notice >= rule.MinimumNotice {
		return result, nil
	}
	if err := rule.PremiumHours.Validate(); err != nil {
		return FairWorkweekAssessment{State: "UNKNOWN_REVIEW", Notice: notice, ReviewCode: "PREMIUM_BASIS_UNRESOLVED"}, nil
	}
	if regularRate.Validate() != nil || regularRate.PerUnit() != "HOUR" {
		return FairWorkweekAssessment{State: "UNKNOWN_REVIEW", Notice: notice, ReviewCode: "REGULAR_RATE_UNRESOLVED"}, nil
	}
	amount, err := regularRate.Apply(rule.PremiumHours, 2, values.RoundingHalfEven)
	if err != nil {
		return FairWorkweekAssessment{}, fmt.Errorf("price fair-workweek premium: %w", err)
	}
	result.State = "PREMIUM"
	result.Obligation = &PremiumObligation{
		Kind: "SCHEDULE_CHANGE_PREMIUM", Amount: amount,
		Basis: PremiumBasisRegularRateHours, PremiumHours: rule.PremiumHours,
		Rate: regularRate, Jurisdiction: jurisdiction, RuleRef: rule.RuleRef,
		RuleVersion: rule.RuleVersion, CitationRef: rule.CitationRef,
		ChangeDigest:   manualChangeDigest(change, changedAt),
		PostingInstant: pub.PublishedAt.UTC(), ChangeInstant: changedAt.UTC(),
	}
	return result, nil
}

func assessManualChange(ctx FairWorkweekReviewContext, change ManualChange, resulting []ReviewAssignment) (FairWorkweekAssessment, error) {
	worker := strings.TrimSpace(change.WorkerRef)
	if change.Kind == ChangeMove || change.Kind == ChangeRemove {
		worker = ""
		for _, assignment := range ctx.Publication.Assignments {
			if assignment.AssignmentID == change.AssignmentID {
				worker = assignment.WorkerRef
				break
			}
		}
	}
	if worker == "" && change.Kind == ChangeAdd {
		for _, assignment := range resulting {
			if assignment.AssignmentID == change.AssignmentID {
				worker = assignment.WorkerRef
				break
			}
		}
	}
	rate, hasRate := ctx.RegularRates[worker]
	if !hasRate {
		return FairWorkweekAssessment{State: "UNKNOWN_REVIEW", ReviewCode: "REGULAR_RATE_UNRESOLVED"}, nil
	}
	return AssessPublishedScheduleChange(ctx.Publication, change, change.ChangedAt, ctx.Jurisdiction, ctx.Rules, rate)
}

func validateFairWorkweekReviewBinding(schedule CandidateSchedule) error {
	ctx := schedule.FairWorkweek
	if ctx.Publication.PublishedAt.IsZero() || ctx.Publication.Digest == "" || ctx.Publication.Digest != ctx.Publication.computedDigest() {
		return reviewReject("review.fair_workweek.publication", "UNBOUND", "a bound publication with posting instant is required")
	}
	if schedule.Tenant != ctx.Publication.Tenant || schedule.Revision != ctx.Publication.Revision {
		return reviewReject("review.fair_workweek.publication", "MISMATCH", "candidate must match the published tenant and revision")
	}
	assignments := func(items []ReviewAssignment) []string {
		out := make([]string, 0, len(items))
		for _, a := range items {
			out = append(out, strings.Join([]string{a.AssignmentID, a.WorkerRef, a.DemandRef, a.WindowRef}, "\x00"))
		}
		sort.Strings(out)
		return out
	}
	if strings.Join(assignments(schedule.Assignments), "\x01") != strings.Join(assignments(ctx.Publication.Assignments), "\x01") {
		return reviewReject("review.fair_workweek.publication", "MISMATCH", "candidate assignments must match the published revision")
	}
	return nil
}

func manualChangeDigest(change ManualChange, changedAt time.Time) string {
	w := canonicalbytes.New("hcmnext.domains.schedopt.FairWorkweekManualChange", 1).
		String("kind", string(change.Kind)).
		String("assignment", change.AssignmentID).
		String("worker", change.WorkerRef).
		String("demand", change.DemandRef).
		String("window", change.WindowRef).
		String("reason", change.Reason).
		String("authority", change.AuthorityRef).
		String("changed_at", changedAt.UTC().Format(time.RFC3339Nano))
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func fairWorkweekAssessmentDigests(assessments []FairWorkweekAssessment) []string {
	items := make([]string, 0, len(assessments))
	for _, a := range assessments {
		item := strings.Join([]string{a.State, a.Notice.String(), a.Jurisdiction, a.RuleRef, a.RuleVersion, a.CitationRef, a.ChangeDigest, a.ReviewCode}, "\x00")
		if a.Obligation != nil {
			item += "\x00" + a.Obligation.ChangeDigest + "\x00" + a.Obligation.Amount.String() + "\x00" + string(a.Obligation.Basis)
		}
		items = append(items, item)
	}
	sort.Strings(items)
	return items
}

func fairWorkweekObligationDigests(obligations []PremiumObligation) []string {
	items := make([]string, 0, len(obligations))
	for _, o := range obligations {
		items = append(items, strings.Join([]string{o.Kind, o.Amount.String(), string(o.Basis), o.PremiumHours.String(), o.Rate.String(), o.Jurisdiction, o.RuleRef, o.RuleVersion, o.CitationRef, o.ChangeDigest, o.PostingInstant.UTC().Format(time.RFC3339Nano), o.ChangeInstant.UTC().Format(time.RFC3339Nano)}, "\x00"))
	}
	sort.Strings(items)
	return items
}
