// Match-result explanation: MATCH-006 explains one MatchResult per
// candidate without changing it.
//
// The explanation binds the exact request and ranking digests, covers the
// candidate source, every hard constraint, the score components and the
// tie/fairness contract, and lists soft misses as explicit unknowns. A
// factor outside the closed reason vocabulary, a hard constraint without
// an explanation, a ranking that contradicts eligibility dominance, or a
// reason that names another candidate refuses as MATCH_006_REJECTED. The
// explanation is pure: it persists nothing and never re-ranks anyone.
package matching

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// MatchExplanationRejectedCode is the stable machine-readable refusal code.
const MatchExplanationRejectedCode = "MATCH_006_REJECTED"

// ErrMatchExplanationRejected is the sentinel for refused explanations.
// Match it with errors.Is rather than parsing the reason.
var ErrMatchExplanationRejected = errors.New("matching: result explanation rejected")

// MatchExplanationRejectedError names the offending field and version.
type MatchExplanationRejectedError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *MatchExplanationRejectedError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// Is reports ErrMatchExplanationRejected without parsing the reason.
func (e *MatchExplanationRejectedError) Is(target error) bool {
	return target == ErrMatchExplanationRejected
}

// AsMatchExplanationRejected unwraps a MATCH_006_REJECTED refusal.
func AsMatchExplanationRejected(err error) (*MatchExplanationRejectedError, bool) {
	var rejected *MatchExplanationRejectedError
	if errors.As(err, &rejected) && rejected.Code == MatchExplanationRejectedCode {
		return rejected, true
	}
	return nil, false
}

func matchExplanationRejected(field, state string) *MatchExplanationRejectedError {
	return &MatchExplanationRejectedError{Code: MatchExplanationRejectedCode, Field: field, State: state, Version: schemaVersion}
}

// unknownPrefix marks a soft miss whose counterfactual effect on rank is
// explicitly unknown rather than silently ignored.
const unknownPrefix = "soft-unsatisfied:"

func validUnknown(token string) bool {
	kind, ok := strings.CutPrefix(token, unknownPrefix)
	return ok && ConstraintKind(kind).Valid()
}

// CandidateDecision explains one ranked candidate: its own eligibility,
// rank, score components and hard-constraint reasons, plus the soft
// misses left as unknowns. It never references another candidate.
type CandidateDecision struct {
	CandidateRef  values.EntityRef
	Eligible      bool
	Rank          int
	Score         Score
	Satisfactions []ConstraintSatisfaction
	Unknowns      []string
}

// Validate implements validation.
func (d CandidateDecision) Validate() error {
	if err := d.CandidateRef.Validate(); err != nil {
		return fmt.Errorf("%w: decision candidate ref: %v", ErrInvalidResult, err)
	}
	if d.Rank <= 0 {
		return fmt.Errorf("%w: decision rank must be positive", ErrInvalidResult)
	}
	if err := d.Score.Validate(); err != nil {
		return err
	}
	if len(d.Satisfactions) == 0 {
		return fmt.Errorf("%w: decision satisfactions are required", ErrInvalidResult)
	}
	for _, satisfaction := range d.Satisfactions {
		if err := satisfaction.Validate(); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(d.Unknowns))
	previous := ""
	for _, unknown := range d.Unknowns {
		if !validUnknown(unknown) {
			return fmt.Errorf("%w: unknown %q is outside the closed vocabulary", ErrInvalidResult, unknown)
		}
		if _, dup := seen[unknown]; dup {
			return fmt.Errorf("%w: duplicate unknown %q", ErrInvalidResult, unknown)
		}
		seen[unknown] = struct{}{}
		if unknown < previous {
			return fmt.Errorf("%w: unknowns are not sorted", ErrInvalidResult)
		}
		previous = unknown
	}
	return nil
}

func (d CandidateDecision) body() []byte {
	unknowns := append([]string(nil), d.Unknowns...)
	sort.Strings(unknowns)
	w := canonicalbytes.New("hcmnext.domains.matching.CandidateDecision", schemaVersion).
		Value("candidate_ref", d.CandidateRef).Bool("eligible", d.Eligible).
		Int("rank", int64(d.Rank)).Value("score", d.Score).
		Count("satisfactions", len(d.Satisfactions))
	for _, satisfaction := range d.Satisfactions {
		w.Value("satisfaction", satisfaction)
	}
	w.SortedStrings("unknown", unknowns)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Canonical implements canonicalbytes.Canonicalizer.
func (d CandidateDecision) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	return d.body()
}

