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
	if !strings.Contains(form, "novalidate") {
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
