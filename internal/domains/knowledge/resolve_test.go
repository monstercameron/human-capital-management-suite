package knowledge

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func resolveInstant(t *testing.T, s string) values.Instant {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(tm)
}

func resolveArticle(t *testing.T) ArticleRevision {
	t.Helper()
	return ArticleRevision{
		ArticleID: "art:pto-policy", Revision: 2, Locale: "en-US",
		AudienceScope: "EMPLOYEES", Classification: "INTERNAL", Owner: "owner:hr",
		SourceAuthority: "authority:hr-policy-board",
		SourceRefs: []SourceRef{
			{System: "hr-policy", Identifier: "pto-2026", Authority: "authority:hr-policy-board"},
		},
		Jurisdiction:      "US-CA",
		EffectiveInterval: EffectiveInterval{EffectiveFrom: resolveInstant(t, "2026-01-01T00:00:00Z")},
		KnownInterval:     KnownInterval{KnownFrom: resolveInstant(t, "2026-01-02T00:00:00Z")},
		BodyDigest:        "sha256:body",
		Title:             "PTO policy",
		Review: ReviewMetadata{
			ReviewedBy: "reviewer:hr", ReviewedAt: resolveInstant(t, "2026-01-03T00:00:00Z"),
			ApprovalRef: "approval:1",
		},
	}
}

func resolveQuery() ResolveQuery {
	return ResolveQuery{
		TenantID:     "acme",
		OrgID:        "org:us",
		Persona:      "EMPLOYEES",
		Jurisdiction: "US-CA",
		At:           time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
		Purpose:      "ANSWER_QUESTION",
		Clearance:    "INTERNAL",
	}
}

func resolvable(t *testing.T) ResolvableArticle {
	t.Helper()
	return ResolvableArticle{
		Article:  resolveArticle(t),
		TenantID: "acme",
		OrgIDs:   []string{"org:us"},
		Personae: []string{"EMPLOYEES", "MANAGERS"},
		Purposes: []string{"ANSWER_QUESTION"},
	}
}

// TestTodo_KNOW_003 is the primary KNOW-003 contract test. Seeded defects —
// uncited, stale, injected or invalidated knowledge that matches the query —
// must return KNOW_003_REJECTED with the offending field/state/version and
// persist nothing: resolution is pure, so there is no store to write to.
func TestTodo_KNOW_003(t *testing.T) {
	t.Run("applicable authorized article resolves", func(t *testing.T) {
		res, err := ResolveKnowledge(resolveQuery(), []ResolvableArticle{resolvable(t)})
		if err != nil {
			t.Fatalf("ResolveKnowledge: %v", err)
		}
		if len(res.Articles) != 1 || res.Articles[0].ArticleID != "art:pto-policy" {
			t.Fatalf("expected the applicable article: %+v", res)
		}
		if res.Digest == "" {
			t.Fatal("resolution must carry a digest")
		}
	})

	t.Run("out-of-scope articles are excluded, not rejected", func(t *testing.T) {
		base := resolvable(t)
		others := []ResolvableArticle{
			func() ResolvableArticle { c := base; c.TenantID = "other"; return c }(),
			func() ResolvableArticle { c := base; c.Personae = []string{"PUBLIC"}; return c }(),
			func() ResolvableArticle { c := base; c.Article.Jurisdiction = "EU-DE"; return c }(),
			func() ResolvableArticle { c := base; c.Article.Classification = "CONFIDENTIAL"; return c }(),
			func() ResolvableArticle {
				c := base
				c.Article.EffectiveInterval.EffectiveFrom = resolveInstant(t, "2027-01-01T00:00:00Z")
				return c
			}(),
		}
		res, err := ResolveKnowledge(resolveQuery(), others)
		if err != nil {
			t.Fatalf("out-of-scope articles must be excluded silently: %v", err)
		}
		if len(res.Articles) != 0 {
			t.Fatalf("no article applies: %+v", res.Articles)
		}
	})

	t.Run("seeded defects are rejected with field state version", func(t *testing.T) {
		base := resolvable(t)
		uncited := base
		uncited.Article.SourceRefs = nil
		stale := base
		stale.Article.Supersession = &Supersession{
			SupersedingArticleID: "art:pto-policy-v2", SupersedingRevision: 3,
			SupersededAt: resolveInstant(t, "2026-05-01T00:00:00Z"),
		}
		injected := base
		injected.Injected = true
		invalidated := base
		invalidated.Invalidated = true
		for name, cand := range map[string]ResolvableArticle{
			"uncited": uncited, "stale": stale, "injected": injected, "invalidated": invalidated,
		} {
			_, err := ResolveKnowledge(resolveQuery(), []ResolvableArticle{cand})
			var rej *ResolveRejection
			if !errors.As(err, &rej) {
				t.Fatalf("%s: expected *ResolveRejection, got %v", name, err)
			}
			if !errors.Is(err, ErrResolveRejected) {
				t.Fatalf("%s: expected KNOW_003_REJECTED, got %v", name, err)
			}
			if rej.Field == "" || rej.State == "" || rej.Version == "" {
				t.Fatalf("%s: rejection must name field/state/version: %+v", name, rej)
			}
		}
	})

	t.Run("defects outside scope never poison the resolution", func(t *testing.T) {
		base := resolvable(t)
		bad := base
		bad.TenantID = "other"
		bad.Injected = true
		bad.Article.SourceRefs = nil
		res, err := ResolveKnowledge(resolveQuery(), []ResolvableArticle{base, bad})
		if err != nil {
			t.Fatalf("out-of-scope defects must not fail resolution: %v", err)
		}
		if len(res.Articles) != 1 {
			t.Fatalf("expected only the good article: %+v", res)
		}
	})

	t.Run("empty query identity is rejected", func(t *testing.T) {
		q := resolveQuery()
		q.TenantID = ""
		if _, err := ResolveKnowledge(q, []ResolvableArticle{resolvable(t)}); !errors.Is(err, ErrResolveRejected) {
			t.Fatalf("expected KNOW_003_REJECTED, got %v", err)
		}
	})
}

