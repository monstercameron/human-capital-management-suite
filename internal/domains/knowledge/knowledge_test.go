package knowledge_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/knowledge"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// helper to create a valid Instant for testing
func newInstant(t *testing.T, s string) values.Instant {
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	inst := values.NewInstant(at)
	return inst
}

// helper to create a minimal valid ArticleRevision for testing
func minimalArticle(t *testing.T) knowledge.ArticleRevision {
	return knowledge.ArticleRevision{
		ArticleID:            "article_123",
		Revision:             1,
		Locale:               "en-US",
		AudienceScope:        "EMPLOYEES",
		AuthorizedRoles:      []string{"worker_self"},
		RetentionScheduleRef: "records:knowledge/current",
		Classification:       "INTERNAL",
		Owner:                "policy@example.com",
		SourceAuthority:      "policy/authority/1.0",
		SourceRefs: []knowledge.SourceRef{
			{
				System:     "hcmnext.knowledge",
				Identifier: "src_456",
				Authority:  "auth/ref/1",
			},
		},
		Jurisdiction: "US",
		EffectiveInterval: knowledge.EffectiveInterval{
			EffectiveFrom: newInstant(t, "2026-01-01T00:00:00Z"),
			EffectiveTo:   newInstant(t, "2027-01-01T00:00:00Z"),
		},
		KnownInterval: knowledge.KnownInterval{
			KnownFrom: newInstant(t, "2025-12-01T00:00:00Z"),
			KnownTo:   newInstant(t, "2026-12-01T00:00:00Z"),
		},
		BodyDigest: "sha256:abcd1234",
		Title:      "Policy on Leave",
		Summary:    "Annual leave policies for employees",
		Review: knowledge.ReviewMetadata{
			ReviewedBy:  "reviewer@example.com",
			ReviewedAt:  newInstant(t, "2026-01-01T10:00:00Z"),
			ExpiresAt:   newInstant(t, "2027-01-01T10:00:00Z"),
			ApprovalRef: "approval_xyz",
		},
	}
}

