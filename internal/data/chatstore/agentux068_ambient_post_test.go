package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestAgentUXAmbient_AgentPost_Integration(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	tenant, conversation := "tenant-a", "ambient-reminders"
	c := chat.Conversation{ID: conversation, TenantID: tenant, Kind: chat.PublicChannel, OwnerID: "alice", Revision: 1}
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{{TenantID: tenant, HomeTenantID: tenant, ConversationID: conversation, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostAmbientAgentMessage(ctx, tenant, conversation, "reminder", "Timesheets are due Friday", "key-1"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("an agent that is not installed posted: %v", err)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_app_installation(id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at) VALUES($1,$2,$3,'reminder',1,'{}',ARRAY['chat.posts.read'],'ACTIVE','alice',1,$4,$4)`, tenant+":"+conversation+":reminder", tenant, conversation, time.Now().Add(-time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	first, err := s.PostAmbientAgentMessage(ctx, tenant, conversation, "reminder", "Timesheets are due Friday", "key-1")
	if err != nil || first.ID == "" || first.AuthorID != "reminder" {
		t.Fatalf("installed agent's post: %+v %v", first, err)
	}
	again, err := s.PostAmbientAgentMessage(ctx, tenant, conversation, "reminder", "Timesheets are due Friday", "key-1")
	if err != nil || again.ID != first.ID {
		t.Fatalf("a retried delivery posted twice: %+v %v", again, err)
	}
	if _, err := s.PostAmbientAgentMessage(ctx, tenant, conversation, "task-catcher", "Not installed here", "key-2"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("another agent posted through this agent's installation: %v", err)
	}
	if _, err := s.PostAmbientAgentMessage(ctx, tenant, conversation, "reminder", " ", "key-3"); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("an empty message was written: %v", err)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_conversation SET kind='PRIVATE_CHANNEL' WHERE tenant_id=$1 AND id=$2`, tenant, conversation)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostAmbientAgentMessage(ctx, tenant, conversation, "reminder", "Private now", "key-4"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("an agent posted into a channel that is no longer public: %v", err)
	}
}
