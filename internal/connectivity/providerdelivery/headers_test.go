package providerdelivery

import (
	"context"
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

const testTraceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

type ctxKeyTrace struct{}

// hostileSource returns the observability headers plus every protected
// header an attacker-controlled or buggy source might try to smuggle in, and
// two malformed entries that would make net/http refuse the request.
func hostileSource(ctx context.Context) http.Header {
	tp, _ := ctx.Value(ctxKeyTrace{}).(string)
	h := http.Header{}
	if tp != "" {
		h.Set("traceparent", tp)
	}
	h.Set("X-Correlation-Id", "corr-77")
	h.Set("Authorization", "Bearer forged")
	h.Set(APIKeyHeader, "forged-key")
	h.Set("Idempotency-Key", "forged-idem")
	h.Set("Content-Type", "text/plain")
	h.Set("Accept", "text/html")
	h["X-Injected"] = []string{"a\r\nX-Evil: 1"}
	h["Bad Name"] = []string{"v"}
	return h
}

func traceCtx() context.Context {
	return context.WithValue(context.Background(), ctxKeyTrace{}, testTraceParent)
}

// headerLog records the headers of every request a test server sees.
type headerLog struct {
	mu   sync.Mutex
	seen []http.Header
}

func (l *headerLog) add(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, r.Header.Clone())
}

func (l *headerLog) all() []http.Header {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]http.Header(nil), l.seen...)
}

func TestPayrollHeaderSourceAddsTraceHeadersWithoutOverridingCredentials(t *testing.T) {
	var log headerLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r)
		_, _ = io.Copy(io.Discard, r.Body)
		switch {
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"change_ref":"chg-1","provider_ref":"P","status":"APPLIED"}`)
		default:
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"change_ref":"chg-1","provider_ref":"P","status":"ACCEPTED"}`)
		}
	}))
	defer srv.Close()
	c, err := NewPayrollClient(PayrollConfig{BaseURL: srv.URL, APIKey: testAPIKey, Client: srv.Client(), Now: func() time.Time { return fixedNow }, Headers: hostileSource})
	if err != nil {
		t.Fatal(err)
	}
	ctx := traceCtx()
	if res, err := c.Deliver(ctx, "chg-1", []byte(payrollPayload), callbackURL); err != nil || res.Outcome != Delivered {
		t.Fatalf("Deliver = %+v, %v", res, err)
	}
	if res, err := c.Reverse(ctx, "chg-1", "rollback"); err != nil || res.Outcome != Delivered {
		t.Fatalf("Reverse = %+v, %v", res, err)
	}
	if st, err := c.Status(ctx, "chg-1"); err != nil || !st.Known {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	seen := log.all()
	if len(seen) != 3 {
		t.Fatalf("server saw %d requests, want 3", len(seen))
	}
	wantIdem := []string{"chg-1", ReversalIdempotencyPrefix + "chg-1", ""}
	for i, h := range seen {
		if h.Get("Traceparent") != testTraceParent || h.Get("X-Correlation-Id") != "corr-77" {
			t.Errorf("request %d: trace headers missing: %v", i, h)
		}
		if h.Get(APIKeyHeader) != testAPIKey {
			t.Errorf("request %d: X-Api-Key overridden: %q", i, h.Get(APIKeyHeader))
		}
		if h.Get("Authorization") != "" {
			t.Errorf("request %d: source introduced Authorization", i)
		}
		if got := h.Get("Idempotency-Key"); got != wantIdem[i] {
			t.Errorf("request %d: Idempotency-Key = %q, want %q", i, got, wantIdem[i])
		}
		if h.Get("Accept") != "application/json" {
			t.Errorf("request %d: Accept overridden: %q", i, h.Get("Accept"))
		}
		if h.Get("X-Injected") != "" || h.Get("X-Evil") != "" {
			t.Errorf("request %d: malformed header forwarded: %v", i, h)
		}
	}
	if seen[0].Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type overridden: %q", seen[0].Get("Content-Type"))
	}
}

