package chatui

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

// chatux016Assistant is the direct conversation with the Assistant, whose
// description is long enough to be cut in the header.
func chatux016Assistant(locale string) Model {
	const purpose = "Answers company questions and lists policies from readable conversation and workspace documents, with citations."
	room := Conversation{ID: "assistant", Name: "Assistant", Kind: DirectMessage, Agent: true, AgentID: "assistant", AgentPurpose: purpose, Joined: true}
	ref := ChatReference{Kind: "AGENT_MENTION", ID: "assistant", TenantID: "tenant", Display: "Assistant", ConversationID: "assistant"}
	return Model{State: StateEmpty, Locale: locale, CurrentUser: "alice", CurrentUserName: "Alice", CurrentTenantID: "tenant", SelectedID: "assistant",
		Conversations:           []Conversation{room},
		ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: ref, Purpose: purpose, Owner: "walt", DataClasses: []string{"WORKSPACE_DOCUMENT"}, CannotDo: []string{"Act beyond your current access", "Use skills outside this published version", "Change governed records", "Write to external systems"}, ReplyPlacement: PersonaReplyPrivateAlways}},
		PersonaLookup:           PersonaLookupReady, PersonaLookupConversationID: "assistant",
		Callbacks: Callbacks{SendMessage: func(string, string) {}, SendMessageWithReferences: func(string, string, []ChatReference) {}, SelectConversation: func(string) {}, ToggleDetails: func(bool) {}},
	}
}

// The header of a conversation with an agent shows the name, the Agent badge
// and a short privacy mark that is never cut; the description is one line of
// its own that may be cut, with its full text one hover away.
func TestTodo_CHATUX_016(t *testing.T) {
	marks := map[string]string{"en-US": "Private to you", "de-DE": "Privat für Sie", "ar": "خاص بك"}
	for locale, mark := range marks {
		m := chatux016Assistant(locale)
		markup := chatPolishMarkup(t, Build(m), 1440, "light")
		lines := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-identity-line") })
		if len(lines) != 1 {
			t.Fatalf("%s: %d identity lines in the header", locale, len(lines))
		}
		parts := chatux003Elements(lines[0])
		if len(parts) != 3 || parts[0].Data != "h1" || !chatPolishHasClass(parts[1], "agent-badge") || !chatPolishHasClass(parts[2], "agent-header-private") {
			t.Fatalf("%s: the identity line is not name, badge, privacy mark", locale)
		}
		if text := chatbug030Text(parts[2]); !strings.Contains(text, mark) {
			t.Fatalf("%s: the privacy mark reads %q, want %q", locale, text, mark)
		}
		purpose := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-header-purpose") })
		if len(purpose) != 1 || chatPolishAttr(purpose[0], "title") != m.Conversations[0].AgentPurpose || chatbug030Text(purpose[0]) != m.Conversations[0].AgentPurpose {
			t.Fatalf("%s: the description line is missing or lost its full text", locale)
		}
		// The privacy sentence is not appended to the description any more.
		if strings.Contains(chatbug030Text(purpose[0]), "·") || strings.Contains(chatbug030Text(purpose[0]), mark) {
			t.Fatalf("%s: the description line carries the privacy note: %q", locale, chatbug030Text(purpose[0]))
		}
	}
	// The mark cannot shrink or wrap; the name and the description give way.
	for _, want := range []string{
		".agent-header-private{display:inline-flex;align-items:center;gap:4px;flex:none;white-space:nowrap",
		".conversation-topic.agent-header-purpose{display:block;max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}",
		".agent-identity-line h1{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}",
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Fatalf("header styles are missing %q", want)
		}
	}
	// Before the description is known the header still says who can see it.
	m := chatux016Assistant("en-US")
	m.Conversations[0].AgentPurpose, m.ResolvedPersonaMentions = "", nil
	markup := chatPolishMarkup(t, Build(m), 390, "light")
	if !strings.Contains(markup, "Private to you") || strings.Contains(markup, "agent-header-purpose") {
		t.Fatal("a header with no description yet lost its privacy mark or drew an empty line")
	}
}

// The full description is in Conversation details, uncut.
func TestTodo_CHATUX_016_Browser(t *testing.T) {
	m := chatux016Assistant("en-US")
	m.ShowDetails = true
	markup := chatPolishMarkup(t, Build(m), 1440, "light")
	summaries := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-summary-purpose") })
	if len(summaries) == 0 || chatbug030Text(summaries[0]) != m.Conversations[0].AgentPurpose {
		t.Fatal("Conversation details does not hold the agent's full description")
	}
}

