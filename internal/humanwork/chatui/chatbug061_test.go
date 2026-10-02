package chatui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// An answered card has no minimum height, so it never ends in an empty band;
// the placeholder keeps the height of the answer it stands for: the height the
// card had when this page last drew it, otherwise that of a two-line answer.
func TestTodo_CHATBUG_061(t *testing.T) {
	card := regexp.MustCompile(`[^{}]*\.chat-ephemeral[^{}]*\{[^}]*\}`)
	for _, rule := range card.FindAllString(Stylesheet, -1) {
		if strings.Contains(rule, "min-block-size") || strings.Contains(rule, "min-height") {
			t.Fatalf("an answer card is given a minimum height, which leaves an empty band under a short answer: %s", rule)
		}
	}
	if strings.Contains(ChatBug040Styles, "208px") {
		t.Fatal("the 208px minimum height is still in the placeholder styles")
	}

	unknown := chatbug040Asked("en-US")
	if got := chatbug061ReserveHeight(unknown, "question"); got != ChatBug061TwoLineHeight {
		t.Fatalf("an answer never drawn reserves %dpx, want the two-line height %dpx", got, ChatBug061TwoLineHeight)
	}
	known := chatbug040Asked("en-US")
	known.AgentCardHeights = map[string]int{"question": 264, "tiny": 3, "huge": 90000}
	if got := chatbug061ReserveHeight(known, "question"); got != 264 {
		t.Fatalf("a card drawn at 264px reserves %dpx", got)
	}
	if got := chatbug061ReserveHeight(known, "tiny"); got != ChatBug061TwoLineHeight {
		t.Fatalf("a nonsense measurement reserves %dpx, want the two-line height", got)
	}
	if got := chatbug061ReserveHeight(known, "huge"); got != chatbug061MaxHeight {
		t.Fatalf("a very tall card reserves %dpx, want the cap %dpx", got, chatbug061MaxHeight)
	}
}

// The room is on the placeholder itself, both before the agent activity has
// arrived and while a stored answer's text is on its way, and the answered card
// carries no height of its own.
func TestTodo_CHATBUG_061_Browser(t *testing.T) {
	first := chatbug040Asked("en-US")
	if page := render(t, first); !strings.Contains(page, `data-reserved-height="172"`) || !strings.Contains(page, `height="150"`) {
		t.Fatal("the first paint does not reserve a two-line answer's height")
	}
	first.AgentCardHeights = map[string]int{"question": 236}
	if page := render(t, first); !strings.Contains(page, `data-reserved-height="236"`) || !strings.Contains(page, `height="214"`) {
		t.Fatal("the first paint does not reserve the height the card had before")
	}
	stored := chatbug079Stored("en-US", time.Now().Add(time.Minute))
	stored.AgentCardHeights = map[string]int{"question": 236}
	if page := render(t, stored); !strings.Contains(page, `data-reserved-height="236"`) || !strings.Contains(page, `height="214"`) {
		t.Fatal("the room changes between the first paint and the wait for the answer's text")
	}
	// The page allows no inline style: the room is held without one.
	if rows, err := ui.RenderToString(html.Div(html.Props{}, personaReplyRowsForPost(stored, localUI{}, "question", time.Now())...)); err != nil || !strings.Contains(rows, "chatbug040-reserve-room") || strings.Contains(rows, "style=") {
		t.Fatal("the placeholder carries an inline style, which the page's content policy refuses")
	}
	answered := chat4Fixture("en-US", "answered", false)
	answered.PersonaActivityReady = true
	answered.AgentCardHeights = map[string]int{"question": 236}
	page := render(t, answered)
	start := strings.Index(page, `data-agent-reply-state="answered-private"`)
	if start < 0 {
		t.Fatal("the answered card is missing")
	}
	open := strings.LastIndex(page[:start], "<article")
	if tag := page[open : start+strings.Index(page[start:], ">")]; strings.Contains(tag, "reserved-height") || strings.Contains(tag, "style=") {
		t.Fatalf("the answered card carries a height of its own: %s", tag)
	}
}
