package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Kind string

const (
	KindREST    Kind = "REST"
	KindSOAP    Kind = "SOAP"
	KindGraphQL Kind = "GRAPHQL"
	KindWebhook Kind = "WEBHOOK"
	KindSFTP    Kind = "SFTP"
	KindSCIM    Kind = "SCIM"
)

type ErrorKind string

const (
	ErrInvalid     ErrorKind = "INVALID"
	ErrDeadline    ErrorKind = "DEADLINE"
	ErrTooLarge    ErrorKind = "TOO_LARGE"
	ErrDestination ErrorKind = "DESTINATION"
	ErrTransport   ErrorKind = "TRANSPORT"
	ErrProtocol    ErrorKind = "PROTOCOL"
	ErrCanceled    ErrorKind = "CANCELED"
)

// Error is the stable, redacted error shape exposed by every façade.
type Error struct {
	Kind      ErrorKind
	Operation Kind
	Status    int
	Cause     error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause == nil {
		return string(e.Operation) + ": " + string(e.Kind)
	}
	return string(e.Operation) + ": " + string(e.Kind) + ": " + e.Cause.Error()
}
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type Limits struct {
	MaxRequestBytes, MaxResponseBytes int64
	Timeout                           time.Duration
}

func (l Limits) withDefaults() Limits {
	if l.MaxRequestBytes <= 0 {
		l.MaxRequestBytes = 1 << 20
	}
	if l.MaxResponseBytes <= 0 {
		l.MaxResponseBytes = 4 << 20
	}
	if l.Timeout <= 0 {
		l.Timeout = 30 * time.Second
	}
	return l
}

// DestinationTrust is an explicit outbound allowlist. An empty host allowlist
// is fail-closed; private and link-local literals are never accepted.
//
// AllowLoopback is an explicit opt-in for local development and test
// simulators: when true, loopback addresses (127.0.0.0/8 and ::1) are
// admitted, provided the host is also on the allowlist. Private, link-local,
// multicast and unspecified addresses stay refused either way. The zero value
// refuses loopback, so production configurations are unaffected.
type DestinationTrust struct {
	Schemes, Hosts []string
	AllowLoopback  bool
}

func (d DestinationTrust) Validate(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Hostname() == "" || u.User != nil {
		return errors.New("invalid destination")
	}
	if !containsFold(d.Schemes, u.Scheme) || !containsFold(d.Hosts, u.Hostname()) {
		return errors.New("destination is not trusted")
	}
	h := net.ParseIP(u.Hostname())
	if h != nil && (h.IsPrivate() || (h.IsLoopback() && !d.AllowLoopback) || h.IsLinkLocalUnicast() || h.IsUnspecified()) {
		return errors.New("private destination")
	}
	return nil
}
func containsFold(xs []string, s string) bool {
	for _, x := range xs {
		if strings.EqualFold(strings.TrimSpace(x), s) {
			return true
		}
	}
	return false
}

type Request struct {
	Method, URL   string
	Headers       map[string]string
	Body          []byte
	RedactHeaders []string
}
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	Kind   Kind
	Limits Limits
	Trust  DestinationTrust
	HTTP   Doer
}

func (c Client) Do(ctx context.Context, in Request) (Response, error) {
	var out Response
	lim := c.Limits.withDefaults()
	if ctx == nil {
		return out, c.fail(ErrInvalid, errors.New("context is required"))
	}
	if c.HTTP == nil {
		return out, c.fail(ErrTransport, errors.New("HTTP boundary is not configured"))
	}
	if err := c.Trust.Validate(in.URL); err != nil {
		return out, c.fail(ErrDestination, err)
	}
	if int64(len(in.Body)) > lim.MaxRequestBytes {
		return out, c.fail(ErrTooLarge, errors.New("request exceeds configured bound"))
	}
	if in.Method == "" {
		in.Method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, in.Method, in.URL, bytes.NewReader(in.Body))
	if err != nil {
		return out, c.fail(ErrInvalid, err)
	}
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}
	deadline := lim.Timeout
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < deadline {
		deadline = time.Until(dl)
	}
	if deadline <= 0 {
		return out, c.fail(ErrDeadline, context.DeadlineExceeded)
	}
	callCtx, cancel := context.WithTimeout(req.Context(), deadline)
	defer cancel()
	req = req.WithContext(callCtx)
	r, err := c.HTTP.Do(req)
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return out, c.fail(ErrDeadline, callCtx.Err())
		}
		if errors.Is(callCtx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
			return out, c.fail(ErrCanceled, context.Canceled)
		}
		return out, c.fail(ErrTransport, err)
	}
	defer r.Body.Close()
	b, err := readBounded(r.Body, lim.MaxResponseBytes)
	if err != nil {
		return out, c.fail(ErrTooLarge, err)
	}
	out = Response{StatusCode: r.StatusCode, Headers: r.Header.Clone(), Body: b}
	if r.StatusCode >= 400 {
		return out, &Error{Kind: ErrProtocol, Operation: c.Kind, Status: r.StatusCode, Cause: fmt.Errorf("remote status %d", r.StatusCode)}
	}
	return out, nil
}
func readBounded(r io.Reader, n int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, n+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > n {
		return nil, errors.New("response exceeds configured bound")
	}
	return b, nil
}
func (c Client) fail(k ErrorKind, e error) error { return &Error{Kind: k, Operation: c.Kind, Cause: e} }

