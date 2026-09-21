package providerdelivery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/oauthcc"
)

const (
	accessPayload = `{"kind":"iam.access_change","change_ref":"acc-1","tenant":"acme","worker_ref":"w-1","job_code":"SAL-DIR","grade":"G7","effective_date":"2026-10-01","correlation_key":"corr-9"}`
	accessGolden  = `{"change_ref":"acc-1","tenant":"acme","worker_ref":"w-1","job_code":"SAL-DIR","grade":"G7","effective_date":"2026-10-01","correlation_key":"corr-9","callback_url":"https://hcm.example.test/callbacks/payroll?src=sim&v=1"}`
	clientSecret  = "iam-secret-never-print"
)

// fakeIAM mints tok-1, tok-2, ... and answers access changes via onAccess.
type fakeIAM struct {
	*httptest.Server
	tokenReqs  atomic.Int64
	accessReqs atomic.Int64
	onToken    func(w http.ResponseWriter) bool
	onAccess   func(w http.ResponseWriter, bearer string)
	cap        capture
	// changeReqs, onChange and changeCap serve every request under
	// /v1/access-changes/ (revocation and status).
	changeReqs atomic.Int64
	onChange   func(w http.ResponseWriter, r *http.Request, bearer string)
	changeCap  capture
}

func newFakeIAM(t *testing.T) *fakeIAM {
	t.Helper()
	f := &fakeIAM{}
	f.onAccess = func(w http.ResponseWriter, _ string) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"change_ref":"acc-1","provider_ref":"IAM-1","status":"ACCEPTED"}`)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		n := f.tokenReqs.Add(1)
		if f.onToken != nil && f.onToken(w) {
			return
		}
		fmt.Fprintf(w, `{"access_token":"tok-%d","token_type":"Bearer","expires_in":300,"scope":"access.write"}`, n)
	})
	mux.HandleFunc("POST /v1/access-changes", func(w http.ResponseWriter, r *http.Request) {
		f.accessReqs.Add(1)
		f.cap.record(r)
		f.onAccess(w, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	})
	mux.HandleFunc("/v1/access-changes/", func(w http.ResponseWriter, r *http.Request) {
		f.changeReqs.Add(1)
		f.changeCap.record(r)
		if f.onChange == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.onChange(w, r, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeIAM) client(t *testing.T, timeout time.Duration) *AccessClient {
	t.Helper()
	tokens, err := oauthcc.New(oauthcc.Config{TokenURL: f.URL + "/oauth2/token", ClientID: "hcm", ClientSecret: clientSecret, Scope: "access.write", Client: f.Client(), Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewAccessClient(AccessConfig{BaseURL: f.URL, Tokens: tokens, Client: f.Client(), Timeout: timeout, Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func invalidTokenChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="iamsim", error="invalid_token"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, `{"error":"invalid_token"}`)
}

func TestAccessDeliverRequestShapeGolden(t *testing.T) {
	f := newFakeIAM(t)
	res, err := f.client(t, 0).Deliver(context.Background(), "acc-1", []byte(accessPayload), callbackURL)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Result{Outcome: Delivered, Class: ClassDelivered, Status: 202, ProviderRef: "IAM-1"}); res != want {
		t.Fatalf("result %+v", res)
	}
	if got := *f.cap.body.Load(); got != accessGolden {
		t.Fatalf("body mismatch\n got: %s\nwant: %s", got, accessGolden)
	}
	h := *f.cap.header.Load()
	if h.Get("Authorization") != "Bearer tok-1" || h.Get("Idempotency-Key") != "acc-1" || h.Get("Content-Type") != "application/json" || h.Get(APIKeyHeader) != "" {
		t.Fatalf("headers %v", h)
	}
}

func TestAccessInvalidTokenRefreshesOnce(t *testing.T) {
	f := newFakeIAM(t)
	f.onAccess = func(w http.ResponseWriter, bearer string) {
		if bearer == "tok-1" {
			invalidTokenChallenge(w)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"provider_ref":"IAM-2"}`)
	}
	res, err := f.client(t, 0).Deliver(context.Background(), "acc-1", []byte(accessPayload), callbackURL)
	if err != nil || res.Outcome != Delivered || res.ProviderRef != "IAM-2" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if f.tokenReqs.Load() != 2 || f.accessReqs.Load() != 2 {
		t.Fatalf("token=%d access=%d, want 2/2", f.tokenReqs.Load(), f.accessReqs.Load())
	}
}