// ResultExplanation is the bound, per-candidate account of one ranking.
type ResultExplanation struct {
	RequestID       string
	RequestDigest   string
	RankingDigest   string
	CandidateSource string
	HardConstraints []ConstraintKind
	ScoreVersion    uint64
	TieBreak        TieBreakPolicy
	FairnessRef     string
	FairnessVersion string
	Decisions       []CandidateDecision
	Unknowns        []string
	CanonicalDigest string
}

// Validate implements validation.
func (e ResultExplanation) Validate() error {
	if strings.TrimSpace(e.RequestID) == "" || strings.TrimSpace(e.RequestDigest) == "" || strings.TrimSpace(e.RankingDigest) == "" {
		return fmt.Errorf("%w: request and ranking binding are required", ErrInvalidResult)
	}
	if strings.TrimSpace(e.CandidateSource) == "" {
		return fmt.Errorf("%w: candidate source is required", ErrInvalidResult)
	}
	for _, kind := range e.HardConstraints {
		if !kind.Valid() {
			return fmt.Errorf("%w: hard constraint %q is not declared", ErrInvalidResult, kind)
		}
	}
	if e.ScoreVersion == 0 || e.TieBreak != TieBreakCandidateRef {
		return fmt.Errorf("%w: explanation cites no declared ranking contract", ErrInvalidResult)
	}
	if strings.TrimSpace(e.FairnessRef) == "" || strings.TrimSpace(e.FairnessVersion) == "" {
		return fmt.Errorf("%w: fairness contract is required", ErrInvalidResult)
	}
	if len(e.Decisions) == 0 {
		return fmt.Errorf("%w: decisions are required", ErrInvalidResult)
	}
	for i, decision := range e.Decisions {
		if err := decision.Validate(); err != nil {
			return fmt.Errorf("decision %d: %w", i, err)
		}
		if decision.Rank != i+1 {
			return fmt.Errorf("%w: decision rank %d at index %d", ErrInvalidResult, decision.Rank, i)
		}
	}
	seen := make(map[string]struct{}, len(e.Unknowns))
	previous := ""
	for _, unknown := range e.Unknowns {
		if !validUnknown(unknown) {
			return fmt.Errorf("%w: unknown %q is outside the closed vocabulary", ErrInvalidResult, unknown)
		}
		if _, dup := seen[unknown]; dup {
			return fmt.Errorf("%w: duplicate unknown %q", ErrInvalidResult, unknown)
		}
		seen[unknown] = struct{}{}
		if unknown < previous {
			return fmt.Errorf("%w: unknowns are not sorted", ErrInvalidResult)
		}
		previous = unknown
	}
	if e.CanonicalDigest != "" && e.CanonicalDigest != e.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidResult)
	}
	return nil
}

