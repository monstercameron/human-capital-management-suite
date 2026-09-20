package productui

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/uicomponents"
)

func appShell(view View, page ui.Node) ui.Node {
	return appShellWithHeading(view, page, true)
}

func appShellWithHeading(view View, page ui.Node, showHeading bool) ui.Node {
	class := "app-shell"
	if view.NavCollapsed {
		class += " nav-collapsed"
	}
	if view.Loading {
		class += " is-loading"
	} else if view.ContentLoading {
		class += " is-content-loading"
	} else if view.Refreshing {
		class += " is-refreshing"
	}
	content := page
	if signedOutState(view) {
		content = signedOut(view)
		showHeading = false
	} else if strings.TrimSpace(view.Tenant) == "" {
		content = FederationEntryList(federationEntryProps(view))
		showHeading = false
	} else if view.LoadError != "" {
		content = html.Div(html.Props{Class: "page-stack"},
			unavailablePanelWithRetry(
				view.Locale.Text("shell.live_unavailable"),
				view.Locale.Text("shell.load_recovery"),
				view.Locale.Text("shell.load_retry"),
				statefulHref(view, view.Page),
				view.Navigate,
			),
			page,
		)
	}
	announcement := view.Locale.Text("shell.page_loaded", map[string]string{"title": view.Title})
	if view.Loading || view.ContentLoading || view.Refreshing {
		announcement = view.Locale.Text("shell.loading_authorized")
	}
	return html.Div(html.Props{Class: class},
		html.A(html.Props{Class: "skip-link", Href: "#main-content"}, ui.Text(view.Locale.Text("shell.skip_main"))),
		html.Div(html.Props{Class: "sr-only route-announcer", Raw: map[string]any{"role": "status", "aria-live": "polite", "aria-atomic": "true"}}, ui.Text(announcement)),
		ui.CreateElement(NavigationDrawerScope, navigationDrawerScopeProps{
			Render: func(open bool, toggle, closeDrawer func()) ui.Node {
				return html.Fragment(
					appHeader(view, open, toggle),
					sessionWarning(view),
					stepUpChallenge(view),
					actingAuthorityBanner(view),
					breakGlassActivation(view),
					policySimulation(view),
					html.Div(html.Props{Class: "shell-grid"}, primarySidebar(view, open, toggle, closeDrawer), pageFrame(view, content, showHeading)),
				)
			},
		}),
	)
}

// navigationDrawerScopeProps carries the shell's render body as a prop so
// NavigationDrawerScope can own one piece of ephemeral state — the narrow-
// viewport overlay drawer's open/closed flag — and hand it to both the
// header trigger and the sidebar it controls, even though they render in
// different branches of the tree. This is the one navigation model the
// REFACTOR clause requires: the same header, the same NavigationSidebar,
// for every viewport; only CSS media queries give the shared Open flag any
// visual effect, and only at narrow widths.
type navigationDrawerScopeProps struct {
	Render func(open bool, toggle func(), closeDrawer func()) ui.Node
}

// NavigationDrawerScope owns the drawer's open/closed state. Open starts
// false (closed) on every render, on every viewport: the trigger that can
// set it true is the ONLY way it ever changes, and CSS removes that trigger
// from hit-testing and the tab order outside narrow viewports, so a desktop
// document is never reachably open. That is the fail-closed default the
// unset case requires, achieved without any runtime viewport detection.
func NavigationDrawerScope(props navigationDrawerScopeProps) ui.Node {
	open := ui.UseState(false)
	if props.Render == nil {
		return html.Fragment()
	}
	return props.Render(open.Get(), func() { open.Set(!open.Get()) }, func() { open.Set(false) })
}

