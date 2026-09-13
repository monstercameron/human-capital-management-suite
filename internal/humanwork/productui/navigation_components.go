package productui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// NavigationSidebarProps is the complete presentation contract for the
// application navigation. Hierarchy, filtering, ordering, and preference
// links are resolved before rendering so this component owns no route rules.
type NavigationSidebarProps struct {
	I18nProps
	Collapsed bool
	Tenant    string
	EmptyText string
	Filter    MenuFilterProps
	Favorites []NavigationItemProps
	Items     []NavigationItemProps
	Support   []NavigationItemProps
	// Search reprojects the navigation catalog without loading page data.
	// Menu filtering is client-local; only its shareable URL state is debounced.
	Search func(string) NavigationSidebarProps
	// Open is the ephemeral narrow-viewport overlay drawer state. It is
	// independent of Collapsed (the persistent desktop icon-rail preference):
	// Open only ever becomes true through the drawer trigger, which CSS keeps
	// out of the tab order and hit-testing outside narrow viewports, so a
	// desktop render is always closed regardless of Collapsed. Zero value
	// (false) is the safe, off-canvas default at every viewport.
	Open bool
	// OnToggle and OnClose drive the shared drawer state owned by
	// NavigationDrawerScope (shell.go). Both are nil-safe: a nil value keeps
	// the render static, which is what a non-interactive SSR document needs.
	OnToggle func()
	OnClose  func()
}

type NavigationToggleProps struct {
	Label    string
	Icon     string
	Href     string
	Navigate func(string)
}

// NavigationItemProps recursively describes a menu group or leaf.
type NavigationItemProps struct {
	I18nProps
	Page         PageID
	Label        string
	Icon         string
	Count        int
	Href         string
	Active       bool
	Expanded     bool
	ForceOpen    bool
	Favorite     bool
	FavoriteHref string
	MatchScore   int
	MatchDetail  string
	Children     []NavigationItemProps
	Navigate     func(string)
}

type MenuFilterProps struct {
	I18nProps
	Query     string
	Action    string
	ClearHref string
	Hidden    []MenuHiddenInput
	Navigate  func(string)
	OnInput   func(string)
	OnFilter  func(string)
	OnClear   func()
}

type MenuHiddenInput struct {
	Name  string
	Value string
}

func navigationSidebarProps(view View) NavigationSidebarProps {
	return navigationDrawerSidebarProps(view, false, nil, nil)
}

// navigationDrawerSidebarProps is navigationSidebarProps plus the shared
// narrow-viewport drawer state. It is a separate entry point (rather than
// adding parameters to every existing navigationSidebarProps call site) so
// the many tests that build props for a persistent desktop sidebar keep
// working unchanged: they get the safe closed default.
func navigationDrawerSidebarProps(view View, open bool, onToggle, onClose func()) NavigationSidebarProps {
	props := navigationSidebarPropsForQuery(view)
	props.Open, props.OnToggle, props.OnClose = open, onToggle, onClose
	props.Search = func(query string) NavigationSidebarProps {
		next := view
		next.MenuQuery = strings.TrimSpace(query)
		reprojected := navigationSidebarPropsForQuery(next)
		reprojected.Open, reprojected.OnToggle, reprojected.OnClose = open, onToggle, onClose
		return reprojected
	}
	return props
}

