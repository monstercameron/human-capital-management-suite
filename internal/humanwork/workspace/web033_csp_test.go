package workspace

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/tokens"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestTodo_WEB_033(t *testing.T) {
	profiles := []struct {
		name       string
		policy     string
		styleHash  string
		scriptHash string
	}{
		{name: "workspace-rendered", policy: ContentSecurityPolicy("cell.test:8080", false), styleHash: stylesheetHash},
		{name: "workspace-enhanced", policy: ContentSecurityPolicy("cell.test:8080", true), styleHash: stylesheetHash, scriptHash: loaderHash},
		{name: "journey", policy: JourneyContentSecurityPolicy("cell.test:8080"), styleHash: journeyStylesheetHash, scriptHash: journeyLoaderHash},
		{name: "product", policy: ProductContentSecurityPolicy("cell.test:8080"), styleHash: productStylesheetHash, scriptHash: journeyLoaderHash},
		{name: "login", policy: cspPolicy{styleHashes: []string{sha256Source(loginStylesheet())}, formActionSelf: true}.header(), styleHash: sha256Source(loginStylesheet())},
	}
	for _, profile := range profiles {
		t.Run(profile.name, func(t *testing.T) {
			directives := web033ParseCSP(t, profile.policy)
			for _, required := range []string{
				"default-src", "base-uri", "object-src", "frame-src", "child-src", "frame-ancestors",
				"form-action", "script-src", "script-src-elem", "script-src-attr", "style-src",
				"style-src-elem", "style-src-attr", "connect-src", "img-src", "font-src", "media-src",
				"worker-src", "manifest-src",
			} {
				if _, ok := directives[required]; !ok {
					t.Errorf("missing %s directive in %s", required, profile.policy)
				}
			}
			if profile.styleHash != "" && !strings.Contains(profile.policy, "'"+profile.styleHash+"'") {
				t.Errorf("policy does not pin the rendered stylesheet %q", profile.styleHash)
			}
			if profile.scriptHash != "" && !strings.Contains(profile.policy, "'"+profile.scriptHash+"'") {
				t.Errorf("policy does not pin the inline loader %q", profile.scriptHash)
			}
			for _, forbidden := range []string{"'unsafe-inline'", "'unsafe-eval'", "connect-src *", "frame-src *", "object-src *"} {
				if strings.Contains(profile.policy, forbidden) {
					t.Errorf("policy contains forbidden capability %q: %s", forbidden, profile.policy)
				}
			}
			for _, name := range []string{"script-src", "script-src-elem"} {
				if web033HasToken(directives[name], "'self'") || web033HasToken(directives[name], "data:") {
					t.Errorf("%s grants an ambient script source: %q", name, directives[name])
				}
			}
		})
	}
}