// Redact returns a copy with sensitive header values removed before logging.
func Redact(in Request) Request {
	out := in
	out.Headers = map[string]string{}
	for k, v := range in.Headers {
		if strings.EqualFold(k, "authorization") || strings.EqualFold(k, "cookie") || containsFold(in.RedactHeaders, k) {
			out.Headers[k] = "[REDACTED]"
		} else {
			out.Headers[k] = v
		}
	}
	out.Body = nil
	return out
}

type REST struct{ Client }
type RESTAdapter = REST

func NewREST(c Client) REST                                          { c.Kind = KindREST; return REST{c} }
func NewRESTAdapter(c Client) REST                                   { return NewREST(c) }
func (a REST) Call(ctx context.Context, r Request) (Response, error) { return a.Do(ctx, r) }

type SOAP struct{ Client }
type SOAPAdapter = SOAP

func NewSOAP(c Client) SOAP                                          { c.Kind = KindSOAP; return SOAP{c} }
func NewSOAPAdapter(c Client) SOAP                                   { return NewSOAP(c) }
func (a SOAP) Call(ctx context.Context, r Request) (Response, error) { return a.Do(ctx, r) }

type GraphQL struct{ Client }
type GraphQLAdapter = GraphQL

func NewGraphQL(c Client) GraphQL                                       { c.Kind = KindGraphQL; return GraphQL{c} }
func NewGraphQLAdapter(c Client) GraphQL                                { return NewGraphQL(c) }
func (a GraphQL) Call(ctx context.Context, r Request) (Response, error) { return a.Do(ctx, r) }

type Webhook struct{ Client }
type WebhookAdapter = Webhook

func NewWebhook(c Client) Webhook                                          { c.Kind = KindWebhook; return Webhook{c} }
func NewWebhookAdapter(c Client) Webhook                                   { return NewWebhook(c) }
func (a Webhook) Deliver(ctx context.Context, r Request) (Response, error) { return a.Do(ctx, r) }

type SCIM struct{ Client }
type SCIMAdapter = SCIM

func NewSCIM(c Client) SCIM                                          { c.Kind = KindSCIM; return SCIM{c} }
func NewSCIMAdapter(c Client) SCIM                                   { return NewSCIM(c) }
func (a SCIM) Call(ctx context.Context, r Request) (Response, error) { return a.Do(ctx, r) }

// FileTransport is the narrow provider boundary for managed-file-transfer
// implementations. It intentionally has no Dial or net.Conn method.
type FileTransport interface {
	Put(context.Context, string, io.Reader) error
	Get(context.Context, string) (io.ReadCloser, error)
}
type SFTPAdapter struct {
	Limits Limits
	Trust  DestinationTrust
	Files  FileTransport
}

func NewSFTP(l Limits, trust DestinationTrust, files FileTransport) SFTPAdapter {
	return SFTPAdapter{Limits: l, Trust: trust, Files: files}
}
func (a SFTPAdapter) Put(ctx context.Context, path string, body []byte) error {
	lim := a.Limits.withDefaults()
	if ctx == nil {
		return &Error{Kind: ErrInvalid, Operation: KindSFTP, Cause: errors.New("context is required")}
	}
	if int64(len(body)) > lim.MaxRequestBytes {
		return &Error{Kind: ErrTooLarge, Operation: KindSFTP, Cause: errors.New("request exceeds configured bound")}
	}
	if a.Files == nil {
		return &Error{Kind: ErrInvalid, Operation: KindSFTP, Cause: errors.New("file transport is required")}
	}
	if err := a.Files.Put(ctx, path, bytes.NewReader(body)); err != nil {
		return &Error{Kind: ErrTransport, Operation: KindSFTP, Cause: err}
	}
	return nil
}
func (a SFTPAdapter) Get(ctx context.Context, path string) ([]byte, error) {
	lim := a.Limits.withDefaults()
	if ctx == nil {
		return nil, &Error{Kind: ErrInvalid, Operation: KindSFTP, Cause: errors.New("context is required")}
	}
	if a.Files == nil {
		return nil, &Error{Kind: ErrInvalid, Operation: KindSFTP, Cause: errors.New("file transport is required")}
	}
	r, err := a.Files.Get(ctx, path)
	if err != nil {
		return nil, &Error{Kind: ErrTransport, Operation: KindSFTP, Cause: err}
	}
	defer r.Close()
	b, err := readBounded(r, lim.MaxResponseBytes)
	if err != nil {
		return nil, &Error{Kind: ErrTooLarge, Operation: KindSFTP, Cause: err}
	}
	return b, nil
}

// TLSConfig returns a conservative TLS configuration for implementations that
// provide their own HTTP client; callers must still use Client for trust checks.
func TLSConfig(serverName string) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName, InsecureSkipVerify: false}
}
