package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// TestIssue3821_GatedJoinOpensFormNotConfirmation: the button that says "Answer
// 3 questions to join" must not start the plain "Join #channel?" confirmation;
// it opens the gate form and puts Browse away. An ungated row still joins.
func TestIssue3821_GatedJoinOpensFormNotConfirmation(t *testing.T) {
	for _, locale := range chatgateLocales {
		m := chatgateBrowseModel(locale)
		markup := chatremoveMarkup(t, browseDialog(m, handlers{}))
		rows := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "li" && chatPolishAttr(n, "data-conversation-id") != ""
		})
		saw := map[string]bool{}
		for _, row := range rows {
			id := chatPolishAttr(row, "data-conversation-id")
			saw[id] = true
			for _, b := range chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishAttr(n, "data-id") == id }) {
				got := chatPolishAttr(b, "data-action")
				if id == "gated" || id == "one" {
					if got == "join" || chatPolishAttr(b, "data-gate-open") != id {
						t.Errorf("%s: gated row %s button action=%q gate-open=%q", locale, id, got, chatPolishAttr(b, "data-gate-open"))
					}
				} else if id == "open" && got != "join" {
					t.Errorf("%s: ungated row lost its join action: %q", locale, got)
				}
			}
			if id == "gated" && (chatPolishAttr(row, "data-action") == "join" || chatPolishAttr(row, "data-gate-open") != id) {
				t.Errorf("%s: the gated row itself still starts the join confirmation", locale)
			}
		}
		if !saw["gated"] {
			t.Fatalf("%s: no gated row: %s", locale, strings.TrimSpace(markup))
		}
		// Pressing it closes Browse and asks for no confirmation.
		closed, asked := false, false
		m.Callbacks.CloseBrowse = func() { closed = true }
		m.Callbacks.RequestJoinConversation = func(string) { asked = true }
		m.act("gate-join", "gated")
		if !closed || asked {
			t.Errorf("%s: closed=%v asked=%v", locale, closed, asked)
		}
	}
}
