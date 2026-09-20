package transport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ErrRateLimited identifies a response that should be scheduled by the
// caller rather than retried immediately.
const ErrRateLimited ErrorKind = "RATE_LIMITED"

// ErrAmbiguous identifies a request whose outcome cannot be known locally,
// such as a timeout after bytes were sent.
const ErrAmbiguous ErrorKind = "AMBIGUOUS"

// DNSResolver is the smallest DNS surface an egress boundary needs. Keeping
// it injectable makes the security decision testable without making the
// transport package depend on a particular resolver implementation.
type DNSResolver interface {
	LookupIP(context.Context, string) ([]net.IP, error)
}

// ResolverFunc adapts a function to [DNSResolver].
type ResolverFunc func(context.Context, string) ([]net.IP, error)

// LookupIP implements [DNSResolver].
func (f ResolverFunc) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return f(ctx, host)
}

// SystemResolver uses the operating system resolver.
type SystemResolver struct{}

// LookupIP implements [DNSResolver].
func (SystemResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// ValidateWithResolver validates a destination and every address currently
// returned for its hostname. A hostname allowlist alone is insufficient: a
// trusted name can be rebound to loopback, link-local, metadata or private
// space between validation and dialing.
func (d DestinationTrust) ValidateWithResolver(ctx context.Context, raw string, resolver DNSResolver) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return errors.New("invalid destination")
	}
	if err := d.Validate(raw); err != nil {
		return err
	}
	_, err = d.resolvePublicHost(ctx, u.Hostname(), resolver)
	return err
}

func (d DestinationTrust) resolvePublicHost(ctx context.Context, host string, resolver DNSResolver) ([]net.IP, error) {
	if !containsFold(d.Hosts, host) {
		return nil, errors.New("destination is not trusted")
	}
	if ip := net.ParseIP(host); ip != nil {
		if d.unsafeIP(ip) {
			return nil, errors.New("private destination")
		}
		return []net.IP{ip}, nil
	}
	if resolver == nil {
		resolver = SystemResolver{}
	}
	ips, err := resolver.LookupIP(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("destination did not resolve")
	}
	for _, ip := range ips {
		if d.unsafeIP(ip) {
			return nil, errors.New("destination resolves to a private address")
		}
	}
	return ips, nil
}

// unsafeIP applies unsafeDestinationIP, except that loopback is admitted
// when the trust explicitly opts in with AllowLoopback.
func (d DestinationTrust) unsafeIP(ip net.IP) bool {
	if d.AllowLoopback && ip != nil && ip.IsLoopback() {
		return false
	}
	return unsafeDestinationIP(ip)
}

func unsafeDestinationIP(ip net.IP) bool {
	return ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}

// NewSafeHTTPClient creates a client whose redirects and DNS resolutions are
// checked by the same destination policy. The returned client is intended to
// be passed as [Client.HTTP]. It never follows a redirect to a destination
// that was not independently admitted.
func NewSafeHTTPClient(trust DestinationTrust, resolver DNSResolver) *http.Client {
	if resolver == nil {
		resolver = SystemResolver{}
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	clone := base.Clone()
	clone.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("destination address is malformed")
		}
		ips, err := trust.resolvePublicHost(ctx, host, resolver)
		if err != nil {
			return nil, err
		}
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	return &http.Client{
		Transport: clone,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return trust.ValidateWithResolver(req.Context(), req.URL.String(), resolver)
		},
	}
}

// WithDNSResolver returns a copy of c wired to the resolver-aware HTTP
// boundary. It is the convenience constructor for adapters that already use
// [Client] but must opt into DNS/IP-aware egress enforcement.
func (c Client) WithDNSResolver(resolver DNSResolver) Client {
	c.HTTP = NewSafeHTTPClient(c.Trust, resolver)
	return c
}

// NormalizeHTTPError converts a status into a stable, payload-free error.
// Provider response bodies are deliberately excluded because they frequently
// contain credentials, workforce values, or vendor diagnostics.
func NormalizeHTTPError(kind Kind, status int) *Error {
	var class ErrorKind
	switch {
	case status == http.StatusTooManyRequests:
		class = ErrRateLimited
	case status == http.StatusRequestTimeout || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout:
		class = ErrTransport
	case status >= 400:
		class = ErrProtocol
	default:
		class = ErrInvalid
	}
	return &Error{
		Kind:      class,
		Operation: kind,
		Status:    status,
		Cause:     errors.New("remote response classified by status"),
	}
}

// NormalizeTransportError retains only the stable classification of an
// error. It is suitable for logs and evidence where the provider's raw error
// string must not cross the transport boundary.
func NormalizeTransportError(err error) error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		return &Error{Kind: typed.Kind, Operation: typed.Operation, Status: typed.Status,
			Cause: errors.New("transport failure")}
	}
	return &Error{Kind: ErrTransport, Cause: errors.New("transport failure")}
}

// AmbiguousTransportError reports a timeout after a request may have reached
// the provider. The cause is intentionally fixed and does not include the
// provider's response or request bytes.
func AmbiguousTransportError(kind Kind) *Error {
	return &Error{Kind: ErrAmbiguous, Operation: kind, Cause: errors.New("request outcome is unknown")}
}

// FormatDestination is a bounded, credential-free destination description
// useful in diagnostics and tests.
func FormatDestination(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "<invalid>"
	}
	port := ""
	if p := u.Port(); p != "" {
		if n, convErr := strconv.Atoi(p); convErr == nil && n > 0 && n <= 65535 {
			port = ":" + strconv.Itoa(n)
		}
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Hostname()) + port
}

// Explain returns the transport security boundary without including an
// endpoint, credential, request body, or provider response.
func Explain() string {
	return "bounded transport: allowlisted schemes/hosts, resolver revalidation, bounded bytes, deadlines, redirects, and redacted errors"
}
