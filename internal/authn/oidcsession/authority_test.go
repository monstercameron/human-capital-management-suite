package oidcsession_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidcsession"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

func rev033Principal(t *testing.T, at time.Time) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "worker-1", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-a", Roles: []string{"worker_self"}, Purposes: []string{"self_service_view"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "oidc:verified", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "cred:sha256:verified-id-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func rev033Authority(t *testing.T, at time.Time) (*oidcsession.Authority, *session.Manager) {
	t.Helper()
	manager, err := session.NewManager(session.ManagerConfig{Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := oidcsession.New(oidcsession.Config{
		Sessions: manager, Key: []byte(strings.Repeat("k", 32)), Issuer: "hcmnext-test", Audience: "workspace",
		Now: func() time.Time { return at }, Lifetime: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return authority, manager
}

// TestTodo_REV_033_01 proves an OIDC principal becomes an AUTHN-004 session
// whose exact fingerprint is reconstructed by the signed browser credential.
func TestTodo_REV_033_01(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	authority, _ := rev033Authority(t, at)
	token, err := authority.Issue(context.Background(), rev033Principal(t, at))
	if err != nil {
		t.Fatal(err)
	}
	got, err := authority.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token})
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject() != "worker-1" || got.Tenant() != "tenant-a" || got.SessionRef() == "oidc:verified" || !got.HasRole("worker_self") {
		t.Fatalf("verified session principal = %v", got)
	}
}

// TestTodo_REV_033_01_Security proves forged, wrong-audience, and revoked
// browser credentials fail closed at the verifier boundary.
func TestTodo_REV_033_01_Security(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	authority, _ := rev033Authority(t, at)
	token, err := authority.Issue(context.Background(), rev033Principal(t, at))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Verify(context.Background(), trust.Credential{Token: token + "x"}); !errors.Is(err, oidcsession.ErrInvalidToken) {
		t.Fatalf("tampered credential error = %v", err)
	}
	if _, err := authority.Verify(context.Background(), trust.Credential{Scheme: "Basic", Token: token}); !errors.Is(err, oidcsession.ErrInvalidToken) {
		t.Fatalf("wrong scheme error = %v", err)
	}
	if _, err := authority.Verify(context.Background(), trust.Credential{Token: token}); err != nil {
		t.Fatal(err)
	}
	if err := authority.Revoke(context.Background(), token); err != nil {
		t.Fatalf("revoke OIDC session: %v", err)
	}
	if _, err := authority.Verify(context.Background(), trust.Credential{Token: token}); !errors.Is(err, oidcsession.ErrInvalidToken) {
		t.Fatalf("revoked credential error = %v", err)
	}
}

type fallbackVerifier struct {
	calls  int
	result *trust.Principal
}

func (f *fallbackVerifier) Verify(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
	f.calls++
	if credential.Token != "development-token" {
		return nil, oidcsession.ErrInvalidToken
	}
	return f.result, nil
}

func TestTodo_REV_033_01_FlaggedFallbackBoundary(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	manager, err := session.NewManager(session.ManagerConfig{Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	fallback := &fallbackVerifier{}
	authority, err := oidcsession.New(oidcsession.Config{Sessions: manager, Fallback: fallback, Key: []byte(strings.Repeat("k", 32)), Issuer: "hcmnext-test", Audience: "workspace", Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	// Use a deliberately ordinary verifier principal for the fallback stub.
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "fallback-user", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "dev", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "cred:sha256:fallback"})
	if err != nil {
		t.Fatal(err)
	}
	fallback.result = principal
	got, err := authority.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: "development-token", Audience: "workspace"})
	if err != nil || got.Subject() != "fallback-user" || fallback.calls != 1 {
		t.Fatalf("fallback Verify = %v, %v; calls=%d", got, err, fallback.calls)
	}
	if _, err := authority.Verify(context.Background(), trust.Credential{Token: "hcm-oidc-session-v1.forged.sig"}); !errors.Is(err, oidcsession.ErrInvalidToken) {
		t.Fatalf("forged OIDC token error = %v", err)
	}
	if fallback.calls != 1 {
		t.Fatalf("forged OIDC credential reached fallback %d times", fallback.calls-1)
	}
	if _, err := authority.Verify(context.Background(), trust.Credential{Token: "development-token", Audience: "other"}); !errors.Is(err, oidcsession.ErrInvalidToken) {
		t.Fatalf("wrong audience error = %v", err)
	}
}
