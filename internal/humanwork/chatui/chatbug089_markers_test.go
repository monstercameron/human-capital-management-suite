package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// chatbug089Markers is every internal comment the writers of an agent answer
// put into a stored body, each in the shapes a surface meets it: whole after a
// source line, with a blank inside the comment, on a line of its own, and cut
// short by an excerpt.
var chatbug089Markers = map[string]string{
	"readable true":         "- 2026 holiday guide · v1.0.0 <!--chat.agent.source.readable:true-->",
	"readable false":        "- Executive succession plan <!--chat.agent.source.readable:false-->",
	"readable with a blank": "- 2026 holiday guide · v1.0.0 <!-- chat.agent.source.readable:true-->",
	"private asked":         "<!--chat.agent.private:asked-->",
	"private audience":      "<!--chat.agent.private:audience-->",
	"private agent":         "<!--chat.agent.private:agent-->",
	"cut readable":          "- 2026 holiday guide · v1.0.0 <!--chat.agent.source.read",
	"cut private":           "<!--chat.agent.priv",
}

// chatbug089Leaks is what no surface may print of them.
var chatbug089Leaks = []string{"<!--", "-->", "chat.agent.", "readable:", "private:"}

func chatbug089Shown(markup string) string {
	return stdhtml.UnescapeString(chatbug021Visible(markup))
}

func chatbug089Check(t *testing.T, where, shown string) {
	t.Helper()
	for _, leak := range chatbug089Leaks {
		if strings.Contains(shown, leak) {
			t.Errorf("%s prints %q: %s", where, leak, shown)
		}
	}
}

// TestTodo_CHATBUG_089 feeds every marker through every surface that draws a
// stored body or a stretch of one: the strip itself, the display and excerpt
// helpers, a message body, search rows, a Saved row, the thread panel (parent
// and reply), a pinned preview and a link card. A person's own text is left as
// it is.
func TestTodo_CHATBUG_089(t *testing.T) {
	for name, marker := range chatbug089Markers {
		body := "Employees may carry over up to 40 hours.\n\nSources\n" + marker + "\nSee the handbook."
		surfaces := map[string]string{
			"strip":           chatStripInternalMarkers(body),
			"chatDisplayBody": chatDisplayBody(body),
			"chatDisplayText": chatDisplayText(body),
			"chatExcerptText": chatExcerptText(body),
			"excerpt":         excerpt(body, 300),
			"searchSnippet":   searchSnippet(body, "hours", 300),
			"markdown body":   chatbug089Shown(renderNode(t, html.Div(html.Props{}, markdownMessageBody(Model{}, body)...))),
		}
		for _, kind := range []chatsearch.Kind{chatsearch.Message, chatsearch.Thread, chatsearch.Saved} {
			surfaces["search row "+string(kind)] = chatbug021SearchRow(t, kind, body)
		}
		// A saved row of a person's message, and of an agent's.
		saved := SavedMessageRow{TenantID: "tenant", ConversationID: "general", PostID: "p1", AuthorID: "walt", Author: "Walt Brennan", SentAt: chatsave002Now.Add(-time.Hour), Body: body, Availability: "readable", Sequence: 5, Revision: 1}
		surfaces["saved row"] = chatbug089Shown(renderNode(t, RenderSavedMessages(chatsave002View("todo", []SavedMessageRow{saved}))))

		// The thread panel: the parent and a reply both carry the marker.
		m := chatux022Model()
		parent := Message{ID: "p1", AuthorID: "walt", Author: "Walt Brennan", Body: body, Revision: 1, Replies: 1}
		m.Messages = []Message{parent}
		m.ShowThread, m.ThreadParentID, m.ThreadParent = true, "p1", &parent
		m.ThreadMessages = []Message{{ID: "r1", AuthorID: "walt", Author: "Walt Brennan", Body: body, Revision: 1}}
		surfaces["thread panel"] = chatbug089Shown(renderNode(t, threadPane(m, handlers{})))

		// A pinned preview and a forwarded-message card.
		surfaces["pinned preview"] = chatbug089Shown(renderNode(t, html.Div(html.Props{}, chatux019PinPreview(m, ChannelPin{PostID: "p1", Body: marker + "\nThe pinned line"})...)))
		for surface, shown := range surfaces {
			chatbug089Check(t, name+" / "+surface, shown)
			// A saved result carries the person's note as its first line, so its
			// text does not start with the sentence.
			if surface != "pinned preview" && surface != "search row saved" && !strings.Contains(shown, "carry over up to 40 hours") {
				t.Errorf("%s / %s lost the sentence: %s", name, surface, shown)
			}
		}
	}

	// A person's own words, comment-like text included, are never altered.
	for _, own := range []string{"Use <!-- a note --> freely.", "chat.agent.source is a name I use", "Sources\n- a book"} {
		if got := chatStripInternalMarkers(own); got != own {
			t.Errorf("a person's text %q became %q", own, got)
		}
		if got := chatDisplayBody(own); got != own {
			t.Errorf("chatDisplayBody changed %q to %q", own, got)
		}
	}

	// The stored body is not changed; only what is drawn.
	stored := "Answer.\n\nSources\n- X <!--chat.agent.source.readable:true-->"
	if got := chatStripInternalMarkers(stored); got != "Answer.\n\nSources\n- X" {
		t.Errorf("the strip left %q", got)
	}
}

// TestTodo_CHATBUG_089_SearchAnnouncement: a search row whose text is a whole
// stored announcement shows its sentence, not the envelope (the page served the
// envelope because only a cut one was unwrapped).
func TestTodo_CHATBUG_089_SearchAnnouncement(t *testing.T) {
	whole := "<hcm_agent_announcement>\n" + `{"AgentName":"Agent","OwnerName":"Walt Brennan","Text":"Upcoming company holidays remaining in 2026:\n- Labor Day — Sep 7\n(2026 holiday guide)","Scheduled":false,"PostedAt":"0001-01-01T00:00:00Z","Sources":[{"Title":"2026 holiday guide","Href":"/workspace/app/docs?document=d"}]}`
	for _, kind := range []chatsearch.Kind{chatsearch.Message, chatsearch.Thread} {
		shown := chatbug021SearchRow(t, kind, whole)
		chatbug021Clean(t, "whole announcement in a "+string(kind)+" row", shown)
		// The matched word is highlighted, so the words are compared apart from it.
		if flat := strings.Join(strings.Fields(shown), " "); !strings.Contains(flat, "Upcoming company holidays remaining in 2026") {
			t.Errorf("the %s row lost the sentence: %s", kind, shown)
		}
	}
	saved := SavedMessageRow{TenantID: "tenant", ConversationID: "general", PostID: "p1", AuthorID: "assistant", Author: "Assistant", SentAt: chatsave002Now.Add(-time.Hour), Body: whole, Availability: "readable", Sequence: 5, Revision: 1}
	shown := chatbug089Shown(renderNode(t, RenderSavedMessages(chatsave002View("todo", []SavedMessageRow{saved}))))
	chatbug021Clean(t, "Saved row of an announcement", shown)
	if !strings.Contains(shown, "Upcoming company holidays remaining in 2026") {
		t.Errorf("the Saved row lost the sentence: %s", shown)
	}
}
