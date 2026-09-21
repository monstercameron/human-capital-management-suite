package payrollsim

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestIntakeValidation(t *testing.T) {
	cb := "http://127.0.0.1:1/integrations/payroll/v1/receipts"
	cases := []struct {
		name   string
		key    string
		mutate func(*PayChangeRequest)
		raw    string
		field  string
		status int
	}{
		{name: "missing key", key: "-", field: "Idempotency-Key", status: 400},
		{name: "key too long", key: strings.Repeat("k", 201), field: "Idempotency-Key", status: 400},
		{name: "malformed json", raw: "{", field: "body", status: 400},
		{name: "missing change_ref", mutate: func(r *PayChangeRequest) { r.ChangeRef = "" }, field: "change_ref", status: 400},
		{name: "missing tenant", mutate: func(r *PayChangeRequest) { r.Tenant = " " }, field: "tenant", status: 400},
		{name: "missing worker_ref", mutate: func(r *PayChangeRequest) { r.WorkerRef = "" }, field: "worker_ref", status: 400},
		{name: "zero amount", mutate: func(r *PayChangeRequest) { r.BasePay.Amount = "0.00" }, field: "base_pay.amount", status: 400},
		{name: "negative amount", mutate: func(r *PayChangeRequest) { r.BasePay.Amount = "-1.00" }, field: "base_pay.amount", status: 400},
		{name: "three fraction digits", mutate: func(r *PayChangeRequest) { r.BasePay.Amount = "1.001" }, field: "base_pay.amount", status: 400},
		{name: "exponent amount", mutate: func(r *PayChangeRequest) { r.BasePay.Amount = "1e5" }, field: "base_pay.amount", status: 400},
		{name: "empty amount", mutate: func(r *PayChangeRequest) { r.BasePay.Amount = "" }, field: "base_pay.amount", status: 400},
		{name: "lowercase currency", mutate: func(r *PayChangeRequest) { r.BasePay.Currency = "usd" }, field: "base_pay.currency", status: 400},
		{name: "long currency", mutate: func(r *PayChangeRequest) { r.BasePay.Currency = "USDX" }, field: "base_pay.currency", status: 400},
		{name: "bad date", mutate: func(r *PayChangeRequest) { r.EffectiveDate = "2026-13-01" }, field: "effective_date", status: 400},
		{name: "unpadded date", mutate: func(r *PayChangeRequest) { r.EffectiveDate = "2026-1-01" }, field: "effective_date", status: 400},
		{name: "missing correlation", mutate: func(r *PayChangeRequest) { r.CorrelationKey = "" }, field: "correlation_key", status: 400},
		{name: "relative callback", mutate: func(r *PayChangeRequest) { r.CallbackURL = "/receipts" }, field: "callback_url", status: 400},
		{name: "ftp callback", mutate: func(r *PayChangeRequest) { r.CallbackURL = "ftp://host/x" }, field: "callback_url", status: 400},
		{name: "key differs from change_ref", key: "payroll:other", field: "change_ref", status: 400},
		{name: "valid one-digit fraction", mutate: func(r *PayChangeRequest) { r.BasePay.Amount = "160000.5" }, status: 202},
		{name: "valid integer amount", mutate: func(r *PayChangeRequest) { r.BasePay.Amount = "160000" }, status: 202},
		{name: "valid https", mutate: func(r *PayChangeRequest) { r.CallbackURL = "https://hcm.example/cb" }, status: 202},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t, func(c *Config) { c.Scenario.ApplyDelayMS = holdDelayMS })
			req := validRequest(cb)
			if tc.mutate != nil {
				tc.mutate(&req)
			}
			key := req.ChangeRef
			switch tc.key {
			case "-":
				key = ""
			case "":
			default:
				key = tc.key
			}
			var body any = req
			if tc.raw != "" {
				body = tc.raw
			}
			if key == "" && tc.key != "-" {
				key = "payroll:rev-1"
			}
			rec := post(t, s, key, body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d %s, want %d", rec.Code, rec.Body, tc.status)
			}
			if tc.status == 400 {
				var got map[string]string
				_ = json.Unmarshal(rec.Body.Bytes(), &got)
				if got["error"] != "invalid" || got["field"] != tc.field {
					t.Fatalf("body = %v, want field %q", got, tc.field)
				}
			}
		})
	}
}

func TestIntakeBodyTooLarge(t *testing.T) {
	s, _ := newTestServer(t, nil)
	big := `{"change_ref":"` + strings.Repeat("x", MaxRequestBytes) + `"}`
	if rec := post(t, s, "k", big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestIntakeReplayAndReuse(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.ApplyDelayMS = holdDelayMS })
	req := validRequest("http://127.0.0.1:1/cb")
	first := post(t, s, req.ChangeRef, req)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	var acc acceptResponse
	if err := json.Unmarshal(first.Body.Bytes(), &acc); err != nil || acc.Status != StatusAccepted || acc.ChangeRef != req.ChangeRef || !strings.HasPrefix(acc.ProviderRef, "PSIM-") {
		t.Fatalf("accept body = %s (%v)", first.Body, err)
	}

	// Same meaning, different bytes: reordered keys, whitespace and an
	// un-padded amount must all replay.
	reordered := `{ "callback_url":"http://127.0.0.1:1/cb", "base_pay":{"currency":"USD","amount":"0160000.0"},
		"effective_date":"2026-12-01","tenant":"harborcare-demo","worker_ref":"worker-42",
		"correlation_key":"corr-abc","change_ref":"payroll:rev-1" }`
	replay := post(t, s, req.ChangeRef, reordered)
	if replay.Code != http.StatusOK || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %s, want 200 %s", replay.Code, replay.Body, first.Body)
	}

	changed := req
	changed.BasePay.Amount = "170000.00"
	reuse := post(t, s, req.ChangeRef, changed)
	if reuse.Code != http.StatusConflict || !strings.Contains(reuse.Body.String(), `"idempotency_key_reused"`) {
		t.Fatalf("reuse = %d %s, want 409", reuse.Code, reuse.Body)
	}
}