func TestTodo_WEB_033_Golden(t *testing.T) {
	journeyWant := strings.Join([]string{
		"default-src 'none'",
		"base-uri 'none'",
		"object-src 'none'",
		"frame-src 'none'",
		"child-src 'none'",
		"form-action 'none'",
		"frame-ancestors 'none'",
		"script-src '" + journeyLoaderHash + "' blob: 'wasm-unsafe-eval'",
		"script-src-elem '" + journeyLoaderHash + "' blob:",
		"script-src-attr 'none'",
		"style-src '" + journeyStylesheetHash + "'",
		"style-src-elem '" + journeyStylesheetHash + "'",
		"style-src-attr 'none'",
		"connect-src http://cell.test:8080" + PathAssetPrefix + " https://cell.test:8080" + PathAssetPrefix + " ws://cell.test:8080" + PathTunnel + " wss://cell.test:8080" + PathTunnel,
		"img-src 'none'",
		"font-src 'none'",
		"media-src 'none'",
		"worker-src 'none'",
		"manifest-src 'none'",
	}, "; ")
	if got := JourneyContentSecurityPolicy("cell.test:8080"); got != journeyWant {
		t.Fatalf("journey CSP changed unexpectedly\ngot:  %s\nwant: %s", got, journeyWant)
	}

	workspaceWant := strings.Join([]string{
		"default-src 'none'",
		"base-uri 'none'",
		"object-src 'none'",
		"frame-src 'none'",
		"child-src 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"script-src 'none'",
		"script-src-elem 'none'",
		"script-src-attr 'none'",
		"style-src '" + stylesheetHash + "'",
		"style-src-elem '" + stylesheetHash + "'",
		"style-src-attr 'none'",
		"connect-src 'none'",
		"img-src 'none'",
		"font-src 'none'",
		"media-src 'none'",
		"worker-src 'none'",
		"manifest-src 'none'",
	}, "; ")
	if got := ContentSecurityPolicy("cell.test:8080", false); got != workspaceWant {
		t.Fatalf("rendered workspace CSP changed unexpectedly\ngot:  %s\nwant: %s", got, workspaceWant)
	}

	first := sha256Source("first")
	second := sha256Source("second")
	got := cspStyleSources([]string{second, first, second})
	want := "'" + first + "' '" + second + "'"
	if first > second {
		want = "'" + second + "' '" + first + "'"
	}
	if got != want {
		t.Fatalf("style source ordering = %q, want %q", got, want)
	}
}

