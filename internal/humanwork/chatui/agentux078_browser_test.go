package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_AGENTUX_078_Browser: the run row of a question asked on a server
// with no model. In every language the card reads the failure's one plain
// sentence with the agent named and nothing else: no failure code, no
// placeholder, no provider detail. Asking again cannot mend it, so what would
// is said; the person may still ask again or dismiss the card.
func TestTodo_AGENTUX_078_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		projection := PersonaProgressProjection{ViewerID: "reader", InvokerID: "reader", AgentName: "Policy Helper", Failure: &PersonaProgressFailure{InvokerID: "reader", InvocationID: "invoke", Code: "SERVER_HAS_NO_MODEL", Retryable: false, Message: "technical secret"}}
		markup := renderPersonaProgressTest(t, Model{Locale: locale}, projection)
		copy := chat.AgentAnswerFailureFor(locale, "Policy Helper", "SERVER_HAS_NO_MODEL")
		if copy.Class != "no_model" || copy.Retryable || copy.Sentence == "" || strings.Contains(copy.Sentence, "SERVER_HAS_NO_MODEL") || strings.Contains(copy.Sentence, "{") {
			t.Fatalf("%s: no-model card reads %+v", locale, copy)
		}
		sentence := strings.ReplaceAll(copy.Sentence, "'", "&#39;")
		next := strings.ReplaceAll(copy.NextStep, "'", "&#39;")
		if !strings.Contains(markup, "Policy Helper") || !strings.Contains(markup, sentence) || !strings.Contains(markup, next) {
			t.Fatalf("%s: the run row does not read the plain sentence: %s", locale, markup)
		}
		for _, leak := range []string{"SERVER_HAS_NO_MODEL", "technical secret", "{agent}"} {
			if strings.Contains(markup, leak) {
				t.Fatalf("%s: the run row leaks %q: %s", locale, leak, markup)
			}
		}
		if !strings.Contains(markup, `data-agent-action="retry"`) || !strings.Contains(markup, `data-agent-action="dismiss"`) {
			t.Fatalf("%s: the run row offers no way to ask again or dismiss: %s", locale, markup)
		}
	}
}
