package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// Issues 3933, 3912 and 3911: the Saved panel says its items are private, the
// note editor has its own Save and Cancel, and the reminder form stacks.

func TestIssue3933_SavedPanelSaysItIsPrivate(t *testing.T) {
	want := map[string]string{
		"en-US": "Only you can see your saved messages, notes and reminders. Saving does not post a message or notify anyone.",
		"de-DE": "Nur Sie sehen Ihre gespeicherten Nachrichten, Notizen und Erinnerungen. Speichern sendet keine Nachricht und benachrichtigt niemanden.",
		"ar":    "أنت وحدك ترى رسائلك المحفوظة وملاحظاتك وتذكيراتك. الحفظ لا ينشر رسالة ولا يُخطر أحدًا.",
	}
	for locale, text := range want {
		view := chatsave002View("todo", chatsave002Rows()[:1])
		view.Locale = locale
		markup := renderNode(t, RenderSavedMessages(view))
		if !strings.Contains(markup, text) || !strings.Contains(markup, `id="chatsave-privacy"`) {
			t.Errorf("%s: the panel does not say Saved is private: %s", locale, markup)
		}
	}
}

func TestIssue3933_NoteEditorSaysNoteIsPrivate(t *testing.T) {
	want := map[string]string{
		"en-US": "Only you can see this note. It is not posted to the conversation.",
		"de-DE": "Nur Sie sehen diese Notiz. Sie wird nicht in der Unterhaltung veröffentlicht.",
		"ar":    "أنت وحدك ترى هذه الملاحظة. لا تُنشر في المحادثة.",
	}
	for locale, text := range want {
		view := chatsave002View("todo", chatsave002Rows()[:1])
		view.Locale = locale
		view.EditingNote = SavedItemKey("tenant", "general", "p1")
		markup := renderNode(t, RenderSavedMessages(view))
		if !strings.Contains(markup, text) {
			t.Errorf("%s: the note editor does not say the note is private", locale)
		}
		if !strings.Contains(markup, `aria-describedby="chatsave-status chatsave-note-private-p1"`) || !strings.Contains(markup, `id="chatsave-note-private-p1"`) {
			t.Errorf("%s: the note field is not described by the private line: %s", locale, markup)
		}
	}
}

func TestIssue3911_NoteEditorHasSaveAndCancel(t *testing.T) {
	want := map[string][2]string{"en-US": {"Save", "Cancel"}, "de-DE": {"Speichern", "Abbrechen"}, "ar": {"حفظ", "إلغاء"}}
	for locale, labels := range want {
		view := chatsave002View("todo", chatsave002Rows()[:1])
		view.Locale = locale
		view.EditingNote = SavedItemKey("tenant", "general", "p1")
		markup := renderNode(t, RenderSavedMessages(view))
		forms := chatsave002Find(t, markup, func(n *xhtml.Node) bool { return chatsave002Class(n, "chatsave-note-form") })
		if len(forms) != 1 {
			t.Fatalf("%s: want one note form: %s", locale, markup)
		}
		submits := chatsave002Find(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "type") == "submit" && chatPolishAncestor(n, "chatsave-note-form")
		})
		cancels := chatsave002Find(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-saved-action") == "note-cancel" && chatPolishAncestor(n, "chatsave-note-form")
		})
		if len(submits) != 1 || len(cancels) != 1 {
			t.Fatalf("%s: the note form needs one Save and one Cancel button: %s", locale, markup)
		}
		if chatPolishAttr(cancels[0], "type") != "button" {
			t.Errorf("%s: Cancel must not submit the form", locale)
		}
		if !strings.Contains(markup, ">"+labels[0]+"<") || !strings.Contains(markup, ">"+labels[1]+"<") {
			t.Errorf("%s: Save/Cancel labels missing: %s", locale, markup)
		}
	}
}

func TestIssue3911_NoteButtonsStayVisibleOnPhones(t *testing.T) {
	phone := Chatsave002Styles[strings.Index(Chatsave002Styles, "@media(max-width:760px),(pointer:coarse){"):]
	phone = phone[:strings.Index(phone, "\n}\n")]
	if !strings.Contains(phone, ".chatsave-note-buttons .button{min-height:44px}") {
		t.Errorf("note buttons are not 44px on phones: %s", phone)
	}
	if strings.Contains(phone, ".chatsave-note-private{display:none}") {
		t.Errorf("the private line must stay visible on phones")
	}
}

func TestIssue3912_CustomReminderFormStacks(t *testing.T) {
	var rule string
	for _, line := range strings.Split(Chatsave002Styles, "\n") {
		if strings.HasPrefix(line, ".chatsave-pick{") {
			rule = line
		}
	}
	if !strings.Contains(rule, "flex-direction:column") || !strings.Contains(rule, "align-items:stretch") {
		t.Errorf("the date and time form does not stack: %q", rule)
	}
	if !strings.Contains(Chatsave002Styles, ".chatsave-pick .button{") || strings.Contains(Chatsave002Styles, ".chatsave-pick .button{flex:none}") {
		t.Errorf("the submit button must not be a fixed-width flex item")
	}
	if !strings.Contains(Chatsave002Styles, ".chatsave-due-input{") || !strings.Contains(Chatsave002Styles, ".chatsave-pick .chatsave-due-input{") {
		t.Errorf("the date input has no full-width rule inside the pick form")
	}
	if !strings.Contains(Chatsave002Styles, "min-width:min(224px,calc(100vw - 32px))") {
		t.Errorf("the menu can be wider than a 320px viewport")
	}
}
