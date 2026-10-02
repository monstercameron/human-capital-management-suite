package chatrecordstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATMOD_005(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	adapter := chatstore.NewAdapter(s.Chat)
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "reviewer"}
	c := chat.Conversation{ID: "room", TenantID: p.TenantID, Kind: chat.PrivateChannel, OwnerID: p.SubjectID, Revision: 1}
	if _, err := adapter.CreateConversation(ctx, c, []chat.Membership{{TenantID: p.TenantID, ConversationID: c.ID, HomeTenantID: p.TenantID, SubjectID: p.SubjectID, Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, ""); err != nil {
		t.Fatal(err)
	}
	post, err := adapter.SendPost(ctx, chat.SendPostRequest{TenantID: p.TenantID, ConversationID: c.ID, IdempotencyKey: "message"}, chat.Post{AuthorID: p.SubjectID, AuthorHomeTenantID: p.TenantID, Body: "context"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Chat.RunTenantTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,'WORKSPACE_ADMIN')`, p.TenantID, p.SubjectID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.PutReport(ctx, chatrecords.Report{TenantID: p.TenantID, ConversationID: c.ID, TargetID: post.ID, ReportID: "one", ReporterID: "reporter", Reason: "spam", CreatedAt: time.Now(), State: "OPEN"}); err != nil {
		t.Fatal(err)
	}
	items, err := s.SearchModeration(ctx, p, p.TenantID, "spam")
	if err != nil || len(items) != 1 || items[0].ReporterID != "reporter" || items[0].Message.Body != "context" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	p.SubjectID = "outsider"
	items, err = s.SearchModeration(ctx, p, p.TenantID, "")
	if err != nil || len(items) != 0 {
		t.Fatalf("private queue leaked %+v %v", items, err)
	}
}
