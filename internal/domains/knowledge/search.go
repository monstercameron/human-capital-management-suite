package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	searchengine "github.com/monstercameron/human-capital-management-suite/internal/engines/search"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

var (
	ErrSearchRequest     = errors.New("knowledge: invalid search request")
	ErrSearchUnavailable = errors.New("knowledge: search source unavailable")
)

// SearchableArticle is an active, retained article revision plus its explicit
// authorization scope. The store must establish tenant scope before returning
// candidates; the service then applies current role and audience claims.
type SearchableArticle struct {
	Article ArticleRevision
}

// SearchSource returns only activated articles for the tenant and locale.
// Query is a hint for efficient retrieval; SearchService repeats the match
// against the returned title and summary before disclosing anything.
type SearchSource interface {
	SearchCandidates(context.Context, string, string, string, string, []string, time.Time) ([]SearchableArticle, error)
}

// SearchRequest carries server-verified tenant, role, and audience claims.
// Callers must construct it from the authenticated principal, never request
// parameters or UI state.
type SearchRequest struct {
	TenantID string
	Query    string
	Locale   string
	Audience string
	Roles    []string
	At       time.Time
}

// SearchResult intentionally contains presentation-safe fields only.
type SearchResult struct {
	ArticleID string
	Revision  uint64
	Locale    string
	Title     string
	Summary   string
}

// SearchMatch is one presentation-safe search result with the authorization
// and source evidence produced by the shared search engine.
type SearchMatch struct {
	SearchResult
	SourceDigest          string
	PolicyDigest          string
	AuthorizationEvidence string
	Score                 int64
}

// SearchOutcome binds visible matches and non-identifying suppression
// evidence to the engine query plan.
type SearchOutcome struct {
	Matches          []SearchMatch
	EnvelopeDigest   string
	SuppressedDigest string
}

// SearchService authorizes and searches published, current knowledge articles.
type SearchService struct{ Source SearchSource }

// AudienceForRoles resolves the broadest help audience carried by a trusted
// effective role set. Article role scopes remain an independent check.
func AudienceForRoles(roles []string) string {
	for _, role := range roles {
		switch strings.ToLower(strings.TrimSpace(role)) {
		case "manager", "hiring_manager", "payroll_manager", "hr_partner", "hcm_admin", "comp_admin":
			return "MANAGERS"
		}
	}
	return "EMPLOYEES"
}

// Search returns only articles authorized for at least one authenticated role
// and the requested audience. Missing source, identity, role, time, or query
// data fails closed. Unauthorized and unmatched articles share an empty result.
func (s SearchService) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	outcome, err := s.SearchDetailed(ctx, req)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, len(outcome.Matches))
	for i, match := range outcome.Matches {
		results[i] = match.SearchResult
	}
	return results, nil
}

