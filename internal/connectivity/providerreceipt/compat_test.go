package providerreceipt_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/iamsim"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/payrollsim"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
)

// receiver is an httptest callback endpoint that runs every delivery through
// Verifier.Parse and answers 200 or 400 accordingly.
type receiver struct {
	srv *httptest.Server
	mu  sync.Mutex
	got []parseResult
}

type parseResult struct {
	parsed providerreceipt.Parsed
	err    error
}

func newReceiver(t *testing.T, v *providerreceipt.Verifier) *receiver {
	t.Helper()
	rc := &receiver{}
	rc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p, err := v.Parse(r.Header, body, time.Now())
		rc.mu.Lock()
		rc.got = append(rc.got, parseResult{p, err})
		rc.mu.Unlock()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(rc.srv.Close)
	return rc
}

func (rc *receiver) results() []parseResult {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]parseResult(nil), rc.got...)
}

func noSleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

func drain(t *testing.T, shutdown func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("simulator did not drain: %v", err)
	}
}

func TestSimulatorContractConstants(t *testing.T) {
	if providerreceipt.PayrollSchema != payrollsim.Schema || providerreceipt.PayrollEventApplied != payrollsim.EventApplied || providerreceipt.PayrollEventRejected != payrollsim.EventRejected ||
		providerreceipt.PayrollEventReversed != payrollsim.EventReversed || string(providerreceipt.OutcomeReversed) != payrollsim.StatusReversed {
		t.Fatal("payroll constants drifted from payrollsim")
	}
	if providerreceipt.IAMSchema != iamsim.Schema || providerreceipt.IAMEventGranted != iamsim.EventGranted || providerreceipt.IAMEventRejected != iamsim.EventRejected ||
		providerreceipt.IAMEventRevoked != iamsim.EventRevoked || string(providerreceipt.OutcomeRevoked) != iamsim.StatusRevoked {
		t.Fatal("iam constants drifted from iamsim")
	}
}

func TestPayrollSimulatorCallbacksParse(t *testing.T) {
	for _, mode := range []payrollsim.Mode{payrollsim.ModeApply, payrollsim.ModeReject} {
		t.Run(string(mode), func(t *testing.T) {
			secret := []byte("payroll-shared-secret-0123456789abcdef")
			v, err := providerreceipt.NewVerifier(providerreceipt.PayrollEndpoint("payroll-ep", "harborcare-demo", secret))
			if err != nil {
				t.Fatal(err)
			}
			rc := newReceiver(t, v)
			sc := payrollsim.DefaultScenario()
			sc.Mode, sc.ApplyDelayMS, sc.DuplicateCallbacks, sc.RejectReason = mode, 0, true, "pay above band"
			sim := payrollsim.New(payrollsim.Config{Secret: secret, Scenario: &sc, Sleep: noSleep, MaxAttempts: 1, HTTPClient: rc.srv.Client()})
			change := payrollsim.PayChangeRequest{ChangeRef: "payroll:rev-1", Tenant: "harborcare-demo", WorkerRef: "worker-42",
				BasePay: payrollsim.Money{Amount: "160000.00", Currency: "USD"}, EffectiveDate: "2026-12-01", CorrelationKey: "corr-abc", CallbackURL: rc.srv.URL}
			raw, _ := json.Marshal(change)
			req := httptest.NewRequest(http.MethodPost, "/v1/pay-changes", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", change.ChangeRef)
			rec := httptest.NewRecorder()
			sim.ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("intake = %d %s", rec.Code, rec.Body)
			}
			drain(t, sim.Shutdown)

			got := rc.results()
			if len(got) != 2 {
				t.Fatalf("deliveries = %d, want 2 (duplicate callbacks)", len(got))
			}
			for i, r := range got {
				if r.err != nil {
					t.Fatalf("delivery %d: %v", i, r.err)
				}
			}
			p := got[0].parsed
			wantOutcome, wantEvent, wantReason := providerreceipt.OutcomeApplied, payrollsim.EventApplied, ""
			if mode == payrollsim.ModeReject {
				wantOutcome, wantEvent, wantReason = providerreceipt.OutcomeRejected, payrollsim.EventRejected, "pay above band"
			}
			if p.Provider != "payroll" || p.Outcome != wantOutcome || p.EventType != wantEvent || p.Reason != wantReason ||
				p.ChangeRef != "payroll:rev-1" || p.CorrelationKey != "corr-abc" || p.TenantID != "harborcare-demo" || p.Schema != payrollsim.Schema {
				t.Fatalf("parsed = %+v", p)
			}
			if p.Details["amount"] != "160000.00" || p.Details["currency"] != "USD" || p.Details["effective_date"] != "2026-12-01" || p.Details["occurred_at"] == "" {
				t.Fatalf("details = %v", p.Details)
			}
			if p.ProviderRef == "" || p.PayloadDigest == "" || got[1].parsed.PayloadDigest != p.PayloadDigest || got[1].parsed.EventID != p.EventID {
				t.Fatalf("duplicate delivery differs: %+v vs %+v", p, got[1].parsed)
			}
		})
	}
}

