package agentmodel

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ErrInvalidEndpoint marks a provider base URL that does not name a bare
// https authority, or http on a loopback host.
var ErrInvalidEndpoint = errors.New("agentmodel: invalid API base URL")

// NormalizeAPIEndpoint trims and defaults a provider base URL, then verifies
// it names a bare https authority, or http on a loopback host for tests and
// local development. Userinfo, query strings and fragments are refused.
func NormalizeAPIEndpoint(raw, fallback string) (string, error) {
	base := strings.TrimRight(raw, "/")
	if base == "" {
		base = fallback
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && !(parsed.Scheme == "http" && IsLoopbackHost(parsed.Hostname()))) {
		return "", ErrInvalidEndpoint
	}
	return base, nil
}

// IsLoopbackHost reports whether host parses as a loopback IP address.
func IsLoopbackHost(host string) bool {
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// NoRedirectClient copies client (or starts a fresh one for nil) and pins it
// to never follow redirects, so a credential is never carried to another
// authority.
func NoRedirectClient(client *http.Client) *http.Client {
	out := &http.Client{}
	if client != nil {
		copied := *client
		out = &copied
	}
	out.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return out
}
