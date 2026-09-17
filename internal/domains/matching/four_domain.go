// Four-domain matching conformance: MATCH-007 proves the shared
// candidate, filter, rank and explain contracts across Scheduling,
// Recruiting, Mobility and Learning.
//
// Proving binds four ranked results under one shared ranking contract. A
// protected attribute that changes rank, a hard constraint without an
// explanation, or a reason that compares candidates refuses as
// MATCH_007_REJECTED. Proving is pure: it persists nothing, assigns
// nobody, and never turns a recommendation into a transaction.
package matching

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// MatchConformanceRejectedCode is the stable machine-readable refusal code.
const MatchConformanceRejectedCode = "MATCH_007_REJECTED"

// ErrMatchConformanceRejected is the sentinel for refused four-domain
// proofs. Match it with errors.Is rather than parsing the reason.
var ErrMatchConformanceRejected = errors.New("matching: four-domain conformance rejected")

// MatchConformanceRejectedError names the offending field and version.
type MatchConformanceRejectedError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *MatchConformanceRejectedError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// Is reports ErrMatchConformanceRejected without parsing the reason.
func (e *MatchConformanceRejectedError) Is(target error) bool {
	return target == ErrMatchConformanceRejected
}

// AsMatchConformanceRejected unwraps a MATCH_007_REJECTED refusal.
func AsMatchConformanceRejected(err error) (*MatchConformanceRejectedError, bool) {
	var rejected *MatchConformanceRejectedError
	if errors.As(err, &rejected) && rejected.Code == MatchConformanceRejectedCode {
		return rejected, true
	}
	return nil, false
}

func matchConformanceRejected(field, state string) *MatchConformanceRejectedError {
	return &MatchConformanceRejectedError{Code: MatchConformanceRejectedCode, Field: field, State: state, Version: schemaVersion}
}

// MatchDomain is the closed vocabulary of proven matching domains.
type MatchDomain string

// The four conformance domains.
const (
	DomainScheduling MatchDomain = "SCHEDULING"
	DomainRecruiting MatchDomain = "RECRUITING"
	DomainMobility   MatchDomain = "MOBILITY"
	DomainLearning   MatchDomain = "LEARNING"
)

// Valid reports whether the domain is declared.
func (d MatchDomain) Valid() bool {
	switch d {
	case DomainScheduling, DomainRecruiting, DomainMobility, DomainLearning:
		return true
	default:
		return false
	}
}

// DomainRun binds one domain to its exact request and ranked result.
type DomainRun struct {
	Domain  MatchDomain
	Request MatchRequest
	Result  MatchResult
}

// FourDomainConformance is the bound verdict: the ranked digest per
// domain under one shared ranking contract. It carries digests and
// policy references only — no candidate identities and no effects.
type FourDomainConformance struct {
	Domains         []MatchDomain
	RankingDigests  []string
	ScoreVersion    uint64
	TieBreak        TieBreakPolicy
	CanonicalDigest string
}

