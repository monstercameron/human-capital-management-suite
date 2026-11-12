package chatstore

import (
	"context"
	"errors"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ISSUE-3706: a second public channel with the name of an existing one (any
// letter case) is refused and nothing is written; a private room with the same
// name is not refused, so the refusal reveals no private room.
func TestIssue3706_PublicChannelNameTakenIsRefused(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	first := chat.Conversation{ID: "issue3706-general", TenantID: "tenant-a", Kind: chat.PublicChannel, Name: "general", OwnerID: "alice", Revision: 1}
	if _, err := s.CreateConversation(ctx, first, []chat.Membership{{ConversationID: first.ID, TenantID: first.TenantID, HomeTenantID: first.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	dup := chat.Conversation{ID: "issue3706-general-2", TenantID: "tenant-a", Kind: chat.PublicChannel, Name: "General", OwnerID: "bob", Revision: 1}
	_, err := s.CreateConversation(ctx, dup, []chat.Membership{{ConversationID: dup.ID, TenantID: dup.TenantID, HomeTenantID: dup.TenantID, SubjectID: "bob", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, "")
	if !errors.Is(err, chat.ErrChannelNameTaken) {
		t.Fatalf("duplicate public channel name: err=%v, want ErrChannelNameTaken", err)
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM chat_conversation WHERE tenant_id=$1 AND lower(name)='general'`, "tenant-a").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("channels named general = %d, want 1", n)
	}
	private := chat.Conversation{ID: "issue3706-private", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "general", OwnerID: "bob", Revision: 1}
	if _, err := s.CreateConversation(ctx, private, []chat.Membership{{ConversationID: private.ID, TenantID: private.TenantID, HomeTenantID: private.TenantID, SubjectID: "bob", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatalf("private room with a public channel's name was refused: %v", err)
	}
}
