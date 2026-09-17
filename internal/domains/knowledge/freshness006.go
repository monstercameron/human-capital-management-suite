// KNOW-006: prove knowledge-answer freshness and citation safety.
//
// EvaluateAnswerFreshness gates one composed answer on the cited
// sources: every claim must cite a current, in-scope, authorized,
// non-injected, non-invalidated article. Stale, uncited, injected,
// invalidated or unauthorized content is never publishable: it returns
// KNOW_006_REJECTED with field, state and version and persists nothing.
// Conflicting cited versions yield an explicit conflict report, never an
// uncited fabrication. The gate is kernel-pure and reuses the KNOW-003
// ResolvableArticle verdicts.
package knowledge

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// FreshnessVersion is the rejection version for the KNOW-006 gate.
const FreshnessVersion = "knowledge-freshness/v1"

// MaxCitationAge bounds how old a citation retrieval may be before the
// cited knowledge counts as stale.
const MaxCitationAge = 30 * 24 * time.Hour

var (
	// ErrFreshnessRejected is the KNOW-006 seeded-defect sentinel.
	// Uncited, stale, injected or invalidated knowledge that remains
	// publishable would be the defect; instead it fails with this error
	// carrying the offending field, state and version.
	ErrFreshnessRejected = errors.New("KNOW_006_REJECTED")
)

// FreshnessRejection is the stable KNOW-006 failure shape.
type FreshnessRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *FreshnessRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrFreshnessRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the KNOW_006_REJECTED sentinel to errors.Is.
func (r *FreshnessRejection) Unwrap() error { return ErrFreshnessRejected }

func freshnessReject(field, state, reason string) error {
	return &FreshnessRejection{Field: field, State: state, Version: FreshnessVersion, Reason: reason}
}

// AnswerCitation is one claim-to-source binding in a composed answer.
type AnswerCitation struct {
	ClaimRef    string
	ArticleID   string
	Version     uint64
	RetrievedAt time.Time
}

// AnswerCandidate is one composed answer awaiting the freshness gate.
// ContentDigest seals the prose; Citations bind every claim; Articles
// carries the cited sources with their pipeline verdicts.
type AnswerCandidate struct {
	ContentDigest string
	Citations     []AnswerCitation
	Articles      []ResolvableArticle
}

// AnswerDecision is the closed KNOW-006 outcome vocabulary.
type AnswerDecision string

const (
	AnswerPublishable AnswerDecision = "PUBLISHABLE"
	AnswerBlocked     AnswerDecision = "BLOCKED"
	AnswerEscalated   AnswerDecision = "ESCALATED"
)

// AnswerVerdict is the deterministic gate outcome.
type AnswerVerdict struct {
	Decision  AnswerDecision
	Cited     []string
	Conflicts []string
	Unknown   []string
	Digest    string
}