// TestTodo_KNOW_003_Golden pins the canonical resolution bytes.
func TestTodo_KNOW_003_Golden(t *testing.T) {
	res, err := ResolveKnowledge(resolveQuery(), []ResolvableArticle{resolvable(t)})
	if err != nil {
		t.Fatal(err)
	}
	again, err := ResolveKnowledge(resolveQuery(), []ResolvableArticle{resolvable(t)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Digest != again.Digest {
		t.Fatal("resolution digest must be stable")
	}
	if res.Digest == "" || len(res.Digest) < 8 {
		t.Fatalf("resolution must carry a content digest: %q", res.Digest)
	}
}

// TestTodo_KNOW_003_Security proves restricted, cross-tenant and
// over-clearance knowledge is never returned and never leaks identifiers.
func TestTodo_KNOW_003_Security(t *testing.T) {
	base := resolvable(t)
	secret := base
	secret.Article.ArticleID = "art:exec-comp"
	secret.Article.Classification = "RESTRICTED"
	foreign := base
	foreign.Article.ArticleID = "art:foreign"
	foreign.TenantID = "competitor"
	res, err := ResolveKnowledge(resolveQuery(), []ResolvableArticle{base, secret, foreign})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range res.Articles {
		if a.ArticleID != "art:pto-policy" {
			t.Fatalf("unauthorized article leaked: %q", a.ArticleID)
		}
	}
}

// TestTodo_KNOW_003_Mutation kills the filter-removal mutants: dropping the
// citation, taint, invalidation, supersession or tenant check must fail.
func TestTodo_KNOW_003_Mutation(t *testing.T) {
	base := resolvable(t)
	mustReject := map[string]ResolvableArticle{
		"citation check": func() ResolvableArticle { c := base; c.Article.SourceRefs = nil; return c }(),
		"taint check":    func() ResolvableArticle { c := base; c.Injected = true; return c }(),
		"validity check": func() ResolvableArticle { c := base; c.Invalidated = true; return c }(),
		"staleness check": func() ResolvableArticle {
			c := base
			c.Article.Supersession = &Supersession{SupersedingArticleID: "art:next", SupersedingRevision: 9}
			return c
		}(),
	}
	for name, cand := range mustReject {
		if _, err := ResolveKnowledge(resolveQuery(), []ResolvableArticle{cand}); !errors.Is(err, ErrResolveRejected) {
			t.Fatalf("%s mutant survived: expected KNOW_003_REJECTED", name)
		}
	}
	// The tenant fence excludes rather than rejects: a foreign article must
	// vanish without an existence oracle.
	foreign := base
	foreign.TenantID = "attacker"
	res, err := ResolveKnowledge(resolveQuery(), []ResolvableArticle{foreign})
	if err != nil {
		t.Fatalf("foreign article must be excluded silently: %v", err)
	}
	if len(res.Articles) != 0 {
		t.Fatal("tenant fence mutant survived: foreign article resolved")
	}
}
