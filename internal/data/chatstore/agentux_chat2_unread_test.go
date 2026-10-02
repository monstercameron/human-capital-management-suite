package chatstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

func TestAgentUXChat2_UnreadAgentAnswerExcludesOnlyViewerAuthoredPosts(t *testing.T) {
	store, _ := chatFixture(t)
	ctx := context.Background()
	seedConversation(t, store, "host")
	if err := store.execTenant(ctx, "host", `UPDATE chat_membership SET home_tenant_id='host' WHERE tenant_id='host' AND conversation_id='c-1' AND member_id='u-1'`); err != nil {
		t.Fatal(err)
	}
	if err := store.execTenant(ctx, "host", `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,state,history_visibility) VALUES('host','c-1','host','policy-agent','active','FULL_HISTORY')`); err != nil {
		t.Fatal(err)
	}
	if err := store.execTenant(ctx, "host", `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('question','host','c-1','u-1','host',1,'Question'),('answer','host','c-1','policy-agent','host',2,'Answer')`); err != nil {
		t.Fatal(err)
	}
	counts, err := NewRecipientStateStore(store).Counts(ctx, chatrecipient.Identity{HostTenantID: "host", HomeTenantID: "host", SubjectID: "u-1", ConversationID: "c-1"})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Unread != 1 {
		t.Fatalf("unread=%d, want only the agent-authored answer", counts.Unread)
	}
}