// An empty conversation with an agent says what the agent is for and offers
// three example questions; pressing one fills the message box and never sends;
// the box says "Ask <name>" until the agent has answered.
func TestTodo_CHATUX_024(t *testing.T) {
	asks := map[string]string{"en-US": "Ask Assistant", "de-DE": "Assistant fragen", "ar": "اسأل Assistant"}
	for locale, ask := range asks {
		m := chatux016Assistant(locale)
		markup := chatPolishMarkup(t, Build(m), 1440, "light")
		panel := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chatux024-empty") })
		if len(panel) != 1 {
			t.Fatalf("%s: %d empty-agent panels", locale, len(panel))
		}
		text := chatbug030Text(panel[0])
		if !strings.Contains(text, ask) || !strings.Contains(text, m.Conversations[0].AgentPurpose) {
			t.Fatalf("%s: the empty conversation does not name the agent and its purpose: %q", locale, text)
		}
		examples := chatPolishNodesIn(panel[0], func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-action") == "agent-example"
		})
		if len(examples) != 3 {
			t.Fatalf("%s: %d example questions, want 3", locale, len(examples))
		}
		for _, example := range examples {
			if chatPolishAttr(example, "data-extra") == "" || strings.TrimSpace(chatPolishAttr(example, "data-extra")) != chatbug030Text(example) || chatPolishAttr(example, "type") != "button" {
				t.Fatalf("%s: an example does not carry its own text: %q", locale, chatbug030Text(example))
			}
		}
		if !strings.Contains(markup, `placeholder="`+ask+`"`) {
			t.Fatalf("%s: the message box does not say %q", locale, ask)
		}
		followUp := strings.ReplaceAll(agentReplyFallback(locale, "chat.agent.follow_up", "Ask {name} a follow-up"), "{name}", "Assistant")
		for _, old := range []string{"Start the conversation", "There are no messages here yet", followUp} {
			if strings.Contains(markup, old) {
				t.Fatalf("%s: the empty agent conversation still says %q", locale, old)
			}
		}
		if strings.Contains(markup, "{name}") || strings.Contains(markup, "⟦") {
			t.Fatalf("%s: a placeholder or a key is on the page", locale)
		}
	}

	// Pressing an example fills the box and never sends.
	m := chatux016Assistant("en-US")
	sent, drafted := 0, ""
	m.Callbacks.SendMessage = func(string, string) { sent++ }
	m.Callbacks.SendMessageWithReferences = func(string, string, []ChatReference) { sent++ }
	m.Callbacks.DraftChanged = func(conversation, value string) { drafted = conversation + "|" + value }
	example := chatux024Examples(m, m.Conversations[0])[0]
	chatux024UseExample(m, example)
	if sent != 0 || drafted != "assistant|"+example {
		t.Fatalf("pressing an example sent %d messages and drafted %q", sent, drafted)
	}
	chatux024UseExample(m, "   ")
	if sent != 0 || drafted != "assistant|"+example {
		t.Fatal("an empty example changed the draft")
	}

	// The examples follow what the agent can read.
	general := chatux024Examples(m, m.Conversations[0])
	policy := chatux016Assistant("en-US")
	policy.ResolvedPersonaMentions[0].DataClasses = []string{"POLICY_DOCUMENT"}
	if got := chatux024Examples(policy, policy.Conversations[0]); got[0] == general[0] || !strings.Contains(strings.ToLower(got[0]), "policy") {
		t.Fatalf("an agent that reads policy documents offers %q", got)
	}

	// Once the agent has answered, the box offers a follow-up.
	answered := chatux016Assistant("en-US")
	answered.State = StateReady
	answered.Messages = []Message{{ID: "q", AuthorID: "alice", Author: "Alice", Body: "Hello", SentAt: time.Now()}, {ID: "a", AuthorID: "assistant", Author: "Assistant", Body: "Hello.", SentAt: time.Now()}}
	if got := chatux024Placeholder(answered, "Assistant"); got != "Ask Assistant a follow-up" {
		t.Fatalf("after an answer the box says %q", got)
	}
	answered.Messages = answered.Messages[:1]
	if got := chatux024Placeholder(answered, "Assistant"); got != "Ask Assistant" {
		t.Fatalf("before any answer the box says %q", got)
	}

	// An ordinary empty conversation keeps its own words.
	channel := chatux016Assistant("en-US")
	channel.Conversations[0] = Conversation{ID: "assistant", Name: "general", Kind: PublicChannel, Joined: true}
	if markup := chatPolishMarkup(t, Build(channel), 1440, "light"); !strings.Contains(markup, "Start the conversation") || strings.Contains(markup, "chatux024-empty") {
		t.Fatal("an empty channel was given the agent's empty state")
	}
}

// The key hint follows one rule in every composer: it shows until the person
// has sent three messages on this page, whatever conversation is open.
func TestTodo_CHATUX_024_Browser(t *testing.T) {
	quiet := chatux016Assistant("en-US")
	busy := chat4Fixture("en-US", "sent", false)
	for i := 0; i < 5; i++ {
		busy.Messages = append(busy.Messages, Message{ID: "own" + string(rune('a'+i)), AuthorID: "alice", Author: "Alice", Body: "note", SentAt: time.Now()})
	}
	for _, sent := range []int{0, 2, 3, 9} {
		local := localUI{sentCount: sent}
		if composerHintVisible(quiet, local) != composerHintVisible(busy, local) {
			t.Fatalf("after %d sends the hint differs between an empty agent conversation and a busy channel", sent)
		}
		if got := composerHintVisible(quiet, local); got != (sent < composerHintSends) {
			t.Fatalf("after %d sends the hint is shown=%v", sent, got)
		}
	}
	for _, m := range []Model{quiet, busy} {
		if markup := chatPolishMarkup(t, Build(m), 1440, "light"); !strings.Contains(markup, `id="composer-help"`) || !strings.Contains(markup, "Enter to send, Shift+Enter for a new line") {
			t.Fatalf("the hint is missing from the composer of %s", m.SelectedID)
		}
	}
}
