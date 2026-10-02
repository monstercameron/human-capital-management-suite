package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A reply the checks refused is drawn as an answer that is not shown, never as
// an agent that is unavailable; a reply that was only a document's name says
// that, and both offer Ask again to the person who asked.
func TestTodo_CHATBUG_049_Browser(t *testing.T) {
	for _, direct := range []bool{false, true} {
		for code, want := range map[string][2]string{
			"OUTPUT_REJECTED":             {"Policy Helper wrote an answer that did not pass its checks, so it is not shown.", "Ask again, or ask about one thing at a time."},
			chat.AgentAnswerTitleOnlyCode: {"Policy Helper answered with only the name of a document, so the answer is not shown.", "Ask again, or ask what the document says."},
		} {
			m := chat4Fixture("en-US", "failed", direct)
			m.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "run", InvokerID: "alice", Code: code}
			card := chatbug054Card(t, m)
			text := chatbug030Text(card)
			if !strings.Contains(text, want[0]) || !strings.Contains(text, want[1]) {
				t.Fatalf("direct=%v %s: the card reads %q", direct, code, text)
			}
			for _, wrong := range []string{"is not available in this conversation right now", "check its setup", code, "could not answer because"} {
				if strings.Contains(text, wrong) {
					t.Fatalf("direct=%v %s: the card says %q: %q", direct, code, wrong, text)
				}
			}
			// The reason is one live sentence; what to do next is the muted line under it.
			if heading := chatPolishNodesIn(card, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-failure-heading") }); len(heading) != 1 || strings.TrimSpace(chatbug030Text(heading[0])) != want[0] {
				t.Fatalf("direct=%v %s: the reason is not the one sentence", direct, code)
			}
			retry := chatPolishNodesIn(card, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-agent-action") == "retry" })
			if len(retry) != 1 || chatPolishAttr(retry[0], "data-agent-invocation-id") != "run" || !strings.Contains(chatbug030Text(retry[0]), "Ask again") {
				t.Fatalf("direct=%v %s: Ask again is not offered for this run", direct, code)
			}
		}
	}
	// German and Arabic have the sentences too, and never the English one.
	for _, locale := range []string{"de-DE", "ar"} {
		m := chat4Fixture(locale, "failed", false)
		m.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "run", InvokerID: "alice", Code: chat.AgentAnswerTitleOnlyCode}
		text := chatbug030Text(chatbug054Card(t, m))
		if !strings.Contains(text, chat.AgentAnswerFailureFor(locale, "Policy Helper", chat.AgentAnswerTitleOnlyCode).Sentence) || strings.Contains(text, "answered with only") {
			t.Fatalf("%s: the title-only card reads %q", locale, text)
		}
	}
}
