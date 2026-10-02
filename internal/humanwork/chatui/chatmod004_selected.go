package chatui

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATMOD-004: removing several selected messages. The server has accepted a
// list of messages since the command was written; the page had no way to make
// one. The Remove dialog now lists the messages around the one it was opened
// on, each with a tick box, as a third form beside "this message" and "this
// person's messages between two times". It works like the second: the count is
// shown first and nothing is removed until it is confirmed.

// ModerationChoice is one message a moderator may tick in the Remove dialog:
// its author and text as the server projected them for the person acting.
type ModerationChoice struct {
	ID, AuthorName, Body string
	At                   time.Time
}

// chatmodSelectedLimit is how many letters of a message the list shows. The
// list is for telling messages apart, not for reading them.
const chatmodSelectedLimit = 140

// chatmodExcerpt is a message as one short line of plain text.
func chatmodExcerpt(body string) string {
	text := strings.Join(strings.Fields(chatExcerptText(Chatcmd002PlainBody(body))), " ")
	if utf8.RuneCountInString(text) <= chatmodSelectedLimit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:chatmodSelectedLimit])) + "…"
}

// chatmodSelected is the form that removes the ticked messages. The message
// the dialog was opened on starts ticked. It is not drawn when the server sent
// no other message to choose: one message is what the first form removes.
func chatmodSelected(m ModerationDialogModel) ui.Node {
	if len(m.Choices) < 2 {
		return nil
	}
	locale := m.Model.Locale
	t := func(key string) string { return chatremoveText(locale, key) }
	target := ""
	if len(m.Selection.PostIDs) == 1 {
		target = m.Selection.PostIDs[0]
	}
	now := time.Now()
	rows := []ui.Node{html.Legend(html.Props{Text: t("selected")})}
	for _, choice := range m.Choices {
		id := "chatremove-pick-" + choice.ID
		who := chatmodName(locale, choice.AuthorName)
		if when := moderationWhen(locale, choice.At, m.TimeZone, now); when != "" {
			who += " · " + when
		}
		rows = append(rows, html.Div(html.Props{Class: "chatremove-choice chatremove-pick"},
			html.Input(html.Props{ID: id, Type: "checkbox", Name: "post", Value: choice.ID, Checked: choice.ID == target}),
			html.Label(html.Props{For: id, Dir: "auto"},
				html.Span(html.Props{Class: "chatremove-pick-who", Text: who}),
				html.Span(html.Props{Class: "chatremove-pick-text", Text: chatmodExcerpt(choice.Body)}))))
	}
	fields := []ui.Node{
		html.P(html.Props{Text: t("choose_help")}),
		html.Fieldset(html.Props{Class: "chatremove-picks"}, rows...),
		chatmodReasons(locale, "chatremove-pick-reason", t("why_removed"), m.ReasonCode),
		html.Button(html.Props{Type: "submit", Class: "primary", Text: t("preview")}),
	}
	data := map[string]string{"chatremove": "preview", "chatremove-selection": "picked", "conversation-id": m.Selection.ConversationID, "removal-action": "remove"}
	return html.Details(html.Props{Class: "chatremove-several chatremove-selected"}, html.Summary(html.Props{Text: t("choose")}), html.Form(html.Props{Data: data}, fields...))
}

// chatmodSelectedStyles lays a tick box beside two short lines: who and when,
// then the start of the message.
const chatmodSelectedStyles = `
.chatremove .chatremove-picks{border:0;padding:0;margin:0;gap:0;max-block-size:40vh;overflow:auto}.chatremove-picks legend{font-weight:650;padding:0;margin-block-end:.25rem}
.chatremove .chatremove-pick{align-items:flex-start;padding-block:.25rem;border-block-end:1px solid var(--hcm-color-border)}.chatremove .chatremove-pick input[type=checkbox]{inline-size:24px;block-size:24px;min-block-size:24px;margin-block-start:.25rem;padding:0;accent-color:var(--hcm-color-brand-primary)}
.chatremove-pick label{flex:1;display:grid;gap:2px;min-block-size:44px;align-content:center;cursor:pointer}.chatremove-pick-who{font-size:.8125rem;color:var(--hcm-color-text-muted)}.chatremove-pick-text{overflow-wrap:anywhere}
`
