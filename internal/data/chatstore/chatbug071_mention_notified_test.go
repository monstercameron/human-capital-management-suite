package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

// TestTodo_CHATBUG_071_Integration follows a mention from the send to the
// mentioned member's own count, through the service and the store with nothing
// written by hand. The count is what the conversation list draws as the mention
// number, and it is read from the stored reference, so the reference the
// service stores has to be the one the counter looks for. A member who is not
// mentioned gets the unread message and no mention; the author gets neither;
// and a mention of somebody who is not in the conversation is refused, which is
// why the page keeps such a name as plain text and offers to add the person.
func TestTodo_CHATBUG_071_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	const tenant, room = "tenant-a", "chatbug071"
	member := func(subject string, role chat.MembershipRole) chat.Membership {
		return chat.Membership{ConversationID: room, TenantID: tenant, HomeTenantID: tenant, SubjectID: subject, Role: role, HistoryVisibility: chat.FullHistory}
	}
	channel := chat.Conversation{ID: room, TenantID: tenant, Kind: chat.PrivateChannel, Name: "scratch", OwnerID: "walt", Revision: 1}
	if _, err := s.CreateConversation(ctx, channel, []chat.Membership{member("walt", chat.Manager), member("loretta", chat.Member), member("eddie", chat.Member)}, ""); err != nil {
		t.Fatal(err)
	}
	service := chat.NewService(s, func() time.Time { return time.Now().UTC() })
	service.SetAuthority(forwardingAuthority{store: s})
	walt := chat.Principal{TenantID: tenant, SubjectID: "walt"}
	send := func(key, body string, refs ...chat.Reference) (chat.Post, error) {
		return service.SendPostWithReferences(ctx, chat.SendPostWithReferencesRequest{
			SendPostRequest: chat.SendPostRequest{Principal: walt, TenantID: tenant, ConversationID: room, Body: body, IdempotencyKey: key},
			References:      refs,
		})
	}
	post, err := send("mention", "hello @Loretta Haynes", chat.Reference{Kind: chat.PersonMention, TenantID: tenant, ID: "loretta", Display: "Loretta Haynes", ConversationID: room})
	if err != nil {
		t.Fatalf("a mention of a member was refused: %v", err)
	}
	if len(post.References) != 1 || post.References[0].Kind != chat.PersonMention || post.References[0].ID != "loretta" {
		t.Fatalf("the stored message does not carry the person's reference: %+v", post.References)
	}

	recipients := NewRecipientStateStore(s.Store)
	counts := func(subject string) chatrecipient.Counts {
		t.Helper()
		got, err := recipients.Counts(ctx, chatrecipient.Identity{HostTenantID: tenant, HomeTenantID: tenant, SubjectID: subject, ConversationID: room})
		if err != nil {
			t.Fatalf("counts for %s: %v", subject, err)
		}
		return got
	}
	if got := counts("loretta"); got.Unread != 1 || got.Mentions != 1 {
		t.Fatalf("the mentioned member's counts = %+v, want one unread message and one mention", got)
	}
	if got := counts("eddie"); got.Unread != 1 || got.Mentions != 0 {
		t.Fatalf("a member who was not mentioned has counts %+v, want one unread message and no mention", got)
	}
	if got := counts("walt"); got.Unread != 0 || got.Mentions != 0 {
		t.Fatalf("the author's own counts = %+v, want none", got)
	}

	if _, err := send("outside", "hello @Sofia Beltran", chat.Reference{Kind: chat.PersonMention, TenantID: tenant, ID: "sofia", Display: "Sofia Beltran", ConversationID: room}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a mention of someone outside the conversation = %v, want it refused", err)
	}
	if got := counts("loretta"); got.Unread != 1 || got.Mentions != 1 {
		t.Fatalf("the refused message changed the member's counts: %+v", got)
	}
}
