package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AGENTUX-070, with CHATUX-028: sharing a private answer to the channel is not a
// button of the card's action row. The row keeps Helpful, Not right and Ask a
// follow-up. "Share to channel" is an item of the card's "…" menu, and the
// "Only visible to you" mark is itself the button that opens the same
// confirmation (CHATUX-026), so the one thing that is public afterwards is asked
// about before it happens, from the place that says it is private.

// agentux070CanShare reports whether the card offers to share its answer: it is
// the asker's private answer in a public channel, from an agent that is not set
// to answer privately, and nothing is happening to it or has been decided about
// it. A refusal or a share already made leaves nothing to offer.
func agentux070CanShare(model Model, local localUI, reason, invocationID string) bool {
	if invocationID == "" || reason == "agent" || reason == "channel" || model.Callbacks.ShareAgentAnswer == nil || model.selected().Kind != PublicChannel {
		return false
	}
	// While the card asks whether to share, the question is the control.
	if chatux026Asking(model, local, invocationID) {
		return false
	}
	switch model.AgentShare[invocationID].Status {
	case AgentShareShared, AgentShareRemoving, AgentShareRefused, AgentShareSharing:
		return false
	}
	return true
}

// agentux070ShareMenuItem is the card's "Share to channel" item, or nothing.
func agentux070ShareMenuItem(model Model, local localUI, reason, invocationID string) []ui.Node {
	if !agentux070CanShare(model, local, reason, invocationID) {
		return nil
	}
	label := chatux003Text(model, "chatux003.share")
	return []ui.Node{html.Button(html.Props{Class: "menu-item agent-share-menu", Type: "button", Role: "menuitem", Data: map[string]string{"action": "agent-share", "id": invocationID},
		Title: chatux003Format(model, "chatux003.share.hint", map[string]string{"channel": chatux003ChannelName(model)})},
		icon("send"), html.Span(html.Props{Text: label}))}
}

// agentux070Mark is the muted "who can see this" mark at the end of the card's
// header: plain text, or, while the answer may be shared, the button that asks
// whether to share it.
func agentux070Mark(model Model, note, seen, shareID string) ui.Node {
	label := html.Span(html.Props{Class: "agent-reply-private-label", Text: note})
	if shareID == "" {
		return html.Span(html.Props{Class: "agent-reply-private", Data: map[string]string{"visibility": seen}}, icon("eye"), label)
	}
	hint := chatux003Format(model, "chatux003.share.hint", map[string]string{"channel": chatux003ChannelName(model)})
	return html.Button(html.Props{Class: "agent-reply-private agent-reply-private-button", Type: "button", Data: map[string]string{"visibility": seen, "action": "agent-share", "id": shareID},
		Aria: map[string]string{"label": note + ". " + chatux003Text(model, "chatux003.share") + ". " + hint}, Title: hint}, icon("eye"), label)
}

// chatUX070MarkStyles lets the mark keep its quiet look when it is a button.
const chatUX070MarkStyles = `.agent-reply-head .agent-reply-private-button{border:0;background:none;padding:2px 4px;border-radius:var(--hcm-radius-control);font:inherit;font-size:.8125rem;cursor:pointer}` +
	`.agent-reply-head .agent-reply-private-button:hover,.agent-reply-head .agent-reply-private-button:focus-visible{color:var(--accent);text-decoration:underline}`
