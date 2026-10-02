package chatui

import "testing"

// TestTodo_CHATBUG_059 holds the creator line: a conversation the viewer made
// names them as the sentence needs ("Erstellt von Ihnen", not "Erstellt von
// du"), and one somebody else made names that person.
func TestTodo_CHATBUG_059(t *testing.T) {
	c := Conversation{ID: "room", OwnerID: "walt"}
	for locale, want := range map[string]string{"en-US": "Created by you", "de-DE": "Erstellt von Ihnen", "ar": "أنشأتها أنت"} {
		m := Model{Locale: locale, CurrentUser: "walt"}
		if got := chatbug059CreatedBy(m, c, "du"); got != want {
			t.Errorf("%s: %q, want %q", locale, got, want)
		}
	}
	m := Model{Locale: "de-DE", CurrentUser: "walt"}
	if got := chatbug059CreatedBy(m, Conversation{OwnerID: "dana"}, "Dana Ortiz"); got != "Erstellt von Dana Ortiz" {
		t.Errorf("another person's conversation reads %q", got)
	}
}
