package agentpersonastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_006_ReviewIssuerIntegration(t *testing.T) {
	f := newFixture(t, "review-issuer")
	store := f.store(t, "review-issuer")
	v := version("review-issuer", "persona-a", 4)
	owner, steward := draftOwners(v)
	if err := store.CreateDraft(context.Background(), v, owner, steward, "user:creator", f.when); err != nil {
		t.Fatal(err)
	}
	seedCurrentReviewGrant(t, f, "review-issuer", "grant-current", "user:reviewer")
	issuer := durableReviewIssuerForTest(t, f)
	if err := issuer.AuthorizePersonaReview(context.Background(), "review-issuer", "user:reviewer"); err != nil {
		t.Fatalf("active reviewer grant authorization: %v", err)
	}
	if _, err := issuer.IssuePersonaReview(context.Background(), "review-issuer", "persona-a", 4, "user:reviewer", "APPROVE"); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("draft review issuance error = %v, want not-awaiting-review denial", err)
	}
	if err := store.AppendLifecycle(context.Background(), lifecycle("review-issuer", "persona-a", 4, "request-review", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}

	got, err := issuer.IssuePersonaReview(context.Background(), "review-issuer", "persona-a", 4, "user:reviewer", "APPROVE")
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "review-issuer" || got.PersonaID != "persona-a" || got.PersonaVersion != 4 || got.ProfileDigest != v.ContentDigest || got.ReviewerID != "user:reviewer" || got.Permission != "persona:review" || got.Decision != "APPROVE" || !got.GrantCurrent || got.ReviewID == "" || got.ReviewDigest == "" {
		t.Fatalf("issued durable review = %+v", got)
	}

	tx, err := store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var storedDigest, storedReviewer string
	if err := tx.QueryRow(context.Background(), `SELECT review_digest, reviewer_id FROM persona_review_decision WHERE tenant_id=$1 AND review_id=$2`, f.ids[values.TenantId("review-issuer")], got.ReviewID).Scan(&storedDigest, &storedReviewer); err != nil {
		t.Fatal(err)
	}
	if storedDigest != got.ReviewDigest || storedReviewer != got.ReviewerID {
		t.Fatalf("stored review differs from issuance: digest=%q reviewer=%q issued=%+v", storedDigest, storedReviewer, got)
	}
}

func TestTodo_AGENTP_006_ReviewIssuerSecurity(t *testing.T) {
	f := newFixture(t, "review-issuer-security")
	store := f.store(t, "review-issuer-security")
	v := version("review-issuer-security", "persona-a", 2)
	owner, steward := draftOwners(v)
	if err := store.CreateDraft(context.Background(), v, owner, steward, "user:creator", f.when); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLifecycle(context.Background(), lifecycle("review-issuer-security", "persona-a", 2, "request-review", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	seedCurrentReviewGrant(t, f, "review-issuer-security", "grant-revoked", "user:reviewer")
	f.db.Exec(t, `UPDATE persona_review_grant SET revoked_at=now(),revoked_by='admin:revoke' WHERE tenant_id=$1 AND grant_id='grant-revoked'`, f.ids[values.TenantId("review-issuer-security")])
	issuer := durableReviewIssuerForTest(t, f)
	if err := issuer.AuthorizePersonaReview(context.Background(), "review-issuer-security", "user:reviewer"); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("revoked reviewer grant authorization error = %v", err)
	}
	for _, tc := range []struct {
		name, reviewer, persona string
		version                 int64
	}{
		{name: "revoked grant", reviewer: "user:reviewer", persona: "persona-a", version: 2},
		{name: "author self review", reviewer: "user:business-owner", persona: "persona-a", version: 2},
		{name: "missing version", reviewer: "user:reviewer", persona: "persona-a", version: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := issuer.IssuePersonaReview(context.Background(), "review-issuer-security", tc.persona, tc.version, tc.reviewer, "APPROVE"); !errors.Is(err, ErrPublicationEvidenceRequired) {
				t.Fatalf("review issuance error = %v, want evidence-required denial", err)
			}
		})
	}
	if _, err := issuer.IssuePersonaReview(context.Background(), "review-issuer-security", "persona-a", 2, "user:reviewer", "MAYBE"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported decision error = %v", err)
	}
	if _, err := issuer.IssuePersonaReview(context.Background(), "review-issuer-security", "persona-a", 2, "user:reviewer", "APPROVE"); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("missing current grant error = %v", err)
	}
}