func TestPayrollNilHeaderSourceAddsNothing(t *testing.T) {
	var log headerLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	if _, err := newPayroll(t, srv.URL, srv.Client(), 0).Deliver(traceCtx(), "chg-1", []byte(payrollPayload), callbackURL); err != nil {
		t.Fatal(err)
	}
	if h := log.all()[0]; h.Get("Traceparent") != "" || h.Get("X-Correlation-Id") != "" {
		t.Fatalf("nil HeaderSource added headers: %v", h)
	}
}

func TestAccessHeaderSourceCoversDeliveryAndTokenFetch(t *testing.T) {
	var apiLog, tokenLog headerLog
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		tokenLog.add(r)
		_, _ = io.WriteString(w, `{"access_token":"tok-1","token_type":"Bearer","expires_in":300}`)
	})
	mux.HandleFunc("POST /v1/access-changes", func(w http.ResponseWriter, r *http.Request) {
		apiLog.add(r)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"change_ref":"acc-1","provider_ref":"IAM-1","status":"ACCEPTED"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tokens, err := oauthcc.New(oauthcc.Config{
		TokenURL: srv.URL + "/oauth2/token", ClientID: "hcm", ClientSecret: clientSecret,
		Client: NewHeaderDoer(srv.Client(), hostileSource), Now: func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewAccessClient(AccessConfig{BaseURL: srv.URL, Tokens: tokens, Client: srv.Client(), Now: func() time.Time { return fixedNow }, Headers: hostileSource})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := c.Deliver(traceCtx(), "acc-1", []byte(accessPayload), callbackURL); err != nil || res.Outcome != Delivered {
		t.Fatalf("Deliver = %+v, %v", res, err)
	}

	api := apiLog.all()[0]
	if api.Get("Traceparent") != testTraceParent || api.Get("X-Correlation-Id") != "corr-77" {
		t.Fatalf("delivery trace headers missing: %v", api)
	}
	if api.Get("Authorization") != "Bearer tok-1" || api.Get("Idempotency-Key") != "acc-1" || api.Get(APIKeyHeader) != "" {
		t.Fatalf("delivery credentials overridden: %v", api)
	}

	tok := tokenLog.all()[0]
	if tok.Get("Traceparent") != testTraceParent {
		t.Fatalf("token fetch did not carry traceparent: %v", tok)
	}
	if !strings.HasPrefix(tok.Get("Authorization"), "Basic ") {
		t.Fatalf("token fetch Authorization overridden: %q", tok.Get("Authorization"))
	}
	if tok.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Fatalf("token fetch Content-Type overridden: %q", tok.Get("Content-Type"))
	}
}

func TestNewHeaderDoerDoesNotMutateCallerOrFollowRedirects(t *testing.T) {
	var hits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1) }))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	d := NewHeaderDoer(redirector.Client(), hostileSource)
	req, err := http.NewRequestWithContext(traceCtx(), http.MethodPost, redirector.URL, strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := d.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || hits.Load() != 0 {
		t.Fatalf("redirect followed: status %d, target hits %d", resp.StatusCode, hits.Load())
	}
	if req.Header.Get("Traceparent") != "" {
		t.Fatal("NewHeaderDoer mutated the caller's request headers")
	}

	if _, ok := NewHeaderDoer(nil, nil).(*http.Client); !ok {
		t.Fatal("NewHeaderDoer(nil, nil) should be a redirect-safe *http.Client")
	}
}

func TestApplyHeaderSourceKeepsExistingValues(t *testing.T) {
	h := http.Header{}
	h.Set("Traceparent", "caller-set")
	applyHeaderSource(traceCtx(), h, hostileSource)
	if got := h.Values("Traceparent"); len(got) != 1 || got[0] != "caller-set" {
		t.Fatalf("existing traceparent overwritten: %v", got)
	}
	long := strings.Repeat("a", maxSourcedHeaderValue+1)
	applyHeaderSource(context.Background(), h, func(context.Context) http.Header {
		return http.Header{"X-Long": {long}, "X-Empty": {""}}
	})
	if h.Get("X-Long") != "" || len(h.Values("X-Empty")) != 0 {
		t.Fatalf("unbounded or empty values forwarded: %v", h)
	}
}
