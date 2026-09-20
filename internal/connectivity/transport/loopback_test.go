package transport

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func loopbackTrust(allow bool) DestinationTrust {
	return DestinationTrust{
		Schemes:       []string{"http", "https"},
		Hosts:         []string{"127.0.0.1", "127.8.9.10", "::1", "10.0.0.4", "169.254.169.254", "0.0.0.0", "sim.local.test"},
		AllowLoopback: allow,
	}
}

func TestDestinationTrustRefusesLoopbackByDefault(t *testing.T) {
	trust := loopbackTrust(false)
	for _, raw := range []string{"http://127.0.0.1:8080/x", "http://127.8.9.10/x", "http://[::1]:9000/x"} {
		if err := trust.Validate(raw); err == nil {
			t.Fatalf("default trust admitted loopback %s", raw)
		}
	}
	err := trust.ValidateWithResolver(context.Background(), "http://sim.local.test/x", publicResolver(net.ParseIP("127.0.0.1")))
	if err == nil {
		t.Fatal("default trust admitted a hostname resolving to loopback")
	}
}

func TestDestinationTrustAllowLoopbackOptIn(t *testing.T) {
	trust := loopbackTrust(true)
	for _, raw := range []string{"http://127.0.0.1:8080/x", "http://127.8.9.10/x", "http://[::1]:9000/x"} {
		if err := trust.Validate(raw); err != nil {
			t.Fatalf("opt-in trust refused loopback %s: %v", raw, err)
		}
	}
	if err := trust.ValidateWithResolver(context.Background(), "http://sim.local.test/x", publicResolver(net.ParseIP("::1"))); err != nil {
		t.Fatalf("opt-in trust refused a hostname resolving to loopback: %v", err)
	}
	// The opt-in is loopback only: every other non-public class stays refused.
	for _, raw := range []string{"http://10.0.0.4/x", "http://169.254.169.254/x", "http://0.0.0.0/x"} {
		if err := trust.Validate(raw); err == nil {
			t.Fatalf("opt-in trust admitted non-loopback private %s", raw)
		}
	}
	if err := trust.ValidateWithResolver(context.Background(), "http://sim.local.test/x", publicResolver(net.ParseIP("10.0.0.4"))); err == nil {
		t.Fatal("opt-in trust admitted a hostname resolving to private space")
	}
	// The allowlist still applies to loopback addresses.
	if err := (DestinationTrust{Schemes: []string{"http"}, Hosts: []string{"example.test"}, AllowLoopback: true}).Validate("http://127.0.0.1/x"); err == nil {
		t.Fatal("loopback admitted without being on the host allowlist")
	}
}

func TestSafeHTTPClientDialsLoopbackOnlyWhenAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer srv.Close()
	for _, allow := range []bool{false, true} {
		trust := loopbackTrust(allow)
		c := NewREST(Client{Limits: Limits{Timeout: 5 * time.Second}, Trust: trust, HTTP: NewSafeHTTPClient(trust, nil)})
		r, err := c.Call(context.Background(), Request{Method: http.MethodGet, URL: srv.URL + "/x"})
		if allow && (err != nil || string(r.Body) != "ok") {
			t.Fatalf("allow=true: body=%q err=%v", r.Body, err)
		}
		if !allow && err == nil {
			t.Fatal("allow=false: loopback simulator was reachable")
		}
	}
}

// TestDestinationTrustLocalhostHostnameGap documents a known gap: plain
// Validate does not resolve names, so an allowlisted "localhost" passes it
// even without AllowLoopback. ValidateWithResolver closes the gap.
func TestDestinationTrustLocalhostHostnameGap(t *testing.T) {
	trust := DestinationTrust{Schemes: []string{"http"}, Hosts: []string{"localhost"}}
	if err := trust.Validate("http://localhost/x"); err != nil {
		t.Fatalf("Validate behaviour changed (gap may be fixed; update this test): %v", err)
	}
	if err := trust.ValidateWithResolver(context.Background(), "http://localhost/x", publicResolver(net.ParseIP("127.0.0.1"))); err == nil {
		t.Fatal("resolver-aware validation admitted localhost without AllowLoopback")
	}
}
