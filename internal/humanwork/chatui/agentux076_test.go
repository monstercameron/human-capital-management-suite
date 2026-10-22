package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// agentux076Mark is the code the server leaves at the end of an answer no
// document backs.
const agentux076Mark = "\n<!--chat.agent.source:none-->"

func agentux076Text0(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// AGENTUX-076: an answer that no document backs says so in one quiet line under
// the answer, in the reader's language, at each width, whether it is drawn as the
// answer card or as a post; an answer without the mark says nothing; and the
// code itself never reaches the page.
func TestTodo_AGENTUX_076_Browser(t *testing.T) {
	want := map[string]string{"en-US": "Not from your documents", "de-DE": "Nicht aus Ihren Dokumenten", "ar": "ليست من مستنداتك"}
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		markup, card := chatux003Card(t, locale, width, "", func(m *Model) { m.EphemeralMessages[0].Body += agentux076Mark })
		lines := chatux003Find(card, "agent-reply-ungrounded")
		if len(lines) != 1 || agentux076Text0(lines[0]) != want[locale] {
			t.Fatalf("%s/%d: the line under the answer is %d lines, %q, want %q", locale, width, len(lines), func() string {
				if len(lines) == 0 {
					return ""
				}
				return agentux076Text0(lines[0])
			}(), want[locale])
		}
		if chatPolishAttr(lines[0], "data-agent-answer-source") != "none" || chatPolishAttr(lines[0], "dir") != "auto" {
			t.Fatalf("%s/%d: the line carries no source flag or direction", locale, width)
		}
		for _, leaked := range []string{"chat.agent.source", "agentux076.", "<!--"} {
			if strings.Contains(markup, leaked) {
				t.Fatalf("%s/%d: %q reached the page", locale, width, leaked)
			}
		}
		// The line sits under the answer and above the sources.
		answer, line, sources := strings.Index(markup, "agent-reply-answer"), strings.Index(markup, "agent-reply-ungrounded"), strings.Index(markup, "agent-reply-sources")
		if answer < 0 || line < answer || sources < line {
			t.Fatalf("%s/%d: order answer=%d line=%d sources=%d", locale, width, answer, line, sources)
		}
		if locale != "en-US" && strings.Contains(markup, want["en-US"]) {
			t.Fatalf("%s/%d: English on a translated page", locale, width)
		}

		// Private and not from the documents: both lines, the mark's first.
		_, both := chatux003Card(t, locale, width, "agent", func(m *Model) { m.EphemeralMessages[0].Body += agentux076Mark })
		if len(chatux003Find(both, "agent-reply-ungrounded")) != 1 || len(chatux003Find(both, "agent-reply-why")) != 2 {
			t.Fatalf("%s/%d: private and unbacked answer does not carry both lines", locale, width)
		}

		// An answer without the mark says nothing of the kind.
		_, plain := chatux003Card(t, locale, width, "", nil)
		if got := chatux003Find(plain, "agent-reply-ungrounded"); len(got) != 0 {
			t.Fatalf("%s/%d: an answer with no mark carries the line", locale, width)
		}
	})

	// The answer posted in a channel carries its sources and then the mark. The
	// sources are still read, and the mark is kept last on the statement.
	posted := "Paris is the capital of France.\n\nSources\n- [Paid time off policy · v1.0.0](/workspace/app/docs?document=pto&version=v1) <!--chat.agent.source.readable:true-->" + agentux076Mark
	envelope := parseAgentReplyEnvelope(posted)
	shown := agentAnswerPresentBody(envelope.Body, envelope.Sources)
	if len(envelope.Sources) != 1 || !strings.HasSuffix(shown, "<!--chat.agent.source:none-->") || strings.Contains(shown, "Sources") || !strings.HasPrefix(shown, "Paris is the capital of France.") {
		t.Errorf("a posted answer with sources and the mark: sources=%d shown=%q", len(envelope.Sources), shown)
	}
	if plain := parseAgentReplyEnvelope("Paris."); strings.Contains(plain.Body, "chat.agent") {
		t.Errorf("an answer with no mark gained one: %q", plain.Body)
	}

	// A post that still carries the mark (the answer in a channel, the copy saved
	// in the asker's conversation) is drawn with the same line.
	for locale, line := range want {
		model := chat4Fixture(locale, "answered", false)
		markup := renderNode(t, spanOf(markdownMessageBody(model, "Paris is the capital of France."+agentux076Mark)))
		if !strings.Contains(markup, line) || strings.Contains(markup, "chat.agent.source") || !strings.Contains(markup, "Paris is the capital of France.") {
			t.Errorf("%s: a post with the mark: %s", locale, markup)
		}
		if plain := renderNode(t, spanOf(markdownMessageBody(model, "Paris is the capital of France."))); strings.Contains(plain, line) {
			t.Errorf("%s: a post without the mark carries the line", locale)
		}
	}
}

// Every example question the empty conversation offers is a whole sentence in
// each language and none is offered twice, so the server-side suite can ask each.
func TestTodo_AGENTUX_076_Examples(t *testing.T) {
	seen := map[string]bool{}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		questions := AgentExampleQuestions(locale)
		if len(questions) != 6 {
			t.Fatalf("%s: %d example questions, want the general three and the policy three", locale, len(questions))
		}
		for _, question := range questions {
			if !chatux024Whole(question) || seen[locale+question] || strings.HasPrefix(question, "chatux024") {
				t.Errorf("%s: example %q is not a whole sentence, is repeated or is a raw key", locale, question)
			}
			seen[locale+question] = true
		}
	}
	english := AgentExampleQuestions("en-US")
	if english[0] != "Which documents can you read in this conversation?" || english[2] != "What can you help me with here?" {
		t.Errorf("the two questions the owner asked are not the general examples: %q", english)
	}
}