func TestAccessSecondInvalidTokenIsRetry(t *testing.T) {
	f := newFakeIAM(t)
	f.onAccess = func(w http.ResponseWriter, _ string) { invalidTokenChallenge(w) }
	res, err := f.client(t, 0).Deliver(context.Background(), "acc-1", []byte(accessPayload), callbackURL)
	if err != nil || res.Outcome != Retry || res.Class != ClassAuthRefreshFailed || res.Status != 401 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if f.tokenReqs.Load() != 2 || f.accessReqs.Load() != 2 {
		t.Fatalf("token=%d access=%d, want exactly one refresh and one retry", f.tokenReqs.Load(), f.accessReqs.Load())
	}
}

func TestAccessAuthRejections(t *testing.T) {
	for name, tc := range map[string]struct {
		onAccess func(w http.ResponseWriter, _ string)
		onToken  func(w http.ResponseWriter) bool
		outcome  Outcome
		class    string
		reason   string
		access   int64
	}{
		"401 without invalid_token": {
			onAccess: func(w http.ResponseWriter, _ string) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="iamsim"`)
				w.WriteHeader(401)
				_, _ = io.WriteString(w, `{"error":"invalid_request"}`)
			},
			outcome: Rejected, class: ClassRejectedAuth, reason: "invalid_request", access: 1,
		},
		"403 insufficient_scope": {
			onAccess: func(w http.ResponseWriter, _ string) {
				w.WriteHeader(403)
				_, _ = io.WriteString(w, `{"error":"insufficient_scope"}`)
			},
			outcome: Rejected, class: ClassRejectedAuth, reason: "insufficient_scope", access: 1,
		},
		"token 401 invalid_client": {
			onToken: func(w http.ResponseWriter) bool {
				w.WriteHeader(401)
				_, _ = io.WriteString(w, `{"error":"invalid_client","error_description":"bad `+clientSecret+`"}`)
				return true
			},
			outcome: Rejected, class: ClassRejectedAuth, reason: "token_invalid_client",
		},
		"token 400 invalid_scope": {
			onToken: func(w http.ResponseWriter) bool {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":"invalid_scope"}`)
				return true
			},
			outcome: Rejected, class: ClassRejectedAuth, reason: "token_invalid_scope",
		},
		"token 503": {
			onToken: func(w http.ResponseWriter) bool {
				w.WriteHeader(503)
				_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
				return true
			},
			outcome: Retry, class: ClassAuthRefreshFailed, reason: "token_unavailable",
		},
		"token malformed": {
			onToken: func(w http.ResponseWriter) bool {
				_, _ = io.WriteString(w, `{"access_token":"x","token_type":"mac","expires_in":60}`)
				return true
			},
			outcome: Retry, class: ClassAuthRefreshFailed, reason: "token_unavailable",
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeIAM(t)
			if tc.onAccess != nil {
				f.onAccess = tc.onAccess
			}
			f.onToken = tc.onToken
			res, err := f.client(t, 0).Deliver(context.Background(), "acc-1", []byte(accessPayload), callbackURL)
			if err != nil || res.Outcome != tc.outcome || res.Class != tc.class || res.Reason != tc.reason {
				t.Fatalf("res=%+v err=%v", res, err)
			}
			if f.accessReqs.Load() != tc.access || f.tokenReqs.Load() != 1 {
				t.Fatalf("access=%d token=%d", f.accessReqs.Load(), f.tokenReqs.Load())
			}
			if strings.Contains(fmt.Sprintf("%+v", res), clientSecret) {
				t.Fatal("secret leaked into Result")
			}
		})
	}
}