func appHeader(view View, drawerOpen bool, drawerToggle func()) ui.Node {
	appearance := NormalizeCustomerTheme(view.Appearance)
	toggle := navigationToggleProps(view)
	brandName, brandMark := HeaderBrandIdentity(appearance, view.Tenant)
	brandProps := html.Props{Class: "wordmark", Title: brandName, Data: map[string]string{"hcm-brand-link": ""}}
	brandContent := ui.CreateElement(BrandLogo, BrandLogoProps{Name: brandName, AccessibleName: brandName, Mark: brandMark, LogoURL: appearance.BrandLogoURL})
	var brand ui.Node = html.Div(brandProps, brandContent)
	if navigationDestinationAuthorized(view, PageHome) {
		brand = appLink(view, brandProps, navigationHref(view, PageHome), brandContent)
	}
	drawerLabel := view.Locale.Text("nav.drawer_open")
	if drawerOpen {
		drawerLabel = view.Locale.Text("nav.drawer_close")
	}
	return html.Header(html.Props{Class: "topbar"},
		html.Div(html.Props{Class: "brand-cluster"},
			brand,
			softwareLink(toggle.Navigate, html.Props{
				Class: "header-nav-toggle",
				Aria:  map[string]string{"label": toggle.Label, "expanded": fmt.Sprint(!view.NavCollapsed), "controls": "workspace-navigation"},
				Raw:   map[string]any{"title": toggle.Label},
			}, toggle.Href, navIcon(toggle.Icon)),
			html.Button(html.Props{
				ID: "nav-drawer-trigger", Class: "nav-drawer-trigger", Type: "button",
				Aria: navigationDrawerTriggerAria(drawerOpen, drawerLabel),
				Raw:  map[string]any{"title": drawerLabel},
				OnClick: ui.UseEvent(func(ui.MouseEvent) {
					if drawerToggle != nil {
						drawerToggle()
					}
				}),
				OnKeyDown: ui.UseEvent(func(event ui.KeyboardEvent) {
					if drawerOpen && drawerEscapeCloses(event.GetKey()) && drawerToggle != nil {
						event.PreventDefault()
						drawerToggle()
					}
				}),
			}, navIcon("menu")),
		),
		html.Div(html.Props{Class: "header-navigation-tools"},
			contextSwitcherSlot(view),
			delegationSelectorSlot(view),
			ui.CreateElement(HistoryNavigation, historyNavigationProps(view)),
			globalSearch(view),
			actionLauncher(view),
			utilityDrawer(view),
		),
		localeMenu(view),
		notificationSlot(view),
		viewerProfileLink(view),
	)
}

// HeaderBrandIdentity resolves the company name used by both the Go render
// and the WASM appearance controller. Explicit customer branding wins; a
// generic, unconfigured theme uses the admitted tenant display name.
func HeaderBrandIdentity(appearance CustomerTheme, tenant string) (name, mark string) {
	name, mark = appearance.BrandName, appearance.BrandMark
	defaults := DefaultCustomerTheme()
	if name != defaults.BrandName || strings.TrimSpace(tenant) == "" {
		return name, mark
	}
	name = normalizedBrandText(tenant, 120, defaults.BrandName, false)
	if mark == defaults.BrandMark {
		var initials []rune
		for _, word := range strings.Fields(name) {
			initials = append(initials, unicode.ToUpper([]rune(word)[0]))
			if len(initials) == 2 {
				break
			}
		}
		if len(initials) > 0 {
			mark = string(initials)
		}
	}
	return name, mark
}

func contextSwitcherSlot(view View) ui.Node {
	if signedOutState(view) {
		return html.Fragment()
	}
	props := view.ContextSwitcher
	if !contextSwitcherVisible(props) {
		return html.Fragment()
	}
	return ui.CreateElement(ContextSwitcher, props)
}

func delegationSelectorSlot(view View) ui.Node {
	if signedOutState(view) {
		return html.Fragment()
	}
	props := view.ContextSwitcher
	if len(delegationSelectorOptions(props)) == 0 {
		return html.Fragment()
	}
	return ui.CreateElement(DelegationSelector, props)
}

func historyNavigationProps(view View) HistoryNavigationProps {
	props := view.HistoryNavigation
	props.I18nProps = I18nProps{Locale: view.Locale}
	return props
}

func viewerProfileLink(view View) ui.Node {
	if view.Loading {
		return html.Div(html.Props{
			Class: "viewer-profile-link viewer-profile-loading network-slot network-slot-pending",
			Raw:   map[string]any{"aria-hidden": "true"},
		}, html.Span(html.Props{Class: "loading-block loading-viewer-profile"}))
	}
	profile := view.Viewer
	name := strings.TrimSpace(profile.Name)
	if strings.TrimSpace(profile.Initials) == "" {
		profile.Initials = uicomponents.Initials(name)
	}
	label := view.Locale.Text("shell.myself_unidentified")
	if name != "" {
		label = view.Locale.Text("shell.myself", map[string]string{"name": name})
	}
	props := html.Props{
		Class: "viewer-profile-link network-slot network-slot-ready", Title: label,
		Aria: map[string]string{"label": label},
	}
	avatar := personAvatar(name, profile.Initials, profile.PhotoURL, "viewer")
	if !navigationDestinationAuthorized(view, PageMyself) {
		return html.Div(props, avatar)
	}
	return appLink(view, props, statefulHref(view, PageMyself), avatar)
}

