package iamsim

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
)

func tokenWith(t *testing.T, h *harness, secret string) int {
	t.Helper()
	return tokenCall(t, h.s, ccForm(), basicAuth(testClientID, secret), formType).Code
}

func TestClientSecretRotationKeepsIssuedTokens(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.ClientSecrets = []string{"second-secret", ""}
		c.ControlToken = "ctl"
	})
	for _, sec := range []string{testClientSecret, "second-secret"} {
		if code := tokenWith(t, h, sec); code != http.StatusOK {
			t.Fatalf("secret %q = %d, want 200", sec, code)
		}
	}
	if code := tokenWith(t, h, "neither"); code != http.StatusUnauthorized {
		t.Fatalf("wrong secret = %d", code)
	}
	oldTok := issueToken(t, h.s, "") // minted with testClientSecret

	if rec := control(t, h.s, http.MethodPut, "/v1/_control/client-secrets", "", `{"secrets":["x"]}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("rotation without control token = %d", rec.Code)
	}
	rec := control(t, h.s, http.MethodPut, "/v1/_control/client-secrets", "ctl", `{"secrets":["rotated-secret"]}`)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"secrets":1,"status":"ok"}` {
		t.Fatalf("rotate = %d %s", rec.Code, rec.Body)
	}
	for _, sec := range []string{testClientSecret, "second-secret"} {
		if code := tokenWith(t, h, sec); code != http.StatusUnauthorized {
			t.Fatalf("rotated-out secret %q = %d, want 401", sec, code)
		}
	}
	if code := tokenWith(t, h, "rotated-secret"); code != http.StatusOK {
		t.Fatalf("new secret = %d", code)
	}
	// The token issued under the old secret is valid until it expires.
	if code, _ := getStatus(t, h.s, oldTok, "iam:x"); code != http.StatusNotFound {
		t.Fatalf("pre-rotation token = %d, want 404 (authenticated)", code)
	}
	h.clock.Advance(300 * time.Second)
	rec2 := httptestGet(t, h, oldTok)
	assertInvalidToken(t, rec2)
	if strings.Contains(h.logs.String(), "rotated-secret") {
		t.Fatal("rotated secret logged")
	}
}

func httptestGet(t *testing.T, h *harness, tok string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/access-changes/iam:x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.s.ServeHTTP(rec, req)
	return rec
}

func TestPutClientSecretsValidation(t *testing.T) {
	h := newHarness(t, nil)
	for body, want := range map[string]int{
		`{"secrets":[]}`:      http.StatusBadRequest,
		`{}`:                  http.StatusBadRequest,
		`{"secrets":["",""]}`: http.StatusBadRequest,
		`nope`:                http.StatusBadRequest,
		`{"secrets":["a"]}`:   http.StatusOK,
	} {
		if rec := control(t, h.s, http.MethodPut, "/v1/_control/client-secrets", "", body); rec.Code != want {
			t.Errorf("%s = %d %s, want %d", body, rec.Code, rec.Body, want)
		}
	}
	tooMany := `{"secrets":["` + strings.Repeat(`s","`, MaxClientSecrets) + `s"]}`
	if rec := control(t, h.s, http.MethodPut, "/v1/_control/client-secrets", "", tooMany); rec.Code != http.StatusBadRequest {
		t.Fatalf("too many = %d", rec.Code)
	}
	if tokenWith(t, h, "a") != http.StatusOK || tokenWith(t, h, testClientSecret) != http.StatusUnauthorized {
		t.Fatal("secret set not as last rotated")
	}
	if got := hashSecrets([]string{"", "a", ""}); len(got) != 1 {
		t.Fatalf("hashSecrets kept %d", len(got))
	}
}

func TestWebhookSecretRotationChangesSignature(t *testing.T) {
	rc := newReceiver(t) // registered with testWebhookSecret
	h := newHarness(t, func(c *Config) { c.ControlToken = "ctl" })
	tok := issueToken(t, h.s, "")
	first := validRequest(rc.url())
	postChange(t, h.s, tok, first.ChangeRef, first)
	waitStatus(t, h, tok, first.ChangeRef, StatusGranted, 1)

	if rec := control(t, h.s, http.MethodPut, "/v1/_control/webhook-secret", "ctl", `{"secret":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty secret = %d", rec.Code)
	}
	if rec := control(t, h.s, http.MethodPut, "/v1/_control/webhook-secret", "ctl", `{"secret":"rotated-wh"}`); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "rotated-wh") {
		t.Fatalf("rotate = %d %s", rec.Code, rec.Body)
	}
	second := validRequest(rc.url())
	second.ChangeRef = "iam:rev-2"
	postChange(t, h.s, tok, second.ChangeRef, second)
	drain(t, h.s)

	calls := rc.calls()
	if len(calls) != 2 || calls[0].err != nil || calls[1].err == nil {
		t.Fatalf("calls = %d, pre-rotation err %v, post-rotation err %v", len(calls), calls[0].err, calls[len(calls)-1].err)
	}
	hd := calls[1].header
	ts, _ := strconv.ParseInt(hd.Get("Webhook-Timestamp"), 10, 64)
	sign := func(secret string) string {
		return webhook.Sign([]byte(secret), webhook.Request{EventID: hd.Get("Webhook-Id"), EventType: EventGranted, Schema: Schema, Timestamp: time.Unix(0, ts), Payload: calls[1].body})
	}
	if got := hd.Get("Webhook-Signature"); got != sign("rotated-wh") || got == sign(testWebhookSecret) {
		t.Fatalf("signature %q is not the rotated secret's", got)
	}
}
