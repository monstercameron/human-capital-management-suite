// Package egress is the enforcing, centralized outbound gateway. Callers
// provide a destination, purpose, classification and reference lease; they
// never receive a direct socket or a path around DNS/address, TLS, proxy and
// DLP checks.
package egress

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

var (
	ErrInvalidGateway = errors.New("egress: invalid gateway configuration")
	ErrInvalidRequest = errors.New("egress: invalid request")
	ErrUnsafeAddress  = errors.New("egress: destination resolves to a disallowed address")
	ErrDNSResolution  = errors.New("egress: destination DNS resolution failed")
	ErrUnsafeRedirect = errors.New("egress: redirect failed destination revalidation")
	ErrProxyBypass    = errors.New("egress: request attempts to bypass the centralized proxy")
	ErrDLPRefused     = errors.New("egress: DLP refused the outbound request")
	ErrTLSRequired    = errors.New("egress: HTTPS and TLS are required")
	ErrUnsafeMethod   = errors.New("egress: HTTP method is not allowed through the gateway")
)

// Resolver is the small DNS port used for deterministic address validation.
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type systemResolver struct{}

func (systemResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, network, host)
}

// Config defines the enforcement dependencies. ProxyURL is mandatory even
// when Transport is a test double: the gateway must have an explicit central
// proxy boundary in every environment.
type Config struct {
	Outbound     *outbound.Policy
	DLP          *dlp.Policy
	Inspector    *dlp.Inspector
	ProxyURL     *url.URL
	Transport    http.RoundTripper
	Resolver     Resolver
	TLSConfig    *tls.Config
	Now          func() time.Time
	MaxRedirects int
	// AllowCONNECT opts into CONNECT tunneling. It defaults off: CONNECT
	// bypasses the repository-method guarantees, so enabling it is an
	// explicit, auditable gateway decision, and TRACE/TRACK stay refused.
	AllowCONNECT bool
}

type Gateway struct {
	outbound     *outbound.Policy
	dlp          *dlp.Policy
	inspector    *dlp.Inspector
	proxy        *url.URL
	transport    http.RoundTripper
	resolver     Resolver
	now          func() time.Time
	maxRedirects int
	allowCONNECT bool
	receipts     *dlp.ReceiptLog
}

// Request is one outbound effect. Payload is consumed for digest/inspection
// and is never retained by Gateway or the receipt log.
type Request struct {
	Method      string
	Target      string
	Purpose     string
	Principal   string
	Payload     []byte
	DataClasses []dlp.DataClass
	Lease       *lease.CredentialLease
	Headers     http.Header
}

type Result struct {
	Response *http.Response
	Receipts []dlp.EgressReceipt
}