func notificationSlot(view View) ui.Node {
	if view.Loading {
		return html.Div(html.Props{Class: "notifications notification-loading network-slot network-slot-pending", Raw: map[string]any{"aria-hidden": "true"}},
			html.Span(html.Props{Class: "loading-block loading-notification"}),
		)
	}
	return notificationMenu(view)
}

func globalSearch(view View) ui.Node {
	props := globalSearchProps(view)
	if view.NavigationProjection != nil {
		props.Items = authorizedGlobalSearchItems(view, props.Items)
		props.FallbackHref = authorizedGlobalSearchFallback(view)
		if favorites := authorizedFavoritePages(view.Navigation, view.FavoritePages); len(favorites) == 0 {
			delete(props.HiddenInputs, "favorites")
		} else {
			values := make([]string, 0, len(favorites))
			for _, page := range favorites {
				values = append(values, string(page))
			}
			props.HiddenInputs["favorites"] = strings.Join(values, ",")
		}
	}
	return ui.CreateElement(GlobalSearch, props)
}

func actionLauncher(view View) ui.Node {
	props := actionLauncherProps(view)
	props.Items = authorizedActionLauncherItems(view, props.Items)
	return ui.CreateElement(ActionLauncher, props)
}

// authorizedActionLauncherItems intersects the allowed starts with the pages
// the authorized navigation (projection or legacy) admits, so the launcher
// never advertises a route the shell itself omits.
func authorizedActionLauncherItems(view View, items []ActionLauncherItem) []ActionLauncherItem {
	allowed := authorizedNavigationPages(view)
	result := make([]ActionLauncherItem, 0, len(items))
	// The canonical per-person items are built once per call and indexed by
	// ID. Rebuilding them inside the policy for every item made each header
	// render quadratic in the population (a full per-person projection per
	// launcher item) and dominated typing and navigation profiles.
	canonical := canonicalPersonLauncherItems{view: view}
	for _, item := range items {
		state, admitted := actionLauncherItemPolicy(view, item, &canonical)
		if !admitted || state.Availability == ActionHidden || !allowed[item.Page] || !actionLauncherDestinationValid(item) {
			continue
		}
		item.Availability = state
		result = append(result, item)
	}
	return result
}

// actionLauncherItemPolicy binds each registered action to the server's exact,
// unique semantic verdict. Item-provided fields are checked rather than
// trusted, and page CRUD never manufactures action authority. Safe-to-disclose
// denials require both an explicit reason and an authorized recovery route.
func actionLauncherItemPolicy(view View, item ActionLauncherItem, people *canonicalPersonLauncherItems) (ActionState, bool) {
	if item.Kind == ActionLauncherAction {
		if strings.HasPrefix(item.ID, "action:") || strings.HasPrefix(item.ID, "action-unavailable:") {
			if canonical, ok := people.lookup(item.ID); ok &&
				item.Page == canonical.Page && item.Action == canonical.Action &&
				item.Kind == canonical.Kind && item.Href == canonical.Href && item.Label == canonical.Label &&
				item.Description == canonical.Description && item.Reason == canonical.Reason &&
				item.Availability.Availability == canonical.Availability.Availability &&
				item.Availability.Reason == canonical.Availability.Reason {
				return canonical.Availability, true
			}
			return ActionState{Availability: ActionHidden}, false
		}
		definition, registered := semanticLauncherDefinitionByID(item.ID)
		if !registered || item.Page != definition.Page || item.Action != definition.Action || item.Href != definition.Href(view) {
			return ActionState{Availability: ActionHidden}, false
		}
		projection, projected := uniqueLauncherActionProjection(view.LauncherActions, item.ID)
		if !projected || projection.Priority < 0 {
			return ActionState{Availability: ActionHidden}, false
		}
		switch projection.State.Availability {
		case ActionAvailable:
			return ActionState{Availability: ActionAvailable}, true
		case ActionUnavailable:
			if strings.TrimSpace(projection.State.Reason) == "" || !actionLauncherRecoveryValid(view, projection.State.Recovery) {
				return ActionState{Availability: ActionHidden}, false
			}
			return projection.State, true
		default:
			return ActionState{Availability: ActionHidden}, false
		}
	}
	if item.Kind != ActionLauncherDestination || item.Action != "view" || item.ID != actionLauncherDestinationID(item.Page) || !view.Allows(item.Page, "view") {
		return ActionState{Availability: ActionHidden}, false
	}
	return ActionState{Availability: ActionAvailable}, true
}

