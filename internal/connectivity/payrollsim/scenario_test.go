package payrollsim

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestControlTokenEnforced(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) { c.ControlToken = "sekrit" })
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/_control/scenario", ""},
		{http.MethodPut, "/v1/_control/scenario", `{"mode":"reject"}`},
		{http.MethodPost, "/v1/_control/reset", ""},
	} {
		if rec := control(t, s, tc.method, tc.path, "", tc.body); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token = %d, want 401", tc.method, tc.path, rec.Code)
		}
		if rec := control(t, s, tc.method, tc.path, "wrong", tc.body); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s wrong token = %d, want 401", tc.method, tc.path, rec.Code)
		}
		if rec := control(t, s, tc.method, tc.path, "sekrit", tc.body); rec.Code != http.StatusOK {
			t.Errorf("%s %s with token = %d, want 200", tc.method, tc.path, rec.Code)
		}
	}
	if s.Scenario().Mode != ModeReject {
		t.Fatal("authorised PUT did not take effect")
	}
}

func TestControlOpenWhenNoToken(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if rec := control(t, s, http.MethodGet, "/v1/_control/scenario", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("open control = %d", rec.Code)
	}
}

func TestScenarioPutMergesAndValidates(t *testing.T) {
	s := New(Config{Secret: []byte("x")})
	rec := control(t, s, http.MethodPut, "/v1/_control/scenario", "", `{"mode":"flaky","flaky_rate":0.25,"duplicate_callbacks":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put = %d %s", rec.Code, rec.Body)
	}
	want := Scenario{Mode: ModeFlaky, ApplyDelayMS: 1500, FlakyRate: 0.25, DuplicateCallbacks: true}
	var got Scenario
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got != want {
		t.Fatalf("put body = %s, want %+v", rec.Body, want)
	}
	rec = control(t, s, http.MethodGet, "/v1/_control/scenario", "", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got != want {
		t.Fatalf("get = %s, want %+v", rec.Body, want)
	}
	for body, field := range map[string]string{
		`{"mode":"explode"}`:      "mode",
		`{"apply_delay_ms":-1}`:   "apply_delay_ms",
		`{"flaky_rate":1.5}`:      "flaky_rate",
		`{"flaky_rate":-0.1}`:     "flaky_rate",
		`not json`:                "body",
		`{"mode":"reject","x":1}`: "",
	} {
		rec := control(t, s, http.MethodPut, "/v1/_control/scenario", "", body)
		if field == "" {
			if rec.Code != http.StatusOK {
				t.Errorf("%s = %d, want 200", body, rec.Code)
			}
			continue
		}
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"`+field+`"`) {
			t.Errorf("%s = %d %s, want 400 field %s", body, rec.Code, rec.Body, field)
		}
	}
	if got := s.Scenario(); got.Mode != ModeReject || got.FlakyRate != 0.25 {
		t.Fatalf("invalid PUTs leaked into scenario: %+v", got)
	}
}

func TestResetClearsChanges(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.ApplyDelayMS = holdDelayMS })
	req := validRequest("http://127.0.0.1:1/cb")
	if rec := post(t, s, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d", rec.Code)
	}
	rec := control(t, s, http.MethodPost, "/v1/_control/reset", "", "")
	if rec.Code != http.StatusOK || rec.Body.String() != `{"cleared":1,"status":"ok"}` {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body)
	}
	if code, _ := getStatus(t, s, req.ChangeRef); code != http.StatusNotFound {
		t.Fatalf("status after reset = %d, want 404", code)
	}
	// The key is free again: a changed body is a new change, not a 409.
	req.BasePay.Amount = "1.00"
	if rec := post(t, s, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("intake after reset = %d, want 202", rec.Code)
	}
}

func TestResetCancelsPendingProcessing(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.ApplyDelayMS = holdDelayMS })
	req := validRequest("http://127.0.0.1:1/cb")
	post(t, s, req.ChangeRef, req)
	control(t, s, http.MethodPost, "/v1/_control/reset", "", "")
	// The parked change blocks until cancelled; drain only succeeds if the
	// reset cancelled it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("reset left processing running: %v", err)
	}
}
