package chatstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// TestTodo_AGENTP_011_Security_SameRoomNonRecipient covers the member the
// other tests leave out: someone who sits in the same conversation as the
// recipient of a private agent answer. Membership of the room is not enough.
// That member finds nothing of the answer in any search source and their
// unread and mention counts do not move, while the recipient still finds it,
// marked private and owned by them.
func TestTodo_AGENTP_011_Security_SameRoomNonRecipient(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	const room = "shared-room"
	seedConversationRow(t, s,
		Conversation{ID: room, TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: "planning", Description: "team planning", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1},
		[]Membership{
			{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"},
			{MemberID: "bob", HomeTenantID: "tenant", Role: "member", State: "active"},
		})
	question, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: room, AuthorID: "alice", ClientKey: "question", Body: "what is the planning timeline"})
	if err != nil {
		t.Fatal(err)
	}
	recipients := NewRecipientStateStore(s)
	counts := func(t *testing.T, person string) chatrecipient.Counts {
		t.Helper()
		got, err := recipients.Counts(ctx, chatrecipient.Identity{HostTenantID: "tenant", HomeTenantID: "tenant", SubjectID: person, ConversationID: room})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	before := counts(t, "bob")
	if before.Unread != 1 {
		t.Fatalf("the other member should have exactly the question unread before the answer: %+v", before)
	}

	// The private answer goes to alice only. Its words appear nowhere else.
	if _, err = NewDurableEphemeralStore(s).PutEphemeral(ctx, chat.EphemeralPost{
		ID: "alice-answer", TenantID: "tenant", ConversationID: room, ThreadID: question.ID,
		RecipientHomeTenantID: "tenant", RecipientSubjectID: "alice",
		Body: "severance figure for alice only", OnlyVisibleToYou: true,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		DurableCopyConversationID: room, DurableCopyPostID: question.ID, ThreadLink: "/chat#thread=" + question.ID,
	}); err != nil {
		t.Fatal(err)
	}

	if after := counts(t, "bob"); after != before {
		t.Fatalf("a private answer to someone else moved the other member's counts: before %+v, after %+v", before, after)
	}

	registry := chatsearch.NewRegistry()
	// Both people are members, so the room-level authority admits both. Only
	// the recipient binding inside the answer source can keep bob out.
	authority := func(_ context.Context, _ chatsearch.Actor, row chatsearch.Row) (bool, error) {
		return row.Target.ConversationID == room, nil
	}
	if err = s.RegisterChatSearch(registry, authority); err != nil {
		t.Fatal(err)
	}
	search := func(t *testing.T, person string, kind chatsearch.Kind, query string) chatsearch.Response {
		t.Helper()
		got, err := registry.Search(ctx, chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: person}, Query: query, Filters: chatsearch.Filters{Kind: kind}, At: time.Now().UTC(), Limit: 50})
		if err != nil {
			t.Fatalf("%s searching %s for %q: %v", person, kind, query, err)
		}
		return got
	}
	kinds := []chatsearch.Kind{chatsearch.Message, chatsearch.Thread, chatsearch.Conversation, chatsearch.File, chatsearch.Pin, chatsearch.Todo, chatsearch.Poll, chatsearch.AgentAnswer}
	for _, kind := range kinds {
		for _, query := range []string{"severance", "severance figure for alice only", "alice only"} {
			if got := search(t, "bob", kind, query); len(got.Groups) != 0 || got.NextCursor != "" {
				t.Errorf("the other member's %s search for %q returned %+v", kind, query, got)
			}
		}
	}
	// The fixture is not vacuous: bob does find the durable question, and the
	// recipient does find her answer.
	if got := search(t, "bob", chatsearch.Message, "planning timeline"); len(got.Groups) != 1 || got.Groups[0].Count != 1 {
		t.Fatalf("the other member cannot find the ordinary message in the same room: %+v", got)
	}
	mine := search(t, "alice", chatsearch.AgentAnswer, "severance")
	if len(mine.Groups) != 1 || len(mine.Groups[0].Rows) != 1 {
		t.Fatalf("the recipient cannot find her own answer: %+v", mine)
	}
	if row := mine.Groups[0].Rows[0]; !row.Private || row.OwnerID != "alice" || row.Target.ConversationID != room {
		t.Fatalf("the recipient's answer row is not a private row of hers: %+v", row)
	}
	// The answer is not a message for anyone, the recipient included: it does
	// not enter the durable message index.
	if got := search(t, "alice", chatsearch.Message, "severance"); len(got.Groups) != 0 {
		t.Fatalf("the private answer entered the message index: %+v", got)
	}
}
