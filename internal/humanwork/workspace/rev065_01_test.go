package workspace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidcsession"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// rev065IssueToken mints a shell credential expiring exactly at expiresAt.
func rev065IssueToken(t *testing.T, expiresAt time.Time) (trust.Verifier, string) {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience,
		Now: func() time.Time { return shellNow },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer: shellIssuer, Audience: shellAudience, Subject: shellSubject,
		SubjectKind: "human", Tenant: shellTenant,
		Roles:                []string{"intent_author", "comp_admin"},
		AuthenticationMethod: "bearer_token", Assurance: "substantial",
		SessionRef:    "session-rev065",
		IssuedAtUnix:  shellNow.Add(-time.Minute).Unix(),
		ExpiresAtUnix: expiresAt.Unix(),
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}
	return verifier, token
}

func rev065Handler(t *testing.T, verifier trust.Verifier) *Handler {
	t.Helper()
	h, err := NewHandler(Options{
		Cell: unreachableCell{t: t},
		Config: transport.Config{
			Verifier: verifier, Audience: shellAudience,
			Now: func() time.Time { return shellNow },
		},
		Now: func() time.Time { return shellNow },
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h
}

func rev065Get(t *testing.T, h *Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "http://cell.test"+path, nil)
	r.Host = "cell.test"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestTodo_REV_065_01(t *testing.T) {
	if got := resolveSessionLifecycle(nil, shellNow); got != nil {
		t.Fatalf("nil principal resolves to %+v, want nil", got)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(shellTenant), Subject: shellSubject,
		SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-rev065", CredentialDigest: "rev065-credential",
		IssuedAt: shellNow.Add(-time.Minute), ExpiresAt: shellNow.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	healthy := resolveSessionLifecycle(principal, shellNow)
	if healthy == nil || healthy.SessionRef != "session-rev065" || healthy.ExpiresAtUnix != shellNow.Add(time.Hour).Unix() {
		t.Fatalf("healthy session resolves to %+v, want carried facts", healthy)
	}
	if healthy.Warning {
		t.Fatalf("healthy session resolves to %+v, want no warning", healthy)
	}
	expiring, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(shellTenant), Subject: shellSubject,
		SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-rev065", CredentialDigest: "rev065-credential",
		IssuedAt: shellNow.Add(-time.Minute), ExpiresAt: shellNow.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	warn := resolveSessionLifecycle(expiring, shellNow)
	if warn == nil || !warn.Warning || warn.RemainingMinutes != 3 {
		t.Fatalf("expiring session resolves to %+v, want warning with 3 minutes", warn)
	}
	expired, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(shellTenant), Subject: shellSubject,
		SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-rev065", CredentialDigest: "rev065-credential",
		IssuedAt: shellNow.Add(-2 * time.Hour), ExpiresAt: shellNow.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	if got := resolveSessionLifecycle(expired, shellNow); got != nil {
		t.Fatalf("expired session resolves to %+v, want nil (admission owns enforcement)", got)
	}
	locale := productui.ResolveProductLocale("en-US")
	view := applySessionLifecycle(productui.NewView(productui.PageHome, "Acme", "user", ""), nil, locale)
	if view.SessionWarning != nil {
		t.Fatal("nil session facts project a warning")
	}
	view = applySessionLifecycle(productui.NewView(productui.PageHome, "Acme", "user", ""), healthy, locale)
	if view.SessionWarning != nil {
		t.Fatal("healthy session projects a warning")
	}
	view = applySessionLifecycle(productui.NewView(productui.PageHome, "Acme", "user", ""), warn, locale)
	if view.SessionWarning == nil {
		t.Fatal("expiring session projects no warning")
	}
	if !strings.Contains(view.SessionWarning.Detail, "3") || view.SessionWarning.ReauthHref != "/workspace/login" {
		t.Fatalf("warning projection = %+v, want countdown detail and login reauth", view.SessionWarning)
	}
	var config JourneyConfig
	config.Session = warn
	if config.Session == nil || !config.Session.Warning {
		t.Fatal("JourneyConfig does not carry the session projection")
	}
}

func TestTodo_REV_065_01_Security(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	manager, err := session.NewManager(session.ManagerConfig{Now: func() time.Time { return at }})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	authority, err := oidcsession.New(oidcsession.Config{
		Sessions: manager, Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience,
		Now: func() time.Time { return at }, Lifetime: time.Hour,
	})
	if err != nil {
		t.Fatalf("oidcsession.New: %v", err)
	}
	login, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(shellTenant), Subject: shellSubject,
		SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodOIDC,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-rev065", CredentialDigest: "rev065-credential",
		IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	token, err := authority.Issue(context.Background(), login)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	h, err := NewHandler(Options{
		Cell: unreachableCell{t: t},
		Config: transport.Config{
			Verifier: authority, Audience: shellAudience,
			Now: func() time.Time { return at },
		},
		Now: func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	live := rev065Get(t, h, productui.Path(productui.PageHome), token)
	if live.Code != http.StatusOK {
		t.Fatalf("live session page status = %d, want 200", live.Code)
	}
	if err := authority.Revoke(context.Background(), token); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	revoked := rev065Get(t, h, productui.Path(productui.PageHome), token)
	garbage := rev065Get(t, h, productui.Path(productui.PageHome), "bogus-credential")
	if revoked.Code != http.StatusUnauthorized || garbage.Code != http.StatusUnauthorized {
		t.Fatalf("statuses = %d/%d, want 401/401", revoked.Code, garbage.Code)
	}
	if revoked.Body.String() != garbage.Body.String() {
		t.Fatalf("revoked response differs from garbage response:\n%s\nvs\n%s", revoked.Body.String(), garbage.Body.String())
	}
	for _, body := range []string{revoked.Body.String(), garbage.Body.String()} {
		if !strings.Contains(body, "Not authenticated") {
			t.Fatalf("401 body %q is not the generic unauthenticated outcome", body)
		}
		for _, leaked := range []string{"session-warning", "journey-config", "Signed out", "session-rev065"} {
			if strings.Contains(body, leaked) {
				t.Fatalf("401 body leaks %q", leaked)
			}
		}
	}
	expiredVerifier, expiredToken := rev065IssueToken(t, shellNow.Add(-time.Minute))
	expiredHandler := rev065Handler(t, expiredVerifier)
	expired := rev065Get(t, expiredHandler, productui.Path(productui.PageHome), expiredToken)
	if expired.Code != http.StatusUnauthorized || !strings.Contains(expired.Body.String(), "Not authenticated") {
		t.Fatalf("expired session status/body = %d/%q, want generic 401", expired.Code, expired.Body.String())
	}
	if strings.Contains(expired.Body.String(), "session-warning") {
		t.Fatal("expired session renders a warning")
	}
}

func TestTodo_REV_065_01_Integration(t *testing.T) {
	verifier, warningToken := rev065IssueToken(t, shellNow.Add(3*time.Minute))
	h := rev065Handler(t, verifier)
	page := rev065Get(t, h, productui.Path(productui.PageHome), warningToken)
	if page.Code != http.StatusOK {
		t.Fatalf("expiring session page status = %d, want 200", page.Code)
	}
	if !strings.Contains(page.Body.String(), `id="session-warning"`) {
		t.Fatal("expiring session page renders no warning banner")
	}
	if !strings.Contains(page.Body.String(), "expires in") {
		t.Fatalf("warning banner carries no countdown detail: %q", page.Body.String())
	}
	_, healthyToken := rev065IssueToken(t, shellNow.Add(time.Hour))
	healthy := rev065Get(t, h, productui.Path(productui.PageHome), healthyToken)
	if healthy.Code != http.StatusOK {
		t.Fatalf("healthy session page status = %d, want 200", healthy.Code)
	}
	if strings.Contains(healthy.Body.String(), "session-warning") {
		t.Fatal("healthy session page renders a warning")
	}
	journey := getJourney(t, h, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+warningToken)
	})
	if journey.Code != http.StatusOK {
		t.Fatalf("journey shell status = %d, want 200", journey.Code)
	}
	config := island(t, journey.Body.String())
	if config.Session == nil || !config.Session.Warning || config.Session.SessionRef != "session-rev065" {
		t.Fatalf("journey island session = %+v, want carried warning facts", config.Session)
	}
}