func navigationSidebarPropsForQuery(view View) NavigationSidebarProps {
	favorites, items := projectNavigation(view)
	filterView := view
	filterView.MenuQuery = ""
	filter := MenuFilterProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Query:     view.MenuQuery, Action: pageHref(view.Page), ClearHref: currentPageHref(filterView, view.NavCollapsed),
		Hidden: menuFilterHiddenState(view), Navigate: view.Navigate,
	}
	if view.Navigate != nil {
		filterHref := func(query string) string {
			next := view
			next.MenuQuery = strings.TrimSpace(query)
			return currentPageHref(next, next.NavCollapsed)
		}
		filter.OnFilter = func(query string) {
			if view.CancelDebouncedNavigation != nil {
				view.CancelDebouncedNavigation()
			}
			view.Navigate(filterHref(query))
		}
		filter.OnClear = func() {
			if view.CancelDebouncedNavigation != nil {
				view.CancelDebouncedNavigation()
			}
		}
		if view.NavigateDebounced != nil {
			filter.OnInput = func(query string) {
				view.NavigateDebounced(filterHref(query))
			}
		}
	}
	supportItems := view.NavigationSupport
	if view.NavigationProjection == nil {
		supportItems = make([]NavItem, 0, 2)
		for _, page := range []PageID{PageHelp, PageSettings} {
			item, ok := navigationItemForPage(page, view.Locale)
			if ok {
				supportItems = append(supportItems, item)
			}
		}
	}
	support := make([]NavigationItemProps, 0, len(supportItems))
	for _, item := range supportItems {
		props := navigationLeafProps(view, item, false)
		props.FavoriteHref = ""
		support = append(support, props)
	}
	sort.SliceStable(support, func(left, right int) bool { return support[left].MatchScore > support[right].MatchScore })
	emptyText := view.Locale.Text("nav.none")
	if view.NavigationProjection != nil && strings.TrimSpace(view.MenuQuery) == "" {
		emptyText = view.Locale.Text("nav.unavailable")
	}
	return NavigationSidebarProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Collapsed: view.NavCollapsed, Tenant: view.Tenant, EmptyText: emptyText, Filter: filter,
		Favorites: favorites, Items: items, Support: support,
	}
}

func navigationToggleProps(view View) NavigationToggleProps {
	toggle := NavigationToggleProps{
		Label: view.Locale.Text("nav.collapse"), Icon: "collapse", Href: currentPageHref(view, true), Navigate: view.Navigate,
	}
	if view.NavCollapsed {
		toggle.Label, toggle.Icon = view.Locale.Text("nav.expand"), "expand"
		toggle.Href = withExplicitQueryValue(currentPageHref(view, false), []string{"nav"}, "expanded")
	}
	return toggle
}

func navigationItemForPage(page PageID, locale LocaleContext) (NavItem, bool) {
	definition, ok := LookupPage(page)
	if !ok {
		return NavItem{}, false
	}
	return navigationItemFromDefinition(definition, locale), true
}

func menuFilterHiddenState(view View) []MenuHiddenInput {
	values := currentPageAddressState(view, view.NavCollapsed)
	values.Del("menu_q")
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]MenuHiddenInput, 0, len(names))
	for _, name := range names {
		result = append(result, MenuHiddenInput{Name: name, Value: values.Get(name)})
	}
	return result
}

func projectNavigation(view View) ([]NavigationItemProps, []NavigationItemProps) {
	favoritePages := authorizedFavoritePages(view.Navigation, view.FavoritePages)
	favoriteSet := make(map[PageID]bool, len(favoritePages))
	for _, page := range favoritePages {
		favoriteSet[page] = true
	}
	leaves := make(map[PageID]NavItem)
	for _, item := range view.Navigation {
		collectNavigationLeaves(item, leaves)
	}
	favorites := make([]NavigationItemProps, 0, len(favoritePages))
	for _, page := range favoritePages {
		if leaf, ok := leaves[page]; ok && navigationSearchScore(leaf, view.MenuQuery) > 0 {
			favorites = append(favorites, navigationLeafProps(view, leaf, true))
		}
	}
	sort.SliceStable(favorites, func(left, right int) bool { return favorites[left].MatchScore > favorites[right].MatchScore })
	items := make([]NavigationItemProps, 0, len(view.Navigation))
	for _, item := range view.Navigation {
		if projected, ok := projectNavigationItem(view, item, favoriteSet); ok {
			items = append(items, projected)
		}
	}
	sort.SliceStable(items, func(left, right int) bool { return items[left].MatchScore > items[right].MatchScore })
	return favorites, items
}

