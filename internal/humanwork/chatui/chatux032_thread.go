package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-032, the thread's reply composer. It is the channel composer in compact
// form: one field, then one row of the same tool buttons with the quiet "Also
// send to #channel" choice and Reply at the right. The formatting row is closed
// when a thread opens, whatever the person chose for the channel composer: a
// reply is a line or two, and the remembered choice is for the long messages.

// threadComposerID is the reply field and the target of its tool buttons.
const threadComposerID = "thread-composer"

// chatux032ThreadFormatState is how the thread's formatting row is set: open
// only when the person opened it in this page session.
func chatux032ThreadFormatState(local localUI) string {
	if local.threadFormatRow == composerFormatShown {
		return composerFormatShown
	}
	return composerFormatHidden
}

// chatux032ThreadFormatChoose flips the thread's formatting row. The choice is
// the session's own and is not kept for the viewer, so the next thread opens
// closed again.
func chatux032ThreadFormatChoose(local localStore) {
	next := composerFormatShown
	if chatux032ThreadFormatState(local.get()) == composerFormatShown {
		next = composerFormatHidden
	}
	local.update(func(u *localUI) { u.threadFormatRow = next })
}

// chatux032ThreadAttach is the paperclip, one of the tools in the row. It is the
// same control the files choice has always had, so the file picker and its
// drafts keep working as they did.
func chatux032ThreadAttach(m Model, disabled bool) ui.Node {
	s := m.Chatattach001.thread()
	if s == nil || s.Choose == nil {
		return nil
	}
	return html.Button(html.Props{Class: "tool-button chatattach001-thread-attach", Type: "button", Disabled: disabled || s.Sending || len(s.Files) >= 10, Title: Chatattach001Text(m.Locale, "note"),
		Data: map[string]string{"chatattach001-choose": "thread"}, Aria: map[string]string{"label": Chatattach001Text(m.Locale, "attach")}}, icon("attach"))
}

// chatux032ThreadDrafts is the list of files waiting to go with the reply. It
// takes no room until a file is chosen.
func chatux032ThreadDrafts(m Model) ui.Node {
	s := m.Chatattach001.thread()
	if s == nil || s.Choose == nil {
		return nil
	}
	return html.Div(html.Props{Class: "chatattach001-thread"}, chatattach001DraftsOf(m, s, Chatattach001ThreadPickerID, "chatattach001-drafts chatattach001-thread-drafts"))
}
