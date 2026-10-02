package chatui

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

// chatux026Card renders the private answer card of the fixture's question, with
// the page's local state as given.
func chatux026Card(t *testing.T, locale string, local localUI, configure func(*Model)) (*xhtml.Node, string) {
	t.Helper()
	model := chat4Fixture(locale, "answered", false)
	model.EphemeralMessages[0].Body = chatux003Body + "\n<!--chat.agent.private:asked-->"
	model.Conversations[0].MemberCount = 18
	model.Callbacks.ShareAgentAnswer = func(string) {}
	model.Callbacks.RemoveSharedAgentAnswer = func(string) {}
	model.Callbacks.OpenThread = func(string) {}
	// CHATUX-028: what a shared answer adds is in the "…" menu; the card is drawn
	// with it open so a test can read the items.
	model.Callbacks.OpenMenu = func(string) {}
	model.MenuID = "agent-card:answer"
	model.PersonaPostActors = map[string]PersonaPostActor{"answer": {Display: "Policy Helper", Actor: PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}}}
	if configure != nil {
		configure(&model)
	}
	rows := personaReplyRowsForPost(model, local, "question", time.Now())
	if len(rows) != 1 {
		t.Fatalf("%s: %d answer rows", locale, len(rows))
	}
	markup := chatPolishMarkup(t, rows[0], 1440, "light")
	cards := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "article" && chatPolishHasClass(n, "chat-ephemeral") })
	if len(cards) != 1 {
		t.Fatalf("%s: %d answer cards", locale, len(cards))
	}
	return cards[0], chatbug030Text(cards[0])
}

// chatux026Disabled reports whether a control carries the disabled attribute.
func chatux026Disabled(n *xhtml.Node) bool {
	for _, attr := range n.Attr {
		if attr.Key == "disabled" {
			return true
		}
	}
	return false
}

func chatux026Actions(card *xhtml.Node) []string {
	var actions []string
	for _, node := range chatPolishNodesIn(card, func(n *xhtml.Node) bool {
		return n.Data == "button" && strings.HasPrefix(chatPolishAttr(n, "data-action"), "agent-share")
	}) {
		actions = append(actions, chatPolishAttr(node, "data-action"))
	}
	return actions
}