// canonicalPersonLauncherItems builds personActionLauncherItems at most once
// and indexes it by item ID. IDs are unique per person and action index, so
// the first entry for an ID is the canonical one.
type canonicalPersonLauncherItems struct {
	view  View
	built bool
	byID  map[string]ActionLauncherItem
}

func (c *canonicalPersonLauncherItems) lookup(id string) (ActionLauncherItem, bool) {
	if !c.built {
		c.built = true
		items := personActionLauncherItems(c.view)
		c.byID = make(map[string]ActionLauncherItem, len(items))
		for _, item := range items {
			if _, seen := c.byID[item.ID]; !seen {
				c.byID[item.ID] = item
			}
		}
	}
	item, ok := c.byID[id]
	return item, ok
}

func actionLauncherRecoveryValid(view View, recovery ActionLinkProps) bool {
	if strings.TrimSpace(recovery.Label) == "" || strings.TrimSpace(recovery.Href) == "" {
		return false
	}
	parsed, err := url.Parse(recovery.Href)
	if err != nil || parsed.IsAbs() || parsed.Opaque != "" || parsed.Scheme != "" || parsed.Host != "" || parsed.Fragment != "" {
		return false
	}
	definition, ok := LookupRoute(parsed.Path)
	if !ok || !view.Can(definition.ID, "view") {
		return false
	}
	for key, values := range parsed.Query() {
		if (key != "locale" && key != "nav" && key != "favorites") || len(values) != 1 {
			return false
		}
	}
	return true
}

func actionLauncherDestinationValid(item ActionLauncherItem) bool {
	definition, ok := LookupPage(item.Page)
	if !ok {
		return false
	}
	if item.Href == "" && strings.HasPrefix(item.ID, "action-unavailable:") && item.Availability.Availability == ActionUnavailable {
		return true // policy already compared the entire item with its canonical denial.
	}
	if strings.TrimSpace(item.Href) == "" {
		return false
	}
	parsed, err := url.Parse(item.Href)
	return err == nil && !parsed.IsAbs() && parsed.Opaque == "" && parsed.Scheme == "" && parsed.User == nil && parsed.Host == "" &&
		parsed.Path == definition.Route && parsed.RawPath == "" && parsed.Fragment == "" && actionLauncherQueryValid(item, parsed.Query())
}

func actionLauncherQueryValid(item ActionLauncherItem, query url.Values) bool {
	allowed := map[string]bool{"locale": true, "nav": true, "favorites": true}
	if item.ID == actionLauncherPromoteWorker {
		allowed["eligible"] = true
		if query.Get("eligible") != "1" {
			return false
		}
	}
	if strings.HasPrefix(item.ID, "action:") {
		allowed["mode"], allowed["worker"], allowed["type"] = true, true, true
		if query.Get("mode") != "new" || query.Get("worker") == "" {
			return false
		}
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			return false
		}
	}
	return true
}

func authorizedGlobalSearchItems(view View, items []GlobalSearchItem) []GlobalSearchItem {
	allowed := authorizedNavigationPages(view)
	if allowed[PagePeople] {
		// Person results come only from the already-filtered People projection.
		// This enables a destination; it does not authorize its independent RPC.
		allowed[PagePerson] = true
	}
	result := make([]GlobalSearchItem, 0, len(items))
	for _, item := range items {
		parsed, err := url.Parse(item.Href)
		if err != nil || parsed.IsAbs() || parsed.Host != "" {
			continue
		}
		definition, ok := LookupRoute(parsed.Path)
		if ok && allowed[definition.ID] {
			result = append(result, item)
		}
	}
	return result
}

func authorizedGlobalSearchFallback(view View) string {
	for _, page := range []PageID{PagePeople, PageHome, view.Page} {
		if navigationDestinationAuthorized(view, page) {
			return statefulHref(view, page)
		}
	}
	return "#"
}

