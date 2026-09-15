package edge

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// BrowserCSRFCookieName is the SameSite-backed token used by the browser
// policy. It is HttpOnly because the policy relies on SameSite and Origin
// rather than asking page script to copy the token into a request header.
const BrowserCSRFCookieName = "hcmnext_browser_csrf"

const (
	browserCSRFCookiePath = "/"
	browserCSRFTTL        = 12 * time.Hour
	browserWorkspacePath  = "/workspace"
)

// BrowserPolicyOptions configures BrowserPolicy. Allowed origins are exact
// origins such as "https://hcm.example"; allowed hosts are exact Host header
// authorities such as "hcm.example" or "hcm.example:8443". Empty lists use
// same-origin checking for browser requests, while still allowing native API
// callers that do not send browser headers.
type BrowserPolicyOptions struct {
	AllowedOrigins []string
	AllowedHosts   []string

	// SecureCookies forces Secure on the browser token cookie. HTTPS requests
	// always receive Secure, even when this is false. The cell leaves this
	// false so its HTTP development listener remains usable.
	SecureCookies bool
}

// BrowserPolicy mounts the browser boundary around next. State-changing
// browser requests must carry an allowed Origin and Host, and a SameSite
// browser token. Redirect responses are checked before their headers reach
// the client, so a handler cannot accidentally create an open redirect.
// Refusals are canonical envelope errors projected through connect's error
// model, including for the non-RPC workspace routes.
func BrowserPolicy(next http.Handler, opts BrowserPolicyOptions) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	policy := browserPolicy{opts: opts}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			return
		}
		if ownedErr := policy.check(r); ownedErr != nil {
			writeBrowserPolicyError(w, r, ownedErr)
			return
		}

		if policy.issueCookie(r) {
			http.SetCookie(w, &http.Cookie{
				Name:     BrowserCSRFCookieName,
				Value:    browserToken(),
				Path:     browserCSRFCookiePath,
				HttpOnly: true,
				Secure:   policy.secure(r),
				SameSite: http.SameSiteStrictMode,
				MaxAge:   int(browserCSRFTTL / time.Second),
				Expires:  time.Now().Add(browserCSRFTTL),
			})
		}

		wrapped := &browserPolicyResponseWriter{
			ResponseWriter: w,
			request:        r,
			secure:         policy.secure(r),
		}
		next.ServeHTTP(wrapped, r)
	})
}

type browserPolicy struct {
	opts BrowserPolicyOptions
}

func (p browserPolicy) check(r *http.Request) *envelope.Error {
	if !isStateChangingMethod(r.Method) {
		return nil
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	_, cookieErr := r.Cookie(BrowserCSRFCookieName)
	browser := origin != "" || isWorkspacePath(r.URL.Path) || cookieErr == nil
	// Fetch Metadata is advisory here, never sufficient on its own: a browser
	// embedded under a foreign top-level document reports every navigation
	// as cross-site even when the page, its Origin and its cookies are all
	// this origin's own. A cross-site report is therefore refused only when
	// the request carries no Origin to check against; an Origin that matches
	// this host cannot be forged by another site, and the SameSite token
	// check below still applies. An opaque ("null") Origin is equally
	// uncheckable, so it takes the same refusal here.
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") && (origin == "" || origin == "null") {
		return browserRefusal("edge.browser_origin_rejected", "the request origin is not allowed", "origin", "browser_policy.cross_site")
	}

	// A caller that supplied browser policy configuration is always subject to
	// the Host allowlist. With no configured list, an Origin-bearing request
	// is still tied to the request Host below.
	if len(p.opts.AllowedHosts) > 0 && !containsExact(p.opts.AllowedHosts, r.Host) {
		return browserRefusal("edge.browser_host_rejected", "the request host is not allowed", "host", "browser_policy.host_allowlist")
	}
	// An opaque ("null") Origin is not checked against the allowlist: the
	// first-party documents this edge serves use Referrer-Policy:
	// no-referrer, so a real browser posts them back with Origin: null. The
	// SameSite browser token below remains the control proving the browser
	// received a token from this edge -- an opaque origin cannot carry
	// another site's cookies -- and routes with their own proof (the
	// workspace HMAC CSRF token) check it again behind this edge.
	if origin != "" && origin != "null" {
		if !p.originAllowed(origin, r) {
			return browserRefusal("edge.browser_origin_rejected", "the request origin is not allowed", "origin", "browser_policy.origin_allowlist")
		}
	}

	// No Origin is valid for a native client. A workspace POST, a request
	// carrying a browser cookie, or any request with an Origin is browser
	// traffic and must prove that the browser received a token from this edge.
	if browser {
		cookie, err := r.Cookie(BrowserCSRFCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			return browserRefusal("edge.browser_csrf_rejected", "the request does not carry a current browser token", "csrf_token", "browser_policy.csrf")
		}
	}
	return nil
}

