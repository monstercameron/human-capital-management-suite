package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ThreadChannelCopyFailedNotice is the line shown when a thread reply posted but
// its copy to the conversation did not (#3997). It says which half succeeded
// and how to retry the other without posting the reply twice.
func ThreadChannelCopyFailedNotice(locale string) string {
	const en = "Your reply was posted, but it was not also sent to the conversation. Tick \"Also send\" and send again to retry; the reply will not be posted twice."
	const de = "Ihre Antwort wurde gesendet, aber nicht zusätzlich an die Unterhaltung. Wählen Sie „Auch senden“ und senden Sie erneut; die Antwort wird nicht doppelt gesendet."
	const ar = "تم إرسال ردك، لكنه لم يُرسل أيضًا إلى المحادثة. فعّل «إرسال أيضًا» ثم أرسل مرة أخرى لإعادة المحاولة؛ لن يُرسل الرد مرتين."
	return [3]string{en, de, ar}[chatbug039LocaleIndex(locale)]
}

// dispatchThreadReply hands a thread reply to the host. The conversation is the
// one the reader was in when they pressed Send, so a switch to another room
// while the request starts cannot re-address the reply or its channel copy
// (#3997). It reports whether anything was dispatched.
//
// A reply that asks an agent is never copied to the conversation: the copy would
// ask it again and a second run is a second charge.
func dispatchThreadReply(m Model, body string, refs []ChatReference, alsoChannel bool) bool {
	conversationID, parentID := m.SelectedID, m.ThreadParentID
	if len(refs) > 0 {
		if m.Callbacks.ReplyInThreadWithReferences == nil {
			return false
		}
		m.Callbacks.ReplyInThreadWithReferences(parentID, body, refs)
		return true
	}
	if m.Callbacks.ReplyInThreadIn != nil {
		m.Callbacks.ReplyInThreadIn(conversationID, parentID, body, alsoChannel)
		return true
	}
	if m.Callbacks.ReplyInThread == nil {
		return false
	}
	m.Callbacks.ReplyInThread(parentID, body)
	if alsoChannel && m.Callbacks.SendMessage != nil {
		m.Callbacks.SendMessage(conversationID, body)
	}
	return true
}

// issue3997AgentNote says, beside "Also send to ...", that a reply asking an
// agent is not copied to the conversation, so the choice never suggests two
// posts that will not happen.
func issue3997AgentNote(locale string) string {
	const en = "Not sent to the conversation: this reply asks an agent."
	const de = "Wird nicht an die Unterhaltung gesendet: Diese Antwort richtet eine Anfrage an einen Agenten."
	const ar = "لن يُرسل إلى المحادثة: هذا الرد يطلب من وكيل."
	return [3]string{en, de, ar}[chatbug039LocaleIndex(locale)]
}

// issue3997AlsoChannel is the "Also send to ..." choice. When the draft asks an
// agent the choice is off and says why, because dispatchThreadReply will not
// copy such a reply.
func issue3997AlsoChannel(m Model, h handlers, disabled bool) ui.Node {
	if len(h.threadRefs) == 0 {
		return chatux014AlsoChannel(m, h, disabled)
	}
	c := m.selected()
	name := displayName(m, c)
	if name == "" || m.Callbacks.SendMessage == nil {
		return nil
	}
	if c.Kind == PublicChannel || c.Kind == PrivateChannel {
		name = "#" + name
	}
	return html.Label(html.Props{Class: "thread-also thread-also-agent", For: threadAlsoChannelID},
		html.Input(html.Props{ID: threadAlsoChannelID, Type: "checkbox", Name: "also", Disabled: true}),
		html.Span(html.Props{Dir: "auto", Text: strings.ReplaceAll(chatux014Text(m.Locale, "also"), "{name}", name) + ". " + issue3997AgentNote(m.Locale)}))
}
