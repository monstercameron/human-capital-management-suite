package chatui

import (
	"strings"
	"testing"
)

// general ask fixtures: a channel (with a persona placed) where the page can
// reach the built-in General agent.
type issue3756Calls struct {
	asked    []string
	finish   func(GeneralAgentAnswer, error)
	sent     []string
	cleared  int
	notice   string
	selected string
}

func issue3756Fixture(locale string, direct bool) (Model, *issue3756Calls) {
	m := composerToolsModel(locale, direct)
	calls := &issue3756Calls{}
	m.Callbacks.AskGeneralAgent = func(question string, done func(GeneralAgentAnswer, error)) {
		calls.asked = append(calls.asked, question)
		calls.finish = done
	}
	m.Callbacks.SendMessage = func(conversation, body string) { calls.sent = append(calls.sent, conversation+"|"+body) }
	return m, calls
}

func issue3756Runtime(m Model, local localStore, calls *issue3756Calls) composerCommandRuntime {
	rt := composerCommandRuntime{
		Model:      m,
		ClearDraft: func() { calls.cleared++ },
		Notice:     func(text string) { calls.notice = text },
	}
	rt.AskGeneral = func(question string) { issue3756Start(m, local, question) }
	return rt
}

// /ask with no agent name, in a channel, asks the built-in General agent:
// nothing is posted, the draft is cleared, and the question reaches the agent
// exactly as typed.
func TestIssue3756_AskWithoutNameStartsGeneralAgent(t *testing.T) {
	m, calls := issue3756Fixture("en-US", false)
	local := localStore{box: &localUI{}}
	rt := issue3756Runtime(m, local, calls)

	text, consumed := composerSendCommand(rt, "/ask how many days carry over?")
	if !consumed || text != "" {
		t.Fatalf("the line was posted instead of asked: consumed=%v text=%q", consumed, text)
	}
	if len(calls.asked) != 1 || calls.asked[0] != "how many days carry over?" {
		t.Fatalf("asked = %#v", calls.asked)
	}
	if calls.notice != "" {
		t.Fatalf("a refusal was shown: %q", calls.notice)
	}
	if len(calls.sent) != 0 {
		t.Fatalf("a message was sent to the conversation: %v", calls.sent)
	}
	if got := local.get().generalAsk; got.Phase != issue3756Asking || got.Room != m.SelectedID || got.Question != "how many days carry over?" {
		t.Fatalf("state = %#v", got)
	}
}

// With nothing asked the person is told what to write, in their language.
func TestIssue3756_AskNeedsAQuestion(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m, calls := issue3756Fixture(locale, false)
		local := localStore{box: &localUI{}}
		text, consumed := composerSendCommand(issue3756Runtime(m, local, calls), "/ask   ")
		if !consumed || text != "" || len(calls.asked) != 0 || calls.cleared != 0 {
			t.Fatalf("%s: consumed=%v text=%q asked=%v cleared=%d", locale, consumed, text, calls.asked, calls.cleared)
		}
		if calls.notice == "" || strings.Contains(calls.notice, "issue3756") || calls.notice != issue3756Text(m, "needs_question") {
			t.Fatalf("%s: notice = %q", locale, calls.notice)
		}
	}
}

// Naming an agent, or being in an agent's own conversation, still goes to that
// agent through the server: the General agent is only the answer to "no name".
func TestIssue3756_NamedAgentAndDirectConversationUnchanged(t *testing.T) {
	m, calls := issue3756Fixture("en-US", false)
	local := localStore{box: &localUI{}}
	text, consumed := composerSendCommand(issue3756Runtime(m, local, calls), "/ask @Policy Helper how many days carry over?")
	if consumed || text != "/ask @Policy Helper how many days carry over?" || len(calls.asked) != 0 {
		t.Fatalf("named: consumed=%v text=%q asked=%v", consumed, text, calls.asked)
	}
	d, dcalls := issue3756Fixture("en-US", true)
	text, consumed = composerSendCommand(issue3756Runtime(d, local, dcalls), "/ask how many days carry over?")
	if consumed || text != "/ask how many days carry over?" || len(dcalls.asked) != 0 {
		t.Fatalf("direct: consumed=%v text=%q asked=%v", consumed, text, dcalls.asked)
	}
	// Two agents named is still refused, not turned into a General ask.
	two := m
	second := two.ResolvedPersonaMentions[0]
	second.Reference.ID, second.Reference.Display = "comp-analyst", "Comp Analyst"
	two.ResolvedPersonaMentions = append(append([]ResolvedPersonaMention(nil), two.ResolvedPersonaMentions...), second)
	_, consumed = composerSendCommand(issue3756Runtime(two, local, calls), "/ask @Policy Helper @Comp Analyst compare")
	if !consumed || len(calls.asked) != 0 || calls.notice != agent027Text(two, agent027OneAgent) {
		t.Fatalf("two agents: consumed=%v asked=%v notice=%q", consumed, calls.asked, calls.notice)
	}
}

