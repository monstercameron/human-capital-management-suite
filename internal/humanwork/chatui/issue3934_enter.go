package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ISSUE-3934: a personal choice for what the Enter key does in message boxes.
// "send" (the default) is the long-standing behaviour: Enter sends, Shift+Enter
// adds a line. "newline" makes Enter add a line and moves sending to
// Ctrl+Enter (Cmd+Enter on a Mac) and the Send button. The choice is kept with
// the person's other Chat preferences (Model.ComposerEnter), so it follows them
// to every device and never changes how anyone else's boxes behave. It covers
// the channel and thread composers, the "/poll" and "/todo" previews (which post
// from the composer) and the message edit box. Input-method composition is
// always left to the input method, and Shift+Enter is a line in both modes.

const (
	// ComposerEnterSends is the default: Enter sends.
	ComposerEnterSends = "send"
	// ComposerEnterNewline makes Enter add a line; Ctrl or Cmd+Enter sends.
	ComposerEnterNewline = "newline"
)

// composerEnterNewline is whether Enter adds a line for this viewer.
func composerEnterNewline(m Model) bool { return m.ComposerEnter == ComposerEnterNewline }

// issue3934EditSaves is whether a key press in the edit box saves the edit.
// With Enter adding lines, saving needs Ctrl or Cmd as well.
func issue3934EditSaves(key string, shift, composing, command, newline bool) bool {
	if !chatbug073EditSaves(key, shift, composing) {
		return false
	}
	return !newline || command
}

const (
	keyIssue3934Title       = "chat.issue3934.title"
	keyIssue3934Send        = "chat.issue3934.send"
	keyIssue3934Newline     = "chat.issue3934.newline"
	keyIssue3934Note        = "chat.issue3934.note"
	keyIssue3934Hint        = "chat.issue3934.hint"
	keyIssue3934EditHint    = "chat.issue3934.edit_hint"
	keyIssue3934PreviewKeys = "chat.issue3934.preview_keys"
)

var issue3934Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3934Title:       "Enter key",
		keyIssue3934Send:        "Enter sends",
		keyIssue3934Newline:     "Enter adds a new line",
		keyIssue3934Note:        "With “Enter adds a new line”, send with Ctrl+Enter or ⌘+Enter, or with the Send button. Shift+Enter always adds a line.",
		keyIssue3934Hint:        "Enter for a new line, Ctrl+Enter or ⌘+Enter to send",
		keyIssue3934EditHint:    "Ctrl+Enter or ⌘+Enter to save, Enter for a new line, Esc to cancel",
		keyIssue3934PreviewKeys: "Ctrl+Enter or ⌘+Enter posts · Enter adds a line · Esc cancels",
	},
	"de-DE": {
		keyIssue3934Title:       "Enter-Taste",
		keyIssue3934Send:        "Enter sendet",
		keyIssue3934Newline:     "Enter fügt eine neue Zeile ein",
		keyIssue3934Note:        "Bei „Enter fügt eine neue Zeile ein“ senden Sie mit Strg+Enter oder ⌘+Enter oder mit der Schaltfläche „Senden“. Umschalt+Enter fügt immer eine Zeile ein.",
		keyIssue3934Hint:        "Enter für eine neue Zeile, Strg+Enter oder ⌘+Enter zum Senden",
		keyIssue3934EditHint:    "Strg+Enter oder ⌘+Enter speichert, Enter fügt eine Zeile ein, Esc bricht ab",
		keyIssue3934PreviewKeys: "Strg+Enter oder ⌘+Enter veröffentlicht · Enter neue Zeile · Esc bricht ab",
	},
	"ar": {
		keyIssue3934Title:       "مفتاح Enter",
		keyIssue3934Send:        "Enter يرسل",
		keyIssue3934Newline:     "Enter يضيف سطرًا جديدًا",
		keyIssue3934Note:        "عند اختيار «Enter يضيف سطرًا جديدًا» أرسل بالضغط على Ctrl+Enter أو ⌘+Enter أو بزر الإرسال. يضيف Shift+Enter سطرًا دائمًا.",
		keyIssue3934Hint:        "Enter لسطر جديد، وCtrl+Enter أو ⌘+Enter للإرسال",
		keyIssue3934EditHint:    "Ctrl+Enter أو ⌘+Enter للحفظ، وEnter لسطر جديد، وEsc للإلغاء",
		keyIssue3934PreviewKeys: "Ctrl+Enter أو ⌘+Enter للنشر · Enter لسطر جديد · Esc للإلغاء",
	},
}

