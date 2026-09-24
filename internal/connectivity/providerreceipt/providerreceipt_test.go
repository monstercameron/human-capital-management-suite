package providerreceipt_test

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

var (
	testSecret = []byte("unit-secret-0123456789abcdef-current")
	oldSecret  = []byte("unit-secret-0123456789abcdef-previous")
	wall       = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
)

const payrollBody = `{"event_id":"evt-1","change_ref":"payroll:rev-1","correlation_key":"corr-1","provider_ref":"PSIM-1","outcome":"APPLIED","reason":"","effective_date":"2026-12-01","base_pay":{"amount":"1.00","currency":"USD"},"occurred_at":"2026-09-19T12:00:00Z"}`

func payrollVerifier(t *testing.T, mutate func(*providerreceipt.Endpoint)) *providerreceipt.Verifier {
	t.Helper()
	ep := providerreceipt.PayrollEndpoint("ep-1", "tenant-a", testSecret)
	if mutate != nil {
		mutate(&ep)
	}
	v, err := providerreceipt.NewVerifier(ep)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// signed builds a callback header set signed with secret.
func signed(secret []byte, eventID, eventType, schema, tenant string, at time.Time, body string) http.Header {
	sig := webhook.Sign(secret, webhook.Request{EventID: eventID, EventType: eventType, Schema: schema, Timestamp: time.Unix(0, at.UnixNano()), Payload: []byte(body)})
	h := http.Header{}
	h.Set("Webhook-Id", eventID)
	h.Set("Webhook-Event", eventType)
	h.Set("Webhook-Schema", schema)
	h.Set("Webhook-Timestamp", strconv.FormatInt(at.UnixNano(), 10))
	h.Set("Webhook-Tenant", tenant)
	h.Set("Webhook-Signature", sig)
	return h
}

func okHeader(body string) http.Header {
	return signed(testSecret, "evt-1", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-a", wall, body)
}

func TestParseValid(t *testing.T) {
	v := payrollVerifier(t, nil)
	p, err := v.Parse(okHeader(payrollBody), []byte(payrollBody), wall.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if p.Outcome != providerreceipt.OutcomeApplied || p.EventID != "evt-1" || p.ChangeRef != "payroll:rev-1" || p.ProviderRef != "PSIM-1" ||
		!strings.HasPrefix(p.PayloadDigest, "sha256:") || string(p.Payload) != payrollBody || p.Details["amount"] != "1.00" {
		t.Fatalf("parsed = %+v", p)
	}
}

func TestParseRefusals(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
		body   string
		now    time.Time
		want   error
	}{
		{"bad signature", signed([]byte("wrong"), "evt-1", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-a", wall, payrollBody), payrollBody, wall, providerreceipt.ErrBadSignature},
		{"tampered body", okHeader(payrollBody), strings.Replace(payrollBody, "1.00", "9.00", 1), wall, providerreceipt.ErrBadSignature},
		{"stale timestamp", okHeader(payrollBody), payrollBody, wall.Add(time.Hour), providerreceipt.ErrOutsideWindow},
		{"wrong tenant", signed(testSecret, "evt-1", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-b", wall, payrollBody), payrollBody, wall, providerreceipt.ErrWrongTenant},
		{"unknown schema", signed(testSecret, "evt-1", providerreceipt.PayrollEventApplied, "other/v1", "tenant-a", wall, payrollBody), payrollBody, wall, providerreceipt.ErrUnknownSchema},
		{"unknown event", signed(testSecret, "evt-1", "payroll.change.exploded", providerreceipt.PayrollSchema, "tenant-a", wall, payrollBody), payrollBody, wall, providerreceipt.ErrUnknownEvent},
		{"event/outcome mismatch", signed(testSecret, "evt-1", providerreceipt.PayrollEventRejected, providerreceipt.PayrollSchema, "tenant-a", wall, payrollBody), payrollBody, wall, providerreceipt.ErrOutcomeMismatch},
		{"iam outcome on payroll", okHeader(strings.Replace(payrollBody, "APPLIED", "GRANTED", 1)), strings.Replace(payrollBody, "APPLIED", "GRANTED", 1), wall, providerreceipt.ErrMalformed},
		{"event id mismatch", okHeader(strings.Replace(payrollBody, `"evt-1"`, `"evt-2"`, 1)), strings.Replace(payrollBody, `"evt-1"`, `"evt-2"`, 1), wall, providerreceipt.ErrMalformed},
		{"missing change_ref", okHeader(strings.Replace(payrollBody, "payroll:rev-1", "", 1)), strings.Replace(payrollBody, "payroll:rev-1", "", 1), wall, providerreceipt.ErrMalformed},
		{"missing correlation", okHeader(strings.Replace(payrollBody, "corr-1", "", 1)), strings.Replace(payrollBody, "corr-1", "", 1), wall, providerreceipt.ErrMalformed},
		{"not json", okHeader("not json"), "not json", wall, providerreceipt.ErrMalformed},
		{"too large", okHeader(payrollBody), payrollBody + strings.Repeat(" ", 64<<10), wall, providerreceipt.ErrTooLarge},
	}
	missing := okHeader(payrollBody)
	missing.Del("Webhook-Signature")
	badTS := okHeader(payrollBody)
	badTS.Set("Webhook-Timestamp", "yesterday")
	cases = append(cases,
		struct {
			name   string
			header http.Header
			body   string
			now    time.Time
			want   error
		}{"missing header", missing, payrollBody, wall, providerreceipt.ErrMalformed},
		struct {
			name   string
			header http.Header
			body   string
			now    time.Time
			want   error
		}{"bad timestamp", badTS, payrollBody, wall, providerreceipt.ErrMalformed})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := payrollVerifier(t, nil)
			if _, err := v.Parse(c.header, []byte(c.body), c.now); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestParseDuplicates(t *testing.T) {
	v := payrollVerifier(t, nil)
	if _, err := v.Parse(okHeader(payrollBody), []byte(payrollBody), wall); err != nil {
		t.Fatal(err)
	}
	// A fresh-timestamp redelivery of identical bytes is accepted again.
	again := signed(testSecret, "evt-1", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-a", wall.Add(time.Second), payrollBody)
	if _, err := v.Parse(again, []byte(payrollBody), wall.Add(time.Second)); err != nil {
		t.Fatalf("identical redelivery: %v", err)
	}
	other := strings.Replace(payrollBody, "PSIM-1", "PSIM-2", 1)
	if _, err := v.Parse(okHeader(other), []byte(other), wall); !errors.Is(err, providerreceipt.ErrDuplicateDifferent) {
		t.Fatalf("different bytes same id: err = %v", err)
	}
}

func TestVerifierWithRegistry(t *testing.T) {
	ep := providerreceipt.PayrollEndpoint("ep-1", "tenant-a", testSecret)
	if _, err := providerreceipt.NewVerifierWithRegistry(ep, nil); !errors.Is(err, providerreceipt.ErrInvalidEndpoint) {
		t.Fatalf("nil lifecycle error = %v, want ErrInvalidEndpoint", err)
	}
	lifecycle := idempotency.NewRegistry()
	first, err := providerreceipt.NewVerifierWithRegistry(ep, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Parse(okHeader(payrollBody), []byte(payrollBody), wall); err != nil {
		t.Fatalf("first callback = %v", err)
	}

	// A newly composed verifier receives only the shared lifecycle. It must
	// still reject a changed body under the previously reserved event identity.
	restarted, err := providerreceipt.NewVerifierWithRegistry(ep, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(payrollBody, "PSIM-1", "PSIM-2", 1)
	changedHeader := signed(testSecret, "evt-1", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-a", wall.Add(time.Second), changed)
	if _, err := restarted.Parse(changedHeader, []byte(changed), wall.Add(time.Second)); !errors.Is(err, providerreceipt.ErrDuplicateDifferent) {
		t.Fatalf("recomposed verifier accepted changed replay: %v", err)
	}
}

func TestIAMParseByHand(t *testing.T) {
	v, err := providerreceipt.NewVerifier(providerreceipt.IAMEndpoint("iam-ep", "tenant-a", testSecret))
	if err != nil {
		t.Fatal(err)
	}
	body := `{"event_id":"e9","change_ref":"iam:rev-1","correlation_key":"c","provider_ref":"ISIM-1","outcome":"REJECTED","reason":"no","job_code":"SAL-DIR","grade":"M4","effective_date":"2026-12-01","occurred_at":"x"}`
	h := signed(testSecret, "e9", providerreceipt.IAMEventRejected, providerreceipt.IAMSchema, "tenant-a", wall, body)
	p, err := v.Parse(h, []byte(body), wall)
	if err != nil || p.Outcome != providerreceipt.OutcomeRejected || p.Reason != "no" || p.Details["grade"] != "M4" || p.Provider != "iam" {
		t.Fatalf("parsed = %+v, %v", p, err)
	}
	applied := strings.Replace(body, "REJECTED", "APPLIED", 1)
	h = signed(testSecret, "e10", providerreceipt.IAMEventRejected, providerreceipt.IAMSchema, "tenant-a", wall, strings.Replace(applied, `"e9"`, `"e10"`, 1))
	if _, err := v.Parse(h, []byte(strings.Replace(applied, `"e9"`, `"e10"`, 1)), wall); !errors.Is(err, providerreceipt.ErrOutcomeMismatch) {
		t.Fatalf("payroll outcome on iam: %v", err)
	}
}

func TestNewVerifierValidation(t *testing.T) {
	bad := map[string]func(*providerreceipt.Endpoint){
		"provider":        func(e *providerreceipt.Endpoint) { e.Provider = "workday" },
		"endpoint id":     func(e *providerreceipt.Endpoint) { e.EndpointID = "" },
		"tenant":          func(e *providerreceipt.Endpoint) { e.TenantID = " " },
		"secret":          func(e *providerreceipt.Endpoint) { e.Secret = nil },
		"short secret":    func(e *providerreceipt.Endpoint) { e.Secret = testSecret[:31] },
		"short previous":  func(e *providerreceipt.Endpoint) { e.Secret, e.Secrets = nil, [][]byte{testSecret, []byte("short")} },
		"empty in list":   func(e *providerreceipt.Endpoint) { e.Secret, e.Secrets = nil, [][]byte{testSecret, nil} },
		"secret conflict": func(e *providerreceipt.Endpoint) { e.Secrets = [][]byte{oldSecret, testSecret} },
		"payroll revoked": func(e *providerreceipt.Endpoint) { e.Events["y"] = providerreceipt.OutcomeRevoked },
		"schema":          func(e *providerreceipt.Endpoint) { e.WebhookSchema = "" },
		"events":          func(e *providerreceipt.Endpoint) { e.Events = nil },
		"iam outcome":     func(e *providerreceipt.Endpoint) { e.Events["x"] = providerreceipt.OutcomeGranted },
		"negative bytes":  func(e *providerreceipt.Endpoint) { e.MaxBytes = -1 },
		"negative window": func(e *providerreceipt.Endpoint) { e.Window = -time.Second },
	}
	for name, mutate := range bad {
		ep := providerreceipt.PayrollEndpoint("ep", "tenant-a", testSecret)
		mutate(&ep)
		if _, err := providerreceipt.NewVerifier(ep); !errors.Is(err, providerreceipt.ErrInvalidEndpoint) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A custom window is honoured: 10s tolerates 5s of skew but not 20s.
	v := payrollVerifier(t, func(e *providerreceipt.Endpoint) { e.Window = 10 * time.Second })
	if _, err := v.Parse(okHeader(payrollBody), []byte(payrollBody), wall.Add(20*time.Second)); !errors.Is(err, providerreceipt.ErrOutsideWindow) {
		t.Fatalf("custom window: %v", err)
	}
	if _, err := v.Parse(okHeader(payrollBody), []byte(payrollBody), wall.Add(-5*time.Second)); err != nil {
		t.Fatalf("within custom window: %v", err)
	}
	// Exactly MinSecretLen bytes is enough; the legacy Secret alone still works.
	ep := providerreceipt.PayrollEndpoint("ep", "tenant-a", testSecret[:providerreceipt.MinSecretLen])
	if _, err := providerreceipt.NewVerifier(ep); err != nil {
		t.Fatalf("32-byte secret: %v", err)
	}
	// Secret equal to Secrets[0] is consistent and accepted.
	ep = providerreceipt.PayrollEndpoint("ep", "tenant-a", testSecret)
	ep.Secrets = [][]byte{testSecret, oldSecret}
	if _, err := providerreceipt.NewVerifier(ep); err != nil {
		t.Fatalf("Secret == Secrets[0]: %v", err)
	}
}

// rotatingVerifier accepts testSecret (current, index 0) and oldSecret
// (previous, index 1).
func rotatingVerifier(t *testing.T) *providerreceipt.Verifier {
	t.Helper()
	return payrollVerifier(t, func(e *providerreceipt.Endpoint) {
		e.Secret, e.Secrets = nil, [][]byte{testSecret, oldSecret}
	})
}

func TestRotationAcceptsEveryListedSecret(t *testing.T) {
	for _, tc := range []struct {
		name   string
		secret []byte
		index  int
	}{{"current", testSecret, 0}, {"previous", oldSecret, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			v := rotatingVerifier(t)
			h := signed(tc.secret, "evt-1", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-a", wall, payrollBody)
			p, err := v.Parse(h, []byte(payrollBody), wall)
			if err != nil || p.SecretIndex != tc.index || p.Outcome != providerreceipt.OutcomeApplied {
				t.Fatalf("parsed = %+v, %v", p, err)
			}
		})
	}
	v := rotatingVerifier(t)
	unknown := []byte("unit-secret-0123456789abcdef-unknown!")
	h := signed(unknown, "evt-1", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-a", wall, payrollBody)
	if _, err := v.Parse(h, []byte(payrollBody), wall); !errors.Is(err, providerreceipt.ErrBadSignature) {
		t.Fatalf("unknown secret: %v", err)
	}
	h.Set("Webhook-Signature", "not-hex")
	if _, err := v.Parse(h, []byte(payrollBody), wall); !errors.Is(err, providerreceipt.ErrBadSignature) {
		t.Fatalf("non-hex signature: %v", err)
	}
	// A refused delivery leaves no trace: the same event signed properly is
	// still accepted afterwards.
	if _, err := v.Parse(okHeader(payrollBody), []byte(payrollBody), wall); err != nil {
		t.Fatalf("after refusals: %v", err)
	}
	// Policy checks keep their precedence over the signature check: a stale
	// delivery signed with an unknown secret is reported as stale.
	stale := signed(unknown, "evt-9", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-a", wall, payrollBody)
	if _, err := v.Parse(stale, []byte(payrollBody), wall.Add(time.Hour)); !errors.Is(err, providerreceipt.ErrOutsideWindow) {
		t.Fatalf("stale + unknown secret: %v", err)
	}
}

func TestRotationKeepsOneReceiptPerEventID(t *testing.T) {
	v := rotatingVerifier(t)
	first, err := v.Parse(signed(oldSecret, "evt-1", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-a", wall, payrollBody), []byte(payrollBody), wall)
	if err != nil || first.SecretIndex != 1 {
		t.Fatalf("old-secret delivery: %+v %v", first, err)
	}
	// The provider rotates and redelivers the same event under the new
	// secret: identical bytes are the same receipt.
	again, err := v.Parse(signed(testSecret, "evt-1", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-a", wall.Add(time.Second), payrollBody), []byte(payrollBody), wall.Add(time.Second))
	if err != nil || again.SecretIndex != 0 || again.PayloadDigest != first.PayloadDigest || again.EventID != first.EventID {
		t.Fatalf("re-signed redelivery: %+v %v", again, err)
	}
	// Different bytes under the other secret are still a conflicting
	// restatement, in both directions.
	other := strings.Replace(payrollBody, "PSIM-1", "PSIM-2", 1)
	for _, secret := range [][]byte{testSecret, oldSecret} {
		h := signed(secret, "evt-1", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-a", wall, other)
		if _, err := v.Parse(h, []byte(other), wall); !errors.Is(err, providerreceipt.ErrDuplicateDifferent) {
			t.Fatalf("different bytes, same id: %v", err)
		}
	}
	// A different event type under the same id is also a restatement.
	h := signed(testSecret, "evt-1", providerreceipt.PayrollEventRejected, providerreceipt.PayrollSchema, "tenant-a", wall, strings.Replace(payrollBody, "APPLIED", "REJECTED", 1))
	if _, err := v.Parse(h, []byte(strings.Replace(payrollBody, "APPLIED", "REJECTED", 1)), wall); !errors.Is(err, providerreceipt.ErrDuplicateDifferent) {
		t.Fatalf("different type, same id: %v", err)
	}
}

func TestReversalEventsParse(t *testing.T) {
	reversed := strings.Replace(strings.Replace(payrollBody, `"APPLIED"`, `"REVERSED"`, 1), `"occurred_at"`, `"reversal_reason":"entered in error","occurred_at"`, 1)
	v := payrollVerifier(t, nil)
	p, err := v.Parse(signed(testSecret, "evt-1", providerreceipt.PayrollEventReversed, providerreceipt.PayrollSchema, "tenant-a", wall, reversed), []byte(reversed), wall)
	if err != nil || p.Outcome != providerreceipt.OutcomeReversed || p.EventType != providerreceipt.PayrollEventReversed || p.Details["reversal_reason"] != "entered in error" || p.Details["amount"] != "1.00" {
		t.Fatalf("payroll reversed = %+v, %v", p, err)
	}

	iam, err := providerreceipt.NewVerifier(providerreceipt.IAMEndpoint("iam-ep", "tenant-a", testSecret))
	if err != nil {
		t.Fatal(err)
	}
	revoked := `{"event_id":"e1","change_ref":"iam:rev-1","correlation_key":"c","provider_ref":"ISIM-1","outcome":"REVOKED","reason":"","reversal_reason":"role removed","job_code":"SAL-DIR","grade":"M4","effective_date":"2026-12-01","occurred_at":"x"}`
	p, err = iam.Parse(signed(testSecret, "e1", providerreceipt.IAMEventRevoked, providerreceipt.IAMSchema, "tenant-a", wall, revoked), []byte(revoked), wall)
	if err != nil || p.Outcome != providerreceipt.OutcomeRevoked || p.Details["reversal_reason"] != "role removed" || p.Details["grade"] != "M4" {
		t.Fatalf("iam revoked = %+v, %v", p, err)
	}

	for name, tc := range map[string]struct {
		v         *providerreceipt.Verifier
		id, event string
		schema    string
		body      string
		want      error
	}{
		"reversed event, applied outcome": {payrollVerifier(t, nil), "e2", providerreceipt.PayrollEventReversed, providerreceipt.PayrollSchema,
			strings.Replace(payrollBody, `"evt-1"`, `"e2"`, 1), providerreceipt.ErrOutcomeMismatch},
		"applied event, reversed outcome": {payrollVerifier(t, nil), "e3", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema,
			strings.Replace(strings.Replace(payrollBody, `"evt-1"`, `"e3"`, 1), `"APPLIED"`, `"REVERSED"`, 1), providerreceipt.ErrOutcomeMismatch},
		"reversal_reason on applied": {payrollVerifier(t, nil), "e4", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema,
			strings.Replace(strings.Replace(payrollBody, `"evt-1"`, `"e4"`, 1), `"occurred_at"`, `"reversal_reason":"x","occurred_at"`, 1), providerreceipt.ErrOutcomeMismatch},
		"revoked event on payroll": {payrollVerifier(t, nil), "e5", providerreceipt.IAMEventRevoked, providerreceipt.PayrollSchema,
			strings.Replace(strings.Replace(payrollBody, `"evt-1"`, `"e5"`, 1), `"APPLIED"`, `"REVOKED"`, 1), providerreceipt.ErrUnknownEvent},
		"reversed outcome on iam": {iam, "e6", providerreceipt.IAMEventRevoked, providerreceipt.IAMSchema,
			strings.Replace(strings.Replace(revoked, `"e1"`, `"e6"`, 1), `"REVOKED"`, `"REVERSED"`, 1), providerreceipt.ErrOutcomeMismatch},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tc.v.Parse(signed(testSecret, tc.id, tc.event, tc.schema, "tenant-a", wall, tc.body), []byte(tc.body), wall); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	// An empty reversal_reason on a non-undo event is tolerated (providers
	// may always emit the field) and never lands in Details.
	empty := strings.Replace(strings.Replace(payrollBody, `"evt-1"`, `"e7"`, 1), `"occurred_at"`, `"reversal_reason":"","occurred_at"`, 1)
	p, err = payrollVerifier(t, nil).Parse(signed(testSecret, "e7", providerreceipt.PayrollEventApplied, providerreceipt.PayrollSchema, "tenant-a", wall, empty), []byte(empty), wall)
	if _, has := p.Details["reversal_reason"]; err != nil || has {
		t.Fatalf("empty reversal_reason: %+v %v", p, err)
	}
}
