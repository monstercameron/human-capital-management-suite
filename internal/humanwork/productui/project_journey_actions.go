package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

// projectJourneyCurrent stands for "the journey in the address bar": the
// journey page never prints its identifier, so the browser host resolves the
// address when an action runs.
const projectJourneyCurrent = "journey:current"

// ProjectJourneyActions is the journey page's header slot: the same Share
// menu tickets and boards use (copy link, send to chat, copy for Docs) and
// "Link to ticket…". It carries no identifiers and no forms; the browser
// host reads the journey from the address and builds its dialogs, whose
// words arrive here as data attributes.
func ProjectJourneyActions(localeCode, worker string) ui.Node {
	locale := ResolveProductLocale(localeCode)
	copy := projectBoardCopy(locale)
	title := locale.Text("projectui.workflow_promotion")
	if strings.TrimSpace(worker) != "" {
		title += ": " + strings.TrimSpace(worker)
	}
	text := func(key string) string { return locale.Text(key) }
	words := map[string]string{
		"title": title,
		// Send to chat.
		"send-title": text("projectui.send_title"), "conversation": text("projectui.conversation"), "channels": text("projectui.channels"),
		"direct": text("projectui.direct_messages"), "note": text("projectui.share_note"), "send": text("projectui.send"),
		"cancel": text("projectui.cancel"), "sent": locale.Text("projectui.sent_to", map[string]string{"name": "{name}"}), "share-failed": text("projectui.share_failed"),
		"open-conversation": text("projectui.open_conversation"), "no-conversations": text("projectui.no_conversations"),
		// Link to ticket.
		"linker-title": text("projectui.ticket_linker_title"), "project": text("projectui.board_project"), "search": text("projectui.ticket_linker_search"),
		"empty": text("projectui.ticket_linker_empty"), "loading": text("projectui.wf_loading"), "done": locale.Text("projectui.ticket_linker_done", map[string]string{"ticket": "{ticket}"}),
		"failed": text("projectui.wf_link_failed"), "close": text("projects.close_detail"), "open": text("projectui.wf_open"),
	}
	data := map[string]string{}
	for key, value := range words {
		data["text-"+key] = value
	}
	share := &projectui.Share{Href: projectJourneyCurrent, Title: title}
	link := html.Button(html.Props{Type: "button", Class: "projectui-share-trigger projectui-journey-link", Data: map[string]string{"projectui-action": "open-ticket-linker"}},
		html.Span(html.Props{Class: "projectui-flow-icon", Aria: map[string]string{"hidden": "true"}}),
		html.Span(html.Props{Text: text("projectui.link_to_ticket")}))
	return html.Div(html.Props{Class: "projectui-journey-actions", Data: data}, projectui.ShareMenu(copy, share), link)
}
