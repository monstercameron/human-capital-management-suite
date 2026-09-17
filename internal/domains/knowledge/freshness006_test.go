package knowledge

import (
	"errors"
	"testing"
	"time"
)

var know006At = time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC)

func know006Query() ResolveQuery {
	return ResolveQuery{
		TenantID: "acme", OrgID: "org-1", Persona: "MANAGER", Jurisdiction: "US-CA",
		At: know006At, Purpose: "hr-help", Clearance: "INTERNAL",
	}
}

func know006Article(id string, revision uint64, classification string) ResolvableArticle {
	return ResolvableArticle{
		Article: ArticleRevision{
			ArticleID: id, Revision: revision, Locale: "en-US",
			Classification: classification, Owner: "owner:hr-1",
			SourceAuthority: "authority:hr-policy", Jurisdiction: "US-CA",
		},
		TenantID: "acme", OrgIDs: []string{"org-1"},
		Personae: []string{"MANAGER"}, Purposes: []string{"hr-help"},
	}
}

func know006Answer() AnswerCandidate {
	return AnswerCandidate{
		ContentDigest: "sha256:answer-body",
		Citations: []AnswerCitation{
			{ClaimRef: "claim-overtime", ArticleID: "article-ot", Version: 2, RetrievedAt: know006At.Add(-time.Hour)},
		},
		Articles: []ResolvableArticle{know006Article("article-ot", 2, "INTERNAL")},
	}
}

// TestTodo_KNOW_006 is the PRIMARY contract: adversarial outdated,
// conflicting, hostile or unauthorized content yields a cited current
// answer, an explicit conflict or unknown, or escalation — never an
// uncited fabrication. The seeded defect (uncited, stale, injected or
// invalidated knowledge staying publishable) returns KNOW_006_REJECTED
// with field/state/version and persists nothing.
func TestTodo_KNOW_006(t *testing.T) {
	got, err := EvaluateAnswerFreshness(know006Query(), know006Answer())
	if err != nil {
		t.Fatalf("EvaluateAnswerFreshness: %v", err)
	}
	if got.Decision != AnswerPublishable || len(got.Cited) != 1 || got.Digest == "" {
		t.Fatalf("cited current answer must publish: %+v", got)
	}

	t.Run("uncited answer never publishes", func(t *testing.T) {
		bare := know006Answer()
		bare.Citations = nil
		_, err := EvaluateAnswerFreshness(know006Query(), bare)
		var rej *FreshnessRejection
		if !errors.As(err, &rej) || !errors.Is(err, ErrFreshnessRejected) {
			t.Fatalf("uncited answer must be KNOW_006_REJECTED, got %v", err)
		}
		if rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version: %+v", rej)
		}
	})

	t.Run("stale citation never publishes", func(t *testing.T) {
		stale := know006Answer()
		stale.Citations[0].RetrievedAt = know006At.Add(-MaxCitationAge - time.Hour)
		_, err := EvaluateAnswerFreshness(know006Query(), stale)
		if !errors.Is(err, ErrFreshnessRejected) {
			t.Fatalf("stale citation must be KNOW_006_REJECTED, got %v", err)
		}
		var rej *FreshnessRejection
		if errors.As(err, &rej) && rej.State != "STALE" {
			t.Fatalf("stale citation must report STALE, got %+v", rej)
		}
	})

	t.Run("injected and invalidated sources never publish", func(t *testing.T) {
		for name, poison := range map[string]func(*ResolvableArticle){
			"injected":    func(a *ResolvableArticle) { a.Injected = true },
			"invalidated": func(a *ResolvableArticle) { a.Invalidated = true },
		} {
			answer := know006Answer()
			poison(&answer.Articles[0])
			_, err := EvaluateAnswerFreshness(know006Query(), answer)
			if !errors.Is(err, ErrFreshnessRejected) {
				t.Fatalf("%s source must be KNOW_006_REJECTED, got %v", name, err)
			}
		}
	})

	t.Run("superseded citation escalates with an explicit unknown", func(t *testing.T) {
		answer := know006Answer()
		answer.Articles[0].Article.Revision = 3
		got, err := EvaluateAnswerFreshness(know006Query(), answer)
		if err != nil {
			t.Fatal(err)
		}
		if got.Decision != AnswerEscalated || len(got.Unknown) != 1 {
			t.Fatalf("superseded citation must escalate explicitly: %+v", got)
		}
	})
}

