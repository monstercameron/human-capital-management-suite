package egress

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

type egressResolver map[string][]netip.Addr

func (r egressResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	addresses, ok := r[host]
	if !ok {
		return nil, errors.New("not found")
	}
	return addresses, nil
}

type roundTripper struct {
	mu    sync.Mutex
	calls int
	last  *http.Request
	fn    func(*http.Request) *http.Response
}

func (r *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.calls++
	r.last = req
	r.mu.Unlock()
	return r.fn(req), nil
}

func (r *roundTripper) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *roundTripper) lastRequest() *http.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

func egressGateway(t *testing.T, resolver Resolver, transport http.RoundTripper) *Gateway {
	t.Helper()
	outboundPolicy, err := outbound.NewPolicy(outbound.Destination{Name: "api.example.test", TrustBundleRef: "bundle:v1", Purposes: []string{"promotion.read"}, DataClasses: []string{"PUBLIC", "PII"}})
	if err != nil {
		t.Fatal(err)
	}
	dlpPolicy, err := dlp.NewPolicy(outboundPolicy, dlp.Clearance{Destination: "api.example.test", DataClass: dlp.ClassPublic, Decision: dlp.Allow}, dlp.Clearance{Destination: "api.example.test", DataClass: dlp.ClassPII, Decision: dlp.Refuse})
	if err != nil {
		t.Fatal(err)
	}
	proxy, _ := url.Parse("http://egress-proxy.example.test:8080")
	gateway, err := New(Config{Outbound: outboundPolicy, DLP: dlpPolicy, ProxyURL: proxy, Resolver: resolver, Transport: transport, Now: func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func okResponse(req *http.Request) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: req}
}

func TestTodo_EDGE_005(t *testing.T) {
	transport := &roundTripper{fn: okResponse}
	gateway := egressGateway(t, egressResolver{"api.example.test": {netip.MustParseAddr("192.0.2.10")}}, transport)
	result, err := gateway.Do(context.Background(), Request{Method: http.MethodPost, Target: "https://api.example.test/v1", Purpose: "promotion.read", Principal: "worker/worker-1", Payload: []byte("safe"), DataClasses: []dlp.DataClass{dlp.ClassPublic}})
	if err != nil || result.Response == nil || transport.callCount() != 1 {
		t.Fatalf("allowed request failed: result=%+v calls=%d err=%v", result, transport.callCount(), err)
	}
	if len(result.Receipts) != 1 || result.Receipts[0].Decision != dlp.Allow {
		t.Fatalf("missing allow receipt: %+v", result.Receipts)
	}
	if err := gateway.receipts.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_EDGE_005_Security(t *testing.T) {
	addresses := map[string][]netip.Addr{
		"loopback.example.test": {netip.MustParseAddr("127.0.0.1")},
		"private.example.test":  {netip.MustParseAddr("10.0.0.7")},
		"metadata.example.test": {netip.MustParseAddr("169.254.169.254")},
		"public.example.test":   {netip.MustParseAddr("192.0.2.10")},
	}
	transport := &roundTripper{fn: okResponse}
	gateway := egressGateway(t, egressResolver(addresses), transport)
	for _, target := range []string{"https://loopback.example.test", "https://private.example.test", "https://metadata.example.test", "http://api.example.test", "https://api.example.test:8443"} {
		_, err := gateway.Do(context.Background(), Request{Method: http.MethodGet, Target: target, Purpose: "promotion.read", Principal: "worker/worker-1"})
		if err == nil {
			t.Fatalf("unsafe target was allowed: %s", target)
		}
	}
	if transport.callCount() != 0 {
		t.Fatalf("unsafe targets sent bytes: %d calls", transport.callCount())
	}
	_, err := gateway.Do(context.Background(), Request{Method: http.MethodGet, Target: "https://api.example.test", Purpose: "promotion.read", Principal: "worker/worker-1", Headers: http.Header{"X-Forwarded-For": []string{"127.0.0.1"}}})
	if !errors.Is(err, ErrProxyBypass) {
		t.Fatalf("proxy header was not refused: %v", err)
	}
}

func TestTodo_EDGE_005_Security_RedirectRevalidation(t *testing.T) {
	transport := &roundTripper{fn: func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusFound, Status: "302 Found", Header: http.Header{"Location": []string{"https://metadata.example.test/"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}
	}}
	gateway := egressGateway(t, egressResolver{"api.example.test": {netip.MustParseAddr("192.0.2.10")}, "metadata.example.test": {netip.MustParseAddr("169.254.169.254")}}, transport)
	_, err := gateway.Do(context.Background(), Request{Method: http.MethodGet, Target: "https://api.example.test", Purpose: "promotion.read", Principal: "worker/worker-1"})
	if !errors.Is(err, ErrUnsafeRedirect) || transport.callCount() != 1 {
		t.Fatalf("unsafe redirect was not revalidated: calls=%d err=%v", transport.callCount(), err)
	}
}

func TestTodo_EDGE_005_Security_DLP(t *testing.T) {
	transport := &roundTripper{fn: okResponse}
	gateway := egressGateway(t, egressResolver{"api.example.test": {netip.MustParseAddr("192.0.2.10")}}, transport)
	_, err := gateway.Do(context.Background(), Request{Method: http.MethodPost, Target: "https://api.example.test", Purpose: "promotion.read", Principal: "worker/worker-1", Payload: []byte("sensitive"), DataClasses: []dlp.DataClass{dlp.ClassPII}})
	if !errors.Is(err, ErrDLPRefused) || transport.callCount() != 0 {
		t.Fatalf("DLP refusal sent bytes: calls=%d err=%v", transport.callCount(), err)
	}
}

func FuzzTodo_EDGE_005(f *testing.F) {
	f.Add("https://api.example.test", "promotion.read", "worker/worker-1")
	f.Fuzz(func(t *testing.T, target, purpose, principal string) {
		transport := &roundTripper{fn: okResponse}
		gateway := egressGateway(t, egressResolver{"api.example.test": {netip.MustParseAddr("192.0.2.10")}}, transport)
		_, _ = gateway.Do(context.Background(), Request{Method: http.MethodGet, Target: target, Purpose: purpose, Principal: principal})
	})
}