func TestTodo_WEB_033_Browser(t *testing.T) {
	nativeWorkspace, err := Render(ux002Contract(), "csrf", "worker", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(nativeWorkspace, "<script") || strings.Contains(nativeWorkspace, ` style=`) || !strings.Contains(nativeWorkspace, "<style>"+gwc.Stylesheet()+"</style>") {
		t.Fatal("native workspace document is not CSP-compatible")
	}
	enhancedWorkspace, err := Render(ux002Contract(), "csrf", "worker", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(enhancedWorkspace, "<script") != 2 || !strings.Contains(enhancedWorkspace, "<script>"+loaderSource+"</script>") || strings.Contains(enhancedWorkspace, ` style=`) {
		t.Fatal("enhanced workspace document is not coherent with its hash-pinned loader policy")
	}

	config := JourneyConfig{TunnelURL: "wss://cell.test" + PathTunnel, Bearer: "opaque-token", Tenant: "tenant", Subject: "subject"}
	journeyDoc, err := journeyShellDocument(config, true)
	if err != nil {
		t.Fatal(err)
	}
	productDoc, err := productShellDocument(config, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{"journey": journeyDoc, "product": productDoc} {
		t.Run(name, func(t *testing.T) {
			if strings.Count(doc, "<script") != 2 {
				t.Fatalf("shell has %d script elements, want JSON island plus one loader", strings.Count(doc, "<script"))
			}
			if !strings.Contains(doc, `<script type="application/json" id="`+JourneyConfigElementID+`">`) || !strings.Contains(doc, "<script>"+journeyLoaderSource+"</script>") {
				t.Fatal("shell did not preserve the JSON island and byte-for-byte loader")
			}
			if strings.Contains(doc, ` style=`) || regexp.MustCompile(`\s+on[a-z]+\s*=`).MatchString(doc) {
				t.Fatal("shell emitted an inline style or event-handler attribute")
			}
		})
	}
	if strings.Count(productDoc, "<style>") != 1 || !strings.Contains(productDoc, "<style>"+productStylesheet()+"</style>") {
		t.Fatal("product shell does not carry exactly its pinned stylesheet")
	}
	hostileDoc, err := productShellDocument(JourneyConfig{Bearer: `</script><img src=x onerror=alert(1)>`}, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hostileDoc, `</script><img`) || !strings.Contains(hostileDoc, `\u003c/script\u003e`) || strings.Count(hostileDoc, "<script") != 2 {
		t.Fatal("JSON island did not HTML-escape a script terminator")
	}
}

func TestTodo_WEB_033_Conformance(t *testing.T) {
	tests := []struct {
		name          string
		policy        string
		styleHash     string
		scriptHash    string
		assetConnect  bool
		tunnelConnect bool
		imageSelf     bool
	}{
		{name: "workspace", policy: ContentSecurityPolicy("cell.test:8080", false), styleHash: stylesheetHash},
		{name: "enhanced", policy: ContentSecurityPolicy("cell.test:8080", true), styleHash: stylesheetHash, scriptHash: loaderHash, assetConnect: true},
		{name: "journey", policy: JourneyContentSecurityPolicy("cell.test:8080"), styleHash: journeyStylesheetHash, scriptHash: journeyLoaderHash, assetConnect: true, tunnelConnect: true},
		{name: "product", policy: ProductContentSecurityPolicy("cell.test:8080"), styleHash: productStylesheetHash, scriptHash: journeyLoaderHash, assetConnect: true, tunnelConnect: true, imageSelf: true},
		{name: "login", policy: cspPolicy{styleHashes: []string{sha256Source(loginStylesheet())}, formActionSelf: true}.header(), styleHash: sha256Source(loginStylesheet())},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directives := web033ParseCSP(t, test.policy)
			for _, name := range []string{"object-src", "frame-src", "child-src", "font-src", "media-src", "worker-src", "manifest-src"} {
				if got := strings.Join(directives[name], " "); got != "'none'" {
					t.Errorf("%s = %q, want 'none'", name, got)
				}
			}
			if got := strings.Join(directives["script-src-attr"], " "); got != "'none'" {
				t.Errorf("script-src-attr = %q", got)
			}
			if got := strings.Join(directives["style-src-attr"], " "); got != "'none'" {
				t.Errorf("style-src-attr = %q", got)
			}
			if test.scriptHash != "" {
				if !web033HasToken(directives["script-src"], "'"+test.scriptHash+"'") || !web033HasToken(directives["script-src-elem"], "'"+test.scriptHash+"'") {
					t.Error("script hash is not present in both script boundaries")
				}
			} else if strings.Join(directives["script-src"], " ") != "'none'" || strings.Join(directives["script-src-elem"], " ") != "'none'" {
				t.Error("non-enhanced profile has an executable script allowance")
			}
			if web033HasToken(directives["script-src"], "'self'") || web033HasToken(directives["script-src-elem"], "'self'") {
				t.Error("same-origin scripts are broader than the authenticated blob loader needs")
			}
			if !web033HasToken(directives["style-src"], "'"+test.styleHash+"'") || !web033HasToken(directives["style-src-elem"], "'"+test.styleHash+"'") {
				t.Error("stylesheet hash is not present in both style boundaries")
			}
			connect := strings.Join(directives["connect-src"], " ")
			if strings.Contains(connect, "'self'") {
				t.Errorf("connect-src retained origin-wide authority: %q", connect)
			}
			if test.assetConnect != strings.Contains(connect, "http://cell.test:8080"+PathAssetPrefix) || test.assetConnect != strings.Contains(connect, "https://cell.test:8080"+PathAssetPrefix) {
				t.Errorf("connect-src = %q, assetConnect=%v", connect, test.assetConnect)
			}
			if test.tunnelConnect != strings.Contains(connect, "ws://cell.test:8080"+PathTunnel) || test.tunnelConnect != strings.Contains(connect, "wss://cell.test:8080"+PathTunnel) {
				t.Errorf("connect-src = %q, tunnelConnect=%v", connect, test.tunnelConnect)
			}
			for _, source := range directives["connect-src"] {
				switch {
				case strings.HasPrefix(source, "ws://"), strings.HasPrefix(source, "wss://"):
					if !strings.HasSuffix(source, PathTunnel) {
						t.Errorf("WebSocket source is not path scoped: %q", source)
					}
				case strings.HasPrefix(source, "http://"), strings.HasPrefix(source, "https://"):
					if !strings.HasSuffix(source, PathAssetPrefix) && !strings.HasSuffix(source, PathChatMediaPrefix) && !strings.HasSuffix(source, PathDocumentMediaPrefix) {
						t.Errorf("HTTP source is not asset- or media-prefix scoped: %q", source)
					}
				}
			}
			if test.imageSelf != strings.Contains(strings.Join(directives["img-src"], " "), "'self'") {
				t.Errorf("img-src does not match profile: %q", directives["img-src"])
			}
		})
	}
}

func TestTodo_WEB_033_Security(t *testing.T) {
	for _, host := range []string{
		"cell.test; connect-src *",
		`cell.test"; script-src 'unsafe-inline'`,
		"cell.test\r\nX-Leak: yes",
		" cell.test",
		"cell.test ",
		"[::1",
		"[::1]",
		"[::1]:8080",
		"192.0.2.1",
		"2130706433",
		"0x7f000001",
		"cell.test:0",
		"cell.test:65536",
		"cell_test",
	} {
		policy := JourneyContentSecurityPolicy(host)
		if strings.Contains(policy, "ws://") || strings.Contains(policy, "wss://") || strings.Contains(policy, "'unsafe-inline'") || strings.Contains(policy, "'unsafe-eval'") || strings.Contains(policy, "X-Leak") {
			t.Errorf("untrusted host %q gained policy authority: %s", host, policy)
		}
		r := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathJourney, nil)
		r.Host = host
		if got := JourneyTunnelURL(r); got != "" {
			t.Errorf("JourneyTunnelURL(%q) = %q, want fail-closed empty URL", host, got)
		}
	}
	for name, source := range map[string]string{"workspace": loaderSource, "journey": journeyLoaderSource} {
		// UXLIVE-013 routed the executable fetches through k(), which builds
		// its request options with o() and nothing else, so the SRI pin now
		// travels as k's argument. The guarantee is the same one: no
		// executable byte is fetched without both the bearer token and its
		// integrity.
		if !strings.Contains(source, `integrity:i||""`) || !strings.Contains(source, "return fetch(u,o(i))") ||
			!strings.Contains(source, ",s.integrity)") || !strings.Contains(source, ",a.integrity)") ||
			strings.Contains(source, "fetch(u,{") {
			t.Errorf("%s loader does not carry authenticated SRI fetches", name)
		}
		for _, forbidden := range []string{"eval(", "innerHTML", "http://", "https://"} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s loader contains forbidden executable or external surface %q", name, forbidden)
			}
		}
	}
	for name, css := range map[string]string{"workspace": tokens.WorkspaceCSS(), "journey": journey.Stylesheet(), "product": productStylesheet()} {
		if strings.Contains(strings.ToLower(css), "</style") || strings.Contains(css, "`") {
			t.Errorf("%s stylesheet is not safe to carry as a hash-pinned inline block", name)
		}
	}
}