func TestIntakeRejectAtIntake(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) {
		c.Scenario.Mode = ModeRejectAtIntake
		c.Scenario.RejectReason = "worker not on payroll"
	})
	req := validRequest("http://127.0.0.1:1/cb")
	rec := post(t, s, req.ChangeRef, req)
	if rec.Code != http.StatusUnprocessableEntity || rec.Body.String() != `{"error":"rejected","reason":"worker not on payroll"}` {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if code, _ := getStatus(t, s, req.ChangeRef); code != http.StatusNotFound {
		t.Fatalf("intake-rejected change was recorded (%d)", code)
	}
}

func TestIntakeFlaky(t *testing.T) {
	rolls := []float64{0.1, 0.9}
	s, _ := newTestServer(t, func(c *Config) {
		c.Scenario.Mode = ModeFlaky
		c.Scenario.FlakyRate = 0.5
		c.Scenario.ApplyDelayMS = holdDelayMS
		c.Random = func() float64 { r := rolls[0]; rolls = rolls[1:]; return r }
	})
	req := validRequest("http://127.0.0.1:1/cb")
	if rec := post(t, s, req.ChangeRef, req); rec.Code != http.StatusServiceUnavailable || rec.Body.String() != `{"error":"unavailable"}` {
		t.Fatalf("first = %d %s, want 503", rec.Code, rec.Body)
	}
	if rec := post(t, s, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("retry = %d %s, want 202", rec.Code, rec.Body)
	}
}

func TestIntakeConcurrentSameKeyOneChangeOneCallback(t *testing.T) {
	rc := newReceiver(t)
	s, _ := newTestServer(t, nil)
	req := validRequest(rc.url())
	codes := make([]int, 20)
	bodies := make([]string, 20)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := post(t, s, req.ChangeRef, req)
			codes[i], bodies[i] = rec.Code, rec.Body.String()
		}()
	}
	wg.Wait()
	drain(t, s)
	accepted := 0
	for i, c := range codes {
		switch c {
		case http.StatusAccepted:
			accepted++
		case http.StatusOK:
		default:
			t.Fatalf("goroutine %d got %d", i, c)
		}
		if bodies[i] != bodies[0] {
			t.Fatalf("bodies differ: %s vs %s", bodies[i], bodies[0])
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted = %d, want exactly 1", accepted)
	}
	if n := len(rc.calls()); n != 1 {
		t.Fatalf("callbacks = %d, want 1", n)
	}
}

func TestNormalizeAmountAndShortID(t *testing.T) {
	for in, want := range map[string]string{"1": "1.00", "001.5": "1.50", "0.01": "0.01", "160000.00": "160000.00"} {
		if got, ok := normalizeAmount(in); !ok || got != want {
			t.Errorf("normalizeAmount(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	if got := shortID("0f8e3c2a-1b2c-4d5e-8f90-123456789abc"); got != "0F8E3C2A1B2C" {
		t.Fatalf("shortID = %q", got)
	}
	if got := shortID("ab"); got != "AB" {
		t.Fatalf("shortID short = %q", got)
	}
}

func TestIntakeRequiresAPIKey(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.ApplyDelayMS = holdDelayMS })
	req := validRequest("http://127.0.0.1:1/cb")
	for name, apiKey := range map[string]string{"missing": "", "wrong": "not-the-key", "prefix": testAPIKey[:5]} {
		rec := postAs(t, s, apiKey, req.ChangeRef, req)
		if rec.Code != http.StatusUnauthorized || rec.Body.String() != `{"error":"invalid_api_key"}` {
			t.Fatalf("%s key = %d %s, want 401", name, rec.Code, rec.Body)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != `ApiKey realm="payrollsim"` {
			t.Fatalf("%s key WWW-Authenticate = %q", name, got)
		}
	}
	// Auth runs before idempotency: the refused requests reserved nothing,
	// so the first authenticated POST is a fresh 202, not a 200 replay.
	if rec := post(t, s, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("authenticated intake = %d %s, want 202", rec.Code, rec.Body)
	}
	// Auth also runs before validation, so garbage is still 401 not 400.
	if rec := postAs(t, s, "", "", "{"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated garbage = %d, want 401", rec.Code)
	}
}

func TestStatusRequiresAPIKey(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) { c.Scenario.ApplyDelayMS = holdDelayMS })
	req := validRequest("http://127.0.0.1:1/cb")
	post(t, s, req.ChangeRef, req)
	for _, apiKey := range []string{"", "wrong"} {
		r := httptest.NewRequest(http.MethodGet, "/v1/pay-changes/"+url.PathEscape(req.ChangeRef), nil)
		if apiKey != "" {
			r.Header.Set(APIKeyHeader, apiKey)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("GET with key %q = %d, want 401", apiKey, rec.Code)
		}
	}
	if code, _ := getStatus(t, s, req.ChangeRef); code != http.StatusOK {
		t.Fatalf("authenticated GET = %d", code)
	}
}
