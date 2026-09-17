// SURVEY-006: calculate aggregate survey results.
//
// ComputeAggregate turns one cohort's scored responses into versioned,
// cohort-safe results. Scoring, weights, nonresponse handling and
// rounding ride the pinned rule version; uncertainty is always reported;
// representativeness is UNKNOWN unless the cohort and response rate earn
// it — it is never fabricated. The function is kernel-pure.
package survey

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// AggregateVersion is the rejection version for aggregate calculation.
const AggregateVersion = "survey-aggregate/v1"

var (
	// ErrAggregateRejected is the SURVEY-006 sentinel for inexact input.
	ErrAggregateRejected = errors.New("SURVEY_006_REJECTED")
)

// AggregateRejection is the stable SURVEY-006 failure shape.
type AggregateRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *AggregateRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrAggregateRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the SURVEY_006_REJECTED sentinel to errors.Is.
func (r *AggregateRejection) Unwrap() error { return ErrAggregateRejected }

func aggregateReject(field, state, reason string) error {
	return &AggregateRejection{Field: field, State: state, Version: AggregateVersion, Reason: reason}
}

// Representativeness is the closed SURVEY-006 coverage claim vocabulary.
type Representativeness string

const (
	RepresentativeCohort  Representativeness = "COHORT_REPRESENTATIVE"
	RepresentativeLimited Representativeness = "LIMITED"
	RepresentativeUnknown Representativeness = "UNKNOWN"
)

// AggregateInput is one cohort aggregation request. Scores carry one
// integer score per respondent in [ScoreMin, ScoreMax]; Weights parallels
// Scores or is empty for equal weight; Nonresponse counts invited
// non-respondents; Rounding is the pinned decimal mode.
type AggregateInput struct {
	Tenant        string
	CampaignRef   string
	RuleVersion   string
	ScoreMin      int64
	ScoreMax      int64
	Scores        []int64
	Weights       []int64
	Nonresponse   int64
	MinimumCohort int
	DecidedAt     time.Time
}

// AggregateResult is the versioned, cohort-safe outcome.
type AggregateResult struct {
	Respondents        int
	Mean               values.Decimal
	Distribution       map[int64]int
	NonresponseRateBps int64
	Uncertainty        values.Decimal
	Representativeness Representativeness
	RuleVersion        string
	Digest             string
}