// TestTodo_KNOW_001 is the PRIMARY test for KNOW-001. It verifies that
// an article without required fields (owner, source authority, classifications,
// audience scope, jurisdiction, effective/known intervals, review, and expiry)
// fails publication (Validate returns error and Canonical returns nil).
func TestTodo_KNOW_001(t *testing.T) {
	t.Run("article_with_all_required_fields_validates", func(t *testing.T) {
		a := minimalArticle(t)
		if err := a.Validate(); err != nil {
			t.Fatalf("minimal article should validate: %v", err)
		}
	})

	t.Run("article_missing_article_id_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.ArticleID = ""
		if err := a.Validate(); !errors.Is(err, knowledge.ErrArticleID) {
			t.Fatalf("error = %v, want ErrArticleID", err)
		}
		if a.Canonical() != nil {
			t.Fatal("invalid article encoded despite missing ID")
		}
	})

	t.Run("article_missing_owner_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.Owner = ""
		if err := a.Validate(); !errors.Is(err, knowledge.ErrOwner) {
			t.Fatalf("error = %v, want ErrOwner", err)
		}
		if a.Canonical() != nil {
			t.Fatal("invalid article encoded despite missing owner")
		}
	})

	t.Run("article_missing_source_authority_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.SourceAuthority = ""
		if err := a.Validate(); !errors.Is(err, knowledge.ErrSourceAuthority) {
			t.Fatalf("error = %v, want ErrSourceAuthority", err)
		}
		if a.Canonical() != nil {
			t.Fatal("invalid article encoded despite missing source authority")
		}
	})

	t.Run("article_missing_classification_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.Classification = ""
		if err := a.Validate(); !errors.Is(err, knowledge.ErrClassification) {
			t.Fatalf("error = %v, want ErrClassification", err)
		}
		if a.Canonical() != nil {
			t.Fatal("invalid article encoded despite missing classification")
		}
	})

	t.Run("article_missing_audience_scope_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.AudienceScope = ""
		if err := a.Validate(); !errors.Is(err, knowledge.ErrAudienceScope) {
			t.Fatalf("error = %v, want ErrAudienceScope", err)
		}
		if a.Canonical() != nil {
			t.Fatal("invalid article encoded despite missing audience scope")
		}
	})

	t.Run("article_missing_jurisdiction_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.Jurisdiction = ""
		if err := a.Validate(); !errors.Is(err, knowledge.ErrJurisdiction) {
			t.Fatalf("error = %v, want ErrJurisdiction", err)
		}
		if a.Canonical() != nil {
			t.Fatal("invalid article encoded despite missing jurisdiction")
		}
	})

	t.Run("article_missing_effective_from_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.EffectiveInterval.EffectiveFrom = values.Instant{}
		if err := a.Validate(); !errors.Is(err, knowledge.ErrEffectiveInterval) {
			t.Fatalf("error = %v, want ErrEffectiveInterval", err)
		}
	})

	t.Run("article_missing_known_from_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.KnownInterval.KnownFrom = values.Instant{}
		if err := a.Validate(); !errors.Is(err, knowledge.ErrKnownInterval) {
			t.Fatalf("error = %v, want ErrKnownInterval", err)
		}
	})

	t.Run("article_missing_review_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.Review = knowledge.ReviewMetadata{}
		if err := a.Validate(); !errors.Is(err, knowledge.ErrReview) {
			t.Fatalf("error = %v, want ErrReview", err)
		}
	})

	t.Run("article_missing_expiry_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.Review.ExpiresAt = values.Instant{}
		if err := a.Validate(); !errors.Is(err, knowledge.ErrReview) {
			t.Fatalf("error = %v, want ErrReview", err)
		}
	})

	t.Run("article_missing_source_refs_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.SourceRefs = nil
		if err := a.Validate(); !errors.Is(err, knowledge.ErrSourceRef) {
			t.Fatalf("error = %v, want ErrSourceRef", err)
		}
	})

	t.Run("article_with_incomplete_source_ref_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.SourceRefs = []knowledge.SourceRef{
			{System: "hcmnext.knowledge", Identifier: "", Authority: "auth/ref/1"},
		}
		if err := a.Validate(); !errors.Is(err, knowledge.ErrSourceRef) {
			t.Fatalf("error = %v, want ErrSourceRef", err)
		}
	})

	t.Run("article_missing_body_digest_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.BodyDigest = ""
		if err := a.Validate(); !errors.Is(err, knowledge.ErrBodyDigest) {
			t.Fatalf("error = %v, want ErrBodyDigest", err)
		}
	})

	t.Run("article_missing_title_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.Title = ""
		if err := a.Validate(); !errors.Is(err, knowledge.ErrTitle) {
			t.Fatalf("error = %v, want ErrTitle", err)
		}
	})

	t.Run("article_with_zero_revision_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.Revision = 0
		if err := a.Validate(); !errors.Is(err, knowledge.ErrRevision) {
			t.Fatalf("error = %v, want ErrRevision", err)
		}
	})

	t.Run("article_missing_locale_is_rejected", func(t *testing.T) {
		a := minimalArticle(t)
		a.Locale = ""
		if err := a.Validate(); !errors.Is(err, knowledge.ErrLocale) {
			t.Fatalf("error = %v, want ErrLocale", err)
		}
	})
}

