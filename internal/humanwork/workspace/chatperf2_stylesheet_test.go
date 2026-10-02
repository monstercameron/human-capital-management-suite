package workspace

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// chatperf2ThemePreferences answers every viewer with one stored theme.
type chatperf2ThemePreferences struct {
	preferences.Store
	theme preferences.Theme
}

func (s chatperf2ThemePreferences) Load(context.Context, values.TenantId, string, string) (preferences.Snapshot, error) {
	return preferences.Snapshot{Theme: preferences.TenantTheme{Theme: s.theme}}, nil
}

// chatperf2CustomTheme is the default theme with a tenant's own colours.
var chatperf2CustomTheme = func() preferences.Theme {
	base := productui.DefaultCustomerTheme()
	return preferences.Theme{
		BrandName: base.BrandName, BrandMark: base.BrandMark, ColorMode: base.ColorMode, Palette: "custom", Shape: base.Shape, Density: base.Density,
		Glyphs: base.Glyphs, Typeface: base.Typeface, Navigation: base.Navigation, Motion: base.Motion,
		TokenOverrides: map[string]string{
			"color.brand.primary": "#4d1f78", "color.brand.hover": "#371455", "color.brand.soft": "#f3eafb",
			"color.text.primary": "#20142b", "color.text.muted": "#5e5167", "color.canvas": "#fbf9fd",
			"color.surface": "#ffffff", "color.border": "#d9cfdf",
		},
	}
}()

var chatperf2LinkPattern = regexp.MustCompile(`<link rel="stylesheet" href="(/workspace/assets/(product-[0-9a-f]{64}\.css))" integrity="(sha256-[A-Za-z0-9+/=]+)">`)

func chatperf2Get(t *testing.T, h *Handler, token, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for name, value := range headers {
		if name == "Host" {
			request.Host = value
			continue
		}
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}

func chatperf2Directive(policy, name string) string {
	for _, directive := range strings.Split(policy, "; ") {
		if strings.HasPrefix(directive, name+" ") {
			return strings.TrimPrefix(directive, name+" ")
		}
	}
	return ""
}

// TestTodo_CHATBUG_014_LinkedStylesheet: a workspace document links its
// stylesheet at an address that carries the digest of its bytes instead of
// carrying the megabyte itself, the policy admits that one address and no
// inline style, and the address answers with those bytes, compressed and kept
// for good.
func TestTodo_CHATBUG_014_LinkedStylesheet(t *testing.T) {
	h, token := newShellHandler(t, false)
	document := chatperf2Get(t, h, token, PathProductHome, nil)
	if document.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", PathProductHome, document.Code)
	}
	body := document.Body.String()
	if strings.Contains(body, "<style") {
		t.Fatal("the document still carries an inline stylesheet")
	}
	if len(body) > 400<<10 {
		t.Fatalf("the document is %d bytes; the stylesheet is still in it", len(body))
	}
	links := chatperf2LinkPattern.FindAllStringSubmatch(body, -1)
	if len(links) != 1 || strings.Count(body, "<link") != 1 {
		t.Fatalf("the document has %d stylesheet links (%d link elements), want exactly one", len(links), strings.Count(body, "<link"))
	}
	href, name, integrity := links[0][1], links[0][2], links[0][3]
	if integrity != productStylesheetHash {
		t.Fatalf("the link's integrity %q is not the digest of the product stylesheet %q", integrity, productStylesheetHash)
	}

	// The policy names the one address. It admits no inline style, by hash or
	// otherwise, and nothing else on the origin.
	policy := document.Header().Get("Content-Security-Policy")
	want := "example.com" + href
	for _, directive := range []string{"style-src", "style-src-elem"} {
		if got := chatperf2Directive(policy, directive); got != want {
			t.Fatalf("%s = %q, want exactly %q", directive, got, want)
		}
	}
	if chatperf2Directive(policy, "style-src-attr") != "'none'" || strings.Contains(policy, "'unsafe-inline'") {
		t.Fatalf("the policy admits inline style: %s", policy)
	}

	// The address answers with the bytes its name and the link's integrity
	// promise, and says they never change.
	asset := chatperf2Get(t, h, token, href, nil)
	if asset.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", href, asset.Code)
	}
	sum := sha256.Sum256(asset.Body.Bytes())
	if got := productStylesheetAssetPrefix + hex.EncodeToString(sum[:]) + productStylesheetAssetSuffix; got != name {
		t.Fatalf("the stylesheet's bytes hash to %s, its address says %s", got, name)
	}
	if asset.Body.String() != productStylesheet() {
		t.Fatal("the linked stylesheet is not the product stylesheet")
	}
	if got := asset.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := asset.Header().Get("Cache-Control"); got != "private, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if asset.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("the stylesheet may be sniffed")
	}

	// Compressed for a browser that accepts it.
	compressed := chatperf2Get(t, h, token, href, map[string]string{"Accept-Encoding": "gzip, br"})
	if compressed.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(compressed.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("the stylesheet was not compressed: encoding %q, vary %q", compressed.Header().Get("Content-Encoding"), compressed.Header().Get("Vary"))
	}
	if compressed.Body.Len()*4 > asset.Body.Len() {
		t.Fatalf("compressed stylesheet is %d of %d bytes", compressed.Body.Len(), asset.Body.Len())
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil || string(plain) != productStylesheet() {
		t.Fatalf("the compressed stylesheet does not decompress to the product stylesheet (%v)", err)
	}

	// A copy the browser already holds is confirmed without the bytes.
	if revalidated := chatperf2Get(t, h, token, href, map[string]string{"If-None-Match": asset.Header().Get("ETag")}); revalidated.Code != http.StatusNotModified || revalidated.Body.Len() != 0 {
		t.Fatalf("revalidation = %d with %d bytes", revalidated.Code, revalidated.Body.Len())
	}
	// It is an authenticated asset like the others, and only a real digest
	// names one.
	if anonymous := chatperf2Get(t, h, "", href, nil); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated request for the stylesheet = %d, want 401", anonymous.Code)
	}
	if unknown := chatperf2Get(t, h, token, PathAssetPrefix+productStylesheetAssetPrefix+strings.Repeat("0", 64)+productStylesheetAssetSuffix, nil); unknown.Code != http.StatusNotFound {
		t.Fatalf("an address with another digest = %d, want 404", unknown.Code)
	}
	for _, name := range []string{"product-.css", "product-" + strings.Repeat("g", 64) + ".css", "product-" + strings.Repeat("A", 64) + ".css", "product-" + strings.Repeat("a", 63) + ".css", strings.Repeat("a", 64) + ".css"} {
		if productStylesheetAssetName(name) {
			t.Errorf("%q was taken for a stylesheet address", name)
		}
	}

	// A host the policy cannot name an address on keeps the inline sheet and
	// its hash.
	inline := chatperf2Get(t, h, token, PathProductHome, map[string]string{"Host": "[::1]:8080"})
	if inline.Code != http.StatusOK || !strings.Contains(inline.Body.String(), "<style>"+productStylesheet()+"</style>") || strings.Contains(inline.Body.String(), "<link") {
		t.Fatalf("a document for an unnameable host (%d) does not carry its stylesheet inline", inline.Code)
	}
	if got := chatperf2Directive(inline.Header().Get("Content-Security-Policy"), "style-src-elem"); got != "'"+productStylesheetHash+"'" {
		t.Fatalf("inline style-src-elem = %q", got)
	}
}

