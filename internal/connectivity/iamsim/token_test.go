package iamsim

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func decodeToken(t *testing.T, rec *httptest.ResponseRecorder) TokenResponse {
	t.Helper()
	var tr TokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &tr); err != nil {
		t.Fatalf("decode token %q: %v", rec.Body, err)
	}
	return tr
}

func decodeOAuthError(t *testing.T, rec *httptest.ResponseRecorder) OAuthError {
	t.Helper()
	var e OAuthError
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode oauth error %q: %v", rec.Body, err)
	}
	return e
}

func ccForm() url.Values { return url.Values{"grant_type": {"client_credentials"}} }

func TestTokenBasicAuthSuccess(t *testing.T) {
	h := newHarness(t, nil)
	rec := tokenCall(t, h.s, ccForm(), basicAuth(testClientID, testClientSecret), formType)
	if rec.Code != http.StatusOK {
		t.Fatalf("token = %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("cache headers = %v", rec.Header())
	}
	tr := decodeToken(t, rec)
	if tr.TokenType != "Bearer" || tr.ExpiresIn != 300 || tr.Scope != "access.read access.write" {
		t.Fatalf("token = %+v", tr)
	}
	raw, err := base64.RawURLEncoding.DecodeString(tr.AccessToken)
	if err != nil || len(raw) < 32 {
		t.Fatalf("token %q is not >=32 bytes base64url: %v", tr.AccessToken, err)
	}
}

// TestTokenBasicAuthDecodesFormEncodedCredentials proves the RFC 6749
// section 2.3.1 rule: Basic halves are form-urlencoded before base64, so a
// secret containing ':' or '+' still authenticates.
func TestTokenBasicAuthDecodesFormEncodedCredentials(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.ClientSecret = "p@ss:word+1 x" })
	rec := tokenCall(t, h.s, ccForm(), basicAuth(testClientID, "p@ss:word+1 x"), formType)
	if rec.Code != http.StatusOK {
		t.Fatalf("token = %d %s", rec.Code, rec.Body)
	}
	lower := "basic " + strings.TrimPrefix(basicAuth(testClientID, "p@ss:word+1 x"), "Basic ")
	if rec := tokenCall(t, h.s, ccForm(), lower, formType); rec.Code != http.StatusOK {
		t.Fatalf("lowercase scheme = %d", rec.Code)
	}
}

func TestTokenFormFieldAuthSuccess(t *testing.T) {
	h := newHarness(t, nil)
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {testClientID}, "client_secret": {testClientSecret}, "scope": {"access.read"}}
	rec := tokenCall(t, h.s, form, "", formType)
	if rec.Code != http.StatusOK {
		t.Fatalf("token = %d %s", rec.Code, rec.Body)
	}
	if tr := decodeToken(t, rec); tr.Scope != "access.read" || tr.AccessToken == "" {
		t.Fatalf("token = %+v", tr)
	}
}

func TestTokenBothAuthMethodsIsInvalidRequest(t *testing.T) {
	h := newHarness(t, nil)
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {testClientID}, "client_secret": {testClientSecret}}
	rec := tokenCall(t, h.s, form, basicAuth(testClientID, testClientSecret), formType)
	if rec.Code != http.StatusBadRequest || decodeOAuthError(t, rec).Error != "invalid_request" {
		t.Fatalf("both methods = %d %s", rec.Code, rec.Body)
	}
}

