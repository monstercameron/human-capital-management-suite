package cell

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The end-to-end proof for this file - a real cell, a real websocket upgrade
// and a real RPC through the tunnel - is test/tunnel, because composing a
// cell needs an ephemeral PostgreSQL this package cannot reach. What is
// tested here is everything that is decidable without one: the constructor's
// contract, the origin policy, the admission hook, and the constant the page
// shell mirrors.

// tunnelTestIdentity is the fixture credential this file's admission cases
// run under. It is a real HMAC credential minted by the same verifier the
// cell authenticates with, because a hand-rolled verifier that always says
// yes would prove the hook was called and nothing about what it does.
const (
	tunnelTestIssuer   = "https://issuer.test.hcm-next.invalid"
	tunnelTestAudience = "hcm-next-api"
)

var tunnelTestKey = []byte("hcm-next-tunnel-suite-signing-key-32+++")

// tunnelTestAdmission returns the admission configuration and one credential
// it accepts.
func tunnelTestAdmission(t *testing.T) (transport.Config, string) {
	t.Helper()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      tunnelTestKey,
		Issuer:   tunnelTestIssuer,
		Audience: tunnelTestAudience,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer:               tunnelTestIssuer,
		Audience:             tunnelTestAudience,
		Subject:              "user-tunnel",
		SubjectKind:          "human",
		Tenant:               "acme-industries",
		OrganizationScopeID:  "org-north-america",
		Roles:                []string{"intent_author"},
		Purposes:             []string{"compensation_review"},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-tunnel",
		IssuedAtUnix:         now.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}
	return transport.Config{
		Verifier: verifier, Audience: tunnelTestAudience,
		Now: func() time.Time { return now },
	}, token
}

func TestNewEdgeHandlerWithTunnelRejectsNilCell(t *testing.T) {
	if _, err := NewEdgeHandlerWithTunnel(nil, nil); err == nil {
		t.Fatal("expected an error for a nil cell and a nil server")
	}
}

func TestNewEdgeHandlerWithTunnelRequiresGRPCServer(t *testing.T) {
	_, err := NewEdgeHandlerWithTunnel(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "gRPC server is required") {
		t.Fatalf("want the missing-server refusal, got %v", err)
	}
}

// TestTunnelPathMirrorsWorkspaceConstant pins the one duplicated constant in
// this design. workspace cannot import this package (this package composes
// it), so the page shell restates the tunnel's address; this assertion is
// what keeps the restatement true.
func TestTunnelPathMirrorsWorkspaceConstant(t *testing.T) {
	if workspace.PathTunnel != TunnelPath {
		t.Fatalf("workspace.PathTunnel = %q, cell.TunnelPath = %q", workspace.PathTunnel, TunnelPath)
	}
	if TunnelPath != "/workspace/grpc" {
		t.Fatalf("TunnelPath = %q, want /workspace/grpc", TunnelPath)
	}
}

func TestSameOriginOnly(t *testing.T) {
	cases := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{name: "absent origin is not a browser holding the page", host: "cell.test", origin: "", want: false},
		{name: "same origin over http", host: "cell.test", origin: "http://cell.test", want: true},
		{name: "same origin over https", host: "cell.test", origin: "https://cell.test", want: true},
		{name: "same host and port", host: "cell.test:8080", origin: "http://cell.test:8080", want: true},
		{name: "case-insensitive host", host: "Cell.Test", origin: "http://cell.test", want: true},
		{name: "different host", host: "cell.test", origin: "https://evil.example", want: false},
		{name: "different port is a different origin", host: "cell.test:8080", origin: "http://cell.test:9090", want: false},
		{name: "unparseable origin", host: "cell.test", origin: "://", want: false},
		{name: "origin with no host", host: "cell.test", origin: "null", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://"+tc.host+TunnelPath, nil)
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := sameOriginOnly(r); got != tc.want {
				t.Fatalf("sameOriginOnly(origin=%q, host=%q) = %v, want %v", tc.origin, tc.host, got, tc.want)
			}
		})
	}
	if sameOriginOnly(nil) {
		t.Fatal("a nil request must not pass the origin check")
	}
}

