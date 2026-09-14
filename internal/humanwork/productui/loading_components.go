package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// LoadingProxyProps describes the shape of an unresolved product surface.
// The proxy contains no guessed business values and is deliberately
// non-interactive; the surrounding shell owns the accessible busy message.
//
// State selects which of the AsyncRegionState members this proxy stands in
// for. The zero value is AsyncRegionLoading, matching every call site that
// predates UXAUDIT-012 and keeping their rendered output byte-identical.
// Message is the failure detail surfaced only when State is
// AsyncRegionFailure; it is ignored otherwise.
type LoadingProxyProps struct {
	Page    PageID
	State   AsyncRegionState
	Message string
}

// LoadingRegionPage is the stable outlet used by route transitions. Keeping
// this identity on both a resolved page and its proxy lets the browser retain
// the outlet's scroll and focus state while the authorized projection changes.
const LoadingRegionPage = "page-content"

// LoadingContractVersion identifies the geometry contract encoded by a
// loading proxy. It is presentation metadata only; changing it deliberately
// makes browser visual baselines and CLS checks fail together.
const LoadingContractVersion = "v1"

// LoadingGeometry describes the coarse shape a page proxy reserves. Skeletons
// do not copy business values, but they must reserve the same kind of region so
// the final projection can replace them without a major layout shift.
type LoadingGeometry struct {
	Layout  string
	Rows    int
	Columns int
}

// LoadingProxyGeometry returns the immutable geometry contract for a page
// family. Unknown pages use the settings shape, which is the conservative
// fallback for a route that cannot be classified.
func LoadingProxyGeometry(page PageID) LoadingGeometry {
	switch page {
	case PagePeople, PageHistory:
		return LoadingGeometry{Layout: "table", Rows: 8, Columns: 4}
	case PagePerson, PageMyself:
		return LoadingGeometry{Layout: "profile", Rows: 10, Columns: 2}
	case PageOrganization, PageInsights:
		return LoadingGeometry{Layout: "analysis", Rows: 13, Columns: 2}
	case PageHome, PageWork, PageJourneys:
		return LoadingGeometry{Layout: "work", Rows: 12, Columns: 2}
	default:
		return LoadingGeometry{Layout: "settings", Rows: 11, Columns: 2}
	}
}

// BuildLoading returns the real product shell with a component-shaped proxy
// in place of database and network-backed content. The router swaps this tree
// atomically for Build(view) when every required answer has resolved.
func BuildLoading(view View) ui.Node {
	view.Loading = true
	view.ContentLoading = false
	view.Refreshing = false
	view.RefreshingRegion = ""
	view.LoadError = ""
	return appShell(view, ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: view.Page}))
}

// BuildContentLoading keeps already-resolved application chrome mounted while
// only the destination page projection is unresolved. This is the normal SPA
// transition after cold boot and avoids turning global navigation, identity,
// and notifications back into placeholders for every route change.
func BuildContentLoading(view View) ui.Node {
	view.Loading = false
	view.ContentLoading = true
	view.Refreshing = false
	view.RefreshingRegion = ""
	view.LoadError = ""
	return appShell(view, ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: view.Page}))
}

// BuildRefreshing keeps the last authorized page tree visible while a newer
// projection is in flight. The shell marks the content busy and supplies a
// progress cue; it never fabricates pending values or changes action authority.
func BuildRefreshing(view View) ui.Node {
	view.Loading = false
	view.ContentLoading = false
	if isFocusedRefreshRegion(view.RefreshingRegion) {
		view.Refreshing = false
		return Build(view)
	}
	view.Refreshing = true
	return Build(view)
}

