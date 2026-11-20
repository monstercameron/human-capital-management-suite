package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-014. The thread composer has the tools of the conversation composer,
// in the same order and with the same controls: Mention (@), the emoji and GIF
// pickers, the Formatting toggle (Aa) with its row of formatting buttons (bold,
// italic, code, link, list, quote), the writing styles, the same Enter hint
// and Send. It differs in its placeholder, in "Also send to #channel", which a
// reply has and a message does not, and in having no Add menu: what the menu
// adds (a poll, a to-do list, a location, a voice message, a file) belongs to
// the conversation, not to a reply.

// threadAlsoChannelID is the checkbox that sends the reply to the conversation
// as well.
const threadAlsoChannelID = "thread-also-channel"

func chatux014Text(locale, key string) string {
	en := map[string]string{"also": "Also send to {name}"}
	de := map[string]string{"also": "Auch an {name} senden"}
	ar := map[string]string{"also": "إرسال أيضًا إلى {name}"}
	table := en
	switch {
	case strings.HasPrefix(locale, "de"):
		table = de
	case strings.HasPrefix(locale, "ar"):
		table = ar
	}
	return chatbug039Text(key, table[key], en[key])
}

// chatux014AlsoChannel is the "Also send to #general" choice above the tools.
func chatux014AlsoChannel(m Model, h handlers, disabled bool) ui.Node {
	c := m.selected()
	name := displayName(m, c)
	if name == "" || m.Callbacks.SendMessage == nil {
		return nil
	}
	if c.Kind == PublicChannel || c.Kind == PrivateChannel {
		name = "#" + name
	}
	return html.Label(html.Props{Class: "thread-also", For: threadAlsoChannelID},
		html.Input(html.Props{ID: threadAlsoChannelID, Type: "checkbox", Name: "also", Disabled: disabled, Checked: threadAlsoChecked(m, h.local), OnChange: h.threadAlso}),
		html.Span(html.Props{Dir: "auto", Text: strings.ReplaceAll(chatux014Text(m.Locale, "also"), "{name}", name)}))
}

// chatux014ThreadTools is what sits under a thread's text: the choice to also
// send to the conversation, the formatting row, and the tool row.
func chatux014ThreadTools(m Model, h handlers, disabled bool) []ui.Node {
	id := "thread-composer"
	var hint ui.Node
	if composerHintVisible(m, h.local) {
		hint = html.Span(html.Props{ID: "thread-composer-help", Class: "composer-help kbd-hint"}, chatux043Hint(m.Locale, m.t(KeyComposeHint))...)
	}
	// CHATUX-032: the paperclip and the "Also send" choice are part of the one
	// tool row, not rows of their own.
	// Issue 3872: Reply is as available as channel Send is: a nonempty draft
	// keeps it enabled, previewed or not.
	canSend := chatPolishSendReady(!disabled, threadDraftNow(m, h.local))
	return []ui.Node{
		composerFormatRowFor(m, h.local, id, disabled),
		html.Div(html.Props{Class: "composer-toolbar thread-toolbar"},
			html.Div(html.Props{Class: "composer-tools"},
				chatux032ThreadAttach(m, disabled),
				composerMentionButton(m, id, disabled),
				chatEmojiComposerPicker(m, h.local, id, disabled),
				giphyPickerControl(m, id, disabled),
				composerFormatToggle(m, id, disabled, h.local),
				composerPreviewToggle(m, id, threadDraftNow(m, h.local), disabled, h.local),
				chattoneToolbar(m, id, disabled),
			),
			hint,
			html.Button(html.Props{Class: "send-button", Type: "submit", Disabled: !canSend, Aria: map[string]string{"label": m.t(KeyReplySend), "disabled": boolString(!canSend)}, Title: m.t(KeyReplySend)}, icon("send"), html.Span(html.Props{Class: "send-label", Text: m.t(KeyReplySend)})),
			issue3997AlsoChannel(m, h, disabled),
		),
	}
}
