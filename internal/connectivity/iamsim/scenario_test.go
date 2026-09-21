package iamsim

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func control(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set(ControlTokenHeader, token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDefaultScenario(t *testing.T) {
	sc := DefaultScenario()
	if sc.Mode != ModeGrant || sc.GrantDelayMS != 1500 || sc.TokenTTLSeconds != 300 || sc.FlakyRate != 0 || sc.DuplicateCallbacks {
		t.Fatalf("default = %+v", sc)
	}
	if (Scenario{}).tokenTTL() != defaultTokenTTL || (Scenario{}).rejectReason() != defaultRejectReason {
		t.Fatal("zero scenario fallbacks")
	}
}

func TestScenarioPatchKeepsOmittedFields(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.ControlToken = "ctl"
		c.Random = func() float64 { return 0.9 } // above the 0.25 flaky rate set below
	})
	rec := control(t, h.s, http.MethodPut, "/v1/_control/scenario", "ctl", `{"mode":"reject","token_ttl_seconds":5,"token_endpoint_flaky_rate":0.25}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put = %d %s", rec.Code, rec.Body)
	}
	rec = control(t, h.s, http.MethodGet, "/v1/_control/scenario", "ctl", "")
	var sc Scenario
	if err := json.Unmarshal(rec.Body.Bytes(), &sc); err != nil {
		t.Fatal(err)
	}
	if sc.Mode != ModeReject || sc.TokenTTLSeconds != 5 || sc.TokenEndpointFlakyRate != 0.25 || sc.GrantDelayMS != 0 {
		t.Fatalf("scenario = %+v", sc)
	}
	// New tokens use the new lifetime.
	if tr := decodeToken(t, tokenCall(t, h.s, ccForm(), basicAuth(testClientID, testClientSecret), formType)); tr.ExpiresIn != 5 {
		t.Fatalf("expires_in = %d, want 5", tr.ExpiresIn)
	}
}

func TestScenarioPatchValidation(t *testing.T) {
	h := newHarness(t, nil)
	for body, field := range map[string]string{
		`{"mode":"approve"}`:                  "mode",
		`{"grant_delay_ms":-1}`:               "grant_delay_ms",
		`{"flaky_rate":1.5}`:                  "flaky_rate",
		`{"token_ttl_seconds":0}`:             "token_ttl_seconds",
		`{"token_endpoint_flaky_rate":-0.1}`:  "token_endpoint_flaky_rate",
		`not json`:                            "body",
		`{"mode":"grant","flaky_rate":2}`:     "flaky_rate",
		`{"reject_reason":"x","mode":"nope"}`: "mode",
	} {
		rec := control(t, h.s, http.MethodPut, "/v1/_control/scenario", "", body)
		var got map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if rec.Code != http.StatusBadRequest || got["field"] != field {
			t.Fatalf("%s -> %d %s, want field %s", body, rec.Code, rec.Body, field)
		}
	}
	if h.s.Scenario() != func() Scenario { sc := DefaultScenario(); sc.GrantDelayMS = 0; return sc }() {
		t.Fatalf("invalid patch leaked into scenario: %+v", h.s.Scenario())
	}
	big := `{"reject_reason":"` + strings.Repeat("x", MaxRequestBytes) + `"}`
	if rec := control(t, h.s, http.MethodPut, "/v1/_control/scenario", "", big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized = %d", rec.Code)
	}
	all := `{"mode":"flaky","grant_delay_ms":10,"reject_reason":"r","flaky_rate":0.5,"duplicate_callbacks":true}`
	if rec := control(t, h.s, http.MethodPut, "/v1/_control/scenario", "", all); rec.Code != http.StatusOK {
		t.Fatalf("full patch = %d", rec.Code)
	}
	if sc := h.s.Scenario(); sc.Mode != ModeFlaky || sc.GrantDelayMS != 10 || sc.RejectReason != "r" || sc.FlakyRate != 0.5 || !sc.DuplicateCallbacks {
		t.Fatalf("full patch = %+v", sc)
	}
}

func TestControlTokenEnforced(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.ControlToken = "ctl" })
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/v1/_control/scenario"},
		{http.MethodPut, "/v1/_control/scenario"},
		{http.MethodPost, "/v1/_control/tokens/revoke"},
		{http.MethodPost, "/v1/_control/reset"},
	} {
		for _, tok := range []string{"", "wrong"} {
			if rec := control(t, h.s, tc.method, tc.path, tok, `{}`); rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s token %q = %d", tc.method, tc.path, tok, rec.Code)
			}
		}
	}
	issueToken(t, h.s, "")
	if rec := control(t, h.s, http.MethodPost, "/v1/_control/tokens/revoke", "wrong", ""); rec.Code != http.StatusUnauthorized || len(h.s.tokens) != 1 {
		t.Fatal("unauthorized revoke took effect")
	}
}

func TestResetForgetsChanges(t *testing.T) {
	h := newHarness(t, nil)
	tok := issueToken(t, h.s, "")
	req := validRequest("http://127.0.0.1:1/cb")
	postChange(t, h.s, tok, req.ChangeRef, req)
	drain(t, h.s)
	rec := control(t, h.s, http.MethodPost, "/v1/_control/reset", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"cleared":1`) {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body)
	}
	if code, _ := getStatus(t, h.s, tok, req.ChangeRef); code != http.StatusNotFound {
		t.Fatalf("status after reset = %d (token should survive reset)", code)
	}
}