func issue3934Text(m Model, key string) string {
	return chatbug039Text(key, issue3934Copy[chatEmojiLocale(m.Locale)][key], issue3934Copy["en-US"][key])
}

// issue3934ComposerHint is the key hint under a message box: the default hint
// until the viewer chooses Enter for a new line, then the hint that names the
// send shortcut.
func issue3934ComposerHint(m Model) string {
	if composerEnterNewline(m) {
		return issue3934Text(m, keyIssue3934Hint)
	}
	return m.t(KeyComposeHint)
}

// issue3934EditHintText is the key hint under the edit box.
func issue3934EditHintText(m Model) string {
	if composerEnterNewline(m) {
		return issue3934Text(m, keyIssue3934EditHint)
	}
	return chatbug073Text(m, keyChatbug073Hint)
}

// issue3934PreviewKeys is the key line of the poll and to-do preview.
func issue3934PreviewKeys(m Model) string {
	if composerEnterNewline(m) {
		return issue3934Text(m, keyIssue3934PreviewKeys)
	}
	return chatcmd003Text(m, "keys")
}

// issue3934Choose applies the viewer's choice: the page keeps it at once through
// the callback, which also files it with their other preferences. Anything but
// the two known modes is refused.
func issue3934Choose(m Model, mode string) bool {
	if m.Callbacks.SetComposerEnter == nil || (mode != ComposerEnterSends && mode != ComposerEnterNewline) {
		return false
	}
	if (mode == ComposerEnterNewline) != composerEnterNewline(m) {
		m.Callbacks.SetComposerEnter(mode == ComposerEnterNewline)
	}
	return true
}

// issue3934Row is the "Enter key" row of Chat preferences: two buttons, the
// current one pressed. It is drawn only where the page can keep the choice.
func issue3934Row(m Model) ui.Node {
	if m.Callbacks.SetComposerEnter == nil {
		return nil
	}
	title := issue3934Text(m, keyIssue3934Title)
	newline := composerEnterNewline(m)
	choice := func(mode, label string, pressed bool) ui.Node {
		return html.Button(html.Props{Class: "chat-prefs-choice", Type: "button", Dir: "auto", Text: label,
			Data: map[string]string{"action": "composer-enter", "extra": mode, "prefs-enter": mode},
			Aria: map[string]string{"pressed": boolString(pressed)}})
	}
	current := issue3934Text(m, keyIssue3934Send)
	if newline {
		current = issue3934Text(m, keyIssue3934Newline)
	}
	return chatux002Row("composer-enter", title, current, nil,
		html.Div(html.Props{Class: "chat-prefs-choices", Role: "group", Aria: map[string]string{"label": title}},
			choice(ComposerEnterSends, issue3934Text(m, keyIssue3934Send), !newline),
			choice(ComposerEnterNewline, issue3934Text(m, keyIssue3934Newline), newline)),
		html.P(html.Props{Class: "field-hint chat-prefs-note", Dir: "auto", Text: issue3934Text(m, keyIssue3934Note)}))
}

// Issue3934Styles is the two-button choice; 44px targets, wrapping on a phone,
// logical properties only so it holds in right-to-left.
const Issue3934Styles = `.chat-prefs-choices{display:flex;flex-wrap:wrap;gap:6px;padding-block:4px}` +
	`.chat-prefs-choice{box-sizing:border-box;min-block-size:44px;padding-block:6px;padding-inline:14px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;cursor:pointer;text-align:start}` +
	`.chat-prefs-choice:hover{background:color-mix(in srgb,var(--ink) 6%,transparent)}` +
	`.chat-prefs-choice[aria-pressed="true"]{border-color:var(--accent);background:color-mix(in srgb,var(--accent) 14%,transparent);box-shadow:inset 0 0 0 1px var(--accent)}` +
	`.chat-prefs-choice:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}`
