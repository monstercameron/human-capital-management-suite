package oauthcc

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testID     = "hcm client:1&x=y"
	testSecret = "s3cr+et/with:colon%and space"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)} }
func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// tokenServer mints tok-1, tok-2, ... and counts requests. handler, when set,
// overrides the response.
type tokenServer struct {
	*httptest.Server
	count    atomic.Int64
	gate     chan struct{}
	handler  func(w http.ResponseWriter, r *http.Request) bool
	lastReq  atomic.Pointer[http.Request]
	lastBody atomic.Pointer[string]
}

func newTokenServer(t *testing.T, expiresIn int) *tokenServer {
	t.Helper()
	ts := &tokenServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := ts.count.Add(1)
		b, _ := io.ReadAll(r.Body)
		s := string(b)
		ts.lastBody.Store(&s)
		ts.lastReq.Store(r)
		if ts.gate != nil {
			<-ts.gate
		}
		if ts.handler != nil && ts.handler(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"tok-%d","token_type":"bearer","expires_in":%d,"scope":"access.write"}`, n, expiresIn)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func newSource(t *testing.T, srv *tokenServer, clk *clock) *TokenSource {
	t.Helper()
	ts, err := New(Config{TokenURL: srv.URL + "/oauth2/token", ClientID: testID, ClientSecret: testSecret, Scope: "access.write", Client: srv.Client(), Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestTokenCachesUntilSkewedExpiry(t *testing.T) {
	srv := newTokenServer(t, 300)
	clk := newClock()
	ts := newSource(t, srv, clk)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		tok, err := ts.Token(ctx)
		if err != nil || tok != "tok-1" {
			t.Fatalf("call %d: tok=%q err=%v", i, tok, err)
		}
	}
	if srv.count.Load() != 1 {
		t.Fatalf("token requests = %d, want 1", srv.count.Load())
	}
	// 300s TTL, 30s skew: fresh until 270s.
	clk.Advance(269 * time.Second)
	if tok, _ := ts.Token(ctx); tok != "tok-1" {
		t.Fatalf("refreshed too early: %q", tok)
	}
	clk.Advance(time.Second)
	if tok, _ := ts.Token(ctx); tok != "tok-2" {
		t.Fatalf("not refreshed at expiry-skew: %q", tok)
	}
	if srv.count.Load() != 2 {
		t.Fatalf("token requests = %d, want 2", srv.count.Load())
	}
}

func TestTokenRequestShapeAndBasicAuth(t *testing.T) {
	srv := newTokenServer(t, 60)
	ts := newSource(t, srv, newClock())
	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := srv.lastReq.Load()
	if r.Method != http.MethodPost || r.URL.Path != "/oauth2/token" {
		t.Fatalf("%s %s", r.Method, r.URL.Path)
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Fatalf("content-type %q", ct)
	}
	form, err := url.ParseQuery(*srv.lastBody.Load())
	if err != nil || form.Get("grant_type") != "client_credentials" || form.Get("scope") != "access.write" || form.Has("client_secret") || form.Has("client_id") {
		t.Fatalf("form %v err=%v", form, err)
	}
	authz := r.Header.Get("Authorization")
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authz, "Basic "))
	if err != nil || !strings.HasPrefix(authz, "Basic ") {
		t.Fatalf("authorization %q", authz)
	}
	// RFC 6749 2.3.1: each half is form-urlencoded, so the only raw colon
	// is the separator.
	if strings.Count(string(raw), ":") != 1 {
		t.Fatalf("separator ambiguous: %q", raw)
	}
	idPart, secretPart, _ := strings.Cut(string(raw), ":")
	if idPart != "hcm+client%3A1%26x%3Dy" {
		t.Fatalf("encoded id %q", idPart)
	}
	id, _ := url.QueryUnescape(idPart)
	secret, _ := url.QueryUnescape(secretPart)
	if id != testID || secret != testSecret {
		t.Fatalf("round trip id=%q secret mismatch=%v", id, secret != testSecret)
	}
}

func TestTokenSingleFlight(t *testing.T) {
	srv := newTokenServer(t, 300)
	srv.gate = make(chan struct{})
	ts := newSource(t, srv, newClock())
	const callers = 20
	var wg sync.WaitGroup
	toks := make([]string, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			toks[i], errs[i] = ts.Token(context.Background())
		}(i)
	}
	// Wait until the one request is parked at the server, then give the
	// other callers time to join it before releasing.
	deadline := time.Now().Add(5 * time.Second)
	for srv.count.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(srv.gate)
	wg.Wait()
	for i := range toks {
		if errs[i] != nil || toks[i] != "tok-1" {
			t.Fatalf("caller %d: %q %v", i, toks[i], errs[i])
		}
	}
	if n := srv.count.Load(); n != 1 {
		t.Fatalf("token requests = %d, want 1", n)
	}
}

func TestTokenWaiterContextDoesNotPoisonOthers(t *testing.T) {
	srv := newTokenServer(t, 300)
	srv.gate = make(chan struct{})
	ts := newSource(t, srv, newClock())
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := ts.Token(ctx); errc <- err }()
	for srv.count.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled leader err=%v", err)
	}
	close(srv.gate)
	tok, err := ts.Token(context.Background())
	if err != nil || tok != "tok-1" {
		t.Fatalf("tok=%q err=%v", tok, err)
	}
}

func TestInvalidate(t *testing.T) {
	srv := newTokenServer(t, 300)
	ts := newSource(t, srv, newClock())
	ctx := context.Background()
	tok, _ := ts.Token(ctx)
	ts.Invalidate("some-other-token")
	ts.Invalidate("")
	if again, _ := ts.Token(ctx); again != tok || srv.count.Load() != 1 {
		t.Fatalf("Invalidate(other) dropped cache: %q count=%d", again, srv.count.Load())
	}
	ts.Invalidate(tok)
	if again, _ := ts.Token(ctx); again != "tok-2" || srv.count.Load() != 2 {
		t.Fatalf("Invalidate(stale) kept cache: %q count=%d", again, srv.count.Load())
	}
	// A late Invalidate of the old token must not discard the new one.
	ts.Invalidate(tok)
	if again, _ := ts.Token(ctx); again != "tok-2" {
		t.Fatalf("racing refresh discarded: %q", again)
	}
}

func TestTokenErrorResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		code   string
	}{
		{"invalid_client", 401, `{"error":"invalid_client","error_description":"client authentication failed ` + testSecret + `"}`, "invalid_client"},
		{"invalid_scope", 400, `{"error":"invalid_scope"}`, "invalid_scope"},
		{"unavailable non-json", 503, `upstream down`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTokenServer(t, 300)
			srv.handler = func(w http.ResponseWriter, _ *http.Request) bool {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
				return true
			}
			ts := newSource(t, srv, newClock())
			_, err := ts.Token(context.Background())
			var te *TokenError
			if !errors.As(err, &te) || te.Status != tc.status || te.Code != tc.code {
				t.Fatalf("err=%#v", err)
			}
			assertNoSecret(t, err)
		})
	}
}

func TestTokenMalformedSuccessResponses(t *testing.T) {
	for name, body := range map[string]string{
		"mac token type": `{"access_token":"a","token_type":"mac","expires_in":60}`,
		"zero expiry":    `{"access_token":"a","token_type":"Bearer","expires_in":0}`,
		"no token":       `{"token_type":"Bearer","expires_in":60}`,
		"not json":       `<html>`,
		"too large":      `{"access_token":"` + strings.Repeat("a", MaxResponseBytes) + `","token_type":"Bearer","expires_in":60}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := newTokenServer(t, 300)
			srv.handler = func(w http.ResponseWriter, _ *http.Request) bool { _, _ = io.WriteString(w, body); return true }
			ts := newSource(t, srv, newClock())
			tok, err := ts.Token(context.Background())
			var te *TokenError
			if err == nil || tok != "" || errors.As(err, &te) {
				t.Fatalf("tok=%q err=%v", tok, err)
			}
			assertNoSecret(t, err)
			// Failures are not cached: the next call retries.
			_, _ = ts.Token(context.Background())
			if srv.count.Load() != 2 {
				t.Fatalf("error was cached, count=%d", srv.count.Load())
			}
		})
	}
}