func (e ResultExplanation) body() []byte {
	kinds := make([]string, 0, len(e.HardConstraints))
	for _, kind := range e.HardConstraints {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)
	unknowns := append([]string(nil), e.Unknowns...)
	sort.Strings(unknowns)
	w := canonicalbytes.New("hcmnext.domains.matching.ResultExplanation", schemaVersion).
		String("request_id", e.RequestID).String("request_digest", e.RequestDigest).
		String("ranking_digest", e.RankingDigest).String("candidate_source", e.CandidateSource).
		SortedStrings("hard_constraint", kinds).Int("score_version", int64(e.ScoreVersion)).
		String("tie_break", string(e.TieBreak)).String("fairness_ref", e.FairnessRef).
		String("fairness_version", e.FairnessVersion).Count("decisions", len(e.Decisions))
	for _, decision := range e.Decisions {
		w.Value("decision", decision)
	}
	w.SortedStrings("unknown", unknowns)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (e ResultExplanation) computedDigest() string {
	b := e.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// Explain renders the bounded human-readable account: counts only, never
// candidate content beyond the refs the decisions already carry.
func (e ResultExplanation) Explain() string {
	eligible := 0
	for _, decision := range e.Decisions {
		if decision.Eligible {
			eligible++
		}
	}
	return fmt.Sprintf("match explanation: eligible=%d excluded=%d unknowns=%d ranking=%s",
		eligible, len(e.Decisions)-eligible, len(e.Unknowns), e.RankingDigest)
}

// requestHardKinds lists every hard gate the request holds, including the
// required qualifications, which are hard by construction.
func requestHardKinds(request MatchRequest) []ConstraintKind {
	var kinds []ConstraintKind
	for _, constraint := range request.Constraints {
		if constraint.effectiveMode() == ConstraintHard {
			kinds = append(kinds, constraint.Kind)
		}
	}
	if len(request.RequiredQualificationRefs) > 0 {
		kinds = append(kinds, ConstraintQualification)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return kinds
}

// ExplainResult explains one ranking without changing it. It refuses when
// the ranking is not bound to the request, when a hard gate has no
// explanation, when a score factor leaves the closed vocabulary, when the
// rank order contradicts eligibility dominance, or when any reason names
// another candidate. It is pure: no rows, events, outbox entries, human
// work or provider requests.
func ExplainResult(request MatchRequest, result MatchResult) (ResultExplanation, error) {
	if err := request.Validate(); err != nil {
		return ResultExplanation{}, matchExplanationRejected("request", "invalid")
	}
	if err := result.Validate(); err != nil {
		return ResultExplanation{}, matchExplanationRejected("result", "invalid")
	}
	if result.RequestID != request.RequestID || result.RequestDigest != request.computedDigest() {
		return ResultExplanation{}, matchExplanationRejected("result", "binding-mismatch")
	}
	if result.CanonicalDigest == "" || result.CanonicalDigest != result.computedDigest() {
		return ResultExplanation{}, matchExplanationRejected("result", "digest-mismatch")
	}
	hard := requestHardKinds(request)
	ids := make(map[string]struct{}, len(result.Matches))
	for _, match := range result.Matches {
		ids[match.CandidateRef.Id] = struct{}{}
	}
	explained := ResultExplanation{
		RequestID: request.RequestID, RequestDigest: request.CanonicalDigest,
		RankingDigest: result.CanonicalDigest, CandidateSource: request.CandidateSourceRef.String(),
		HardConstraints: hard, ScoreVersion: result.ScoreVersion, TieBreak: result.TieBreak,
		FairnessRef: request.Fairness.PolicyRef, FairnessVersion: request.Fairness.Version,
	}
	unknownSet := make(map[string]struct{})
	seenIneligible := false
	for _, match := range result.Matches {
		if match.Eligible && seenIneligible {
			return ResultExplanation{}, matchExplanationRejected("result", "rank-mismatch")
		}
		if !match.Eligible {
			seenIneligible = true
		}
		present := make(map[ConstraintKind]struct{}, len(match.Satisfactions))
		for _, satisfaction := range match.Satisfactions {
			present[satisfaction.Kind] = struct{}{}
			for id := range ids {
				if id != match.CandidateRef.Id &&
					(strings.Contains(string(satisfaction.Reason), id) || strings.Contains(satisfaction.Detail, id)) {
					return ResultExplanation{}, matchExplanationRejected("match.satisfactions", "comparison-leakage")
				}
			}
		}
		for _, kind := range hard {
			if _, ok := present[kind]; !ok {
				return ResultExplanation{}, matchExplanationRejected("match.satisfactions", "missing-hard-constraint")
			}
		}
		decision := CandidateDecision{
			CandidateRef: match.CandidateRef, Eligible: match.Eligible, Rank: match.Rank,
			Score:         match.Score,
			Satisfactions: append([]ConstraintSatisfaction(nil), match.Satisfactions...),
		}
		decision.Score.Factors = append([]ScoreFactor(nil), match.Score.Factors...)
		for _, factor := range decision.Score.Factors {
			if !SatisfactionReason(factor.Reason).Valid() {
				return ResultExplanation{}, matchExplanationRejected("match.score", "unexplained-factor")
			}
			for id := range ids {
				if id != match.CandidateRef.Id && strings.Contains(factor.Reason, id) {
					return ResultExplanation{}, matchExplanationRejected("match.score", "comparison-leakage")
				}
			}
		}
		for _, satisfaction := range decision.Satisfactions {
			if satisfaction.Mode == ConstraintSoft && satisfaction.Status == SatisfactionUnsatisfied {
				token := unknownPrefix + string(satisfaction.Kind)
				unknownSet[token] = struct{}{}
				decision.Unknowns = append(decision.Unknowns, token)
			}
		}
		sort.Strings(decision.Unknowns)
		explained.Decisions = append(explained.Decisions, decision)
	}
	for token := range unknownSet {
		explained.Unknowns = append(explained.Unknowns, token)
	}
	sort.Strings(explained.Unknowns)
	explained.CanonicalDigest = explained.computedDigest()
	if err := explained.Validate(); err != nil {
		return ResultExplanation{}, matchExplanationRejected("explanation", "invalid")
	}
	return explained, nil
}
