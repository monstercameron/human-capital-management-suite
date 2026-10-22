package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	xhtml "golang.org/x/net/html"
)

// TestTodo_CHATBUG_089_Browser opens the PTO thread the review found broken, in
// each language: the agent's answer carries the internal comments its writers
// leave after a source line and for a private reply, and a document card quotes
// a text that was cut mid-sentence. The page shows the sentences and none of the
// comments, and the card ends at a sentence with one ellipsis, offers "Show
// more" in the reader's language, and names the document rather than the person
// who asked.
func TestTodo_CHATBUG_089_Browser(t *testing.T) {
	const snippet = "Full-time employees accrue paid time off (PTO) every pay period. The accrual rate rises with tenure: 15 days a year in years one to three, 20 days a year in years four to seven, and 25 days after that..."
	const closed = "Full-time employees accrue paid time off (PTO) every pay period…"
	answer := "Employees may carry over up to 40 hours, see doc:d1 for the rules.\n\nSources\n" +
		"- 2026 holiday guide · v1.0.0 <!--chat.agent.source.readable:true-->\n" +
		"- Executive succession plan <!-- chat.agent.source.readable:false-->\n" +
		"<!--chat.agent.private:asked-->"
	words := map[string]struct{ more, less string }{
		"en-US": {"Show more", "Show less"},
		"de-DE": {"Mehr anzeigen", "Weniger anzeigen"},
		"ar":    {"عرض المزيد", "عرض أقل"},
	}
	for _, locale := range w7Locales {
		page := w7Page(t, locale, func(m *chatui.Model) {
			parent := chatui.Message{ID: "p1", AuthorID: "walt", Author: "Walt Brennan", Body: "How many PTO hours carry over? " + answer, Revision: 1, Replies: 1}
			m.Messages = []chatui.Message{parent}
			m.ShowThread, m.ThreadParentID, m.ThreadParent = true, "p1", &parent
			m.ThreadMessages = []chatui.Message{{ID: "r1", AuthorID: "jake", Author: "Jake Sullivan", Body: "Also in the thread: " + answer, Revision: 1}}
			m.EmbedOrigin = "https://hcm.example"
			m.DocPreviews = map[string]chatui.DocPreview{"d1": {ID: "d1", State: "ready", Readable: true, Title: "2026 holiday guide", Owner: "Walt Brennan", UpdatedAt: "Oct 1", Snippet: snippet}}
		})
		root := w7Parse(t, page)
		thread := w7One(t, locale+" thread panel", root, func(n *xhtml.Node) bool { return w7Class(n, "thread-pane") || w7Attr(n, "data-pane") == "thread" })
		shown := w7Text(thread)

		// No internal comment, flag or key is printed, in the markup or as text.
		for _, leak := range []string{"<!--", "-->", "&lt;!--", "chat.agent.", "readable:", "private:"} {
			if strings.Contains(page, leak) || strings.Contains(shown, leak) {
				t.Errorf("%s: the page prints %q", locale, leak)
			}
		}
		// The sentences around them stay, in the parent and in the reply.
		if strings.Count(shown, "carry over up to 40 hours") < 2 || !strings.Contains(shown, "2026 holiday guide") || !strings.Contains(shown, "Executive succession plan") {
			t.Errorf("%s: the thread lost the sentences around the comments: %s", locale, shown)
		}

		// The document card ends at a sentence with one ellipsis and offers the rest.
		bodies := w7All(thread, func(n *xhtml.Node) bool { return w7Class(n, "chat-embed-body") && !w7Class(n, "chat-embed-full") })
		if len(bodies) == 0 {
			t.Fatalf("%s: the thread draws no document card", locale)
		}
		for _, body := range bodies {
			if got := w7Text(body); got != closed {
				t.Errorf("%s: the card's closed text is %q, want %q", locale, got, closed)
			}
		}
		more := w7All(thread, func(n *xhtml.Node) bool { return n.Data == "details" && w7Class(n, "chat-embed-more") })
		if len(more) != len(bodies) {
			t.Fatalf("%s: %d cards but %d Show more disclosures", locale, len(bodies), len(more))
		}
		for _, disclosure := range more {
			text := w7Text(disclosure)
			if !strings.Contains(text, words[locale].more) || !strings.Contains(text, words[locale].less) {
				t.Errorf("%s: the card does not offer %q and %q: %s", locale, words[locale].more, words[locale].less, text)
			}
			if !strings.HasSuffix(text, "25 days after that…") || strings.Contains(text, "...") {
				t.Errorf("%s: Show more does not hold the whole text with one ellipsis: %s", locale, text)
			}
		}

		// The source of the card is the document, not the person who asked.
		sources := w7All(thread, func(n *xhtml.Node) bool { return w7Class(n, "chat-embed-source") })
		if len(sources) != len(bodies) {
			t.Fatalf("%s: %d cards but %d source lines", locale, len(bodies), len(sources))
		}
		for _, source := range sources {
			if got := w7Text(source); !strings.Contains(got, "2026 holiday guide") || strings.Contains(got, "Walt Brennan") {
				t.Errorf("%s: the card's source line is %q, want the document and not its owner", locale, got)
			}
		}
	}
}