// The first press asks before anything is posted: what, to whom and where, with
// Share and Cancel. Nothing is sent until Share is pressed, and Cancel leaves
// the card as it was.
func TestTodo_CHATUX_026(t *testing.T) {
	// Before any press: the one control, and the card is private.
	card, text := chatux026Card(t, "en-US", localUI{}, nil)
	// AGENTUX-070: the control is the mark that says the answer is private and, with
	// the card's menu open as this fixture has it, the menu's "Share to channel".
	if got := chatux026Actions(card); len(got) != 2 || got[0] != "agent-share" || got[1] != "agent-share" || len(chatux003Find(card, "agent-reply-private-button")) != 1 || len(chatux003Find(card, "agent-share-menu")) != 1 || !strings.Contains(text, "Only visible to you") || len(chatux003Find(card, "agent-share-confirm")) != 0 {
		t.Fatalf("an untouched card offers %v and reads %q", got, text)
	}

	// The first press: the question, in place of the control.
	card, text = chatux026Card(t, "en-US", localUI{agentShareAsk: "run"}, nil)
	confirm := chatux003Find(card, "agent-share-confirm")
	if len(confirm) != 1 || chatPolishAttr(confirm[0], "role") != "group" || chatPolishAttr(confirm[0], "aria-label") != "Share this answer with the channel?" {
		t.Fatalf("the first press does not ask: %q", text)
	}
	for _, want := range []string{"Share this answer with the channel?", "What is posted", "This answer and its sources", "Who can read it", "Everyone in ⁨#general⁩, 18 people", "Where", "Under your question, as a message from you", "Share", "Cancel", "Only visible to you"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the question does not say %q: %q", want, text)
		}
	}
	if got := chatux026Actions(card); len(got) != 2 || got[0] != "agent-share-confirm" || got[1] != "agent-share-cancel" {
		t.Fatalf("the question offers %v, want Share then Cancel", got)
	}
	if strings.Contains(text, "Shared with") || len(chatux003Find(card, "agent-reply-share")) != 0 {
		t.Fatalf("the card shares, or still shows its first control, while it asks: %q", text)
	}
	// The count follows what the page knows about the channel.
	_, text = chatux026Card(t, "en-US", localUI{agentShareAsk: "run"}, func(m *Model) { m.Conversations[0].MemberCount = 1 })
	if !strings.Contains(text, "Everyone in ⁨#general⁩, 1 person") {
		t.Fatalf("one member: %q", text)
	}
	_, text = chatux026Card(t, "en-US", localUI{agentShareAsk: "run"}, func(m *Model) { m.Conversations[0].MemberCount = 0 })
	if !strings.Contains(text, "Everyone in ⁨#general⁩") || strings.Contains(text, "people") || strings.Contains(text, "person") {
		t.Fatalf("an unknown number of members: %q", text)
	}
	// Another card's question is not this card's.
	if card, _ = chatux026Card(t, "en-US", localUI{agentShareAsk: "another-run"}, nil); len(chatux003Find(card, "agent-share-confirm")) != 0 {
		t.Fatal("a card asks about another card's answer")
	}

	// What each press does, without a browser: only Share after the question sends.
	shared, removed, opened := []string{}, []string{}, []string{}
	model := chat4Fixture("en-US", "answered", false)
	model.Callbacks.ShareAgentAnswer = func(id string) { shared = append(shared, id) }
	model.Callbacks.RemoveSharedAgentAnswer = func(id string) { removed = append(removed, id) }
	model.Callbacks.OpenThread = func(id string) { opened = append(opened, id) }
	step := func(asking, action, id string) (string, string) {
		t.Helper()
		next, effect, taken := chatux026Step(model, asking, action, id)
		if !taken {
			t.Fatalf("%s was not taken as a share control", action)
		}
		return next, effect
	}
	if next, effect := step("", "agent-share", "run"); next != "run" || effect != "ask" {
		t.Fatalf("the first press = %q %q, want the question", next, effect)
	}
	if next, effect := step("run", "agent-share-cancel", "run"); next != "" || effect != "cancel" {
		t.Fatalf("Cancel = %q %q", next, effect)
	}
	if next, effect := step("run", "agent-share-confirm", "run"); next != "" || effect != "share" {
		t.Fatalf("Share = %q %q", next, effect)
	}
	// A confirmation nobody was asked for shares nothing.
	if next, effect := step("", "agent-share-confirm", "run"); next != "" || effect != "" {
		t.Fatalf("a confirmation with no question = %q %q", next, effect)
	}
	if next, effect := step("another-run", "agent-share-confirm", "run"); next != "" || effect != "" {
		t.Fatalf("a confirmation of another card's question = %q %q", next, effect)
	}
	if _, _, taken := chatux026Step(model, "", "agent-follow-up", "run"); taken {
		t.Fatal("another action was taken as a share control")
	}
	// A request on its way, a shared answer and a refusal ask nothing again.
	for _, status := range []AgentShareStatus{AgentShareSharing, AgentShareShared, AgentShareRemoving, AgentShareRefused} {
		model.AgentShare = map[string]AgentShareState{"run": {Status: status, PostID: "copy"}}
		if next, effect := step("", "agent-share", "run"); next != "" || effect != "" {
			t.Fatalf("%s: the share control asks again (%q %q)", status, next, effect)
		}
		if chatux026Asking(model, localUI{agentShareAsk: "run"}, "run") {
			t.Fatalf("%s: the card still asks", status)
		}
	}
	// A failed request may be asked again.
	model.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareFailed}}
	if next, effect := step("", "agent-share", "run"); next != "run" || effect != "ask" {
		t.Fatalf("after a failure the control = %q %q", next, effect)
	}
	// View and Remove act only on a shared answer that names its copy.
	model.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareShared, PostID: "copy"}}
	if _, effect := step("", "agent-share-view", "question"); effect != "view" {
		t.Fatalf("View = %q", effect)
	}
	if _, effect := step("", "agent-share-remove", "run"); effect != "remove" {
		t.Fatalf("Remove = %q", effect)
	}
	model.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareRemoving, PostID: "copy"}}
	if _, effect := step("", "agent-share-remove", "run"); effect != "" {
		t.Fatal("Remove can be pressed twice")
	}
	model.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareShared}}
	if _, effect := step("", "agent-share-remove", "run"); effect != "" {
		t.Fatal("Remove acts on a shared answer whose copy is not known")
	}
	if len(shared)+len(removed)+len(opened) != 0 {
		t.Fatal("deciding what a press does called the server")
	}
	// Leaving the conversation ends the question.
	store := localStore{box: &localUI{room: "general", agentShareAsk: "run"}}
	store.forRoom("general")
	if store.box.agentShareAsk != "run" {
		t.Fatal("a render in the same conversation ended the question")
	}
	store.forRoom("random")
	if store.box.agentShareAsk != "" {
		t.Fatal("the question followed the person to another conversation")
	}
	if got := chatux026Quoted(`a"b\c`); got != `a\"b\\c` {
		t.Fatalf("selector quoting = %q", got)
	}
}

