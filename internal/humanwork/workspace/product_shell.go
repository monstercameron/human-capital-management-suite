package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	gwchtml "github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
	journey "github.com/monstercameron/human-capital-management-suite/internal/experience/journeycss"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
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
	if productLegacyAgentsPath(r.URL.Path) {
		location := productui.Path(productui.PageAgents)
		if r.URL.RawQuery != "" {
			location += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, location, http.StatusSeeOther)
		return
	}
	definition, ok := productui.LookupRoute(r.URL.Path)
	if !ok {
		h.serveProductNotFound(w, r)
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
		h.applyCompanyBrand(&config)
	}
	access, loadErr := h.resolveProductAccess(admitted.Context(), principal)
	if loadErr != nil {
		h.writeProductProblem(w, r, http.StatusServiceUnavailable, productui.ResolveProductLocalePreference(r.URL.Query().Get("locale"), ""), definition.ID, config)
		return
	}
	config.Roles, config.PagePermissions = access.roles, access.permissions
	if h.personaAdmin != nil {
		config.PersonaAdminClient = h.personaAdmin.ClientForRequest(admitted.Context())
		if config.PersonaAdminClient != nil && definition.ID == productui.PagePersonaAdmin && access.can(definition.ID, roleaccess.ActionView) {
			snapshot, snapshotErr := config.PersonaAdminClient.Snapshot(admitted.Context(), productui.PersonaAdminSnapshotRequest{
				TenantID: config.Tenant, Principal: config.Subject,
			})
			if snapshotErr == nil && snapshot.Available {
				config.PersonaAdminSnapshot = &snapshot
			} else {
				h.logPersonaAdminSnapshotFailure(snapshotErr)
			}
		}
	}
	if access.featuresConfigured {
		config.FeaturePermissions = access.features
	}
	config.LauncherActions = resolveProductLauncherActions(access.configured, access.permissions)
	config.WorkflowStarts = h.resolveWorkflowStarts(admitted.Context(), principal, access)
	config.Agents = h.resolveAgents(admitted.Context(), principal, access)
	if config.Agents != nil && config.Agents.Enabled {
		access = projectAgentOperationsAccess(access)
		config.Roles, config.PagePermissions = access.roles, access.permissions
	}
	config.Clock = h.resolveClock(access)
	if !access.can(definition.ID, roleaccess.ActionView) && !assignedJourneyDetail(definition.ID, r.URL.Query(), access) {
		theme, accessibility := h.productProblemAppearance(admitted.Context(), principal, config)
		h.writeProductProblemWithAppearance(w, r, http.StatusForbidden, productui.ResolveProductLocalePreference(r.URL.Query().Get("locale"), ""), definition.ID, config, theme, accessibility)
		return
	}
	if definition.ID == productui.PageHistory {
		location := PathProductPrefix + "workflows/history"
		if r.URL.RawQuery != "" {
			location += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, location, http.StatusSeeOther)
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
	revision, governed, servable, _, governanceErr := h.resolveGovernedRevision(admitted.Context(), config.Tenant, definition.ID, revisionScope, h.now().Unix())
	if governanceErr != nil {
		h.writeProductProblem(w, r, http.StatusServiceUnavailable, productui.ResolveProductLocalePreference(r.URL.Query().Get("locale"), ""), definition.ID, config)
		return
	}
	if governed {
		if !servable {
			h.writeProductProblem(w, r, http.StatusNotFound, productui.ResolveProductLocalePreference(r.URL.Query().Get("locale"), ""), definition.ID, config)
			return
		}
		w.Header().Set("X-Page-Revision-Digest", revision.Digest)
	}
	query := r.URL.Query()
	locale := productui.ResolveProductLocalePreference(query.Get("locale"), "")
	nav := ""
	if values := query["nav"]; len(values) == 1 {
		switch values[0] {
		case "collapsed", "expanded":
			nav = values[0]
		}
	}
	appearance := productui.DefaultCustomerTheme()
	accessibility := productui.DefaultAccessibilityPreferences()
	savedLocale := ""
	var favoritePages []productui.PageID
	var navigationGroups map[productui.PageID]bool
	if h.preferences != nil && principal != nil {
		snapshot, loadErr := h.preferences.Load(admitted.Context(), principal.Tenant(), principal.OrganizationScopeID(), principal.Subject())
		if loadErr != nil {
			h.writeProductProblem(w, r, http.StatusServiceUnavailable, productui.ResolveProductLocalePreference(query.Get("locale"), ""), definition.ID, config)
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
			TextSize: stored.TextSize, Contrast: stored.Contrast, Motion: stored.Motion, Links: stored.Links, ColorMode: stored.ColorMode,
		})
		savedLocale = snapshot.User.Locale
		favoritePages = make([]productui.PageID, 0, len(snapshot.User.FavoritePages))
		for _, page := range snapshot.User.FavoritePages {
			favoritePages = append(favoritePages, productui.PageID(page))
		}
		navigationGroups = make(map[productui.PageID]bool, len(snapshot.User.NavigationGroups))
		for page, open := range snapshot.User.NavigationGroups {
			navigationGroups[productui.PageID(page)] = open
		}
	}
	if config.TenantLogo != "" && appearance.BrandLogoURL == "" {
		// A multi-company workspace shows the signed-in company's own logo
		// until an administrator configures one.
		appearance.BrandLogoURL = config.TenantLogo
	}
	if config.TenantName != "" && appearance.BrandName == productui.DefaultCustomerTheme().BrandName {
		// Seed the loading shell with the same company identity the client will
		// receive after its preference read. This keeps the header and sidebar
		// geometry stable while the stored appearance is being adopted.
		appearance.BrandName = config.TenantName
		appearance.BrandMark = config.TenantMark
	}
	locale = productui.ResolveProductLocalePreference(query.Get("locale"), savedLocale)
	if h.catalogs != nil && config.Tenant != "" {
		revision, catalogErr := h.catalogs.Active(admitted.Context(), i18n.Scope{Tenant: config.Tenant, Product: "workspace"}, locale.Resolved, h.now().UTC())
		if catalogErr != nil && !errors.Is(catalogErr, i18n.ErrNoActiveRevision) {
			h.writeProductProblem(w, r, http.StatusServiceUnavailable, locale, definition.ID, config)
			return
		}
		if catalogErr == nil {
			locale, catalogErr = productui.ApplyActivatedCatalog(locale, i18n.Scope{Tenant: config.Tenant, Product: "workspace"}, revision, h.now().UTC())
			if catalogErr != nil {
				h.writeProductProblem(w, r, http.StatusServiceUnavailable, productui.ResolveProductLocalePreference(query.Get("locale"), savedLocale), definition.ID, config)
				return
			}
			config.CatalogLocale = locale.Resolved
			config.CatalogVersion = locale.CatalogVersion
			config.CatalogRevision = revision.ID
			config.CatalogDigest = revision.CanonicalDigest
			config.CatalogMessages = make(map[string]string, len(revision.Translations))
			at := h.now().UTC()
			for _, translation := range revision.Translations {
				if (translation.EffectiveFrom.IsZero() || !at.Before(translation.EffectiveFrom)) && (translation.EffectiveUntil.IsZero() || at.Before(translation.EffectiveUntil)) {
					config.CatalogMessages[translation.Key] = translation.Text
				}
			}
		}
	}
	appearance = productui.NormalizeCustomerTheme(appearance)
	if productui.ValidateCustomerTheme(appearance) != nil {
		appearance = productui.DefaultCustomerTheme()
	}
	stylesheet, err := productStylesheetForTheme(appearance)
	if err != nil {
		h.writeProductProblem(w, r, http.StatusServiceUnavailable, locale, definition.ID, config)
		return
	}
	doc, err := productShellDocumentForRouteStateWithPreferencesAndNavigation(config, JourneyBundleBuilt(), locale, definition.ID, query.Get("menu_q"), nav, appearance, accessibility, stylesheet, favoritePages, navigationGroups)
	if err != nil {
		h.writeProductProblem(w, r, http.StatusInternalServerError, locale, definition.ID, config)
		return
	}
	writeHTMLDocument(w, http.StatusOK, doc, productContentSecurityPolicyForStylesheetAndGiphy(h.policyHost(r), stylesheet, h.giphyAPIKey != ""))
}

