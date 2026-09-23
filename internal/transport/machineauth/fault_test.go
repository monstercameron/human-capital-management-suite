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

func faultFixture(t *testing.T) (*intFixture, ed25519.PrivateKey) {
	t.Helper()
	f := newIntFixture(t)
	_, priv, jwk := edKeyForTest(t)
	f.addClient(t, partnerapp.MachineClient{
		ClientID: "client-f", Owner: "owner-f", Scopes: []string{"intents.read"},
		Purpose: "sync", IPAllowlist: []string{"127.0.0.1/32"},
	}, []partnerapp.MachineClientKey{{
		ClientID: "client-f", KID: "ck-f", Alg: machine.AlgEdDSA, JWK: jwk,
		NotBefore: f.now.Add(-time.Hour),
	}})
	return f, priv
}

func goodAssertion(t *testing.T, f *intFixture, priv ed25519.PrivateKey, client, kid string, iat, exp time.Time) string {
	t.Helper()
	return mintAssertion(t, machine.AlgEdDSA, kid, client, iat, exp,
		func(input string) []byte { return ed25519.Sign(priv, []byte(input)) })
}

// TestTodo_INTAPI_001_Fault proves every malformed, unauthenticated or
// unauthorized token request fails with its RFC 6749 code and never mints.
func TestTodo_INTAPI_001_Fault(t *testing.T) {
	f, priv := faultFixture(t)
	mux := f.handler.Routes()
	assert := func(client, kid string, iat, exp time.Time) string {
		return goodAssertion(t, f, priv, client, kid, iat, exp)
	}
	window := func() (time.Time, time.Time) { return f.now.Add(-time.Minute), f.now.Add(4 * time.Minute) }

	cases := []struct {
		name    string
		form    url.Values
		headers map[string]string
		status  int
		code    string
	}{
		{
			name:   "wrong grant type",
			form:   url.Values{"grant_type": {"authorization_code"}, "tenant": {intTestTenant}},
			status: http.StatusBadRequest, code: "unsupported_grant_type",
		},
		{
			name:   "missing tenant",
			form:   url.Values{"grant_type": {"client_credentials"}},
			status: http.StatusBadRequest, code: "invalid_request",
		},
		{
			name:   "no client authentication",
			form:   url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant}},
			status: http.StatusUnauthorized, code: "invalid_client",
		},
		{
			name: "unknown client with a well-formed assertion",
			form: url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant},
				"client_assertion_type": {clientAssertionType},
				"client_assertion":      {assert("ghost", "ck-f", f.now.Add(-time.Minute), f.now.Add(4*time.Minute))}},
			status: http.StatusUnauthorized, code: "invalid_client",
		},
		{
			name: "forged assertion signature",
			form: func() url.Values {
				iat, exp := window()
				bad := assert("client-f", "ck-f", iat, exp) + "AA"
				return url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant},
					"client_assertion_type": {clientAssertionType}, "client_assertion": {bad}}
			}(),
			status: http.StatusBadRequest, code: "invalid_grant",
		},
		{
			name: "expired assertion",
			form: url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant},
				"client_assertion_type": {clientAssertionType},
				"client_assertion":      {assert("client-f", "ck-f", f.now.Add(-10*time.Minute), f.now.Add(-6*time.Minute))}},
			status: http.StatusBadRequest, code: "invalid_grant",
		},
		{
			name: "assertion window wider than five minutes",
			form: url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant},
				"client_assertion_type": {clientAssertionType},
				"client_assertion":      {assert("client-f", "ck-f", f.now.Add(-time.Minute), f.now.Add(10*time.Minute))}},
			status: http.StatusBadRequest, code: "invalid_grant",
		},
		{
			name: "unknown assertion key",
			form: url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant},
				"client_assertion_type": {clientAssertionType},
				"client_assertion": {mintAssertion(t, machine.AlgEdDSA, "ck-nope", "client-f",
					f.now.Add(-time.Minute), f.now.Add(4*time.Minute),
					func(input string) []byte { return ed25519.Sign(priv, []byte(input)) })}},
			status: http.StatusBadRequest, code: "invalid_grant",
		},
		{
			name: "scope beyond the grant",
			form: func() url.Values {
				iat, exp := window()
				return url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant},
					"scope":                 {"intents.read workers.write"},
					"client_assertion_type": {clientAssertionType},
					"client_assertion":      {assert("client-f", "ck-f", iat, exp)}}
			}(),
			status: http.StatusBadRequest, code: "invalid_scope",
		},
		{
			name: "mutual TLS with an unbound fingerprint",
			form: url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant}, "client_id": {"client-f"}},
			headers: map[string]string{
				HeaderTLSCertSHA256: strings.Repeat("0", 64),
			},
			status: http.StatusUnauthorized, code: "invalid_client",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := postToken(t, mux, tc.form, tc.headers)
			if code != tc.status {
				t.Fatalf("status = %d, want %d (body = %s)", code, tc.status, body)
			}
			if got := decodeOAuthError(t, body).Error; got != tc.code {
				t.Fatalf("code = %q, want %q (body = %s)", got, tc.code, body)
			}
		})
	}

	t.Run("a spent assertion is refused on replay", func(t *testing.T) {
		iat, exp := window()
		form := url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant},
			"client_assertion_type": {clientAssertionType},
			"client_assertion":      {assert("client-f", "ck-f", iat, exp)}}
		if code, _ := postToken(t, mux, form, nil); code != http.StatusOK {
			t.Fatalf("first use status = %d, want 200", code)
		}
		code, body := postToken(t, mux, form, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("replay status = %d, want 400 (body = %s)", code, body)
		}
		if got := decodeOAuthError(t, body).Error; got != "invalid_grant" {
			t.Fatalf("replay code = %q, want invalid_grant", got)
		}
	})

	t.Run("suspended client and denied source fail", func(t *testing.T) {
		f.registry.clients[intTestTenant+"\x00client-f"] = partnerapp.MachineClient{
			Tenant: intTestTenant, ClientID: "client-f", Owner: "owner-f", Status: partnerapp.MachineClientSuspended,
			Scopes: []string{"intents.read"}, Purpose: "sync", IPAllowlist: []string{"127.0.0.1/32"},
		}
		iat, exp := window()
		form := url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant},
			"client_assertion_type": {clientAssertionType},
			"client_assertion":      {assert("client-f", "ck-f", iat, exp)}}
		if code, body := postToken(t, mux, form, nil); code != http.StatusBadRequest ||
			decodeOAuthError(t, body).Error != "invalid_grant" {
			t.Fatalf("suspended = %d %s, want 400 invalid_grant", code, body)
		}
		f.registry.clients[intTestTenant+"\x00client-f"] = partnerapp.MachineClient{
			Tenant: intTestTenant, ClientID: "client-f", Owner: "owner-f", Status: partnerapp.MachineClientActive,
			Scopes: []string{"intents.read"}, Purpose: "sync", IPAllowlist: []string{"10.0.0.0/8"},
		}
		iat, exp = window()
		form = url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant},
			"client_assertion_type": {clientAssertionType},
			"client_assertion":      {assert("client-f", "ck-f", iat, exp)}}
		if code, body := postToken(t, mux, form, nil); code != http.StatusBadRequest ||
			decodeOAuthError(t, body).Error != "invalid_grant" {
			t.Fatalf("denied source = %d %s, want 400 invalid_grant", code, body)
		}
	})

	t.Run("write-capable client without possession proof is refused", func(t *testing.T) {
		f.registry.clients[intTestTenant+"\x00client-w"] = partnerapp.MachineClient{
			Tenant: intTestTenant, ClientID: "client-w", Owner: "owner-w",
			Status: partnerapp.MachineClientActive, Scopes: []string{"intents.read", "intents.write"},
			Purpose: "sync", IPAllowlist: []string{"127.0.0.1/32"},
		}
		f.registry.keys[intTestTenant+"\x00client-w"] = []partnerapp.MachineClientKey{{
			ClientID: "client-w", KID: "ck-w", Alg: machine.AlgEdDSA,
			JWK:       f.registry.keys[intTestTenant+"\x00client-f"][0].JWK,
			NotBefore: f.now.Add(-time.Hour),
		}}
		iat, exp := window()
		form := url.Values{"grant_type": {"client_credentials"}, "tenant": {intTestTenant},
			"client_assertion_type": {clientAssertionType},
			"client_assertion": {mintAssertion(t, machine.AlgEdDSA, "ck-w", "client-w", iat, exp,
				func(input string) []byte { return ed25519.Sign(priv, []byte(input)) })}}
		if code, body := postToken(t, mux, form, nil); code != http.StatusBadRequest ||
			decodeOAuthError(t, body).Error != "invalid_grant" {
			t.Fatalf("write-capable Bearer [REDACTED] = %d %s, want 400 invalid_grant", code, body)
		}
	})

	t.Run("wrong content type and method are refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/oauth2/token", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET status = %d, want 400 or 405", rec.Code)
		}
		bad := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=client_credentials"))
		bad.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, bad)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("JSON status = %d, want 400", rec.Code)
		}
		if got := decodeOAuthError(t, rec.Body.Bytes()).Error; got != "invalid_request" {
			t.Fatalf("JSON code = %q, want invalid_request", got)
		}
	})
}