// After sharing, the card says who it is shared with in place of "Only visible
// to you", leads to the shared copy and lets the asker remove it; removed, the
// card is private again. A refusal keeps the card private and says why.
func TestTodo_CHATUX_026_Browser(t *testing.T) {
	headers := map[string]string{"en-US": "Shared with ⁨#general⁩", "de-DE": "Geteilt mit ⁨#general⁩", "ar": "تمت المشاركة مع ⁨#general⁩"}
	for locale, header := range headers {
		card, text := chatux026Card(t, locale, localUI{agentShareAsk: "run"}, func(m *Model) {
			m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareShared, PostID: "copy"}}
		})
		private := agentReplyFallback(locale, "chat.agent.only_visible", "Only visible to you")
		note := chatux003Find(card, "agent-reply-private")
		if len(note) != 1 || chatbug030Text(note[0]) != header || chatPolishAttr(note[0], "data-visibility") != "shared" || strings.Contains(text, private) {
			t.Fatalf("%s: the shared card's header reads %q", locale, text)
		}
		if chatPolishAttr(card, "data-agent-answer-visibility") != "shared" {
			t.Fatalf("%s: the card is not marked shared", locale)
		}
		// The reason it was private is gone with the privacy.
		if len(chatux003Find(card, "agent-reply-why")) != 0 || len(chatux003Find(card, "agent-share-confirm")) != 0 || len(chatux003Find(card, "agent-reply-share")) != 0 {
			t.Fatalf("%s: a shared card still explains its privacy, asks, or offers to share: %q", locale, text)
		}
		if got := chatux026Actions(card); len(got) != 2 || got[0] != "agent-share-view" || got[1] != "agent-share-remove" {
			t.Fatalf("%s: the shared card offers %v, want View then Remove", locale, got)
		}
		view := chatux003Find(card, "agent-share-view")[0]
		if chatPolishAttr(view, "data-id") != "question" || chatPolishAttr(view, "data-extra") != "copy" {
			t.Fatalf("%s: View does not lead to the copy under the question", locale)
		}
		if strings.Contains(text, "{channel}") || strings.Contains(text, "chatux026") || strings.Contains(text, "⟦") {
			t.Fatalf("%s: a key or placeholder is on the card: %q", locale, text)
		}
	}
	_, text := chatux026Card(t, "en-US", localUI{}, func(m *Model) {
		m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareShared, PostID: "copy"}}
	})
	if !strings.Contains(text, "View shared answer") || !strings.Contains(text, "Remove shared answer") {
		t.Fatalf("the shared card reads %q", text)
	}

	// Removing: the control waits; a failure says so and the copy is still shared.
	card, text := chatux026Card(t, "en-US", localUI{}, func(m *Model) {
		m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareRemoving, PostID: "copy"}}
	})
	remove := chatux003Find(card, "agent-share-remove")
	if len(remove) != 1 || !chatux026Disabled(remove[0]) || !strings.Contains(text, "Removing…") || !strings.Contains(text, "Shared with") {
		t.Fatalf("while the copy is removed the card reads %q", text)
	}
	_, text = chatux026Card(t, "en-US", localUI{}, func(m *Model) {
		m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareShared, PostID: "copy", RemoveFailed: true}}
	})
	if !strings.Contains(text, "Could not remove the shared answer. Try again.") || !strings.Contains(text, "Shared with") {
		t.Fatalf("a failed removal reads %q", text)
	}
	// Removed: the card is the private card again, with its control and its reason.
	card, text = chatux026Card(t, "en-US", localUI{}, func(m *Model) { m.AgentShare = map[string]AgentShareState{"run": {}} })
	if got := chatux026Actions(card); len(got) != 2 || got[0] != "agent-share" || got[1] != "agent-share" || !strings.Contains(text, "Only visible to you") || !strings.Contains(text, "You asked for this answer to stay private.") {
		t.Fatalf("after removal the card offers %v and reads %q", got, text)
	}
	// A shared answer whose copy the page cannot name offers no removal.
	card, _ = chatux026Card(t, "en-US", localUI{}, func(m *Model) { m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareShared}} })
	if got := chatux026Actions(card); len(got) != 1 || got[0] != "agent-share-view" {
		t.Fatalf("a shared answer with no known copy offers %v", got)
	}

	// A refusal keeps the card private and says why; nothing asks again.
	card, text = chatux026Card(t, "en-US", localUI{agentShareAsk: "run"}, func(m *Model) {
		m.AgentShare = map[string]AgentShareState{"run": {Status: AgentShareRefused, Reason: "audience", Source: "Executive succession plan"}}
	})
	if !strings.Contains(text, "Only visible to you") || !strings.Contains(text, "Executive succession plan") || !strings.Contains(text, "so this stays private.") || len(chatux026Actions(card)) != 0 {
		t.Fatalf("a refused share reads %q", text)
	}
	// In a direct conversation with the agent nothing is ever shared.
	direct := chat4Fixture("en-US", "answered", true)
	direct.Callbacks.ShareAgentAnswer = func(string) {}
	if nodes := chatux026ShareControls(direct, localUI{agentShareAsk: "run"}, "", "Policy Helper", "run", "question"); len(nodes) != 0 {
		t.Fatal("a direct conversation offers to share")
	}
	for _, want := range []string{".agent-share-confirm{flex:1 0 100%", ".agent-share-confirm dl{display:grid", ".agent-share-confirm-actions{display:flex"} {
		if !strings.Contains(Stylesheet, want) {
			t.Fatalf("the question's styles are missing %q", want)
		}
	}
}