func collectNavigationLeaves(item NavItem, into map[PageID]NavItem) {
	if len(item.Children) == 0 {
		into[item.Page] = item
		return
	}
	for _, child := range item.Children {
		collectNavigationLeaves(child, into)
	}
}

func projectNavigationItem(view View, item NavItem, favorites map[PageID]bool) (NavigationItemProps, bool) {
	if len(item.Children) == 0 {
		if favorites[item.Page] || navigationSearchScore(item, view.MenuQuery) == 0 {
			return NavigationItemProps{}, false
		}
		return navigationLeafProps(view, item, false), true
	}
	groupScore := navigationSearchScore(item, view.MenuQuery)
	children := make([]NavigationItemProps, 0, len(item.Children))
	for _, child := range item.Children {
		if favorites[child.Page] || navigationSearchScore(child, view.MenuQuery) == 0 {
			continue
		}
		children = append(children, navigationLeafProps(view, child, false))
	}
	sort.SliceStable(children, func(left, right int) bool { return children[left].MatchScore > children[right].MatchScore })
	if len(children) == 0 {
		return NavigationItemProps{}, false
	}
	active := navigationChildrenActive(children)
	expanded := active
	if saved, ok := view.NavigationGroupOpen[item.Page]; ok {
		expanded = saved
	}
	if view.MenuQuery != "" {
		expanded = true
	}
	return NavigationItemProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Page:      item.Page, Label: item.Label, Icon: item.Icon, Count: item.Count,
		Href: navigationHrefForItem(view, item), Active: active, Expanded: expanded, ForceOpen: view.MenuQuery != "",
		MatchScore: maxNavigationScore(groupScore, children), Children: children, Navigate: view.Navigate,
	}, true
}

func navigationLeafProps(view View, item NavItem, favorite bool) NavigationItemProps {
	detail := ""
	if view.MenuQuery != "" {
		detail = item.Description
	}
	return NavigationItemProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Page:      item.Page, Label: item.Label, Icon: item.Icon, Count: item.Count,
		Href: navigationHrefForItem(view, item), Active: navigationPageActive(item.Page, view.Page),
		Favorite: favorite, FavoriteHref: favoriteToggleHref(view, item.Page), MatchScore: navigationSearchScore(item, view.MenuQuery), MatchDetail: detail, Navigate: view.Navigate,
	}
}

func maxNavigationScore(groupScore int, children []NavigationItemProps) int {
	result := groupScore
	for _, child := range children {
		if child.MatchScore > result {
			result = child.MatchScore
		}
	}
	return result
}

func navigationPageActive(item, current PageID) bool {
	return item == current || item == PagePeople && current == PagePerson
}

func navigationChildrenActive(children []NavigationItemProps) bool {
	for _, child := range children {
		if child.Active {
			return true
		}
	}
	return false
}

func favoriteToggleHref(view View, page PageID) string {
	next := view
	next.FavoritePages = make([]PageID, 0, len(view.FavoritePages)+1)
	found := false
	for _, favorite := range view.FavoritePages {
		if favorite == page {
			found = true
			continue
		}
		next.FavoritePages = append(next.FavoritePages, favorite)
	}
	if !found {
		next.FavoritePages = append([]PageID{page}, next.FavoritePages...)
	}
	href := currentPageHref(next, next.NavCollapsed)
	if len(next.FavoritePages) == 0 {
		return withExplicitEmptyQuery(href, "favorites")
	}
	return href
}