// TestTodo_CHATBUG_014_LinkedStylesheet_CustomTheme: a tenant with colours of
// its own gets its own address, the sheet is built once and not per request,
// and a process that has never built it serves it from the viewer's stored
// theme.
func TestTodo_CHATBUG_014_LinkedStylesheet_CustomTheme(t *testing.T) {
	h, token := newShellHandler(t, false)
	h.preferences = chatperf2ThemePreferences{theme: chatperf2CustomTheme}
	document := chatperf2Get(t, h, token, PathProductHome, nil)
	if document.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", PathProductHome, document.Code)
	}
	body := document.Body.String()
	link := chatperf2LinkPattern.FindStringSubmatch(body)
	if link == nil || strings.Contains(body, "<style") || !strings.Contains(body, `data-hcm-palette="custom"`) {
		t.Fatal("the custom-theme document does not link its stylesheet")
	}
	href, name := link[1], link[2]
	if name == defaultServedStylesheet().name {
		t.Fatal("the custom theme links the default stylesheet")
	}
	asset := chatperf2Get(t, h, token, href, nil)
	if asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "--hcm-color-brand-primary:#4d1f78") {
		t.Fatalf("the custom stylesheet (%d) does not carry the tenant's colours", asset.Code)
	}
	sum := sha256.Sum256(asset.Body.Bytes())
	if hex.EncodeToString(sum[:]) != strings.TrimSuffix(strings.TrimPrefix(name, productStylesheetAssetPrefix), productStylesheetAssetSuffix) {
		t.Fatal("the custom stylesheet's address is not the digest of its bytes")
	}
	if got := chatperf2Directive(document.Header().Get("Content-Security-Policy"), "style-src-elem"); got != "example.com"+href {
		t.Fatalf("style-src-elem = %q", got)
	}

	// The second document reuses the built sheet: the same value, not a
	// second megabyte.
	first := h.stylesheets.named(name)
	if again := chatperf2LinkPattern.FindStringSubmatch(chatperf2Get(t, h, token, PathProductHome, nil).Body.String()); again == nil || again[2] != name {
		t.Fatal("the second document links a different stylesheet")
	}
	if h.stylesheets.named(name) != first || len(h.stylesheets.order) != 1 {
		t.Fatalf("the custom stylesheet was built again (%d held)", len(h.stylesheets.order))
	}

	// A process that never rendered the document still answers the address.
	fresh, freshToken := newShellHandler(t, false)
	fresh.preferences = chatperf2ThemePreferences{theme: chatperf2CustomTheme}
	if cold := chatperf2Get(t, fresh, freshToken, href, nil); cold.Code != http.StatusOK || cold.Body.String() != asset.Body.String() {
		t.Fatalf("a fresh process answered the custom stylesheet with %d", cold.Code)
	}
	// A viewer whose stored theme is another one is not served under it.
	other, otherToken := newShellHandler(t, false)
	other.preferences = chatperf2ThemePreferences{}
	if refused := chatperf2Get(t, other, otherToken, href, nil); refused.Code != http.StatusNotFound {
		t.Fatalf("a stylesheet that is not the viewer's = %d, want 404", refused.Code)
	}

	// The handler keeps a bounded number of customer sheets.
	var cache productStylesheets
	for i := 0; i < productStylesheetCacheLimit+3; i++ {
		theme := productThemeFromPreference(chatperf2CustomTheme)
		theme.TokenOverrides = map[string]string{}
		for key, value := range chatperf2CustomTheme.TokenOverrides {
			theme.TokenOverrides[key] = value
		}
		theme.TokenOverrides["color.brand.primary"] = "#4d1f7" + string(rune('0'+i%10))
		if i >= 10 {
			theme.TokenOverrides["color.brand.primary"] = "#4d1f6" + string(rune('0'+i%10))
		}
		if _, err := cache.forTheme(theme); err != nil {
			t.Fatalf("theme %d: %v", i, err)
		}
	}
	if len(cache.byKey) != productStylesheetCacheLimit || len(cache.order) != productStylesheetCacheLimit {
		t.Fatalf("the cache holds %d sheets, want %d", len(cache.byKey), productStylesheetCacheLimit)
	}
}