func productLegacyAgentsPath(path string) bool {
	return path == "/workspace/app/agents" || path == "/workspace/app/agents/"
}

func (h *Handler) serveProductNotFound(w http.ResponseWriter, r *http.Request) {
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
		h.applyCompanyBrand(&config)
	}
	access, err := h.resolveProductAccess(admitted.Context(), principal)
	if err != nil {
		h.writeProblem(w, http.StatusNotFound, "No such workspace route", "This page does not exist.")
		return
	}
	config.Roles, config.PagePermissions = access.roles, access.permissions
	if access.featuresConfigured {
		config.FeaturePermissions = access.features
	}
	config.LauncherActions = resolveProductLauncherActions(access.configured, access.permissions)
	config.WorkflowStarts = h.resolveWorkflowStarts(admitted.Context(), principal, access)
	config.Agents = h.resolveAgents(admitted.Context(), principal, access)
	if config.Agents != nil && config.Agents.Enabled {
		access = projectAgentOperationsAccess(access)
		config.Roles, config.PagePermissions = access.roles, access.permissions
	}
	config.Clock = h.resolveClock(access)
	locale := productui.ResolveProductLocalePreference(r.URL.Query().Get("locale"), "")
	theme, accessibility := h.productProblemAppearance(admitted.Context(), principal, config)
	stylesheet, styleErr := productStylesheetForTheme(theme)
	if styleErr != nil {
		theme = productui.DefaultCustomerTheme()
		stylesheet = productStylesheet()
	}
	actions := []ui.Node{
		gwchtml.A(gwchtml.Props{Class: "button primary", Href: productui.Path(productui.PageHome)}, ui.Text(productUnknownText(locale, "home"))),
	}
	if closest, ok := closestProductRouteByLastSegment(r.URL.Path); ok {
		actions = append(actions, gwchtml.A(gwchtml.Props{Class: "button secondary", Href: productui.Path(closest.ID)}, ui.Text(locale.Text(closest.LabelKey))))
	}
	problem := gwchtml.Section(gwchtml.Props{Class: "surface empty-state workspace-page-problem", Role: "alert", Aria: map[string]string{"labelledby": "workspace-problem-title"}},
		gwchtml.H1(gwchtml.Props{ID: "workspace-problem-title"}, ui.Text(productUnknownText(locale, "title"))),
		gwchtml.P(gwchtml.Props{Class: "workspace-problem-message"}, ui.Text(productUnknownText(locale, "message"))),
		gwchtml.Div(gwchtml.Props{Class: "action-row"}, actions...),
	)
	doc, err := productShellProblemDocument(config, locale, productui.PageHome, problem, theme, accessibility, stylesheet)
	if err != nil {
		http.Error(w, productUnknownText(locale, "title"), http.StatusNotFound)
		return
	}
	writeHTMLDocument(w, http.StatusNotFound, doc, productContentSecurityPolicyForStylesheet(h.policyHost(r), stylesheet))
}