func TestAccessTimeoutCoversTokenFetch(t *testing.T) {
	f := newFakeIAM(t)
	release := make(chan struct{})
	defer close(release)
	f.onToken = func(http.ResponseWriter) bool { <-release; return false }
	start := time.Now()
	res, err := f.client(t, 50*time.Millisecond).Deliver(context.Background(), "acc-1", []byte(accessPayload), callbackURL)
	if err != nil || res.Outcome != Retry || res.Class != ClassTransientTimeout {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if time.Since(start) > 5*time.Second || f.accessReqs.Load() != 0 {
		t.Fatalf("timeout not enforced on token fetch (access=%d)", f.accessReqs.Load())
	}
}

func TestAccessStatusMappingSharedWithPayroll(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		class  string
		reason string
	}{
		{409, `{"error":"idempotency_key_reused"}`, ClassRejectedConflict, "idempotency_key_reused"},
		{422, `{"error":"rejected","reason":"unknown_job_code"}`, ClassRejectedBusiness, "unknown_job_code"},
		{400, `{"error":"invalid","field":"grade"}`, ClassRejectedRequest, "invalid:grade"},
		{503, `{"error":"unavailable"}`, ClassTransientStatus, "unavailable"},
	} {
		f := newFakeIAM(t)
		f.onAccess = func(w http.ResponseWriter, _ string) {
			w.Header().Set("Retry-After", "11")
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}
		res, err := f.client(t, 0).Deliver(context.Background(), "acc-1", []byte(accessPayload), callbackURL)
		if err != nil || res.Class != tc.class || res.Reason != tc.reason || res.Status != tc.status {
			t.Fatalf("%d: res=%+v err=%v", tc.status, res, err)
		}
		if tc.status == 503 && (res.Outcome != Retry || res.RetryAfter != 11*time.Second) {
			t.Fatalf("503: %+v", res)
		}
	}
}

func TestAccessConcurrentDeliverSharesToken(t *testing.T) {
	f := newFakeIAM(t)
	gate := make(chan struct{})
	f.onToken = func(http.ResponseWriter) bool { <-gate; return false }
	c := f.client(t, 5*time.Second)
	const n = 12
	var wg sync.WaitGroup
	results := make([]Result, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = c.Deliver(context.Background(), "acc-1", []byte(accessPayload), callbackURL)
		}(i)
	}
	for f.tokenReqs.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(gate)
	wg.Wait()
	for i, r := range results {
		if r.Outcome != Delivered {
			t.Fatalf("delivery %d: %+v", i, r)
		}
	}
	if f.tokenReqs.Load() != 1 || f.accessReqs.Load() != n {
		t.Fatalf("token=%d access=%d", f.tokenReqs.Load(), f.accessReqs.Load())
	}
}

func TestAccessValidationSendsNothing(t *testing.T) {
	f := newFakeIAM(t)
	c := f.client(t, 0)
	for name, tc := range map[string]struct {
		ref, payload, cb string
		want             error
	}{
		"mismatch":      {"acc-2", accessPayload, callbackURL, ErrInvalidPayload},
		"missing ref":   {"acc-1", strings.Replace(accessPayload, `"acc-1"`, `""`, 1), callbackURL, ErrInvalidPayload},
		"missing grade": {"acc-1", strings.Replace(accessPayload, `"G7"`, `""`, 1), callbackURL, ErrInvalidPayload},
		"bad json":      {"acc-1", `{`, callbackURL, ErrInvalidPayload},
		"bad callback":  {"acc-1", accessPayload, "not a url", ErrInvalidCallbackURL},
	} {
		if _, err := c.Deliver(context.Background(), tc.ref, []byte(tc.payload), tc.cb); !errors.Is(err, tc.want) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	if f.tokenReqs.Load() != 0 || f.accessReqs.Load() != 0 {
		t.Fatal("invalid input caused network traffic")
	}
	if _, err := NewAccessClient(AccessConfig{BaseURL: f.URL}); err == nil {
		t.Fatal("nil Tokens accepted")
	}
	if _, err := NewAccessClient(AccessConfig{BaseURL: "::", Tokens: &oauthcc.TokenSource{}}); err == nil {
		t.Fatal("bad base URL accepted")
	}
	if s := fmt.Sprintf("%v %#v", c, c); !strings.Contains(s, "/v1/access-changes") || strings.Contains(s, "tok-") {
		t.Fatalf("String: %s", s)
	}
	var nilClient *AccessClient
	if nilClient.String() == "" {
		t.Fatal("nil String")
	}
}

func TestTokenFailureContextClasses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := tokenFailure(ctx, context.Canceled); r.Class != ClassTransientNetwork || r.Outcome != Retry {
		t.Fatalf("canceled: %+v", r)
	}
	if r := tokenFailure(context.Background(), fmt.Errorf("wrapped: %w", context.DeadlineExceeded)); r.Class != ClassTransientTimeout {
		t.Fatalf("deadline: %+v", r)
	}
}

