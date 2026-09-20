package providerdelivery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testAPIKey     = "pk_live_do-not-leak-7f3a"
	payrollPayload = `{"kind":"payroll.pay_change","change_ref":"chg-1","tenant":"acme","worker_ref":"w-1","base_pay":{"amount":"160000.00","currency":"USD"},"pay_frequency":"ANNUAL","effective_date":"2026-10-01","correlation_key":"corr-1"}`
	callbackURL    = "https://hcm.example.test/callbacks/payroll?src=sim&v=1"
	payrollGolden  = `{"change_ref":"chg-1","tenant":"acme","worker_ref":"w-1","base_pay":{"amount":"160000.00","currency":"USD"},"effective_date":"2026-10-01","correlation_key":"corr-1","callback_url":"https://hcm.example.test/callbacks/payroll?src=sim&v=1"}`
)

type capture struct {
	count  atomic.Int64
	header atomic.Pointer[http.Header]
	body   atomic.Pointer[string]
	path   atomic.Pointer[string]
}

func (c *capture) record(r *http.Request) {
	c.count.Add(1)
	h := r.Header.Clone()
	c.header.Store(&h)
	b, _ := io.ReadAll(r.Body)
	s := string(b)
	c.body.Store(&s)
	p := r.Method + " " + r.URL.Path
	c.path.Store(&p)
}

