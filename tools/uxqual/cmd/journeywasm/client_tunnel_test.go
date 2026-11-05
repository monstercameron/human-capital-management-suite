package main

import "testing"

// TestTodo_TUNNEL_HTTP_BASE pins the one TunnelURL prelude every REST client
// shares: ws(s) becomes http(s), http(s) passes through, and the two failure
// modes stay distinguishable so voice keeps refusing them differently.
func TestTodo_TUNNEL_HTTP_BASE(t *testing.T) {
	for _, test := range []struct {
		raw    string
		scheme string
	}{
		{"ws://cell.example/grpc", "http"},
		{"wss://cell.example/grpc", "https"},
		{"http://cell.example/api", "http"},
		{"https://cell.example:8443/api", "https"},
	} {
		endpoint, problem := tunnelHTTPBase(test.raw)
		if problem != tunnelOK || endpoint == nil || endpoint.Scheme != test.scheme || endpoint.Host == "" {
			t.Fatalf("tunnelHTTPBase(%q) = %v, %v, want scheme %q with a host", test.raw, endpoint, problem, test.scheme)
		}
	}
	for _, raw := range []string{"", "http:///no-host", "://missing-scheme", "http://[::1]:namedport"} {
		if endpoint, problem := tunnelHTTPBase(raw); problem != tunnelUnusable || endpoint != nil {
			t.Fatalf("tunnelHTTPBase(%q) = %v, %v, want (nil, tunnelUnusable)", raw, endpoint, problem)
		}
	}
	for _, raw := range []string{"ftp://cell.example/x", "file://cell.example/etc/hosts", "grpc://cell.example/x"} {
		if endpoint, problem := tunnelHTTPBase(raw); problem != tunnelBadScheme || endpoint != nil {
			t.Fatalf("tunnelHTTPBase(%q) = %v, %v, want (nil, tunnelBadScheme)", raw, endpoint, problem)
		}
	}
	// The caller owns path, query and fragment from here: the helper maps the
	// scheme and leaves the rest untouched.
	endpoint, problem := tunnelHTTPBase("ws://cell.example/tunnel?secret=discard#frag")
	if problem != tunnelOK || endpoint.Path != "/tunnel" || endpoint.RawQuery != "secret=discard" || endpoint.Fragment != "frag" {
		t.Fatalf("tunnelHTTPBase kept %v, %v, want the address untouched but for the scheme", endpoint, problem)
	}
}
