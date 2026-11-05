package agentmodel

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestNormalizeAPIEndpoint(t *testing.T) {
	const fallback = "https://api.example.com/v1"
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "https", raw: "https://api.example.com/v1", want: "https://api.example.com/v1"},
		{name: "trailing slash trimmed", raw: "https://api.example.com/v1//", want: "https://api.example.com/v1"},
		{name: "empty uses fallback", raw: "", want: fallback},
		{name: "http loopback", raw: "http://127.0.0.1:8080/v1", want: "http://127.0.0.1:8080/v1"},
		{name: "http ipv6 loopback", raw: "http://[::1]/v1", want: "http://[::1]/v1"},
		{name: "remote http refused", raw: "http://example.com", wantErr: true},
		{name: "http hostname refused", raw: "http://localhost/v1", wantErr: true},
		{name: "userinfo refused", raw: "https://user:pass@example.com", wantErr: true},
		{name: "query refused", raw: "https://example.com/v1?key=x", wantErr: true},
		{name: "fragment refused", raw: "https://example.com/v1#key", wantErr: true},
		{name: "missing host refused", raw: "https:///v1", wantErr: true},
		{name: "unparseable refused", raw: "https://[::1", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeAPIEndpoint(tc.raw, fallback)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidEndpoint) {
					t.Fatalf("NormalizeAPIEndpoint(%q) error = %v", tc.raw, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("NormalizeAPIEndpoint(%q) = %q, %v; want %q", tc.raw, got, err, tc.want)
			}
		})
	}
}

func TestNoRedirectClient(t *testing.T) {
	if client := NoRedirectClient(nil); client == nil || client.CheckRedirect == nil {
		t.Fatal("nil client did not produce a redirect-pinned client")
	} else if err := client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect was not refused: %v", err)
	}
	original := &http.Client{Timeout: time.Second}
	client := NoRedirectClient(original)
	if client == original || client.Timeout != time.Second {
		t.Fatal("client was not copied with its settings")
	}
	original.Timeout = 0
	if client.Timeout != time.Second {
		t.Fatal("client shares state with the caller's client")
	}
	if err := client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect was not refused: %v", err)
	}
}
