package payrollsim

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
)

func statusWithKey(t *testing.T, s *Server, apiKey string) int {
	t.Helper()
	rec := postAs(t, s, apiKey, "", "{")
	// 400 means authentication passed and validation ran; 401 means refused.
	return rec.Code
}

func TestTwoAPIKeysBothValidThenRotateOut(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) {
		c.APIKey = "old-key"
		c.APIKeys = []string{"new-key", ""}
		c.ControlToken = "ctl"
	})
	for _, k := range []string{"old-key", "new-key"} {
		if code := statusWithKey(t, s, k); code != http.StatusBadRequest {
			t.Fatalf("key %q = %d, want accepted (400 from validation)", k, code)
		}
	}
	for _, k := range []string{"", "other", "old-key,new-key"} {
		if code := statusWithKey(t, s, k); code != http.StatusUnauthorized {
			t.Fatalf("key %q = %d, want 401", k, code)
		}
	}

	if rec := control(t, s, http.MethodPut, "/v1/_control/api-keys", "", `{"keys":["x"]}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("rotation without control token = %d", rec.Code)
	}
	rec := control(t, s, http.MethodPut, "/v1/_control/api-keys", "ctl", `{"keys":["new-key"]}`)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"keys":1,"status":"ok"}` {
		t.Fatalf("rotate = %d %s", rec.Code, rec.Body)
	}
	if code := statusWithKey(t, s, "old-key"); code != http.StatusUnauthorized {
		t.Fatalf("rotated-out key = %d, want 401", code)
	}
	if code := statusWithKey(t, s, "new-key"); code != http.StatusBadRequest {
		t.Fatalf("surviving key = %d", code)
	}
	// The reversal and status endpoints share the key set.
	if rec := postReversalRaw(t, s, "old-key", "payroll:x", "reversal:payroll:x", `{"reason":"r"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("reversal with old key = %d", rec.Code)
	}
}

func TestPutAPIKeysValidation(t *testing.T) {
	s, _ := newTestServer(t, nil)
	for body, want := range map[string]int{
		`{"keys":[]}`:        http.StatusBadRequest,
		`{}`:                 http.StatusBadRequest,
		`{"keys":["a",""]}`:  http.StatusBadRequest,
		`{"keys":"a"}`:       http.StatusBadRequest,
		`not json`:           http.StatusBadRequest,
		`{"keys":["a","b"]}`: http.StatusOK,
	} {
		if rec := control(t, s, http.MethodPut, "/v1/_control/api-keys", "", body); rec.Code != want {
			t.Errorf("%s = %d %s, want %d", body, rec.Code, rec.Body, want)
		}
	}
	tooMany := `{"keys":["` + strings.Repeat(`k","`, MaxAPIKeys) + `k"]}`
	if rec := control(t, s, http.MethodPut, "/v1/_control/api-keys", "", tooMany); rec.Code != http.StatusBadRequest {
		t.Fatalf("too many keys = %d", rec.Code)
	}
	big := `{"keys":["` + strings.Repeat("x", MaxRequestBytes) + `"]}`
	if rec := control(t, s, http.MethodPut, "/v1/_control/api-keys", "", big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized = %d", rec.Code)
	}
	// Refused rotations left the last good set ("a","b") in place.
	if statusWithKey(t, s, "b") != http.StatusBadRequest || statusWithKey(t, s, testAPIKey) != http.StatusUnauthorized {
		t.Fatal("key set not as last rotated")
	}
}

func TestMatchAnyAndHashKeys(t *testing.T) {
	keys := hashKeys([]string{"alpha", "", "beta"})
	if len(keys) != 2 {
		t.Fatalf("hashKeys kept %d, want 2 (empty dropped)", len(keys))
	}
	for got, want := range map[string]bool{"alpha": true, "beta": true, "": false, "alph": false, "gamma": false} {
		if matchAny(keys, got) != want {
			t.Errorf("matchAny(%q) = %v", got, !want)
		}
	}
	if matchAny(nil, "") {
		t.Fatal("empty key set matched")
	}
}

func TestWebhookSecretRotationChangesSignature(t *testing.T) {
	rc := newReceiver(t) // registered with testSecret
	s, _ := newTestServer(t, func(c *Config) { c.ControlToken = "ctl" })
	first := validRequest(rc.url())
	post(t, s, first.ChangeRef, first)
	waitStatus(t, s, first.ChangeRef, StatusApplied, 1)

	if rec := control(t, s, http.MethodPut, "/v1/_control/webhook-secret", "ctl", `{"secret":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty secret = %d", rec.Code)
	}
	if rec := control(t, s, http.MethodPut, "/v1/_control/webhook-secret", "", `{"secret":"x"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no control token = %d", rec.Code)
	}
	rec := control(t, s, http.MethodPut, "/v1/_control/webhook-secret", "ctl", `{"secret":"rotated-secret"}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "rotated-secret") {
		t.Fatalf("rotate = %d %s", rec.Code, rec.Body)
	}
	second := validRequest(rc.url())
	second.ChangeRef = "payroll:rev-2"
	post(t, s, second.ChangeRef, second)
	drain(t, s)

	calls := rc.calls()
	if len(calls) != 2 {
		t.Fatalf("callbacks = %d", len(calls))
	}
	if calls[0].err != nil {
		t.Fatalf("pre-rotation callback failed: %v", calls[0].err)
	}
	if calls[1].err == nil {
		t.Fatal("post-rotation callback still verified with the old secret")
	}
	h := calls[1].header
	ts, _ := strconv.ParseInt(h.Get("Webhook-Timestamp"), 10, 64)
	sign := func(secret string) string {
		return webhook.Sign([]byte(secret), webhook.Request{EventID: h.Get("Webhook-Id"), EventType: EventApplied, Schema: Schema, Timestamp: time.Unix(0, ts), Payload: calls[1].body})
	}
	if got := h.Get("Webhook-Signature"); got != sign("rotated-secret") || got == sign(testSecret) {
		t.Fatalf("signature %q is not the rotated secret's", got)
	}
}
