package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	xhtml "golang.org/x/net/html"
)

// chatbug006Body is an answer as the server projects it for one reader: a
// source that reader may open is a link to the cited section, and one they may
// not is plain text marked as such. The decision is the server's.
const chatbug006Body = "Employees may carry over up to 40 hours.\n\nSources\n" +
	"- [Paid time off policy · Carryover · v1.0.0](/workspace/app/docs?document=pto-policy&version=3#carryover) <!--chat.agent.source.readable:true-->\n" +
	"- 2026 holiday guide · v1.0.0 <!--chat.agent.source.readable:false-->"

func chatbug006Chips(t *testing.T, markup string) []*xhtml.Node {
	t.Helper()
	return chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "li" && chatPolishHasClass(n, "agent-reply-source") })
}

// Under an answer, a source the reader may open is a link that opens the
// document at the cited section; the access note appears only on a source the
// server said this reader may not open.
func TestTodo_CHATBUG_006_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, direct := range []bool{false, true} {
			m := chat4Fixture(locale, "answered", direct)
			m.PersonaActivityReady = true
			var markup string
			if direct {
				m.Messages[1].Body = chatbug006Body
				markup = render(t, m)
			} else {
				m.EphemeralMessages[0].Body = chatbug006Body
				markup = chatPolishMarkup(t, html.Div(html.Props{}, personaReplyRowsForPost(m, localUI{}, "question", time.Now())...), 1440, "light")
			}
			chips := chatbug006Chips(t, markup)
			if len(chips) != 2 {
				t.Fatalf("%s direct=%v: %d source chips, want 2", locale, direct, len(chips))
			}
			// The readable source: one link, to the section, and no access note.
			links := chatPolishNodesIn(chips[0], func(n *xhtml.Node) bool { return n.Data == "a" })
			if len(links) != 1 || chatPolishAttr(links[0], "href") != "/workspace/app/docs?document=pto-policy&version=3#carryover" {
				t.Fatalf("%s direct=%v: the readable source is not a link to its section: %+v", locale, direct, links)
			}
			if text := chatbug030Text(chips[0]); !strings.Contains(text, "Paid time off policy · Carryover · v1.0.0") || strings.Contains(text, agentAnswerSourceUnavailable(locale)) {
				t.Fatalf("%s direct=%v: readable source reads %q", locale, direct, text)
			}
			if chatPolishHasClass(chips[0], "agent-reply-source-locked") {
				t.Fatalf("%s direct=%v: a source the reader may open is drawn as closed", locale, direct)
			}
			// The closed source: no link, the note as its tooltip, reachable by keyboard.
			if closed := chatPolishNodesIn(chips[1], func(n *xhtml.Node) bool { return n.Data == "a" }); len(closed) != 0 {
				t.Fatalf("%s direct=%v: a source the reader may not open is a link", locale, direct)
			}
			if !chatPolishHasClass(chips[1], "agent-reply-source-locked") || chatPolishAttr(chips[1], "tabindex") != "0" || !strings.Contains(chatbug030Text(chips[1]), agentAnswerSourceUnavailable(locale)) || !strings.Contains(chatbug030Text(chips[1]), "2026 holiday guide") {
				t.Fatalf("%s direct=%v: the closed source reads %q", locale, direct, chatbug030Text(chips[1]))
			}
			if got := strings.Count(markup, agentAnswerSourceUnavailable(locale)); got != 1 {
				t.Fatalf("%s direct=%v: the access note appears %d times, want once, on the closed source", locale, direct, got)
			}
		}
	}
	// An address that is not a document of this product is never made a link.
	for _, href := range []string{"javascript:alert(1)", "/workspace/app/people?document=x", "/workspace/app/docs", "ftp://files.example/workspace/app/docs?document=x"} {
		envelope := parseAgentReplyEnvelope("Answer.\n\nSources\n- [Handbook](" + href + ")")
		if len(envelope.Sources) != 1 || envelope.Sources[0].Href != "" {
			t.Fatalf("%q became a source link: %+v", href, envelope.Sources)
		}
	}
}
