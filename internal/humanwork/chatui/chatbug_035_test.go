package chatui

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

func TestTodo_CHATBUG_035(t *testing.T) {
	// Jump to newest: only with newer messages out of view.
	for _, tc := range []struct {
		name                                         string
		scrollHeight, scrollTop, clientHeight        float64
		hasNewer, wantOutOfView, wantScrollableAtAll bool
	}{
		{"nothing to scroll (one message)", 120, 0, 600, false, false, false},
		{"at the bottom of a long list", 3000, 2400, 600, false, false, true},
		{"scrolled up in a long list", 3000, 1000, 600, false, true, true},
		{"a newer page is not loaded yet", 3000, 2400, 600, true, true, true},
		{"nothing to scroll but a newer page exists", 120, 0, 600, true, true, false},
		{"within rounding of the bottom", 3000, 2399.5, 600, false, false, true},
	} {
		if got := chatNewestOutOfView(tc.scrollHeight, tc.scrollTop, tc.clientHeight, tc.hasNewer); got != tc.wantOutOfView {
			t.Errorf("%s: chatNewestOutOfView = %v, want %v", tc.name, got, tc.wantOutOfView)
		}
		if got := chatJumpScrollable(tc.scrollHeight, tc.clientHeight); got != tc.wantScrollableAtAll {
			t.Errorf("%s: chatJumpScrollable = %v, want %v", tc.name, got, tc.wantScrollableAtAll)
		}
	}
	// The failure sentence is set at body weight and size.
	if got := chatbugCascadeValue(ChatMsgListStyles, ".agent-failure-heading", "font-weight"); got != "400" {
		t.Fatalf("failure sentence font-weight = %q, want 400", got)
	}
	if got := chatbugCascadeValue(ChatMsgListStyles, ".agent-failure-heading", "font-size"); got != ".9375rem" {
		t.Fatalf("failure sentence font-size = %q, want the body size .9375rem", got)
	}
	if got := chatbugCascadeValue(Stylesheet, ".message-body", "font-size"); got != ".9375rem" {
		t.Fatalf("premise changed: body font-size = %q", got)
	}
}

func TestTodo_CHATBUG_035_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, direct := range []bool{false, true} {
			// A failed answer to a recent question offers Try again, once, with one icon.
			m := chat4Fixture(locale, "failed", direct)
			m.Messages[0].SentAt = time.Now().Add(-2 * time.Minute)
			card := chatbug035Failure(t, m)
			icons := chatPolishNodesIn(card, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chat-icon") })
			if len(icons) != 1 {
				t.Fatalf("%s direct=%v: failure card has %d icons, want the one warning", locale, direct, len(icons))
			}
			retries := chatPolishNodesIn(card, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-agent-action") == "retry" })
			if len(retries) != 1 {
				t.Fatalf("%s direct=%v: %d retry offers on a two minute old failure", locale, direct, len(retries))
			}
			sentence := chatbug030Text(card)
			if !strings.Contains(sentence, "Policy Helper") {
				t.Fatalf("%s: failure card lost its sentence: %q", locale, sentence)
			}
			// The sentence never tells the reader to press something: the button
			// says it, once.
			if locale == "en-US" && strings.Contains(sentence, "Try again") {
				t.Fatalf("the failure sentence repeats its button: %q", sentence)
			}
			// CHATBUG-054: nine hours later the person who asked can still ask
			// again, with the same single action and the same single icon.
			m.Messages[0].SentAt = time.Now().Add(-9 * time.Hour)
			late := chatbug035Failure(t, m)
			if got := chatPolishNodesIn(late, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-agent-action") == "retry" }); len(got) != 1 {
				t.Fatalf("%s direct=%v: %d Ask again offers nine hours after the question, want 1", locale, direct, len(got))
			}
			if len(chatPolishNodesIn(late, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chat-icon") })) != 1 {
				t.Fatalf("%s direct=%v: expired failure card has more than one icon", locale, direct)
			}
		}
	}
	// An answer card names its agent once in the text a reader hears.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "answered", false)
		rows := personaReplyRowsForPost(m, localUI{}, "question", time.Now())
		if len(rows) != 1 {
			t.Fatalf("%s: %d answer rows", locale, len(rows))
		}
		markup := chatPolishMarkup(t, rows[0], 1440, "light")
		cards := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "article" && chatPolishHasClass(n, "chat-ephemeral") })
		if len(cards) != 1 {
			t.Fatalf("%s: %d answer cards", locale, len(cards))
		}
		// The footer link ("Saved in your conversation with Policy Helper") is its
		// own control; the identity header and the status line are what must not
		// both carry the name.
		head := ""
		for c := cards[0].FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && !chatPolishHasClass(c, "agent-reply-footer") {
				head += " " + chatbug030Text(c)
			}
		}
		if got := strings.Count(head, "Policy Helper"); got != 1 {
			t.Fatalf("%s: the agent's name is in the card's identity and status text %d times: %q", locale, got, head)
		}
	}
}

// chatbug035Failure renders the failed-answer card for the fixture's question.
func chatbug035Failure(t *testing.T, m Model) *xhtml.Node {
	t.Helper()
	rows := personaReplyRowsForPost(m, localUI{}, "question", time.Now())
	if len(rows) != 1 {
		t.Fatalf("%d agent rows", len(rows))
	}
	markup := chatPolishMarkup(t, rows[0], 1440, "light")
	cards := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return n.Data == "article" && (chatPolishHasClass(n, "persona-progress-failure") || chatPolishHasClass(n, "agent-direct-state"))
	})
	if len(cards) != 1 {
		t.Fatalf("%d failure cards", len(cards))
	}
	return cards[0]
}
