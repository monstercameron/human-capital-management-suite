package chatui

import (
	"strings"
	"testing"
)

// ISSUE-3706: when the service refuses a public channel name because another
// channel holds it, the create dialog keeps open and says the name is taken
// under the name box, in every shipped language. A different name clears it.
func TestIssue3706_NameTakenIsShownUnderTheNameBox(t *testing.T) {
	for locale, taken := range map[string]string{
		"en-US": "A public channel with this name already exists.",
		"de-DE": "Ein öffentlicher Kanal mit diesem Namen existiert bereits.",
		"ar":    "توجد قناة عامة بهذا الاسم بالفعل.",
	} {
		m := Model{Locale: locale, NewNameTaken: "general", Callbacks: Callbacks{CreateConversation: func(ConversationKind, string, []string) {}}}
		dialog := renderNode(t, createDialog(m, handlers{local: localUI{nameTyped: "general"}}))
		if !strings.Contains(dialog, taken) || strings.Contains(dialog, "chat.bug072") {
			t.Errorf("%s: the taken-name note is missing or prints a copy key: %s", locale, dialog)
		}
		if strings.Contains(dialog, "Use letters, numbers") && locale == "en-US" {
			t.Errorf("%s: the naming rule is shown for a name the rule accepts: %s", locale, dialog)
		}
		other := renderNode(t, createDialog(m, handlers{local: localUI{nameTyped: "random"}}))
		if strings.Contains(other, taken) {
			t.Errorf("%s: the taken-name note outlives the name it was for: %s", locale, other)
		}
	}
}