func (v FourDomainConformance) body() []byte {
	// Domains and digests stay index-aligned in domain-sorted order, so
	// the verdict binds each domain to its ranking. Prove always stores
	// domain-sorted order, which keeps the digest input deterministic.
	w := canonicalbytes.New("hcmnext.domains.matching.FourDomainConformance", schemaVersion).
		Int("score_version", int64(v.ScoreVersion)).String("tie_break", string(v.TieBreak)).
		Count("domains", len(v.Domains))
	for i, domain := range v.Domains {
		digest := ""
		if i < len(v.RankingDigests) {
			digest = v.RankingDigests[i]
		}
		w.String("domain", string(domain)).String("ranking_digest", digest)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Canonical implements canonicalbytes.Canonicalizer.
func (v FourDomainConformance) Canonical() []byte {
	if len(v.Domains) != 4 || len(v.RankingDigests) != 4 || v.ScoreVersion == 0 || v.TieBreak != TieBreakCandidateRef {
		return nil
	}
	return v.body()
}

func (v FourDomainConformance) computedDigest() string {
	b := v.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// Explain renders the bounded human-readable account: domains, ranking
// count and shared contract only — never candidate content.
func (v FourDomainConformance) Explain() string {
	domains := make([]string, 0, len(v.Domains))
	for _, domain := range v.Domains {
		domains = append(domains, string(domain))
	}
	sort.Strings(domains)
	return fmt.Sprintf("four-domain conformance %s: rankings=%d score_version=%d",
		strings.Join(domains, ","), len(v.RankingDigests), v.ScoreVersion)
}

// ProveFourDomainConformance proves the shared contracts across exactly
// the four domains. Every run must bind its request, share one ranking
// contract, explain every hard constraint and stay inside the closed
// reason vocabulary. It is pure: no rows, events, outbox entries, human
// work or provider requests.
func ProveFourDomainConformance(runs []DomainRun) (FourDomainConformance, error) {
	if len(runs) != 4 {
		return FourDomainConformance{}, matchConformanceRejected("domains", "count")
	}
	seen := make(map[MatchDomain]struct{}, len(runs))
	for _, run := range runs {
		if !run.Domain.Valid() {
			return FourDomainConformance{}, matchConformanceRejected("domain", "unknown")
		}
		if _, dup := seen[run.Domain]; dup {
			return FourDomainConformance{}, matchConformanceRejected("domains", "duplicate")
		}
		seen[run.Domain] = struct{}{}
	}
	for _, domain := range []MatchDomain{DomainScheduling, DomainRecruiting, DomainMobility, DomainLearning} {
		if _, ok := seen[domain]; !ok {
			return FourDomainConformance{}, matchConformanceRejected("domains", "incomplete")
		}
	}
	for _, run := range runs {
		if err := run.Request.Validate(); err != nil {
			return FourDomainConformance{}, matchConformanceRejected("request", "invalid")
		}
		if err := run.Result.Validate(); err != nil {
			return FourDomainConformance{}, matchConformanceRejected("result", "invalid")
		}
		if run.Result.RequestID != run.Request.RequestID || run.Result.RequestDigest != run.Request.computedDigest() {
			return FourDomainConformance{}, matchConformanceRejected("result", "binding-mismatch")
		}
		if run.Result.CanonicalDigest == "" || run.Result.CanonicalDigest != run.Result.computedDigest() {
			return FourDomainConformance{}, matchConformanceRejected("result", "digest-mismatch")
		}
	}
	base := runs[0].Result
	for _, run := range runs[1:] {
		if run.Result.ScoreVersion != base.ScoreVersion || run.Result.TieBreak != base.TieBreak {
			return FourDomainConformance{}, matchConformanceRejected("ranking", "contract-diverged")
		}
		if run.Request.Ranking.ScoreVersion != base.ScoreVersion || run.Request.Ranking.TieBreak != base.TieBreak {
			return FourDomainConformance{}, matchConformanceRejected("ranking", "contract-diverged")
		}
	}
	for _, run := range runs {
		if err := proveRunFactors(run); err != nil {
			return FourDomainConformance{}, err
		}
	}
	ordered := append([]DomainRun(nil), runs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Domain < ordered[j].Domain })
	verdict := FourDomainConformance{ScoreVersion: base.ScoreVersion, TieBreak: base.TieBreak}
	for _, run := range ordered {
		verdict.Domains = append(verdict.Domains, run.Domain)
		verdict.RankingDigests = append(verdict.RankingDigests, run.Result.CanonicalDigest)
	}
	verdict.CanonicalDigest = verdict.computedDigest()
	return verdict, nil
}

// proveRunFactors checks one ranked result explains every hard constraint
// and keeps every score factor inside the closed, candidate-free reason
// vocabulary. A protected attribute or proxy cannot name a closed reason,
// so it cannot change rank through this gate.
func proveRunFactors(run DomainRun) error {
	identities := make(map[string]struct{}, len(run.Result.Matches))
	for _, match := range run.Result.Matches {
		identities[match.CandidateRef.Id] = struct{}{}
	}
	for _, match := range run.Result.Matches {
		for _, satisfaction := range match.Satisfactions {
			if satisfaction.Mode == ConstraintHard && strings.TrimSpace(satisfaction.Detail) == "" {
				return matchConformanceRejected("constraint", "unexplained-hard-constraint")
			}
		}
		for _, factor := range match.Score.Factors {
			if !SatisfactionReason(factor.Reason).Valid() {
				return matchConformanceRejected("score", "unexplained-factor")
			}
			for id := range identities {
				if id != "" && strings.Contains(factor.Reason, id) {
					return matchConformanceRejected("score", "comparison-leakage")
				}
			}
		}
	}
	return nil
}