// NavigationSidebar renders independently scrolling, searchable navigation.
func NavigationSidebar(props NavigationSidebarProps) ui.Node {
	query := ui.UseState(props.Filter.Query)
	propQuery := props.Filter.Query
	ui.UseEffectOf(func() func() {
		if query.Get() != propQuery {
			query.Set(propQuery)
		}
		return nil
	}, propQuery)
	if props.Search != nil {
		search := props.Search
		// Local input must project immediately; the effect synchronizes genuine
		// route changes without overwriting a draft with unchanged route props.
		props = search(query.Get())
		props.Search = search
		filterInput := props.Filter.OnInput
		filterClear := props.Filter.OnClear
		props.Filter.OnInput = func(next string) {
			query.Set(next)
			if filterInput != nil {
				filterInput(next)
			}
		}
		props.Filter.OnClear = func() {
			query.Set("")
			if filterClear != nil {
				filterClear()
			}
		}
	}
	// The drawer's open/close and Escape wiring are unconditional hook calls
	// (GWC requires a stable hook order every render); the resulting handlers
	// are only ever reachable in practice at narrow viewports, where CSS is
	// the sole thing that makes the trigger focusable and hit-testable.
	closeDrawer := ui.UseEvent(func(ui.MouseEvent) {
		if props.OnClose != nil {
			props.OnClose()
		}
	})
	onEscape := ui.UseEvent(func(event ui.KeyboardEvent) {
		if drawerEscapeCloses(event.GetKey()) && props.OnClose != nil {
			props.OnClose()
		}
	})
	useDrawerFocusTrap("workspace-navigation", "nav-drawer-trigger", props.Open)

	class := "sidebar"
	if props.Collapsed {
		class += " collapsed"
	}
	backdropClass := "nav-drawer-backdrop"
	if props.Open {
		class += " nav-drawer-open"
		backdropClass += " nav-drawer-open"
	}
	backdrop := html.Div(html.Props{
		Class:   backdropClass,
		Raw:     map[string]any{"aria-hidden": "true"},
		OnClick: closeDrawer,
	})
	children := []ui.Node{html.Div(html.Props{Class: "tenant"}, ui.Text(props.Tenant))}
	if !props.Collapsed {
		children = append(children, ui.CreateElement(MenuFilter, props.Filter))
	}
	menu := make([]ui.Node, 0, len(props.Favorites)+len(props.Items)+2)
	if len(props.Favorites) > 0 {
		menu = append(menu, html.Li(html.Props{Class: "nav-section-label"}, ui.Text(props.Text("nav.favorites"))))
		for _, item := range props.Favorites {
			menu = append(menu, ui.CreateElement(NavigationItem, item))
		}
		if len(props.Items) > 0 {
			menu = append(menu, html.Li(html.Props{Class: "nav-section-label"}, ui.Text(props.Text("nav.all"))))
		}
	}
	for _, item := range props.Items {
		if props.Collapsed && len(item.Children) > 0 {
			item.Children = nil
		}
		menu = append(menu, ui.CreateElement(NavigationItem, item))
	}
	// Matching support pages belong beside search results, not below an empty
	// scroll region. Keep only the non-matches in the stable recovery area.
	supportItems := make([]NavigationItemProps, 0, len(props.Support))
	for _, item := range props.Support {
		if strings.TrimSpace(props.Filter.Query) != "" && item.MatchScore > 0 {
			menu = append(menu, ui.CreateElement(NavigationItem, item))
		} else {
			supportItems = append(supportItems, item)
		}
	}
	if len(menu) == 0 {
		emptyText := props.EmptyText
		if emptyText == "" {
			emptyText = props.Text("nav.none")
		}
		menu = append(menu, html.Li(html.Props{Class: "nav-empty", Raw: map[string]any{"role": "status"}}, ui.Text(emptyText)))
	}
	children = append(children, ui.CreateElement(ScrollRegion, ScrollRegionProps{
		Tag: "nav", ID: "primary-nav", Class: "primary-nav", Focusable: true, RestoreScroll: true,
		Aria: map[string]string{"label": props.Text("nav.main")}, Children: []ui.Node{html.Ul(html.Props{}, menu...)},
	}))
	if len(supportItems) > 0 {
		support := make([]ui.Node, 0, len(supportItems))
		for _, item := range supportItems {
			support = append(support, ui.CreateElement(NavigationItem, item))
		}
		children = append(children, html.Nav(html.Props{Class: "nav-bottom", Aria: map[string]string{"label": props.Text("nav.support")}}, support...))
	}
	// A real user can only ever reach the trigger that sets Open at narrow
	// viewports (CSS removes it from hit-testing and the tab order at wider
	// ones), so dialog semantics only ever appear there too. Every other
	// render — including every default and desktop SSR document — keeps the
	// plain complementary-landmark contract WEB-048 pins.
	var role string
	var rawAttrs map[string]any
	if attrs := navigationDrawerDialogAttrs(props.Open); attrs != nil {
		rawAttrs = attrs
		role = "dialog"
	}
	// UIPOLISH-004 "drawer": at narrow viewports ".primary-nav" gives up its
	// own overflow (declared overflow:visible!important there, see
	// navigation_components_test.go's coverage of that breakpoint) and this
	// aside becomes the sole scroll owner instead -- two independently
	// scrolling regions nested inside each other is exactly the "page and
	// table compete for the same gesture" pattern RED forbids. Rendering it
	// through the same ScrollRegion component as every other scroll owner
	// keeps it keyboard-reachable there too.
	return html.Fragment(backdrop, ui.CreateElement(ScrollRegion, ScrollRegionProps{
		Tag: "aside", ID: "workspace-navigation", Class: class, Role: role, Focusable: true, RestoreScroll: true,
		Aria: map[string]string{"label": props.Text("nav.workspace")}, Raw: rawAttrs, OnKeyDown: onEscape, Children: children,
	}))
}