func TestTokenInvalidClient(t *testing.T) {
	h := newHarness(t, nil)
	cases := map[string]struct {
		form  url.Values
		authz string
	}{
		"basic wrong secret": {ccForm(), basicAuth(testClientID, "wrong")},
		"basic wrong id":     {ccForm(), basicAuth("someone-else", testClientSecret)},
		"form wrong secret":  {url.Values{"grant_type": {"client_credentials"}, "client_id": {testClientID}, "client_secret": {"wrong"}}, ""},
		"no credentials":     {ccForm(), ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := tokenCall(t, h.s, tc.form, tc.authz, formType)
			if rec.Code != http.StatusUnauthorized || decodeOAuthError(t, rec).Error != "invalid_client" {
				t.Fatalf("got %d %s", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="iamsim"` {
				t.Fatalf("WWW-Authenticate = %q", got)
			}
		})
	}
	if !strings.Contains(h.logs.String(), "token client authentication failed") || strings.Contains(h.logs.String(), testClientSecret) {
		t.Fatalf("logs = %s", h.logs)
	}
}

func TestTokenRequestSyntaxErrors(t *testing.T) {
	h := newHarness(t, nil)
	good := basicAuth(testClientID, testClientSecret)
	cases := map[string]struct {
		body        string
		authz, ct   string
		status      int
		wantErrCode string
	}{
		"bad grant":        {"grant_type=password", good, formType, 400, "unsupported_grant_type"},
		"missing grant":    {"scope=access.read", good, formType, 400, "invalid_request"},
		"unknown scope":    {"grant_type=client_credentials&scope=admin", good, formType, 400, "invalid_scope"},
		"partly bad scope": {"grant_type=client_credentials&scope=access.read+admin", good, formType, 400, "invalid_scope"},
		"json body":        {`{"grant_type":"client_credentials"}`, good, "application/json", 400, "invalid_request"},
		"repeated param":   {"grant_type=client_credentials&grant_type=client_credentials", good, formType, 400, "invalid_request"},
		"bearer scheme":    {"grant_type=client_credentials", "Bearer abc", formType, 400, "invalid_request"},
		"basic not base64": {"grant_type=client_credentials", "Basic !!!", formType, 400, "invalid_request"},
		"basic no colon":   {"grant_type=client_credentials", "Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon")), formType, 400, "invalid_request"},
		"bad form escape":  {"grant_type=%zz", good, formType, 400, "invalid_request"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.ct)
			req.Header.Set("Authorization", tc.authz)
			rec := httptest.NewRecorder()
			h.s.ServeHTTP(rec, req)
			if rec.Code != tc.status || decodeOAuthError(t, rec).Error != tc.wantErrCode {
				t.Fatalf("got %d %s", rec.Code, rec.Body)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("error response cacheable: %v", rec.Header())
			}
		})
	}
	if n := len(h.s.tokens); n != 0 {
		t.Fatalf("failed requests stored %d tokens", n)
	}
}

func TestTokenScopeNotAllowedForClient(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.ClientScopes = []string{ScopeRead} })
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {"access.write"}}
	rec := tokenCall(t, h.s, form, basicAuth(testClientID, testClientSecret), formType)
	if rec.Code != http.StatusBadRequest || decodeOAuthError(t, rec).Error != "invalid_scope" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if tr := decodeToken(t, tokenCall(t, h.s, ccForm(), basicAuth(testClientID, testClientSecret), formType)); tr.Scope != "access.read" {
		t.Fatalf("default scope for read-only client = %q", tr.Scope)
	}
}

func TestTokensDifferPerIssue(t *testing.T) {
	h := newHarness(t, nil)
	seen := map[string]bool{}
	for range 5 {
		tok := issueToken(t, h.s, "")
		if seen[tok] {
			t.Fatalf("token %q issued twice", tok)
		}
		seen[tok] = true
	}
	if len(h.s.tokens) != 5 {
		t.Fatalf("stored %d tokens, want 5", len(h.s.tokens))
	}
}

// TestServerNeverStoresRawToken inspects server state and logs: only the
// SHA-256 is kept, and logs carry just a short hash prefix.
func TestServerNeverStoresRawToken(t *testing.T) {
	h := newHarness(t, nil)
	tok := issueToken(t, h.s, "")
	h.s.mu.Lock()
	dump := fmt.Sprintf("%#v", h.s.tokens)
	_, keyed := h.s.tokens[sha256.Sum256([]byte(tok))]
	h.s.mu.Unlock()
	if !keyed {
		t.Fatal("token is not keyed by its SHA-256")
	}
	if strings.Contains(dump, tok) {
		t.Fatalf("raw token found in server state: %s", dump)
	}
	sum := sha256.Sum256([]byte(tok))
	logs := h.logs.String()
	if strings.Contains(logs, tok) || strings.Contains(logs, testClientSecret) {
		t.Fatalf("logs leak a credential: %s", logs)
	}
	if !strings.Contains(logs, "token_id="+hex.EncodeToString(sum[:6])) || strings.Contains(logs, hex.EncodeToString(sum[:])) {
		t.Fatalf("logs should carry only a short hash prefix: %s", logs)
	}
}

func TestTokenEndpointFlakyAndEntropyFailure(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.Scenario.TokenEndpointFlakyRate = 1
		c.Random = func() float64 { return 0.5 }
	})
	rec := tokenCall(t, h.s, ccForm(), basicAuth(testClientID, testClientSecret), formType)
	if rec.Code != http.StatusServiceUnavailable || decodeOAuthError(t, rec).Error != "temporarily_unavailable" {
		t.Fatalf("flaky token = %d %s", rec.Code, rec.Body)
	}

	h2 := newHarness(t, func(c *Config) { c.TokenRand = failingReader{} })
	rec = tokenCall(t, h2.s, ccForm(), basicAuth(testClientID, testClientSecret), formType)
	if rec.Code != http.StatusInternalServerError || decodeOAuthError(t, rec).Error != "server_error" {
		t.Fatalf("entropy failure = %d %s", rec.Code, rec.Body)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

func TestUnconfiguredClientNeverAuthenticates(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.ClientSecret = "" })
	rec := tokenCall(t, h.s, ccForm(), basicAuth(testClientID, ""), formType)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty secret authenticated: %d", rec.Code)
	}
}

func TestBearerMissingOrMalformedIsInvalidRequest(t *testing.T) {
	h := newHarness(t, nil)
	for name, authz := range map[string]string{
		"missing":      "",
		"basic scheme": basicAuth(testClientID, testClientSecret),
		"empty bearer": "Bearer ",
		"bad chars":    "Bearer a b",
	} {
		t.Run(name, func(t *testing.T) {
			// No Idempotency-Key and an empty body: a 400 here would mean the
			// handler parsed before authenticating.
			req := httptest.NewRequest(http.MethodPost, "/v1/access-changes", strings.NewReader(""))
			if authz != "" {
				req.Header.Set("Authorization", authz)
			}
			rec := httptest.NewRecorder()
			h.s.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized || errorCode(t, rec) != "invalid_request" {
				t.Fatalf("got %d %s", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="iamsim"` {
				t.Fatalf("WWW-Authenticate = %q", got)
			}
		})
	}
}

func assertInvalidToken(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized || errorCode(t, rec) != "invalid_token" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="iamsim", error="invalid_token"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
}

func TestBearerGarbageTokenIsInvalidToken(t *testing.T) {
	h := newHarness(t, nil)
	assertInvalidToken(t, postChange(t, h.s, "not-a-real-token", "iam:rev-1", validRequest("http://127.0.0.1:1/cb")))
}

func TestBearerExpiredTokenIsInvalidToken(t *testing.T) {
	h := newHarness(t, nil)
	tok := issueToken(t, h.s, "")
	h.clock.Advance(299 * time.Second)
	if code, _ := getStatus(t, h.s, tok, "iam:x"); code != http.StatusNotFound {
		t.Fatalf("token rejected before expiry: %d", code)
	}
	h.clock.Advance(time.Second)
	assertInvalidToken(t, postChange(t, h.s, tok, "iam:rev-1", validRequest("http://127.0.0.1:1/cb")))
	if !strings.Contains(h.logs.String(), "reason=expired") {
		t.Fatalf("expiry not logged as such: %s", h.logs)
	}
	// Issuing a fresh token prunes the expired record.
	issueToken(t, h.s, "")
	if len(h.s.tokens) != 1 {
		t.Fatalf("expired token not pruned: %d stored", len(h.s.tokens))
	}
}

func TestRevokeAllThenOldTokenFailsAndNewTokenWorks(t *testing.T) {
	h := newHarness(t, nil)
	old := issueToken(t, h.s, "")
	req := httptest.NewRequest(http.MethodPost, "/v1/_control/tokens/revoke", nil)
	rec := httptest.NewRecorder()
	h.s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"revoked":1`) {
		t.Fatalf("revoke = %d %s", rec.Code, rec.Body)
	}
	rec2 := httptest.NewRecorder()
	sreq := httptest.NewRequest(http.MethodGet, "/v1/access-changes/iam:x", nil)
	sreq.Header.Set("Authorization", "Bearer "+old)
	h.s.ServeHTTP(rec2, sreq)
	assertInvalidToken(t, rec2)

	fresh := issueToken(t, h.s, "")
	if code, _ := getStatus(t, h.s, fresh, "iam:x"); code != http.StatusNotFound {
		t.Fatalf("fresh token status = %d, want 404 (authenticated)", code)
	}
}

func TestBearerInsufficientScope(t *testing.T) {
	h := newHarness(t, nil)
	readOnly := issueToken(t, h.s, "access.read")
	rec := postChange(t, h.s, readOnly, "iam:rev-1", validRequest("http://127.0.0.1:1/cb"))
	if rec.Code != http.StatusForbidden || errorCode(t, rec) != "insufficient_scope" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="iamsim", error="insufficient_scope", scope="access.write"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
	if len(h.s.changes) != 0 {
		t.Fatal("forbidden request created a change")
	}
	// Read scope suffices for status; so does write.
	if code, _ := getStatus(t, h.s, readOnly, "iam:x"); code != http.StatusNotFound {
		t.Fatalf("read token on GET = %d", code)
	}
	writeOnly := issueToken(t, h.s, "access.write")
	if code, _ := getStatus(t, h.s, writeOnly, "iam:x"); code != http.StatusNotFound {
		t.Fatalf("write token on GET = %d", code)
	}
}

func TestBearerTokenSyntax(t *testing.T) {
	for token, want := range map[string]bool{"abc-DEF_123.~+/": true, "abc==": true, "a=b": false, "": false, "a b": false} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if _, ok := bearerToken(req); ok != want {
			t.Fatalf("bearerToken(%q) = %v, want %v", token, ok, want)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add("Authorization", "Bearer a")
	req.Header.Add("Authorization", "Bearer b")
	if _, ok := bearerToken(req); ok {
		t.Fatal("two Authorization headers accepted")
	}
}

func TestNormalizeScopes(t *testing.T) {
	got := normalizeScopes([]string{"access.write", "bogus", "access.read", "access.write"})
	if strings.Join(got, " ") != "access.read access.write" {
		t.Fatalf("normalizeScopes = %v", got)
	}
}
