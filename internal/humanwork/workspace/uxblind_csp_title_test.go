package workspace

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// cspConnectAllows applies CSP source-expression path matching to one
// connect-src directive: a source path ending in "/" matches as a prefix, any
// other source path matches exactly, and the query never takes part.
func cspConnectAllows(t *testing.T, policy, target string) bool {
	t.Helper()
	var sources []string
	for _, directive := range strings.Split(policy, ";") {
		fields := strings.Fields(directive)
		if len(fields) > 0 && fields[0] == "connect-src" {
			sources = fields[1:]
		}
	}
	if sources == nil {
		t.Fatalf("policy has no connect-src: %q", policy)
	}
	want, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range sources {
		source, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if source.Scheme == "" && source.Host == "" {
			// CSP host sources may omit the scheme; in that form the
			// protected document supplies it. Match the host and path exactly
			// for both HTTP and HTTPS fixtures.
			if !strings.HasPrefix(raw, want.Host) || len(raw) == len(want.Host) || raw[len(want.Host)] != '/' {
				continue
			}
			sourcePath := raw[len(want.Host):]
			if sourcePath == want.Path || strings.HasSuffix(sourcePath, "/") && strings.HasPrefix(want.Path, sourcePath) {
				return true
			}
			continue
		}
		if source.Scheme != want.Scheme || source.Host != want.Host {
			continue
		}
		if source.Path == "" || source.Path == want.Path ||
			strings.HasSuffix(source.Path, "/") && strings.HasPrefix(want.Path, source.Path) {
			return true
		}
	}
	return false
}

// clientConnectTargets are the addresses the workspace WASM client opens
// with fetch or a WebSocket: the asset loader, protected chat and document
// media, the brand-asset picker (list, upload, lifecycle) and the tunnel.
var clientConnectTargets = []string{
	"http://cell.test" + PathAssetPrefix + "manifest.json",
	"http://cell.test" + PathChatMediaPrefix + "media-1",
	"http://cell.test" + PathDocumentMediaPrefix + "media-1",
	"http://cell.test" + PathBrandAssets,
	"http://cell.test" + PathBrandAssets + "?before=3",
	"http://cell.test" + PathBrandAssetLifecycle,
	"ws://cell.test" + PathTunnel,
}

// TestTodo_UXBLIND_085 asserts the product policy's connect-src covers every
// client fetch path, so the appearance page's brand-asset requests are no
// longer refused by the browser.
func TestTodo_UXBLIND_085(t *testing.T) {
	policy := ProductContentSecurityPolicy("cell.test")
	for _, target := range clientConnectTargets {
		if !cspConnectAllows(t, policy, target) {
			t.Errorf("connect-src refuses client request %s", target)
		}
	}
}

// TestTodo_UXBLIND_085_Browser reads the policy header the browser actually
// receives with a signed-in product page and checks the brand-asset requests
// against it.
func TestTodo_UXBLIND_085_Browser(t *testing.T) {
	h, token := newShellHandler(t, false)
	for _, page := range []productui.PageID{productui.PageHome, productui.PagePeople, productui.PageSettings} {
		request := httptest.NewRequest(http.MethodGet, "http://cell.test"+productui.Path(page), nil)
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", page, recorder.Code)
		}
		policy := recorder.Header().Get("Content-Security-Policy")
		for _, target := range clientConnectTargets {
			if !cspConnectAllows(t, policy, target) {
				t.Errorf("%s: served connect-src refuses %s", page, target)
			}
		}
	}
}

// TestTodo_UXBLIND_085_Regression keeps the new allowance exact: no sibling
// path, other product route, other host or the standalone journey shell.
func TestTodo_UXBLIND_085_Regression(t *testing.T) {
	policy := ProductContentSecurityPolicy("cell.test")
	for _, target := range []string{
		"http://cell.test" + PathBrandAssets + "-export",
		"http://cell.test" + PathBrandAssetPrefix + "0123abcd",
		"http://cell.test" + PathProductHome,
		"http://cell.test" + PathLogin,
		"http://evil.test" + PathBrandAssets,
		"http://cell.test/",
	} {
		if cspConnectAllows(t, policy, target) {
			t.Errorf("connect-src widened to %s", target)
		}
	}
	if journey := JourneyContentSecurityPolicy("cell.test"); strings.Contains(journey, PathBrandAssets) {
		t.Fatalf("standalone journey shell gained brand-asset connections: %q", journey)
	}
}

// TestTodo_UXBLIND_087_Browser checks the title the browser receives for
// three product pages: '<page> · <company display name>', never the suite.
func TestTodo_UXBLIND_087_Browser(t *testing.T) {
	h := newCompanyLoginHandler(t)
	config := JourneyConfig{Tenant: "ironridge-demo"}
	h.applyCompanyBrand(&config)
	if config.TenantName != "Ironridge Builders" {
		t.Fatalf("company brand = %q", config.TenantName)
	}
	for _, tc := range []struct {
		page productui.PageID
		want string
	}{
		{productui.PageHome, "<title>Home · Ironridge Builders</title>"},
		{productui.PagePeople, "<title>People · Ironridge Builders</title>"},
		{productui.PageJourneys, "<title>Journeys · Ironridge Builders</title>"},
	} {
		doc, err := productShellDocumentForRouteStateWithTheme(config, true, productui.ResolveProductLocale("en-US"), tc.page, "", "", productui.DefaultCustomerTheme(), productStylesheet())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, tc.want) {
			start := strings.Index(doc, "<title>")
			t.Errorf("%s: document title = %q, want %q", tc.page, doc[start:start+strings.Index(doc[start:], "</title>")+8], tc.want)
		}
		if strings.Contains(doc, "<title>"+"Human Capital Management Suite") || strings.Contains(doc, "· Human Capital Management Suite</title>") {
			t.Errorf("%s: document title names the suite instead of the company", tc.page)
		}
	}
}