// navigationDrawerDialogAttrs returns the raw attributes that make the
// overlay drawer an accessible dialog while, and only while, it is open. It
// is a pure function so the open-state contract is provable directly, not
// only inferred from an SSR document that can only ever show it closed.
// Role is set separately (html.Props has a typed field for it); this covers
// what does not.
func navigationDrawerDialogAttrs(open bool) map[string]any {
	if !open {
		return nil
	}
	return map[string]any{"aria-modal": "true"}
}

// navigationDrawerTriggerAria computes the mobile drawer trigger's
// accessible wiring independent of any hook state, so its contract (the
// disclosure pattern: aria-haspopup, aria-expanded, aria-controls) is
// provable from a plain Go test without a live client render.
func navigationDrawerTriggerAria(open bool, label string) map[string]string {
	return map[string]string{
		"label": label, "haspopup": "dialog", "expanded": fmt.Sprint(open), "controls": "workspace-navigation",
	}
}

// NavigationItem renders either a leaf with its favorite control or a native,
// keyboard-operable disclosure group.
func NavigationItem(props NavigationItemProps) ui.Node {
	if len(props.Children) == 0 {
		linkProps := html.Props{Class: "nav-link", Aria: map[string]string{"label": props.Label}, Raw: map[string]any{"title": props.Label}}
		if props.Active {
			linkProps.Aria["current"] = "page"
		}
		copy := []ui.Node{html.Span(html.Props{Class: "nav-label"}, ui.Text(props.Label))}
		class := "nav-link"
		if props.MatchDetail != "" {
			class += " has-search-detail"
			copy = append(copy, html.Small(html.Props{Class: "nav-search-detail"}, ui.Text(props.MatchDetail)))
		}
		linkProps.Class = class
		content := []ui.Node{navIcon(props.Icon), html.Span(html.Props{Class: "nav-copy"}, copy...)}
		if props.Count > 0 {
			content = append(content, html.Span(html.Props{Class: "nav-count"}, ui.Text(fmt.Sprint(props.Count))))
		}
		children := []ui.Node{softwareLink(props.Navigate, linkProps, props.Href, content...)}
		if props.FavoriteHref != "" {
			label, glyph := props.Text("nav.favorite_add", map[string]string{"label": props.Label}), "☆"
			if props.Favorite {
				label, glyph = props.Text("nav.favorite_remove", map[string]string{"label": props.Label}), "★"
			}
			children = append(children, softwareLink(props.Navigate, html.Props{Class: "nav-favorite", Aria: map[string]string{"label": label}, Raw: map[string]any{"title": label}}, props.FavoriteHref, ui.Text(glyph)))
		}
		return html.Li(html.Props{Class: "nav-entry"}, children...)
	}
	class := "nav-group"
	if props.Active {
		class += " current"
	}
	raw := map[string]any{}
	if props.Expanded {
		raw["open"] = true
	}
	data := map[string]string{"hcm-nav-group": string(props.Page)}
	if props.ForceOpen {
		data["hcm-nav-force-open"] = "true"
	}
	children := make([]ui.Node, 0, len(props.Children))
	for _, child := range props.Children {
		children = append(children, ui.CreateElement(NavigationItem, child))
	}
	summary := []ui.Node{navIcon(props.Icon), html.Span(html.Props{Class: "nav-label"}, ui.Text(props.Label))}
	if props.Count > 0 {
		summary = append(summary, html.Span(html.Props{Class: "nav-count"}, ui.Text(fmt.Sprint(props.Count))))
	}
	summary = append(summary, html.Span(html.Props{Class: "nav-chevron", Aria: map[string]string{"hidden": "true"}}, ui.Text("›")))
	return html.Li(html.Props{}, html.Details(html.Props{Class: class, Raw: raw, Data: data},
		html.Summary(html.Props{Class: "nav-group-summary"}, summary...),
		html.Ul(html.Props{Class: "subnav"}, children...),
	))
}

