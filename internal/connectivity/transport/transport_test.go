package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type doer func(*http.Request) (*http.Response, error)

func (f doer) Do(r *http.Request) (*http.Response, error) { return f(r) }
func trusted() DestinationTrust {
	return DestinationTrust{Schemes: []string{"https"}, Hosts: []string{"api.example.test"}}
}
func base(d doer) REST {
	return NewREST(Client{Limits: Limits{MaxRequestBytes: 8, MaxResponseBytes: 8, Timeout: time.Second}, Trust: trusted(), HTTP: d})
}
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestTodo_CONN_RT_002(t *testing.T) {
	a := base(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() != "api.example.test" {
			t.Fatal(r.URL)
		}
		return response("ok"), nil
	})
	r, err := a.Call(context.Background(), Request{URL: "https://api.example.test/x", Body: []byte("hi")})
	if err != nil || string(r.Body) != "ok" {
		t.Fatalf("response=%q err=%v", r.Body, err)
	}
}

func TestTodo_REV_033_02_TransportNoDefaultClient(t *testing.T) {
	c := Client{Kind: KindREST, Trust: trusted()}
	_, err := c.Do(context.Background(), Request{URL: "https://api.example.test/v1"})
	var transportErr *Error
	if !errors.As(err, &transportErr) || transportErr.Kind != ErrTransport || transportErr.Cause == nil || transportErr.Cause.Error() != "HTTP boundary is not configured" {
		t.Fatalf("missing HTTP port error = %v, want fail-closed transport error", err)
	}
	if safe := c.WithDNSResolver(ResolverFunc(func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	})); safe.HTTP == nil {
		t.Fatal("resolver-aware safe client was not installed")
	}
}

func TestTodo_CONN_RT_002_Property(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  Request
		want ErrorKind
	}{
		{"untrusted", Request{URL: "https://evil.example/x"}, ErrDestination},
		{"oversize request", Request{URL: "https://api.example.test/x", Body: []byte("123456789")}, ErrTooLarge},
		{"oversize response", Request{URL: "https://api.example.test/x"}, ErrTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := doer(func(*http.Request) (*http.Response, error) { return response("123456789"), nil })
			e := base(d)
			_, err := e.Call(context.Background(), tc.req)
			var got *Error
			if !errors.As(err, &got) || got.Kind != tc.want {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
func TestTodo_CONN_RT_002_Fault(t *testing.T) {
	a := base(doer(func(*http.Request) (*http.Response, error) { return nil, errors.New("boom") }))
	_, err := a.Call(context.Background(), Request{URL: "https://api.example.test/x"})
	var e *Error
	if !errors.As(err, &e) || e.Kind != ErrTransport {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = a.Call(ctx, Request{URL: "https://api.example.test/x"})
	if !errors.As(err, &e) || e.Kind != ErrDeadline && e.Kind != ErrCanceled && e.Kind != ErrTransport {
		t.Fatal(err)
	}
}
func TestTodo_CONN_RT_002_Integration(t *testing.T) {
	for _, kind := range []Kind{KindREST, KindSOAP, KindGraphQL, KindWebhook, KindSCIM} {
		t.Run(string(kind), func(t *testing.T) {
			c := Client{Kind: kind, Limits: Limits{Timeout: time.Second}, Trust: trusted(), HTTP: doer(func(*http.Request) (*http.Response, error) { return response("ok"), nil })}
			if _, err := c.Do(context.Background(), Request{URL: "https://api.example.test/x"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func FuzzTodo_CONN_RT_002(f *testing.F) {
	f.Add("ok")
	f.Fuzz(func(t *testing.T, s string) {
		a := base(doer(func(*http.Request) (*http.Response, error) { return response(s), nil }))
		_, _ = a.Call(context.Background(), Request{URL: "https://api.example.test/x"})
	})
}

func TestRedact(t *testing.T) {
	r := Redact(Request{Headers: map[string]string{"Authorization": "secret", "X": "ok"}})
	if r.Headers["Authorization"] != "[REDACTED]" || r.Headers["X"] != "ok" || r.Body != nil {
		t.Fatal(r)
	}
}