// BuildFailure keeps already-resolved application chrome mounted while the
// destination content region's own fetch could not be completed and no
// earlier resolved answer for it exists to fall back to (the case
// BuildRefreshing already covers when there is one). It renders through the
// exact same size-compatible proxy as BuildContentLoading -- see
// LoadingProxy's doc comment -- so a failed region takes up the same space a
// loading or eventually resolved one would, rather than collapsing to a
// small error message.
//
// message is the failure detail, announced assertively rather than
// fabricated into a placeholder business value. It is carried only through
// LoadingProxyProps.Message, never through View.LoadError: that field
// drives appShellWithHeading's own, older, page-level LoadError branch
// (stacking an unavailablePanel above whatever the page itself still
// renders, used by pages such as Admin that keep showing their still-good
// cards next to one degraded one), which is a different, pre-existing
// degradation contract. BuildFailure clears any LoadError already on the
// incoming view for exactly this reason: leaving it set would make that
// older branch stack its own, differently-sized panel on top of this
// function's size-matched proxy, silently defeating the geometry guarantee
// this function exists to provide.
func BuildFailure(view View, message string) ui.Node {
	view.Loading = false
	view.ContentLoading = false
	view.Refreshing = false
	view.RefreshingRegion = ""
	view.LoadError = ""
	return appShell(view, ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: view.Page, State: AsyncRegionFailure, Message: message}))
}

// LoadingProxy preserves the broad geometry of each page family, avoiding
// layout jumps without drawing fake names, amounts, statuses, or permissions.
//
// Loading, Empty and Failure all render the identical loadingProxyBody
// markup -- same classes, same declared CSS min-heights -- because
// UXAUDIT-012's Fault clause requires the failure state to preserve geometry
// exactly as the loading state does: "a region that collapses when its
// fetch fails is the same defect as one that collapses while loading."
// Reusing one body call rather than writing a second, similar-looking one
// for failure is what makes that proof exact instead of approximate.
//
// The switch has no default case: an AsyncRegionState this function does
// not recognize is refused with a visible diagnostic panel rather than
// silently rendered as either the decorative (aria-hidden) shimmer or an
// unannounced failure -- both of which would hide a real programming
// mistake from assistive technology.
func LoadingProxy(props LoadingProxyProps) ui.Node {
	geometry := LoadingProxyGeometry(props.Page)
	class := "loading-proxy loading-proxy-" + safeLoadingPageClass(props.Page)
	body := html.Fragment(
		html.Div(html.Props{Class: "loading-progress"}),
		loadingProxyBody(props.Page),
	)
	switch props.State {
	case AsyncRegionLoading, AsyncRegionEmpty, AsyncRegionStale, AsyncRegionResolved:
		return html.Section(html.Props{Class: class, Raw: map[string]any{"aria-hidden": "true", "data-async-region": LoadingRegionPage, "data-loading-contract": LoadingContractVersion, "data-loading-layout": geometry.Layout, "data-preserve-scroll": "true", "data-preserve-focus": "true"}}, body)
	case AsyncRegionFailure:
		return html.Section(html.Props{Class: class, Raw: map[string]any{"role": "alert", "aria-live": "assertive"}},
			html.Span(html.Props{Class: "sr-only"}, ui.Text(strings.TrimSpace(props.Message))),
			body,
		)
	default:
		return html.Section(html.Props{Class: class + " loading-proxy-invalid-state", Raw: map[string]any{"role": "alert"}},
			ui.Text(fmt.Sprintf("productui: %v", fmt.Errorf("%w: %v", ErrUnknownAsyncRegionState, props.State))),
		)
	}
}

func loadingProxyBody(page PageID) ui.Node {
	switch page {
	case PagePeople, PageHistory:
		return html.Div(html.Props{Class: "loading-table-layout"},
			loadingToolbar(),
			loadingPanel("loading-table-panel", loadingTable(7)),
		)
	case PagePerson, PageMyself:
		return html.Div(html.Props{Class: "loading-profile-layout"},
			loadingPanel("loading-profile-hero", html.Div(html.Props{Class: "loading-profile-head"},
				loadingBlock("loading-avatar"),
				html.Div(html.Props{Class: "loading-copy"}, loadingBlock("loading-line loading-line-title"), loadingBlock("loading-line loading-line-short")),
			)),
			html.Div(html.Props{Class: "loading-two-column"},
				loadingPanel("", loadingFacts(6)),
				loadingPanel("", loadingRows(4, false)),
			),
		)
	case PageOrganization, PageInsights:
		return html.Div(html.Props{Class: "loading-analysis-layout"},
			loadingMetrics(3),
			html.Div(html.Props{Class: "loading-two-column"},
				loadingPanel("loading-chart-panel", loadingBars(5)),
				loadingPanel("", loadingRows(5, true)),
			),
		)
	case PageHome, PageWork, PageJourneys:
		return html.Div(html.Props{Class: "loading-work-layout"},
			loadingToolbar(),
			html.Div(html.Props{Class: "loading-two-column"},
				loadingPanel("", loadingRows(6, true)),
				loadingPanel("loading-detail-proxy", loadingFacts(6)),
			),
		)
	default:
		return html.Div(html.Props{Class: "loading-settings-layout"},
			html.Div(html.Props{Class: "loading-two-column"},
				loadingPanel("", loadingRows(6, false)),
				loadingPanel("", loadingFacts(5)),
			),
		)
	}
}