func newPayroll(t *testing.T, baseURL string, client Doer, timeout time.Duration) *PayrollClient {
	t.Helper()
	c, err := NewPayrollClient(PayrollConfig{BaseURL: baseURL, APIKey: testAPIKey, Client: client, Timeout: timeout, Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPayrollDeliverRequestShapeGolden(t *testing.T) {
	var cap capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"change_ref":"chg-1","provider_ref":"PSIM-ABC","status":"ACCEPTED"}`)
	}))
	defer srv.Close()
	res, err := newPayroll(t, srv.URL+"/", srv.Client(), 0).Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Result{Outcome: Delivered, Class: ClassDelivered, Status: 202, ProviderRef: "PSIM-ABC"}); res != want {
		t.Fatalf("result %+v", res)
	}
	if *cap.path.Load() != "POST /v1/pay-changes" {
		t.Fatalf("path %s", *cap.path.Load())
	}
	if got := *cap.body.Load(); got != payrollGolden {
		t.Fatalf("body mismatch\n got: %s\nwant: %s", got, payrollGolden)
	}
	h := *cap.header.Load()
	if h.Get(APIKeyHeader) != testAPIKey || h.Get("Idempotency-Key") != "chg-1" || h.Get("Content-Type") != "application/json" || h.Get("Authorization") != "" {
		t.Fatalf("headers %v", h)
	}
}

func TestPayrollNumericAmountKeepsExactText(t *testing.T) {
	body, err := buildPayrollRequest("chg-1", []byte(strings.Replace(payrollPayload, `"160000.00"`, `160000.10`, 1)), callbackURL)
	if err != nil || !strings.Contains(string(body), `"amount":"160000.10"`) {
		t.Fatalf("body=%s err=%v", body, err)
	}
	if _, err := buildPayrollRequest("chg-1", []byte(strings.Replace(payrollPayload, `"160000.00"`, `true`, 1)), callbackURL); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("bool amount err=%v", err)
	}
}

func TestPayrollStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		status     int
		header     string
		body       string
		outcome    Outcome
		class      string
		reason     string
		retryAfter time.Duration
	}{
		{200, "", `{"provider_ref":"PSIM-1"}`, Delivered, ClassDelivered, "", 0},
		{202, "", `{"provider_ref":"PSIM-1"}`, Delivered, ClassDelivered, "", 0},
		{503, "7", `{"error":"unavailable"}`, Retry, ClassTransientStatus, "unavailable", 7 * time.Second},
		{429, fixedNow.Add(2 * time.Minute).Format(http.TimeFormat), `{}`, Retry, ClassTransientStatus, "status_429", 2 * time.Minute},
		{429, "garbage", `{}`, Retry, ClassTransientStatus, "status_429", 0},
		{408, "", ``, Retry, ClassTransientStatus, "status_408", 0},
		{502, "", ``, Retry, ClassTransientStatus, "status_502", 0},
		{400, "", `{"error":"invalid","field":"base_pay.currency"}`, Rejected, ClassRejectedRequest, "invalid:base_pay.currency", 0},
		{413, "", `{"error":"too_large"}`, Rejected, ClassRejectedRequest, "too_large", 0},
		{409, "", `{"error":"idempotency_key_reused"}`, Rejected, ClassRejectedConflict, "idempotency_key_reused", 0},
		{422, "", `{"error":"rejected","reason":"worker_terminated"}`, Rejected, ClassRejectedBusiness, "worker_terminated", 0},
		{401, "", `{"error":"something_else"}`, Rejected, ClassRejectedAuth, "invalid_api_key", 0},
	} {
		t.Run(fmt.Sprint(tc.status, tc.header), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.header != "" {
					w.Header().Set("Retry-After", tc.header)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			res, err := newPayroll(t, srv.URL, srv.Client(), 0).Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL)
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != tc.outcome || res.Class != tc.class || res.Reason != tc.reason || res.Status != tc.status || res.RetryAfter != tc.retryAfter {
				t.Fatalf("got %+v", res)
			}
		})
	}
}

func TestPayrollTransportFailures(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}))
		defer srv.Close()
		defer close(release)
		res, err := newPayroll(t, srv.URL, srv.Client(), 50*time.Millisecond).Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL)
		if err != nil || res.Outcome != Retry || res.Class != ClassTransientTimeout || res.Status != 0 {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	})
	t.Run("network", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		res, err := newPayroll(t, url, nil, time.Second).Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL)
		if err != nil || res.Outcome != Retry || res.Class != ClassTransientNetwork || res.Reason != "network_error" {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	})
	t.Run("redirect not followed", func(t *testing.T) {
		var elsewhere atomic.Int64
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			elsewhere.Add(1)
			w.WriteHeader(http.StatusAccepted)
		}))
		defer other.Close()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL+"/v1/pay-changes", http.StatusPermanentRedirect)
		}))
		defer srv.Close()
		res, err := newPayroll(t, srv.URL, &http.Client{}, 0).Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL)
		if err != nil || res.Outcome != Retry || res.Class != ClassTransientStatus || res.Status != http.StatusPermanentRedirect {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		if elsewhere.Load() != 0 {
			t.Fatal("payload and API key were re-sent to the redirect target")
		}
	})
}

func TestPayrollValidationSendsNothing(t *testing.T) {
	var cap capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cap.record(r) }))
	defer srv.Close()
	c := newPayroll(t, srv.URL, srv.Client(), 0)
	for name, tc := range map[string]struct {
		ref, payload, cb string
		want             error
	}{
		"empty arg":         {"", payrollPayload, callbackURL, ErrInvalidPayload},
		"missing change":    {"chg-1", strings.Replace(payrollPayload, `"change_ref":"chg-1",`, "", 1), callbackURL, ErrInvalidPayload},
		"mismatch":          {"chg-2", payrollPayload, callbackURL, ErrInvalidPayload},
		"missing tenant":    {"chg-1", strings.Replace(payrollPayload, `"acme"`, `""`, 1), callbackURL, ErrInvalidPayload},
		"missing currency":  {"chg-1", strings.Replace(payrollPayload, `"USD"`, `""`, 1), callbackURL, ErrInvalidPayload},
		"not json":          {"chg-1", `[1,2]`, callbackURL, ErrInvalidPayload},
		"relative callback": {"chg-1", payrollPayload, "/callbacks", ErrInvalidCallbackURL},
		"ftp callback":      {"chg-1", payrollPayload, "ftp://hcm.example.test/cb", ErrInvalidCallbackURL},
	} {
		res, err := c.Deliver(context.Background(), tc.ref, []byte(tc.payload), tc.cb)
		if !errors.Is(err, tc.want) || res != (Result{}) {
			t.Fatalf("%s: res=%+v err=%v", name, res, err)
		}
		if strings.Contains(err.Error(), "160000") || strings.Contains(err.Error(), "w-1") {
			t.Fatalf("%s: error leaks payload values: %v", name, err)
		}
	}
	if cap.count.Load() != 0 {
		t.Fatalf("%d requests sent for invalid input", cap.count.Load())
	}
}

func TestPayrollAPIKeyNeverRendered(t *testing.T) {
	cfg := PayrollConfig{BaseURL: "https://payroll.example.test", APIKey: testAPIKey}
	c, err := NewPayrollClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{fmt.Sprintf("%v %+v %#v %s", c, c, c, c), fmt.Sprintf("%v %+v %#v", cfg, cfg, cfg)} {
		if strings.Contains(s, testAPIKey) || !strings.Contains(s, "[REDACTED]") {
			t.Fatalf("api key rendered: %s", s)
		}
	}
	if _, err := NewPayrollClient(PayrollConfig{BaseURL: "https://payroll.example.test"}); err == nil {
		t.Fatal("empty API key accepted")
	}
	if _, err := NewPayrollClient(PayrollConfig{BaseURL: "not a url", APIKey: testAPIKey}); err == nil || strings.Contains(err.Error(), testAPIKey) {
		t.Fatalf("bad base URL err=%v", err)
	}
	var nilClient *PayrollClient
	if nilClient.String() == "" {
		t.Fatal("nil String")
	}
}

func TestPayrollReverseRequestShapeAndStatuses(t *testing.T) {
	for _, tc := range []struct {
		status     int
		header     string
		body       string
		outcome    Outcome
		class      string
		reason     string
		retryAfter time.Duration
	}{
		{202, "", `{"change_ref":"chg/1","status":"REVERSAL_PENDING"}`, Delivered, ClassDelivered, "", 0},
		{200, "", `{"change_ref":"chg/1","status":"REVERSED"}`, Delivered, ClassDelivered, "", 0},
		{409, "", `{"error":"not_reversible","status":"REJECTED"}`, Delivered, ClassSettledNotReversible, "not_reversible", 0},
		{409, "", `{"error":"idempotency_key_reused"}`, Rejected, ClassRejectedConflict, "idempotency_key_reused", 0},
		{404, "", `{"error":"not_found"}`, Rejected, ClassRejectedRequest, "not_found", 0},
		{422, "", `{"error":"rejected","reason":"period_closed"}`, Rejected, ClassRejectedBusiness, "period_closed", 0},
		{503, "9", `{"error":"unavailable"}`, Retry, ClassTransientStatus, "unavailable", 9 * time.Second},
		{401, "", `{"error":"unauthorized"}`, Rejected, ClassRejectedAuth, "invalid_api_key", 0},
		{307, "", ``, Retry, ClassTransientStatus, "redirect_not_followed", 0},
	} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			var cap capture
			var rawPath atomic.Pointer[string]
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cap.record(r)
				p := r.URL.EscapedPath()
				rawPath.Store(&p)
				if tc.header != "" {
					w.Header().Set("Retry-After", tc.header)
				}
				if tc.status == 307 {
					w.Header().Set("Location", "/elsewhere")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			res, err := newPayroll(t, srv.URL, srv.Client(), 0).Reverse(context.Background(), "chg/1", "entered in error")
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != tc.outcome || res.Class != tc.class || res.Reason != tc.reason || res.Status != tc.status || res.RetryAfter != tc.retryAfter {
				t.Fatalf("got %+v", res)
			}
			if cap.count.Load() != 1 {
				t.Fatalf("requests = %d, want exactly one", cap.count.Load())
			}
			if got := *rawPath.Load(); got != "/v1/pay-changes/chg%2F1/reversal" {
				t.Fatalf("path %s", got)
			}
			h := *cap.header.Load()
			if h.Get(APIKeyHeader) != testAPIKey || h.Get("Idempotency-Key") != "reversal:chg/1" || h.Get("Content-Type") != "application/json" || h.Get("Authorization") != "" {
				t.Fatalf("headers %v", h)
			}
			if got := *cap.body.Load(); got != `{"reason":"entered in error"}` || !strings.HasPrefix(*cap.path.Load(), "POST ") {
				t.Fatalf("request %s %s", *cap.path.Load(), got)
			}
			if strings.Contains(fmt.Sprintf("%+v", res), testAPIKey) {
				t.Fatal("api key leaked into Result")
			}
		})
	}
}

func TestPayrollStatusMappingAndRequestShape(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		known   bool
		state   string
		ref     string
		reason  string
		outcome Outcome
		class   string
		rReason string
	}{
		{"applied", 200, `{"change_ref":"chg-1","provider_ref":"PSIM-1","status":"APPLIED","applied_at":"2026-09-19T12:00:00Z"}`, true, "APPLIED", "PSIM-1", "", Delivered, ClassDelivered, ""},
		{"rejected", 200, `{"change_ref":"chg-1","provider_ref":"PSIM-1","status":"REJECTED","reason":"worker_terminated"}`, true, "REJECTED", "PSIM-1", "worker_terminated", Delivered, ClassDelivered, ""},
		{"reversed", 200, `{"change_ref":"chg-1","provider_ref":"PSIM-1","status":"REVERSED","reason":"entered in error"}`, true, "REVERSED", "PSIM-1", "entered in error", Delivered, ClassDelivered, ""},
		{"accepted", 200, `{"change_ref":"chg-1","status":"ACCEPTED"}`, true, "ACCEPTED", "", "", Delivered, ClassDelivered, ""},
		{"iam state on payroll", 200, `{"change_ref":"chg-1","status":"GRANTED"}`, false, "", "", "", Retry, ClassTransientStatus, "unrecognised_status"},
		{"other change", 200, `{"change_ref":"chg-2","status":"APPLIED"}`, false, "", "", "", Retry, ClassTransientStatus, "status_change_ref_mismatch"},
		{"not json", 200, `<html>`, false, "", "", "", Retry, ClassTransientStatus, "malformed_status_body"},
		{"unknown", 404, `{"error":"not_found"}`, false, "", "", "", Delivered, ClassDelivered, "not_found"},
		{"unknown empty body", 404, ``, false, "", "", "", Delivered, ClassDelivered, "not_found"},
		{"accepted status code", 202, `{"status":"APPLIED"}`, false, "", "", "", Retry, ClassTransientStatus, "unexpected_status"},
		{"unavailable", 503, `{"error":"unavailable"}`, false, "", "", "", Retry, ClassTransientStatus, "unavailable"},
		{"bad key", 401, `{}`, false, "", "", "", Rejected, ClassRejectedAuth, "invalid_api_key"},
		{"bad request", 400, `{"error":"invalid","field":"change_ref"}`, false, "", "", "", Rejected, ClassRejectedRequest, "invalid:change_ref"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cap capture
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cap.record(r)
				w.Header().Set("Retry-After", "4")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			got, err := newPayroll(t, srv.URL, srv.Client(), 0).Status(context.Background(), "chg-1")
			if err != nil {
				t.Fatal(err)
			}
			if got.Known != tc.known || got.Status != tc.state || got.ProviderRef != tc.ref || got.Reason != tc.reason ||
				got.Result.Outcome != tc.outcome || got.Result.Class != tc.class || got.Result.Reason != tc.rReason || got.Result.Status != tc.status {
				t.Fatalf("got %+v", got)
			}
			if tc.status == 503 && got.Result.RetryAfter != 4*time.Second {
				t.Fatalf("retry after %v", got.Result.RetryAfter)
			}
			h := *cap.header.Load()
			if *cap.path.Load() != "GET /v1/pay-changes/chg-1" || h.Get(APIKeyHeader) != testAPIKey || h.Get("Idempotency-Key") != "" || *cap.body.Load() != "" {
				t.Fatalf("request %s %v body=%q", *cap.path.Load(), h, *cap.body.Load())
			}
		})
	}
}

func TestPayrollUndoTransportAndValidation(t *testing.T) {
	var cap capture
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { cap.record(r) }))
	c := newPayroll(t, srv.URL, srv.Client(), 0)
	for name, tc := range map[string]struct{ ref, reason string }{
		"empty ref": {"", "x"}, "blank ref": {"  ", "x"}, "dot": {".", "x"}, "dotdot": {"..", "x"}, "no reason": {"chg-1", " "},
	} {
		if res, err := c.Reverse(context.Background(), tc.ref, tc.reason); !errors.Is(err, ErrInvalidPayload) || res != (Result{}) {
			t.Fatalf("Reverse %s: res=%+v err=%v", name, res, err)
		}
		if tc.reason == "x" {
			if res, err := c.Status(context.Background(), tc.ref); !errors.Is(err, ErrInvalidPayload) || res != (StatusResult{}) {
				t.Fatalf("Status %s: res=%+v err=%v", name, res, err)
			}
		}
	}
	if cap.count.Load() != 0 {
		t.Fatalf("%d requests sent for invalid input", cap.count.Load())
	}
	srv.Close()
	if res, err := c.Reverse(context.Background(), "chg-1", "why"); err != nil || res.Outcome != Retry || res.Class != ClassTransientNetwork {
		t.Fatalf("reverse network: %+v %v", res, err)
	}
	if res, err := c.Status(context.Background(), "chg-1"); err != nil || res.Known || res.Result.Outcome != Retry || res.Result.Class != ClassTransientNetwork {
		t.Fatalf("status network: %+v %v", res, err)
	}
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer slow.Close()
	defer close(release)
	sc := newPayroll(t, slow.URL, slow.Client(), 50*time.Millisecond)
	if res, _ := sc.Reverse(context.Background(), "chg-1", "why"); res.Class != ClassTransientTimeout {
		t.Fatalf("reverse timeout: %+v", res)
	}
	if res, _ := sc.Status(context.Background(), "chg-1"); res.Result.Class != ClassTransientTimeout {
		t.Fatalf("status timeout: %+v", res)
	}
}
