package marketdata

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

type marketResolver map[string][]netip.Addr

func (r marketResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	addresses, ok := r[host]
	if !ok {
		return nil, errors.New("not found")
	}
	return addresses, nil
}

type marketTransport struct {
	calls int
	last  *http.Request
	fn    func(*http.Request) *http.Response
}

func (t *marketTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	t.last = req
	return t.fn(req), nil
}

func marketJSON(body string) func(*http.Request) *http.Response {
	return func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}
	}
}

func marketGateway(t *testing.T, transport http.RoundTripper) *egress.Gateway {
	t.Helper()
	outboundPolicy, err := outbound.NewPolicy(outbound.Destination{Name: "market.example.test", TrustBundleRef: "bundle:v1", Purposes: []string{Purpose}, DataClasses: []string{"PUBLIC"}})
	if err != nil {
		t.Fatal(err)
	}
	dlpPolicy, err := dlp.NewPolicy(outboundPolicy, dlp.Clearance{Destination: "market.example.test", DataClass: dlp.ClassPublic, Decision: dlp.Allow})
	if err != nil {
		t.Fatal(err)
	}
	proxy, _ := url.Parse("http://egress-proxy.example.test:8080")
	gateway, err := egress.New(egress.Config{Outbound: outboundPolicy, DLP: dlpPolicy, ProxyURL: proxy, Resolver: marketResolver{"market.example.test": {netip.MustParseAddr("192.0.2.20")}}, Transport: transport, Now: func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func marketQuery(t *testing.T) rewards.MarketQuery {
	t.Helper()
	asOf, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatal(err)
	}
	return rewards.MarketQuery{
		Tenant:   values.TenantId("harborcare-demo"),
		JobCode:  "CARE-CC3",
		Grade:    "P3",
		PayZone:  "US-EAST",
		Currency: "USD",
		AsOf:     asOf,
	}
}

func marketSource(t *testing.T, transport http.RoundTripper) *Source {
	t.Helper()
	source, err := New(Config{
		Gateway: marketGateway(t, transport),
		BaseURL: "https://market.example.test",
		Now:     func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

const stubWireAnchor = `{"p25":"74000.00","p50":"83000.00","p75":"92000.00","currency":"USD","as_of":"2026-01-01","source_version":"vendor.survey/2026.q1"}`

// TestTodo_HIPERF_001_Integration proves the HTTP skeleton speaks its wire
// contract through the real egress gateway: the query reaches the vendor
// path with the scope on it, a 200 becomes a validated record citing the
// vendor, and 404, 500, garbage and unordered legs each fail with the
// sentinel the governed lookup promises.
func TestTodo_HIPERF_001_Integration(t *testing.T) {
	ctx := context.Background()

	t.Run("200 becomes a validated vendor record", func(t *testing.T) {
		transport := &marketTransport{fn: marketJSON(stubWireAnchor)}
		source := marketSource(t, transport)
		record, err := source.LookupMarketRate(ctx, marketQuery(t))
		if err != nil {
			t.Fatalf("LookupMarketRate: %v", err)
		}
		if transport.calls != 1 {
			t.Fatalf("calls = %d, want 1", transport.calls)
		}
		target := transport.last.URL
		if !strings.HasSuffix(transport.last.URL.Path, "/v1/market-rate") {
			t.Fatalf("path = %s, want the market-rate path", transport.last.URL.Path)
		}
		for key, want := range map[string]string{"job_code": "CARE-CC3", "grade": "P3", "pay_zone": "US-EAST", "currency": "USD"} {
			if target.Query().Get(key) != want {
				t.Fatalf("query %s = %q, want %q", key, target.Query().Get(key), want)
			}
		}
		if record.Anchor.P50.String() != "83000.00 USD" {
			t.Fatalf("p50 = %s, want 83000.00 USD", record.Anchor.P50)
		}
		if record.SourceVersion != "vendor.survey/2026.q1" {
			t.Fatalf("source version = %q, want the wire version", record.SourceVersion)
		}
		if err := record.Validate("USD"); err != nil {
			t.Fatalf("vendor record does not validate: %v", err)
		}
		eval, err := rewards.LookupMarketRate(ctx, source, marketQuery(t))
		if err != nil {
			t.Fatalf("governed lookup over HTTP: %v", err)
		}
		if eval.ResultDigest == "" || eval.SourceVersion != record.SourceVersion {
			t.Fatalf("governed lookup does not cite the vendor record: %+v", eval)
		}
	})

	t.Run("404 is the typed miss", func(t *testing.T) {
		transport := &marketTransport{fn: func(req *http.Request) *http.Response {
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}
		}}
		_, err := marketSource(t, transport).LookupMarketRate(ctx, marketQuery(t))
		if !errors.Is(err, rewards.ErrMarketRateNotFound) {
			t.Fatalf("404 = %v, want ErrMarketRateNotFound", err)
		}
	})

	t.Run("500 is a source failure", func(t *testing.T) {
		transport := &marketTransport{fn: func(req *http.Request) *http.Response {
			return &http.Response{StatusCode: http.StatusBadGateway, Status: "502 Bad Gateway", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}
		}}
		_, err := marketSource(t, transport).LookupMarketRate(ctx, marketQuery(t))
		if !errors.Is(err, rewards.ErrMarketSourceFailed) {
			t.Fatalf("502 = %v, want ErrMarketSourceFailed", err)
		}
	})

	t.Run("garbage body is a source failure", func(t *testing.T) {
		transport := &marketTransport{fn: marketJSON("{not json")}
		_, err := marketSource(t, transport).LookupMarketRate(ctx, marketQuery(t))
		if !errors.Is(err, rewards.ErrMarketSourceFailed) {
			t.Fatalf("garbage = %v, want ErrMarketSourceFailed", err)
		}
	})

	t.Run("unordered legs are refused", func(t *testing.T) {
		transport := &marketTransport{fn: marketJSON(`{"p25":"92000.00","p50":"83000.00","p75":"74000.00","currency":"USD","as_of":"2026-01-01"}`)}
		_, err := marketSource(t, transport).LookupMarketRate(ctx, marketQuery(t))
		if !errors.Is(err, rewards.ErrMarketAnchorInvalid) {
			t.Fatalf("unordered = %v, want ErrMarketAnchorInvalid", err)
		}
	})

	t.Run("construction refuses the unconfigured states", func(t *testing.T) {
		if _, err := New(Config{BaseURL: "https://market.example.test"}); err == nil {
			t.Fatal("a sourceless skeleton was constructed")
		}
		if _, err := New(Config{Gateway: marketGateway(t, &marketTransport{fn: marketJSON(stubWireAnchor)})}); err == nil {
			t.Fatal("a URL-less skeleton was constructed")
		}
		if _, err := New(Config{Gateway: marketGateway(t, &marketTransport{fn: marketJSON(stubWireAnchor)}), BaseURL: "://bad"}); err == nil {
			t.Fatal("a bad-URL skeleton was constructed")
		}
		var nilSource *Source
		if _, err := nilSource.LookupMarketRate(ctx, marketQuery(t)); !errors.Is(err, rewards.ErrMarketQueryInvalid) {
			t.Fatalf("nil source = %v, want ErrMarketQueryInvalid", err)
		}
	})
}
