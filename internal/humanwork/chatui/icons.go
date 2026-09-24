package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// iconPaths are 24-unit stroke glyphs drawn in the same hand as the product
// shell's navigation icons (1.8 stroke, round caps) so chat controls read as
// part of the same suite rather than a bolted-on widget.
var iconPaths = map[string]string{
	"compose":       "M4 20h4l10-10-4-4L4 16v4zM13 7l4 4",
	"plus":          "M12 5v14M5 12h14",
	"more":          "M5 12h.01M12 12h.01M19 12h.01",
	"more-vertical": "M12 5h.01M12 12h.01M12 19h.01",
	"link":          "M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1.5 1.5M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1.5-1.5",
	"copy":          "M8 4h10a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2zM4 16H3a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1h11",
	"close":         "M6 6l12 12M18 6L6 18",
	"search":        "M10.5 17a6.5 6.5 0 1 0 0-13 6.5 6.5 0 0 0 0 13zM20 20l-4.5-4.5",
	"browse":        "M4 6h16M4 12h16M4 18h10",
	"poll":          "M4 19V5M4 19h16M8 16v-4M12 16V8M16 16V5",
	"chevron-down":  "M6 9l6 6 6-6",
	"chevron-right": "M9 6l6 6-6 6",
	"arrow-up":      "M12 19V5M5 12l7-7 7 7",
	"arrow-down":    "M12 5v14M5 12l7 7 7-7",
	"people":        "M16 19v-1a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4v1M9.5 11a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7zM21 19v-1a4 4 0 0 0-3-3.87M15 4.13a3.5 3.5 0 0 1 0 6.74",
	"lock":          "M6 11h12v9H6zM9 11V7a3 3 0 0 1 6 0v4",
	"moon":          "M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z",
	"menu":          "M4 7h16M4 12h16M4 17h16",
	"info":          "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 11v6M12 7.5v.5",
	"chat":          "M4 5h16v11H9l-5 4V5z",
	"reply":         "M9 15l-5-5 5-5M4 10h9a7 7 0 0 1 7 7v3",
	"smile":         "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM8 14s1.5 2 4 2 4-2 4-2M9 9.5h.01M15 9.5h.01",
	"smile-filled":  "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM8 14s1.5 2 4 2 4-2 4-2M9 9.5h.01M15 9.5h.01M12 3a9 9 0 0 1 0 18",
	"pin":           "M9 4h6l-1 6 3 3v2H7v-2l3-3-1-6zM12 15v6",
	"pin-filled":    "M9 4h6l-1 6 3 3v2H7v-2l3-3-1-6zM12 15v6M10 5h4",
	"edit":          "M4 20h4l10-10-4-4L4 16v4zM13 7l4 4",
	"trash":         "M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v6M14 11v6",
	"attach":        "M20 12.5l-8 8a5 5 0 0 1-7-7l9-9a3.5 3.5 0 0 1 5 5l-9 9a2 2 0 0 1-3-3l8-8",
	"send":          "M20 12L4 4l4 8-4 8 16-8zM16 12H8",
	"refresh":       "M20 12a8 8 0 1 1-2.3-5.7M20 4v5h-5",
	"format-bold":   "M7 5h6a3.5 3.5 0 0 1 0 7H7zM7 12h7a3.5 3.5 0 0 1 0 7H7z",
	"format-italic": "M10 5h8M6 19h8M14 5l-4 14",
	"format-code":   "M9 8l-4 4 4 4M15 8l4 4-4 4",
	"format-list":   "M9 6h11M9 12h11M9 18h11M4.5 6h.01M4.5 12h.01M4.5 18h.01",
	"format-quote":  "M5 6v12M9 8h10M9 12h10M9 16h6",
	"checklist":     "M10 6h10M10 12h10M10 18h10M3.5 6l1.5 1.5L7.5 5M3.5 12l1.5 1.5 2.5-2.5M3.5 18l1.5 1.5 2.5-2.5",
	"arrow-left":    "M19 12H5M11 18l-6-6 6-6",
	"panel-left":    "M4 5h16v14H4zM9 5v14",
	"check":         "M5 12.5l4.5 4.5L19 7.5",
}

// iconSVGAttrs and iconPathAttrs are built once and only read: the renderer
// copies Raw maps, so sharing them saves two map allocations per icon, and a
// timeline draws several icons per message (16% of a render's allocations).
var iconSVGAttrs = map[string]any{
	"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8",
	"stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", "focusable": "false",
}

var iconPathAttrs = func() map[string]map[string]any {
	out := make(map[string]map[string]any, len(iconPaths))
	for name, d := range iconPaths {
		out[name] = map[string]any{"d": d}
	}
	return out
}()

func icon(name string) ui.Node {
	attrs, ok := iconPathAttrs[name]
	if !ok {
		attrs = iconPathAttrs["chat"]
	}
	return html.Tag("svg", html.Props{Class: "chat-icon icon-" + name, Raw: iconSVGAttrs}, html.Tag("path", html.Props{Raw: attrs}))
}
