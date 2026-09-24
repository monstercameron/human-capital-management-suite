package workspace

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidcsession"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

type oidcHandlerSessionIssuer struct{}

func (oidcHandlerSessionIssuer) Issue(context.Context, *trust.Principal) (string, error) {
	return "", nil
}
func (oidcHandlerSessionIssuer) Verify(context.Context, trust.Credential) (*trust.Principal, error) {
	return nil, trust.ErrInvalidCredential
}

func newOIDCHandlerForTest(t *testing.T) *Handler {
	t.Helper()
	verifier := oidcHandlerSessionIssuer{}
	h, err := NewHandler(Options{
		Cell: unreachableCell{t}, Config: transport.Config{Verifier: verifier, Audience: "workspace"},
		OIDCFlow: &oidc.Flow{}, OIDCTenant: values.TenantId("tenant-a"),
		OIDCIssuerURL: "https://idp.example/tenant-a", OIDCSessionIssuer: verifier,
		Now: func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }, Secure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestTodo_REV_033_01_WorkspaceOIDCRoutesAndFlaggedFallback(t *testing.T) {
	h := newOIDCHandlerForTest(t)
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest(http.MethodGet, PathLogin, nil))
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), "Sign in with your organization") {
		t.Fatalf("OIDC login page status/body = %d/%q", login.Code, login.Body.String())
	}
	if strings.Contains(login.Body.String(), "Development fallback") || strings.Contains(login.Body.String(), `name="token"`) {
		t.Fatal("production OIDC page exposed pasted-token fallback")
	}
	callback := httptest.NewRecorder()
	h.ServeHTTP(callback, httptest.NewRequest(http.MethodGet, PathOIDCCallback+"?state=attacker-state&code=attacker-code", nil))
	if callback.Code != http.StatusUnauthorized {
		t.Fatalf("unbound callback status = %d, want 401", callback.Code)
	}
	for _, cookie := range callback.Result().Cookies() {
		if cookie.Name == loginSessionCookie {
			t.Fatal("unbound callback minted a browser session")
		}
		if cookie.Name == oidcStateCookie && cookie.MaxAge != -1 {
			t.Fatalf("state cookie was not cleared: %+v", cookie)
		}
	}
}

func TestTodo_REV_033_01_WorkspaceOIDCStateMustMatchBrowser(t *testing.T) {
	h := newOIDCHandlerForTest(t)
	request := httptest.NewRequest(http.MethodGet, PathOIDCCallback+"?state=query-state&code=code", nil)
	request.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "different-browser-state"})
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("mismatched callback status = %d, want 401", response.Code)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == loginSessionCookie {
			t.Fatal("mismatched callback minted a browser session")
		}
	}
}

