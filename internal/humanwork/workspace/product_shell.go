package workspace

import (
	"encoding/json"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

const (
	// PathProductPrefix is the authenticated production UI subtree.
	PathProductPrefix = "/workspace/app/"
	// PathProductHome is the default human-facing page for this cell.
	PathProductHome = PathProductPrefix + "home"
)

// serveProduct serves a CSP-pinned shell. The existing Go/WASM client loads
// the reusable productui tree and calls the canonical JourneyService through
// the cell's gRPC-over-WebSocket tunnel; no JSON shadow API is introduced.
func (h *Handler) serveProduct(w http.ResponseWriter, r *http.Request) {
	definition, ok := productui.LookupRoute(r.URL.Path)
	if !ok {
		h.serveNotFound(w, r)
		return
	}
	admitted, ok := h.admit(w, r)
	if !ok {
		return
	}
	principal, _ := trust.FromContext(admitted.Context())
	config := JourneyConfig{
		TunnelURL: h.tunnelURL(r), Bearer: normalizeBearerInput(BearerFromRequest(r, h.devBrowserLogin)),
		Roles: []string{}, JourneysPath: PathJourney,
	}
	if h.devBrowserLogin {
		config.LogoutPath = PathLogout
	}
	if principal != nil {
		config.Tenant = string(principal.Tenant())
		config.Subject = principal.Subject()
		config.Roles = principal.Roles()
		config.Purpose = principal.DefaultPurpose()
	}
	permissionConfigured := false
	if principal != nil && h.roleAccess != nil {
		snapshot, loadErr := h.roleAccess.Load(admitted.Context(), principal.Tenant(), principal.OrganizationScopeID())
		if loadErr != nil {
			h.writeProblem(w, http.StatusServiceUnavailable, "Access unavailable", "The role policy could not be resolved for this page.")
			return
		}
		if len(snapshot.PagePermissions) > 0 {
			permissionConfigured = true
			config.Roles = roleaccess.AssignedRoles(snapshot, principal.Subject(), principal.Roles())
			config.PagePermissions = roleaccess.EffectivePagePermissions(snapshot, config.Roles)
		}
	}
	allowed := productui.PageVisible(definition.ID, config.Roles)
	if permissionConfigured {
		allowed = roleaccess.CanPageAction(config.PagePermissions, string(definition.ID), roleaccess.ActionView)
	}
	if !allowed {
		h.writeProblem(w, http.StatusForbidden, "Page unavailable", "Your current role does not grant access to this workspace page.")
		return
	}
	locale := productui.ResolveProductLocale(r.URL.Query().Get("locale"))
	doc, err := productShellDocumentForRouteQuery(config, JourneyBundleBuilt(), locale, definition.ID, r.URL.Query().Get("menu_q"))
	if err != nil {
		h.writeProblem(w, http.StatusInternalServerError, "Workspace unavailable", err.Error())
		return
	}
	writeHTMLDocument(w, http.StatusOK, doc, ProductContentSecurityPolicy(h.policyHost(r)))
}

func productShellDocument(config JourneyConfig, bundleBuilt bool) (string, error) {
	return productShellDocumentForLocale(config, bundleBuilt, productui.ResolveProductLocale(""))
}

func productShellDocumentForLocale(config JourneyConfig, bundleBuilt bool, locale productui.LocaleContext) (string, error) {
	return productShellDocumentForRoute(config, bundleBuilt, locale, productui.PageHome)
}

func productShellDocumentForRoute(config JourneyConfig, bundleBuilt bool, locale productui.LocaleContext, page productui.PageID) (string, error) {
	return productShellDocumentForRouteQuery(config, bundleBuilt, locale, page, "")
}

func productShellDocumentForRouteQuery(config JourneyConfig, bundleBuilt bool, locale productui.LocaleContext, page productui.PageID, menuQuery string) (string, error) {
	island, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="` + html.EscapeString(locale.Resolved) + `" dir="` + html.EscapeString(string(locale.Direction)) + `" data-hcm-locale="` + html.EscapeString(locale.Resolved) + `" data-hcm-catalog="` + html.EscapeString(locale.CatalogVersion) + `"`)
	if locale.Fallback != productui.LocaleFallbackNone {
		b.WriteString(` data-hcm-locale-fallback="` + html.EscapeString(string(locale.Fallback)) + `"`)
	}
	if missing := len(productui.MissingProductTranslations(locale.Resolved)); missing > 0 {
		b.WriteString(` data-hcm-message-fallback="en-US" data-hcm-message-fallback-count="` + strconv.Itoa(missing) + `"`)
	}
	appearance := productui.CustomerThemeAttributes(productui.DefaultCustomerTheme())
	for _, name := range []string{"data-hcm-color-mode", "data-hcm-palette", "data-hcm-shape", "data-hcm-density", "data-hcm-glyphs", "data-hcm-typeface", "data-hcm-navigation", "data-hcm-motion"} {
		b.WriteString(` ` + name + `="` + html.EscapeString(appearance[name]) + `"`)
	}
	accessibility := productui.AccessibilityPreferenceAttributes(productui.DefaultAccessibilityPreferences())
	for _, name := range []string{"data-hcm-text-size", "data-hcm-contrast", "data-hcm-motion-preference", "data-hcm-links"} {
		b.WriteString(` ` + name + `="` + html.EscapeString(accessibility[name]) + `"`)
	}
	b.WriteString(`><head><meta charset="utf-8"><meta name="color-scheme" content="light dark">`)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	b.WriteString("<title>Human Capital Management Suite</title><style>")
	b.WriteString(productStylesheet())
	b.WriteString("</style></head><body>")
	b.WriteString(`<div id="` + JourneyRootElementID + `">`)
	if bundleBuilt {
		// UXAUDIT-007: config.Purpose is the admitted principal's authorized
		// data-processing purpose (a GDPR-style processing reason), not the
		// shell's "authorized scope" -- feeding it here made e.g.
		// "Compensation Review" persist as a global scope badge on every
		// page, unrelated pages included. NewView has no other authorized-
		// scope fact to offer, so this passes the honest empty value and
		// lets ResolvePageIdentity's own "Workspace access" fallback
		// stand in rather than mislabeling a purpose as a scope. The
		// purpose itself still reaches the one place GREEN says it belongs:
		// the Promotion journey experience's own masthead
		// (tools/uxqual/render/journey's principalChip), fed independently
		// through journeyclient.Config/journeyApp in
		// tools/uxqual/cmd/journeywasm/product_wasm.go, untouched by this
		// SSR loading shell.
		view := productui.NewView(page, productui.DisplayLabel(config.Tenant), productui.DisplayLabel(config.Subject), "")
		// Hydration preserves live input values. Seed the request's query in the
		// loading shell so an empty SSR value cannot hide an active client filter.
		view.MenuQuery = strings.TrimSpace(menuQuery)
		view.LogoutHref = config.LogoutPath
		view = productui.ApplyRoleVisibility(view, config.Roles)
		if len(config.PagePermissions) > 0 {
			view = productui.ApplyPagePermissions(view, productPagePermissions(config.PagePermissions))
		}
		view = productui.ApplyLocale(view, locale)
		loading, renderErr := ui.RenderToString(productui.BuildLoading(view))
		if renderErr != nil {
			return "", renderErr
		}
		b.WriteString(loading)
	} else {
		b.WriteString(`<main class="main"><section class="surface empty-state" role="status"><h1>` + html.EscapeString(locale.Text("shell.connecting")) + `</h1><p class="muted">` + html.EscapeString(locale.Text("shell.loading_authorized")) + `</p>`)
		b.WriteString(`<p>This build carries no browser client. Build it with <code>`)
		b.WriteString(html.EscapeString(journeyBuildCommand))
		b.WriteString(`</code>.</p>`)
		b.WriteString(`</section></main>`)
	}
	b.WriteString(`</div>`)
	b.WriteString(`<script type="application/json" id="` + JourneyConfigElementID + `">`)
	b.Write(island)
	b.WriteString("</script>")
	if bundleBuilt {
		b.WriteString("<script>" + journeyLoaderSource + "</script>")
	}
	b.WriteString("</body></html>")
	return b.String(), nil
}

func productPagePermissions(values []roleaccess.PagePermission) []productui.RolePagePermission {
	result := make([]productui.RolePagePermission, 0, len(values))
	for _, value := range values {
		result = append(result, productui.RolePagePermission{Version: value.Version, RoleID: value.RoleID, Page: productui.PageID(value.PageID), View: value.View, Create: value.Create, Update: value.Update, Delete: value.Delete})
	}
	return result
}

func productStylesheet() string {
	// Journey CSS comes first so the product shell retains ownership of
	// global layout while the prefixed journey components keep their tokens.
	return journey.Stylesheet() + productui.Stylesheet()
}

var productStylesheetHash = sha256Source(productStylesheet())

// ProductContentSecurityPolicy allows exactly the shared Go/WASM loader, the
// same-origin gRPC tunnel, and the product component stylesheet.
func ProductContentSecurityPolicy(host string) string {
	return cspPolicy{
		styleHashes:           []string{productStylesheetHash},
		scriptHash:            journeyLoaderHash,
		formActionSelf:        true,
		connectHost:           host,
		allowAssetConnections: true,
		allowTunnelConnection: true,
		sameOriginImages:      true,
		allowBlobScript:       true,
		allowWASM:             true,
	}.header()
}
