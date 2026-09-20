package providerdelivery

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

func TestParseRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"0", 0},
		{"120", 120 * time.Second},
		{" 7 ", 7 * time.Second},
		{"3600", time.Hour},
		{"3601", time.Hour},
		{"99999999999999999999999", time.Hour},
		{"-5", 0},
		{"1.5", 0},
		{"soon", 0},
		{fixedNow.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		{fixedNow.Add(-time.Minute).Format(http.TimeFormat), 0},
		{fixedNow.Add(5 * time.Hour).Format(http.TimeFormat), time.Hour},
		{fixedNow.Add(30 * time.Second).Format(time.RFC850), 30 * time.Second},
	} {
		if got := parseRetryAfter(tc.in, fixedNow); got != tc.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestClassifyStatuses(t *testing.T) {
	for _, tc := range []struct {
		status  int
		body    string
		outcome Outcome
		class   string
		reason  string
		ref     string
	}{
		{200, `{"provider_ref":"P-1"}`, Delivered, ClassDelivered, "", "P-1"},
		{202, `{"provider_ref":"P-2"}`, Delivered, ClassDelivered, "", "P-2"},
		{202, `not json`, Delivered, ClassDelivered, "", ""},
		{204, ``, Retry, ClassTransientStatus, "unexpected_status", ""},
		{302, ``, Retry, ClassTransientStatus, "redirect_not_followed", ""},
		{408, ``, Retry, ClassTransientStatus, "status_408", ""},
		{429, `{"error":"slow_down"}`, Retry, ClassTransientStatus, "slow_down", ""},
		{500, ``, Retry, ClassTransientStatus, "status_500", ""},
		{503, `{"error":"unavailable"}`, Retry, ClassTransientStatus, "unavailable", ""},
		{400, `{"error":"invalid","field":"effective_date"}`, Rejected, ClassRejectedRequest, "invalid:effective_date", ""},
		{413, `{"error":"too_large"}`, Rejected, ClassRejectedRequest, "too_large", ""},
		{404, ``, Rejected, ClassRejectedRequest, "status_404", ""},
		{409, `{"error":"idempotency_key_reused"}`, Rejected, ClassRejectedConflict, "idempotency_key_reused", ""},
		{422, `{"error":"rejected","reason":"worker_not_found"}`, Rejected, ClassRejectedBusiness, "worker_not_found", ""},
		{403, `{"error":"insufficient_scope"}`, Rejected, ClassRejectedAuth, "insufficient_scope", ""},
	} {
		got := classify(response{status: tc.status, header: http.Header{}, body: []byte(tc.body)}, fixedNow)
		want := Result{Outcome: tc.outcome, Class: tc.class, Status: tc.status, Reason: tc.reason, ProviderRef: tc.ref}
		if got != want {
			t.Errorf("status %d: got %+v, want %+v", tc.status, got, want)
		}
	}
}

func TestClassifyRetryAfterOnlyOn429And503(t *testing.T) {
	h := http.Header{"Retry-After": {"30"}}
	for status, want := range map[int]time.Duration{429: 30 * time.Second, 503: 30 * time.Second, 500: 0, 502: 0} {
		if got := classify(response{status: status, header: h}, fixedNow).RetryAfter; got != want {
			t.Errorf("status %d RetryAfter=%v want %v", status, got, want)
		}
	}
}

func TestSanitizeAndOutcomeString(t *testing.T) {
	if got := sanitize("ok\x00\n\x1b[31mred " + strings.Repeat("x", 500)); strings.ContainsAny(got, "\x00\n\x1b") || len(got) > maxReasonLen {
		t.Fatalf("sanitize = %q", got)
	}
	names := map[Outcome]string{Delivered: "delivered", Retry: "retry", Rejected: "rejected", Outcome(0): "unknown"}
	for o, want := range names {
		if o.String() != want {
			t.Fatalf("%d.String() = %q", o, o.String())
		}
	}
}

func TestValidateBaseURL(t *testing.T) {
	if got, err := validateBaseURL("https://payroll.example.test/api/"); err != nil || got != "https://payroll.example.test/api" {
		t.Fatalf("got %q err=%v", got, err)
	}
	for _, bad := range []string{"", "payroll.example.test", "ftp://x", "https://u:p@x", "https://x?a=1", "https://x#f"} {
		if _, err := validateBaseURL(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestNewSenderNeverFollowsRedirects(t *testing.T) {
	orig := &http.Client{}
	s := newSender(orig, 0, nil)
	hc, ok := s.client.(*http.Client)
	if !ok || hc == orig || hc.CheckRedirect == nil || orig.CheckRedirect != nil {
		t.Fatal("caller client must be copied with redirects disabled, not mutated")
	}
	if hc.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirect policy does not stop")
	}
	if s.timeout != defaultTimeout || s.now == nil {
		t.Fatal("defaults not applied")
	}
	if d, ok := newSender(nil, 0, nil).client.(*http.Client); !ok || d.CheckRedirect == nil {
		t.Fatal("default client follows redirects")
	}
}

func TestClassifyReverse(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   Result
	}{
		{409, `{"error":"not_reversible","status":"REJECTED"}`, Result{Outcome: Delivered, Class: ClassSettledNotReversible, Status: 409, Reason: "not_reversible"}},
		// Only the exact provider code settles; lookalikes stay conflicts.
		{409, `{"error":"not_reversible_yet"}`, Result{Outcome: Rejected, Class: ClassRejectedConflict, Status: 409, Reason: "not_reversible_yet"}},
		{409, `not json`, Result{Outcome: Rejected, Class: ClassRejectedConflict, Status: 409, Reason: "conflict"}},
		{422, `{"error":"not_reversible"}`, Result{Outcome: Rejected, Class: ClassRejectedBusiness, Status: 422, Reason: "not_reversible"}},
		{202, `{"status":"REVERSAL_PENDING"}`, Result{Outcome: Delivered, Class: ClassDelivered, Status: 202}},
	} {
		if got := classifyReverse(response{status: tc.status, header: http.Header{}, body: []byte(tc.body)}, fixedNow); got != tc.want {
			t.Errorf("%d %s: got %+v want %+v", tc.status, tc.body, got, tc.want)
		}
	}
}

func TestClassifyStatusSanitisesAndScopesStates(t *testing.T) {
	body := `{"change_ref":"c","provider_ref":"P\u0000-1","status":"REVOKED","reason":"a\nb"}`
	got := classifyStatus(response{status: 200, header: http.Header{}, body: []byte(body)}, "c", accessStatuses, fixedNow)
	if !got.Known || got.Status != "REVOKED" || got.ProviderRef != "P-1" || got.Reason != "ab" || got.Result.ProviderRef != "P-1" {
		t.Fatalf("got %+v", got)
	}
	if got := classifyStatus(response{status: 200, header: http.Header{}, body: []byte(body)}, "c", payrollStatuses, fixedNow); got.Known {
		t.Fatalf("REVOKED accepted for payroll: %+v", got)
	}
	// A body without change_ref is accepted (the URL already names it).
	if got := classifyStatus(response{status: 200, header: http.Header{}, body: []byte(`{"status":"APPLIED"}`)}, "c", payrollStatuses, fixedNow); !got.Known {
		t.Fatalf("got %+v", got)
	}
}

func TestUndoHelpers(t *testing.T) {
	if got := changeURL("https://p.test/v1/pay-changes", "a b/c?d#e"); got != "https://p.test/v1/pay-changes/a%20b%2Fc%3Fd%23e" {
		t.Fatalf("changeURL = %s", got)
	}
	b, err := buildReversalRequest("c", `<"x">&`)
	if err != nil || string(b) != `{"reason":"<\"x\">&"}` {
		t.Fatalf("body=%s err=%v", b, err)
	}
	for _, ref := range []string{"", " ", ".", ".."} {
		if validateChangeRef(ref) == nil {
			t.Fatalf("accepted %q", ref)
		}
	}
	if validateChangeRef("...") != nil || validateChangeRef("payroll:rev-1") != nil {
		t.Fatal("refused a valid ref")
	}
}