// SearchDetailed executes the live authorized ranker. Source reads remain
// tenant, locale, audience and role scoped; the engine repeats current article
// policy checks before text is passed to the ranker.
func (s SearchService) SearchDetailed(ctx context.Context, req SearchRequest) (SearchOutcome, error) {
	if s.Source == nil {
		return SearchOutcome{}, ErrSearchUnavailable
	}
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.Query) == "" ||
		strings.TrimSpace(req.Locale) == "" || strings.TrimSpace(req.Audience) == "" || req.At.IsZero() || len(req.Roles) == 0 {
		return SearchOutcome{}, fmt.Errorf("%w: tenant, query, locale, audience, roles and time are required", ErrSearchRequest)
	}
	query := normalizedQuery(req.Query)
	if len(query) == 0 {
		return SearchOutcome{}, fmt.Errorf("%w: query has no searchable terms", ErrSearchRequest)
	}
	candidates, err := s.Source.SearchCandidates(ctx, req.TenantID, req.Locale, strings.Join(query, " "), req.Audience, req.Roles, req.At)
	if err != nil {
		return SearchOutcome{}, fmt.Errorf("%w: %v", ErrSearchUnavailable, err)
	}
	tenant := values.TenantId(req.TenantID)
	if err := tenant.Validate(); err != nil {
		return SearchOutcome{}, fmt.Errorf("%w: tenant: %v", ErrSearchRequest, err)
	}
	instant := values.NewInstant(req.At)
	knownAt, err := values.NewKnownAt(instant)
	if err != nil {
		return SearchOutcome{}, fmt.Errorf("%w: time: %v", ErrSearchRequest, err)
	}
	roles := append([]string(nil), req.Roles...)
	sort.Strings(roles)
	policyDigest := digestText("knowledge-policy/v1|" + req.TenantID + "|" + req.Locale + "|" + req.Audience + "|" + strings.Join(roles, ","))
	engineQuery := strings.Join(query, " ")
	queryDigest := searchengine.QueryDigest(engineQuery)
	articleBySubject := make(map[string]ArticleRevision, len(candidates))
	engineCandidates := make([]searchengine.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		a := candidate.Article
		classification := dlp.DataClass(a.Classification)
		if !classification.Valid() {
			continue
		}
		identity := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s:%d", a.ArticleID, a.Revision))).String()
		subject := values.EntityRef{Tenant: tenant, Kind: "knowledge_article", Id: identity}
		sourceDigest := knowledgeSearchSourceDigest(a)
		if sourceDigest == "" {
			continue
		}
		articleBySubject[subject.String()] = a
		engineCandidates = append(engineCandidates, searchengine.Candidate{
			Subject: subject, SearchText: a.Title + " " + a.Summary,
			SourceDigest: sourceDigest, PolicyDigest: policyDigest,
			Purpose: "knowledge.search", Classification: classification,
			Fields:      []authz.FieldID{"knowledge.article.title", "knowledge.article.summary"},
			EffectiveAt: instant, KnownAt: knownAt, Watermark: instant,
		})
	}
	indexDigests := make([]string, 0, len(engineCandidates))
	for _, candidate := range engineCandidates {
		indexDigests = append(indexDigests, candidate.SourceDigest)
	}
	sort.Strings(indexDigests)
	indexDigest := digestText(strings.Join(indexDigests, "\n"))
	var outcome SearchOutcome
	classifications := make(map[dlp.DataClass]struct{})
	for _, candidate := range engineCandidates {
		classifications[candidate.Classification] = struct{}{}
	}
	classNames := make([]string, 0, len(classifications))
	for classification := range classifications {
		classNames = append(classNames, string(classification))
	}
	sort.Strings(classNames)
	proofs := make([]string, 0, len(classNames))
	for _, className := range classNames {
		classification := dlp.DataClass(className)
		group := make([]searchengine.Candidate, 0)
		for _, candidate := range engineCandidates {
			if candidate.Classification == classification {
				group = append(group, candidate)
			}
		}
		envelope := searchengine.QueryEnvelope{
			Tenant: tenant, Purpose: "knowledge.search", Classification: classification,
			Fields:      []authz.FieldID{"knowledge.article.title", "knowledge.article.summary"},
			EffectiveAt: instant, KnownAt: knownAt, PolicyDigest: policyDigest,
			QueryDigest: queryDigest, SemanticPlanDigest: digestText("knowledge.lexical/v1"),
			IndexDigest: indexDigest, MinimumWatermark: instant,
			Mode: searchengine.ModeExact, MaxResults: 50,
		}
		authorizer := searchengine.AuthorizerFunc(func(_ context.Context, e searchengine.QueryEnvelope, c searchengine.Candidate) (searchengine.AuthorizationDecision, error) {
			a := articleBySubject[c.Subject.String()]
			allowed := searchRoleAllowed(req.Roles, a.AuthorizedRoles) && searchAudienceAllowed(req.Audience, a.AudienceScope) &&
				a.RetentionScheduleRef != "" && (!a.RetainUntil.IsSet() || req.At.Before(a.RetainUntil.Time())) &&
				a.Locale == req.Locale && a.Supersession == nil && req.At.Before(a.Review.ExpiresAt.Time()) &&
				!req.At.Before(a.EffectiveInterval.EffectiveFrom.Time()) &&
				(!a.EffectiveInterval.EffectiveTo.IsSet() || req.At.Before(a.EffectiveInterval.EffectiveTo.Time()))
			evidence := digestText(e.PolicyDigest + "|" + c.SourceDigest + "|" + strings.Join(roles, ",") + "|" + req.Audience)
			return searchengine.AuthorizationDecision{Allowed: allowed, PolicyDigest: e.PolicyDigest, Evidence: evidence}, nil
		})
		runtime := searchengine.Runtime{
			Source: searchengine.CandidateSourceFunc(func(context.Context, searchengine.QueryEnvelope) ([]searchengine.Candidate, error) {
				return group, nil
			}),
			Authorizer: authorizer,
			Ranker:     searchengine.LexicalRanker{},
		}
		results, proof, execErr := runtime.Search(ctx, searchengine.RuntimeRequest{Envelope: envelope, QueryText: engineQuery})
		if execErr != nil {
			return SearchOutcome{}, fmt.Errorf("%w: engine execution: %v", ErrSearchUnavailable, execErr)
		}
		proofs = append(proofs, proof.EnvelopeDigest+":"+proof.SuppressedDigest)
		for _, result := range results {
			a := articleBySubject[result.Subject.String()]
			outcome.Matches = append(outcome.Matches, SearchMatch{
				SearchResult: SearchResult{ArticleID: a.ArticleID, Revision: a.Revision, Locale: a.Locale, Title: a.Title, Summary: a.Summary},
				SourceDigest: result.SourceDigest, PolicyDigest: result.PolicyDigest,
				AuthorizationEvidence: result.AuthorizationEvidence, Score: result.Score,
			})
		}
	}
	sort.Slice(outcome.Matches, func(i, j int) bool {
		if outcome.Matches[i].Score != outcome.Matches[j].Score {
			return outcome.Matches[i].Score > outcome.Matches[j].Score
		}
		if outcome.Matches[i].Title != outcome.Matches[j].Title {
			return outcome.Matches[i].Title < outcome.Matches[j].Title
		}
		return outcome.Matches[i].ArticleID < outcome.Matches[j].ArticleID
	})
	outcome.EnvelopeDigest = digestText(strings.Join(proofs, "\n"))
	outcome.SuppressedDigest = digestText("knowledge.suppressed/v1|" + strings.Join(proofs, "\n"))
	return outcome, nil
}

