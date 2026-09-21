package egress

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

func TestNew_RejectsInvalidGatewayConfiguration(t *testing.T) {
	policy, err := outbound.NewPolicy(outbound.Destination{Name: "api.example.test", TrustBundleRef: "bundle:v1", Purposes: []string{"promotion.read"}, DataClasses: []string{"PUBLIC"}})
	if err != nil {
		t.Fatal(err)
	}
	dlpPolicy, err := dlp.NewPolicy(policy, dlp.Clearance{Destination: "api.example.test", DataClass: dlp.ClassPublic, Decision: dlp.Allow})
	if err != nil {
		t.Fatal(err)
	}
	validProxy, _ := url.Parse("http://proxy.example.test")
	for name, cfg := range map[string]Config{
		"missing outbound":   {DLP: dlpPolicy, ProxyURL: validProxy},
		"missing dlp":        {Outbound: policy, ProxyURL: validProxy},
		"missing proxy":      {Outbound: policy, DLP: dlpPolicy},
		"bad proxy scheme":   {Outbound: policy, DLP: dlpPolicy, ProxyURL: mustURL(t, "ftp://proxy.example.test")},
		"missing proxy host": {Outbound: policy, DLP: dlpPolicy, ProxyURL: mustURL(t, "http:")},
		"negative redirects": {Outbound: policy, DLP: dlpPolicy, ProxyURL: validProxy, MaxRedirects: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(cfg); !errors.Is(err, ErrInvalidGateway) {
				t.Fatalf("New err = %v, want ErrInvalidGateway", err)
			}
		})
	}
}

func TestGateway_Do_RejectsMalformedAndUnresolvableTargets(t *testing.T) {
	transport := &roundTripper{fn: okResponse}
	gateway := egressGateway(t, egressResolver{"api.example.test": {netip.MustParseAddr("192.0.2.10")}}, transport)
	for name, in := range map[string]Request{
		"empty method":     {Target: "https://api.example.test", Purpose: "promotion.read", Principal: "worker"},
		"malformed target": {Method: http.MethodGet, Target: "https://%gh", Purpose: "promotion.read", Principal: "worker"},
		"dns failure":      {Method: http.MethodGet, Target: "https://unknown.example.test", Purpose: "promotion.read", Principal: "worker"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := gateway.Do(context.Background(), in)
			if err == nil {
				t.Fatal("malformed or unresolvable target was accepted")
			}
		})
	}
	if _, err := (*Gateway)(nil).Do(context.Background(), Request{}); !errors.Is(err, ErrInvalidGateway) {
		t.Fatalf("nil gateway err = %v, want ErrInvalidGateway", err)
	}
	_, err := gateway.Do(context.Background(), Request{Method: http.MethodGet, Target: "https://api.example.test", Purpose: "promotion.read", Principal: "worker", Headers: http.Header{"Forwarded": []string{"for=127.0.0.1"}}})
	if !errors.Is(err, ErrProxyBypass) {
		t.Fatal("forwarded header was not refused")
	}
}

func TestGateway_Do_HandlesRedirectLimitMalformedLocationAndPostDowngrade(t *testing.T) {
	resolver := egressResolver{"api.example.test": {netip.MustParseAddr("192.0.2.10")}}
	t.Run("redirect limit", func(t *testing.T) {
		transport := &roundTripper{fn: func(req *http.Request) *http.Response {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://api.example.test/next"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}
		}}
		gateway := egressGateway(t, resolver, transport)
		gateway.maxRedirects = 1
		_, err := gateway.Do(context.Background(), Request{Method: http.MethodGet, Target: "https://api.example.test", Purpose: "promotion.read", Principal: "worker"})
		if !errors.Is(err, ErrUnsafeRedirect) || transport.callCount() != 2 {
			t.Fatalf("redirect limit result calls=%d err=%v", transport.callCount(), err)
		}
	})
	t.Run("malformed location", func(t *testing.T) {
		transport := &roundTripper{fn: func(req *http.Request) *http.Response {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"://bad"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}
		}}
		gateway := egressGateway(t, resolver, transport)
		_, err := gateway.Do(context.Background(), Request{Method: http.MethodGet, Target: "https://api.example.test", Purpose: "promotion.read", Principal: "worker"})
		if !errors.Is(err, ErrUnsafeRedirect) || transport.callCount() != 1 {
			t.Fatalf("malformed location calls=%d err=%v", transport.callCount(), err)
		}
	})
	t.Run("post becomes get", func(t *testing.T) {
		calls := 0
		transport := &roundTripper{fn: func(req *http.Request) *http.Response {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusSeeOther, Header: http.Header{"Location": []string{"https://api.example.test/next"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}
			}
			return okResponse(req)
		}}
		gateway := egressGateway(t, resolver, transport)
		result, err := gateway.Do(context.Background(), Request{Method: http.MethodPost, Target: "https://api.example.test", Purpose: "promotion.read", Principal: "worker", Payload: []byte("body")})
		last := transport.lastRequest()
		if err != nil || result.Response == nil || transport.callCount() != 2 || last == nil || last.Method != http.MethodGet {
			lastMethod := ""
			if last != nil {
				lastMethod = last.Method
			}
			t.Fatalf("post redirect result=%+v calls=%d last=%s err=%v", result, transport.callCount(), lastMethod, err)
		}
	})
}

func TestUnsafeAddress_RejectsAllReservedClasses(t *testing.T) {
	for _, address := range []netip.Addr{
		netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("169.254.169.254"),
		netip.MustParseAddr("100.100.100.200"), netip.MustParseAddr("224.0.0.1"), netip.MustParseAddr("0.0.0.0"),
	} {
		if !unsafeAddress(address) {
			t.Fatalf("reserved address %s was considered safe", address)
		}
	}
	if unsafeAddress(netip.MustParseAddr("192.0.2.10")) {
		t.Fatal("documentation public address was considered unsafe")
	}
}

func TestExplain_IsBoundedAndStable(t *testing.T) {
	if got := Explain(); got == "" || !strings.Contains(got, "centralized egress") {
		t.Fatalf("Explain = %q", got)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
