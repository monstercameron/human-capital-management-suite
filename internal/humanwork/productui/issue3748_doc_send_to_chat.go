package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsSendToChatItem is the document menu's "Send to chat…" item (issue 3748).
// It is the same control tickets use: the browser host opens the shared
// send-to-chat dialog from the item's address, title and the dialog words
// carried here as data attributes, posts the canonical document link through
// the normal chat post call (so membership and policy still apply), and shows
// the same "Sent to …" toast with an Open conversation link. It is only drawn
// for a document this page could read, when the host says chat is available.
func docsSendToChatItem(view View, summary DocumentSummary) ui.Node {
	if !view.CanSendDocumentToChat || summary.ID == "" {
		return nil
	}
	locale := ResolveProductLocale(view.Locale.Resolved)
	text := func(key string) string { return locale.Text(key) }
	words := map[string]string{
		"send-title": text("projectui.send_title"), "conversation": text("projectui.conversation"), "channels": text("projectui.channels"),
		"direct": text("projectui.direct_messages"), "note": text("projectui.share_note"), "send": text("projectui.send"),
		"cancel": text("projectui.cancel"), "sent": locale.Text("projectui.sent_to", map[string]string{"name": "{name}"}), "share-failed": text("projectui.share_failed"),
		"open-conversation": text("projectui.open_conversation"), "no-conversations": text("projectui.no_conversations"), "close": text("projects.close_detail"),
	}
	data := map[string]string{"projectui-action": "share-chat", "href": docsDocumentHref(summary.ID), "title": summary.Title}
	for key, value := range words {
		data["text-"+key] = value
	}
	return html.Button(html.Props{Class: "docs-menu-item", Type: "button", Data: data}, productIcon("chat", "docs-menu-icon"), html.Span(html.Props{}, ui.Text(text("projectui.send_to_chat"))))
}
