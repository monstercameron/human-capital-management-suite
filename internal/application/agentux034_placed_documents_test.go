package application

import (
	"context"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// The count and titles on an Agent setup placement row come from one reader of
// the officially placed documents of that conversation. A conversation with a
// document placed in it reports the document to the administrator and to an
// ordinary member of the audience alike, a conversation with none reports zero
// (the page then warns in words), and a person outside the conversation is told
// nothing. The demo preparation places the policy document the same way, so a
// fresh local cell can answer.
func TestTodo_AGENTUX_034(t *testing.T) {
	ctx := context.Background()
	documents := documentServiceFixture(t).store
	chatDB := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, chatDB)
	chat, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, chatDB.URL, chatDB.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chat.Close)

	const tenant, admin, placed, empty = "agentux-034", "admin", "general", "empty-room"
	adapter := chatstore.NewAdapter(chat)
	for _, room := range []string{placed, empty} {
		members := []chatcore.Membership{
			{ConversationID: room, TenantID: tenant, HomeTenantID: tenant, SubjectID: admin, Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory},
			{ConversationID: room, TenantID: tenant, HomeTenantID: tenant, SubjectID: localAgentDemoAgentID, Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
			{ConversationID: room, TenantID: tenant, HomeTenantID: tenant, SubjectID: "reviewer", Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
			{ConversationID: room, TenantID: tenant, HomeTenantID: tenant, SubjectID: "member", Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
		}
		if _, err := adapter.CreateConversation(ctx, chatcore.Conversation{ID: room, TenantID: tenant, Kind: chatcore.PrivateChannel, Name: room, OwnerID: admin}, members, "agentux-034-"+room); err != nil {
			t.Fatal(err)
		}
	}
	reader := documentService{store: documents}

	// Nothing placed yet: every reader sees zero, which the page says in words.
	for _, subject := range []string{admin, "member"} {
		rows, err := reader.ListPersonaAdminPlacementDocuments(ctx, tenant, subject, placed)
		if err != nil || len(rows) != 0 {
			t.Fatalf("%s sees %+v before any placement (err=%v)", subject, rows, err)
		}
	}

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	if changed, err := ensureLocalAgentDemoPolicyDocument(ctx, documents, chat, tenant, admin, placed, []string{placed}, now); err != nil || changed != 1 {
		t.Fatalf("demo placement changed=%d err=%v", changed, err)
	}
	for _, subject := range []string{admin, "reviewer", "member"} {
		rows, err := reader.ListPersonaAdminPlacementDocuments(ctx, tenant, subject, placed)
		if err != nil || len(rows) != 1 || rows[0].Title != localAgentDemoPolicyTitle || rows[0].DocumentID == "" || rows[0].VersionID == "" {
			t.Fatalf("%s sees %+v after the placement (err=%v), want the one placed policy document with its version", subject, rows, err)
		}
	}
	// The other conversation has nothing placed in it.
	if rows, err := reader.ListPersonaAdminPlacementDocuments(ctx, tenant, admin, empty); err != nil || len(rows) != 0 {
		t.Fatalf("the unplaced conversation lists %+v (err=%v)", rows, err)
	}
	// A person outside the conversation reads none of it.
	if rows, err := reader.ListPersonaAdminPlacementDocuments(ctx, tenant, "outsider", placed); err != nil || len(rows) != 0 {
		t.Fatalf("an outsider is told of %+v (err=%v)", rows, err)
	}
}
