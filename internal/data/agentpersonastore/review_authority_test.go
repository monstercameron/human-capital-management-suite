package agentpersonastore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_006_ReviewAuthorityIntegration(t *testing.T) {
	f := newFixture(t, "review-owner", "review-other")
	store := f.store(t, "review-owner")
	seedReviewAuthority(t, f, "review-owner", "review-approved", "grant-approved", "user:reviewer", "user:author", "APPROVE", false, false)
	authority := testDurableReviewAuthority(t, f)
	tx, err := store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())

	got, err := authority.ResolvePersonaReview(context.Background(), tx, "review-owner", "persona-a", 3, "sha256:profile-a", "review-approved")
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "review-owner" || got.PersonaID != "persona-a" || got.PersonaVersion != 3 || got.ProfileDigest != "sha256:profile-a" || got.ReviewerID != "user:reviewer" || got.Permission != "persona:review" || got.Decision != "APPROVE" || !got.GrantCurrent || got.ReviewDigest != "sha256:review-approved" {
		t.Fatalf("resolved durable approval = %+v", got)
	}

	other := f.store(t, "review-other")
	otherTx, err := other.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer otherTx.Rollback(context.Background())
	if _, err := authority.ResolvePersonaReview(context.Background(), otherTx, "review-other", "persona-a", 3, "sha256:profile-a", "review-approved"); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("cross-tenant review resolution error = %v", err)
	}
}

func TestTodo_AGENTP_006_ReviewAuthoritySecurity(t *testing.T) {
	f := newFixture(t, "review-security")
	store := f.store(t, "review-security")
	seedReviewAuthority(t, f, "review-security", "review-approved", "grant-approved", "user:reviewer", "user:author", "APPROVE", false, false)
	seedReviewAuthority(t, f, "review-security", "review-rejected", "grant-rejected", "user:reviewer", "user:author", "REJECT", false, false)
	seedReviewAuthority(t, f, "review-security", "review-revoked", "grant-revoked", "user:reviewer", "user:author", "APPROVE", true, false)
	seedReviewAuthority(t, f, "review-security", "review-stale", "grant-stale", "user:reviewer", "user:author", "APPROVE", false, true)
	tx, err := store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	authority := testDurableReviewAuthority(t, f)
	for _, id := range []string{"missing", "review-rejected", "review-revoked", "review-stale"} {
		if _, err := authority.ResolvePersonaReview(context.Background(), tx, "review-security", "persona-a", 3, "sha256:profile-a", id); !errors.Is(err, ErrPublicationEvidenceRequired) {
			t.Errorf("review %q error = %v, want fail-closed authority error", id, err)
		}
	}
	if _, err := authority.ResolvePersonaReview(context.Background(), tx, "review-security", "persona-a", 3, "sha256:other-profile", "review-approved"); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("wrong profile digest error = %v", err)
	}
	if _, err := authority.ResolvePersonaReview(context.Background(), tx, "review-security", "persona-a", 4, "sha256:profile-a", "review-approved"); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("wrong persona version error = %v", err)
	}
}

func TestTodo_AGENTP_006_ReviewAuthorityFault(t *testing.T) {
	var authority DurableReviewAuthority
	if _, err := authority.ResolvePersonaReview(context.Background(), nil, values.TenantId("tenant"), "persona", 1, "digest", "review"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil transaction error = %v", err)
	}
	if _, err := authority.ResolvePersonaReview(nil, nil, "tenant", "persona", 1, "digest", "review"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil context error = %v", err)
	}
}

func testDurableReviewAuthority(t *testing.T, f *fixture) DurableReviewAuthority {
	t.Helper()
	authority, err := NewDurableReviewAuthority(func(tenant values.TenantId) uuid.UUID { return f.ids[tenant] })
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func seedReviewAuthority(t *testing.T, f *fixture, tenant values.TenantId, reviewID, grantID, reviewer, author, decision string, revoked, stale bool) {
	t.Helper()
	f.db.Exec(t, `INSERT INTO persona_owners (tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at)
		VALUES ($1,'persona-a','BUSINESS_OWNER',$2,'test:authority',now()) ON CONFLICT (tenant_id,persona_id,owner_role) DO NOTHING`, f.ids[tenant], author)
	grantExpiry, reviewExpiry := "now() + interval '1 day'", "now() + interval '1 day'"
	if stale {
		grantExpiry, reviewExpiry = "now() - interval '30 minutes'", "now() - interval '30 minutes'"
	}
	grantRevocation, reviewRevocation := "NULL", "NULL"
	grantRevokedBy, reviewRevokedBy := "", ""
	if revoked {
		grantRevocation, reviewRevocation = "now() - interval '1 hour'", "now() - interval '1 hour'"
		grantRevokedBy, reviewRevokedBy = "security:revoke", "security:revoke"
	}
	f.db.Exec(t, `INSERT INTO persona_review_grant
		(tenant_id,grant_id,principal_id,permission,granted_at,expires_at,revoked_at,revoked_by)
		VALUES ($1,$2,$3,'persona:review',now()-interval '1 hour',`+grantExpiry+`,`+grantRevocation+`,$4)`,
		f.ids[tenant], grantID, reviewer, grantRevokedBy)
	f.db.Exec(t, `INSERT INTO persona_review_decision
		(tenant_id,review_id,persona_id,persona_version,profile_digest,author_id,reviewer_id,grant_id,permission,decision,review_digest,reviewed_at,expires_at,revoked_at,revoked_by)
		VALUES ($1,$2,'persona-a',3,'sha256:profile-a',$3,$4,$5,'persona:review',$6,$7,now()-interval '1 hour',`+reviewExpiry+`,`+reviewRevocation+`,$8)`,
		f.ids[tenant], reviewID, author, reviewer, grantID, decision, "sha256:"+reviewID, reviewRevokedBy)
}