func (p browserPolicy) originAllowed(origin string, r *http.Request) bool {
	parsed, err := parseOrigin(origin)
	if err != nil {
		return false
	}
	if len(p.opts.AllowedOrigins) > 0 {
		for _, allowed := range p.opts.AllowedOrigins {
			if canonicalOrigin(allowed) == parsed {
				return true
			}
		}
		return false
	}

	// With no explicit origin list, the only accepted browser origin is the
	// origin of this request. Comparing the scheme as well as the authority
	// prevents an HTTPS page from being treated as the HTTP origin.
	wantScheme := "http"
	if r.TLS != nil {
		wantScheme = "https"
	}
	return parsed == wantScheme+"://"+strings.ToLower(strings.TrimSpace(r.Host))
}

func (p browserPolicy) issueCookie(r *http.Request) bool {
	if !isWorkspacePath(r.URL.Path) || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}
	_, err := r.Cookie(BrowserCSRFCookieName)
	return err != nil
}

func (p browserPolicy) secure(r *http.Request) bool {
	return p.opts.SecureCookies || r.TLS != nil
}

func browserToken() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		// A process that cannot obtain randomness must not mint a predictable
		// browser proof. The empty value is rejected on the next state change.
		return ""
	}
	return hex.EncodeToString(raw)
}

func browserRefusal(reason, message, field, rule string) *envelope.Error {
	return envelope.New(envelope.CodePermissionDenied, reason, message).
		WithViolation(field, message, rule)
}

func writeBrowserPolicyError(w http.ResponseWriter, r *http.Request, ownedErr *envelope.Error) {
	if err := connect.NewErrorWriter().Write(w, r, ToConnectError(ownedErr)); err != nil {
		http.Error(w, ownedErr.Message(), ownedErr.HTTPStatus())
	}
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isWorkspacePath(path string) bool {
	return path == browserWorkspacePath || strings.HasPrefix(path, browserWorkspacePath+"/")
}

func containsExact(values []string, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return false
	}
	for _, candidate := range values {
		if strings.ToLower(strings.TrimSpace(candidate)) == value {
			return true
		}
	}
	return false
}

func parseOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", url.InvalidHostError(raw)
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

func canonicalOrigin(raw string) string {
	origin, err := parseOrigin(raw)
	if err != nil {
		return ""
	}
	return origin
}

// browserPolicyResponseWriter checks redirect destinations and normalizes
// security attributes on cookies emitted by workspace handlers.
type browserPolicyResponseWriter struct {
	http.ResponseWriter
	request *http.Request
	secure  bool
	wrote   bool
	blocked bool
}

func (w *browserPolicyResponseWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	if status >= http.StatusMultipleChoices && status < http.StatusBadRequest {
		location := w.Header().Get("Location")
		if location != "" && !sameOriginRelativeTarget(location) {
			w.wrote = true
			w.blocked = true
			w.Header().Del("Location")
			writeBrowserPolicyError(w.ResponseWriter, w.request,
				browserRefusal("edge.browser_redirect_rejected", "the redirect target is not same-origin", "redirect", "browser_policy.redirect"))
			return
		}
	}
	normalizeCookieHeaders(w.Header(), w.secure)
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *browserPolicyResponseWriter) Write(p []byte) (int, error) {
	if w.blocked {
		return len(p), nil
	}
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *browserPolicyResponseWriter) Flush() {
	if w.blocked {
		return
	}
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *browserPolicyResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

func (w *browserPolicyResponseWriter) Push(target string, opts *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

func (w *browserPolicyResponseWriter) ReadFrom(src io.Reader) (int64, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if w.blocked {
		return 0, nil
	}
	if reader, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return reader.ReadFrom(src)
	}
	return io.Copy(struct{ io.Writer }{w.ResponseWriter}, src)
}

func (w *browserPolicyResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func sameOriginRelativeTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" || strings.Contains(target, "\\") || strings.HasPrefix(target, "//") {
		return false
	}
	u, err := url.Parse(target)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil {
		return false
	}
	return strings.HasPrefix(target, "/") || !strings.Contains(target, ":")
}

func normalizeCookieHeaders(header http.Header, secure bool) {
	values := header.Values("Set-Cookie")
	if len(values) == 0 {
		return
	}
	for i, value := range values {
		name := strings.TrimSpace(strings.SplitN(value, "=", 2)[0])
		if !strings.HasPrefix(name, "hcmnext_") {
			continue
		}
		value = appendCookieAttribute(value, "HttpOnly")
		value = appendCookieAttribute(value, "SameSite=Strict")
		if secure {
			value = appendCookieAttribute(value, "Secure")
		}
		values[i] = value
	}
	header.Del("Set-Cookie")
	for _, value := range values {
		header.Add("Set-Cookie", value)
	}
}

func appendCookieAttribute(cookie, attribute string) string {
	needle := strings.ToLower(strings.SplitN(attribute, "=", 2)[0])
	for _, part := range strings.Split(cookie, ";") {
		if strings.ToLower(strings.TrimSpace(strings.SplitN(part, "=", 2)[0])) == needle {
			return cookie
		}
	}
	return cookie + "; " + attribute
}