func TestTodo_AGENTP_006_ReviewRevocationRace(t *testing.T) {
	f := newFixture(t, "review-race")
	seedReviewAuthority(t, f, "review-race", "review-race-id", "grant-race", "user:reviewer", "user:author", "APPROVE", false, false)
	store := f.store(t, "review-race")
	readTx, err := store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	authority := testDurableReviewAuthority(t, f)
	resolved, err := authority.ResolvePersonaReview(context.Background(), readTx, "review-race", "persona-a", 3, "sha256:profile-a", "review-race-id")
	if err != nil || !resolved.GrantCurrent {
		t.Fatalf("review before concurrent revocation = %+v, %v", resolved, err)
	}

	writer := reviewAuthorityConnectionForTest(t, f)
	revokeTx, err := writer.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer revokeTx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), revokeTx, f.ids[values.TenantId("review-race")]); err != nil {
		t.Fatal(err)
	}
	if _, err := revokeTx.Exec(context.Background(), `SET LOCAL lock_timeout='500ms'`); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		close(started)
		_, err := revokeTx.Exec(context.Background(), `UPDATE persona_review_grant SET revoked_at=clock_timestamp(),revoked_by='admin:race'
			WHERE tenant_id=$1 AND grant_id='grant-race'`, f.ids[values.TenantId("review-race")])
		finished <- err
	}()
	<-started
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("revocation committed while publication still held the review lock")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("revocation did not observe the bounded lock timeout")
	}
	if err := revokeTx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := readTx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	revokeAfterRead, err := writer.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer revokeAfterRead.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), revokeAfterRead, f.ids[values.TenantId("review-race")]); err != nil {
		t.Fatal(err)
	}
	if _, err := revokeAfterRead.Exec(context.Background(), `UPDATE persona_review_grant SET revoked_at=clock_timestamp(),revoked_by='admin:after-read'
		WHERE tenant_id=$1 AND grant_id='grant-race'`, f.ids[values.TenantId("review-race")]); err != nil {
		t.Fatalf("revoke after publication transaction: %v", err)
	}
	if err := revokeAfterRead.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkTx, err := store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer checkTx.Rollback(context.Background())
	if _, err := authority.ResolvePersonaReview(context.Background(), checkTx, "review-race", "persona-a", 3, "sha256:profile-a", "review-race-id"); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("review after revocation error = %v", err)
	}
}

func durableReviewIssuerForTest(t *testing.T, f *fixture) *DurablePersonaReviewIssuer {
	t.Helper()
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_persona_review_authority"); err != nil {
		t.Fatal(err)
	}
	issuer, err := NewDurablePersonaReviewIssuer(conn, func(tenant values.TenantId) uuid.UUID { return f.ids[tenant] })
	if err != nil {
		t.Fatal(err)
	}
	return issuer
}

func reviewAuthorityConnectionForTest(t *testing.T, f *fixture) dbport.Beginner {
	t.Helper()
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_persona_review_authority"); err != nil {
		t.Fatal(err)
	}
	return conn
}

func seedCurrentReviewGrant(t *testing.T, f *fixture, tenant values.TenantId, grantID, reviewer string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := f.db.SQL.ExecContext(context.Background(), `INSERT INTO persona_review_grant
		(tenant_id,grant_id,principal_id,permission,granted_at,expires_at)
		VALUES ($1,$2,$3,'persona:review',$4,$5)`, f.ids[tenant], grantID, reviewer, now.Add(-time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("seed current persona review grant: %v", err)
	}
}
