package kioskserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/timeclockapp"
)

const (
	pagePath = "/timeclock/"
	wasmPath = "/timeclock/assets/timeclock.wasm"
	execPath = "/timeclock/assets/wasm_exec.js"
	cssPath  = "/timeclock/assets/timeclock.css"
	maxBody  = 1 << 20
)

// Config describes the standalone kiosk mount. Upstream must be the already
// authenticated cell boundary; this package does not mint or invent device
// credentials.
type Config struct {
	// Upstream receives the public device calls after the same-origin prefix is
	// removed. It may be nil when only the shell/assets are being tested.
	Upstream http.Handler
	// UpstreamURL is an optional URL for a real HTTP upstream. Use either this
	// or Upstream, never both.
	UpstreamURL *url.URL
	WASM        []byte
	WASMExec    []byte
	CSS         string
}

// New returns a handler mounted at /timeclock/. The handler owns exactly the
// page, its three assets, and the four public device API methods.
func New(cfg Config) (http.Handler, error) {
	if cfg.Upstream != nil && cfg.UpstreamURL != nil {
		return nil, fmt.Errorf("kioskserver: configure Upstream or UpstreamURL, not both")
	}
	if cfg.CSS == "" {
		cfg.CSS = timeclockapp.KioskCSS
	}
	if cfg.UpstreamURL != nil {
		if err := validateUpstreamURL(cfg.UpstreamURL); err != nil {
			return nil, err
		}
		cfg.Upstream = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proxyRequest(cfg.UpstreamURL, w, r)
		})
	}
	return &server{cfg: cfg}, nil
}

type server struct{ cfg Config }

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	switch {
	case r.URL.Path == "/timeclock" || r.URL.Path == pagePath:
		s.page(w, r)
	case r.URL.Path == wasmPath:
		s.asset(w, r, s.cfg.WASM, "application/wasm")
	case r.URL.Path == execPath:
		s.asset(w, r, s.cfg.WASMExec, "text/javascript; charset=utf-8")
	case r.URL.Path == cssPath:
		s.asset(w, r, []byte(s.cfg.CSS), "text/css; charset=utf-8")
	case strings.HasPrefix(r.URL.Path, "/timeclock/hcmnext.time.v1.ClockDeviceService/"):
		s.device(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *server) page(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	loader := "window.addEventListener('load',()=>{const g=new Go();WebAssembly.instantiateStreaming(fetch('" + wasmPath + "'),g.importObject).then(x=>g.run(x.instance))})"
	hash := sha256.Sum256([]byte(loader))
	csp := "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; connect-src 'self'; style-src 'self'; script-src 'self' 'sha256-" + base64.StdEncoding.EncodeToString(hash[:]) + "' 'wasm-unsafe-eval'"
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The WASM component owns the semantic <main>; keeping the mount point a
	// neutral container avoids nested landmarks and ensures the loading copy is
	// replaced atomically on the first render.
	fallback := "<div id=app><p>Loading kiosk…</p></div>"
	body := "<!doctype html><html lang=en><head><meta charset=utf-8><meta name=viewport content=\"width=device-width,initial-scale=1\"><title>Time clock</title><link rel=stylesheet href=\"" + cssPath + "\"></head><body>" + fallback + "<script src=\"" + execPath + "\"></script><script>" + loader + "</script></body></html>"
	_, _ = io.WriteString(w, body)
}

func (s *server) asset(w http.ResponseWriter, r *http.Request, body []byte, contentType string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if len(body) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

func (s *server) device(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || s.cfg.Upstream == nil {
		http.NotFound(w, r)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/timeclock/hcmnext.time.v1.ClockDeviceService/")
	if !timeclockapp.IsDeviceMethod(method) || strings.Contains(method, "/") || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	if r.ContentLength > maxBody {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	// ContentLength is commonly absent for fetch requests. Read the bounded
	// body before forwarding so chunked requests cannot bypass the limit.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	clone := r.Clone(r.Context())
	clone.URL = cloneURL(r.URL, "/v1/time/clock-device/"+method)
	// Forward the server-verified credential context only. In particular, the
	// proxy never manufactures an X-* identity header from browser input.
	clone.Header = forwardedHeaders(r.Header)
	s.cfg.Upstream.ServeHTTP(w, clone)
}

func cloneURL(in *url.URL, path string) *url.URL {
	u := *in
	u.Path, u.RawPath = path, ""
	return &u
}

func proxyRequest(base *url.URL, w http.ResponseWriter, r *http.Request) {
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + r.URL.Path
	u.RawQuery = r.URL.RawQuery
	req := r.Clone(r.Context())
	req.Header = forwardedHeaders(r.Header)
	req.URL = &u
	req.RequestURI = ""
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "device service unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func validateUpstreamURL(u *url.URL) error {
	if u == nil || u.Host == "" {
		return fmt.Errorf("kioskserver: upstream URL must include a host")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme != "http" {
		return fmt.Errorf("kioskserver: upstream URL must use https")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if host == "localhost" {
		return nil
	}
	return fmt.Errorf("kioskserver: cleartext upstream must be loopback")
}

func forwardedHeaders(in http.Header) http.Header {
	out := make(http.Header)
	for _, key := range []string{"Authorization", "Cookie", "Content-Type", "Accept", "User-Agent", "Traceparent", "Tracestate"} {
		for _, value := range in.Values(key) {
			out.Add(key, value)
		}
	}
	return out
}
