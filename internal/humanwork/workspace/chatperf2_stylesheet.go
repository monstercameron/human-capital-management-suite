package workspace

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CHATBUG-014: the product stylesheet is about 1.1 MB, and it was written into
// every workspace document. Each navigation therefore transferred it again
// (uncompressed), the browser parsed it again, and a tenant with its own
// colours had it rebuilt and hashed for every request.
//
// It is now an asset of its own. The document links it at an address that
// carries the SHA-256 of its bytes, so the browser keeps it for good and a
// changed sheet is a different address. The policy stays as strict as the
// hash it replaces: it names that one address and nothing else on the origin,
// the link carries the same digest as its integrity value so other bytes are
// refused, and inline style elements are still refused outright.

const (
	productStylesheetAssetPrefix = "product-"
	productStylesheetAssetSuffix = ".css"
	// productStylesheetCacheLimit bounds the customer sheets one process
	// keeps. A sheet that has been dropped is built again from the viewer's
	// stored theme when it is asked for.
	productStylesheetCacheLimit = 8
)

// servedStylesheet is one complete product stylesheet and the names it is
// served and pinned under. It does not change after it is built.
type servedStylesheet struct {
	// name is the asset name, product-<sha256 hex>.css.
	name string
	body string
	// integrity is the CSP and subresource-integrity form of the same digest.
	integrity string
	etag      string

	gzipOnce sync.Once
	gzipBody []byte
}

func newServedStylesheet(body string) *servedStylesheet {
	sum := sha256.Sum256([]byte(body))
	digest := hex.EncodeToString(sum[:])
	return &servedStylesheet{
		name:      productStylesheetAssetPrefix + digest + productStylesheetAssetSuffix,
		body:      body,
		integrity: "sha256-" + base64.StdEncoding.EncodeToString(sum[:]),
		etag:      `"` + digest + `"`,
	}
}

// compressed is the gzip form, made the first time a browser asks for it.
func (s *servedStylesheet) compressed() []byte {
	s.gzipOnce.Do(func() {
		var out bytes.Buffer
		writer, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
		if err != nil {
			return
		}
		if _, err := writer.Write([]byte(s.body)); err != nil {
			return
		}
		if writer.Close() != nil {
			return
		}
		s.gzipBody = out.Bytes()
	})
	return s.gzipBody
}

// linkElement is the head element of a document that links the sheet.
func (s *servedStylesheet) linkElement() string {
	return `<link rel="stylesheet" href="` + PathAssetPrefix + s.name + `" integrity="` + s.integrity + `">`
}

// inlineElement is the head element of a document that carries the sheet
// itself, for a host the policy cannot name an address on.
func (s *servedStylesheet) inlineElement() string {
	return "<style>" + s.body + "</style>"
}