// Without the page's way to start a General agent task, nothing changes: the
// command is not offered where no agent is placed and a bare ask is refused.
func TestIssue3756_NoCallbackNoGeneralAsk(t *testing.T) {
	m := composerToolsModel("en-US", false)
	m.ResolvedPersonaMentions = nil
	for _, command := range composerCommandsFor("chat-composer").menu(m, "") {
		if command.Name == "ask" {
			t.Fatal("/ask offered with no agent and no way to ask the General agent")
		}
	}
	m.Callbacks.AskGeneralAgent = func(string, func(GeneralAgentAnswer, error)) {}
	found := false
	for _, command := range composerCommandsFor("chat-composer").menu(m, "") {
		found = found || command.Name == "ask"
	}
	if !found {
		t.Fatal("/ask is not offered although the General agent can be asked")
	}
}

// The question is asked once at a time.
func TestIssue3756_OneAskAtATime(t *testing.T) {
	m, calls := issue3756Fixture("en-US", false)
	local := localStore{box: &localUI{}}
	rt := issue3756Runtime(m, local, calls)
	composerSendCommand(rt, "/ask first?")
	composerSendCommand(rt, "/ask second?")
	if len(calls.asked) != 1 {
		t.Fatalf("asked = %v", calls.asked)
	}
	if got := local.get().composerNotice; got != issue3756Text(m, "busy") {
		t.Fatalf("notice = %q", got)
	}
}

// The answer comes back to the person alone, with the way to open the task in
// Agents; nothing is posted until they choose to.
func TestIssue3756_AnswerIsPrivateUntilPosted(t *testing.T) {
	m, calls := issue3756Fixture("en-US", false)
	local := localStore{box: &localUI{}}
	issue3756Start(m, local, "how many days carry over?")
	calls.finish(GeneralAgentAnswer{TaskID: "task 7/a", State: "completed", Answer: "Up to five days carry over."}, nil)
	st := local.get().generalAsk
	if st.Phase != issue3756Answered || st.Answer != "Up to five days carry over." || st.TaskID != "task 7/a" {
		t.Fatalf("state = %#v", st)
	}
	markup := renderNode(t, issue3756Card(m, local.get()))
	for _, want := range []string{
		"General agent", "Only you can see this", "Up to five days carry over.",
		`href="/workspace/app/chat/agents?task=task+7%2Fa"`, "Open in Agents",
		`data-action="general-ask-post"`, "Post answer here", `data-action="general-ask-dismiss"`,
		`role="status"`, `aria-live="polite"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("card lacks %q: %s", want, markup)
		}
	}
	if len(calls.sent) != 0 {
		t.Fatalf("the answer was posted without being asked to: %v", calls.sent)
	}
	// Another conversation shows nothing of it.
	other := m
	other.SelectedID = "people"
	if issue3756Card(other, local.get()) != nil {
		t.Fatal("the private answer shows in another conversation")
	}

	// Posting is the person's own message, naming where it came from.
	if !issue3756Post(m, local) {
		t.Fatal("the answer was not posted")
	}
	if len(calls.sent) != 1 || !strings.HasPrefix(calls.sent[0], "general|") ||
		!strings.Contains(calls.sent[0], "how many days carry over?") || !strings.Contains(calls.sent[0], "Up to five days carry over.") ||
		!strings.Contains(calls.sent[0], "General agent") {
		t.Fatalf("sent = %v", calls.sent)
	}
	if local.get().generalAsk.Phase != "" {
		t.Fatalf("the card stayed after posting: %#v", local.get().generalAsk)
	}
	// Nothing left to post.
	if issue3756Post(m, local) || len(calls.sent) != 1 {
		t.Fatal("a second post went out")
	}
}

// A task that has not finished is not an answer: the card says it is still
// working and offers the task, and has nothing to post.
func TestIssue3756_StillWorkingHasNothingToPost(t *testing.T) {
	m, calls := issue3756Fixture("en-US", false)
	local := localStore{box: &localUI{}}
	issue3756Start(m, local, "summarise the handbook")
	calls.finish(GeneralAgentAnswer{TaskID: "t1", State: "running"}, nil)
	markup := renderNode(t, issue3756Card(m, local.get()))
	if !strings.Contains(markup, "still working") || !strings.Contains(markup, "Open in Agents") || strings.Contains(markup, "general-ask-post") {
		t.Fatalf("card = %s", markup)
	}
	if issue3756Post(m, local) || len(calls.sent) != 0 {
		t.Fatal("a post went out with no answer")
	}
}

// A failure says nothing was posted, keeps the question, and offers Try again,
// which asks again.
func TestIssue3756_FailureAndRetry(t *testing.T) {
	m, calls := issue3756Fixture("en-US", false)
	local := localStore{box: &localUI{}}
	issue3756Start(m, local, "what is my band?")
	calls.finish(GeneralAgentAnswer{}, errTestIssue3756)
	markup := renderNode(t, issue3756Card(m, local.get()))
	for _, want := range []string{"could not answer", "Nothing was posted", `data-action="general-ask-retry"`, "Try again", `role="alert"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("failure card lacks %q: %s", want, markup)
		}
	}
	// A refusal the person can act on is named in plain words.
	issue3756Start(m, local, "what is my band?")
	calls.finish(GeneralAgentAnswer{Reason: "disabled"}, nil)
	if markup = renderNode(t, issue3756Card(m, local.get())); !strings.Contains(markup, "Agents are turned off for your organization.") {
		t.Fatalf("reason sentence missing: %s", markup)
	}
	before := len(calls.asked)
	if !issue3756Retry(m, local) || len(calls.asked) != before+1 || calls.asked[before] != "what is my band?" {
		t.Fatalf("retry did not ask again: %v", calls.asked)
	}
	if local.get().generalAsk.Phase != issue3756Asking {
		t.Fatalf("phase = %q", local.get().generalAsk.Phase)
	}
}