func TestTodo_WEB_033_Integration(t *testing.T) {
	h, token := web033HandlerWithoutManifest(t)
	journeyRequest := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathJourney, nil)
	journeyRequest.Host = "cell.test"
	journeyRequest.Header.Set("Authorization", "Bearer "+token)
	journeyResponse := httptest.NewRecorder()
	h.serveJourney(journeyResponse, journeyRequest)
	if journeyResponse.Code != http.StatusOK || journeyResponse.Header().Get("Content-Security-Policy") != JourneyContentSecurityPolicy("cell.test") {
		t.Fatalf("journey response status/CSP = %d/%q", journeyResponse.Code, journeyResponse.Header().Get("Content-Security-Policy"))
	}
	if !strings.Contains(journeyResponse.Body.String(), `id="`+JourneyConfigElementID+`"`) {
		t.Fatal("journey response lost its configuration island")
	}

	productRequest := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathProductHome, nil)
	productRequest.Host = "cell.test"
	productRequest.Header.Set("Authorization", "Bearer "+token)
	productResponse := httptest.NewRecorder()
	h.serveProduct(productResponse, productRequest)
	if productResponse.Code != http.StatusOK || productResponse.Header().Get("Content-Security-Policy") != ProductContentSecurityPolicy("cell.test") {
		t.Fatalf("product response status/CSP = %d/%q", productResponse.Code, productResponse.Header().Get("Content-Security-Policy"))
	}

	loginResponse := httptest.NewRecorder()
	h.writeLoginPage(loginResponse, http.StatusOK, "")
	if got := loginResponse.Header().Get("Content-Security-Policy"); got != (cspPolicy{styleHashes: []string{sha256Source(loginStylesheet())}, formActionSelf: true}.header()) {
		t.Fatalf("login response CSP = %q", got)
	}

	problemResponse := httptest.NewRecorder()
	h.writeProblem(problemResponse, http.StatusBadRequest, "Bad request", "refused")
	problemDirectives := web033ParseCSP(t, problemResponse.Header().Get("Content-Security-Policy"))
	if got := strings.Join(problemDirectives["form-action"], " "); got != "'none'" {
		t.Fatalf("problem form-action = %q", got)
	}
	web033AssertHTMLSecurityHeaders(t, problemResponse)

	redirectResponse := httptest.NewRecorder()
	writeRedirect(redirectResponse, httptest.NewRequest(http.MethodGet, "http://cell.test/", nil), PathLogin, http.StatusSeeOther)
	redirectDirectives := web033ParseCSP(t, redirectResponse.Header().Get("Content-Security-Policy"))
	if redirectResponse.Code != http.StatusSeeOther || strings.Join(redirectDirectives["default-src"], " ") != "'none'" {
		t.Fatalf("redirect response status/CSP = %d/%q", redirectResponse.Code, redirectResponse.Header().Get("Content-Security-Policy"))
	}
	web033AssertHTMLSecurityHeaders(t, redirectResponse)

	h.assetManifestETag = `"web033"`
	cachedRequest := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathAssetManifest, nil)
	cachedRequest.Header.Set("If-None-Match", h.assetManifestETag)
	cachedResponse := httptest.NewRecorder()
	h.serveAssetIntegrityManifest(cachedResponse, cachedRequest)
	if cachedResponse.Code != http.StatusNotModified || cachedResponse.Header().Get("X-Content-Type-Options") != "nosniff" || cachedResponse.Header().Get("Cache-Control") != "private, max-age=0, must-revalidate" {
		t.Fatalf("cached asset response status/headers = %d/%v", cachedResponse.Code, cachedResponse.Header())
	}
}