// navigationDestinationAuthorized answers only whether the resolver chose to
// advertise a destination. It is deliberately not action or route authority;
// every destination service still authenticates and authorizes its own read.
func navigationDestinationAuthorized(view View, page PageID) bool {
	if view.NavigationProjection == nil {
		return view.Allows(page, "view")
	}
	// The collector validates before exposing any destination. Do not validate
	// the same projection twice for each header link.
	return authorizedNavigationPages(view)[page]
}

func authorizedNavigationPages(view View) map[PageID]bool {
	result := make(map[PageID]bool)
	if view.NavigationProjection != nil {
		if err := validateAuthorizedNavigationProjection(*view.NavigationProjection); err != nil {
			return result
		}
		var collect func([]AuthorizedNavigationItem)
		collect = func(items []AuthorizedNavigationItem) {
			for _, item := range items {
				result[item.Page] = true
				collect(item.Children)
			}
		}
		collect(view.NavigationProjection.Items)
		collect(view.NavigationProjection.Support)
		return result
	}
	var collect func([]NavItem)
	collect = func(items []NavItem) {
		for _, item := range items {
			result[item.Page] = true
			collect(item.Children)
		}
	}
	collect(view.Navigation)
	collect(view.NavigationSupport)
	return result
}

func notificationMenu(view View) ui.Node {
	// The attention count follows the current authority: denied
	// instances keep no share of it. A silent server keeps the current
	// count, and page-visibility keeps its honest restricted state.
	// PROMOUX-012: the summary counts only work the viewer must act on, so a
	// passive wait or a tracked request never inflates it.
	open := len(ActionableWorkItems(admittedWork(view)))
	label := view.Locale.Text("shell.work_overview") + ", " + view.Locale.Plural("shell.work_count", int64(open))
	children := []ui.Node{
		html.H2(html.Props{}, ui.Text(view.Locale.Text("shell.work_overview"))),
		html.P(html.Props{}, ui.Text(view.Locale.Plural("shell.work_count", int64(open)))),
	}
	if navigationDestinationAuthorized(view, PageWork) {
		children = append(children, appLink(view, html.Props{}, statefulHref(view, PageWork), ui.Text(view.Locale.Text("shell.open_work"))))
	}
	return ui.CreateElement(TransientPopover, TransientPopoverProps{
		Kind: "notification", Class: "notifications network-slot network-slot-ready", Label: label,
		Trigger: []ui.Node{navIcon("notifications")}, PanelClass: "popover notification-popover",
		Children: children,
	})
}

func localeMenu(view View) ui.Node {
	locale := view.Locale.normalized()
	localePreferences := localePreferencesProps(view)
	items := make([]ui.Node, 0, len(localePreferences.Options))
	for _, option := range localePreferences.Options {
		props := html.Props{
			Class: "locale-option", Lang: option.Language, Dir: option.TextDirection,
			Aria: map[string]string{"label": option.Label},
		}
		if option.Current {
			props.Aria["current"] = "true"
		}
		items = append(items, softwareLink(option.Navigate, props, option.Href,
			html.Span(html.Props{Lang: option.Language, Dir: option.TextDirection}, ui.Text(option.Label))))
	}
	label := locale.Text("shell.locale")
	return ui.CreateElement(TransientPopover, TransientPopoverProps{
		Kind: "locale", Class: "locale-menu", Label: label, Title: label,
		Trigger:    []ui.Node{ui.Text(strings.ToUpper(strings.Split(locale.Resolved, "-")[0]))},
		PanelClass: "popover locale-popover", Children: items,
	})
}

func primarySidebar(view View, drawerOpen bool, drawerToggle, drawerClose func()) ui.Node {
	return ui.CreateElement(NavigationSidebar, navigationDrawerSidebarProps(view, drawerOpen, drawerToggle, drawerClose))
}

func navigationHref(view View, page PageID) string {
	return statefulHref(view, page)
}

func navigationHrefForItem(view View, item NavItem) string {
	if strings.TrimSpace(item.Href) == "" {
		return navigationHref(view, item.Page)
	}
	return statefulHrefAtRoute(view, item.Href)
}

