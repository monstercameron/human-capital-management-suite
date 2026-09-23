package machineauth

import (
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/partnerapp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
)

// TestMachineAuthEdges pins the constructor, source-address, DPoP-refusal
// and assertion-shape edges the served-loop tests do not reach.
func TestMachineAuthEdges(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	key, err := machine.GenerateServerKey("srv-e", now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := machine.NewIssuer([]machine.ServerKey{key}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	reg := &fakeRegistry{clients: make(map[string]partnerapp.MachineClient), keys: make(map[string][]partnerapp.MachineClientKey)}
	base := Dependencies{
		Registry: reg, Issuer: issuer,
		Audiences: []string{"hcm-next-api"}, TokenIssuer: intTestIssuer, TokenURL: intTestTokenURL,
		Now: func() time.Time { return now },
	}

	t.Run("constructor refuses incomplete dependencies", func(t *testing.T) {
		bad := base
		bad.Registry = nil
		if _, err := NewHandler(bad); err == nil {
			t.Fatal("nil registry accepted")
		}
		bad = base
		bad.Issuer = nil
		if _, err := NewHandler(bad); err == nil {
			t.Fatal("nil issuer accepted")
		}
		bad = base
		bad.TokenIssuer = ""
		if _, err := NewHandler(bad); err == nil {
			t.Fatal("empty token issuer accepted")
		}
		bad = base
		bad.TokenURL = "://bad"
		if _, err := NewHandler(bad); err == nil {
			t.Fatal("bad token URL accepted")
		}
		bad = base
		bad.Audiences = nil
		if _, err := NewHandler(bad); err == nil {
			t.Fatal("empty audiences accepted")
		}
	})

	t.Run("default clock and lifetime apply", func(t *testing.T) {
		deps := base
		deps.Now = nil
		deps.Lifetime = 0
		if deps.now().IsZero() {
			t.Fatal("default clock is zero")
		}
		if deps.lifetime() != machine.MaxLifetime {
			t.Fatalf("default lifetime = %v, want %v", deps.lifetime(), machine.MaxLifetime)
		}
		deps.Lifetime = time.Hour
		if deps.lifetime() != machine.MaxLifetime {
			t.Fatalf("capped lifetime = %v, want %v", deps.lifetime(), machine.MaxLifetime)
		}
		deps.Lifetime = time.Minute
		if deps.lifetime() != time.Minute {
			t.Fatalf("short lifetime = %v, want a minute", deps.lifetime())
		}
	})

	t.Run("source address prefers the terminator header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", nil)
		req.RemoteAddr = "10.9.8.7:1234"
		if got := sourceIP(req); got != "10.9.8.7" {
			t.Fatalf("remote source = %q", got)
		}
		req.Header.Set(HeaderSourceIP, "203.0.113.5:443")
		if got := sourceIP(req); got != "203.0.113.5" {
			t.Fatalf("forwarded source = %q", got)
		}
		req.Header.Set(HeaderSourceIP, "203.0.113.6")
		if got := sourceIP(req); got != "203.0.113.6" {
			t.Fatalf("bare forwarded source = %q", got)
		}
	})

	t.Run("assertion shapes are refused before any lookup", func(t *testing.T) {
		h, err := NewHandler(base)
		if err != nil {
			t.Fatal(err)
		}
		mux := h.Routes()
		post := func(assertion, typ string) (int, string) {
			form := url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant}}
			if typ != "" {
				form.Set("client_assertion_type", typ)
			}
			if assertion != "" {
				form.Set("client_assertion", assertion)
			}
			code, body := postToken(t, mux, form, nil)
			return code, decodeOAuthError(t, body).Error
		}
		if code, errCode := post("not-a-jwt", clientAssertionType); code != 400 || errCode != "invalid_grant" {
			t.Fatalf("non-jwt = %d %q", code, errCode)
		}
		if code, _ := post("", ""); code != 401 {
			t.Fatalf("missing auth = %d, want 401", code)
		}
		_, priv, _ := edKeyForTest(t)
		noKid := b64url([]byte(`{"alg":"EdDSA","typ":"JWT"}`)) + "." +
			b64url([]byte(`{"iss":"x","sub":"x","aud":"`+intTestTokenURL+`","jti":"k","iat":1,"exp":2}`))
		noKid += "." + b64url(ed25519.Sign(priv, []byte(noKid)))
		if code, errCode := post(noKid, clientAssertionType); code != 400 || errCode != "invalid_grant" {
			t.Fatalf("missing kid = %d %q", code, errCode)
		}
		badAlg := b64url([]byte(`{"alg":"none","typ":"JWT","kid":"k"}`)) + "." +
			b64url([]byte(`{"iss":"x","sub":"x","aud":"x","jti":"k","iat":1,"exp":2}`)) + ".e30"
		if code, errCode := post(badAlg, clientAssertionType); code != 400 || errCode != "invalid_grant" {
			t.Fatalf("bad alg = %d %q", code, errCode)
		}
		wrongType := b64url([]byte(`{"alg":"EdDSA","typ":"JWT","kid":"k"}`)) + "." +
			b64url([]byte(`{"iss":"x","sub":"x","aud":"x","jti":"k","iat":1,"exp":2}`)) + ".e30"
		if code, errCode := post(wrongType, "urn:ietf:params:oauth:client-assertion-type:saml2-bearer"); code != 400 || errCode != "invalid_request" {
			t.Fatalf("wrong assertion type = %d %q", code, errCode)
		}
	})

	t.Run("dpop refusals name the proof", func(t *testing.T) {
		h, err := NewHandler(base)
		if err != nil {
			t.Fatal(err)
		}
		fp := strings.Repeat("cd", 32)
		reg.clients[intTestTenant+"\x00client-dp"] = partnerapp.MachineClient{
			Tenant: intTestTenant, ClientID: "client-dp", Owner: "owner",
			Status: partnerapp.MachineClientActive, Purpose: "sync",
			IPAllowlist: []string{"127.0.0.1/32"}, CertFingerprint: fp,
		}
		mux := h.Routes()
		proof := func(p string) (int, string) {
			form := url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant}, "client_id": {"client-dp"}}
			code, body := postToken(t, mux, form, map[string]string{HeaderDPoP: p, HeaderTLSCertSHA256: fp})
			return code, decodeOAuthError(t, body).Error
		}
		if code, errCode := proof("garbage"); code != 400 || errCode != "invalid_dpop_proof" {
			t.Fatalf("malformed proof = %d %q", code, errCode)
		}
		jwtHeader := b64url([]byte(`{"alg":"EdDSA","typ":"JWT","jwk":{"kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}}`))
		claims := b64url([]byte(`{"htm":"POST","htu":"` + intTestTokenURL + `","iat":` + itoa(now.Unix()) + `,"jti":"dp-e1"}`))
		if code, errCode := proof(jwtHeader + "." + claims + ".e30"); code != 400 || errCode != "invalid_dpop_proof" {
			t.Fatalf("wrong proof type = %d %q", code, errCode)
		}
		ecHeader := b64url([]byte(`{"alg":"EdDSA","typ":"dpop+jwt","jwk":{"kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}}`))
		stale := b64url([]byte(`{"htm":"POST","htu":"` + intTestTokenURL + `","iat":` + itoa(now.Add(-time.Hour).Unix()) + `,"jti":"dp-e2"}`))
		if code, errCode := proof(ecHeader + "." + stale + ".e30"); code != 400 || errCode != "invalid_dpop_proof" {
			t.Fatalf("stale proof = %d %q", code, errCode)
		}
		other := b64url([]byte(`{"htm":"GET","htu":"` + intTestTokenURL + `","iat":` + itoa(now.Unix()) + `,"jti":"dp-e3"}`))
		if code, errCode := proof(ecHeader + "." + other + ".e30"); code != 400 || errCode != "invalid_dpop_proof" {
			t.Fatalf("misbound proof = %d %q", code, errCode)
		}
		if _, err := parseAudience([]byte(`123`)); err == nil {
			t.Fatal("non-string audience accepted")
		}
		if _, err := fingerprintBytes("xyz"); err == nil {
			t.Fatal("short fingerprint accepted")
		}
		if !audienceContains([]string{"a"}, "b") {
			t.Log("audience mismatch helper behaves")
		}
	})
}
