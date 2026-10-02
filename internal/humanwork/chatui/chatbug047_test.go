package chatui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

func chatbug047Rows(t *testing.T, m Model) string {
	t.Helper()
	return chatPolishMarkup(t, html.Div(html.Props{}, personaReplyRowsForPost(m, localUI{}, "question", time.Now())...), 1440, "light")
}

// chatbug047Attempt is the run the server started from the recorded copy of the
// question: a reply in the question's thread.
func chatbug047Attempt(state string) PersonaThreadInvocation {
	projection := PersonaProgressProjection{InvocationID: "run-2", ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper"}
	switch state {
	case "working":
		projection.Progress = &PersonaProgressProps{InvocationID: "run-2", InvokerID: "alice", AgentName: "Policy Helper", Visible: true, ElapsedSeconds: 12}
	case "failed":
		projection.Failure = &PersonaProgressFailure{InvocationID: "run-2", InvokerID: "alice", Code: "MODEL_UNAVAILABLE"}
	case "stored":
		projection.AnswerStored, projection.PrivateReplyHref, projection.AnswerDue = true, ChannelReferenceURL("policy"), time.Now().Add(time.Minute)
	}
	return PersonaThreadInvocation{PostID: "question-copy", ThreadID: "question", Projection: projection}
}

// Asking again shows the new attempt in the failed card's place, under the
// question it belongs to: first the working state, then the answer, and never
// two cards.
func TestTodo_CHATBUG_047(t *testing.T) {
	failed := func() Model {
		m := chat4Fixture("en-US", "failed", false)
		m.PersonaActivityReady = true
		return m
	}

	// The click: the working state at once, before the server has said anything.
	m := failed()
	m.AgentRetries = map[string]AgentRetryState{"question": {Asking: true, Since: time.Now().Add(-2 * time.Second)}}
	page := chatbug047Rows(t, m)
	if strings.Count(page, `data-agent-reply-state=`) != 1 || !strings.Contains(page, `data-agent-reply-state="working"`) || strings.Contains(page, `data-agent-reply-state="failed"`) {
		t.Fatalf("Ask again left the failed card in place or drew two cards: %s", page)
	}

	// The server reports the new attempt, started from the recorded copy.
	m = failed()
	m.AgentRetries = map[string]AgentRetryState{"question": {PostID: "question-copy"}}
	m.PersonaInvocations = append(m.PersonaInvocations, chatbug047Attempt("working"))
	page = chatbug047Rows(t, m)
	if strings.Count(page, `data-agent-reply-state=`) != 1 || !strings.Contains(page, `data-agent-reply-state="working"`) || !strings.Contains(page, "0:12") {
		t.Fatalf("the new attempt is not the one card under the question: %s", page)
	}

	// After a reload the page has no memory of the click; the attempt is still
	// drawn under the question because it lives in the question's thread.
	m.AgentRetries = nil
	if page = chatbug047Rows(t, m); strings.Count(page, `data-agent-reply-state=`) != 1 || !strings.Contains(page, `data-agent-reply-state="working"`) {
		t.Fatalf("after a reload the new attempt is not under its question: %s", page)
	}

	// Its private answer is stored: the answer's placeholder, not the failure.
	m.PersonaInvocations[1] = chatbug047Attempt("stored")
	if page = chatbug047Rows(t, m); strings.Count(page, `data-agent-reply-state=`) != 1 || !strings.Contains(page, `data-agent-reply-state="loading-answer"`) {
		t.Fatalf("a stored answer of the new attempt does not replace the failed card: %s", page)
	}

	// Its private answer arrives: the answer card, rated as the attempt that gave it.
	m.EphemeralMessages = []EphemeralMessage{{ID: "answer", ThreadID: "question", Body: "Carry over up to 40 hours.", OnlyVisibleToYou: true, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}}
	page = chatbug047Rows(t, m)
	if strings.Count(page, `data-agent-reply-state=`) != 1 || !strings.Contains(page, `data-agent-reply-state="answered-private"`) || !strings.Contains(page, "Carry over up to 40 hours.") {
		t.Fatalf("the answer did not replace the failed card under its question: %s", page)
	}
	if !strings.Contains(page, `data-id="run-2"`) || strings.Contains(page, `data-id="run"`) {
		t.Fatalf("the answer is rated as the attempt that failed: %s", page)
	}

	// An answer posted to the channel sits in the thread: the failed card is gone.
	m.EphemeralMessages = nil
	m.PersonaInvocations[1] = chatbug047Attempt("answered")
	if rows := personaReplyRowsForPost(m, localUI{}, "question", time.Now()); len(rows) != 0 {
		t.Fatalf("a question answered on a later attempt still shows %d cards", len(rows))
	}

	// Every attempt failed: one failed card, whose Ask again names the question's own run.
	m.PersonaInvocations[1] = chatbug047Attempt("failed")
	page = chatbug047Rows(t, m)
	if strings.Count(page, `data-agent-reply-state=`) != 1 || !strings.Contains(page, `data-agent-reply-state="failed"`) || !strings.Contains(page, `data-agent-invocation-id="run"`) {
		t.Fatalf("two failed attempts do not end in one card for the question: %s", page)
	}

	// Asking again that never started stops waiting and says so.
	m = failed()
	m.AgentRetries = map[string]AgentRetryState{"question": {Asking: true, Since: time.Now().Add(-agentRetryPendingWindow - time.Second)}}
	page = chatbug047Rows(t, m)
	if !strings.Contains(page, `data-agent-reply-state="failed"`) || !strings.Contains(page, "The question could not be asked again.") {
		t.Fatalf("an Ask again nobody answered waits for ever: %s", page)
	}

	// A question that did not fail is not touched by a later question in its thread.
	working := chat4Fixture("en-US", "working2", false)
	working.PersonaInvocations = append(working.PersonaInvocations, chatbug047Attempt("failed"))
	if page = chatbug047Rows(t, working); strings.Count(page, `data-agent-reply-state=`) != 1 || !strings.Contains(page, `data-agent-reply-state="working"`) {
		t.Fatalf("a later question in the thread changed the first question's card: %s", page)
	}
}

// The recorded copy of the question is not shown as a reply, and an answer that
// does not directly follow its question carries a one-line quote of it.
func TestTodo_CHATBUG_047_Browser(t *testing.T) {
	// The thread of a question that was asked again twice.
	m := chat4Fixture("en-US", "failed", false)
	parent := m.Messages[0]
	m.ShowThread, m.ThreadParentID, m.ThreadParent = true, "question", &parent
	m.Callbacks.CloseThread, m.Callbacks.ReplyInThread = func() {}, func(string, string) {}
	copyOf := func(id string) Message {
		reply := parent
		reply.ID, reply.Replies = id, 0
		return reply
	}
	// The same words typed again in the thread with no agent named: no run was
	// asked for, so it is a reply like any other.
	repeat := copyOf("unrelated-repeat")
	repeat.PersonaReferences = nil
	m.ThreadMessages = []Message{copyOf("question-copy"), {ID: "real-reply", AuthorID: "bob", Author: "Bob", Body: "Same question here.", TimeLabel: "9:40"}, repeat}
	m.PersonaInvocations = append(m.PersonaInvocations, chatbug047Attempt("failed"))
	if !chatbug047RetryCopy(m, m.ThreadMessages[0]) {
		t.Fatal("the recorded copy of the question is not recognised")
	}
	// Another member of the channel holds none of the asker's runs: the copy is
	// recognised from the two messages alone, and is not a reply for them either.
	other := m
	other.CurrentUser, other.PersonaInvocations, other.AgentRetries = "bob", nil, nil
	if !chatbug047RetryCopy(other, m.ThreadMessages[0]) || chatbug047RetryCopy(other, m.ThreadMessages[1]) || chatbug047RetryCopy(other, m.ThreadMessages[2]) {
		t.Fatal("for another member the recorded copy is a reply, or a real reply is not")
	}
	if !AgentRetryCopy(parent, m.ThreadMessages[0]) || AgentRetryCopy(parent, m.ThreadMessages[1]) || AgentRetryCopy(parent, repeat) || AgentRetryCopy(parent, parent) || AgentRetryCopy(Message{}, m.ThreadMessages[0]) {
		t.Fatal("the copy rule does not hold on the two messages alone")
	}
	if page := render(t, other); strings.Contains(page, `data-message-id="question-copy"`) || !strings.Contains(page, "2 replies") {
		t.Fatal("another member sees the recorded copy as a thread reply")
	}
	// The page may hold the question only in the conversation's own list.
	listed := m
	listed.ThreadParent = nil
	if !chatbug047RetryCopy(listed, m.ThreadMessages[0]) {
		t.Fatal("the recorded copy is not recognised when the question is held in the message list")
	}
	closed := m
	closed.ThreadParentID, closed.ThreadParent = "", nil
	if chatbug047RetryCopy(closed, m.ThreadMessages[0]) {
		t.Fatal("a message is taken for a recorded copy with no thread open")
	}
	if chatbug047RetryCopy(m, m.ThreadMessages[1]) {
		t.Fatal("somebody else's reply is taken for a recorded copy")
	}
	if chatbug047RetryCopy(m, m.ThreadMessages[2]) {
		t.Fatal("a reply the person typed again themselves, with no run behind it, is hidden")
	}
	if chatbug047RetryCopy(m, parent) {
		t.Fatal("the question is taken for its own copy")
	}
	page := render(t, m)
	if strings.Contains(page, `data-message-id="question-copy"`) {
		t.Fatal("the recorded copy is shown as a thread reply")
	}
	if !strings.Contains(page, `data-message-id="real-reply"`) || !strings.Contains(page, `data-message-id="unrelated-repeat"`) {
		t.Fatal("real replies were hidden with the recorded copy")
	}
	if !strings.Contains(page, "2 replies") || strings.Contains(page, "3 replies") {
		t.Fatal("the thread counts the recorded copy as a reply")
	}

	// The agent's own conversation: the answer to a later attempt arrives after
	// other messages and carries the question it answers.
	now := time.Now()
	direct := chat4Fixture("en-US", "sent", true)
	var jumped []string
	direct.Callbacks.OpenSearchMessage = func(conversation, post string, sequence uint64) {
		jumped = append(jumped, conversation+"/"+post+"/"+strconv.FormatUint(sequence, 10))
	}
	agent := &PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}
	direct.Messages = []Message{
		{ID: "q1", Sequence: 41, AuthorID: "alice", Author: "Alice", Body: "give me a list of\nthe top 5 policies here", TimeLabel: "8:05", SentAt: now.Add(-6 * time.Hour)},
		{ID: "q2", AuthorID: "alice", Author: "Alice", Body: "how many PTO hours carry over?", TimeLabel: "11:18", SentAt: now.Add(-3 * time.Hour)},
		{ID: "a2", AuthorID: "policy-helper", Author: "Policy Helper", Body: "Up to 40 hours.", TimeLabel: "11:18", SentAt: now.Add(-3 * time.Hour).Add(time.Second), PersonaActor: agent},
		{ID: "a1", AuthorID: "policy-helper", Author: "Policy Helper", Body: "I can read one policy here: Paid time off policy.", TimeLabel: "2:07", SentAt: now.Add(-time.Minute), PersonaActor: agent},
	}
	done := func(id, post, thread, answer string) PersonaThreadInvocation {
		return PersonaThreadInvocation{PostID: post, ThreadID: thread, Projection: PersonaProgressProjection{InvocationID: id, ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper", DurablePostID: answer}}
	}
	direct.PersonaInvocations = []PersonaThreadInvocation{done("run-q2", "q2", "q2", "a2"), done("run-q1-again", "q1-copy", "q1", "a1")}
	if quote := chatbug047QuestionQuote(direct, direct.Messages[2], false); quote != nil {
		t.Fatal("an answer directly under its question repeats the question")
	}
	if quote := chatbug047QuestionQuote(direct, direct.Messages[3], true); quote != nil {
		t.Fatal("an answer that already carries its channel question got a second quote")
	}
	page = render(t, direct)
	if got := strings.Count(page, `class="agent-question-quote"`); got != 1 {
		t.Fatalf("%d question quotes in the conversation, want one, on the late answer", got)
	}
	for _, want := range []string{"give me a list of the top 5 policies here", "8:05", `data-action="agent-question-jump"`, `data-id="q1"`, `aria-label="Go to the question: give me a list of the top 5 policies here"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("the quote on the late answer is missing %q", want)
		}
	}
	chatbug047JumpToQuestion(direct, "q1")
	chatbug047JumpToQuestion(direct, "not-on-the-page")
	if len(jumped) != 1 || jumped[0] != "policy/q1/41" {
		t.Fatalf("pressing the quote went to %v, want the question", jumped)
	}
	if !strings.Contains(Stylesheet, ".agent-question-quote-text{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}") {
		t.Fatal("the quote is not held to one line")
	}
	// In a channel an agent's posted answer is a thread reply and gets no quote.
	channel := chat4Fixture("en-US", "answered", false)
	if quote := chatbug047QuestionQuote(channel, Message{ID: "answer", PersonaActor: agent}, false); quote != nil {
		t.Fatal("a quote was drawn outside the agent's own conversation")
	}
}
