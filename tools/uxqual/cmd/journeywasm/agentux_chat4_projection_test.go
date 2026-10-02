package main

import (
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestAgentUXChat4_M5_CommittedMention(t *testing.T) {
	for _, parent := range []string{"", "root"} {
		t.Run("parent="+parent, func(t *testing.T) {
			state := newChatStateForTest(t)
			state.model.SelectedID = "c1"
			state.model.ShowThread, state.model.ThreadParentID = parent != "", parent
			state.model.Messages = []chatui.Message{{ID: "root", Sequence: 1}}
			state.cursor = chatCursor{ConversationID: "c1", LastSequence: 1}
			post := chatTestPost("question", 2, "avery", "@Policy Helper explain our PTO policy in detail: accrual", parent, time.Now())
			post.References = []*chatv1.Reference{{Kind: chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION, TenantId: "northwind", ConversationId: "c1", Id: "policy", Display: "Policy Helper"}}
			applied, gapped := state.applySentChatPost("c1", post, "en-US", nil, time.Now())
			model := state.snapshot()
			if !applied || gapped || len(model.PersonaInvocations) != 1 || model.ShowThread != (parent != "") {
				t.Fatalf("send did not paint admitted state without opening a thread: applied=%v gap=%v model=%+v", applied, gapped, model)
			}
			progress := model.PersonaInvocations[0].Projection
			if progress.AgentName != "Policy Helper" || progress.ViewerID != "avery" || progress.Progress == nil || !progress.Progress.Visible || progress.Progress.InvocationID != "" {
				t.Fatalf("waiting state lost attribution/privacy or exposed a fake cancel ID: %+v", progress)
			}
			if parent == "" && (len(model.Messages) != 2 || model.Messages[1].Body != post.Body) || parent != "" && len(model.ThreadMessages) != 1 {
				t.Fatal("committed question did not reach its timeline")
			}
			state.applySentChatPost("c1", post, "en-US", nil, time.Now())
			if len(state.snapshot().PersonaInvocations) != 1 {
				t.Fatal("stream echo duplicated the waiting card")
			}
		})
	}
}

func TestAgentUXChat4_M5_PendingScope(t *testing.T) {
	for _, wrong := range []string{"tenant", "room", "author", "person"} {
		t.Run(wrong, func(t *testing.T) {
			model := chatui.Model{CurrentUser: "alice", CurrentTenantID: "tenant", SelectedID: "general"}
			post := &chatv1.Post{Id: "question", AuthorId: "alice", ConversationId: "general", References: []*chatv1.Reference{{Kind: chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION, Id: "policy", Display: "Policy Helper", TenantId: "tenant", ConversationId: "general"}}}
			switch wrong {
			case "tenant":
				post.References[0].TenantId = "other"
			case "room":
				post.References[0].ConversationId = "other"
			case "author":
				post.AuthorId = "bob"
			case "person":
				post.References[0].Kind = chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION
			}
			applyCommittedAgentPending(&model, post)
			if len(model.PersonaInvocations) != 0 {
				t.Fatal("waiting state admitted another tenant, room, member or a human mention")
			}
		})
	}
}

func TestAgentUXChat4_J9_RetryReplacesState(t *testing.T) {
	model := chatui.Model{CurrentUser: "alice", CurrentTenantID: "tenant", SelectedID: "general"}
	post := &chatv1.Post{Id: "question", AuthorId: "alice", ConversationId: "general", References: []*chatv1.Reference{{Kind: chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION, Id: "policy", Display: "Policy Helper", TenantId: "tenant", ConversationId: "general"}}}
	applyCommittedAgentPending(&model, post)
	if got := reconcileAgentPending(model.PersonaInvocations, nil); len(got) != 1 {
		t.Fatal("empty first read hid a committed question's working state")
	}
	failed := []chatui.PersonaThreadInvocation{{PostID: "question", Projection: chatui.PersonaProgressProjection{InvocationID: "run", Failure: &chatui.PersonaProgressFailure{Code: "MODEL_UNAVAILABLE"}}}}
	got := reconcileAgentPending(model.PersonaInvocations, failed)
	if len(got) != 1 || got[0].Projection.InvocationID != "run" || got[0].Projection.Failure == nil {
		t.Fatal("failure did not replace provisional state")
	}
	working := []chatui.PersonaThreadInvocation{{PostID: "question", Projection: chatui.PersonaProgressProjection{InvocationID: "retry", Progress: &chatui.PersonaProgressProps{Visible: true}}}}
	got = reconcileAgentPending(got, working)
	if len(got) != 1 || got[0].Projection.Failure != nil || got[0].Projection.Progress == nil {
		t.Fatal("retry retained failure")
	}
	answered := []chatui.PersonaThreadInvocation{{PostID: "question", Projection: chatui.PersonaProgressProjection{InvocationID: "retry", DurablePostID: "answer"}}}
	got = reconcileAgentPending(got, answered)
	if len(got) != 1 || got[0].Projection.Progress != nil || got[0].Projection.Failure != nil || got[0].Projection.DurablePostID != "answer" {
		t.Fatal("successful retry retained an old card")
	}
}

func TestAgentUXChat4_M19_AgentIdentityAcrossRefresh(t *testing.T) {
	state := newChatStateForTest(t)
	state.model.SelectedID = "general"
	state.model.Conversations = []chatui.Conversation{{ID: "policy", Name: "Policy Helper", Kind: chatui.DirectMessage, Agent: true, AgentID: "policy"}}
	loaded := state.snapshot()
	loaded.Conversations = []chatui.Conversation{{ID: "policy", Name: "Policy Helper", Kind: chatui.DirectMessage, Unread: 1}}
	loaded.Sections = []chatui.SidebarSection{{ID: "direct", Chats: loaded.Conversations}}
	if !state.adoptLoadedChatProjection(state.generation, loaded, chatCursor{}, false) {
		t.Fatal("current refresh rejected")
	}
	got := state.snapshot()
	if !got.Conversations[0].Agent || got.Conversations[0].AgentID != "policy" || !got.Sections[0].Chats[0].Agent || got.Conversations[0].Unread != 1 {
		t.Fatal("late listing erased the badge or unread arrival")
	}
	loaded.CurrentTenantID = "other"
	loaded.Conversations[0].Agent = false
	preserveAgentConversationIdentity(got, &loaded)
	if loaded.Conversations[0].Agent {
		t.Fatal("agent identity crossed tenant scope")
	}
}

func TestAgentUXChat4_M19_ChannelAnswerNamesAgentRow(t *testing.T) {
	model := chatui.Model{CurrentTenantID: "tenant", CurrentUser: "alice", SelectedID: "general", Conversations: []chatui.Conversation{{ID: "policy", Name: "Policy Helper", Kind: chatui.DirectMessage}}, Messages: []chatui.Message{{ID: "question", PersonaReferences: []chatui.ChatReference{{Kind: "AGENT_MENTION", TenantID: "tenant", ConversationID: "general", ID: "policy-helper", Display: "Policy Helper"}}}}, PersonaInvocations: []chatui.PersonaThreadInvocation{{PostID: "question", Projection: chatui.PersonaProgressProjection{ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper", PrivateReplyHref: chatui.ChannelReferenceURL("policy")}}}}
	bindAgentInvocationConversations(&model)
	if !model.Conversations[0].Agent || model.Conversations[0].AgentID != "policy-helper" {
		t.Fatal("channel answer left its saved agent conversation unmarked")
	}
	for _, wrong := range []string{"tenant", "viewer", "href"} {
		model.Conversations[0].Agent = false
		model.Messages[0].PersonaReferences[0].TenantID = "tenant"
		model.PersonaInvocations[0].Projection.ViewerID = "alice"
		model.PersonaInvocations[0].Projection.PrivateReplyHref = chatui.ChannelReferenceURL("policy")
		switch wrong {
		case "tenant":
			model.Messages[0].PersonaReferences[0].TenantID = "other"
		case "viewer":
			model.PersonaInvocations[0].Projection.ViewerID = "bob"
		case "href":
			model.PersonaInvocations[0].Projection.PrivateReplyHref = "https://evil.invalid/workspace/app/chat#channel=policy"
		}
		bindAgentInvocationConversations(&model)
		if model.Conversations[0].Agent {
			t.Fatalf("agent room classification accepted wrong %s", wrong)
		}
	}
}

func TestAgentUXChat4_M9_G68_VisibleReplyCount(t *testing.T) {
	now := time.Now()
	root := chatTestPost("root", 1, "avery", "A public question", "", now)
	receipt := chatTestPost("receipt", 2, "agent", "The persona reply was sent privately to you.", "root", now)
	reply := chatTestPost("reply", 3, "bob", "A public reply", "root", now)
	posts := []*chatv1.Post{root, receipt, reply}
	messages := chatMessages(posts, "en-US", nil, nil, now)
	thread := chatThreadMessages(posts, "root", "en-US", nil, now)
	if len(messages) != 1 || messages[0].Replies != 1 || len(thread) != 1 || thread[0].ID != "reply" {
		t.Fatalf("private receipt leaked through initial reply projection: messages=%+v thread=%+v", messages, thread)
	}
	for _, delivery := range []string{"send", "catchup", "stream"} {
		t.Run(delivery, func(t *testing.T) {
			state := newChatStateForTest(t)
			state.model.SelectedID = "c1"
			state.model.ShowThread, state.model.ThreadParentID = true, "root"
			state.model.Messages = []chatui.Message{{ID: "root", Sequence: 1}}
			state.model.ThreadParent = &chatui.Message{ID: "root", Sequence: 1}
			state.cursor = chatCursor{ConversationID: "c1", LastSequence: 1}
			switch delivery {
			case "send":
				state.applySentChatPost("c1", receipt, "en-US", nil, now)
				state.applySentChatPost("c1", reply, "en-US", nil, now)
			case "catchup":
				state.applyCatchup(state.generation, "c1", posts[1:], "en-US", nil, now)
			case "stream":
				for _, post := range posts[1:] {
					applyChatEvent(&state.model, &state.cursor, &chatv1.ConversationEvent{Sequence: post.GetSequence(), Kind: chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED, Post: post}, "en-US", nil, now)
				}
			}
			model := state.snapshot()
			applyChatPostDeleted(&model, receipt)
			if model.Messages[0].Replies != 1 || model.ThreadParent.Replies != 1 || len(model.ThreadMessages) != 1 || model.ThreadMessages[0].ID != "reply" || state.cursor.LastSequence != 3 {
				t.Fatalf("receipt leaked into count/thread or stalled the cursor: model=%+v cursor=%+v", model, state.cursor)
			}
		})
	}
}
