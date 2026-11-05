package chatui_test

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatux039ThreadSources is the body used for the thread and the conversation
// copies of one answer: two document sources under a Sources heading.
func chatux039ThreadSources() string {
	link := func(title, id string) string { return "[" + title + "](/workspace/app/docs?document=" + id + ")" }
	return "Paid time off policy.\n\nSources\n\n- " + link("Paid time off policy · Carryover · v1.0.0", "pto") + "\n- " + link("2026 holiday guide · v1.0.0", "hol")
}

// chatux039ChipHrefs reads the linked documents of every source chip under n,
// in order. Agent answers mark their chip links; chips from copied text carry
// plain links.
func chatux039ChipHrefs(n *xhtml.Node) []string {
	var hrefs []string
	for _, chip := range w7All(n, func(c *xhtml.Node) bool { return w7Class(c, "agent-reply-source") }) {
		for _, link := range w7All(chip, func(c *xhtml.Node) bool { return c.Data == "a" }) {
			hrefs = append(hrefs, w7Attr(link, "href"))
		}
	}
	return hrefs
}

// TestTodo_CHATUX_039_Browser renders the thread pane in each language with an
// agent answer, a copied answer and a linked document beside the same answer
// in the conversation: thread sources are the conversation's chips, one per
// source, never a Markdown list; the document card is neutral and shares no
// agent-answer component; thread and conversation text share the message-body
// component with no thread-only size.
func TestTodo_CHATUX_039_Browser(t *testing.T) {
	sources := chatux039ThreadSources()
	agentBody := "Carryover is capped at five days.\n\nSources\n\n- [Paid time off policy](/workspace/app/docs?document=pto)\n- [2026 holiday guide](/workspace/app/docs?document=hol)"
	for _, locale := range w7Locales {
		parent := chatui.Message{ID: "p1", AuthorID: "walt", Author: "Walt Brennan", Body: "What is the carryover policy?", Revision: 1, Replies: 3}
		page := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) {
			m.Messages = []chatui.Message{parent,
				{ID: "m1", AuthorID: "jake", Author: "Jake Sullivan", Body: sources, Revision: 1, TimeLabel: "9:31"}}
			m.ShowThread, m.ThreadParentID, m.ThreadParent = true, "p1", &parent
			m.ThreadMessages = []chatui.Message{
				{ID: "r1", AuthorID: "policy-helper", Author: "Policy Helper", Body: agentBody, Revision: 1, TimeLabel: "9:32"},
				{ID: "r2", AuthorID: "jake", Author: "Jake Sullivan", Body: "doc:pto has the carryover details", Revision: 1, TimeLabel: "9:33"},
				{ID: "r0", AuthorID: "jake", Author: "Jake Sullivan", Body: sources, Revision: 1, TimeLabel: "9:34"},
			}
			m.PersonaPostActors = map[string]chatui.PersonaPostActor{
				"r1": {Actor: chatui.PersonaActor{PersonaID: "policy-helper", AgentID: "agent-1", Trusted: true}, Display: "Policy Helper"},
			}
			m.DocPreviews = map[string]chatui.DocPreview{
				"pto": {ID: "pto", Title: "Paid time off policy", Collection: "Benefits", UpdatedAt: "Oct 1", Readable: true, State: "ready"},
			}
			m.Callbacks.CloseThread = func() {}
		}))
		thread := w7One(t, locale+" thread pane", page, func(n *xhtml.Node) bool { return w7Class(n, "thread-pane") })

		// The agent's thread answer carries the conversation's Sources chips.
		agent := w7One(t, locale+" agent thread reply", thread, func(n *xhtml.Node) bool {
			return w7Class(n, "thread-message") && w7Attr(n, "data-message-id") == "r1"
		})
		agentSources := w7One(t, locale+" agent thread sources", agent, func(n *xhtml.Node) bool { return w7Class(n, "agent-reply-sources") })
		if got := chatux039ChipHrefs(agentSources); len(got) != 2 || got[0] != "/workspace/app/docs?document=pto" || got[1] != "/workspace/app/docs?document=hol" {
			t.Errorf("%s: the agent thread answer links %v, want one chip per source in order", locale, got)
		}

		// A copied answer in the thread is chips too, never a Markdown list.
		copied := w7One(t, locale+" copied thread reply", thread, func(n *xhtml.Node) bool {
			return w7Class(n, "thread-message") && w7Attr(n, "data-message-id") == "r0"
		})
		copiedBody := w7One(t, locale+" copied thread reply text", copied, func(n *xhtml.Node) bool { return w7Class(n, "message-body") })
		if chips := w7All(copiedBody, func(n *xhtml.Node) bool { return w7Class(n, "md-source-chips") }); len(chips) != 1 {
			t.Errorf("%s: the copied thread answer holds %d chip lists, want one", locale, len(chips))
		}
		threadHrefs := chatux039ChipHrefs(copiedBody)
		if len(threadHrefs) != 2 {
			t.Errorf("%s: the copied thread answer holds %d source chips, want one per source", locale, len(threadHrefs))
		}
		// The chip list is the only list, and every item is a chip: no plain
		// Markdown bullets survive.
		if bare := w7All(copiedBody, func(n *xhtml.Node) bool { return n.Data == "ul" && !w7Class(n, "md-source-chips") }); len(bare) != 0 {
			t.Errorf("%s: the copied thread answer still holds %d plain bulleted lists", locale, len(bare))
		}
		for _, item := range w7All(copiedBody, func(n *xhtml.Node) bool { return n.Data == "li" }) {
			if !w7Class(item, "agent-reply-source") {
				t.Errorf("%s: a copied source is not a chip: %s", locale, w7Text(item))
			}
		}

		// The same answer in the conversation renders the same chips.
		timeline := w7One(t, locale+" timeline", page, func(n *xhtml.Node) bool { return w7Class(n, "message-list") })
		var conversationHrefs []string
		for _, body := range w7All(timeline, func(n *xhtml.Node) bool { return w7Class(n, "message-body") }) {
			if chips := w7All(body, func(n *xhtml.Node) bool { return w7Class(n, "md-source-chips") }); len(chips) == 1 {
				conversationHrefs = chatux039ChipHrefs(body)
			}
		}
		if len(conversationHrefs) != 2 {
			t.Errorf("%s: the conversation copy holds %d source chips, want one per source", locale, len(conversationHrefs))
		} else if strings.Join(conversationHrefs, ",") != strings.Join(threadHrefs, ",") {
			t.Errorf("%s: thread chips %v are not the conversation chips %v", locale, threadHrefs, conversationHrefs)
		}

		// The document card is neutral: it shares no agent-answer component,
		// and sits outside any agent container.
		card := w7One(t, locale+" document card", thread, func(n *xhtml.Node) bool { return w7Class(n, "chat-doc-embed") })
		if card.Data != "a" || w7Attr(card, "href") != "/workspace/app/docs?document=pto" {
			t.Errorf("%s: the document card is a %s to %q, want a link to the document", locale, card.Data, w7Attr(card, "href"))
		}
		for _, class := range []string{"agent-answer", "agent-reply-row", "agent-reply-answer", "agent-reply-sources", "chat-ephemeral"} {
			if w7Class(card, class) {
				t.Errorf("%s: the document card carries the agent class %s", locale, class)
			}
			if w7Inside(card, func(p *xhtml.Node) bool { return w7Class(p, class) }) {
				t.Errorf("%s: the document card sits inside %s", locale, class)
			}
		}

		// Thread and conversation text share the message-body component.
		var threadBodies, timelineBodies int
		for _, body := range w7All(thread, func(n *xhtml.Node) bool { return w7Class(n, "message-body") }) {
			threadBodies++
			if w7Attr(body, "class") != "message-body" {
				t.Errorf("%s: a thread text block carries %q, want the shared message-body", locale, w7Attr(body, "class"))
			}
		}
		for range w7All(timeline, func(n *xhtml.Node) bool { return w7Class(n, "message-body") }) {
			timelineBodies++
		}
		if threadBodies == 0 || timelineBodies == 0 {
			t.Errorf("%s: thread texts=%d conversation texts=%d, want both rendered", locale, threadBodies, timelineBodies)
		}
	}

	// No thread-only text size: thread body selectors carry no font-size rule.
	sheet := chatui.ScopedStylesheet()
	for _, selector := range []string{".thread-root-body", ".thread-message-body", ".thread-replies", ".thread-pane .message-body", ".thread-root .message-body", ".thread-message .message-body"} {
		for start := 0; start < len(sheet); {
			idx := strings.Index(sheet[start:], selector)
			if idx < 0 {
				break
			}
			idx += start
			end := strings.Index(sheet[idx:], "}")
			if end < 0 {
				break
			}
			if block := sheet[idx : idx+end]; strings.Contains(block, "font-size") {
				t.Errorf("a thread text rule sets its own size: %.120q", block)
			}
			start = idx + end
		}
	}
}
