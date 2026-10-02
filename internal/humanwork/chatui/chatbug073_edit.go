package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-073. Editing a message used to open a two-row box that did not hold
// the caret, where Enter added a line although Enter sends in the composer, and
// the word "edited" was printed above a grouped message's text, where it read
// as the previous message's. The box now takes the caret at the end of the
// text and grows with it (edit_focus_js.go), Enter saves and Shift+Enter adds a
// line with the keys named under the box, "edited" follows the text, and
// ArrowUp in an empty composer opens the person's latest message.

// ChatBug073Styles is joined into the workspace stylesheet in styles.go. The
// box sizes to its text where the browser can do that itself; edit_focus_js.go
// sets the height where it cannot. Half the window is the cap in both.
const ChatBug073Styles = `
.chat-workspace .edit-input{display:block;box-sizing:border-box;resize:none;overflow-y:auto;field-sizing:content;min-block-size:calc(1.45em + 18px);max-block-size:50vh;max-block-size:50dvh}
.chat-workspace .message-edit .edit-hint{font-size:.75rem;color:var(--hcm-color-text-muted);margin-inline-start:auto;align-self:center}
.chat-workspace .message-edit .edit-actions{flex-wrap:wrap;align-items:center}
.chat-workspace .message-edited{display:block;font-size:.75rem;font-weight:500;line-height:1.3;color:var(--hcm-color-text-muted)}
`

const (
	keyChatbug073Hint   = "chat.bug073.edit_hint"
	keyChatbug073Edited = "chat.bug073.edited"
)

var chatbug073Copy = map[string]map[string]string{
	"en-US": {
		keyChatbug073Hint:   "Enter to save, Shift+Enter for a new line, Esc to cancel",
		keyChatbug073Edited: "(edited)",
	},
	"de-DE": {
		keyChatbug073Hint:   "Enter speichert, Umschalt+Enter fügt eine Zeile ein, Esc bricht ab",
		keyChatbug073Edited: "(bearbeitet)",
	},
	"ar": {
		keyChatbug073Hint:   "Enter للحفظ، Shift+Enter لسطر جديد، Esc للإلغاء",
		keyChatbug073Edited: "(معدّلة)",
	},
}

// chatbug073Text answers one of the strings above for the model's locale.
func chatbug073Text(m Model, key string) string {
	return chatbug039Text(key, chatbug073Copy[chatEmojiLocale(m.Locale)][key], chatbug073Copy["en-US"][key])
}

// chatbug073HintID is the id the edit box names as its description.
func chatbug073HintID(postID string) string { return "edit-hint-" + postID }

// chatbug073EditHint is the key hint under the edit box, in the composer hint's
// type.
func chatbug073EditHint(m Model, postID string) ui.Node {
	return html.Span(html.Props{ID: chatbug073HintID(postID), Class: "edit-hint kbd-hint", Text: chatbug073Text(m, keyChatbug073Hint)})
}

// chatbug073EditAria adds the key hint to whatever already describes the edit
// box (the moderation line of CHATMOD-002, when there is one).
func chatbug073EditAria(postID string, aria map[string]string) map[string]string {
	out := map[string]string{}
	for name, value := range aria {
		out[name] = value
	}
	if described := out["describedby"]; described != "" {
		out["describedby"] = described + " " + chatbug073HintID(postID)
	} else {
		out["describedby"] = chatbug073HintID(postID)
	}
	return out
}

// chatbug073EditedMark is the "(edited)" that follows an edited message's text.
// It is not drawn while that message is open in the edit box.
func chatbug073EditedMark(m Model, msg Message) ui.Node {
	if !msg.Edited || m.EditingID == msg.ID {
		return nil
	}
	return html.Span(html.Props{Class: "message-edited", Text: chatbug073Text(m, keyChatbug073Edited)})
}

// chatbug073EditSaves reports whether a key press in the edit box saves the
// edit. Enter saves, as it sends in the composer; Shift+Enter is a line break,
// and a key press that belongs to an input method is the input method's.
func chatbug073EditSaves(key string, shift, composing bool) bool {
	return key == "Enter" && !shift && !composing
}

// chatbug073LatestOwn is the newest message the viewer wrote in the open
// conversation and may still edit: their own, written as themselves, and not
// deleted or removed.
func chatbug073LatestOwn(m Model) (Message, bool) {
	var latest Message
	found := false
	for _, msg := range m.Messages {
		if m.CurrentUser == "" || msg.AuthorID != m.CurrentUser || msg.PersonaActor != nil || chatremoveIsTombstone(msg) {
			continue
		}
		if !found || !msg.SentAt.Before(latest.SentAt) {
			latest, found = msg, true
		}
	}
	return latest, found
}

// chatbug073ArrowUpEdits opens the viewer's latest message for editing when
// ArrowUp is pressed in an empty composer, and reports whether it did. A draft,
// a held modifier or an edit already open leaves the key to the box.
func chatbug073ArrowUpEdits(m Model, key, draft string, modified bool) bool {
	if key != "ArrowUp" || draft != "" || modified || m.EditingID != "" || m.Callbacks.BeginEdit == nil {
		return false
	}
	latest, ok := chatbug073LatestOwn(m)
	if !ok {
		return false
	}
	m.Callbacks.BeginEdit(latest.ID)
	return true
}

// editBoxHeight caps the edit box at half the window.
func editBoxHeight(text, window float64) float64 {
	if window > 0 && text > window/2 {
		return window / 2
	}
	return text
}
