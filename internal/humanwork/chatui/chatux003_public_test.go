package chatui

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

// chatux003PublicAnswer is the agent's answer posted under a question in the
// open thread, the way the server delivers it when it is visible to the channel.
func chatux003PublicAnswer(locale, viewer string) (Model, Message) {
	model := chat4Fixture(locale, "sent", false)
	model.PersonaInvocations = []PersonaThreadInvocation{{PostID: "question", Projection: PersonaProgressProjection{InvocationID: "run", ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper", DurablePostID: "answer"}}}
	actor := PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", InvokerHandle: "alice", Trusted: true}
	answer := Message{ID: "answer", AuthorID: "policy-helper", Author: "Policy Helper", Body: chatux003Body, SentAt: time.Now(), PersonaActor: &actor}
	model.PersonaPostActors = map[string]PersonaPostActor{"answer": {Display: "Policy Helper", Actor: actor}}
	model.Members = []Member{{ID: "alice", Name: "Alice Smith"}, {ID: "bob", Name: "Bob Jones"}}
	model.CurrentUser = viewer
	model.CurrentUserName = map[string]string{"alice": "Alice Smith", "bob": "Bob Jones"}[viewer]
	model.ShowThread, model.ThreadParentID = true, "question"
	model.ThreadMessages = []Message{model.Messages[0], answer}
	model.Callbacks.SubmitAgentFeedback = func(string, bool) {}
	if viewer != "alice" {
		// The server projects invocations to the person who made them only.
		model.PersonaInvocations = nil
	}
	return model, answer
}

// TestAgentUXPublicAnswer_Default_Browser renders an agent's answer that was
// posted to the channel: an ordinary message from the agent that says who asked,
// with the same compact source chips and the same rating controls as the private
// card, and none of the private card's chrome, for the asker and for a colleague,
// in each language and at each width.
func TestAgentUXPublicAnswer_Default_Browser(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		for viewer, name := range map[string]string{"alice": "Alice Smith", "bob": "Alice Smith"} {
			model, answer := chatux003PublicAnswer(locale, viewer)
			markup := chatPolishMarkup(t, message(model, handlers{}, answer, false), width, "light")
			articles := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "article" && chatPolishHasClass(n, "message") })
			if len(articles) != 1 {
				t.Fatalf("%s sees %d messages", viewer, len(articles))
			}
			card := articles[0]
			for _, private := range []string{"agent-reply-private", "chat-ephemeral", "agent-reply-why", "agent-reply-share"} {
				if len(chatux003Find(card, private)) != 0 {
					t.Fatalf("%s: a public answer carries the private card's %s", viewer, private)
				}
			}
			badge := chatux003Find(card, "agent-badge")
			if len(badge) != 1 || !strings.Contains(chatbug030Text(badge[0]), chatux003Format(model, "chatux003.asked_by", map[string]string{"name": name})) {
				t.Fatalf("%s: badge = %q, want it to say who asked (%s)", viewer, chatbug030Text(badge[0]), name)
			}
			chips := chatux003Find(card, "agent-reply-source")
			if len(chips) != 2 || len(chatux003Find(chips[0], "agent-reply-source-link")) != 1 || !chatPolishHasClass(chips[1], "agent-reply-source-locked") {
				t.Fatalf("%s: %d source chips, want a link and a locked chip", viewer, len(chips))
			}
			rating := chatux003Find(card, "agent-feedback")
			if viewer == "alice" && len(rating) != 1 || viewer == "bob" && len(rating) != 0 {
				t.Fatalf("%s: %d rating groups; only the person who asked rates", viewer, len(rating))
			}
		}
		// An asker the conversation does not know is not guessed at and never shown as an identifier.
		model, answer := chatux003PublicAnswer(locale, "bob")
		model.Members = nil
		unknown := chatPolishMarkup(t, message(model, handlers{}, answer, false), width, "light")
		if strings.Contains(unknown, chatux003Format(model, "chatux003.asked_by", map[string]string{"name": "alice"})) {
			t.Fatal("an unknown asker was shown by identifier")
		}
		// A top-level agent message (an announcement) keeps its own attribution.
		model.ThreadParentID, model.ThreadMessages = "", nil
		isBadge := func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-badge") }
		got := chatPolishNodes(t, chatPolishMarkup(t, message(model, handlers{}, answer, false), width, "light"), isBadge)
		want := chatPolishNodes(t, chatPolishMarkup(t, personaMessageBadge(model, answer.PersonaActor), width, "light"), isBadge)
		if len(got) != 1 || len(want) != 1 || chatbug030Text(got[0]) != chatbug030Text(want[0]) {
			t.Fatalf("a message that is not an answer changed its attribution: %q, want %q", chatbug030Text(got[0]), chatbug030Text(want[0]))
		}
	})
}
