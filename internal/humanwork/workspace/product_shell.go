package workspace

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
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
		Roles: []string{}, JourneysPath: PathJourney, GiphyAPIKey: h.giphyAPIKey,
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
	access, loadErr := h.resolveProductAccess(admitted.Context(), principal)
	if loadErr != nil {
		h.writeProblem(w, http.StatusServiceUnavailable, "Access unavailable", "The role policy could not be resolved for this page.")
		return
	}
	config.Roles, config.PagePermissions = access.roles, access.permissions
	if access.featuresConfigured {
		config.FeaturePermissions = access.features
	}
	config.LauncherActions = resolveProductLauncherActions(access.configured, access.permissions)
	if !access.can(definition.ID, roleaccess.ActionView) && !assignedJourneyDetail(definition.ID, r.URL.Query(), access) {
		h.writeProblem(w, http.StatusForbidden, "Page unavailable", "Your current role does not grant access to this workspace page.")
		return
	}
	// REV-067-01: resolve the served page through the governed rollout
	// ledger before rendering. A retired page or a scope with no live
	// rollout is never served; an ungoverned page falls back to the compiled
	// registry definition. A governed page carries its rolled-out revision
	// digest so the served definition is pinned to the ledger.
	revisionScope := ""
	if principal != nil {
		revisionScope = principal.OrganizationScopeID()
		if revisionScope == "" {
			revisionScope = string(principal.Tenant())
		}
	}
	if revision, governed, servable, reason := h.resolveGovernedRevision(definition.ID, revisionScope, h.now().Unix()); governed {
		if !servable {
			h.writeProblem(w, http.StatusNotFound, "Page unavailable", reason)
			return
		}
		w.Header().Set("X-Page-Revision-Digest", revision.Digest)
	}
	query := r.URL.Query()
	locale := productui.ResolveProductLocale(query.Get("locale"))
	nav := ""
	if values := query["nav"]; len(values) == 1 && values[0] == "collapsed" {
		nav = "collapsed"
	}
	appearance := productui.DefaultCustomerTheme()
	accessibility := productui.DefaultAccessibilityPreferences()
	if h.preferences != nil && principal != nil {
		snapshot, loadErr := h.preferences.Load(admitted.Context(), principal.Tenant(), principal.OrganizationScopeID(), principal.Subject())
		if loadErr != nil {
			h.writeProblem(w, http.StatusServiceUnavailable, "Appearance unavailable", "Organization appearance could not be loaded.")
			return
		}
		// REV-092-01: the person's own density overrides the organization's
		// on first paint, so the page does not reflow when the client loads.
		appearance = productui.EffectiveAppearance(productThemeFromPreference(snapshot.Theme.Theme), snapshot.User.Density)
		// The same read already returned this person's own accessibility
		// preferences, and the document used to discard them and render the
		// defaults. Someone who reads at large text or needs more contrast
		// therefore got standard text and system contrast on first paint,
		// then watched the page reflow to their setting once the client
		// loaded -- on every navigation.
		stored := snapshot.User.Accessibility
		accessibility = productui.NormalizeAccessibilityPreferences(productui.AccessibilityPreferences{
			TextSize: stored.TextSize, Contrast: stored.Contrast, Motion: stored.Motion, Links: stored.Links,
		})
	}
	appearance = productui.NormalizeCustomerTheme(appearance)
	if productui.ValidateCustomerTheme(appearance) != nil {
		appearance = productui.DefaultCustomerTheme()
	}
	stylesheet, err := productStylesheetForTheme(appearance)
	if err != nil {
		h.writeProblem(w, http.StatusServiceUnavailable, "Appearance unavailable", "Organization appearance could not be qualified.")
		return
	}
	doc, err := productShellDocumentForRouteStateWithPreferences(config, JourneyBundleBuilt(), locale, definition.ID, query.Get("menu_q"), nav, appearance, accessibility, stylesheet)
	if err != nil {
		h.writeProblem(w, http.StatusInternalServerError, "Workspace unavailable", err.Error())
		return
	}
	writeHTMLDocument(w, http.StatusOK, doc, productContentSecurityPolicyForStylesheetAndGiphy(h.policyHost(r), stylesheet, h.giphyAPIKey != ""))
}

func productThemeFromPreference(value preferences.Theme) productui.CustomerTheme {
	return productui.CustomerTheme{BrandName: value.BrandName, BrandMark: value.BrandMark, BrandLogoURL: value.BrandLogoURL,
		ColorMode: value.ColorMode, Palette: value.Palette, Shape: value.Shape, Density: value.Density,
		Glyphs: value.Glyphs, Typeface: value.Typeface, Navigation: value.Navigation, Motion: value.Motion,
		TokenOverrides: value.TokenOverrides, DarkTokenOverrides: value.DarkTokenOverrides}
}

