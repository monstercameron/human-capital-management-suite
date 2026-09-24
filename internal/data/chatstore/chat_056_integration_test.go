package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// chat056IntegrationAuthority mirrors the production discovery/join policy
// shape against a real membership row: present and current membership
// authorizes the discover/read paths, and anyone may attempt ActionJoin on a
// public channel whether or not they are already a member.
type chat056IntegrationAuthority struct{ store *Adapter }

func (a chat056IntegrationAuthority) Authorize(ctx context.Context, p chat.Principal, c chat.Conversation, _ chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	in := chatpolicy.Input{
		Principal: chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: 1},
		Channel:   chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Private: c.Kind != chat.PublicChannel, Enabled: !c.Archived, Revision: c.Revision},
		Now:       now,
	}
	m, err := a.store.GetMembership(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
	if err == nil && m.LeftAt == nil {
		in.HasMembership = true
		in.Membership = chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: m.Revision, JoinedAt: now.Add(-time.Hour)}
	}
	return in, nil
}

func chat056ConversationIDs(cs []chat.Conversation) []string {
	ids := make([]string, 0, len(cs))
	for _, c := range cs {
		ids = append(ids, c.ID)
	}
	return ids
}

func chat056Contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// TestTodo_CHAT_056_Integration is the CHAT-056 INTEGRATION matrix test. It
// proves the discoverable listing and self-join paths against the real
// tenant-scoped PostgreSQL store: a plain listing shows only the caller's
// own rooms, opting in adds a not-yet-joined public channel but never a
// private one, a self-join of a public channel persists a real membership
// row, and self-joining a private channel is refused before any write.
func TestTodo_CHAT_056_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	tenant := "tenant-a"

	mine := chat.Conversation{ID: "mine", TenantID: tenant, Kind: chat.PrivateChannel, Name: "mine", OwnerID: "alice", Revision: 1}
	if _, err := s.CreateConversation(ctx, mine, []chat.Membership{{ConversationID: mine.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	open := chat.Conversation{ID: "open", TenantID: tenant, Kind: chat.PublicChannel, Name: "open", OwnerID: "bob", Revision: 1}
	if _, err := s.CreateConversation(ctx, open, []chat.Membership{{ConversationID: open.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: "bob", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	hidden := chat.Conversation{ID: "hidden", TenantID: tenant, Kind: chat.PrivateChannel, Name: "hidden", OwnerID: "carol", Revision: 1}
	if _, err := s.CreateConversation(ctx, hidden, []chat.Membership{{ConversationID: hidden.ID, TenantID: tenant, HomeTenantID: tenant, SubjectID: "carol", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}

	service := chat.NewService(s, func() time.Time { return time.Now().UTC() })
	service.SetAuthority(chat056IntegrationAuthority{store: s})
	principal := chat.Principal{TenantID: tenant, SubjectID: "alice"}

	plain, err := service.ListConversations(ctx, chat.ListConversationsRequest{Principal: principal, TenantID: tenant})
	if err != nil {
		t.Fatal(err)
	}
	plainIDs := chat056ConversationIDs(plain.Conversations)
	if len(plainIDs) != 1 || plainIDs[0] != "mine" {
		t.Fatalf("plain listing = %v, want only the caller's own room", plainIDs)
	}

	discoverable, err := service.ListConversations(ctx, chat.ListConversationsRequest{Principal: principal, TenantID: tenant, IncludeDiscoverable: true})
	if err != nil {
		t.Fatal(err)
	}
	discoverableIDs := chat056ConversationIDs(discoverable.Conversations)
	if !chat056Contains(discoverableIDs, "mine") || !chat056Contains(discoverableIDs, "open") || chat056Contains(discoverableIDs, "hidden") {
		t.Fatalf("discoverable listing = %v, want mine+open but never the private hidden channel", discoverableIDs)
	}

	joined, err := service.AddMembership(ctx, chat.AddMembershipRequest{Principal: principal, Membership: chat.Membership{TenantID: tenant, HomeTenantID: tenant, ConversationID: "open", SubjectID: "alice", Role: chat.Manager}})
	if err != nil {
		t.Fatalf("self-join of a discoverable public channel = %v", err)
	}
	if joined.Role != chat.Member {
		t.Fatalf("self-join role = %v, want MEMBER even though the request asked for MANAGER", joined.Role)
	}
	persisted, err := s.GetMembership(ctx, tenant, "open", tenant, "alice")
	if err != nil || persisted.LeftAt != nil {
		t.Fatalf("self-join did not persist a live membership row: %+v %v", persisted, err)
	}

	if _, err := service.AddMembership(ctx, chat.AddMembershipRequest{Principal: principal, Membership: chat.Membership{TenantID: tenant, HomeTenantID: tenant, ConversationID: "hidden", SubjectID: "alice"}}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("self-join of a private channel = %v, want permission denied", err)
	}
	if _, err := s.GetMembership(ctx, tenant, "hidden", tenant, "alice"); err == nil {
		t.Fatal("refused private self-join still wrote a membership row")
	}
}
