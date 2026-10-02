package main

import (
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// "Ask again" is remembered by the question it was pressed under: the wait
// starts at the click, takes the recorded copy the server answers with, ends
// when the server reports the run started from that copy, and says so when
// nothing was started. No step writes into a map a render may be reading.
func TestTodo_CHATBUG_047(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	var none map[string]chatui.AgentRetryState
	if got := chatbug047Asked(none, "", now); got != nil {
		t.Fatalf("a click with no question was remembered: %+v", got)
	}
	asked := chatbug047Asked(none, "question", now)
	if state := asked["question"]; !state.Asking || !state.Since.Equal(now) || state.PostID != "" || state.Failed {
		t.Fatalf("the click was not remembered: %+v", asked)
	}
	accepted := chatbug047Answered(asked, "question", "question-copy", false)
	if state := accepted["question"]; !state.Asking || state.PostID != "question-copy" || state.Failed {
		t.Fatalf("the server's answer was not kept: %+v", accepted)
	}
	if asked["question"].PostID != "" {
		t.Fatal("the earlier map was written into")
	}
	// Nothing reported yet: still waiting. Reported: the attempt's own state takes over.
	if waiting := chatbug047Settled(accepted, []chatui.PersonaThreadInvocation{{PostID: "question"}}); !waiting["question"].Asking {
		t.Fatal("the wait ended before the new attempt was reported")
	}
	settled := chatbug047Settled(accepted, []chatui.PersonaThreadInvocation{{PostID: "question"}, {PostID: "question-copy", ThreadID: "question"}})
	if state := settled["question"]; state.Asking || state.PostID != "question-copy" {
		t.Fatalf("the wait did not end with the new attempt: %+v", settled)
	}
	if !accepted["question"].Asking {
		t.Fatal("settling wrote into the map it was given")
	}
	// The request failed: the card says so, and the next click starts afresh.
	refused := chatbug047Answered(asked, "question", "", true)
	if state := refused["question"]; state.Asking || !state.Failed {
		t.Fatalf("a failed request is still shown as working: %+v", refused)
	}
	again := chatbug047Asked(refused, "question", now.Add(time.Minute))
	if state := again["question"]; !state.Asking || state.Failed || !state.Since.Equal(now.Add(time.Minute)) {
		t.Fatalf("asking once more did not start afresh: %+v", again)
	}
	// While it waits the page is redrawn each second; once the wait is over, or
	// the attempt was reported, it is not.
	if !chatbug047Waiting(asked, now.Add(5*time.Second)) || chatbug047Waiting(asked, now.Add(10*time.Minute)) || chatbug047Waiting(settled, now.Add(5*time.Second)) || chatbug047Waiting(refused, now.Add(time.Second)) {
		t.Fatal("the ticker does not follow the wait of a question asked again")
	}
	// An answer for a question nobody asked again changes nothing.
	if got := chatbug047Answered(none, "question", "copy", false); got != nil {
		t.Fatalf("an unasked question was remembered: %+v", got)
	}
}

// The question's reply count leaves out the copies "Ask again" recorded in its
// thread, when the conversation is read and when a copy arrives while it is
// open, for the asker and for every other reader.
func TestTodo_CHATBUG_047_Browser(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	mention := []*chatv1.Reference{{Kind: chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION, TenantId: "northwind", Id: "policy-helper", Display: "Policy Helper", ConversationId: "c1"}}
	asked := func(id string, sequence uint64, parent string) *chatv1.Post {
		post := chatTestPost(id, sequence, "alice", "@Policy Helper give me a list of the top 5 policies here", parent, now)
		post.References = mention
		return post
	}
	question := asked("question", 1, "")
	posts := []*chatv1.Post{
		question,
		asked("copy-1", 2, "question"),
		chatTestPost("reply-bob", 3, "bob", "Same question here.", "question", now),
		asked("copy-2", 4, "question"),
		// The same words typed again with no agent named asked nobody: a reply.
		chatTestPost("typed-again", 5, "alice", "@Policy Helper give me a list of the top 5 policies here", "question", now),
		// Somebody else repeating the question, agent and all, is their own question.
		func() *chatv1.Post { p := asked("bob-asks", 6, "question"); p.AuthorId = "bob"; return p }(),
		chatTestPost("other", 7, "bob", "Unrelated", "", now),
		chatTestPost("other-reply", 8, "alice", "Unrelated", "other", now),
	}
	if !chatbug047CopyPost(question, posts[1]) || chatbug047CopyPost(question, posts[2]) || chatbug047CopyPost(question, posts[4]) || chatbug047CopyPost(question, posts[5]) || chatbug047CopyPost(nil, posts[1]) || chatbug047CopyPost(posts[6], posts[1]) {
		t.Fatal("the copy rule does not pick out the recorded copies and nothing else")
	}
	for _, reader := range []string{"alice", "bob"} {
		messages := chatMessages(posts, "en-US", nil, nil, now)
		if len(messages) != 2 || messages[0].ID != "question" || messages[0].Replies != 3 || messages[1].Replies != 1 {
			t.Fatalf("%s reads %d replies under the question (and %d under the other message), want 3 and 1", reader, messages[0].Replies, messages[1].Replies)
		}
	}

	// While the conversation is open: "Ask again" records another copy, then
	// somebody replies.
	model := chatui.Model{State: chatui.StateReady, SelectedID: "c1", CurrentUser: "alice", Messages: chatMessages(posts, "en-US", nil, nil, now)}
	parent := model.Messages[0]
	model.ShowThread, model.ThreadParentID, model.ThreadParent = true, "question", &parent
	if chatbug047CountsAsReply(&model, asked("copy-3", 9, "question")) || !chatbug047CountsAsReply(&model, chatTestPost("reply-2", 10, "bob", "Thanks", "question", now)) || !chatbug047CountsAsReply(&model, asked("unknown-parent", 11, "not-on-the-page")) {
		t.Fatal("a copy that arrives live is counted, or a reply is not")
	}
	applyChatPostCreated(&model, asked("copy-3", 9, "question"), map[string]uint64{}, "en-US", nil, now)
	if model.Messages[0].Replies != 3 || model.ThreadParent.Replies != 3 {
		t.Fatalf("a recorded copy raised the reply count to %d (thread %d)", model.Messages[0].Replies, model.ThreadParent.Replies)
	}
	applyChatPostCreated(&model, chatTestPost("reply-2", 10, "bob", "Thanks", "question", now), map[string]uint64{}, "en-US", nil, now)
	if model.Messages[0].Replies != 4 || model.ThreadParent.Replies != 4 {
		t.Fatalf("a real reply is not counted: %d (thread %d)", model.Messages[0].Replies, model.ThreadParent.Replies)
	}
}

// A dismissed card stays dismissed for the page, one card at a time, and comes
// back into view when its question is asked again.
func TestTodo_CHATBUG_054(t *testing.T) {
	var none map[string]bool
	if got := chatbug054Dismiss(none, ""); got != nil {
		t.Fatalf("a card with no name was dismissed: %+v", got)
	}
	one := chatbug054Dismiss(none, "run")
	two := chatbug054Dismiss(one, "question:q2")
	if !one["run"] || one["question:q2"] || !two["run"] || !two["question:q2"] {
		t.Fatalf("dismissals: %+v then %+v", one, two)
	}
	back := chatbug054Undismiss(two, "run", "question:q9")
	if back["run"] || !back["question:q2"] || !two["run"] {
		t.Fatalf("asking again did not bring exactly that card back: %+v (earlier map %+v)", back, two)
	}
	if same := chatbug054Undismiss(two, "another"); len(same) != 2 {
		t.Fatalf("an unrelated question changed the dismissals: %+v", same)
	}
}
