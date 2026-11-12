package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// #3839: in the icon rail an item with child pages (Chat, Workflows, My Work,
// Admin) used to render as a single link to its first page, so Chat's Agents
// page was unreachable without expanding the navigation. The rail now gives
// such an item a trigger that opens a small flyout of its children on hover,
// on keyboard focus and on activation (a tap on touch). The flyout is a list
// of links; Escape closes it and returns focus to the trigger.
//
// The flyout opens from the stylesheet (:hover, :focus-within), so it works in
// the server-rendered document before the client is running; the state kept
// here only keeps aria-expanded true to what is shown and carries Escape.

const (
	railFlyoutClosed    = ""
	railFlyoutOpen      = "open"
	railFlyoutDismissed = "dismissed"
)

// NavigationRailFlyout renders one rail item that has children.
func NavigationRailFlyout(props NavigationItemProps) ui.Node {
	state := ui.UseState(railFlyoutClosed)
	triggerRef := ui.UseDOMRef()
	listID := "nav-flyout-" + string(props.Page)

	open := ui.UseEvent(func(ui.MouseEvent) { state.Set(railFlyoutOpen) })
	shut := ui.UseEvent(func(ui.MouseEvent) { state.Set(railFlyoutClosed) })
	toggle := ui.UseEvent(func(ui.MouseEvent) {
		if state.Get() == railFlyoutOpen {
			state.Set(railFlyoutClosed)
			return
		}
		state.Set(railFlyoutOpen)
	})
	focusIn := ui.UseEvent(func(ui.FocusEvent) { state.Set(railFlyoutOpen) })
	focusOut := ui.UseEvent(func(ui.FocusEvent) {
		if state.Get() == railFlyoutOpen {
			state.Set(railFlyoutClosed)
		}
	})
	onKey := ui.UseEvent(func(event ui.KeyboardEvent) {
		if drawerEscapeCloses(event.GetKey()) && state.Get() != railFlyoutDismissed {
			state.Set(railFlyoutDismissed)
			triggerRef.Focus()
		}
	})

	class := "nav-entry nav-flyout"
	switch state.Get() {
	case railFlyoutOpen:
		class += " is-open"
	case railFlyoutDismissed:
		class += " is-dismissed"
	}
	triggerClass := "nav-link nav-flyout-trigger"
	if props.Active {
		triggerClass += " has-current"
	}
	trigger := html.Button(html.PropsOf(
		html.Class(triggerClass), html.Type("button"),
		html.OnClick(toggle), html.OnFocus(focusIn), html.OnBlur(focusOut),
		html.Aria("label", props.Label), html.Aria("haspopup", "true"),
		html.Aria("expanded", boolString(state.Get() == railFlyoutOpen)), html.Aria("controls", listID),
		html.Attr("title", props.Label), html.Attr("data-nav-tooltip", props.Label),
		html.Ref(triggerRef),
	), navIcon(props.Icon))

	items := []ui.Node{html.Li(html.Props{Class: "nav-flyout-title"}, ui.Text(props.Label))}
	for _, child := range props.Children {
		items = append(items, ui.CreateElement(NavigationRailFlyoutLink, child))
	}
	return html.Li(html.Props{Class: class, OnMouseEnter: open, OnMouseLeave: shut, OnKeyDown: onKey},
		trigger,
		html.Ul(html.Props{ID: listID, Class: "nav-flyout-list", Aria: map[string]string{"label": props.Label}}, items...),
	)
}

// NavigationRailFlyoutLink is one child page inside a flyout.
func NavigationRailFlyoutLink(props NavigationItemProps) ui.Node {
	linkProps := html.Props{Class: "nav-flyout-link"}
	if props.Active {
		linkProps.Aria = map[string]string{"current": "page"}
		linkProps.DataAttr = html.DataAttribute{Name: "hcm-nav-current", Value: "true"}
	}
	return html.Li(html.Props{},
		softwareLink(navigationSelectNavigate(props.Navigate, props.OnSelect), linkProps, props.Href,
			navIcon(props.Icon), html.Span(html.Props{Class: "nav-flyout-label"}, ui.Text(props.Label))))
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func chatDesignNavFlyoutStylesheet() string {
	const rail = `.sidebar.collapsed `
	shown := rail + `.nav-flyout.is-open>.nav-flyout-list,` +
		rail + `.nav-flyout:not(.is-dismissed):is(:hover,:focus-within)>.nav-flyout-list`
	return `@media(min-width:761px){` +
		rail + `.nav-flyout{position:relative}` +
		rail + `.nav-flyout-trigger{width:100%;background:transparent;border:0;color:inherit;font:inherit;cursor:pointer}` +
		rail + `.nav-flyout-trigger:hover,` + rail + `.nav-flyout-trigger[aria-expanded=true]{background:var(--soft);color:var(--accent)}` +
		rail + `.nav-flyout-trigger.has-current{box-shadow:inset 3px 0 0 0 var(--accent)}` +
		`[dir=rtl] ` + rail + `.nav-flyout-trigger.has-current{box-shadow:inset -3px 0 0 0 var(--accent)}` +
		rail + `.nav-flyout-list{display:none;position:absolute;inset-inline-start:100%;top:0;z-index:40;margin:0;padding:6px;list-style:none;min-width:200px;` +
		`background:var(--surface);color:var(--ink);border:1px solid var(--line);border-radius:var(--hcm-radius-control);box-shadow:var(--hcm-shadow-raised);margin-inline-start:8px}` +
		rail + `.nav-flyout-list::before{content:"";position:absolute;inset-inline-end:100%;top:0;bottom:0;width:10px}` +
		shown + `{display:block}` +
		rail + `.nav-flyout-title{padding:6px 10px;font-size:var(--hcm-type-label);font-weight:600;color:var(--muted)}` +
		rail + `.nav-flyout-link{display:flex;align-items:center;gap:10px;min-height:40px;padding:8px 10px;border-radius:var(--hcm-radius-control);color:inherit;text-decoration:none;white-space:nowrap;font-size:.875rem;font-weight:500}` +
		rail + `.nav-flyout-link:hover,` + rail + `.nav-flyout-link:focus-visible,` + rail + `.nav-flyout-link[aria-current=page]{background:var(--soft);color:var(--accent)}` +
		rail + `.nav-flyout-link .nav-icon{flex:none}` +
		rail + `.nav-flyout-trigger::after{content:none!important}` +
		`.sidebar.collapsed:has(.nav-flyout:is(:hover,:focus-within,.is-open)),.sidebar.collapsed:has(.nav-flyout:is(:hover,:focus-within,.is-open)) .primary-nav{overflow:visible}` +
		`}` +
		`@media(forced-colors:active){` + rail + `.nav-flyout-list{border-color:CanvasText;background:Canvas;color:CanvasText}}`
}
