package chatui

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-040: a message that asked an agent gets an answer card under it. The
// card arrives with the agent activity, which is read beside the page, not in
// it; until it does, the message keeps room for the card, so nothing below it
// moves when the answer lands. The room is a placeholder drawn only for a
// message the viewer wrote that names an agent, in a conversation that is not a
// direct one with an agent (there the answer is an ordinary message and comes
// with the page).
//
// CHATBUG-061: the room is the height of the answer it stands for. A card that
// was drawn before left its height with the page (Model.AgentCardHeights); a
// card never seen keeps the height of a two-line answer. The answered card
// itself has no minimum height, so it never ends in an empty band.

// ChatBug061TwoLineHeight is the height of an answer card that holds a
// two-line answer, one row of sources and the action row.
const ChatBug061TwoLineHeight = 172

const (
	chatbug061MinHeight = 96
	chatbug061MaxHeight = 1200
	// chatbug061Chrome is the card's own padding and border, top and bottom.
	chatbug061Chrome = 22
)

// chatbug061ReserveHeight is the room, in pixels, kept under postID.
func chatbug061ReserveHeight(model Model, postID string) int {
	height, known := model.AgentCardHeights[postID]
	if !known || height < chatbug061MinHeight {
		return ChatBug061TwoLineHeight
	}
	return min(height, chatbug061MaxHeight)
}

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
	return html.WithKey(chatbug040Placeholder(model, postID, "reserved"), "agent-reserve:"+postID)
}

// chatbug040Placeholder is the quiet outline of an answer card: a header line,
// two lines of text and a row of buttons, with nothing that reads as a run at
// work. state names why it is there, for tests and for the stylesheet.
//
// The page's content policy allows no inline style, so the room is held by an
// empty drawing of that height beside the outline: the placeholder is as tall
// as the taller of the two.
func chatbug040Placeholder(model Model, postID, state string) ui.Node {
	line := func(class string) ui.Node { return html.Span(html.Props{Class: "chat-skeleton " + class}) }
	height := chatbug061ReserveHeight(model, postID)
	return html.Div(html.Props{Class: "agent-reply-row chatbug040-reserve", Dir: agentReplyDirection(model.Locale), Aria: map[string]string{"hidden": "true"}, Data: map[string]string{"agent-reply-state": state, "reserved-for": postID, "reserved-height": strconv.Itoa(height)}},
		html.Tag("svg", html.Props{Class: "chatbug040-reserve-room", Raw: map[string]any{"width": "0", "height": strconv.Itoa(max(0, height-chatbug061Chrome)), "focusable": "false"}}),
		html.Div(html.Props{Class: "chatbug040-reserve-outline"},
			html.Div(html.Props{Class: "chatbug040-reserve-head"}, line("chatbug040-reserve-avatar"), line("chatbug040-reserve-name")),
			html.Div(html.Props{Class: "chatbug040-reserve-body"}, line("chatbug040-reserve-line"), line("chatbug040-reserve-line short")),
			html.Div(html.Props{Class: "chatbug040-reserve-foot"}, line("chatbug040-reserve-pill"), line("chatbug040-reserve-pill"), line("chatbug040-reserve-pill"))))
}

// ChatBug040Styles draws the placeholder in the answer card's own frame (the
// same border, start rule, padding and margins), so the card that replaces it
// sits in the same box. Its height comes from the element, per answer.
const ChatBug040Styles = `.chatbug040-reserve,.chat-ephemeral.agent-reply-row{box-sizing:border-box}` +
	`.chatbug040-reserve{display:flex;align-items:flex-start;width:100%;max-width:var(--chat-measure);margin:8px 0;padding:10px 12px;border:1px solid var(--hcm-color-border);border-inline-start:3px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface)}` +
	`.chatbug040-reserve-room{flex:none}` +
	`.chatbug040-reserve-outline{flex:1 1 auto;min-width:0;display:grid;align-content:start;gap:14px}` +
	`.chatbug040-reserve-head{display:flex;align-items:center;gap:8px}` +
	`.chatbug040-reserve-avatar{flex:none;inline-size:26px;block-size:26px;border-radius:50%}` +
	`.chatbug040-reserve-name{inline-size:min(40%,160px);block-size:10px;border-radius:999px}` +
	`.chatbug040-reserve-body{display:grid;gap:12px}` +
	`.chatbug040-reserve-line{block-size:10px;border-radius:999px;inline-size:min(92%,560px)}` +
	`.chatbug040-reserve-line.short{inline-size:min(58%,340px)}` +
	`.chatbug040-reserve-foot{display:flex;gap:8px}` +
	`.chatbug040-reserve-pill{inline-size:84px;block-size:24px;border-radius:999px}`
