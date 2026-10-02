package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-040: a message that asked an agent gets an answer card under it. The
// card arrives with the agent activity, which is read beside the page, not in
// it; until it does, the message keeps room for the card, so nothing below it
// moves when the answer lands. The room is a placeholder of the card's own
// minimum height, drawn only for a message the viewer wrote that names an agent,
// in a conversation that is not a direct one with an agent (there the answer is
// an ordinary message and comes with the page).

// ChatBug040CardMinHeight is the height a reserved card and every
// agent answer card keep at least, so the placeholder and the card it becomes
// are the same size for an answer of ordinary length.
const ChatBug040CardMinHeight = "208px"

// chatbug040AskedAnAgent reports whether msg is a message the viewer wrote that
// names an agent.
func chatbug040AskedAnAgent(model Model, msg Message) bool {
	if msg.ID == "" || model.CurrentUser == "" || msg.AuthorID != model.CurrentUser {
		return false
	}
	for _, ref := range msg.PersonaReferences {
		if ref.Kind == "AGENT_MENTION" {
			return true
		}
	}
	return false
}

// chatbug040MessageByID finds the message the agent state is placed under.
func chatbug040MessageByID(model Model, postID string) (Message, bool) {
	for _, msg := range model.Messages {
		if msg.ID == postID {
			return msg, true
		}
	}
	for _, msg := range model.ThreadMessages {
		if msg.ID == postID {
			return msg, true
		}
	}
	if model.ThreadParent != nil && model.ThreadParent.ID == postID {
		return *model.ThreadParent, true
	}
	return Message{}, false
}

// chatbug040ReservedRow is the placeholder for the card under postID, or nil
// when the message has none coming or the activity has already answered.
func chatbug040ReservedRow(model Model, postID string) ui.Node {
	if model.PersonaActivityReady || model.selected().Agent {
		return nil
	}
	msg, ok := chatbug040MessageByID(model, postID)
	if !ok || !chatbug040AskedAnAgent(model, msg) {
		return nil
	}
	line := func(class string) ui.Node { return html.Span(html.Props{Class: "chat-skeleton " + class}) }
	return html.WithKey(html.Div(html.Props{Class: "agent-reply-row chatbug040-reserve", Dir: agentReplyDirection(model.Locale), Aria: map[string]string{"hidden": "true"}, Data: map[string]string{"agent-reply-state": "reserved", "reserved-for": postID}},
		html.Div(html.Props{Class: "chatbug040-reserve-head"}, line("chatbug040-reserve-avatar"), line("chatbug040-reserve-name")),
		html.Div(html.Props{Class: "chatbug040-reserve-body"}, line("chatbug040-reserve-line"), line("chatbug040-reserve-line"), line("chatbug040-reserve-line short"))),
		"agent-reserve:"+postID)
}

// ChatBug040Styles sizes the placeholder and gives every answer card the same
// minimum height.
const ChatBug040Styles = `.chatbug040-reserve,.chat-ephemeral.agent-reply-row{min-block-size:` + ChatBug040CardMinHeight + `;box-sizing:border-box}` +
	`.chatbug040-reserve{display:grid;align-content:start;gap:14px;padding:12px 14px;margin-block:6px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--surface-muted)}` +
	`.chatbug040-reserve-head{display:flex;align-items:center;gap:10px}` +
	`.chatbug040-reserve-avatar{flex:none;inline-size:26px;block-size:26px;border-radius:50%}` +
	`.chatbug040-reserve-name{inline-size:min(40%,160px);block-size:10px;border-radius:999px}` +
	`.chatbug040-reserve-body{display:grid;gap:9px}` +
	`.chatbug040-reserve-line{block-size:10px;border-radius:999px;inline-size:min(92%,560px)}` +
	`.chatbug040-reserve-line.short{inline-size:min(58%,340px)}`
