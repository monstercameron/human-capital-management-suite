package agentpersonastore

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestPersonaPublicationEvidenceResolvesDurableAuthorities(t *testing.T) {
	ctx := context.Background()
	tenant := values.TenantId("publication-query")
	f := newFixture(t, tenant, "publication-other")
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := sha256.Sum256([]byte("publication query evaluator"))
	private := ed25519.NewKeyFromSeed(seed[:])
	seal, err := NewEvaluationSealAuthority(map[string]ed25519.PublicKey{"eval-v1": private.Public().(ed25519.PublicKey)}, func(key values.TenantId) uuid.UUID { return f.ids[key] }, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	s := f.storeWithAuthorities(t, tenant, testDurableReviewAuthority(t, f), seal)
	v := version(tenant, "persona-a", 3)
	v.ContentDigest = "sha256:" + strings.Repeat("a", 64)
	owner, steward := draftOwners(v)
	if err := s.CreateDraft(ctx, v, owner, steward, "user:creator", f.when); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLifecycle(ctx, lifecycle(tenant, v.PersonaID, v.Version, "review-start", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	seedCurrentReviewGrant(t, f, tenant, "grant-query", "user:reviewer")
	review, err := durableReviewIssuerForTest(t, f).IssuePersonaReview(ctx, tenant, v.PersonaID, v.Version, "user:reviewer", "APPROVE")
	if err != nil {
		t.Fatal(err)
	}
	record := func(id string, passed bool, issued time.Time) {
		t.Helper()
		evidence := signedEvaluation(private, string(tenant), id, v.PersonaID, v.Version, v.ContentDigest, passed, issued, now.Add(time.Hour))
		tx, err := s.begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := seal.RecordPersonaEvaluation(ctx, tx, evidence); err != nil {
			t.Fatal(err)
		}
		if err := commit(ctx, tx); err != nil {
			t.Fatal(err)
		}
	}
	record("eval-pass", true, now.Add(-time.Minute))
	evidence, err := s.ResolvePublicationEvidence(ctx, v.PersonaID, v.Version)
	if err != nil || evidence.ReviewID != review.ReviewID || evidence.EvaluationRunID != "eval-pass" {
		t.Fatalf("resolved evidence = %+v, %v", evidence, err)
	}
	other := f.storeWithAuthorities(t, "publication-other", testDurableReviewAuthority(t, f), seal)
	if _, err := other.ResolvePublicationEvidence(ctx, v.PersonaID, v.Version); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("other tenant resolved evidence: %v", err)
	}
	record("eval-failed", false, now)
	if _, err := s.ResolvePublicationEvidence(ctx, v.PersonaID, v.Version); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("older passing evaluation survived latest failure: %v", err)
	}
}
