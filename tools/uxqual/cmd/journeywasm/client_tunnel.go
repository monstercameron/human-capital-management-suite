package main

import "net/url"

// tunnelProblem is what is wrong with a tunnel address, if anything. Most
// clients refuse both failure modes the same way, while voice tells them
// apart, so the shared prelude reports the problem instead of an error and
// each client maps it onto its own typed refusal.
type tunnelProblem int

const (
	tunnelOK tunnelProblem = iota
	// tunnelUnusable is an address that never names a host: unparsable or
	// hostless.
	tunnelUnusable
	// tunnelBadScheme parsed with a host but is not a ws(s)/http(s) address.
	tunnelBadScheme
)

// tunnelHTTPBase parses a journey tunnel address into the http(s) base URL
// for direct REST calls: ws becomes http and wss becomes https, while http
// and https pass through. Path, query and fragment are left untouched for
// the caller to set; the endpoint is nil unless the problem is tunnelOK.
//
// The bearer check stays with the caller: most clients refuse an empty
// bearer, the demo inbox and announcements refuse a blank one, and the
// persona chat routes carry no bearer precondition of their own.
func tunnelHTTPBase(raw string) (*url.URL, tunnelProblem) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Host == "" {
		return nil, tunnelUnusable
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return nil, tunnelBadScheme
	}
	return endpoint, tunnelOK
}
