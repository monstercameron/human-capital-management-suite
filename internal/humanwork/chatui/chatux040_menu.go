package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-040: the sidebar's row and section menus. A menu is as wide as its
// longest item (never under 240 px or over 320 px), opens beside its button
// below the sidebar's head, and every item leads with a glyph in one column, so
// the labels start at one edge and none of them wraps to a second line.

const (
	chatux040MenuMin = 240.0
	chatux040MenuMax = 320.0
)

// chatux040RailMenuFit is the width a row or section menu is drawn at, given
// the width its longest item asks for.
func chatux040RailMenuFit(natural, viewportWidth float64) float64 {
	return min(max(natural, chatux040MenuMin), chatux040MenuMax, max(0, viewportWidth-16))
}

// chatux040RailMenuBounds is the rectangle a row or section menu stays inside:
// from the sidebar's start edge to the far edge of the page, and below the
// sidebar's head (its title and search), so the menu never covers them. fixed
// is the first row under the head. A rail that was not measured leaves bounds
// as they were.
func chatux040RailMenuBounds(bounds, rail, fixed chatLayerRect, viewportWidth float64, rtl bool) chatLayerRect {
	if !chatLayerRectUsable(rail) {
		return bounds
	}
	out := chatLayerRect{left: max(bounds.left, rail.left), top: bounds.top, right: min(bounds.right, viewportWidth), bottom: bounds.bottom}
	if rtl {
		out.left, out.right = bounds.left, min(bounds.right, rail.right)
	}
	if chatLayerRectUsable(fixed) {
		out.top = max(out.top, fixed.top)
	}
	return out
}

// chatux040MenuItem is one item of a row or section menu: a glyph, then the
// label in its own span.
func chatux040MenuItem(class, action, id, glyph, label string, disabled bool, extra ...string) ui.Node {
	data := map[string]string{"action": action, "id": id}
	if len(extra) > 0 && extra[0] != "" {
		data["extra"] = extra[0]
	}
	return html.Button(html.Props{Class: class, Type: "button", Role: "menuitem", Disabled: disabled, Data: data}, icon(glyph), html.Span(html.Props{Text: label}))
}

// ChatUX040Styles is the sidebar's menu width, the one selected-row look and the
// one text column of its rows. The selected row is a light tint with the bar;
// the hovered row is lighter than that, so a row under the pointer is never
// mistaken for the open one. The hover rule keeps its exclusions inside :where()
// so they add no specificity, and a selected row under the pointer keeps its
// own look.
const ChatUX040Styles = `
.chat-workspace{--rail-selected:color-mix(in srgb,var(--accent) 16%,var(--canvas));--rail-hover:color-mix(in srgb,var(--ink) 5%,transparent)}
.chat-workspace .rail-row-menu[data-chat-layer]{inline-size:max-content;min-inline-size:240px;max-inline-size:min(320px,calc(100vw - 16px))}
.chat-workspace .rail-row-menu .menu-item{inline-size:100%;white-space:nowrap}
.chat-workspace .rail-row-menu .menu-item>.chat-icon,.chat-workspace .rail-row-menu .menu-item>.rail-menu-check{flex:none;inline-size:16px;block-size:16px;text-align:center;line-height:16px}
.chat-workspace .rail-row-menu .menu-item>span:not(.rail-menu-check){flex:1 1 auto;min-inline-size:0}
.chat-workspace .chat-rail .chat-row{gap:8px;padding-inline-start:12px}
.chat-workspace .chat-rail .chat-row>:is(.kind-glyph,.avatar.tiny,.agent-dm-avatar){flex:none;inline-size:24px;block-size:24px;display:inline-flex;align-items:center;justify-content:center}
.chat-workspace .chat-rail .chat-row>.chat-icon:first-child{flex:none;inline-size:16px;block-size:16px;margin-inline:4px}
.chat-workspace .chat-rail .chat-row:where(:not(.selected):not([aria-expanded="true"])):hover{background:var(--rail-hover);color:var(--ink)}
.chat-workspace:not(:has([data-chatremove-overlay="page"])) .chat-rail .chat-row.selected,.chat-workspace:not(:has([data-chatremove-overlay="page"])) .chat-rail .chat-row.selected:hover{background:var(--rail-selected);color:var(--ink);font-weight:600;box-shadow:inset 3px 0 0 var(--accent)}
.chat-workspace[dir="rtl"]:not(:has([data-chatremove-overlay="page"])) .chat-rail .chat-row.selected,.chat-workspace[dir="rtl"]:not(:has([data-chatremove-overlay="page"])) .chat-rail .chat-row.selected:hover{box-shadow:inset -3px 0 0 var(--accent)}
.chat-workspace .chat-rail .chat-row.selected::before{content:none}
`