func closestProductRouteByLastSegment(path string) (productui.PageDefinition, bool) {
	wanted := lastProductPathSegment(path)
	if wanted == "" {
		return productui.PageDefinition{}, false
	}
	for _, definition := range productui.PageDefinitions() {
		if lastProductPathSegment(definition.Route) == wanted {
			return definition, true
		}
	}
	return productui.PageDefinition{}, false
}

func lastProductPathSegment(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func productUnknownText(locale productui.LocaleContext, key string) string {
	// One table, shared with the client's not-found state.
	return productui.RouteStateText(locale, key)
}

func (h *Handler) logPersonaAdminSnapshotFailure(err error) {
	if h == nil || h.config.Logger == nil {
		return
	}
	stage := "snapshot_unavailable"
	if err != nil {
		var staged interface{ PersonaCatalogFailureStage() string }
		if errors.As(err, &staged) {
			switch staged.PersonaCatalogFailureStage() {
			case "authorization", "persona_store", "versions", "installations", "targets", "target_rooms", "target_memberships", "target_directory", "target_directory_identity", "target_directory_label", "member_identity_read", "member_identity_absent", "member_identity_malformed", "member_facts_read", "member_facts_scope", "member_facts_absent", "member_name_absent", "target_data", "target_authority", "target_validation", "installation_validation", "profile_validation", "skill_resolution", "preview":
				stage = staged.PersonaCatalogFailureStage()
			}
		}
	}
	h.config.Logger.LogRequest(transport.LogRecord{
		Method: "GET /workspace/app/persona-admin", Transport: transport.KindHTTPEdge,
		ReasonRef: "PERSONA_ADMIN_SNAPSHOT_UNAVAILABLE", ErrorType: stage, Failed: true,
	})
}

// writeProductProblem renders a product-context error in the locale already
// trusted by this request. Before the preference snapshot is available, the
// caller supplies only a supported URL locale or the English default. In
// particular, a failed preference read never invents a locale from a cookie,
// header, tenant setting, or another principal's record.
func (h *Handler) writeProductProblem(w http.ResponseWriter, r *http.Request, status int, locale productui.LocaleContext, page productui.PageID, config JourneyConfig) {
	theme, accessibility := productProblemAppearance(config)
	h.writeProductProblemWithAppearance(w, r, status, locale, page, config, theme, accessibility)
}

func (h *Handler) writeProductProblemWithAppearance(w http.ResponseWriter, r *http.Request, status int, locale productui.LocaleContext, page productui.PageID, config JourneyConfig, theme productui.CustomerTheme, accessibility productui.AccessibilityPreferences) {
	if locale.Resolved == "" {
		locale = productui.ResolveProductLocale("")
	}
	stylesheet, styleErr := productStylesheetForTheme(theme)
	if styleErr != nil {
		theme = productui.DefaultCustomerTheme()
		stylesheet = productStylesheet()
	}
	title := locale.Text("shell.page_unavailable")
	message, help := locale.Text("shell.load_recovery"), locale.Text("shell.page_recovery")
	agentOperationsProblem := page == productui.PageAgentOperations
	if agentOperationsProblem {
		title = locale.Text("page.agent_operations.title")
		message, help = locale.Text("agents.page_load_failed"), locale.Text("agents.page_load_help")
	}
	if status == http.StatusForbidden {
		if definition, ok := productui.LookupPage(page); ok && definition.TitleKey != "" {
			title = locale.Text(definition.TitleKey)
		}
		message, help = locale.Text("agents.page_access_denied"), locale.Text("agents.page_access_help")
	}
	actions := []ui.Node{gwchtml.A(gwchtml.Props{Class: "button secondary", Href: PathProductHome}, ui.Text(locale.Text("shell.page_recovery")))}
	if status == http.StatusForbidden {
		actions = []ui.Node{gwchtml.A(gwchtml.Props{Class: "button secondary", Href: productProblemBackHref(r)}, ui.Text(locale.Text("agents.back")))}
		actions = append([]ui.Node{gwchtml.A(gwchtml.Props{Class: "button primary", Href: productui.Path(productui.PageAgents)}, ui.Text(agentUXProblemText(locale, "go_to_agents")))}, actions...)
	} else {
		actions = append([]ui.Node{gwchtml.A(gwchtml.Props{Class: "button primary", Href: r.URL.RequestURI()}, ui.Text(locale.Text("shell.load_retry")))}, actions...)
		if agentOperationsProblem {
			actions[1] = gwchtml.A(gwchtml.Props{Class: "button secondary", Href: productProblemBackHref(r)}, ui.Text(locale.Text("agents.back")))
		}
	}
	problem := gwchtml.Section(gwchtml.Props{Class: "surface empty-state workspace-page-problem", Role: "alert", Aria: map[string]string{"labelledby": "workspace-problem-title"}},
		gwchtml.H1(gwchtml.Props{ID: "workspace-problem-title"}, ui.Text(title)),
		gwchtml.P(gwchtml.Props{Class: "workspace-problem-message"}, ui.Text(message)),
		gwchtml.P(gwchtml.Props{Class: "muted"}, ui.Text(help)),
		gwchtml.Div(gwchtml.Props{Class: "action-row"}, actions...),
	)
	problemConfig := config
	problemConfig.PersonaAdminSnapshot = nil
	if config.Agents != nil {
		agents := *config.Agents
		agents.Agents = nil
		agents.Tasks = nil
		problemConfig.Agents = &agents
	}
	doc, err := productShellProblemDocument(problemConfig, locale, page, problem, theme, accessibility, stylesheet)
	if err != nil {
		http.Error(w, message, status)
		return
	}
	writeHTMLDocument(w, status, doc, productContentSecurityPolicyForStylesheet(h.policyHost(r), stylesheet))
}

func productProblemAppearance(config JourneyConfig) (productui.CustomerTheme, productui.AccessibilityPreferences) {
	theme := productui.DefaultCustomerTheme()
	if config.TenantName != "" {
		theme.BrandName = config.TenantName
		theme.BrandMark = config.TenantMark
	}
	if config.TenantLogo != "" {
		theme.BrandLogoURL = config.TenantLogo
	}
	return productui.NormalizeCustomerTheme(theme), productui.DefaultAccessibilityPreferences()
}

func (h *Handler) productProblemAppearance(ctx context.Context, principal *trust.Principal, config JourneyConfig) (productui.CustomerTheme, productui.AccessibilityPreferences) {
	theme, accessibility := productProblemAppearance(config)
	if h == nil || h.preferences == nil || principal == nil {
		return theme, accessibility
	}
	snapshot, err := h.preferences.Load(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject())
	if err != nil {
		return theme, accessibility
	}
	theme = productui.EffectiveAppearance(productThemeFromPreference(snapshot.Theme.Theme), snapshot.User.Density)
	if config.TenantLogo != "" && theme.BrandLogoURL == "" {
		theme.BrandLogoURL = config.TenantLogo
	}
	if config.TenantName != "" && theme.BrandName == productui.DefaultCustomerTheme().BrandName {
		theme.BrandName, theme.BrandMark = config.TenantName, config.TenantMark
	}
	stored := snapshot.User.Accessibility
	accessibility = productui.NormalizeAccessibilityPreferences(productui.AccessibilityPreferences{TextSize: stored.TextSize, Contrast: stored.Contrast, Motion: stored.Motion, Links: stored.Links, ColorMode: stored.ColorMode})
	return productui.NormalizeCustomerTheme(theme), accessibility
}

func productProblemBackHref(r *http.Request) string {
	if r == nil {
		return PathProductHome
	}
	referer, err := url.Parse(r.Referer())
	if err != nil || referer == nil || !strings.HasPrefix(referer.Path, PathProductPrefix) {
		return PathProductHome
	}
	if referer.Host != "" && !strings.EqualFold(referer.Host, r.Host) {
		return PathProductHome
	}
	return referer.RequestURI()
}

// projectAgentOperationsAccess is retained as the pure policy projection used
// by the Agents availability tests. Route admission still consumes only the
// durable page permission resolved by productAccess.can.
func projectAgentOperationsAccess(access productAccess) productAccess {
	if !access.configured || !agentsAdmin(access) {
		return access
	}
	if access.can(productui.PageAgentOperations, roleaccess.ActionView) {
		return access
	}
	const reason = "derived from agent owner authority"
	access.permissions = append(access.permissions, roleaccess.PagePermission{PageID: string(productui.PageAgentOperations), View: true, Reason: reason})
	// A tenant with feature-level policy admits a page only when its content
	// feature is granted too; the page grant alone left the owner refused.
	if access.featuresConfigured {
		access.features = append(access.features, roleaccess.FeaturePermission{PageID: string(productui.PageAgentOperations), FeatureID: string(productui.FeatureContent), View: true, Reason: reason})
	}
	return access
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
	return productShellDocumentForRouteStateWithPreferencesAndNavigation(config, bundleBuilt, locale, page, menuQuery, nav, theme, accessibilityPreferences, stylesheet, nil, nil)
}

func productShellDocumentForRouteStateWithPreferencesAndNavigation(config JourneyConfig, bundleBuilt bool, locale productui.LocaleContext, page productui.PageID, menuQuery, nav string, theme productui.CustomerTheme, accessibilityPreferences productui.AccessibilityPreferences, stylesheet string, favoritePages []productui.PageID, navigationGroups map[productui.PageID]bool) (string, error) {
	return productShellDocumentForRouteStateWithPreferencesNavigationAndOutlet(config, bundleBuilt, locale, page, menuQuery, nav, theme, accessibilityPreferences, stylesheet, favoritePages, navigationGroups, nil)
}

func productShellProblemDocument(config JourneyConfig, locale productui.LocaleContext, page productui.PageID, problem ui.Node, theme productui.CustomerTheme, accessibility productui.AccessibilityPreferences, stylesheet string) (string, error) {
	theme = productui.NormalizeCustomerTheme(theme)
	organizationColorMode := theme.ColorMode
	theme = productui.EffectiveColorMode(theme, accessibility.ColorMode)
	view := productShellView(config, locale, page, "", "", theme)
	content := productui.BuildShell(view, problem, false)
	markup, err := ui.RenderToString(content)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="` + html.EscapeString(locale.Resolved) + `" dir="` + html.EscapeString(string(locale.Direction)) + `" data-hcm-locale="` + html.EscapeString(locale.Resolved) + `" data-hcm-catalog="` + html.EscapeString(locale.CatalogVersion) + `"`)
	if locale.CatalogRevision != "" {
		b.WriteString(` data-hcm-catalog-revision="` + html.EscapeString(locale.CatalogRevision) + `"`)
	}
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
	b.WriteString(` data-hcm-organization-color-mode="` + html.EscapeString(organizationColorMode) + `"`)
	preferences := productui.AccessibilityPreferenceAttributes(accessibility)
	for _, name := range []string{"data-hcm-text-size", "data-hcm-contrast", "data-hcm-motion-preference", "data-hcm-links"} {
		b.WriteString(` ` + name + `="` + html.EscapeString(preferences[name]) + `"`)
	}
	b.WriteString(`><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="` + html.EscapeString(productui.ColorSchemeContent(appearance["data-hcm-color-mode"])) + `"><title>`)
	b.WriteString(html.EscapeString(locale.Text("shell.page_unavailable")))
	b.WriteString(`</title><style>`)
	b.WriteString(stylesheet)
	b.WriteString(`</style></head><body>`)
	b.WriteString(markup)
	b.WriteString(`</body></html>`)
	return b.String(), nil
}

func productShellView(config JourneyConfig, locale productui.LocaleContext, page productui.PageID, menuQuery, nav string, theme productui.CustomerTheme) productui.View {
	tenantLabel := productui.DisplayLabel(config.Tenant)
	if config.TenantName != "" {
		tenantLabel = config.TenantName
	}
	view := productui.NewView(page, tenantLabel, productui.DisplayLabel(config.Subject), "")
	view.PersonaAdminClient = config.PersonaAdminClient
	view.Appearance = theme
	view.MenuQuery = strings.TrimSpace(menuQuery)
	view.NavCollapsed = nav == "collapsed"
	view.NavExpanded = nav == "expanded"
	view.LogoutHref = config.LogoutPath
	view = productui.ApplyRoleVisibility(view, config.Roles)
	if len(config.PagePermissions) > 0 {
		view = productui.ApplyPagePermissions(view, productPagePermissions(config.PagePermissions))
	}
	if config.FeaturePermissions != nil {
		view = productui.ApplyFeaturePermissions(view, productFeaturePermissions(config.FeaturePermissions))
	}
	view.LauncherActions = productLauncherActions(config.LauncherActions)
	view.WorkflowStartCatalog = productWorkflowStartItems(config.WorkflowStarts)
	view.WorkflowStartFavorites = append([]string(nil), config.WorkflowStartFavorites...)
	view.WorkflowStartRecent = append([]string(nil), config.WorkflowStartRecent...)
	if config.Agents != nil {
		view = productui.ApplyAgentsAvailability(view, ProductAgentsAvailability(config.Agents))
	}
	if config.Clock != nil {
		view = productui.ApplyClockAvailability(view, ProductClockAvailability(config.Clock))
	}
	return productui.ApplyLocale(view, locale)
}

func productShellDocumentForRouteStateWithPreferencesNavigationAndOutlet(config JourneyConfig, bundleBuilt bool, locale productui.LocaleContext, page productui.PageID, menuQuery, nav string, theme productui.CustomerTheme, accessibilityPreferences productui.AccessibilityPreferences, stylesheet string, favoritePages []productui.PageID, navigationGroups map[productui.PageID]bool, outlet ui.Node) (string, error) {
	island, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="` + html.EscapeString(locale.Resolved) + `" dir="` + html.EscapeString(string(locale.Direction)) + `" data-hcm-locale="` + html.EscapeString(locale.Resolved) + `" data-hcm-catalog="` + html.EscapeString(locale.CatalogVersion) + `"`)
	if locale.CatalogRevision != "" {
		b.WriteString(` data-hcm-catalog-revision="` + html.EscapeString(locale.CatalogRevision) + `"`)
	}
	if locale.Fallback != productui.LocaleFallbackNone {
		b.WriteString(` data-hcm-locale-fallback="` + html.EscapeString(string(locale.Fallback)) + `"`)
	}
	if missing := len(productui.MissingProductTranslations(locale.Resolved)); missing > 0 {
		b.WriteString(` data-hcm-message-fallback="en-US" data-hcm-message-fallback-count="` + strconv.Itoa(missing) + `"`)
	}
	organizationColorMode := productui.NormalizeCustomerTheme(theme).ColorMode
	theme = productui.EffectiveColorMode(theme, accessibilityPreferences.ColorMode)
	appearance := productui.CustomerThemeAttributes(theme)
	for _, name := range []string{"data-hcm-color-mode", "data-hcm-palette", "data-hcm-shape", "data-hcm-density", "data-hcm-glyphs", "data-hcm-typeface", "data-hcm-navigation", "data-hcm-motion"} {
		b.WriteString(` ` + name + `="` + html.EscapeString(appearance[name]) + `"`)
	}
	b.WriteString(` data-hcm-organization-color-mode="` + html.EscapeString(organizationColorMode) + `"`)
	accessibility := productui.AccessibilityPreferenceAttributes(accessibilityPreferences)
	for _, name := range []string{"data-hcm-text-size", "data-hcm-contrast", "data-hcm-motion-preference", "data-hcm-links"} {
		b.WriteString(` ` + name + `="` + html.EscapeString(accessibility[name]) + `"`)
	}
	b.WriteString(`><head><meta charset="utf-8"><meta name="color-scheme" content="` + html.EscapeString(productui.ColorSchemeContent(appearance["data-hcm-color-mode"])) + `">`)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	title := "Human Capital Management Suite"
	if definition, ok := productui.LookupPage(page); ok && definition.TitleKey != "" {
		title = locale.Text(definition.TitleKey)
	}
	tenantLabel := productui.DisplayLabel(config.Tenant)
	if config.TenantName != "" {
		tenantLabel = config.TenantName
	}
	b.WriteString("<title>" + html.EscapeString(productui.DocumentTitle(title, theme, tenantLabel)) + "</title><style>")
	b.WriteString(stylesheet)
	b.WriteString("</style></head><body>")
	b.WriteString(`<div id="` + JourneyRootElementID + `">`)
	if bundleBuilt || outlet != nil {
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
		view := productShellView(config, locale, page, menuQuery, nav, theme)
		// Hydration preserves live input values. Seed the request's query in the
		// loading shell so an empty SSR value cannot hide an active client filter.
		view.FavoritePages = favoritePages
		view.NavigationGroupOpen = navigationGroups
		// The server has already resolved the authenticated shell chrome
		// (identity, brand, navigation preferences and support controls), so
		// only the route content is pending. Using the content-loading tree
		// keeps the first paint identical to the hydrated shell and confines
		// the inert proxy to the main outlet.
		content := productui.BuildContentLoading(view)
		if outlet != nil {
			content = productui.BuildShell(view, outlet, false)
		}
		loading, renderErr := ui.RenderToString(content)
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
		styleHashes:                  []string{stylesheetHash},
		scriptHash:                   journeyLoaderHash,
		formActionSelf:               true,
		connectHost:                  host,
		allowAssetConnections:        true,
		allowTunnelConnection:        true,
		allowMediaConnections:        true,
		allowPersonaAdminConnections: true,
		allowAgentConnections:        true,
		// UXBLIND-085: the appearance page's brand-asset picker fetches the
		// collection and its lifecycle endpoint from the WASM client.
		allowBrandAssetConnections: true,
		allowGiphy:                 allowGiphy,
		sameOriginImages:           true,
		blobImages:                 true,
		allowBlobScript:            true,
		allowWASM:                  true,
	}.header()
}
