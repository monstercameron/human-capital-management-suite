package chatui

import (
	"strings"
	"testing"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
)

// chatux024Shown renders the empty conversation with the agent and returns the
// example questions on it, as read.
func chatux024Shown(t *testing.T, m Model) []string {
	t.Helper()
	markup := chatPolishMarkup(t, Build(m), 1440, "light")
	buttons := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return n.Data == "button" && chatPolishAttr(n, "data-action") == "agent-example"
	})
	shown := make([]string, 0, len(buttons))
	for _, button := range buttons {
		text := chatbug030Text(button)
		if chatPolishAttr(button, "data-extra") != text {
			t.Fatalf("an example fills the box with %q and reads %q", chatPolishAttr(button, "data-extra"), text)
		}
		shown = append(shown, text)
	}
	return shown
}

// Every example on the empty conversation is a whole sentence. An agent with
// nothing placed for it (it reads no document class the page knows) gets the
// general set; none of its examples is a stem with its topic missing.
func TestTodo_CHATUX_024_Examples(t *testing.T) {
	ends := map[string]string{"en-US": "?.", "de-DE": "?.", "ar": "؟?."}
	for locale, allowed := range ends {
		for name, classes := range map[string][]string{"no placed documents": nil, "workspace documents": {"WORKSPACE_DOCUMENT"}, "policy documents": {"POLICY_DOCUMENT"}} {
			m := chatux016Assistant(locale)
			m.ResolvedPersonaMentions[0].DataClasses = classes
			shown := chatux024Shown(t, m)
			if len(shown) != 3 {
				t.Fatalf("%s, %s: %d examples, want 3: %q", locale, name, len(shown), shown)
			}
			for _, example := range shown {
				last, _ := utf8.DecodeLastRuneInString(example)
				if example != strings.TrimSpace(example) || !strings.ContainsRune(allowed, last) {
					t.Fatalf("%s, %s: the example %q does not end in a question mark or a full stop", locale, name, example)
				}
				if len(strings.Fields(example)) < 3 {
					t.Fatalf("%s, %s: the example %q is not a sentence", locale, name, example)
				}
			}
		}
		// The agent the page knows nothing about yet (its directory has not
		// arrived) is offered the general set, whole.
		unknown := chatux016Assistant(locale)
		unknown.ResolvedPersonaMentions = nil
		for _, example := range chatux024Shown(t, unknown) {
			last, _ := utf8.DecodeLastRuneInString(example)
			if !strings.ContainsRune(allowed, last) {
				t.Fatalf("%s: before the directory arrives the example %q is not whole", locale, example)
			}
		}
	}
	if page := chatPolishMarkup(t, Build(chatux016Assistant("en-US")), 1440, "light"); strings.Contains(page, "Summarise the policy on") || strings.Contains(page, "Summarise the document about") {
		t.Fatal("the empty conversation still offers a sentence with its topic missing")
	}

	// The agent's own examples come first and replace the built-in set.
	own := chatux016Assistant("en-US")
	own.ResolvedPersonaMentions[0].DataClasses = []string{"POLICY_DOCUMENT"}
	own.ResolvedPersonaMentions[0].Examples = []string{"Which holidays are left this year?", "How do I book a meeting room?", "Who approves my expenses?", "A fourth one is kept for later."}
	if shown := chatux024Shown(t, own); len(shown) != 3 || shown[0] != "Which holidays are left this year?" || shown[1] != "How do I book a meeting room?" || shown[2] != "Who approves my expenses?" {
		t.Fatalf("the agent's own examples are not the three shown: %q", shown)
	}
	// One of its examples: that one alone, not padded from another agent's set.
	own.ResolvedPersonaMentions[0].Examples = []string{"Which holidays are left this year?"}
	if shown := chatux024Shown(t, own); len(shown) != 1 || shown[0] != "Which holidays are left this year?" {
		t.Fatalf("one example of its own is shown as %q", shown)
	}
	// Examples that are not whole sentences, are blank or repeat are left out;
	// when none is left the built-in set stands in.
	own.ResolvedPersonaMentions[0].Examples = []string{"Summarise the policy on ", "  ", "Which holidays are left this year?", "Which holidays   are left this year?", "Tell me about"}
	if shown := chatux024Shown(t, own); len(shown) != 1 || shown[0] != "Which holidays are left this year?" {
		t.Fatalf("unfinished or repeated examples were shown: %q", shown)
	}
	own.ResolvedPersonaMentions[0].Examples = []string{"Summarise the policy on ", ""}
	if shown := chatux024Shown(t, own); len(shown) != 3 || !strings.Contains(strings.ToLower(shown[0]), "policy") {
		t.Fatalf("an agent with no usable example of its own shows %q, want the built-in policy set", shown)
	}
	// Pressing one of its own examples fills the box and sends nothing.
	sent, drafted := 0, ""
	own.Callbacks.SendMessage = func(string, string) { sent++ }
	own.Callbacks.DraftChanged = func(conversation, value string) { drafted = conversation + "|" + value }
	chatux024UseExample(own, "Which holidays are left this year?")
	if sent != 0 || drafted != "assistant|Which holidays are left this year?" {
		t.Fatalf("pressing an example sent %d messages and drafted %q", sent, drafted)
	}
}