// productAccess is the server-resolved policy the login cards and the product
// shell both consume. A configured but empty effective grant remains a denial;
// it must never fall back to the credential's broader role claims.
type productAccess struct {
	roles              []string
	permissions        []roleaccess.PagePermission
	features           []roleaccess.FeaturePermission
	configured         bool
	featuresConfigured bool
}

func (h *Handler) resolveProductAccess(ctx context.Context, principal *trust.Principal) (productAccess, error) {
	if principal == nil {
		return productAccess{}, nil
	}
	access := productAccess{roles: principal.Roles()}
	if h.roleAccess == nil {
		return access, nil
	}
	snapshot, err := h.roleAccess.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
	if err != nil {
		return productAccess{}, err
	}
	access.configured = true
	access.roles = roleaccess.AssignedRoles(snapshot, principal.Subject(), access.roles)
	// Page grants come only from the durable policy. New-page defaults are
	// inserted by roleaccessstore.Bootstrap, never synthesized during a read:
	// an empty or restricted stored policy must remain a denial.
	access.permissions = roleaccess.EffectivePagePermissions(snapshot, access.roles)
	access.featuresConfigured = len(snapshot.FeaturePermissions) > 0
	access.features = roleaccess.EffectiveFeaturePermissions(snapshot, access.roles)
	return access, nil
}

func (access productAccess) can(page productui.PageID, action string) bool {
	if access.configured {
		if !access.featuresConfigured {
			return roleaccess.CanPageAction(access.permissions, string(page), action)
		}
		featureID := string(productui.FeatureActions)
		if action == roleaccess.ActionView {
			featureID = string(productui.FeatureContent)
		}
		return roleaccess.CanFeatureAction(access.permissions, access.features, string(page), featureID, action)
	}
	return action == roleaccess.ActionView && productui.PageVisible(page, access.roles)
}

// assignedJourneyDetail admits one journey's detail to a viewer who has My
// Work but not the Journeys page, the same pair InspectJourney accepts
// (journeys/journey_detail or work/assigned_queue). A finance approver's
// only way to decide the review routed to them is that detail; refusing the
// route here left "Open live journey" on My Work leading to "Page
// unavailable". Only the detail is admitted, never the journeys list, and
// every read and decision on it is still authorized by the RPC.
func assignedJourneyDetail(page productui.PageID, query map[string][]string, access productAccess) bool {
	if page != productui.PageJourneys || len(query["journey"]) != 1 || strings.TrimSpace(query["journey"][0]) == "" {
		return false
	}
	return access.can(productui.PageWork, roleaccess.ActionView)
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
	return productShellDocumentForRouteState(config, bundleBuilt, locale, page, menuQuery, "")
}

// Explicit route state may shape the loading chrome, but never grants access
// or claims a server-stored preference that has not yet been loaded.
func productShellDocumentForRouteState(config JourneyConfig, bundleBuilt bool, locale productui.LocaleContext, page productui.PageID, menuQuery, nav string) (string, error) {
	return productShellDocumentForRouteStateWithTheme(config, bundleBuilt, locale, page, menuQuery, nav, productui.DefaultCustomerTheme(), productStylesheet())
}

func productShellDocumentForRouteStateWithTheme(config JourneyConfig, bundleBuilt bool, locale productui.LocaleContext, page productui.PageID, menuQuery, nav string, theme productui.CustomerTheme, stylesheet string) (string, error) {
	return productShellDocumentForRouteStateWithPreferences(config, bundleBuilt, locale, page, menuQuery, nav, theme, productui.DefaultAccessibilityPreferences(), stylesheet)
}