func TestIAMSimulatorCallbacksParse(t *testing.T) {
	for _, mode := range []iamsim.Mode{iamsim.ModeGrant, iamsim.ModeReject} {
		t.Run(string(mode), func(t *testing.T) {
			secret := []byte("iam-shared-secret-0123456789abcdef0123")
			v, err := providerreceipt.NewVerifier(providerreceipt.IAMEndpoint("iam-ep", "harborcare-demo", secret))
			if err != nil {
				t.Fatal(err)
			}
			rc := newReceiver(t, v)
			sc := iamsim.DefaultScenario()
			sc.Mode, sc.GrantDelayMS, sc.DuplicateCallbacks, sc.RejectReason = mode, 0, true, "grade not allowed"
			sim := iamsim.New(iamsim.Config{ClientID: "hcm", ClientSecret: "client-secret", WebhookSecret: secret, Scenario: &sc, Sleep: noSleep, MaxAttempts: 1, HTTPClient: rc.srv.Client()})

			treq := httptest.NewRequest(http.MethodPost, "/oauth2/token", bytes.NewReader([]byte("grant_type=client_credentials")))
			treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			treq.SetBasicAuth("hcm", "client-secret")
			trec := httptest.NewRecorder()
			sim.ServeHTTP(trec, treq)
			var tok iamsim.TokenResponse
			if trec.Code != http.StatusOK || json.Unmarshal(trec.Body.Bytes(), &tok) != nil {
				t.Fatalf("token = %d %s", trec.Code, trec.Body)
			}

			change := iamsim.AccessChangeRequest{ChangeRef: "iam:rev-1", Tenant: "harborcare-demo", WorkerRef: "worker-42", JobCode: "SAL-DIR",
				Grade: "M4", EffectiveDate: "2026-12-01", CorrelationKey: "corr-abc", CallbackURL: rc.srv.URL}
			raw, _ := json.Marshal(change)
			req := httptest.NewRequest(http.MethodPost, "/v1/access-changes", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
			req.Header.Set("Idempotency-Key", change.ChangeRef)
			rec := httptest.NewRecorder()
			sim.ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("intake = %d %s", rec.Code, rec.Body)
			}
			drain(t, sim.Shutdown)

			got := rc.results()
			if len(got) != 2 {
				t.Fatalf("deliveries = %d, want 2", len(got))
			}
			for i, r := range got {
				if r.err != nil {
					t.Fatalf("delivery %d: %v", i, r.err)
				}
			}
			p := got[0].parsed
			wantOutcome, wantReason := providerreceipt.OutcomeGranted, ""
			if mode == iamsim.ModeReject {
				wantOutcome, wantReason = providerreceipt.OutcomeRejected, "grade not allowed"
			}
			if p.Provider != "iam" || p.Outcome != wantOutcome || p.Reason != wantReason || p.ChangeRef != "iam:rev-1" || p.CorrelationKey != "corr-abc" {
				t.Fatalf("parsed = %+v", p)
			}
			if p.Details["job_code"] != "SAL-DIR" || p.Details["grade"] != "M4" || p.Details["effective_date"] != "2026-12-01" {
				t.Fatalf("details = %v", p.Details)
			}
			if got[1].parsed.PayloadDigest != p.PayloadDigest {
				t.Fatal("duplicate delivery digest differs")
			}
		})
	}
}