func digestText(value string) string { return digestBytes([]byte(value)) }

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func knowledgeSearchSourceDigest(article ArticleRevision) string {
	roles := append([]string(nil), article.AuthorizedRoles...)
	sort.Strings(roles)
	w := canonicalbytes.New("knowledge.search-source", 1).
		String("article_id", article.ArticleID).
		Int("revision", int64(article.Revision)).
		String("locale", article.Locale).
		String("audience", article.AudienceScope).
		String("classification", article.Classification).
		SortedStrings("authorized_roles", roles).
		String("body_digest", article.BodyDigest).
		String("title", article.Title).
		String("summary", article.Summary)
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return digestBytes(raw)
}

func searchRoleAllowed(claims, articleRoles []string) bool {
	for _, claim := range claims {
		for _, role := range articleRoles {
			if strings.EqualFold(strings.TrimSpace(claim), strings.TrimSpace(role)) && strings.TrimSpace(role) != "" {
				return true
			}
		}
	}
	return false
}

func searchAudienceAllowed(request, article string) bool {
	rank := func(value string) (int, bool) {
		switch strings.ToUpper(strings.TrimSpace(value)) {
		case "PUBLIC":
			return 0, true
		case "EMPLOYEES":
			return 1, true
		case "MANAGERS":
			return 2, true
		default:
			return 0, false
		}
	}
	reader, ok := rank(request)
	content, ok2 := rank(article)
	return ok && ok2 && content <= reader
}

func normalizedQuery(raw string) []string {
	parts := strings.FieldsFunc(strings.ToLower(raw), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127) })
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if _, ok := seen[part]; !ok {
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out
}
