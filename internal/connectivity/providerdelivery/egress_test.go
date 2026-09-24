package providerdelivery

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/oauthcc"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

type providerResolver map[string][]netip.Addr

func (r providerResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return r[host], nil
}

type providerRoundTripper func(*http.Request) (*http.Response, error)

func (f providerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func newProviderGateway(t *testing.T, classes []string, purposes []string, roundTripper http.RoundTripper) *egress.Gateway {
	t.Helper()
	trust, err := outbound.NewPolicy(outbound.Destination{
		Name: "payroll.example.test", TrustBundleRef: "bundle:payroll:v1", Purposes: purposes, DataClasses: classes,
	})
	if err != nil {
		t.Fatal(err)
	}
	clearances := make([]dlp.Clearance, 0, len(classes))
	for _, class := range classes {
		clearances = append(clearances, dlp.Clearance{Destination: "payroll.example.test", DataClass: dlp.DataClass(class), Decision: dlp.Allow})
	}
	policy, err := dlp.NewPolicy(trust, clearances...)
	if err != nil {
		t.Fatal(err)
	}
	proxy, _ := url.Parse("http://egress-proxy.example.test:8080")
	gateway, err := egress.New(egress.Config{
		Outbound: trust, DLP: policy, ProxyURL: proxy,
		Resolver: providerResolver{"payroll.example.test": {netip.MustParseAddr("203.0.113.10")}}, Transport: roundTripper,
	})
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func TestTodo_REV_033_02(t *testing.T) {
	var called bool
	gateway := newProviderGateway(t, []string{string(dlp.ClassPII), string(dlp.ClassCompensation)}, []string{string(PurposePayrollDelivery)}, providerRoundTripper(func(req *http.Request) (*http.Response, error) {
		called = true
		if req.Header.Get(APIKeyHeader) != testAPIKey {
			t.Fatalf("provider auth header missing: %v", req.Header)
		}
		body, _ := io.ReadAll(req.Body)
		if !strings.Contains(string(body), `"160000.00"`) {
			t.Fatalf("provider body missing compensation: %s", body)
		}
		return &http.Response{StatusCode: http.StatusAccepted, Status: "202 Accepted", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"provider_ref":"P-1"}`)), Request: req}, nil
	}))
	client, err := NewPayrollClient(PayrollConfig{BaseURL: "https://payroll.example.test", APIKey: testAPIKey, Gateway: gateway, Principal: "workload/payroll-dispatcher", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL)
	if err != nil || got.Outcome != Delivered || !called {
		t.Fatalf("gateway provider delivery failed: called=%t result=%+v err=%v", called, got, err)
	}
}

func TestTodo_REV_033_02_Security(t *testing.T) {
	if _, err := NewPayrollClient(PayrollConfig{BaseURL: "https://payroll.example.test", APIKey: testAPIKey}); err == nil {
		t.Fatal("payroll adapter constructed without Gateway or injected HTTP port")
	}
	if _, err := NewAccessClient(AccessConfig{BaseURL: "https://iam.example.test", Tokens: &oauthcc.TokenSource{}}); err == nil {
		t.Fatal("access adapter constructed without Gateway or injected HTTP port")
	}
	var calls int
	gateway := newProviderGateway(t, []string{string(dlp.ClassPII)}, []string{string(PurposePayrollDelivery)}, providerRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusAccepted, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
	}))
	client, err := NewPayrollClient(PayrollConfig{BaseURL: "https://payroll.example.test", APIKey: testAPIKey, Gateway: gateway, Principal: "workload/payroll-dispatcher", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL)
	if err != nil || result.Outcome != Retry || calls != 0 {
		t.Fatalf("uncleared compensation did not fail closed: calls=%d result=%+v err=%v", calls, result, err)
	}
	if _, err := NewPayrollClient(PayrollConfig{BaseURL: "https://payroll.example.test", APIKey: testAPIKey, Gateway: gateway, Tenant: "acme"}); err == nil {
		t.Fatal("gateway accepted missing workload principal")
	}
	if _, err := NewPayrollClient(PayrollConfig{BaseURL: "https://payroll.example.test", APIKey: testAPIKey, Gateway: gateway, Principal: "workload/payroll-dispatcher"}); err == nil {
		t.Fatal("gateway accepted missing tenant identity")
	}
	if _, err := egress.NewHTTPDoer(gateway, string(PurposePayrollDelivery), "workload/payroll-dispatcher", "acme", []dlp.DataClass{dlp.ClassCompensation}, &lease.CredentialLease{Tenant: "other", Purpose: string(PurposePayrollDelivery)}); err == nil {
		t.Fatal("gateway accepted a lease from another tenant")
	}
	if PurposePayrollDelivery != "payroll_processing" || PurposeAccessChangeDelivery != "access_change_delivery" {
		t.Fatalf("provider purposes changed unexpectedly: payroll=%q access=%q", PurposePayrollDelivery, PurposeAccessChangeDelivery)
	}
}

func TestTodo_REV_033_02_Integration(t *testing.T) {
	var calls int
	gateway := newProviderGateway(t, []string{string(dlp.ClassPII), string(dlp.ClassCompensation)}, []string{string(PurposePayrollDelivery)}, providerRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusAccepted, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"provider_ref":"P-INT"}`)), Request: req}, nil
	}))
	client, err := NewPayrollClient(PayrollConfig{BaseURL: "https://payroll.example.test", APIKey: testAPIKey, Gateway: gateway, Principal: "worker/tenant-acme/payroll-dispatch", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("gateway did not issue exactly one provider call: %d", calls)
	}

	calls = 0
	redirectGateway := newProviderGateway(t, []string{string(dlp.ClassPII), string(dlp.ClassCompensation)}, []string{string(PurposePayrollDelivery)}, providerRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://payroll.example.test/other"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}))
	redirectClient, err := NewPayrollClient(PayrollConfig{BaseURL: "https://payroll.example.test", APIKey: testAPIKey, Gateway: redirectGateway, Principal: "worker/tenant-acme/payroll-dispatch", Tenant: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	redirectResult, err := redirectClient.Deliver(context.Background(), "chg-1", []byte(payrollPayload), callbackURL)
	if err != nil || redirectResult.Class != ClassTransientStatus || redirectResult.Reason != "redirect_not_followed" || calls != 1 {
		t.Fatalf("provider redirect was followed or misclassified: calls=%d result=%+v err=%v", calls, redirectResult, err)
	}
}
