// Fairness and policy gating: MATCH-005 assesses one MatchResult
// against a caller-owned gate without re-ranking anyone.
//
// A prohibited attribute or proxy blocks; a cohort below the minimum
// or a fairness policy breach goes to human review. The assessment
// binds the exact ranking digest it gated, names only factor kinds and
// policy tokens in its reasons — it never accuses a candidate and
// never reorders a match. Assessment is pure: malformed inputs reject
// with MATCH_005_REJECTED naming field/version and persist nothing.
package matching

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// FairnessRejectedCode is the stable machine-readable refusal code.
const FairnessRejectedCode = "MATCH_005_REJECTED"

// FairnessRejectedError names the offending field and version.
type FairnessRejectedError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *FairnessRejectedError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// AsFairnessRejected unwraps a MATCH_005_REJECTED refusal.
func AsFairnessRejected(err error) (*FairnessRejectedError, bool) {
	if err == nil {
		return nil, false
	}
	if rejected, ok := err.(*FairnessRejectedError); ok && rejected.Code == FairnessRejectedCode {
		return rejected, true
	}
	return nil, false
}

func fairnessRejected(field, state string) *FairnessRejectedError {
	return &FairnessRejectedError{Code: FairnessRejectedCode, Field: field, State: state, Version: schemaVersion}
}

// FairnessVerdict is the only MATCH-005 verdict vocabulary.
type FairnessVerdict string

// The fairness verdicts.
const (
	FairnessAllow       FairnessVerdict = "ALLOW"
	FairnessNeedsReview FairnessVerdict = "NEEDS_REVIEW"
	FairnessBlocked     FairnessVerdict = "BLOCKED"
)

func (v FairnessVerdict) Valid() bool {
	return v == FairnessAllow || v == FairnessNeedsReview || v == FairnessBlocked
}

// FairnessGate is the caller-owned policy boundary. ProhibitedProxies
// lists attribute or proxy tokens that must not influence scoring;
// MinCohort is the smallest cohort the policy lets through without
// review; Policy is the fairness policy the request must declare.
type FairnessGate struct {
	ProhibitedProxies []string
	MinCohort         int
	Policy            FairnessPolicy
}

func (g FairnessGate) Validate() error {
	if g.MinCohort <= 0 {
		return fmt.Errorf("%w: minimum cohort must be positive", ErrInvalidRequest)
	}
	for _, token := range g.ProhibitedProxies {
		if strings.TrimSpace(token) == "" {
			return fmt.Errorf("%w: prohibited proxy must not be blank", ErrInvalidRequest)
		}
	}
	if err := g.Policy.Validate(); err != nil {
		return err
	}
	return nil
}

// FairnessAssessment is the detached gate outcome. Reasons use closed
// tokens only — factor kinds and policy states, never candidate refs.
type FairnessAssessment struct {
	RequestID       string
	RequestDigest   string
	RankingDigest   string
	CohortSize      int
	MinCohort       int
	Verdict         FairnessVerdict
	Reasons         []string
	PolicyRef       string
	PolicyVersion   string
	CanonicalDigest string
}

func (a FairnessAssessment) body() []byte {
	reasons := append([]string(nil), a.Reasons...)
	sort.Strings(reasons)
	b, err := canonicalbytes.New("hcmnext.domains.matching.FairnessAssessment", schemaVersion).
		String("request_id", a.RequestID).String("request_digest", a.RequestDigest).
		String("ranking_digest", a.RankingDigest).Int("cohort_size", int64(a.CohortSize)).
		Int("min_cohort", int64(a.MinCohort)).String("verdict", string(a.Verdict)).
		SortedStrings("reason", reasons).
		String("policy_ref", a.PolicyRef).String("policy_version", a.PolicyVersion).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Explain renders the bounded human-readable account: verdict, reason
// count and cohort size only — never candidate content.
func (a FairnessAssessment) Explain() string {
	return fmt.Sprintf("fairness %s: reasons=%d cohort=%d", a.Verdict, len(a.Reasons), a.CohortSize)
}

// AssessFairness gates one ranking result against gate. It never
// re-ranks: the assessment binds result's digest and carries no
// matches. It is pure: no rows, events, outbox entries, human work or
// provider requests.
func AssessFairness(request MatchRequest, result MatchResult, gate FairnessGate) (FairnessAssessment, error) {
	if err := request.Validate(); err != nil {
		return FairnessAssessment{}, fairnessRejected("request", "invalid")
	}
	if err := result.Validate(); err != nil {
		return FairnessAssessment{}, fairnessRejected("result", "invalid")
	}
	if err := gate.Validate(); err != nil {
		return FairnessAssessment{}, fairnessRejected("gate", "invalid")
	}
	if result.RequestID != request.RequestID || result.RequestDigest != request.computedDigest() {
		return FairnessAssessment{}, fairnessRejected("result", "binding-mismatch")
	}
	if result.CanonicalDigest == "" || result.CanonicalDigest != result.computedDigest() {
		return FairnessAssessment{}, fairnessRejected("result", "digest-mismatch")
	}
	assessment := FairnessAssessment{
		RequestID: request.RequestID, RequestDigest: result.RequestDigest,
		RankingDigest: result.CanonicalDigest, CohortSize: len(result.Matches),
		MinCohort: gate.MinCohort, Verdict: FairnessAllow,
		PolicyRef: request.Fairness.PolicyRef, PolicyVersion: request.Fairness.Version,
	}
	if kinds := prohibitedKinds(result, gate.ProhibitedProxies); len(kinds) > 0 {
		assessment.Verdict = FairnessBlocked
		for _, kind := range kinds {
			assessment.Reasons = append(assessment.Reasons, "prohibited-proxy:"+string(kind))
		}
	}
	if len(result.Matches) < gate.MinCohort && assessment.Verdict == FairnessAllow {
		assessment.Verdict = FairnessNeedsReview
	}
	if len(result.Matches) < gate.MinCohort {
		assessment.Reasons = append(assessment.Reasons, "cohort-below-minimum")
	}
	if (request.Fairness.PolicyRef != gate.Policy.PolicyRef || request.Fairness.Version != gate.Policy.Version) && assessment.Verdict == FairnessAllow {
		assessment.Verdict = FairnessNeedsReview
	}
	if request.Fairness.PolicyRef != gate.Policy.PolicyRef || request.Fairness.Version != gate.Policy.Version {
		assessment.Reasons = append(assessment.Reasons, "fairness-policy-mismatch")
	}
	sort.Strings(assessment.Reasons)
	assessment.CanonicalDigest = canonicalbytes.Digest(assessment.body())
	return assessment, nil
}

// prohibitedKinds returns the sorted factor kinds whose score reasons
// or satisfaction details mention a prohibited token. Matching is
// case-insensitive; only the kind is reported, never the text hit.
func prohibitedKinds(result MatchResult, tokens []string) []ConstraintKind {
	lowered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		lowered = append(lowered, strings.ToLower(token))
	}
	seen := make(map[ConstraintKind]struct{})
	for _, match := range result.Matches {
		for _, factor := range match.Score.Factors {
			text := strings.ToLower(factor.Reason)
			for _, token := range lowered {
				if strings.Contains(text, token) {
					seen[factor.Kind] = struct{}{}
				}
			}
		}
		for _, satisfaction := range match.Satisfactions {
			text := strings.ToLower(satisfaction.Detail)
			for _, token := range lowered {
				if strings.Contains(text, token) {
					seen[satisfaction.Kind] = struct{}{}
				}
			}
		}
	}
	kinds := make([]ConstraintKind, 0, len(seen))
	for kind := range seen {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return kinds
}