// productStylesheetAssetName reports whether name has the form of a product
// stylesheet address: the prefix, 64 lower-case hex digits, the suffix.
func productStylesheetAssetName(name string) bool {
	if !strings.HasPrefix(name, productStylesheetAssetPrefix) || !strings.HasSuffix(name, productStylesheetAssetSuffix) {
		return false
	}
	digest := name[len(productStylesheetAssetPrefix) : len(name)-len(productStylesheetAssetSuffix)]
	if len(digest) != hex.EncodedLen(sha256.Size) {
		return false
	}
	for _, r := range digest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// defaultServedStylesheet is the sheet of every tenant without colours of its
// own. It is the same for the life of the process.
var defaultServedStylesheet = sync.OnceValue(func() *servedStylesheet {
	return newServedStylesheet(productStylesheet())
})

// productStylesheets holds the customer sheets a handler has built. The zero
// value is ready to use.
type productStylesheets struct {
	mu    sync.Mutex
	byKey map[string]*servedStylesheet
	// order lists the keys oldest first, so the oldest is the one dropped.
	order []string
}

// forTheme returns the sheet for a normalized, validated theme, building it
// only the first time the theme is seen.
func (c *productStylesheets) forTheme(theme productui.CustomerTheme) (*servedStylesheet, error) {
	if theme.Palette != "custom" {
		return defaultServedStylesheet(), nil
	}
	// The sheet of a custom palette depends on its two override tables and on
	// nothing else; encoding/json writes map keys in sorted order.
	encoded, err := json.Marshal([2]map[string]string{theme.TokenOverrides, theme.DarkTokenOverrides})
	if err != nil {
		return nil, err
	}
	key := string(encoded)
	c.mu.Lock()
	sheet := c.byKey[key]
	c.mu.Unlock()
	if sheet != nil {
		return sheet, nil
	}
	body, err := productStylesheetForTheme(theme)
	if err != nil {
		return nil, err
	}
	built := newServedStylesheet(body)
	c.mu.Lock()
	defer c.mu.Unlock()
	if sheet := c.byKey[key]; sheet != nil {
		// Another request built the same theme meanwhile.
		return sheet, nil
	}
	if c.byKey == nil {
		c.byKey = make(map[string]*servedStylesheet, productStylesheetCacheLimit)
	}
	if len(c.order) >= productStylesheetCacheLimit {
		delete(c.byKey, c.order[0])
		c.order = c.order[1:]
	}
	c.byKey[key] = built
	c.order = append(c.order, key)
	return built, nil
}

// named returns the held sheet served under name, or nil.
func (c *productStylesheets) named(name string) *servedStylesheet {
	if sheet := defaultServedStylesheet(); sheet.name == name {
		return sheet
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, sheet := range c.byKey {
		if sheet.name == name {
			return sheet
		}
	}
	return nil
}

// viewerStylesheet builds the sheet of the signed-in viewer's stored theme. It
// answers a request for a customer sheet this process no longer holds (it was
// restarted, or the sheet was dropped) without the browser having to reload
// the document.
func (h *Handler) viewerStylesheet(ctx context.Context) *servedStylesheet {
	principal, _ := trust.FromContext(ctx)
	if h.preferences == nil || principal == nil {
		return nil
	}
	snapshot, err := h.preferences.Load(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject())
	if err != nil {
		return nil
	}
	theme := productui.NormalizeCustomerTheme(productui.EffectiveAppearance(productThemeFromPreference(snapshot.Theme.Theme), snapshot.User.Density))
	if productui.ValidateCustomerTheme(theme) != nil {
		return nil
	}
	sheet, err := h.stylesheets.forTheme(theme)
	if err != nil {
		return nil
	}
	return sheet
}

// serveProductStylesheet answers the address a workspace document links. The
// request is already admitted. The name is the digest of the bytes, so the
// answer never changes and the browser may keep it without asking again; the
// cache stays private like every other authenticated asset.
func (h *Handler) serveProductStylesheet(w http.ResponseWriter, r *http.Request, name string) {
	sheet := h.stylesheets.named(name)
	if sheet == nil {
		sheet = h.viewerStylesheet(r.Context())
	}
	if sheet == nil || sheet.name != name {
		h.writeProblem(w, http.StatusNotFound, "No such stylesheet",
			"This address names a stylesheet this workspace does not serve. Reload the page.")
		return
	}
	body, encoding := []byte(sheet.body), ""
	if acceptsGzip(r.Header.Get("Accept-Encoding")) {
		if compressed := sheet.compressed(); len(compressed) > 0 {
			body, encoding = compressed, "gzip"
		}
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Vary", "Accept-Encoding")
	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
	}
	w.Header().Set("ETag", sheet.etag)
	if matchesETag(r.Header.Get("If-None-Match"), sheet.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// productDocumentStyle chooses how a workspace document carries its sheet and
// returns the head element with the policy that admits exactly that. The
// sheet is linked whenever the policy can name its address. A host the policy
// cannot express (see sanitizeHostAuthority) keeps the inline sheet and its
// hash, as before.
func productDocumentStyle(host string, sheet *servedStylesheet, allowGiphy bool) (element, policy string) {
	if sanitizeHostAuthority(host) == "" {
		return sheet.inlineElement(), productContentSecurityPolicyForHashAndGiphy(host, sheet.integrity, allowGiphy)
	}
	return sheet.linkElement(), productContentSecurityPolicyForStyleAsset(host, sheet.name, allowGiphy)
}

// cspStyleAssetSource is the one stylesheet address a linking document admits:
// the sanitized authority and the exact asset path. A path source without a
// trailing slash matches only that path, so no other stylesheet on the origin
// is admitted. An omitted scheme follows the document's own.
func cspStyleAssetSource(rawHost, name string) string {
	if !productStylesheetAssetName(name) {
		return ""
	}
	authority := sanitizeHostAuthority(rawHost)
	if authority == "" {
		return ""
	}
	return authority + PathAssetPrefix + name
}

// CHATBUG-014: the document itself is compressed too. Without its stylesheet
// it is about 95 KB of markup and configuration, which gzip takes to about a
// sixth.
//
// The document carries the session credential and reflects request text (the
// menu query), which is the combination a compression side channel needs. That
// attack depends on another site making the browser issue the requests, so the
// document is compressed only for a request the browser itself reports as
// coming from this origin or from the person (address bar, bookmark, reload).
// A request from another site, or from a browser that does not say, is
// answered uncompressed.

// documentCompressible reports whether r may be answered with a compressed
// document.
func documentCompressible(r *http.Request) bool {
	if r == nil || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
		return false
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	default:
		return false
	}
}

// writeProductDocument writes a workspace document under the security headers
// every workspace response carries, compressed when the request allows it.
func writeProductDocument(w http.ResponseWriter, r *http.Request, status int, doc, policy string) {
	setHTMLSecurityHeaders(w, policy)
	w.Header().Set("Vary", "Accept-Encoding, Sec-Fetch-Site")
	if documentCompressible(r) {
		var out bytes.Buffer
		writer, err := gzip.NewWriterLevel(&out, gzip.DefaultCompression)
		if err == nil {
			_, err = writer.Write([]byte(doc))
		}
		if err == nil {
			err = writer.Close()
		}
		if err == nil {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Length", strconv.Itoa(out.Len()))
			w.WriteHeader(status)
			_, _ = w.Write(out.Bytes())
			return
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(doc))
}
