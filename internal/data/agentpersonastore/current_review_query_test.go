package agentpersonastore

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_006_CurrentReviewRequiresExactLatestIndependentAuthority(t *testing.T) {
	ctx := context.Background()
	tenant := values.TenantId("current-review")
	f := newFixture(t, tenant, "current-review-other")
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	mapper := func(key values.TenantId) uuid.UUID { return f.ids[key] }
	if _, err := NewWithReviewAuthority(conn, mapper, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing review authority: %v", err)
	}
	root, err := NewWithReviewAuthority(conn, mapper, testDurableReviewAuthority(t, f))
	if err != nil {
		t.Fatal(err)
	}
	s, err := root.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	v := version(tenant, "persona-current-review", 1)
	v.ContentDigest = "sha256:" + strings.Repeat("a", 64)
	owner, steward := draftOwners(v)
	if err := s.CreateDraft(ctx, v, owner, steward, "user:creator", f.when); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLifecycle(ctx, lifecycle(tenant, v.PersonaID, v.Version, "review-start", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	seedCurrentReviewGrant(t, f, tenant, "current-grant", "user:reviewer")
	issuer := durableReviewIssuerForTest(t, f)
	approval, err := issuer.IssuePersonaReview(ctx, tenant, v.PersonaID, v.Version, "user:reviewer", "APPROVE")
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.ResolveCurrentReview(ctx, v.PersonaID, v.Version)
	if err != nil || review.ReviewID != approval.ReviewID || review.Decision != "APPROVE" || !review.GrantCurrent {
		t.Fatalf("current approval = %+v, %v", review, err)
	}
	if _, err := s.ResolvePublicationEvidence(ctx, v.PersonaID, v.Version); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("review-only store enabled publication: %v", err)
	}
	other := f.storeWithAuthorities(t, "current-review-other", testDurableReviewAuthority(t, f), testEvaluationSource{})
	if _, err := other.ResolveCurrentReview(ctx, v.PersonaID, v.Version); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("foreign tenant read review: %v", err)
	}
	if _, err := s.ResolveCurrentReview(ctx, v.PersonaID, v.Version+1); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("foreign profile version read review: %v", err)
	}
	f.db.Exec(t, `UPDATE persona_review_grant SET revoked_at=clock_timestamp(),revoked_by='admin:revoke' WHERE tenant_id=$1 AND grant_id='current-grant'`, f.ids[tenant])
	if _, err := s.ResolveCurrentReview(ctx, v.PersonaID, v.Version); err == nil {
		t.Fatal("revoked current review grant retained signable review metadata")
	}
	seedCurrentReviewGrant(t, f, tenant, "replacement-grant", "user:reviewer")
	if _, err := s.ResolveCurrentReview(ctx, v.PersonaID, v.Version); err == nil {
		t.Fatal("new grant revived an approval issued under a revoked grant")
	}
	_, err = issuer.IssuePersonaReview(ctx, tenant, v.PersonaID, v.Version, "user:reviewer", "REJECT")
	if err != nil {
		t.Fatal(err)
	}
	review, err = s.ResolveCurrentReview(ctx, v.PersonaID, v.Version)
	if !errors.Is(err, ErrPublicationEvidenceRequired) || review.ReviewID != "" {
		t.Fatalf("latest rejection replaced by older approval: %+v, %v", review, err)
	}
}
