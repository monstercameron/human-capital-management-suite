package chatui

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func issue3977Agent(id, name, purpose string) Conversation {
	return Conversation{ID: "dm-" + id, Name: name, Kind: DirectMessage, Agent: true, AgentID: id, AgentPurpose: purpose, Joined: true}
}

func issue3977Configure(model *Model) {
	question, _ := json.Marshal(agentQuestionContext{Label: "#general", Text: "What is the PTO carryover policy?", At: time.Now().Add(-time.Minute)})
	model.EphemeralMessages[0].Body = chatux003Body + "\n\n[chat-agent-question-context:" + base64.RawURLEncoding.EncodeToString(question) + "](/chat/share/signed)\n<!--chat.agent.private:asked-->"
	model.Conversations = append(model.Conversations,
		issue3977Agent("policy-helper", "Policy Helper", "Answers policy questions"),
		issue3977Agent("benefits-helper", "Benefits Helper", "Explains benefits plans"))
}

func TestIssue3977_MenuOffersContinueAndOnlyOtherInvokableAgents(t *testing.T) {
	card, text := chatux026Card(t, "en-US", localUI{handoff: issue3977State{Card: "answer", Target: "dm-benefits-helper", Question: true}}, issue3977Configure)
	if len(chatux003Find(card, "agent-handoff-menu")) != 1 {
		t.Fatalf("the card menu does not offer Continue with another agent: %q", text)
	}
	panel := chatux003Find(card, "agent-handoff")
	if len(panel) != 1 {
		t.Fatalf("no review panel: %q", text)
	}
	for _, want := range []string{"Benefits Helper", "Explains benefits plans", "private conversation with Benefits Helper", "What is the PTO carryover policy?", "Only you can see it", "not sent until you send it", "Open draft", "Cancel"} {
		if !strings.Contains(text, want) {
			t.Fatalf("review does not say %q: %q", want, text)
		}
	}
	// One agent radio and two checkboxes: the agent that answered is not offered.
	if len(chatux003Find(card, "agent-handoff-choice")) != 3 || strings.Contains(text, "Explains benefits plans Its answer would appear in your private conversation with Policy Helper") {
		t.Fatalf("review lists the wrong choices: %q", text)
	}
	draft := chatux003Find(card, "agent-handoff-draft")
	if len(draft) != 1 || strings.Contains(chatbug030Text(draft[0]), "Carry over") {
		t.Fatalf("the answer text is previewed without being chosen")
	}
}

func TestIssue3977_EmptyStateWhenNoOtherAgent(t *testing.T) {
	card, text := chatux026Card(t, "en-US", localUI{handoff: issue3977State{Card: "answer"}}, func(m *Model) {
		m.Conversations = append(m.Conversations, issue3977Agent("policy-helper", "Policy Helper", ""))
	})
	if len(chatux003Find(card, "agent-handoff-empty")) != 1 || !strings.Contains(text, "No other agent is available to you right now") {
		t.Fatalf("no honest empty state: %q", text)
	}
	if strings.Contains(text, "Open draft") {
		t.Fatalf("an empty review offers to open a draft: %q", text)
	}
}

func TestIssue3977_StepCarriesOnlyWhatIsChosen(t *testing.T) {
	model := chat4Fixture("en-US", "answered", false)
	issue3977Configure(&model)
	next, effect, taken := issue3977Step(model, issue3977State{}, issue3977Open, "answer")
	if !taken || effect != "start" || next.Card != "answer" || !next.Question || next.Answer || next.Target != "dm-benefits-helper" {
		t.Fatalf("opening: %+v %q", next, effect)
	}
	source, _ := issue3977Read(model, "answer")
	if got := issue3977Draft(model, next, source); got != "What is the PTO carryover policy?" {
		t.Fatalf("default draft carries more than the question: %q", got)
	}
	next, _, _ = issue3977Step(model, next, issue3977Answer, "")
	got := issue3977Draft(model, next, source)
	if !strings.Contains(got, "Earlier answer:\nCarry over up to 40 hours.") || strings.Contains(got, "Paid time off policy") {
		t.Fatalf("answer draft: %q", got)
	}
	next, _, _ = issue3977Step(model, next, issue3977Question, "")
	next, _, _ = issue3977Step(model, next, issue3977Answer, "")
	next, effect, _ = issue3977Step(model, next, issue3977Go, "")
	if effect != "" || next.Notice != "nothing" {
		t.Fatalf("an empty draft was opened: %+v %q", next, effect)
	}
}

