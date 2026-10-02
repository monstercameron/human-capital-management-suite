package chatui

import (
	"strings"
	"testing"
	"time"
)

// AGENTUX-032 clause table (the GREEN line, one row each):
//
//	a Sources list names each cited document ................... TestTodo_AGENTUX_032, TestTodo_AGENTUX_032_Browser
//	a reader who may open it gets a link at that version ....... TestTodo_AGENTUX_032_Browser (the link names document and version)
//	another reader sees the title as text ...................... TestTodo_AGENTUX_032_Browser
//	nothing when the title itself is restricted ................ TestTodo_AGENTUX_032_Browser
//	access revoked after delivery .............................. TestTodo_AGENTUX_032_Security (both halves: the page here, the server in internal/application)
//
// The same answer is what the server delivers to three readers: one who may
// open both documents, one who may open only the first, and one who may not
// open the second or even learn its title.
func TestTodo_AGENTUX_032_Browser(t *testing.T) {
	const sentence = "Employees may carry over up to 40 hours of unused PTO."
	both := sentence + "\n\nSources\n- [Paid time off policy · Carryover · v1.0.0](" + chatbug021Pol + ") <!--chat.agent.source.readable:true-->\n- [2026 holiday guide · v1.0.0](" + chatbug021Guide + ") <!--chat.agent.source.readable:true-->"
	textOnly := sentence + "\n\nSources\n- [Paid time off policy · Carryover · v1.0.0](" + chatbug021Pol + ") <!--chat.agent.source.readable:true-->\n- 2026 holiday guide <!--chat.agent.source.readable:false-->"
	hidden := sentence + "\n\nSources\n- [Paid time off policy · Carryover · v1.0.0](" + chatbug021Pol + ") <!--chat.agent.source.readable:true-->"

	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for name, tc := range map[string]struct {
			body          string
			links, locked int
			guideShown    bool
		}{
			"may open both":                {both, 2, 0, true},
			"may open one":                 {textOnly, 1, 1, true},
			"title of the other is hidden": {hidden, 1, 0, false},
		} {
			now := time.Now()
			card := renderNode(t, renderPersonaPrivateAnswer(Model{Locale: locale, CurrentUser: "walt"}, localUI{}, EphemeralMessage{ID: "card", ThreadID: "question", Body: tc.body, OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, PersonaProgressProjection{AgentName: "Policy Helper"}))
			m, post := agentux051Direct(tc.body)
			m.Locale = locale
			direct := renderNode(t, message(m, handlers{}, post, false))
			for surface, markup := range map[string]string{"channel card": card, "agent conversation": direct} {
				where := locale + "/" + name + "/" + surface
				if got := strings.Count(markup, "agent-reply-source-link"); got != tc.links {
					t.Fatalf("%s: %d source links, want %d: %s", where, got, tc.links, markup)
				}
				if got := strings.Count(markup, "agent-reply-source-locked"); got != tc.locked {
					t.Fatalf("%s: %d text-only sources, want %d: %s", where, got, tc.locked, markup)
				}
				// The link goes into the Documents hub at the cited version.
				if !strings.Contains(markup, `href="`+chatbug021Attr(chatbug021Pol)+`"`) || !strings.Contains(chatbug021Attr(chatbug021Pol), "version=docv-10264426") {
					t.Fatalf("%s: the first source does not open the cited version: %s", where, markup)
				}
				visible := chatbug021Visible(markup)
				if got := strings.Contains(visible, "2026 holiday guide"); got != tc.guideShown {
					t.Fatalf("%s: the second title is shown=%v, want %v", where, got, tc.guideShown)
				}
				if !tc.guideShown && (strings.Contains(markup, "doc-10c773e5") || strings.Contains(markup, "docv-8d270c40")) {
					t.Fatalf("%s: a restricted document's address reached the page: %s", where, markup)
				}
				// Each cited document is named once in the row.
				if strings.Count(strings.Join(strings.Fields(visible), " "), "Paid time off policy · Carryover · v1.0.0") != 1 {
					t.Fatalf("%s: the first source is not named exactly once in the row: %s", where, visible)
				}
			}
		}
	}

	// Access revoked after delivery: the answer is stored with its links; the
	// server projects the stored copy again for the reader as they are now, so
	// the page that reads the revoked projection shows text, not a link.
	revoked := strings.ReplaceAll(both, "[2026 holiday guide · v1.0.0]("+chatbug021Guide+") <!--chat.agent.source.readable:true-->", "2026 holiday guide <!--chat.agent.source.readable:false-->")
	m, post := agentux051Direct(both)
	delivered := renderNode(t, message(m, handlers{}, post, false))
	post.Body = revoked
	after := renderNode(t, message(m, handlers{}, post, false))
	if strings.Count(delivered, "agent-reply-source-link") != 2 || strings.Count(after, "agent-reply-source-link") != 1 || strings.Contains(after, "docv-8d270c40") {
		t.Fatalf("the revoked reader still has the link: delivered %d links, after %d", strings.Count(delivered, "agent-reply-source-link"), strings.Count(after, "agent-reply-source-link"))
	}
}
