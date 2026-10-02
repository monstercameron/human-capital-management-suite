package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-044: the mention menu is a scrolling list above a fixed hint line, and
// every agent in it is one grid.
//
// The menu used to scroll as a whole with the hint line sticky at its bottom, so
// the hint covered the last row. The rows now scroll inside .mention-list and the
// hint is a footer that takes its own height under the list: the list ends where
// the hint begins, so no row is ever behind it.
//
// An agent row is a grid with three columns beside the information button: the
// agent's icon; its name and handle with the description under them (two lines at
// most); and the Agent badge. The information button is the row's last column,
// centred on the whole row, and the active highlight covers the button too.
const ChatBug044Styles = `.mention-menu.mention-menu-split{overflow:hidden;padding:6px 6px 0}` +
	`.mention-menu-split>.mention-list{display:flex;flex-direction:column;flex:1 1 auto;min-block-size:0;overflow-y:auto;overscroll-behavior:contain;padding-block-end:4px}` +
	`.mention-menu.mention-menu-split>.mention-hint{position:static;flex:none;margin:0 -6px;padding:6px 14px 8px;border-top:1px solid var(--line);background:var(--surface);border-radius:0 0 var(--hcm-radius-surface) var(--hcm-radius-surface)}` +
	`.mention-menu .mention-agent-option{display:grid;grid-template-columns:minmax(0,1fr) 44px;align-items:center;min-inline-size:0;border-radius:var(--hcm-radius-control)}` +
	`.mention-menu .mention-agent-option>.mention-option.persona{display:grid;grid-template-columns:auto minmax(0,1fr) auto;grid-template-areas:"icon identity badge" "icon purpose purpose" "icon attribution attribution";column-gap:10px;row-gap:2px;align-items:center;align-content:center;inline-size:100%;min-inline-size:0;padding:6px 8px}` +
	`.mention-menu .mention-agent-option>.mention-option.persona>:first-child{grid-area:icon;align-self:center}` +
	`.mention-menu .mention-agent-option>.mention-option.persona>.mention-agent-identity{grid-area:identity;flex-direction:row;align-items:baseline;flex-wrap:wrap;column-gap:6px;row-gap:0;min-inline-size:0}` +
	`.mention-menu .mention-agent-option>.mention-option.persona .mention-name{flex:0 1 auto;max-inline-size:100%}` +
	`.mention-menu .mention-agent-option>.mention-option.persona .mention-handle{flex:0 1 auto;min-inline-size:0;max-inline-size:100%}` +
	`.mention-menu .mention-agent-option>.mention-option.persona>.agent-badge{grid-area:badge;justify-self:end;white-space:nowrap}` +
	`.mention-menu .mention-agent-option>.mention-option.persona>.mention-attribution{grid-area:attribution}` +
	`.mention-menu .mention-agent-option>.mention-option.persona>.mention-purpose{grid-area:purpose;flex-basis:auto;min-inline-size:0}` +
	`.mention-menu .mention-agent-option>.mention-option.persona>.mention-purpose:empty{display:none}` +
	`.mention-menu .mention-agent-option>.mention-agent-info{align-self:center;justify-self:center;inline-size:44px;block-size:44px;min-inline-size:44px;min-block-size:44px;flex:none}` +
	`.mention-menu .mention-agent-option:hover{background:color-mix(in srgb,var(--ink) 6%,transparent)}` +
	`.mention-menu .mention-agent-option:has(>.mention-option.active){background:color-mix(in srgb,var(--accent) 16%,transparent);box-shadow:inset 3px 0 0 var(--accent)}` +
	`.mention-menu[dir="rtl"] .mention-agent-option:has(>.mention-option.active){box-shadow:inset -3px 0 0 var(--accent)}` +
	`.mention-menu .mention-agent-option>.mention-option.persona,.mention-menu .mention-agent-option>.mention-option.persona.active,.mention-menu .mention-agent-option>.mention-option.persona:hover{background:none;box-shadow:none}`

// chatbug044Menu is the people-and-agents menu: the rows scroll in their own
// list, the keyboard hint is a footer under it. The listbox role stays on the
// menu, whose options are the rows' descendants.
func chatbug044Menu(id, direction, label string, rows []ui.Node, hint ui.Node) ui.Node {
	return html.Div(html.Props{ID: id, Class: "mention-menu mention-menu-split", Role: "listbox", Dir: direction, Aria: map[string]string{"label": label}},
		html.Div(html.Props{Class: "mention-list"}, rows...), hint)
}