// TestTodo_CHATBUG_014_CompressedDocument: the document is compressed for a
// request the browser reports as its own or the person's, and for no other.
func TestTodo_CHATBUG_014_CompressedDocument(t *testing.T) {
	h, token := newShellHandler(t, false)
	plain := chatperf2Get(t, h, token, PathProductHome, nil)
	if plain.Code != http.StatusOK || plain.Header().Get("Content-Encoding") != "" {
		t.Fatalf("a request that accepts no encoding got %d, encoding %q", plain.Code, plain.Header().Get("Content-Encoding"))
	}
	for _, site := range []string{"same-origin", "none"} {
		response := chatperf2Get(t, h, token, PathProductHome, map[string]string{"Accept-Encoding": "gzip, deflate, br", "Sec-Fetch-Site": site})
		if response.Code != http.StatusOK || response.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("Sec-Fetch-Site %s: %d, encoding %q", site, response.Code, response.Header().Get("Content-Encoding"))
		}
		if vary := response.Header().Get("Vary"); !strings.Contains(vary, "Accept-Encoding") || !strings.Contains(vary, "Sec-Fetch-Site") {
			t.Fatalf("Vary = %q", vary)
		}
		if response.Body.Len()*3 > plain.Body.Len() {
			t.Fatalf("compressed document is %d of %d bytes", response.Body.Len(), plain.Body.Len())
		}
		reader, err := gzip.NewReader(bytes.NewReader(response.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil || string(body) != plain.Body.String() {
			t.Fatalf("Sec-Fetch-Site %s: the compressed document is not the document (%v)", site, err)
		}
		if response.Header().Get("Content-Security-Policy") != plain.Header().Get("Content-Security-Policy") || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("the compressed document lost its security headers")
		}
	}
	// The document carries the session credential and reflects request text.
	// A request another site caused, or one that does not say where it came
	// from, is never compressed.
	for _, site := range []string{"cross-site", "same-site", ""} {
		headers := map[string]string{"Accept-Encoding": "gzip"}
		if site != "" {
			headers["Sec-Fetch-Site"] = site
		}
		response := chatperf2Get(t, h, token, PathProductHome, headers)
		if response.Code != http.StatusOK || response.Header().Get("Content-Encoding") != "" || response.Body.String() != plain.Body.String() {
			t.Fatalf("Sec-Fetch-Site %q was answered compressed", site)
		}
	}
	if documentCompressible(nil) {
		t.Fatal("a missing request is compressible")
	}
}