func New(cfg Config) (*Gateway, error) {
	if cfg.Outbound == nil || cfg.DLP == nil || cfg.ProxyURL == nil || cfg.ProxyURL.Host == "" || (cfg.ProxyURL.Scheme != "http" && cfg.ProxyURL.Scheme != "https") {
		return nil, ErrInvalidGateway
	}
	if cfg.Resolver == nil {
		cfg.Resolver = systemResolver{}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxRedirects < 0 {
		return nil, ErrInvalidGateway
	}
	if cfg.MaxRedirects == 0 {
		cfg.MaxRedirects = 3
	}
	transport := cfg.Transport
	if transport == nil {
		transport = &http.Transport{Proxy: http.ProxyURL(cfg.ProxyURL), TLSClientConfig: cfg.TLSConfig}
	}
	return &Gateway{outbound: cfg.Outbound, dlp: cfg.DLP, inspector: cfg.Inspector, proxy: cfg.ProxyURL, transport: transport, resolver: cfg.Resolver, now: cfg.Now, maxRedirects: cfg.MaxRedirects, allowCONNECT: cfg.AllowCONNECT, receipts: dlp.NewReceiptLog()}, nil
}

// Do authorizes, resolves, and sends each hop through the configured proxy.
// Redirects are handled manually so every hop repeats the full validation.
func (g *Gateway) Do(ctx context.Context, in Request) (Result, error) {
	if g == nil {
		return Result{}, ErrInvalidGateway
	}
	if !allowedEgressMethod(in.Method, g.allowCONNECT) {
		return Result{}, fmt.Errorf("%w: %s", ErrUnsafeMethod, in.Method)
	}
	if err := validateRequest(in); err != nil {
		return Result{}, err
	}
	current, err := url.Parse(in.Target)
	if err != nil {
		return Result{}, fmt.Errorf("%w: target: %v", ErrInvalidRequest, err)
	}
	body := append([]byte(nil), in.Payload...)
	method := in.Method
	var receipts []dlp.EgressReceipt
	for redirects := 0; ; redirects++ {
		if err := validateURL(current); err != nil {
			if redirects > 0 {
				return Result{Receipts: receipts}, fmt.Errorf("%w: %v", ErrUnsafeRedirect, err)
			}
			return Result{Receipts: receipts}, err
		}
		addresses, err := g.resolve(ctx, current.Hostname())
		if err != nil {
			if redirects > 0 {
				return Result{Receipts: receipts}, fmt.Errorf("%w: %v", ErrUnsafeRedirect, err)
			}
			return Result{Receipts: receipts}, err
		}
		_ = addresses // resolution is the security gate; the configured proxy performs the network hop.
		receipt, err := g.authorize(in, current, body)
		if err != nil {
			if receipt.Digest != "" {
				receipts = append(receipts, receipt)
			}
			return Result{Receipts: receipts}, err
		}
		receipts = append(receipts, receipt)
		req, err := http.NewRequestWithContext(ctx, method, current.String(), bytes.NewReader(body))
		if err != nil {
			return Result{Receipts: receipts}, fmt.Errorf("%w: request: %v", ErrInvalidRequest, err)
		}
		req.Header = cloneHeaders(in.Headers)
		// Use the configured round tripper directly. http.Client parses Location
		// before returning even when CheckRedirect asks for the last response,
		// which would bypass this gateway's own per-hop redirect classification.
		resp, err := g.transport.RoundTrip(req)
		if err != nil {
			return Result{Receipts: receipts}, err
		}
		if resp.StatusCode < http.StatusMultipleChoices || resp.StatusCode >= http.StatusBadRequest || resp.Header.Get("Location") == "" {
			return Result{Response: resp, Receipts: receipts}, nil
		}
		if redirects >= g.maxRedirects {
			resp.Body.Close()
			return Result{Receipts: receipts}, fmt.Errorf("%w: redirect limit exceeded", ErrUnsafeRedirect)
		}
		location, err := url.Parse(resp.Header.Get("Location"))
		resp.Body.Close()
		if err != nil {
			return Result{Receipts: receipts}, fmt.Errorf("%w: malformed Location", ErrUnsafeRedirect)
		}
		next := current.ResolveReference(location)
		if method == http.MethodPost && (resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther) {
			method = http.MethodGet
			body = nil
		}
		current = next
	}
}

func (g *Gateway) authorize(in Request, target *url.URL, payload []byte) (dlp.EgressReceipt, error) {
	classes := append([]dlp.DataClass(nil), in.DataClasses...)
	if len(classes) == 0 {
		classes = []dlp.DataClass{dlp.ClassPublic}
	}
	inspection := dlp.Inspection{}
	var err error
	if g.inspector != nil {
		inspection, err = g.inspector.Inspect(payload)
		if err != nil {
			return dlp.EgressReceipt{}, err
		}
	}
	decision := dlp.Allow
	reason := ""
	for _, class := range classes {
		if _, err := g.outbound.Check(outbound.CheckRequest{Destination: target.Hostname(), Purpose: in.Purpose, DataClass: string(class), Lease: in.Lease}); err != nil {
			decision = dlp.Refuse
			reason = err.Error()
			break
		}
	}
	if decision == dlp.Allow {
		evaluation, evalErr := g.dlp.Evaluate(dlp.DecisionRequest{Destination: target.Hostname(), Purpose: in.Purpose, Principal: in.Principal, DeclaredClasses: classes, Inspection: inspection})
		if evalErr != nil {
			return dlp.EgressReceipt{}, evalErr
		}
		decision = evaluation.Decision
		reason = evaluation.Reason
		if decision != dlp.Allow {
			decision = dlp.Refuse
			if reason == "" {
				reason = "DLP policy requires transformation or approval"
			}
		}
	}
	receipt, err := g.receipts.Append(dlp.ReceiptInput{Destination: target.Hostname(), Purpose: in.Purpose, Principal: in.Principal, Payload: payload, Inspection: inspection, Decision: decision})
	if err != nil {
		return dlp.EgressReceipt{}, err
	}
	if decision != dlp.Allow {
		return receipt, fmt.Errorf("%w: %s", ErrDLPRefused, reason)
	}
	return receipt, nil
}

func (g *Gateway) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		if unsafeAddress(ip) {
			return nil, fmt.Errorf("%w: %s", ErrUnsafeAddress, ip)
		}
		return []netip.Addr{ip}, nil
	}
	answers, err := g.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(answers) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrDNSResolution, host)
	}
	for _, address := range answers {
		if unsafeAddress(address) {
			return nil, fmt.Errorf("%w: %s", ErrUnsafeAddress, address)
		}
	}
	return answers, nil
}

// allowedEgressMethod reports whether method may reach the proxy. Only
// the repository methods pass; CONNECT additionally requires the
// gateway's explicit opt-in, and TRACE, TRACK and anything else never
// pass, opt-in or not.
func allowedEgressMethod(method string, allowCONNECT bool) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead:
		return true
	case http.MethodConnect:
		return allowCONNECT
	default:
		return false
	}
}

func validateRequest(in Request) error {
	if in.Method == "" || in.Purpose == "" || in.Principal == "" || strings.TrimSpace(in.Method) != in.Method || strings.TrimSpace(in.Purpose) != in.Purpose || strings.TrimSpace(in.Principal) != in.Principal {
		return ErrInvalidRequest
	}
	for key := range in.Headers {
		lower := strings.ToLower(key)
		if lower == "proxy-authorization" || lower == "proxy-authenticate" || lower == "forwarded" || strings.HasPrefix(lower, "x-forwarded-") {
			return ErrProxyBypass
		}
	}
	return nil
}

func validateURL(target *url.URL) error {
	if target == nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil || target.Fragment != "" {
		return ErrTLSRequired
	}
	if target.Port() != "" && target.Port() != "443" {
		return fmt.Errorf("%w: only TLS port 443 is allowed", ErrTLSRequired)
	}
	return nil
}

func unsafeAddress(address netip.Addr) bool {
	return !address.IsValid() || address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() || address == netip.MustParseAddr("169.254.169.254") || address == netip.MustParseAddr("100.100.100.200")
}

func cloneHeaders(in http.Header) http.Header {
	out := make(http.Header, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

// Explain is safe for operator output and names the enforced boundary only.
func Explain() string {
	return "centralized egress: DNS/address, TLS, proxy, destination trust, DLP, and receipt enforcement"
}