// waitFor polls until n callbacks have been received.
func (rc *receiver) waitFor(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(rc.results()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("callbacks = %d, want %d", len(rc.results()), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestSimulatorUndoCallbacksParse drives each simulator through
// apply/grant then reversal/revocation and parses both callbacks with a
// rotating Verifier whose PREVIOUS secret is the one the simulator signs
// with, so the rotation path is exercised against real signatures.
func TestSimulatorUndoCallbacksParse(t *testing.T) {
	current := []byte("undo-current-secret-0123456789abcdef")
	previous := []byte("undo-previous-secret-0123456789abcde")

	t.Run("payroll", func(t *testing.T) {
		ep := providerreceipt.PayrollEndpoint("payroll-ep", "harborcare-demo", nil)
		ep.Secrets = [][]byte{current, previous}
		v, err := providerreceipt.NewVerifier(ep)
		if err != nil {
			t.Fatal(err)
		}
		rc := newReceiver(t, v)
		sc := payrollsim.DefaultScenario()
		sc.ApplyDelayMS, sc.DuplicateCallbacks = 0, false
		sim := payrollsim.New(payrollsim.Config{Secret: previous, Scenario: &sc, Sleep: noSleep, MaxAttempts: 1, HTTPClient: rc.srv.Client()})
		change := payrollsim.PayChangeRequest{ChangeRef: "payroll:rev-9", Tenant: "harborcare-demo", WorkerRef: "worker-42",
			BasePay: payrollsim.Money{Amount: "160000.00", Currency: "USD"}, EffectiveDate: "2026-12-01", CorrelationKey: "corr-undo", CallbackURL: rc.srv.URL}
		raw, _ := json.Marshal(change)
		serve(t, sim, "/v1/pay-changes", change.ChangeRef, "", raw, http.StatusAccepted)
		rc.waitFor(t, 1)
		serve(t, sim, "/v1/pay-changes/payroll:rev-9/reversal", "reversal:payroll:rev-9", "", []byte(`{"reason":"entered in error"}`), http.StatusAccepted)
		drain(t, sim.Shutdown)
		got := rc.results()
		if len(got) != 2 || got[0].err != nil || got[1].err != nil {
			t.Fatalf("callbacks = %+v", got)
		}
		applied, reversed := got[0].parsed, got[1].parsed
		if applied.Outcome != providerreceipt.OutcomeApplied || applied.SecretIndex != 1 {
			t.Fatalf("applied = %+v", applied)
		}
		if reversed.Outcome != providerreceipt.OutcomeReversed || reversed.EventType != providerreceipt.PayrollEventReversed || reversed.SecretIndex != 1 ||
			reversed.EventID == applied.EventID || reversed.ChangeRef != "payroll:rev-9" || reversed.Details["reversal_reason"] != "entered in error" {
			t.Fatalf("reversed = %+v", reversed)
		}
	})

	t.Run("iam", func(t *testing.T) {
		ep := providerreceipt.IAMEndpoint("iam-ep", "harborcare-demo", nil)
		ep.Secrets = [][]byte{current, previous}
		v, err := providerreceipt.NewVerifier(ep)
		if err != nil {
			t.Fatal(err)
		}
		rc := newReceiver(t, v)
		sc := iamsim.DefaultScenario()
		sc.GrantDelayMS, sc.DuplicateCallbacks = 0, false
		sim := iamsim.New(iamsim.Config{ClientID: "hcm", ClientSecret: "client-secret", WebhookSecret: previous, Scenario: &sc, Sleep: noSleep, MaxAttempts: 1, HTTPClient: rc.srv.Client()})
		treq := httptest.NewRequest(http.MethodPost, "/oauth2/token", bytes.NewReader([]byte("grant_type=client_credentials")))
		treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		treq.SetBasicAuth("hcm", "client-secret")
		trec := httptest.NewRecorder()
		sim.ServeHTTP(trec, treq)
		var tok iamsim.TokenResponse
		if trec.Code != http.StatusOK || json.Unmarshal(trec.Body.Bytes(), &tok) != nil {
			t.Fatalf("token = %d %s", trec.Code, trec.Body)
		}
		change := iamsim.AccessChangeRequest{ChangeRef: "iam:rev-9", Tenant: "harborcare-demo", WorkerRef: "worker-42", JobCode: "SAL-DIR",
			Grade: "M4", EffectiveDate: "2026-12-01", CorrelationKey: "corr-undo", CallbackURL: rc.srv.URL}
		raw, _ := json.Marshal(change)
		serve(t, sim, "/v1/access-changes", change.ChangeRef, tok.AccessToken, raw, http.StatusAccepted)
		rc.waitFor(t, 1)
		serve(t, sim, "/v1/access-changes/iam:rev-9/revocation", "revocation:iam:rev-9", tok.AccessToken, []byte(`{"reason":"role removed"}`), http.StatusAccepted)
		drain(t, sim.Shutdown)
		got := rc.results()
		if len(got) != 2 || got[0].err != nil || got[1].err != nil {
			t.Fatalf("callbacks = %+v", got)
		}
		granted, revoked := got[0].parsed, got[1].parsed
		if granted.Outcome != providerreceipt.OutcomeGranted || granted.SecretIndex != 1 {
			t.Fatalf("granted = %+v", granted)
		}
		if revoked.Outcome != providerreceipt.OutcomeRevoked || revoked.EventType != providerreceipt.IAMEventRevoked || revoked.SecretIndex != 1 ||
			revoked.EventID == granted.EventID || revoked.Details["reversal_reason"] != "role removed" || revoked.Details["grade"] != "M4" {
			t.Fatalf("revoked = %+v", revoked)
		}
	})
}

// serve posts one JSON request straight into a simulator handler.
func serve(t *testing.T, h http.Handler, path, idempotencyKey, bearer string, body []byte, want int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("POST %s = %d %s", path, rec.Code, rec.Body)
	}
}