func TestTodo_WEB_033_Fault(t *testing.T) {
	validHashWithLineBreak := sha256Source("web-033") + "\n"
	malformed := cspPolicy{
		styleHashes:           []string{"sha256-not-base64", "sha512-nope", validHashWithLineBreak},
		scriptHash:            validHashWithLineBreak,
		formActionSelf:        true,
		connectHost:           "cell.test; connect-src *",
		allowAssetConnections: true,
		allowTunnelConnection: true,
		allowBlobScript:       true,
		allowWASM:             true,
	}.header()
	directives := web033ParseCSP(t, malformed)
	if got := strings.Join(directives["script-src"], " "); got != "'none'" {
		t.Fatalf("malformed script hash was authorized: %q", got)
	}
	if got := strings.Join(directives["script-src-elem"], " "); got != "'none'" {
		t.Fatalf("malformed script element hash was authorized: %q", got)
	}
	if got := strings.Join(directives["style-src"], " "); got != "'none'" {
		t.Fatalf("malformed style hash was authorized: %q", got)
	}
	if strings.Contains(malformed, "ws://cell.test") || strings.Contains(malformed, "connect-src *") {
		t.Fatalf("malformed host gained a tunnel authority: %s", malformed)
	}
	if !strings.Contains(malformed, "connect-src 'none'") {
		t.Fatal("invalid tunnel authority did not fail closed")
	}

	for _, input := range []string{"cell.test:bogus", "cell.test:0", "cell.test:99999", "[127.0.0.1]", "[::1]", "192.0.2.1", "999.999.999.999", "2130706433", "0x7f000001", "-bad.test", "bad-.test", "bad..test"} {
		if got := sanitizeHostAuthority(input); got != "" {
			t.Errorf("sanitizeHostAuthority(%q) = %q, want empty", input, got)
		}
	}
	for input, want := range map[string]string{"Cell.TEST:08080": "cell.test:8080", "127.0.0.1:443": "127.0.0.1:443", "localhost": "localhost"} {
		if got := sanitizeHostAuthority(input); got != want {
			t.Errorf("sanitizeHostAuthority(%q) = %q, want %q", input, got, want)
		}
	}

	maxHost := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 57) + ":65535"
	if sanitizeHostAuthority(maxHost) == "" {
		t.Fatal("maximum bounded host fixture was not a valid authority")
	}
	// The product policy names eight path-scoped connect sources (assets,
	// chat media, document media and the tunnel, each over both schemes), so
	// the bound leaves room for them at the longest valid authority while
	// staying well under the 4 KiB header line common proxies enforce.
	if got := len(ProductContentSecurityPolicy(maxHost)); got > 3072 {
		t.Fatalf("maximum valid CSP header is %d bytes, want <= 3072", got)
	}
}

