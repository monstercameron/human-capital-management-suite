package chatstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

// TestTodo_CHATUX_021_SystemLineIsNotUnread: the "added people" system line is
// a record for readers of the conversation, not a message to catch up on, so a
// person added by somebody else, and everyone else in the room, see an unread
// count that leaves it out.
func TestTodo_CHATUX_021_SystemLineIsNotUnread(t *testing.T) {
	store, _ := chatFixture(t)
	ctx := context.Background()
	seedConversation(t, store, "host")
	if err := store.execTenant(ctx, "host", `UPDATE chat_membership SET home_tenant_id='host' WHERE tenant_id='host' AND conversation_id='c-1' AND member_id='u-1'`); err != nil {
		t.Fatal(err)
	}
	if err := store.execTenant(ctx, "host", `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,state,history_visibility) VALUES('host','c-1','host','adder','active','FULL_HISTORY')`); err != nil {
		t.Fatal(err)
	}
	line := chat.MembershipAddedBody("u-1")
	if err := store.execTenant(ctx, "host", `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('hello','host','c-1','adder','host',1,'Welcome'),('added','host','c-1','adder','host',2,$1)`, line); err != nil {
		t.Fatal(err)
	}
	counts, err := NewRecipientStateStore(store).Counts(ctx, chatrecipient.Identity{HostTenantID: "host", HomeTenantID: "host", SubjectID: "u-1", ConversationID: "c-1"})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Unread != 1 {
		t.Fatalf("unread=%d, want the one real message and not the system line", counts.Unread)
	}
}