func loadingToolbar() ui.Node {
	return html.Div(html.Props{Class: "loading-toolbar"},
		loadingBlock("loading-line loading-line-medium"),
		loadingBlock("loading-control"),
	)
}

func loadingMetrics(count int) ui.Node {
	items := make([]ui.Node, 0, count)
	for index := 0; index < count; index++ {
		items = append(items, loadingPanel("loading-metric", html.Fragment(
			loadingBlock("loading-line loading-line-short"),
			loadingBlock("loading-value"),
		)))
	}
	return html.Div(html.Props{Class: "loading-metrics"}, items...)
}

func loadingRows(count int, avatars bool) ui.Node {
	rows := make([]ui.Node, 0, count)
	for index := 0; index < count; index++ {
		children := make([]ui.Node, 0, 3)
		if avatars {
			children = append(children, loadingBlock("loading-avatar loading-avatar-small"))
		}
		children = append(children,
			html.Div(html.Props{Class: "loading-copy"},
				loadingBlock("loading-line loading-line-medium"),
				loadingBlock("loading-line loading-line-short"),
			),
			loadingBlock("loading-chip"),
		)
		rows = append(rows, html.Div(html.Props{Class: "loading-row"}, children...))
	}
	return html.Div(html.Props{Class: "loading-rows"}, rows...)
}

func loadingTable(count int) ui.Node {
	rows := make([]ui.Node, 0, count+1)
	rows = append(rows, html.Div(html.Props{Class: "loading-table-row loading-table-head"},
		loadingBlock("loading-line"), loadingBlock("loading-line"), loadingBlock("loading-line"), loadingBlock("loading-line"),
	))
	for index := 0; index < count; index++ {
		rows = append(rows, html.Div(html.Props{Class: "loading-table-row"},
			loadingBlock("loading-line loading-line-medium"), loadingBlock("loading-line"), loadingBlock("loading-line loading-line-short"), loadingBlock("loading-chip"),
		))
	}
	return html.Div(html.Props{Class: "loading-table"}, rows...)
}

func loadingFacts(count int) ui.Node {
	rows := make([]ui.Node, 0, count)
	for index := 0; index < count; index++ {
		rows = append(rows, html.Div(html.Props{Class: "loading-fact"},
			loadingBlock("loading-line loading-line-short"), loadingBlock("loading-line loading-line-medium"),
		))
	}
	return html.Div(html.Props{Class: "loading-facts"}, rows...)
}

func loadingBars(count int) ui.Node {
	rows := make([]ui.Node, 0, count)
	for index := 0; index < count; index++ {
		rows = append(rows, html.Div(html.Props{Class: "loading-bar-row"},
			loadingBlock("loading-line loading-line-short"),
			loadingBlock("loading-bar loading-bar-"+fmt.Sprint(5+index)),
		))
	}
	return html.Div(html.Props{Class: "loading-bars"}, rows...)
}

func loadingPanel(class string, content ui.Node) ui.Node {
	return html.Div(html.Props{Class: strings.TrimSpace("loading-panel " + class)}, content)
}

func loadingBlock(class string) ui.Node {
	return html.Span(html.Props{Class: strings.TrimSpace("loading-block " + class)})
}

func safeLoadingPageClass(page PageID) string {
	if _, ok := LookupPage(page); ok {
		return string(page)
	}
	return string(PageHome)
}

func isFocusedRefreshRegion(region string) bool {
	switch region {
	case RefreshRegionPeopleDirectory:
		return true
	default:
		return false
	}
}