func TestIssue3977_StepRechecksTheAgentAtHandoff(t *testing.T) {
	model := chat4Fixture("en-US", "answered", false)
	issue3977Configure(&model)
	state := issue3977State{Card: "answer", Target: "dm-benefits-helper", Question: true}
	// The agent's conversation disappears before Open draft.
	model.Conversations = model.Conversations[:len(model.Conversations)-1]
	next, effect, _ := issue3977Step(model, state, issue3977Go, "")
	if effect == "open" || next.Notice != "unavailable" || next.Target != "" || next.Card != "answer" {
		t.Fatalf("a vanished agent was handed a draft: %+v %q", next, effect)
	}
	// A conversation that is not an agent the person can reach cannot be picked.
	model.Conversations = append(model.Conversations, Conversation{ID: "people-chat", Name: "Someone", Kind: DirectMessage, Joined: true})
	next, _, _ = issue3977Step(model, state, issue3977Pick, "people-chat")
	if next.Target != "dm-benefits-helper" {
		t.Fatalf("a non-agent conversation was picked: %+v", next)
	}
	// A left (not joined) agent conversation is not offered.
	model.Conversations = append(model.Conversations, Conversation{ID: "dm-left", Name: "Left", Kind: DirectMessage, Agent: true, AgentID: "left", Joined: false})
	source, _ := issue3977Read(model, "answer")
	if _, ok := issue3977Has(issue3977Agents(model, source), "dm-left"); ok {
		t.Fatal("an agent the person has left is offered")
	}
}

func TestIssue3977_MergePreservesAnExistingDraft(t *testing.T) {
	if got := issue3977Merge("half typed", "carried"); got != "half typed\n\ncarried" {
		t.Fatalf("merge overwrote or reordered: %q", got)
	}
	if got := issue3977Merge("  ", "carried"); got != "carried" {
		t.Fatalf("blank draft: %q", got)
	}
	card, text := chatux026Card(t, "en-US", localUI{handoff: issue3977State{Card: "answer", Target: "dm-benefits-helper", Question: true}}, func(m *Model) {
		issue3977Configure(m)
		m.Preferences.Drafts = map[string]string{"dm-benefits-helper": "half typed"}
	})
	if len(chatux003Find(card, "agent-handoff-existing")) != 1 || !strings.Contains(text, "already have an unsent draft") {
		t.Fatalf("the preview does not say a draft is kept: %q", text)
	}
}

func TestIssue3977_OfferedWhenASourceIsClosed(t *testing.T) {
	// The fixture answer cites a document the reader cannot open; the card still
	// offers to continue, and the row of actions keeps its usual buttons.
	card, text := chatux026Card(t, "en-US", localUI{}, issue3977Configure)
	if !strings.Contains(text, "You cannot open this document") || len(chatux003Find(card, "agent-handoff-menu")) != 1 || len(chatux003Find(card, "agent-handoff-open")) != 0 {
		t.Fatalf("closed-source answer: %q", text)
	}
}

func TestIssue3977_CopyInEveryLanguage(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		m := Model{Locale: locale}
		for _, key := range []string{"open", "note", "none", "where", "audience", "existing", "go", "notice_unavailable"} {
			if issue3977Text(m, key) == issue3977Text(Model{Locale: "en-US"}, key) {
				t.Fatalf("%s: %s is not translated", locale, key)
			}
		}
	}
}

// Open draft puts the carried text after the unsent draft already in the
// agent's conversation, opens that conversation, and keeps the draft left
// behind in the conversation the card is in. Nothing is sent.
func TestIssue3977_HandoffAppendsToExistingDraftAndSendsNothing(t *testing.T) {
	model := chat4Fixture("en-US", "answered", false)
	issue3977Configure(&model)
	model.SelectedID = "general"
	model.Preferences.Drafts = map[string]string{"dm-benefits-helper": "half typed"}
	var opened string
	changed := map[string]string{}
	model.Callbacks.SelectConversation = func(id string) { opened = id }
	model.Callbacks.DraftChanged = func(id, value string) { changed[id] = value }
	drafts := &browserDrafts{}
	drafts.set("dm-benefits-helper", "half typed")
	state := issue3977State{Card: "answer", Target: "dm-benefits-helper", Question: true}
	issue3977Handoff(model, localStore{box: &localUI{}, tick: ui.UseState(uint64(0))}, drafts, state)
	if opened != "dm-benefits-helper" {
		t.Fatalf("the agent's conversation was not opened: %q", opened)
	}
	if want := "half typed\n\nWhat is the PTO carryover policy?"; drafts.values["dm-benefits-helper"] != want || changed["dm-benefits-helper"] != want {
		t.Fatalf("the unsent draft was overwritten or not carried: %q / %q", drafts.values["dm-benefits-helper"], changed["dm-benefits-helper"])
	}
	if strings.Contains(drafts.values["dm-benefits-helper"], "Carry over") {
		t.Fatalf("the answer text came along unasked: %q", drafts.values["dm-benefits-helper"])
	}
}
