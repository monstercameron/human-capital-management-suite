package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaReviewAuthorityFunc func(context.Context, values.TenantId, string) error

func (f personaReviewAuthorityFunc) AuthorizePersonaReview(ctx context.Context, tenant values.TenantId, reviewer string) error {
	return f(ctx, tenant, reviewer)
}

type personaReviewWriterFunc func(context.Context, values.TenantId, string, int64, string, string) (agentpersonastore.VerifiedReview, error)

func (f personaReviewWriterFunc) IssuePersonaReview(ctx context.Context, tenant values.TenantId, persona string, version int64, reviewer, decision string) (agentpersonastore.VerifiedReview, error) {
	return f(ctx, tenant, persona, version, reviewer, decision)
}

func TestTodo_AGENTP_006_ReviewIssuance(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "user:reviewer", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-a", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	authority := personaReviewAuthorityFunc(func(_ context.Context, tenant values.TenantId, reviewer string) error {
		calls = append(calls, "authorize")
		if tenant != "tenant-a" || reviewer != "user:reviewer" {
			t.Fatalf("authority received tenant=%q reviewer=%q", tenant, reviewer)
		}
		return nil
	})
	writer := personaReviewWriterFunc(func(_ context.Context, tenant values.TenantId, persona string, version int64, reviewer, decision string) (agentpersonastore.VerifiedReview, error) {
		calls = append(calls, "write")
		if tenant != "tenant-a" || persona != "persona-a" || version != 4 || reviewer != "user:reviewer" || decision != "APPROVE" {
			t.Fatalf("issuer received tenant=%q persona=%q version=%d reviewer=%q decision=%q", tenant, persona, version, reviewer, decision)
		}
		return agentpersonastore.VerifiedReview{ReviewID: "review-1", TenantID: "tenant-a", PersonaID: "persona-a", PersonaVersion: 4, ProfileDigest: "sha256:profile", ReviewerID: reviewer, Permission: "persona:review", Decision: decision, ReviewDigest: "sha256:review", GrantCurrent: true}, nil
	})
	issuer, err := NewPersonaReviewIssuanceService(authority, writer)
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	got, err := issuer.Issue(ctx, "persona-a", 4, "APPROVE")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewID != "review-1" || got.ReviewerID != principal.Subject() || len(calls) != 2 || calls[0] != "authorize" || calls[1] != "write" {
		t.Fatalf("issued review = %+v, calls=%v", got, calls)
	}
}

func TestTodo_AGENTP_006_ReviewIssuanceSecurity(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "agent:reviewer", SubjectKind: trust.SubjectKindAgent,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-a", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	authorityCalls, writerCalls := 0, 0
	authority := personaReviewAuthorityFunc(func(context.Context, values.TenantId, string) error {
		authorityCalls++
		return nil
	})
	writer := personaReviewWriterFunc(func(context.Context, values.TenantId, string, int64, string, string) (agentpersonastore.VerifiedReview, error) {
		writerCalls++
		return agentpersonastore.VerifiedReview{}, nil
	})
	issuer, err := NewPersonaReviewIssuanceService(authority, writer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issuer.Issue(context.Background(), "persona-a", 1, "APPROVE"); !errors.Is(err, ErrPersonaReviewDenied) {
		t.Fatalf("missing principal error = %v", err)
	}
	if _, err := issuer.Issue(trust.WithPrincipal(context.Background(), principal), "persona-a", 1, "APPROVE"); !errors.Is(err, ErrPersonaReviewDenied) {
		t.Fatalf("non-human principal error = %v", err)
	}
	if _, err := issuer.Issue(trust.WithPrincipal(context.Background(), principal), "persona-a", 1, "APPROVE "); !errors.Is(err, ErrPersonaReviewInvalid) {
		t.Fatalf("unsupported decision error = %v", err)
	}
	if authorityCalls != 0 || writerCalls != 0 {
		t.Fatalf("authority/writer were called for denied or malformed input: authority=%d writer=%d", authorityCalls, writerCalls)
	}
	denyingAuthority := personaReviewAuthorityFunc(func(context.Context, values.TenantId, string) error { return errors.New("grant revoked") })
	denied, err := NewPersonaReviewIssuanceService(denyingAuthority, writer)
	if err != nil {
		t.Fatal(err)
	}
	if err := denied.Authorize(trust.WithPrincipal(context.Background(), principal)); !errors.Is(err, ErrPersonaReviewDenied) {
		t.Fatalf("revoked grant authorization error = %v", err)
	}
	if _, err := denied.Issue(trust.WithPrincipal(context.Background(), principal), "persona-a", 1, "APPROVE"); !errors.Is(err, ErrPersonaReviewDenied) {
		t.Fatalf("revoked grant issuance error = %v", err)
	}
	if writerCalls != 0 {
		t.Fatalf("writer was called %d times after authority denial", writerCalls)
	}
}

func TestTodo_AGENTP_006_ReviewIssuanceFailsClosed(t *testing.T) {
	allow := personaReviewAuthorityFunc(func(context.Context, values.TenantId, string) error { return nil })
	write := personaReviewWriterFunc(func(context.Context, values.TenantId, string, int64, string, string) (agentpersonastore.VerifiedReview, error) {
		return agentpersonastore.VerifiedReview{}, errors.New("grant unavailable")
	})
	for _, tc := range []struct {
		name      string
		authority PersonaReviewAuthority
		writer    PersonaReviewEvidenceWriter
	}{{name: "nil authority", writer: write}, {name: "nil writer", authority: allow}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewPersonaReviewIssuanceService(tc.authority, tc.writer); !errors.Is(err, ErrPersonaReviewUnavailable) {
				t.Fatalf("constructor error = %v", err)
			}
		})
	}
	issuer, err := NewPersonaReviewIssuanceService(allow, write)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "user:reviewer", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-a", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issuer.Issue(trust.WithPrincipal(context.Background(), principal), "persona-a", 1, "APPROVE"); err == nil {
		t.Fatal("missing current grant unexpectedly issued review evidence")
	}
}