// MenuFilter is an SSR-safe GET form upgraded to software navigation in WASM.
func MenuFilter(props MenuFilterProps) ui.Node {
	query := props.Query
	inputProps := html.Props{ID: "menu-filter", Name: "menu_q", Value: props.Query,
		Raw: map[string]any{"type": "search", "placeholder": props.Text("nav.filter_placeholder"), "aria-label": props.Text("nav.filter")}}
	formProps := html.Props{Class: "menu-filter", Action: props.Action, Method: "get", Raw: map[string]any{"role": "search"}}
	if props.OnFilter != nil || props.OnInput != nil {
		inputProps.OnInput = ui.UseEvent(func(event ui.InputEvent) {
			query = event.GetValue()
			if props.OnInput != nil {
				props.OnInput(query)
			}
		})
	}
	if props.OnFilter != nil {
		onFilter := props.OnFilter
		formProps.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			onFilter(query)
		})
	}
	children := []ui.Node{
		html.Label(html.Props{For: "menu-filter", Class: "sr-only"}, ui.Text(props.Text("nav.filter"))),
		html.Div(html.Props{Class: "menu-filter-control"}, html.Tag("input", inputProps),
			html.Button(html.Props{Class: "menu-filter-submit", Type: "submit", Aria: map[string]string{"label": props.Text("nav.filter_apply")}, Raw: map[string]any{"title": props.Text("nav.filter_apply")}}, ui.Text("⌕"))),
	}
	for _, hidden := range props.Hidden {
		children = append(children, html.Tag("input", html.Props{Name: hidden.Name, Value: hidden.Value, Raw: map[string]any{"type": "hidden"}}))
	}
	if props.Query != "" {
		clearNavigate := menuFilterClearNavigate(props)
		children = append(children, softwareLink(clearNavigate, html.Props{Class: "menu-filter-clear"}, props.ClearHref, ui.Text(props.Text("nav.filter_clear"))))
	}
	return html.Form(formProps, children...)
}

func menuFilterClearNavigate(props MenuFilterProps) func(string) {
	if props.OnClear == nil {
		return props.Navigate
	}
	clear, navigate := props.OnClear, props.Navigate
	return func(href string) {
		clear()
		if navigate != nil {
			navigate(href)
		}
	}
}