func TestTransportErrorHidesSecret(t *testing.T) {
	failing := doerFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed; header was " + r.Header.Get("Authorization") + " " + testSecret)
	})
	ts, err := New(Config{TokenURL: "https://idp.example.test/token", ClientID: testID, ClientSecret: testSecret, Client: failing, Now: newClock().Now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ts.Token(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	assertNoSecret(t, err)
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestRedactionAndValidation(t *testing.T) {
	cfg := Config{TokenURL: "https://idp.example.test/token", ClientID: testID, ClientSecret: testSecret}
	ts, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{fmt.Sprint(ts), fmt.Sprintf("%v %+v %#v %s", ts, ts, ts, ts), fmt.Sprintf("%v %+v %#v", cfg, cfg, cfg)} {
		if strings.Contains(s, testSecret) || !strings.Contains(s, redacted) {
			t.Fatalf("not redacted: %s", s)
		}
	}
	for _, bad := range []Config{
		{TokenURL: "ftp://x/token", ClientID: "a", ClientSecret: "b"},
		{TokenURL: "https://u:p@x/token", ClientID: "a", ClientSecret: "b"},
		{TokenURL: "https://x/token", ClientSecret: "b"},
		{TokenURL: "https://x/token", ClientID: "a"},
	} {
		if _, err := New(bad); err == nil {
			t.Fatalf("accepted %v", bad)
		} else {
			assertNoSecret(t, err)
		}
	}
}

func assertNoSecret(t *testing.T, err error) {
	t.Helper()
	enc := url.QueryEscape(testSecret)
	for _, s := range []string{err.Error(), fmt.Sprintf("%#v", err), fmt.Sprintf("%+v", err)} {
		if strings.Contains(s, testSecret) || strings.Contains(s, enc) || strings.Contains(s, "Basic ") {
			t.Fatalf("secret leaked: %s", s)
		}
	}
}

func TestTokenEndpointRedirectIsNotFollowed(t *testing.T) {
	var elsewhere atomic.Int64
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	defer other.Close()
	srv := newTokenServer(t, 300)
	srv.handler = func(w http.ResponseWriter, r *http.Request) bool {
		http.Redirect(w, r, other.URL+"/steal", http.StatusTemporaryRedirect)
		return true
	}
	// The caller's client follows redirects by default; New must neutralise it.
	ts, err := New(Config{TokenURL: srv.URL + "/token", ClientID: testID, ClientSecret: testSecret, Client: &http.Client{}, Now: newClock().Now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ts.Token(context.Background())
	var te *TokenError
	if !errors.As(err, &te) || te.Status != http.StatusTemporaryRedirect {
		t.Fatalf("err=%v", err)
	}
	if elsewhere.Load() != 0 {
		t.Fatal("credentials were re-posted to the redirect target")
	}
}