func TestTodo_KNOW_006_Conformance(t *testing.T) {
	// Every article fixture passes the same gate: the resolver verdicts
	// (injected, invalidated, scope) compose with freshness identically.
	fixtures := []struct {
		name   string
		mutate func(*AnswerCandidate)
		state  string
	}{
		{"injected", func(a *AnswerCandidate) { a.Articles[0].Injected = true }, "INJECTED"},
		{"invalidated", func(a *AnswerCandidate) { a.Articles[0].Invalidated = true }, "INVALIDATED"},
		{"out of scope", func(a *AnswerCandidate) { a.Articles[0].TenantID = "other" }, "OUT_OF_SCOPE"},
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			answer := know006Answer()
			f.mutate(&answer)
			_, err := EvaluateAnswerFreshness(know006Query(), answer)
			var rej *FreshnessRejection
			if !errors.As(err, &rej) || rej.State != f.state {
				t.Fatalf("fixture %s must report %s, got %v", f.name, f.state, err)
			}
		})
	}
	// Unauthorized content is refused, never leaked through redaction.
	answer := know006Answer()
	answer.Articles[0].Article.Classification = "RESTRICTED"
	_, err := EvaluateAnswerFreshness(know006Query(), answer)
	var rej *FreshnessRejection
	if !errors.As(err, &rej) || rej.State != "UNAUTHORIZED" {
		t.Fatalf("restricted article must report UNAUTHORIZED, got %v", err)
	}
}

func TestTodo_KNOW_006_Mutation(t *testing.T) {
	a, err := EvaluateAnswerFreshness(know006Query(), know006Answer())
	if err != nil {
		t.Fatal(err)
	}
	answer := know006Answer()
	answer.ContentDigest = "sha256:forged-body"
	b, err := EvaluateAnswerFreshness(know006Query(), answer)
	if err != nil {
		t.Fatal(err)
	}
	if b.Digest == a.Digest {
		t.Fatalf("content change must move the seal")
	}
	for field, mutate := range map[string]func(*AnswerCandidate){
		"claim":   func(a *AnswerCandidate) { a.Citations[0].ClaimRef = "" },
		"article": func(a *AnswerCandidate) { a.Citations[0].ArticleID = "article-ghost" },
		"version": func(a *AnswerCandidate) { a.Citations[0].Version = 0 },
	} {
		candidate := know006Answer()
		mutate(&candidate)
		if _, err := EvaluateAnswerFreshness(know006Query(), candidate); !errors.Is(err, ErrFreshnessRejected) {
			t.Fatalf("mutated %s must be KNOW_006_REJECTED", field)
		}
	}
}

func TestTodo_KNOW_006_Security(t *testing.T) {
	// Clearance never upgrades through the gate: a PUBLIC reader cannot
	// publish INTERNAL knowledge.
	public := know006Query()
	public.Clearance = "PUBLIC"
	if _, err := EvaluateAnswerFreshness(public, know006Answer()); !errors.Is(err, ErrFreshnessRejected) {
		t.Fatalf("clearance gap must be KNOW_006_REJECTED")
	}
	// Cross-tenant articles never publish, and the refusal names the
	// scope without disclosing the foreign article body.
	answer := know006Answer()
	answer.Articles[0].TenantID = "foreign"
	_, err := EvaluateAnswerFreshness(know006Query(), answer)
	var rej *FreshnessRejection
	if !errors.As(err, &rej) || rej.State != "OUT_OF_SCOPE" {
		t.Fatalf("foreign article must report OUT_OF_SCOPE, got %v", err)
	}
}
