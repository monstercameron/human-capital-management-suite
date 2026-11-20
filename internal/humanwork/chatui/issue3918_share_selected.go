package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// #3918: the destination checked in the share dialog is the audience, whatever
// the filter shows. The dialog names it, announces it, and lets the person
// remove it before sharing.

func issue3918Text(m Model, key, name string) string {
	copy := map[string][3]string{
		"selected": {"Sharing to {name}", "Teilen mit {name}", "المشاركة مع {name}"},
		"remove":   {"Remove {name}", "{name} entfernen", "إزالة {name}"},
	}
	text := chatbug039Text(key, copy[key][chatbug039LocaleIndex(m.Locale)], copy[key][0])
	return strings.ReplaceAll(text, "{name}", name)
}

// issue3918SelectedDestination names the checked destination, or "" when it is
// not among the destinations the person may share to.
func issue3918SelectedDestination(m Model) string {
	if m.ShareDestinationID == "" {
		return ""
	}
	for _, c := range m.ShareDestinations {
		if c.ID == m.ShareDestinationID {
			return c.Name
		}
	}
	return ""
}

// issue3918SelectedNode is the summary of the checked destination, shown even
// while the filter hides its row. It is a polite status region so a screen
// reader hears the audience change.
func issue3918SelectedNode(m Model) ui.Node {
	name := issue3918SelectedDestination(m)
	if name == "" {
		return html.Div(html.Props{Class: "share-selected-slot", Role: "status"})
	}
	return html.Div(html.Props{Class: "share-selected-slot", Role: "status"},
		html.Div(html.Props{Class: "share-selected"},
			html.Span(html.Props{Dir: "auto", Text: issue3918Text(m, "selected", name)}),
			html.Button(html.Props{Class: "share-selected-remove", Type: "button", Disabled: m.SharePending, Data: map[string]string{"action": "share-clear"}, Aria: map[string]string{"label": issue3918Text(m, "remove", name)}, Text: "×"})))
}

const issue3918Styles = `.share-selected{display:flex;align-items:center;gap:8px;padding:6px 6px 6px 12px;border:1px solid var(--accent);border-radius:var(--hcm-radius-control);background:var(--soft);overflow-wrap:anywhere}` +
	`.share-selected-remove{flex:none;min-inline-size:32px;min-block-size:32px;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--ink);font:inherit;cursor:pointer}.share-selected-remove:hover,.share-selected-remove:focus-visible{background:var(--surface)}` +
	`@media(pointer:coarse),(max-width:767px){.share-selected-remove{min-inline-size:44px;min-block-size:44px}}`
