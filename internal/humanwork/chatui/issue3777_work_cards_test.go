//go:build !(js && wasm)

package chatui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// #3777: documents, polls and to-do lists are drawn in the same card as a
// journey or a ticket, and a poll or list that waits on the reader stands out
// from one that is only informational.
func TestIssue3777_DocumentCardIsTheSharedWorkCard(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale}
		markup := chatDesignRender(t, docPreviewCard(m, DocPreview{ID: "doc-1", Title: "Holiday guide", Readable: true, State: "ready", Owner: "Kim Park"}))
		for _, part := range []string{"chat-work-card", "chat-work-card-type"} {
			if !regexp.MustCompile(`class="[^"]*\b` + part + `(\s[^"]*)?"`).MatchString(markup) {
				t.Errorf("%s: the document card has no %s part: %s", locale, part, markup)
			}
		}
		if !strings.Contains(markup, "<svg") || !strings.HasPrefix(markup, "<a ") || strings.Contains(markup, "data-waits") {
			t.Errorf("%s: the document card needs its type icon, stays the link that opens it and is informational: %s", locale, markup)
		}
		loading := chatDesignRender(t, docPreviewCard(m, DocPreview{ID: "doc-1", State: "loading"}))
		if !strings.Contains(loading, "chat-work-card") {
			t.Errorf("%s: the loading document card is not the shared card: %s", locale, loading)
		}
	}
}

func TestIssue3777_PollAndTodoCardsStandOutWhenTheyWaitOnTheReader(t *testing.T) {
	poll := chat.Chatcmd002Card{Kind: "poll", Title: "Lunch?", Poll: &chat.Chatcmd002Poll{Results: "always", Options: []chat.ChannelPollOption{{ID: "a", Text: "Tacos"}, {ID: "b", Text: "Pizza"}}}}
	todo := chat.Chatcmd002Card{Kind: "todo", Title: "Launch", Todo: &chat.Chatcmd002Todo{Items: []chat.Chatcmd002Task{
		{ChannelTodoItem: chat.ChannelTodoItem{ID: "t1", Text: "Book the room"}, AssigneeID: "u-me"},
		{ChannelTodoItem: chat.ChannelTodoItem{ID: "t2", Text: "Send invites"}, AssigneeID: "u-kim"},
	}}}
	closedAt := time2026()
	closedPoll := poll
	closedPoll.ClosedAt = &closedAt
	for locale, waiting := range map[string]string{"en-US": "Waiting on you", "de-DE": "Wartet auf Sie", "ar": "بانتظارك"} {
		m := chatcmd003UIFixture()
		m.Locale, m.CurrentUser = locale, "u-me"
		m.Chatcmd002.Mutate = func(string, uint64, chat.Chatcmd002Mutation) {}
		render := func(view chat.Chatcmd002View, preview bool) string {
			return chatDesignRender(t, Chatcmd002RenderCard(m, "post", view, preview))
		}
		for name, c := range map[string]struct {
			markup string
			waits  bool
		}{
			"unvoted poll":       {render(chat.Chatcmd002View{Card: poll, ResultsVisible: true}, false), true},
			"voted poll":         {render(chat.Chatcmd002View{Card: poll, ResultsVisible: true, MyOptions: []string{"a"}}, false), false},
			"closed poll":        {render(chat.Chatcmd002View{Card: closedPoll, ResultsVisible: true}, false), false},
			"poll preview":       {render(chat.Chatcmd002View{Card: poll, ResultsVisible: true}, true), false},
			"outside audience":   {render(chat.Chatcmd002View{Card: poll, ResultsVisible: true, NotInAudience: true}, false), false},
			"task for reader":    {render(chat.Chatcmd002View{Card: todo, CanTick: map[string]bool{"t1": true}}, false), true},
			"task for others":    {render(chat.Chatcmd002View{Card: todo, CanTick: map[string]bool{"t2": true}}, false), false},
			"reader cannot tick": {render(chat.Chatcmd002View{Card: todo}, false), false},
		} {
			if !strings.Contains(c.markup, "chat-work-card") {
				t.Errorf("%s %s: not the shared card: %s", locale, name, c.markup)
			}
			marked := strings.Contains(c.markup, `data-waits="you"`)
			slot := strings.Contains(c.markup, `<span class="chat-work-card-action">`+waiting+`</span>`)
			named := strings.Contains(c.markup, ", "+waiting+`"`)
			if marked != c.waits || slot != c.waits || named != c.waits {
				t.Errorf("%s %s: marker=%v slot=%v name=%v, want all %v: %s", locale, name, marked, slot, named, c.waits, c.markup)
			}
		}
	}
}

func time2026() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