func TestTodo_REV_033_01_WorkspaceOIDCCallbackMintsVerifiedSession(t *testing.T) {
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	tenant := values.TenantId("tenant-a")
	issuerURL := "https://idp.example/tenant-a"
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	registry := issuerregistry.NewMemoryStore()
	registered, err := issuerregistry.Publish(registry, issuerregistry.Issuer{
		Tenant: tenant, IssuerURL: issuerURL, Audience: "client-a",
		JWKS:       issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedKeys, PinnedKeys: []issuerregistry.PinnedKey{{KeyID: "key-a", Algorithm: trustfederation.AlgRS256, PublicKeyDER: der}}},
		Algorithms: []trustfederation.Algorithm{trustfederation.AlgRS256}, ClockSkew: time.Minute, MetadataStaleness: 24 * time.Hour,
		Revision: 1, PublisherPrincipal: "test-publisher", PublishedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issuerregistry.Activate(registry, registered.Ref(), issuerregistry.Evidence{ActedBy: "test-approver", Authority: "test", Reason: "test", At: at}); err != nil {
		t.Fatal(err)
	}
	var expectedNonce string
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "test-code" || r.Form.Get("code_verifier") == "" {
			http.Error(w, "bad authorization exchange", http.StatusBadRequest)
			return
		}
		idToken := signTestIDToken(t, key, map[string]any{"iss": issuerURL, "sub": "employee-7", "aud": "client-a", "iat": at.Unix(), "exp": at.Add(time.Hour).Unix(), "nonce": expectedNonce})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": idToken, "token_type": "Bearer", "access_token": "provider-access-token"})
	}))
	defer tokenServer.Close()
	flow, err := oidc.NewFlow(oidc.FlowConfig{
		Registry: registry, Keys: issuerregistry.NewTenantResolver(registry, tenant, nil),
		Clients: oidc.NewStaticClientSource().WithClient(oidc.ClientRegistration{Tenant: tenant, IssuerURL: issuerURL, ClientID: "client-a", RedirectURI: "https://app.example" + PathOIDCCallback, AuthorizationEndpoint: "https://idp.example/authorize", TokenEndpoint: tokenServer.URL, Scopes: []string{"openid"}}),
		States:  oidc.NewMemoryStateStore(), Exchanger: oidc.HTTPTokenExchanger{}, Secret: []byte(strings.Repeat("p", 32)),
	})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := session.NewManager(session.ManagerConfig{Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := oidcsession.New(oidcsession.Config{Sessions: manager, Key: []byte(strings.Repeat("s", 32)), Issuer: "hcmnext-test", Audience: "workspace-a", Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{Cell: unreachableCell{t}, Config: transport.Config{Verifier: authority, Audience: "workspace-a"}, OIDCFlow: flow, OIDCTenant: tenant, OIDCIssuerURL: issuerURL, OIDCSessionIssuer: authority, Now: func() time.Time { return at }, Secure: true})
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "https://app.example"+PathOIDCLogin, nil))
	if login.Code != http.StatusFound {
		t.Fatalf("OIDC login status = %d body=%s", login.Code, login.Body.String())
	}
	location, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Query().Get("code_challenge_method") != "S256" || location.Query().Get("code_challenge") == "" {
		t.Fatalf("authorization redirect lacks PKCE S256: %s", location)
	}
	expectedNonce = location.Query().Get("nonce")
	var browserState *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == oidcStateCookie {
			browserState = cookie
		}
	}
	if browserState == nil || !browserState.HttpOnly || !browserState.Secure || browserState.SameSite != http.SameSiteLaxMode {
		t.Fatalf("OIDC state cookie is not browser-bound and hardened: %+v", browserState)
	}
	callbackReq := httptest.NewRequest(http.MethodGet, "https://app.example"+PathOIDCCallback+"?state="+url.QueryEscape(location.Query().Get("state"))+"&code=test-code", nil)
	callbackReq.AddCookie(browserState)
	callback := httptest.NewRecorder()
	h.ServeHTTP(callback, callbackReq)
	if callback.Code != http.StatusSeeOther {
		t.Fatalf("callback status = %d body=%s", callback.Code, callback.Body.String())
	}
	var accessCookie *http.Cookie
	for _, cookie := range callback.Result().Cookies() {
		if cookie.Name == loginSessionCookie {
			accessCookie = cookie
		}
	}
	if accessCookie == nil || !accessCookie.HttpOnly || !accessCookie.Secure {
		t.Fatalf("callback did not set a hardened session cookie: %+v", accessCookie)
	}
	principal, err := authority.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: accessCookie.Value, Audience: "workspace-a"})
	if err != nil {
		t.Fatalf("verify issued browser session: %v", err)
	}
	if principal.Subject() != "employee-7" || principal.Tenant() != tenant || principal.SessionRef() == "" {
		t.Fatalf("issued session principal = %+v", principal)
	}
	logoutReq := httptest.NewRequest(http.MethodGet, "https://app.example"+PathLogout, nil)
	logoutReq.AddCookie(accessCookie)
	logout := httptest.NewRecorder()
	h.ServeHTTP(logout, logoutReq)
	if logout.Code != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want 303", logout.Code)
	}
	if _, err := authority.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: accessCookie.Value, Audience: "workspace-a"}); err == nil {
		t.Fatal("logout left the AUTHN-004 session active")
	}
}

func signTestIDToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	encode := func(value any) string {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	header := encode(map[string]string{"alg": "RS256", "kid": "key-a", "typ": "JWT"})
	body := encode(claims)
	input := header + "." + body
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}