func statefulHrefAtRoute(view View, route string) string {
	route = strings.TrimSpace(route)
	if route == "" {
		return pageHref(PageHome)
	}
	parsed, err := url.Parse(route)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/workspace/app/") {
		return pageHref(PageHome)
	}
	values := parsed.Query()
	// Projected navigation items carry a server-owned destination href. The
	// current shell state still belongs to this navigation, even when that href
	// is already populated, so a collapsed sidebar must not reopen on select.
	if view.NavCollapsed {
		values.Set("nav", "collapsed")
	} else {
		values.Del("nav")
	}
	setMenuAddressState(values, view)
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func currentPageHref(view View, collapsed bool) string {
	values := currentPageAddressState(view, collapsed)
	href := pageHref(view.Page)
	if query := values.Encode(); query != "" {
		return href + "?" + query
	}
	return href
}

func currentPageAddressState(view View, collapsed bool) url.Values {
	values := url.Values{}
	if collapsed {
		values.Set("nav", "collapsed")
	}
	setMenuAddressState(values, view)
	if routeProfile, _, ok := PageProfiles(view.Page); ok {
		routeProfile.AddressValues(values, view)
	}
	return values
}

func pageFrame(view View, page ui.Node, showHeading bool) ui.Node {
	// Every child is keyed. The refresh progress bar comes and goes at the
	// front of this list; unkeyed, its arrival shifted the page head and the
	// page itself one position, so a sort or filter refresh re-mounted them.
	// The browser's scroll anchoring then moved the reader by the page
	// head's height and the focused heading was replaced (UXLIVE-028).
	children := make([]ui.Node, 0, 4)
	if view.Refreshing {
		children = append(children, html.Div(html.Props{
			Key:   "network-progress",
			Class: "loading-progress network-progress",
			Raw:   map[string]any{"aria-hidden": "true"},
		}))
	}
	if showHeading {
		children = append(children, html.WithKey(PageIdentityHeader(view), "page-identity"))
	}
	children = append(children, keyUnlessKeyed(page, "page-content"),
		html.Footer(html.Props{Key: "page-footer", Class: "footer"},
			html.Span(html.Props{Data: map[string]string{"hcm-brand-name": ""}}, ui.Text(NormalizeCustomerTheme(view.Appearance).BrandName)),
			html.Span(html.Props{}, ui.Text(view.Locale.Text("shell.live_source"))),
		),
	)
	// UIPOLISH-004: ".main-scroll" is the page's one scroll owner and must
	// stay keyboard-reachable and named regardless of whether the page
	// itself renders a heading -- a feature module using BuildEmbedded
	// (showHeading false, e.g. Journeys) previously left this region with
	// neither aria-label nor aria-labelledby, an unnamed landmark once it
	// also gains a tabIndex. aria-labelledby="page-title" stays the primary
	// name whenever a heading exists; the fallback label only ever applies
	// when it does not.
	aria := map[string]string{}
	if showHeading {
		aria["labelledby"] = "page-title"
	} else {
		aria["label"] = view.Locale.Text("shell.main_region")
	}
	if view.Loading || view.ContentLoading || view.Refreshing {
		aria["busy"] = "true"
	}
	stageClass := "main network-stage network-stage-ready"
	stage := "ready"
	if view.Loading || view.ContentLoading {
		stageClass = "main network-stage network-stage-pending"
		stage = "pending"
	} else if view.Refreshing {
		stageClass = "main network-stage network-stage-refreshing"
		stage = "refreshing"
	}
	// overflow-x stays hidden here deliberately (UIPOLISH-004 RED names this
	// explicitly): every region beneath the page shell that can genuinely
	// need to scroll sideways already owns that scroll itself
	// (".data-table-scroll", the responsive card/table breakpoints in
	// dataTableStylesStylesheet, the sticky ".data-table thead" horizontal
	// scroll under 1050px). Nothing below the shell is meant to overflow the
	// page horizontally without its own scroll owner, so the shell clipping
	// stray overflow here is a safety net, not a competing scrollbar.
	// The main region's position is owned by the history router's one scroll
	// policy (UXLIVE-028), keyed by history entry and resource; a second,
	// per-element restore here could put an unrelated page's offset back.
	return ui.CreateElement(ScrollRegion, ScrollRegionProps{
		Tag: "main", ID: "main-content", Class: "main-scroll", Focusable: true, Aria: aria,
		Children: []ui.Node{html.Div(html.Props{Class: stageClass, Data: map[string]string{"network-state": stage}}, children...)},
	})
}
