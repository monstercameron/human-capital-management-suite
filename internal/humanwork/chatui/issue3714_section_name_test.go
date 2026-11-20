package chatui

import (
	"strings"
	"testing"
)

func issue3714SectionModel(locale, err string) Model {
	room := Conversation{ID: "sales", Name: "Sales", Kind: PublicChannel}
	m := Model{Locale: locale, State: StateReady, Conversations: []Conversation{room},
		Sections:  []SidebarSection{{ID: "channels", Name: "Channels", Chats: []Conversation{room}}, {ID: "direct", Name: "Direct messages"}},
		Callbacks: Callbacks{CreateSection: func(string) {}, ToggleSection: func(string) {}, OpenRailMenu: func(string) {}}}
	m.side.err = err
	return m
}

// Issue #3714: an empty or blank section name gave no feedback. The New
// section form must reach the handler (no native required bubble that a
// keyboard-less or scripted submit never shows) so it can say why.
func TestIssue3714_SectionFormSubmitsWithoutNativeRequiredBubble(t *testing.T) {
	markup := render(t, issue3714SectionModel("en-US", ""))
	at := strings.Index(markup, `<form class="section-create-form"`)
	if at < 0 {
		t.Fatal("the New section form is not drawn")
	}
	form := markup[at:]
	form = form[:strings.Index(form, ">")+1]
	if !strings.Contains(strings.ToLower(form), "novalidate") {
		t.Errorf("the New section form lets the browser stop an empty name silently: %s", form)
	}
}

// The refusal is said in words, announced, and tied to the field, in each language.
func TestIssue3714_SectionBlankNameIsSaidAndTiedToTheField(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "Give the section a name.", "de-DE": "Gib dem Bereich einen Namen.", "ar": "أعطِ القسم اسماً."} {
		m := issue3714SectionModel(locale, "")
		if got := chatside001NameError(m, "   ", ""); got != want {
			t.Fatalf("%s: blank name refusal = %q, want %q", locale, got, want)
		}
		markup := renderNode(t, rail(issue3714SectionModel(locale, want), handlers{}))
		if !strings.Contains(markup, `role="alert"`) || !strings.Contains(markup, want) {
			t.Errorf("%s: the refusal is not shown as an alert", locale)
		}
		at := strings.Index(markup, `id="chat-new-section"`)
		field := markup[strings.LastIndex(markup[:at], "<input") : at+300]
		field = field[:strings.Index(field, ">")+1]
		if !strings.Contains(field, `aria-invalid="true"`) || !strings.Contains(field, `aria-describedby="chat-new-section-error"`) {
			t.Errorf("%s: the field is not marked invalid and tied to its message: %s", locale, field)
		}
	}
}

// The live DOM sets Raw entries as element properties, and the form property is
// noValidate: a lower-case "novalidate" key becomes an unused expando and the
// browser still stops an empty name in its own bubble. Both forms must use the
// property name.
func TestIssue3714_FormsUseTheNoValidateProperty(t *testing.T) {
	section := render(t, issue3714SectionModel("en-US", ""))
	at := strings.Index(section, `<form class="section-create-form"`)
	if at < 0 {
		t.Fatal("the New section form is not drawn")
	}
	if tag := section[at : at+strings.Index(section[at:], ">")+1]; !strings.Contains(tag, "noValidate") {
		t.Errorf("section form is not set through the noValidate property: %s", tag)
	}
	m := Model{Locale: "en-US", ShowCreate: true, NewKind: PublicChannel, CurrentUser: "walt", Callbacks: walkCallbacks()}
	dialog := renderNode(t, createDialog(m, handlers{local: localUI{createKind: PublicChannel}}))
	formAt := strings.Index(dialog, `<form`)
	if formAt < 0 {
		t.Fatal("the create dialog has no form")
	}
	if tag := dialog[formAt : formAt+strings.Index(dialog[formAt:], ">")+1]; !strings.Contains(tag, "noValidate") {
		t.Errorf("channel form is not set through the noValidate property: %s", tag)
	}
}
