package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	// ErrResolveRejected is the KNOW-003 seeded-defect sentinel. Uncited,
	// stale, injected or invalidated knowledge that matches the query must
	// fail with this error carrying the offending field, state and version.
	ErrResolveRejected = errors.New("KNOW_003_REJECTED")
)

// classificationRank orders clearances from widest to narrowest audience. A
// reader resolves an article only when the reader clearance dominates the
// article classification.
var classificationRank = map[string]int{
	"PUBLIC": 0, "INTERNAL": 1, "CONFIDENTIAL": 2, "RESTRICTED": 3,
}

// ResolveQuery is the context an answer must satisfy: tenant, organization,
// persona, jurisdiction, point in time, purpose and reader clearance.
type ResolveQuery struct {
	TenantID     string
	OrgID        string
	Persona      string
	Jurisdiction string
	At           time.Time
	Purpose      string
	Clearance    string
}

func (q ResolveQuery) Validate() error {
	if strings.TrimSpace(q.TenantID) == "" || strings.TrimSpace(q.Persona) == "" ||
		strings.TrimSpace(q.Jurisdiction) == "" || strings.TrimSpace(q.Purpose) == "" {
		return fmt.Errorf("%w: tenant, persona, jurisdiction and purpose are required", ErrResolveRejected)
	}
	if q.At.IsZero() {
		return fmt.Errorf("%w: resolution time is required", ErrResolveRejected)
	}
	if _, ok := classificationRank[strings.ToUpper(q.Clearance)]; !ok {
		return fmt.Errorf("%w: clearance is not declared", ErrResolveRejected)
	}
	return nil
}

// ResolvableArticle is a published article plus the pipeline verdicts the
// resolver enforces: tenant/org/persona/purpose scope, injection taint and
// source invalidation. The resolver is pure: it persists nothing.
type ResolvableArticle struct {
	Article     ArticleRevision
	TenantID    string
	OrgIDs      []string
	Personae    []string
	Purposes    []string
	Injected    bool
	Invalidated bool
}

// ResolveRejection is the stable KNOW-003 failure shape.
type ResolveRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *ResolveRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrResolveRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the KNOW_003_REJECTED sentinel to errors.Is.
func (r *ResolveRejection) Unwrap() error { return ErrResolveRejected }

func resolveReject(field, state, version, reason string) error {
	return &ResolveRejection{Field: field, State: state, Version: version, Reason: reason}
}

// Resolution is the KNOW-003 result: authorized applicable articles plus the
// explicit unknown/conflict report. Stale sources are never returned.
type Resolution struct {
	Articles  []ArticleRevision
	Conflicts []string
	Unknown   []string
	Digest    string
}

func (c ResolvableArticle) inScope(q ResolveQuery) bool {
	if c.TenantID != q.TenantID {
		return false
	}
	if len(c.OrgIDs) > 0 {
		found := false
		for _, org := range c.OrgIDs {
			if org == q.OrgID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	persona := false
	for _, p := range c.Personae {
		if strings.EqualFold(p, q.Persona) {
			persona = true
			break
		}
	}
	if !persona {
		return false
	}
	purpose := false
	for _, p := range c.Purposes {
		if strings.EqualFold(p, q.Purpose) {
			purpose = true
			break
		}
	}
	if !purpose {
		return false
	}
	if !strings.EqualFold(c.Article.Jurisdiction, q.Jurisdiction) && !strings.EqualFold(c.Article.Jurisdiction, "GLOBAL") {
		return false
	}
	clearance, ok := classificationRank[strings.ToUpper(q.Clearance)]
	article, ok2 := classificationRank[strings.ToUpper(c.Article.Classification)]
	if !ok || !ok2 || article > clearance {
		return false
	}
	from := c.Article.EffectiveInterval.EffectiveFrom.Time()
	if q.At.Before(from) {
		return false
	}
	if to := c.Article.EffectiveInterval.EffectiveTo; to.IsSet() && !q.At.Before(to.Time()) {
		return false
	}
	return true
}

func resolutionDigest(q ResolveQuery, articles []ArticleRevision) string {
	ids := make([]string, 0, len(articles))
	for _, a := range articles {
		ids = append(ids, fmt.Sprintf("%s#%d", a.ArticleID, a.Revision))
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(append([]string{
		q.TenantID, q.OrgID, q.Persona, q.Jurisdiction,
		q.At.UTC().Format(time.RFC3339Nano), q.Purpose, strings.ToUpper(q.Clearance),
	}, ids...), "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ResolveKnowledge returns only authorized articles applicable to the query
// context and explicitly reports conflict/unknown/stale sources. In-scope
// articles that are uncited, superseded, injected or invalidated fail the
// whole resolution with KNOW_003_REJECTED: unsafe knowledge is never
// publishable. Out-of-scope candidates are excluded silently.
func ResolveKnowledge(q ResolveQuery, candidates []ResolvableArticle) (Resolution, error) {
	const version = "knowledge-resolve/v1"
	if err := q.Validate(); err != nil {
		return Resolution{}, err
	}
	var applicable []ArticleRevision
	var conflicts []string
	seen := map[string]string{}
	for _, c := range candidates {
		if !c.inScope(q) {
			continue
		}
		a := c.Article
		switch {
		case len(a.SourceRefs) == 0:
			return Resolution{}, resolveReject("article.source_refs", "UNCITED", version, fmt.Sprintf("article %q has no citations", a.ArticleID))
		case a.Supersession != nil:
			return Resolution{}, resolveReject("article.supersession", "STALE", version, fmt.Sprintf("article %q was superseded by %q", a.ArticleID, a.Supersession.SupersedingArticleID))
		case c.Injected:
			return Resolution{}, resolveReject("article.provenance", "INJECTED", version, fmt.Sprintf("article %q arrived from an untrusted source", a.ArticleID))
		case c.Invalidated:
			return Resolution{}, resolveReject("article.validity", "INVALIDATED", version, fmt.Sprintf("article %q was invalidated by its source", a.ArticleID))
		}
		if prev, dup := seen[a.ArticleID]; dup && prev != a.BodyDigest {
			conflicts = append(conflicts, a.ArticleID)
		}
		seen[a.ArticleID] = a.BodyDigest
		applicable = append(applicable, a)
	}
	sort.Slice(applicable, func(i, j int) bool {
		if applicable[i].ArticleID != applicable[j].ArticleID {
			return applicable[i].ArticleID < applicable[j].ArticleID
		}
		return applicable[i].Revision < applicable[j].Revision
	})
	var unknown []string
	if len(applicable) == 0 {
		unknown = []string{fmt.Sprintf("no applicable article for persona %q jurisdiction %q purpose %q", q.Persona, q.Jurisdiction, q.Purpose)}
	}
	return Resolution{
		Articles:  applicable,
		Conflicts: conflicts,
		Unknown:   unknown,
		Digest:    resolutionDigest(q, applicable),
	}, nil
}
