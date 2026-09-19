package workspace

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

const (
	shellIssuer   = "https://issuer.test.hcm-next.invalid"
	shellAudience = "hcm-next-api"
	shellTenant   = "acme-industries"
	shellSubject  = "user-journey"
	shellPurpose  = "compensation_review"
)

var shellSigningKey = []byte("hcm-next-journey-shell-signing-key-32+++")

var shellNow = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// unreachableCell is the Cell port for a route that never reads a worker. The
// journey shell touches no cell data at all - its client does, over the
// tunnel - so a port that fails loudly if it is called is the honest stub.
type unreachableCell struct{ t *testing.T }

func (c unreachableCell) ReadPromotion(context.Context, Request) (Reading, error) {
	c.t.Fatal("the journey shell must not read the cell")
	return Reading{}, errors.New("unreachable")
}

// newShellHandler builds a Handler and one credential it admits. An
// optional public origin composes the handler the way a TLS-terminating
// deployment would.
func newShellHandler(t *testing.T, devBrowserLogin bool, publicOrigin ...string) (*Handler, string) {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      shellSigningKey,
		Issuer:   shellIssuer,
		Audience: shellAudience,
		Now:      func() time.Time { return shellNow },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer:               shellIssuer,
		Audience:             shellAudience,
		Subject:              shellSubject,
		SubjectKind:          "human",
		Tenant:               shellTenant,
		OrganizationScopeID:  "org-north-america",
		Roles:                []string{"intent_author", "comp_admin"},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{shellPurpose},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-journey",
		IssuedAtUnix:         shellNow.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        shellNow.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}
	declared := ""
	if len(publicOrigin) > 0 {
		declared = publicOrigin[0]
	}
	h, err := NewHandler(Options{
		Cell: unreachableCell{t: t},
		Config: transport.Config{
			Verifier: verifier, Audience: shellAudience,
			Now: func() time.Time { return shellNow },
		},
		Now:             func() time.Time { return shellNow },
		DevBrowserLogin: devBrowserLogin,
		PublicOrigin:    declared,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h, token
}

// getJourney drives one GET of the journey shell.
func getJourney(t *testing.T, h *Handler, decorate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathJourney, nil)
	r.Host = "cell.test"
	if decorate != nil {
		decorate(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// island parses the config island out of a rendered shell.
func island(t *testing.T, body string) JourneyConfig {
	t.Helper()
	const open = `<script type="application/json" id="` + JourneyConfigElementID + `">`
	_, rest, found := strings.Cut(body, open)
	if !found {
		t.Fatalf("the shell carries no %q island", JourneyConfigElementID)
	}
	payload, _, terminated := strings.Cut(rest, "</script>")
	if !terminated {
		t.Fatal("the config island is not terminated")
	}
	var cfg JourneyConfig
	if err := json.Unmarshal([]byte(payload), &cfg); err != nil {
		t.Fatalf("the config island is not JSON: %v (%s)", err, payload)
	}
	return cfg
}

// ---------------------------------------------------------------------------
// Admission
// ---------------------------------------------------------------------------

func TestJourneyShellRefusesAnUnauthenticatedRequest(t *testing.T) {
	h, _ := newShellHandler(t, false)
	w := getJourney(t, h, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Fatalf("WWW-Authenticate = %q, want a Bearer challenge", got)
	}
	if strings.Contains(w.Body.String(), JourneyConfigElementID) {
		t.Fatal("a refused request must not be answered with a config island")
	}
}

func TestJourneyShellRefusesAnInvalidCredential(t *testing.T) {
	h, _ := newShellHandler(t, false)
	w := getJourney(t, h, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer not-a-credential")
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// ---------------------------------------------------------------------------
// The document
// ---------------------------------------------------------------------------

func TestJourneyShellRendersTheDocument(t *testing.T) {
	h, token := newShellHandler(t, false)
	w := getJourney(t, h, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"<!doctype html>",
		`<html lang="en">`,
		`<meta charset="utf-8">`,
		`<meta name="viewport"`,
		"<title>Promotion journey</title>",
		`<div id="` + JourneyRootElementID + `">`,
		"This page needs WebAssembly and JavaScript enabled",
		`<a href="` + PathPromotion + `">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the shell does not contain %q", want)
		}
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestJourneyShellIslandCarriesTheAdmittedCredential(t *testing.T) {
	h, token := newShellHandler(t, false)
	w := getJourney(t, h, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
	})
	cfg := island(t, w.Body.String())
	if cfg.Bearer != token {
		t.Fatalf("island bearer = %q, want the admitted token", cfg.Bearer)
	}
	if strings.HasPrefix(cfg.Bearer, "Bearer ") {
		t.Fatal("the island carries the token, not the whole header value")
	}
	if cfg.Tenant != shellTenant {
		t.Errorf("island tenant = %q, want %q", cfg.Tenant, shellTenant)
	}
	if cfg.Subject != shellSubject {
		t.Errorf("island subject = %q, want %q", cfg.Subject, shellSubject)
	}
	if cfg.Purpose != shellPurpose {
		t.Errorf("island purpose = %q, want %q", cfg.Purpose, shellPurpose)
	}
	if len(cfg.Roles) == 0 {
		t.Error("island roles are empty")
	}
	if cfg.JourneysPath != PathJourney {
		t.Errorf("island journeys_path = %q, want %q", cfg.JourneysPath, PathJourney)
	}
}

// TestJourneyShellIslandCarriesTheDevLoginCookie proves the shell hands the
// client whatever credential the request was actually admitted with, which
// for a browser signed in through the dev flow is the cookie's value.
func TestJourneyShellIslandCarriesTheDevLoginCookie(t *testing.T) {
	h, token := newShellHandler(t, true)
	w := getJourney(t, h, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: loginSessionCookie, Value: token})
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if cfg := island(t, w.Body.String()); cfg.Bearer != token {
		t.Fatalf("island bearer = %q, want the cookie's credential", cfg.Bearer)
	}
}

// TestJourneyShellIslandIgnoresTheCookieWithoutDevLogin is the same request
// against a cell that never turned the operator flag on: no admission, and so
// no island at all.
func TestJourneyShellIslandIgnoresTheCookieWithoutDevLogin(t *testing.T) {
	h, token := newShellHandler(t, false)
	w := getJourney(t, h, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: loginSessionCookie, Value: token})
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestJourneyShellTunnelURLFollowsTheRequest(t *testing.T) {
	h, token := newShellHandler(t, false)

	plain := getJourney(t, h, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
	})
	if got := island(t, plain.Body.String()).TunnelURL; got != "ws://cell.test"+PathTunnel {
		t.Errorf("tunnel_url = %q, want ws://cell.test%s", got, PathTunnel)
	}

	forwarded := getJourney(t, h, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Forwarded-Proto", "https, http")
	})
	if got := island(t, forwarded.Body.String()).TunnelURL; got != "wss://cell.test"+PathTunnel {
		t.Errorf("tunnel_url = %q, want wss://cell.test%s behind an https proxy", got, PathTunnel)
	}

	terminated := getJourney(t, h, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
		r.TLS = &tls.ConnectionState{}
	})
	if got := island(t, terminated.Body.String()).TunnelURL; got != "wss://cell.test"+PathTunnel {
		t.Errorf("tunnel_url = %q, want wss://cell.test%s over TLS", got, PathTunnel)
	}
}

func TestJourneyTunnelURLNilRequest(t *testing.T) {
	if got := JourneyTunnelURL(nil); got != "" {
		t.Fatalf("JourneyTunnelURL(nil) = %q, want the empty string", got)
	}
}

// TestJourneyShellBindsTheDeclaredPublicOrigin is the complex-deployment
// case: a proxy that terminates TLS and rewrites Host delivers the page
// request with an internal authority, so the tunnel address and the policy
// must come from the origin the deployment declared, not from the request.
func TestJourneyShellBindsTheDeclaredPublicOrigin(t *testing.T) {
	h, token := newShellHandler(t, false, "https://hcm.example.com")
	w := getJourney(t, h, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := island(t, w.Body.String()).TunnelURL; got != "wss://hcm.example.com"+PathTunnel {
		t.Errorf("tunnel_url = %q, want wss://hcm.example.com%s", got, PathTunnel)
	}
	if got := w.Header().Get("Content-Security-Policy"); got != JourneyContentSecurityPolicy("hcm.example.com") {
		t.Errorf("Content-Security-Policy = %q, want the public authority bound", got)
	}

	plain, plainToken := newShellHandler(t, false, "http://hcm.internal:8080")
	w = getJourney(t, plain, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+plainToken)
	})
	if got := island(t, w.Body.String()).TunnelURL; got != "ws://hcm.internal:8080"+PathTunnel {
		t.Errorf("tunnel_url = %q, want ws://hcm.internal:8080%s", got, PathTunnel)
	}
}

// TestJourneyShellRejectsAPublicOriginItCannotServe is the fail-closed half:
// an origin the sanitizer cannot represent must not reach a shell that would
// silently emit empty tunnel addresses.
func TestJourneyShellRejectsAPublicOriginItCannotServe(t *testing.T) {
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience,
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}
	for _, raw := range []string{
		"hcm.example.com", "https://", "https://hcm.example.com/path",
		"https://user@hcm.example.com", "ftp://hcm.example.com",
		"https://203.0.113.10",
	} {
		_, err := NewHandler(Options{
			Cell:         unreachableCell{t: t},
			Config:       transport.Config{Verifier: verifier, Audience: shellAudience},
			PublicOrigin: raw,
		})
		if err == nil {
			t.Errorf("PublicOrigin %q: NewHandler returned no error", raw)
		}
	}
}

// ---------------------------------------------------------------------------
// The bundle
// ---------------------------------------------------------------------------

// TestJourneyShellNamesTheBuildCommandWhenTheBundleIsMissing pins the
// stock-checkout behaviour: no bundle is committed, so this is what a person
// who opens the page in a fresh clone sees.
func TestJourneyShellNamesTheBuildCommandWhenTheBundleIsMissing(t *testing.T) {
	if JourneyBundleBuilt() {
		t.Skip("this build carries a journey bundle; the not-built notice cannot be observed")
	}
	h, token := newShellHandler(t, false)
	w := getJourney(t, h, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
	})
	body := w.Body.String()
	if !strings.Contains(body, `data-journey-bundle="missing"`) {
		t.Error("the shell does not carry the not-built notice")
	}
	if !strings.Contains(body, journeyBuildCommand) {
		t.Errorf("the shell does not name %q", journeyBuildCommand)
	}
	if strings.Contains(body, "<script src=") {
		t.Error("a build with no bundle must not load a client that is not there")
	}
	// The island is written either way: the page's configuration does not
	// depend on whether its client has been built yet.
	if cfg := island(t, body); cfg.TunnelURL == "" {
		t.Error("the island is missing from an unbuilt shell")
	}
}

// TestJourneyShellDocumentLoadsTheClientWhenBuilt drives the renderer
// directly, because whether this repository's own build embeds a bundle is
// not something a test may assume either way.
func TestJourneyShellDocumentLoadsTheClientWhenBuilt(t *testing.T) {
	doc, err := journeyShellDocument(JourneyConfig{
		TunnelURL: "ws://cell.test" + PathTunnel, Bearer: "token",
		Roles: []string{}, JourneysPath: PathJourney,
	}, true)
	if err != nil {
		t.Fatalf("journeyShellDocument: %v", err)
	}
	for _, want := range []string{
		"<script>" + journeyLoaderSource + "</script>",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("a built shell does not contain %q", want)
		}
	}
	if strings.Contains(doc, `<script src="`) {
		t.Fatal("a bearer-only shell cannot use a script request that omits its Authorization header")
	}
	if strings.Contains(doc, `data-journey-bundle="missing"`) {
		t.Error("a built shell must not carry the not-built notice")
	}
}

// TestJourneyLoaderSourceIsTheDocumentedConstant pins the loader byte for
// byte. It is the string whose sha256 the policy allows, so a change to it
// that is not also a change to the policy is a page that will not start.
func TestJourneyLoaderSourceIsTheDocumentedConstant(t *testing.T) {
	const want = `(function(){if(!window.WebAssembly){return}` +
		`var c=document.getElementById("journey-config"),j=JSON.parse(c?c.textContent||"{}":"{}");` +
		`function o(i){return{credentials:"same-origin",headers:{authorization:"Bearer "+(j.bearer||"")},integrity:i||""}}` +
		`function v(a){return a&&a.sha256?"?v="+encodeURIComponent(a.sha256):""}` +
		`function k(u,i){var f=function(){return fetch(u,o(i))};if(!window.caches){return f()}return caches.open("hcmnext-assets").then(function(c){return c.match(u).then(function(r){if(r){return r}return f().then(function(r){if(r.ok){c.put(u,r.clone()).then(function(){return c.keys().then(function(ks){for(var n=0;n<ks.length;n++){var q=new URL(ks[n].url);if(q.pathname==new URL(u,location.href).pathname&&ks[n].url.indexOf(u)<0){c.delete(ks[n])}}})},function(){})}return r})})},f)}` +
		`fetch("/workspace/assets/manifest.json",o())` +
		`.then(function(r){if(!r.ok){throw new Error("asset manifest unavailable")}return r.json()})` +
		`.then(function(m){var a,s;for(var i=0;i<m.assets.length;i++){if(m.assets[i].path=="/workspace/assets/journey.wasm"){a=m.assets[i]}else if(m.assets[i].path=="/workspace/assets/wasm_exec.js"){s=m.assets[i]}}` +
		`if(!a||!s||typeof a.integrity!=="string"||typeof s.integrity!=="string"){throw new Error("asset integrity unavailable")}` +
		`return k("/workspace/assets/wasm_exec.js"+v(s),s.integrity).then(function(r){if(!r.ok){throw new Error("wasm runtime unavailable")}return r.blob()}).then(function(b){var u=URL.createObjectURL(b);return import(u).then(function(){URL.revokeObjectURL(u)},function(e){URL.revokeObjectURL(u);throw e})}).then(function(){if(!window.Go){throw new Error("wasm runtime unavailable")}var g=new window.Go();return window.WebAssembly.instantiateStreaming(k("/workspace/assets/journey.wasm"+v(a),a.integrity),g.importObject).then(function(r){g.run(r.instance)})})})` +
		`.catch(function(){});})();`
	if journeyLoaderSource != want {
		t.Fatalf("journeyLoaderSource =\n%q\nwant\n%q", journeyLoaderSource, want)
	}
}

// ---------------------------------------------------------------------------
// The policy
// ---------------------------------------------------------------------------

func TestJourneyContentSecurityPolicy(t *testing.T) {
	policy := JourneyContentSecurityPolicy("cell.test:8080")
	for _, want := range []string{
		"default-src 'none'",
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'none'",
		"script-src '" + sha256Source(journeyLoaderSource) + "' blob: 'wasm-unsafe-eval'",
		"connect-src http://cell.test:8080" + PathAssetPrefix + " https://cell.test:8080" + PathAssetPrefix + " ws://cell.test:8080" + PathTunnel + " wss://cell.test:8080" + PathTunnel,
		"style-src '" + sha256Source(journey.Stylesheet()) + "'",
		"img-src 'none'",
	} {
		if !strings.Contains(policy, want) {
			t.Errorf("the policy does not contain %q\npolicy: %s", want, policy)
		}
	}
	if strings.Contains(policy, "unsafe-inline") {
		t.Error("the policy must not admit inline styles or scripts by keyword")
	}
}

// TestJourneyStylesheetHashIsTheClientsStylesheet is the assertion the CSP
// depends on: the hash in style-src is the hash of exactly the string the
// wasm client injects, journey.Stylesheet(), and not of a copy of it.
func TestJourneyStylesheetHashIsTheClientsStylesheet(t *testing.T) {
	if journeyStylesheetHash != sha256Source(journey.Stylesheet()) {
		t.Fatalf("journeyStylesheetHash = %q, want sha256Source(journey.Stylesheet())", journeyStylesheetHash)
	}
	if journeyStylesheetHash == stylesheetHash {
		t.Fatal("the journey page and the workspace do not share a stylesheet; their hashes must differ")
	}
}

func TestJourneyShellServesThePolicy(t *testing.T) {
	h, token := newShellHandler(t, false)
	w := getJourney(t, h, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
	})
	got := w.Header().Get("Content-Security-Policy")
	if got != JourneyContentSecurityPolicy("cell.test") {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	if !strings.Contains(got, "ws://cell.test"+PathTunnel) || !strings.Contains(got, "wss://cell.test"+PathTunnel) {
		t.Fatalf("the served policy does not admit this host's tunnel: %s", got)
	}
}

func TestSanitizeHostAuthority(t *testing.T) {
	cases := map[string]string{
		"cell.test":        "cell.test",
		"cell.test:8080":   "cell.test:8080",
		"[::1]:8080":       "",
		"192.0.2.1:8080":   "",
		"127.0.0.1:8080":   "127.0.0.1:8080",
		"":                 "",
		"   ":              "",
		"cell.test; evil":  "",
		"cell.test\nevil":  "",
		"cell.test'unsafe": "",
		"cell test":        "",
	}
	for host, want := range cases {
		if got := sanitizeHostAuthority(host); got != want {
			t.Errorf("sanitizeHostAuthority(%q) = %q, want %q", host, got, want)
		}
	}
	if got := JourneyContentSecurityPolicy("cell.test; evil"); strings.Contains(got, "evil") {
		t.Fatalf("an unusable Host must be dropped from the policy, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Publication
// ---------------------------------------------------------------------------

func TestJourneyRouteIsPublished(t *testing.T) {
	var found bool
	for _, route := range Routes() {
		if route.Path == PathJourney {
			found = true
			if route.Method != http.MethodGet {
				t.Errorf("the journey route is published as %s", route.Method)
			}
			if route.EffectClass != effectClassReadOnly {
				t.Errorf("the journey route is published as %s", route.EffectClass)
			}
		}
	}
	if !found {
		t.Fatalf("%s is served but not published in Routes()", PathJourney)
	}
}

func TestJourneyPathsAreConsistent(t *testing.T) {
	if !strings.HasPrefix(PathJourney, RoutePrefix) {
		t.Fatalf("PathJourney = %q is not under RoutePrefix %q", PathJourney, RoutePrefix)
	}
	if strings.Contains(PathJourney, "//") {
		t.Fatalf("PathJourney = %q carries a doubled separator", PathJourney)
	}
	if PathJourneyWasm != PathAssetPrefix+assetJourneyWasm {
		t.Fatalf("PathJourneyWasm = %q", PathJourneyWasm)
	}
}
