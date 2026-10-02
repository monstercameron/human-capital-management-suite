package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const legacyPrivateAnswerReceiptBody = "The persona reply was sent privately to you."

func legacyPrivateAnswerReceipt(model Model, message Message) (ui.Node, bool) {
	if strings.TrimSpace(message.Body) != legacyPrivateAnswerReceiptBody {
		return nil, false
	}
	text := personaProgressText(model, "chat.agent.legacy_private", "The answer was sent privately.")
	if selected := ReaderMessageBody(model, message); selected != message.Body {
		text = selected
	}
	children := []ui.Node{icon("eye"), html.Span(html.Props{Text: text})}
	if message.TimeLabel != "" {
		children = append(children, html.Time(html.Props{Text: message.TimeLabel}))
	}
	return html.Div(html.Props{Class: "chat-system-line agent-legacy-private", Role: "note", Data: map[string]string{"message-id": message.ID}}, children...), true
}