func TestTodo_WEB_033_Latency(t *testing.T) {
	budget := latencygate.Budget{Name: "production CSP construction", P95: 2 * time.Millisecond, Warmups: 3, Samples: 25}
	result, err := latencygate.Measure(budget, func() error {
		if ContentSecurityPolicy("cell.test:8080", true) == "" || JourneyContentSecurityPolicy("cell.test:8080") == "" || ProductContentSecurityPolicy("cell.test:8080") == "" {
			return errors.New("CSP builder returned an empty policy")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := latencygate.Check(budget, result); err != nil {
		t.Fatalf("%v (%s)", err, result)
	}
	t.Logf("%s", result)
}

func BenchmarkProductionContentSecurityPolicy(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ContentSecurityPolicy("cell.test:8080", true)
		_ = JourneyContentSecurityPolicy("cell.test:8080")
		_ = ProductContentSecurityPolicy("cell.test:8080")
	}
}

func web033AssertHTMLSecurityHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	for name, want := range map[string]string{
		"Cache-Control":          "no-store",
		"Content-Type":           "text/html; charset=utf-8",
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
	} {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func web033ParseCSP(t *testing.T, policy string) map[string][]string {
	t.Helper()
	result := make(map[string][]string)
	for _, rawDirective := range strings.Split(policy, ";") {
		fields := strings.Fields(strings.TrimSpace(rawDirective))
		if len(fields) == 0 {
			continue
		}
		if _, exists := result[fields[0]]; exists {
			t.Fatalf("duplicate CSP directive %q in %q", fields[0], policy)
		}
		result[fields[0]] = fields[1:]
	}
	return result
}

func web033HasToken(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// web033HandlerWithoutManifest exercises the authenticated shell paths
// without invoking NewHandler's generated asset-manifest gate. The checkout
// intentionally carries only compressed WEB-032 artifacts; this fixture keeps
// WEB-033 integration coverage focused on response policy wiring.
func web033HandlerWithoutManifest(t *testing.T) (*Handler, string) {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience,
		Now: func() time.Time { return shellNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer: shellIssuer, Audience: shellAudience, Subject: shellSubject,
		SubjectKind: "human", Tenant: shellTenant, OrganizationScopeID: "org-north-america",
		Roles: []string{"intent_author", "comp_admin"}, Purposes: []string{shellPurpose},
		AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "web033",
		IssuedAtUnix: shellNow.Add(-time.Minute).Unix(), ExpiresAtUnix: shellNow.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Handler{
		cell:        unreachableCell{t: t},
		config:      transport.Config{Verifier: verifier, Audience: shellAudience, Now: func() time.Time { return shellNow }},
		now:         func() time.Time { return shellNow },
		devPersonas: map[string]DevPersona{},
	}, token
}