func (r AggregateResult) computedDigest(in AggregateInput) string {
	buckets := make([]string, 0, len(r.Distribution))
	for score, count := range r.Distribution {
		buckets = append(buckets, fmt.Sprintf("%d:%d", score, count))
	}
	sort.Strings(buckets)
	scores := make([]string, 0, len(in.Scores))
	for _, s := range in.Scores {
		scores = append(scores, fmt.Sprintf("%d", s))
	}
	sort.Strings(scores)
	w := canonicalbytes.New("hcmnext.domains.survey.AggregateResult", 1).
		String("tenant", in.Tenant).
		String("campaign", in.CampaignRef).
		String("rule_version", in.RuleVersion).
		SortedStrings("scores", scores).
		Int("nonresponse", in.Nonresponse).
		Value("mean", r.Mean).
		Value("uncertainty", r.Uncertainty).
		SortedStrings("distribution", buckets).
		String("representativeness", string(r.Representativeness))
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// ComputeAggregate calculates the cohort aggregate for one question.
func ComputeAggregate(in AggregateInput) (AggregateResult, error) {
	if strings.TrimSpace(in.Tenant) == "" {
		return AggregateResult{}, aggregateReject("aggregate.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(in.CampaignRef) == "" {
		return AggregateResult{}, aggregateReject("aggregate.campaign_ref", "MISSING", "campaign ref is required")
	}
	if strings.TrimSpace(in.RuleVersion) == "" {
		return AggregateResult{}, aggregateReject("aggregate.rule_version", "MISSING", "rule version is required")
	}
	if in.ScoreMax <= in.ScoreMin {
		return AggregateResult{}, aggregateReject("aggregate.score_range", "INVALID", "score range must be non-empty")
	}
	if len(in.Scores) == 0 {
		return AggregateResult{}, aggregateReject("aggregate.scores", "MISSING", "at least one response is required")
	}
	if in.Nonresponse < 0 {
		return AggregateResult{}, aggregateReject("aggregate.nonresponse", "INVALID", "nonresponse must not be negative")
	}
	minimum := in.MinimumCohort
	if minimum <= 0 {
		minimum = MinReleaseCohort
	}
	if len(in.Scores) < minimum {
		return AggregateResult{}, aggregateReject("aggregate.cohort", "BELOW_MINIMUM", fmt.Sprintf("cohort %d below minimum %d", len(in.Scores), minimum))
	}
	if in.DecidedAt.IsZero() {
		return AggregateResult{}, aggregateReject("aggregate.decided_at", "MISSING", "decision instant is required")
	}
	weights := in.Weights
	if len(weights) == 0 {
		weights = make([]int64, len(in.Scores))
		for i := range weights {
			weights[i] = 1
		}
	}
	if len(weights) != len(in.Scores) {
		return AggregateResult{}, aggregateReject("aggregate.weights", "MISMATCH", "weights parallel scores")
	}
	var weightedSum, weightTotal int64
	distribution := make(map[int64]int, len(in.Scores))
	for i, s := range in.Scores {
		if s < in.ScoreMin || s > in.ScoreMax {
			return AggregateResult{}, aggregateReject("aggregate.scores", "OUT_OF_RANGE", fmt.Sprintf("score %d outside [%d, %d]", s, in.ScoreMin, in.ScoreMax))
		}
		if weights[i] <= 0 {
			return AggregateResult{}, aggregateReject("aggregate.weights", "INVALID", "weights must be positive")
		}
		weightedSum += s * weights[i]
		weightTotal += weights[i]
		distribution[s]++
	}
	mean, err := values.NewDecimal(fmt.Sprintf("%d.00", weightedSum), 2, values.RoundingHalfUp)
	if err != nil {
		return AggregateResult{}, aggregateReject("aggregate.mean", "INVALID", "weighted sum is not representable")
	}
	divisor, err := values.NewDecimal(fmt.Sprintf("%d.00", weightTotal), 2, values.RoundingHalfUp)
	if err != nil {
		return AggregateResult{}, aggregateReject("aggregate.mean", "INVALID", "weight total is not representable")
	}
	mean, err = mean.Div(divisor, 2, values.RoundingHalfUp)
	if err != nil {
		return AggregateResult{}, aggregateReject("aggregate.mean", "INEXACT", fmt.Sprintf("mean is not computable: %v", err))
	}
	invited := int64(len(in.Scores)) + in.Nonresponse
	nonresponseBps := in.Nonresponse * 10000 / invited
	// Uncertainty widens with nonresponse: half the score range scaled
	// by the nonresponse rate, so silence can never sharpen a result.
	halfRange := float64(in.ScoreMax-in.ScoreMin) / 2
	uncertainty, err := values.NewDecimal(fmt.Sprintf("%.2f", halfRange*float64(nonresponseBps)/10000), 2, values.RoundingHalfUp)
	if err != nil {
		return AggregateResult{}, aggregateReject("aggregate.uncertainty", "INEXACT", fmt.Sprintf("uncertainty is not computable: %v", err))
	}
	representativeness := RepresentativeUnknown
	switch {
	case nonresponseBps == 0 && len(in.Scores) >= minimum*2:
		representativeness = RepresentativeCohort
	case nonresponseBps <= 2000:
		representativeness = RepresentativeLimited
	}
	res := AggregateResult{
		Respondents: len(in.Scores), Mean: mean, Distribution: distribution,
		NonresponseRateBps: nonresponseBps, Uncertainty: uncertainty,
		Representativeness: representativeness, RuleVersion: in.RuleVersion,
	}
	res.Digest = res.computedDigest(in)
	return res, nil
}