// TestTodo_KNOW_001_Property verifies that content equality implies digest
// equality, and digest equality implies content equality (when both articles
// are valid).
func TestTodo_KNOW_001_Property(t *testing.T) {
	t.Run("identical_articles_have_identical_digests", func(t *testing.T) {
		a1 := minimalArticle(t)
		a2 := minimalArticle(t)

		d1 := a1.Digest()
		d2 := a2.Digest()

		if d1 != d2 {
			t.Fatalf("identical articles have different digests: %s vs %s", d1, d2)
		}
	})

	t.Run("identical_articles_have_identical_canonical_bytes", func(t *testing.T) {
		a1 := minimalArticle(t)
		a2 := minimalArticle(t)

		c1 := a1.Canonical()
		c2 := a2.Canonical()

		if !bytes.Equal(c1, c2) {
			t.Fatal("identical articles have different canonical encodings")
		}
	})

	t.Run("articles_differing_only_in_title_have_different_digests", func(t *testing.T) {
		a1 := minimalArticle(t)
		a2 := minimalArticle(t)
		a2.Title = "Different Title"

		d1 := a1.Digest()
		d2 := a2.Digest()

		if d1 == d2 {
			t.Fatal("articles with different titles have the same digest")
		}
	})

	t.Run("articles_differing_only_in_revision_have_different_digests", func(t *testing.T) {
		a1 := minimalArticle(t)
		a2 := minimalArticle(t)
		a2.Revision = 2

		d1 := a1.Digest()
		d2 := a2.Digest()

		if d1 == d2 {
			t.Fatal("articles with different revisions have the same digest")
		}
	})

	t.Run("articles_differing_only_in_owner_have_different_digests", func(t *testing.T) {
		a1 := minimalArticle(t)
		a2 := minimalArticle(t)
		a2.Owner = "other@example.com"

		d1 := a1.Digest()
		d2 := a2.Digest()

		if d1 == d2 {
			t.Fatal("articles with different owners have the same digest")
		}
	})

	t.Run("articles_differing_only_in_body_digest_have_different_digests", func(t *testing.T) {
		a1 := minimalArticle(t)
		a2 := minimalArticle(t)
		a2.BodyDigest = "sha256:different"

		d1 := a1.Digest()
		d2 := a2.Digest()

		if d1 == d2 {
			t.Fatal("articles with different body digests have the same digest")
		}
	})
}

// TestTodo_KNOW_001_Security verifies that articles without classification
// or audience scope are refused, and that an article cannot supersede itself.
func TestTodo_KNOW_001_Security(t *testing.T) {
	t.Run("article_without_classification_is_refused", func(t *testing.T) {
		a := minimalArticle(t)
		a.Classification = ""
		if err := a.Validate(); !errors.Is(err, knowledge.ErrClassification) {
			t.Fatalf("error = %v, want ErrClassification", err)
		}
		if a.Canonical() != nil {
			t.Fatal("article without classification must not encode")
		}
	})

	t.Run("article_without_audience_scope_is_refused", func(t *testing.T) {
		a := minimalArticle(t)
		a.AudienceScope = ""
		if err := a.Validate(); !errors.Is(err, knowledge.ErrAudienceScope) {
			t.Fatalf("error = %v, want ErrAudienceScope", err)
		}
		if a.Canonical() != nil {
			t.Fatal("article without audience scope must not encode")
		}
	})

	t.Run("article_superseding_itself_is_refused", func(t *testing.T) {
		a := minimalArticle(t)
		a.Supersession = &knowledge.Supersession{
			SupersedingArticleID: a.ArticleID,
			SupersedingRevision:  2,
			SupersededAt:         newInstant(t, "2026-06-01T00:00:00Z"),
		}
		if err := a.Validate(); !errors.Is(err, knowledge.ErrSupersession) {
			t.Fatalf("error = %v, want ErrSupersession", err)
		}
		if a.Canonical() != nil {
			t.Fatal("article superseding itself must not encode")
		}
	})

	t.Run("article_with_valid_supersession_encodes", func(t *testing.T) {
		a := minimalArticle(t)
		a.Supersession = &knowledge.Supersession{
			SupersedingArticleID: "article_124",
			SupersedingRevision:  2,
			SupersededAt:         newInstant(t, "2026-06-01T00:00:00Z"),
		}
		if err := a.Validate(); err != nil {
			t.Fatalf("article with valid supersession should validate: %v", err)
		}
		if a.Canonical() == nil {
			t.Fatal("article with valid supersession must encode")
		}
	})

	t.Run("supersession_with_zero_revision_is_refused", func(t *testing.T) {
		a := minimalArticle(t)
		a.Supersession = &knowledge.Supersession{
			SupersedingArticleID: "article_124",
			SupersedingRevision:  0,
			SupersededAt:         newInstant(t, "2026-06-01T00:00:00Z"),
		}
		if err := a.Validate(); !errors.Is(err, knowledge.ErrSupersession) {
			t.Fatalf("error = %v, want ErrSupersession", err)
		}
	})

	t.Run("supersession_missing_article_id_is_refused", func(t *testing.T) {
		a := minimalArticle(t)
		a.Supersession = &knowledge.Supersession{
			SupersedingArticleID: "",
			SupersedingRevision:  2,
			SupersededAt:         newInstant(t, "2026-06-01T00:00:00Z"),
		}
		if err := a.Validate(); !errors.Is(err, knowledge.ErrSupersession) {
			t.Fatalf("error = %v, want ErrSupersession", err)
		}
	})

	t.Run("supersession_missing_superseded_at_is_refused", func(t *testing.T) {
		a := minimalArticle(t)
		a.Supersession = &knowledge.Supersession{
			SupersedingArticleID: "article_124",
			SupersedingRevision:  2,
			SupersededAt:         values.Instant{},
		}
		if err := a.Validate(); !errors.Is(err, knowledge.ErrSupersession) {
			t.Fatalf("error = %v, want ErrSupersession", err)
		}
	})
}

