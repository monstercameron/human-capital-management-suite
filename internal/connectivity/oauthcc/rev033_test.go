package oauthcc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

type revResolver map[string][]netip.Addr

func (r revResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	addresses, ok := r[host]
	if !ok {
		return nil, errors.New("unmapped host")
	}
	return addresses, nil
}

type revTransport struct{}

func (revTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"token","token_type":"Bearer","expires_in":60}`)), Request: req}, nil
}

func revGateway(t *testing.T, address netip.Addr, clearance dlp.Decision) *egress.Gateway {
	t.Helper()
	outboundPolicy, err := outbound.NewPolicy(outbound.Destination{
		Name: "api.example.test", TrustBundleRef: "bundle:v1",
		Purposes: []string{"oauth_client_credentials"}, DataClasses: []string{string(dlp.ClassPII)},
	})
	if err != nil {
		t.Fatal(err)
	}
	dlpPolicy, err := dlp.NewPolicy(outboundPolicy, dlp.Clearance{Destination: "api.example.test", DataClass: dlp.ClassPII, Decision: clearance})
	if err != nil {
		t.Fatal(err)
	}
	proxy, _ := url.Parse("http://egress-proxy.example.test:8080")
	gateway, err := egress.New(egress.Config{
		Outbound: outboundPolicy, DLP: dlpPolicy, ProxyURL: proxy,
		Resolver: revResolver{"api.example.test": {address}}, Transport: revTransport{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func TestTodo_REV_033_02_NoDefaultClient(t *testing.T) {
	_, err := New(Config{TokenURL: "https://api.example.test/token", ClientID: "client", ClientSecret: "secret"})
	if err == nil || !strings.Contains(err.Error(), "Gateway or Client is required") {
		t.Fatalf("New without the enforcing or injected HTTP port error = %v", err)
	}
}

func TestTodo_REV_033_02_GatewayRefusals(t *testing.T) {
	base := Config{TokenURL: "https://api.example.test/token", ClientID: "client", ClientSecret: "secret", Principal: "workload/oauth", Tenant: "tenant-1"}
	for _, tc := range []struct {
		name      string
		gateway   *egress.Gateway
		wantError error
	}{
		{name: "unsafe DNS", gateway: revGateway(t, netip.MustParseAddr("127.0.0.1"), dlp.Allow), wantError: egress.ErrUnsafeAddress},
		{name: "TLS required", gateway: revGateway(t, netip.MustParseAddr("192.0.2.10"), dlp.Allow), wantError: egress.ErrTLSRequired},
		{name: "DLP refused", gateway: revGateway(t, netip.MustParseAddr("192.0.2.10"), dlp.Refuse), wantError: egress.ErrDLPRefused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			cfg.Gateway = tc.gateway
			if tc.name == "TLS required" {
				cfg.TokenURL = "http://api.example.test/token"
			}
			source, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest(http.MethodPost, cfg.TokenURL, strings.NewReader("grant_type=client_credentials"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = source.client.Do(req)
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("gateway error = %v; want %v", err, tc.wantError)
			}
		})
	}
}
