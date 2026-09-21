package productui

import (
	"strings"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// asyncRegionFailedClass marks a loading proxy that stands for a failed read.
// Its styles stop the loading animations and draw the failure notice over
// the unchanged body.
const asyncRegionFailedClass = "loading-proxy-failed"

// AsyncRegionFailureProps is the visible failure state of an async region
// (REV-090-01): a short title, a plain recovery line, and a Retry control
// that re-runs the same route read. The copy reuses UXLIVE-005's localized
// recovery strings (shell.live_unavailable, shell.load_recovery,
// shell.load_retry).
type AsyncRegionFailureProps struct {
	Locale LocaleContext
	// Retry re-runs the failed read in place (the live router revalidates
	// the current route). When nil, RetryHref is offered as a link instead.
	Retry func()
	// RetryHref is the failed page's own address; Navigate, when set, makes
	// it a software navigation.
	RetryHref string
	Navigate  func(string)
}

// AsyncRegionFailureCovered reports whether a page's content region fails
// through the async-region contract (REV-090-01): a genuine fetch failure
// renders in the same size-compatible shape the region loads in, rather than
// the older page-level View.LoadError panel stacked above an empty page.
// Pages outside this set (Admin, Insights, Home and others) keep their own
// partial-degradation contract driven by View.LoadError.
func AsyncRegionFailureCovered(page PageID) bool {
	switch page {
	case PagePeople, PageWork, PageHistory, PageOrganization:
		return true
	default:
		return false
	}
}

// BuildContentFailure renders a route outlet's content region in the
// AsyncRegionFailure state for the live router, whose shell is a persistent
// layout: the region keeps its loading geometry and shows a visible,
// assertively announced message with a Retry control.
func BuildContentFailure(view View, retry func()) ui.Node {
	message, _, err := AsyncRegionFailure.Announcement(view.Locale, view.Locale.Text("shell.load_recovery"))
	if err != nil {
		message = view.Locale.Text("shell.live_unavailable")
	}
	return ui.CreateElement(LoadingProxy, LoadingProxyProps{
		Page: view.Page, State: AsyncRegionFailure, Message: message,
		Failure: &AsyncRegionFailureProps{Locale: view.Locale, Retry: retry, RetryHref: statefulHref(view, view.Page), Navigate: view.Navigate},
	})
}

// asyncRegionFailureNotice is the visible failure message. It is its own
// component so the Retry handler's hook has a component context.
func asyncRegionFailureNotice(props AsyncRegionFailureProps) ui.Node {
	locale := props.Locale
	if strings.TrimSpace(locale.Resolved) == "" {
		locale = ResolveProductLocale("")
	}
	retryLabel := locale.Text("shell.load_retry")
	var retry ui.Node
	if props.Retry != nil {
		run := props.Retry
		retry = html.Button(html.Props{
			Type: "button", Class: "button secondary async-region-retry",
			OnClick: ui.UseEvent(func(ui.MouseEvent) { run() }),
		}, ui.Text(retryLabel))
	} else if strings.TrimSpace(props.RetryHref) != "" {
		retry = softwareLink(props.Navigate, html.Props{Class: "button secondary async-region-retry"}, props.RetryHref, ui.Text(retryLabel))
	}
	children := []ui.Node{
		html.Strong(html.Props{Class: "async-region-failure-title"}, ui.Text(locale.Text("shell.live_unavailable"))),
		html.P(html.Props{Class: "async-region-failure-detail"}, ui.Text(locale.Text("shell.load_recovery"))),
	}
	if retry != nil {
		children = append(children, retry)
	}
	return html.Div(html.Props{Class: "async-region-failure", Raw: map[string]any{"role": "alert", "aria-live": "assertive", "aria-atomic": "true"}}, children...)
}

func asyncRegionFailureStylesheet() string {
	return buildTypedSheet(declareAsyncRegionFailureStyles)
}

// declareAsyncRegionFailureStyles draws the failure notice over the proxy
// body. The notice is absolutely positioned inside .loading-proxy (already a
// positioned, overflow-hidden box with the loading min-height), so it adds no
// height of its own: the failed region measures exactly like the loading one.
func declareAsyncRegionFailureStyles() {
	declareGlobal(".loading-proxy-failed .loading-progress",
		gwccss.Display.None,
	)
	declareGlobal(".loading-proxy-failed .loading-block,.loading-proxy-failed .loading-panel",
		gwccss.Raw("opacity", ".35"),
	)
	// The shimmer is a motion-gated :after with a stronger selector; a failed
	// region must not look like it is still working.
	declareGlobal(".loading-proxy-failed .loading-block:after",
		gwccss.Raw("animation", "none !important"),
		gwccss.Raw("display", "none !important"),
	)
	declareGlobal(".async-region-failure",
		gwccss.Position.Absolute,
		gwccss.ZIndex(2),
		gwccss.Raw("inset-block-start", "0"),
		gwccss.Raw("inset-inline", "0"),
		gwccss.Display.Grid,
		gwccss.Raw("justify-items", "start"),
		gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("padding", "20px 24px"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".async-region-failure-title",
		gwccss.Raw("font-size", "1rem"),
	)
	declareGlobal(".async-region-failure-detail",
		gwccss.Raw("margin", "0"),
		gwccss.Raw("max-width", "60ch"),
	)
}
