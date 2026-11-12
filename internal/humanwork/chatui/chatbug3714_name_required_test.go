package chatui

import (
	"strings"
	"testing"
)

// Issue #3714: submitting a channel with an empty name gave no feedback. The
// native required check stopped the submit and the handler dropped an empty
// name, so the dialog stayed open with nothing said. The dialog must say the
// name is required, in words, under the name box.
func TestIssue3714_ChannelNameMissingIsSaidUnderTheBox(t *testing.T) {
	m := Model{Locale: "en-US", ShowCreate: true, NewKind: PublicChannel, CurrentUser: "walt", Callbacks: walkCallbacks()}
	dialog := renderNode(t, createDialog(m, handlers{local: localUI{createKind: PublicChannel, nameMissing: true}}))

	if !strings.Contains(dialog, "Name is required.") {
		t.Fatal("an empty channel name submitted gives no message under the name box")
	}
	if !strings.Contains(dialog, `role="alert"`) || !strings.Contains(dialog, `aria-invalid="true"`) {
		t.Error("the missing-name message is not announced as an alert on the name field")
	}
}

// The submit must reach the handler so it can say why: the form may not stop
// an empty channel name in the browser's own bubble, which leaves nothing on
// the page.
func TestIssue3714_ChannelFormSubmitsWithoutNativeRequiredBubble(t *testing.T) {
	m := Model{Locale: "en-US", ShowCreate: true, NewKind: PublicChannel, CurrentUser: "walt", Callbacks: walkCallbacks()}
	dialog := renderNode(t, createDialog(m, handlers{local: localUI{createKind: PublicChannel}}))
	formAt := strings.Index(dialog, `<form`)
	if formAt < 0 {
		t.Fatal("the create dialog has no form")
	}
	form := dialog[formAt:]
	form = form[:strings.Index(form, ">")+1]
	if !strings.Contains(form, "novalidate") {
		t.Errorf("the create form lets the browser stop an empty channel name silently; form is %s", form)
	}
}

// The message is localised like its siblings, and it is absent until a submit
// has set the flag, so a fresh dialog shows nothing.
func TestIssue3714_NameMissingCopyAndAbsence(t *testing.T) {
	want := map[string]string{
		"en-US": "Name is required.",
		"de-DE": "Name ist erforderlich.",
		"ar":    "الاسم مطلوب.",
	}
	for locale, text := range want {
		m := Model{Locale: locale}
		got := chatbug3714NameNote(m, localUI{nameMissing: true}, PublicChannel)
		if got != text {
			t.Errorf("%s: note = %q, want %q", locale, got, text)
		}
		if strings.HasPrefix(got, "chat.bug3714") {
			t.Errorf("%s: the copy key was printed instead of text", locale)
		}
	}
	if got := chatbug3714NameNote(Model{Locale: "en-US"}, localUI{}, PublicChannel); got != "" {
		t.Errorf("a fresh dialog shows %q before any submit", got)
	}
	if got := chatbug3714NameNote(Model{Locale: "en-US"}, localUI{nameMissing: true}, GroupChat); got != "" {
		t.Errorf("a group name is optional, but the note shows %q", got)
	}
}