// Copy exists in every language and never shows a key.
func TestIssue3756_CopyInEveryLanguage(t *testing.T) {
	keys := []string{"title", "asking", "answered", "working", "failed", "failed_nothing_posted", "open", "post", "retry", "dismiss", "reason_disabled", "reason_denied", "needs_question", "busy", "answer_intro"}
	for _, key := range keys {
		seen := map[string]bool{}
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			m := composerToolsModel(locale, false)
			text := issue3756Text(m, key)
			if text == "" || strings.Contains(text, "⟦") || strings.Contains(text, "issue3756") || seen[text] {
				t.Fatalf("%s in %s: %q", key, locale, text)
			}
			seen[text] = true
		}
	}
}

// The card renders in the right-to-left page with direction left to the text,
// and a busy ask is announced.
func TestIssue3756_CardRTLAndAsking(t *testing.T) {
	m, calls := issue3756Fixture("ar", false)
	local := localStore{box: &localUI{}}
	issue3756Start(m, local, "كم يوما")
	markup := renderNode(t, issue3756Card(m, local.get()))
	if !strings.Contains(markup, issue3756Text(m, "asking")) || !strings.Contains(markup, `aria-busy="true"`) || !strings.Contains(markup, `dir="auto"`) {
		t.Fatalf("asking card = %s", markup)
	}
	calls.finish(GeneralAgentAnswer{TaskID: "t", State: "completed", Answer: "خمسة أيام"}, nil)
	markup = renderNode(t, issue3756Card(m, local.get()))
	if !strings.Contains(markup, "خمسة أيام") || !strings.Contains(markup, issue3756Text(m, "post")) {
		t.Fatalf("answered card = %s", markup)
	}
}

type testErr3756 string

func (e testErr3756) Error() string { return string(e) }

const errTestIssue3756 = testErr3756("rpc failed")

// The composer itself carries the card for the open conversation only.
func TestIssue3756_ComposerShowsCard(t *testing.T) {
	m, _ := issue3756Fixture("de-DE", false)
	local := localUI{generalAsk: issue3756State{Room: m.SelectedID, Question: "Wie viele Tage?", Phase: issue3756Answered, TaskID: "t1", Answer: "Fünf Tage."}}
	markup := composerToolsMarkup(t, m, local)
	for _, want := range []string{`data-general-ask="answered"`, "Allgemeiner Agent", "Fünf Tage.", "Antwort hier posten", "Unter Agenten öffnen"} {
		if !strings.Contains(markup, want) {
			t.Errorf("composer lacks %q", want)
		}
	}
	m.SelectedID = "people"
	if strings.Contains(composerToolsMarkup(t, m, local), "general-ask") {
		t.Error("the card shows in another conversation")
	}
}
