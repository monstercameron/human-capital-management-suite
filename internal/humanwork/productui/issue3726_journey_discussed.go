package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ProjectJourneyDiscussed is the journey page's "Discussed in" slot (issue
// 3726). It is rendered empty and hidden: the browser host asks the server for
// the conversations this journey was shared into that the viewer may open,
// fills the list and reveals the slot. A viewer who may open none sees nothing
// at all (no heading, no count), so a restricted conversation is never hinted
// at. The slot carries no identifiers; the host reads the journey from the
// address. Its words arrive as data attributes.
func ProjectJourneyDiscussed(localeCode string) ui.Node {
	locale := ResolveProductLocale(localeCode)
	text := func(key string) string { return locale.Text(key) }
	data := map[string]string{
		"projectui-discussed": projectJourneyCurrent,
		"text-direct":         text("projectui.discussed_direct"),
		"text-open":           text("projectui.discussed_open"),
		"text-incomplete":     text("projectui.discussed_incomplete"),
		"text-heading":        text("projectui.discussed_in"),
	}
	return html.Section(html.Props{Class: "projectui-journey-discussed", Hidden: true, Dir: string(locale.Direction), Data: data, Aria: map[string]string{"live": "polite"}})
}