// productShellDocumentForRouteStateWithPreferences renders the shell with the
// stored theme and the stored accessibility preferences on <html>, which is
// what makes the first paint final: the client adopts these attributes rather
// than replacing them (see browserThemeController.Reapply).
func productShellDocumentForRouteStateWithPreferences(config JourneyConfig, bundleBuilt bool, locale productui.LocaleContext, page productui.PageID, menuQuery, nav string, theme productui.CustomerTheme, accessibilityPreferences productui.AccessibilityPreferences, stylesheet string) (string, error) {
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
	appearance := productui.CustomerThemeAttributes(theme)
	for _, name := range []string{"data-hcm-color-mode", "data-hcm-palette", "data-hcm-shape", "data-hcm-density", "data-hcm-glyphs", "data-hcm-typeface", "data-hcm-navigation", "data-hcm-motion"} {
		b.WriteString(` ` + name + `="` + html.EscapeString(appearance[name]) + `"`)
	}
	accessibility := productui.AccessibilityPreferenceAttributes(accessibilityPreferences)
	for _, name := range []string{"data-hcm-text-size", "data-hcm-contrast", "data-hcm-motion-preference", "data-hcm-links"} {
		b.WriteString(` ` + name + `="` + html.EscapeString(accessibility[name]) + `"`)
	}
	b.WriteString(`><head><meta charset="utf-8"><meta name="color-scheme" content="` + html.EscapeString(productui.ColorSchemeContent(appearance["data-hcm-color-mode"])) + `">`)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	b.WriteString("<title>Human Capital Management Suite</title><style>")
	b.WriteString(stylesheet)
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
		view.Appearance = theme
		// Hydration preserves live input values. Seed the request's query in the
		// loading shell so an empty SSR value cannot hide an active client filter.
		view.MenuQuery = strings.TrimSpace(menuQuery)
		view.NavCollapsed = nav == "collapsed"
		view.LogoutHref = config.LogoutPath
		view = productui.ApplyRoleVisibility(view, config.Roles)
		if len(config.PagePermissions) > 0 {
			view = productui.ApplyPagePermissions(view, productPagePermissions(config.PagePermissions))
		}
		if config.FeaturePermissions != nil {
			view = productui.ApplyFeaturePermissions(view, productFeaturePermissions(config.FeaturePermissions))
		}
		view.LauncherActions = productLauncherActions(config.LauncherActions)
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

func resolveProductLauncherActions(permissionConfigured bool, permissions []roleaccess.PagePermission) []LauncherActionConfig {
	if !permissionConfigured {
		return nil
	}
	if !roleaccess.CanPageAction(permissions, string(productui.PagePeople), roleaccess.ActionView) ||
		!roleaccess.CanPageAction(permissions, string(productui.PageJourneys), roleaccess.ActionCreate) {
		return nil
	}
	return []LauncherActionConfig{{
		ID: productui.SemanticActionPromoteWorker, Availability: string(productui.ActionAvailable),
	}}
}

func productLauncherActions(values []LauncherActionConfig) []productui.LauncherActionProjection {
	result := make([]productui.LauncherActionProjection, 0, len(values))
	for _, value := range values {
		result = append(result, productui.LauncherActionProjection{
			ID: value.ID,
			State: productui.ActionState{
				Availability: productui.ActionAvailability(value.Availability),
				Reason:       value.Reason,
				Recovery: productui.ActionLinkProps{
					Label: value.RecoveryLabel,
					Href:  value.RecoveryHref,
				},
			},
			Priority: value.Priority,
		})
	}
	return result
}

func productPagePermissions(values []roleaccess.PagePermission) []productui.RolePagePermission {
	result := make([]productui.RolePagePermission, 0, len(values))
	for _, value := range values {
		result = append(result, productui.RolePagePermission{Version: value.Version, RoleID: value.RoleID, Page: productui.PageID(value.PageID), View: value.View, Create: value.Create, Update: value.Update, Delete: value.Delete})
	}
	return result
}

func productFeaturePermissions(values []roleaccess.FeaturePermission) []productui.RoleFeaturePermission {
	result := make([]productui.RoleFeaturePermission, 0, len(values))
	for _, value := range values {
		result = append(result, productui.RoleFeaturePermission{
			Version: value.Version, RoleID: value.RoleID, Page: productui.PageID(value.PageID), Feature: productui.FeatureID(value.FeatureID),
			View: value.View, Create: value.Create, Update: value.Update, Delete: value.Delete,
		})
	}
	return result
}

func productStylesheet() string {
	// Journey CSS comes first so the product shell retains ownership of
	// global layout while the prefixed journey components keep their tokens.
	return journey.Stylesheet() + productui.Stylesheet()
}

func productStylesheetForTheme(theme productui.CustomerTheme) (string, error) {
	if theme.Palette != "custom" {
		return productStylesheet(), nil
	}
	stylesheet, err := productui.StylesheetForCustomerTheme(theme)
	if err != nil {
		return "", err
	}
	return journey.Stylesheet() + stylesheet, nil
}

var productStylesheetHash = sha256Source(productStylesheet())

// ProductContentSecurityPolicy allows exactly the shared Go/WASM loader, the
// same-origin gRPC tunnel, and the product component stylesheet.
func ProductContentSecurityPolicy(host string) string {
	return productContentSecurityPolicyForHash(host, productStylesheetHash)
}

func productContentSecurityPolicyForStylesheet(host, stylesheet string) string {
	return productContentSecurityPolicyForHash(host, sha256Source(stylesheet))
}

func productContentSecurityPolicyForStylesheetAndGiphy(host, stylesheet string, allowGiphy bool) string {
	return productContentSecurityPolicyForHashAndGiphy(host, sha256Source(stylesheet), allowGiphy)
}

func productContentSecurityPolicyForHash(host, stylesheetHash string) string {
	return productContentSecurityPolicyForHashAndGiphy(host, stylesheetHash, false)
}

func productContentSecurityPolicyForHashAndGiphy(host, stylesheetHash string, allowGiphy bool) string {
	return cspPolicy{
		styleHashes:           []string{stylesheetHash},
		scriptHash:            journeyLoaderHash,
		formActionSelf:        true,
		connectHost:           host,
		allowAssetConnections: true,
		allowTunnelConnection: true,
		allowMediaConnections: true,
		allowGiphy:            allowGiphy,
		sameOriginImages:      true,
		blobImages:            true,
		allowBlobScript:       true,
		allowWASM:             true,
	}.header()
}