func (v AnswerVerdict) computedDigest(query ResolveQuery, answer AnswerCandidate) string {
	cites := make([]string, 0, len(answer.Citations))
	for _, c := range answer.Citations {
		cites = append(cites, strings.Join([]string{c.ClaimRef, c.ArticleID, fmt.Sprintf("%d", c.Version)}, "\x00"))
	}
	sort.Strings(cites)
	w := canonicalbytes.New("hcmnext.domains.knowledge.AnswerVerdict", 1).
		String("tenant", query.TenantID).
		String("persona", query.Persona).
		String("content", answer.ContentDigest).
		String("decision", string(v.Decision)).
		SortedStrings("citations", cites).
		SortedStrings("cited", append([]string(nil), v.Cited...)).
		SortedStrings("conflicts", append([]string(nil), v.Conflicts...)).
		SortedStrings("unknown", append([]string(nil), v.Unknown...))
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// EvaluateAnswerFreshness gates one composed answer on citation safety.
// Conflicting cited versions and unknown coverage escalate with an
// explicit report; everything unsafe is refused.
func EvaluateAnswerFreshness(query ResolveQuery, answer AnswerCandidate) (AnswerVerdict, error) {
	if err := query.Validate(); err != nil {
		return AnswerVerdict{}, freshnessReject("freshness.query", "INVALID", fmt.Sprintf("query is invalid: %v", err))
	}
	if strings.TrimSpace(answer.ContentDigest) == "" {
		return AnswerVerdict{}, freshnessReject("freshness.content", "UNCITED", "answer prose without a content digest is not gateable")
	}
	if len(answer.Citations) == 0 {
		return AnswerVerdict{}, freshnessReject("freshness.citations", "UNCITED", "an answer with no citations is not publishable")
	}
	byID := make(map[string]ResolvableArticle, len(answer.Articles))
	for _, a := range answer.Articles {
		byID[a.Article.ArticleID] = a
	}
	verdict := AnswerVerdict{}
	versions := map[string]map[uint64]bool{}
	for i, c := range answer.Citations {
		if strings.TrimSpace(c.ClaimRef) == "" || strings.TrimSpace(c.ArticleID) == "" || c.Version == 0 {
			return AnswerVerdict{}, freshnessReject(fmt.Sprintf("freshness.citations[%d]", i), "UNCITED", "every claim binds article and version")
		}
		if c.RetrievedAt.IsZero() {
			return AnswerVerdict{}, freshnessReject(fmt.Sprintf("freshness.citations[%d].retrieved_at", i), "STALE", "citation retrieval instant is required")
		}
		if query.At.Sub(c.RetrievedAt) > MaxCitationAge {
			return AnswerVerdict{}, freshnessReject(fmt.Sprintf("freshness.citations[%d]", i), "STALE", fmt.Sprintf("citation for %s is older than the freshness horizon", c.ArticleID))
		}
		src, ok := byID[c.ArticleID]
		if !ok {
			return AnswerVerdict{}, freshnessReject(fmt.Sprintf("freshness.citations[%d]", i), "UNCITED", fmt.Sprintf("claim cites unknown article %s", c.ArticleID))
		}
		if src.Injected {
			return AnswerVerdict{}, freshnessReject(fmt.Sprintf("freshness.citations[%d]", i), "INJECTED", fmt.Sprintf("article %s carries injection taint", c.ArticleID))
		}
		if src.Invalidated {
			return AnswerVerdict{}, freshnessReject(fmt.Sprintf("freshness.citations[%d]", i), "INVALIDATED", fmt.Sprintf("article %s was invalidated at source", c.ArticleID))
		}
		if rankOf(src.Article.Classification) > rankOf(query.Clearance) {
			return AnswerVerdict{}, freshnessReject(fmt.Sprintf("freshness.citations[%d]", i), "UNAUTHORIZED", fmt.Sprintf("article %s exceeds reader clearance", c.ArticleID))
		}
		if !src.inScope(query) {
			return AnswerVerdict{}, freshnessReject(fmt.Sprintf("freshness.citations[%d]", i), "OUT_OF_SCOPE", fmt.Sprintf("article %s is outside the query scope", c.ArticleID))
		}
		if src.Article.Revision != c.Version {
			verdict.Unknown = append(verdict.Unknown, fmt.Sprintf("%s: cited v%d, current v%d", c.ArticleID, c.Version, src.Article.Revision))
			continue
		}
		verdict.Cited = append(verdict.Cited, c.ArticleID)
		if versions[c.ArticleID] == nil {
			versions[c.ArticleID] = map[uint64]bool{}
		}
		versions[c.ArticleID][c.Version] = true
	}
	for id, set := range versions {
		if len(set) > 1 {
			verdict.Conflicts = append(verdict.Conflicts, id)
		}
	}
	sort.Strings(verdict.Cited)
	sort.Strings(verdict.Conflicts)
	sort.Strings(verdict.Unknown)
	switch {
	case len(verdict.Conflicts) > 0 || len(verdict.Unknown) > 0:
		verdict.Decision = AnswerEscalated
	default:
		verdict.Decision = AnswerPublishable
	}
	verdict.Digest = verdict.computedDigest(query, answer)
	return verdict, nil
}

func rankOf(classification string) int {
	if r, ok := classificationRank[strings.ToUpper(classification)]; ok {
		return r
	}
	return len(classificationRank)
}
