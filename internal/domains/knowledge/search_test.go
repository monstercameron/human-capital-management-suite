package knowledge_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/knowledge"
)

type searchSourceFixture struct {
	candidates     []knowledge.SearchableArticle
	tenant, locale string
	err            error
}

func (s *searchSourceFixture) SearchCandidates(_ context.Context, tenant, locale, _ string, _ string, _ []string, _ time.Time) ([]knowledge.SearchableArticle, error) {
	s.tenant, s.locale = tenant, locale
	return append([]knowledge.SearchableArticle(nil), s.candidates...), s.err
}

func searchArticle(t *testing.T, id, audience, title string, expiry string) knowledge.SearchableArticle {
	t.Helper()
	a := minimalArticle(t)
	a.ArticleID, a.AudienceScope, a.Title = id, audience, title
	a.Review.ExpiresAt = newInstant(t, expiry)
	a.AuthorizedRoles = []string{"worker_self"}
	a.RetentionScheduleRef = "records:knowledge/current"
	return knowledge.SearchableArticle{Article: a}
}

func TestTodo_REV_077_02(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	source := &searchSourceFixture{candidates: []knowledge.SearchableArticle{
		searchArticle(t, "leave", "EMPLOYEES", "Annual leave policy", "2027-01-01T00:00:00Z"),
		searchArticle(t, "manager", "MANAGERS", "Leave approval workflow", "2027-01-01T00:00:00Z"),
	}}
	service := knowledge.SearchService{Source: source}
	got, err := service.Search(context.Background(), knowledge.SearchRequest{TenantID: "tenant-a", Query: "leave policy", Locale: "en-US", Audience: "EMPLOYEES", Roles: []string{"worker_self"}, At: now})
	if err != nil {
		t.Fatal(err)
	}
	if source.tenant != "tenant-a" || source.locale != "en-US" {
		t.Fatalf("source scope tenant=%q locale=%q", source.tenant, source.locale)
	}
	if len(got) != 1 || got[0].ArticleID != "leave" || got[0].Title != "Annual leave policy" {
		t.Fatalf("results = %+v", got)
	}
}

func TestTodo_REV_077_02_Security(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	restricted := searchArticle(t, "restricted-secret", "MANAGERS", "Sensitive leave complaint", "2027-01-01T00:00:00Z")
	restricted.Article.AuthorizedRoles = []string{"hr_partner"}
	expired := searchArticle(t, "expired", "EMPLOYEES", "Old leave policy", "2026-05-01T00:00:00Z")
	expiredRetention := searchArticle(t, "expired-retention", "EMPLOYEES", "Leave retention cutoff", "2027-01-01T00:00:00Z")
	expiredRetention.Article.RetainUntil = newInstant(t, "2026-05-01T00:00:00Z")
	managerOnly := searchArticle(t, "manager-audience", "MANAGERS", "Manager leave policy", "2027-01-01T00:00:00Z")
	noRetention := searchArticle(t, "undeclared-retention", "EMPLOYEES", "Leave archive", "2027-01-01T00:00:00Z")
	noRetention.Article.RetentionScheduleRef = ""
	source := &searchSourceFixture{candidates: []knowledge.SearchableArticle{
		restricted,
		searchArticle(t, "wrong-role", "EMPLOYEES", "Leave policy details", "2027-01-01T00:00:00Z"),
		expired,
		expiredRetention,
		managerOnly,
		noRetention,
	}}
	service := knowledge.SearchService{Source: source}
	got, err := service.Search(context.Background(), knowledge.SearchRequest{TenantID: "tenant-a", Query: "leave", Locale: "en-US", Audience: "EMPLOYEES", Roles: []string{"worker_self"}, At: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ArticleID != "wrong-role" {
		t.Fatalf("unauthorized, expired or undeclared records disclosed: %+v", got)
	}
	if len(got) == 1 && got[0].ArticleID == "restricted-secret" {
		t.Fatal("restricted article metadata leaked")
	}

	if _, err := (knowledge.SearchService{}).Search(context.Background(), knowledge.SearchRequest{TenantID: "tenant-a", Query: "leave", Locale: "en-US", Audience: "EMPLOYEES", Roles: []string{"worker_self"}, At: now}); !errors.Is(err, knowledge.ErrSearchUnavailable) {
		t.Fatalf("nil source error=%v", err)
	}
	source.err = errors.New("database failure")
	if got, err := service.Search(context.Background(), knowledge.SearchRequest{TenantID: "tenant-a", Query: "leave", Locale: "en-US", Audience: "EMPLOYEES", Roles: []string{"worker_self"}, At: now}); len(got) != 0 || !errors.Is(err, knowledge.ErrSearchUnavailable) {
		t.Fatalf("source failure results=%+v error=%v", got, err)
	}
}

func TestTodo_REV_077_02_Golden(t *testing.T) {
	source := &searchSourceFixture{candidates: []knowledge.SearchableArticle{searchArticle(t, "leave", "EMPLOYEES", "Annual leave policy", "2027-01-01T00:00:00Z")}}
	got, err := (knowledge.SearchService{Source: source}).Search(context.Background(), knowledge.SearchRequest{TenantID: "tenant-a", Query: "Leave policy, leave", Locale: "en-US", Audience: "EMPLOYEES", Roles: []string{"worker_self"}, At: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if got := hex.EncodeToString(digest[:]); got != "49eaacdaa07b8d31422a75c995c82c813075e1f426609795a331920155813199" {
		t.Fatalf("search result golden digest = %s; payload=%s", got, raw)
	}
}

func TestTodo_REV_026_01(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	source := &searchSourceFixture{candidates: []knowledge.SearchableArticle{
		searchArticle(t, "leave-policy", "EMPLOYEES", "Annual leave policy", "2027-01-01T00:00:00Z"),
		searchArticle(t, "leave-approval", "EMPLOYEES", "Leave approval process", "2027-01-01T00:00:00Z"),
	}}
	outcome, err := (knowledge.SearchService{Source: source}).SearchDetailed(context.Background(), knowledge.SearchRequest{
		TenantID: "tenant-a", Query: "leave policy", Locale: "en-US", Audience: "EMPLOYEES",
		Roles: []string{"worker_self"}, At: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Matches) != 1 || outcome.Matches[0].ArticleID != "leave-policy" || outcome.Matches[0].Score == 0 {
		t.Fatalf("ranked matches = %+v", outcome.Matches)
	}
	match := outcome.Matches[0]
	if match.SourceDigest == "" || match.PolicyDigest == "" || match.AuthorizationEvidence == "" || outcome.EnvelopeDigest == "" || outcome.SuppressedDigest == "" {
		t.Fatalf("search omitted source, authorization, or suppression evidence: %+v", outcome)
	}
}