// TestTunnelOriginCheckAcceptsTheDeclaredPublicOrigin is the deployment the
// flag exists for: a TLS-terminating proxy delivers the upgrade with the
// internal listener's Host while the browser's Origin carries the public
// authority the cell declared.
func TestTunnelOriginCheckAcceptsTheDeclaredPublicOrigin(t *testing.T) {
	check := tunnelOriginCheck("hcm.example.com")
	upgrade := func(origin string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://internal:8080"+TunnelPath, nil)
		r.Host = "internal:8080"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	if !check(upgrade("https://hcm.example.com")) {
		t.Fatal("the declared public origin must admit despite a rewritten Host")
	}
	if !check(upgrade("http://hcm.example.com")) {
		t.Fatal("the public authority is matched by host, not scheme")
	}
	if check(upgrade("https://evil.example")) {
		t.Fatal("an unrelated origin must not admit")
	}
	if check(upgrade("")) {
		t.Fatal("a client with no Origin is not a browser holding the page and must not admit")
	}
	if check(nil) {
		t.Fatal("a nil request must not pass")
	}
	if got := tunnelOriginCheck(""); got == nil || !got(upgrade("http://internal:8080")) || got(upgrade("https://hcm.example.com")) {
		t.Fatal("with no declared origin the check is sameOriginOnly unchanged")
	}
}

func TestTunnelAuthorizerAdmitsABearerCredential(t *testing.T) {
	cfg, token := tunnelTestAdmission(t)
	authorize := tunnelAuthorizer(cfg, false)
	r := httptest.NewRequest(http.MethodGet, TunnelPath, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	if err := authorize(r); err != nil {
		t.Fatalf("a valid credential must admit: %v", err)
	}
}

func TestTunnelAuthorizerRefusesWithoutACredential(t *testing.T) {
	cfg, _ := tunnelTestAdmission(t)
	authorize := tunnelAuthorizer(cfg, false)
	r := httptest.NewRequest(http.MethodGet, TunnelPath, nil)
	if err := authorize(r); err == nil {
		t.Fatal("an unauthenticated upgrade must be refused")
	}
	if err := authorize(nil); err == nil {
		t.Fatal("a nil upgrade request must be refused")
	}
}

func TestTunnelAuthorizerRefusesAnInvalidCredential(t *testing.T) {
	cfg, _ := tunnelTestAdmission(t)
	authorize := tunnelAuthorizer(cfg, false)
	r := httptest.NewRequest(http.MethodGet, TunnelPath, nil)
	r.Header.Set("Authorization", "Bearer not-a-credential")
	if err := authorize(r); err == nil {
		t.Fatal("an invalid credential must be refused")
	}
}

// TestTunnelAuthorizerRefusesCallerSelectedAuthority proves the upgrade runs
// the whole of PreAdmit, not only authentication: a request that tries to
// select its own trusted context is refused before the socket exists.
func TestTunnelAuthorizerRefusesCallerSelectedAuthority(t *testing.T) {
	cfg, token := tunnelTestAdmission(t)
	authorize := tunnelAuthorizer(cfg, false)
	r := httptest.NewRequest(http.MethodGet, TunnelPath, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-HCM-Roles", "operator")
	err := authorize(r)
	if err == nil {
		t.Fatal("a caller-selected trusted-context header must be refused")
	}
	if !strings.Contains(err.Error(), "trusted_context.caller_selected_authority") {
		t.Fatalf("want the trusted-context refusal, got %v", err)
	}
}

// TestTunnelAuthorizerDevLoginCookie shows the cookie substitution is gated
// entirely behind the cell's own DevBrowserLogin decision: the same request
// admits with it on and is refused with it off.
func TestTunnelAuthorizerDevLoginCookie(t *testing.T) {
	cfg, token := tunnelTestAdmission(t)
	build := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, TunnelPath, nil)
		r.AddCookie(&http.Cookie{Name: "hcmnext_session", Value: token})
		return r
	}
	if err := tunnelAuthorizer(cfg, true)(build()); err != nil {
		t.Fatalf("with dev browser login on, the session cookie must admit: %v", err)
	}
	if err := tunnelAuthorizer(cfg, false)(build()); err == nil {
		t.Fatal("with dev browser login off, the session cookie must not admit")
	}
}

// TestTunnelAuthorizerPrefersTheHeader shows the cookie never overrides a
// credential the request actually presented.
func TestTunnelAuthorizerPrefersTheHeader(t *testing.T) {
	cfg, token := tunnelTestAdmission(t)
	authorize := tunnelAuthorizer(cfg, true)
	r := httptest.NewRequest(http.MethodGet, TunnelPath, nil)
	r.Header.Set("Authorization", "Bearer not-a-credential")
	r.AddCookie(&http.Cookie{Name: "hcmnext_session", Value: token})
	if err := authorize(r); err == nil {
		t.Fatal("the presented header must be the credential, not the cookie")
	}
}

// TestTunnelAuthorizerReturnsAnOwnedError pins that the bridge's log carries
// the same reason identifier every other surface records.
func TestTunnelAuthorizerReturnsAnOwnedError(t *testing.T) {
	cfg, _ := tunnelTestAdmission(t)
	authorize := tunnelAuthorizer(cfg, false)
	err := authorize(httptest.NewRequest(http.MethodGet, TunnelPath, nil))
	if err == nil {
		t.Fatal("expected a refusal")
	}
	var owned interface{ ReasonRef() string }
	if !errors.As(err, &owned) {
		t.Fatalf("want an owned envelope error, got %T", err)
	}
}