// TestTodo_KNOW_001_Mutation verifies that Canonical returns nil for any
// invalid or incomplete article.
func TestTodo_KNOW_001_Mutation(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*knowledge.ArticleRevision)
		err   error
	}{
		{"no_article_id", func(a *knowledge.ArticleRevision) { a.ArticleID = "" }, knowledge.ErrArticleID},
		{"no_revision", func(a *knowledge.ArticleRevision) { a.Revision = 0 }, knowledge.ErrRevision},
		{"no_locale", func(a *knowledge.ArticleRevision) { a.Locale = "" }, knowledge.ErrLocale},
		{"no_audience", func(a *knowledge.ArticleRevision) { a.AudienceScope = "" }, knowledge.ErrAudienceScope},
		{"no_classification", func(a *knowledge.ArticleRevision) { a.Classification = "" }, knowledge.ErrClassification},
		{"no_owner", func(a *knowledge.ArticleRevision) { a.Owner = "" }, knowledge.ErrOwner},
		{"no_source_authority", func(a *knowledge.ArticleRevision) { a.SourceAuthority = "" }, knowledge.ErrSourceAuthority},
		{"no_source_refs", func(a *knowledge.ArticleRevision) { a.SourceRefs = nil }, knowledge.ErrSourceRef},
		{"no_jurisdiction", func(a *knowledge.ArticleRevision) { a.Jurisdiction = "" }, knowledge.ErrJurisdiction},
		{"no_body_digest", func(a *knowledge.ArticleRevision) { a.BodyDigest = "" }, knowledge.ErrBodyDigest},
		{"no_title", func(a *knowledge.ArticleRevision) { a.Title = "" }, knowledge.ErrTitle},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := minimalArticle(t)
			tc.setup(&a)
			if err := a.Validate(); !errors.Is(err, tc.err) {
				t.Fatalf("validate: got %v, want %v", err, tc.err)
			}
			if a.Canonical() != nil {
				t.Fatalf("%s: invalid article encoded despite error", tc.name)
			}
			if a.Digest() != "" {
				t.Fatalf("%s: digest should be empty for invalid article", tc.name)
			}
		})
	}
}