func TestAccessReverseRequestShapeAndStatuses(t *testing.T) {
	for _, tc := range []struct {
		status  int
		body    string
		outcome Outcome
		class   string
		reason  string
	}{
		{202, `{"change_ref":"acc-1","status":"REVOCATION_PENDING"}`, Delivered, ClassDelivered, ""},
		{200, `{"change_ref":"acc-1","status":"REVOKED"}`, Delivered, ClassDelivered, ""},
		{409, `{"error":"not_reversible","status":"REJECTED"}`, Delivered, ClassSettledNotReversible, "not_reversible"},
		{409, `{"error":"idempotency_key_reused"}`, Rejected, ClassRejectedConflict, "idempotency_key_reused"},
		{404, `{"error":"not_found"}`, Rejected, ClassRejectedRequest, "not_found"},
		{422, `{"error":"rejected","reason":"account_locked"}`, Rejected, ClassRejectedBusiness, "account_locked"},
		{503, `{"error":"unavailable"}`, Retry, ClassTransientStatus, "unavailable"},
		{403, `{"error":"insufficient_scope"}`, Rejected, ClassRejectedAuth, "insufficient_scope"},
	} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			f := newFakeIAM(t)
			f.onChange = func(w http.ResponseWriter, _ *http.Request, _ string) {
				w.Header().Set("Retry-After", "3")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}
			res, err := f.client(t, 0).Reverse(context.Background(), "acc-1", "role removed")
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != tc.outcome || res.Class != tc.class || res.Reason != tc.reason || res.Status != tc.status {
				t.Fatalf("got %+v", res)
			}
			if tc.status == 503 && res.RetryAfter != 3*time.Second {
				t.Fatalf("retry after %v", res.RetryAfter)
			}
			h := *f.changeCap.header.Load()
			if *f.changeCap.path.Load() != "POST /v1/access-changes/acc-1/revocation" || h.Get("Authorization") != "Bearer tok-1" ||
				h.Get("Idempotency-Key") != "revocation:acc-1" || h.Get("Content-Type") != "application/json" || h.Get(APIKeyHeader) != "" {
				t.Fatalf("request %s %v", *f.changeCap.path.Load(), h)
			}
			if got := *f.changeCap.body.Load(); got != `{"reason":"role removed"}` {
				t.Fatalf("body %s", got)
			}
			if f.changeReqs.Load() != 1 || f.accessReqs.Load() != 0 {
				t.Fatalf("change=%d access=%d", f.changeReqs.Load(), f.accessReqs.Load())
			}
		})
	}
}

func TestAccessStatusMappingAndRequestShape(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		known   bool
		state   string
		outcome Outcome
		class   string
		rReason string
	}{
		{"granted", 200, `{"change_ref":"acc-1","provider_ref":"ISIM-1","status":"GRANTED","granted_at":"2026-09-19T12:00:00Z"}`, true, "GRANTED", Delivered, ClassDelivered, ""},
		{"revoked", 200, `{"change_ref":"acc-1","provider_ref":"ISIM-1","status":"REVOKED","reason":"role removed"}`, true, "REVOKED", Delivered, ClassDelivered, ""},
		{"payroll state on iam", 200, `{"change_ref":"acc-1","status":"APPLIED"}`, false, "", Retry, ClassTransientStatus, "unrecognised_status"},
		{"unknown", 404, `{"error":"not_found"}`, false, "", Delivered, ClassDelivered, "not_found"},
		{"401 without invalid_token", 401, `{"error":"invalid_request"}`, false, "", Rejected, ClassRejectedAuth, "invalid_request"},
		{"429", 429, `{}`, false, "", Retry, ClassTransientStatus, "status_429"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIAM(t)
			f.onChange = func(w http.ResponseWriter, _ *http.Request, _ string) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}
			got, err := f.client(t, 0).Status(context.Background(), "acc-1")
			if err != nil {
				t.Fatal(err)
			}
			if got.Known != tc.known || got.Status != tc.state || got.Result.Outcome != tc.outcome || got.Result.Class != tc.class || got.Result.Reason != tc.rReason || got.Result.Status != tc.status {
				t.Fatalf("got %+v", got)
			}
			if tc.known && (got.ProviderRef != "ISIM-1" || (tc.state == "REVOKED" && got.Reason != "role removed")) {
				t.Fatalf("got %+v", got)
			}
			h := *f.changeCap.header.Load()
			if *f.changeCap.path.Load() != "GET /v1/access-changes/acc-1" || h.Get("Authorization") != "Bearer tok-1" || h.Get("Idempotency-Key") != "" || h.Get("Content-Type") != "" {
				t.Fatalf("request %s %v", *f.changeCap.path.Load(), h)
			}
		})
	}
}

// TestAccessUndoAndStatusRefreshOnce proves Reverse and Status share
// Deliver's single invalid_token refresh: one refresh succeeds, a second
// invalid_token is auth_refresh_failed with no third attempt.
func TestAccessUndoAndStatusRefreshOnce(t *testing.T) {
	calls := map[string]func(*AccessClient) (Result, error){
		"reverse": func(c *AccessClient) (Result, error) { return c.Reverse(context.Background(), "acc-1", "why") },
		"status": func(c *AccessClient) (Result, error) {
			s, err := c.Status(context.Background(), "acc-1")
			if s.Known != (s.Result.Outcome == Delivered) {
				return Result{}, fmt.Errorf("known=%v with outcome %v", s.Known, s.Result.Outcome)
			}
			return s.Result, err
		},
	}
	for name, call := range calls {
		t.Run(name+"/refresh succeeds", func(t *testing.T) {
			f := newFakeIAM(t)
			var bearers []string
			var mu sync.Mutex
			f.onChange = func(w http.ResponseWriter, r *http.Request, bearer string) {
				mu.Lock()
				bearers = append(bearers, bearer)
				mu.Unlock()
				if bearer == "tok-1" {
					invalidTokenChallenge(w)
					return
				}
				if r.Method == http.MethodGet {
					_, _ = io.WriteString(w, `{"change_ref":"acc-1","provider_ref":"ISIM-1","status":"GRANTED"}`)
					return
				}
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, `{"change_ref":"acc-1","status":"REVOCATION_PENDING"}`)
			}
			res, err := call(f.client(t, 0))
			if err != nil || res.Outcome != Delivered {
				t.Fatalf("res=%+v err=%v", res, err)
			}
			if f.tokenReqs.Load() != 2 || f.changeReqs.Load() != 2 || strings.Join(bearers, ",") != "tok-1,tok-2" {
				t.Fatalf("token=%d change=%d bearers=%v", f.tokenReqs.Load(), f.changeReqs.Load(), bearers)
			}
		})
		t.Run(name+"/second invalid_token", func(t *testing.T) {
			f := newFakeIAM(t)
			f.onChange = func(w http.ResponseWriter, _ *http.Request, _ string) { invalidTokenChallenge(w) }
			res, err := call(f.client(t, 0))
			if err != nil || res.Outcome != Retry || res.Class != ClassAuthRefreshFailed || res.Reason != "invalid_token_after_refresh" {
				t.Fatalf("res=%+v err=%v", res, err)
			}
			if f.tokenReqs.Load() != 2 || f.changeReqs.Load() != 2 {
				t.Fatalf("token=%d change=%d, want exactly one refresh", f.tokenReqs.Load(), f.changeReqs.Load())
			}
		})
		t.Run(name+"/token endpoint rejects", func(t *testing.T) {
			f := newFakeIAM(t)
			f.onToken = func(w http.ResponseWriter) bool {
				w.WriteHeader(401)
				_, _ = io.WriteString(w, `{"error":"invalid_client","error_description":"`+clientSecret+`"}`)
				return true
			}
			res, err := call(f.client(t, 0))
			if err != nil || res.Outcome != Rejected || res.Class != ClassRejectedAuth || res.Reason != "token_invalid_client" || f.changeReqs.Load() != 0 {
				t.Fatalf("res=%+v err=%v change=%d", res, err, f.changeReqs.Load())
			}
			if strings.Contains(fmt.Sprintf("%+v", res), clientSecret) {
				t.Fatal("secret leaked into Result")
			}
		})
	}
}

func TestAccessUndoValidationSendsNothing(t *testing.T) {
	f := newFakeIAM(t)
	c := f.client(t, 0)
	if _, err := c.Reverse(context.Background(), "", "why"); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("empty ref: %v", err)
	}
	if _, err := c.Reverse(context.Background(), "acc-1", ""); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("empty reason: %v", err)
	}
	if _, err := c.Status(context.Background(), ".."); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("dotdot: %v", err)
	}
	if f.tokenReqs.Load() != 0 || f.changeReqs.Load() != 0 {
		t.Fatal("invalid input caused network traffic")
	}
	f.Close()
	if res, err := c.Status(context.Background(), "acc-1"); err != nil || res.Known || res.Result.Outcome != Retry {
		t.Fatalf("closed server: %+v %v", res, err)
	}
}
