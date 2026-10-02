package application

import (
	"context"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_AGENTUX_PATH_T5_DemoPolicyPlacements_Integration(t *testing.T) {
	ctx := context.Background()
	documents := documentServiceFixture(t).store
	chatDB := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, chatDB)
	chat, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, chatDB.URL, chatDB.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chat.Close)

	const tenant, admin, audience, second = "agentux-path-docs", "admin", "audience", "direct"
	members := []chatcore.Membership{
		{ConversationID: audience, TenantID: tenant, HomeTenantID: tenant, SubjectID: admin, Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory},
		{ConversationID: audience, TenantID: tenant, HomeTenantID: tenant, SubjectID: localAgentDemoAgentID, Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
		{ConversationID: audience, TenantID: tenant, HomeTenantID: tenant, SubjectID: "reviewer", Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
		{ConversationID: audience, TenantID: tenant, HomeTenantID: tenant, SubjectID: "member", Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
	}
	if _, err := chatstore.NewAdapter(chat).CreateConversation(ctx, chatcore.Conversation{ID: audience, TenantID: tenant, Kind: chatcore.PrivateChannel, Name: "Audience", OwnerID: admin}, members, "agentux-path-docs"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	changed, err := ensureLocalAgentDemoPolicyDocument(ctx, documents, chat, tenant, admin, audience, []string{audience}, now)
	if err != nil || changed != 1 {
		t.Fatalf("first placement count=%d err=%v", changed, err)
	}
	firstHits, err := documents.SearchOfficialPlacementLexical(ctx, tenant, audience, localAgentDemoPolicyProbe, "person", admin)
	if err != nil || len(firstHits) != 1 {
		t.Fatalf("administrator audience search=%#v err=%v", firstHits, err)
	}
	documentID := firstHits[0].DocumentID
	placement, err := documents.CurrentPlacement(ctx, tenant, documentID, "placement", audience)
	if err != nil || !placement.IsOfficialPlacement() || placement.CustodianID != admin || !placement.ReviewDueAt.Equal(now.Add(localAgentDemoPolicyReviewDue)) {
		t.Fatalf("official placement=%+v err=%v", placement, err)
	}
	for _, reader := range []string{"reviewer", "member"} {
		hits, err := documents.SearchOfficialPlacementLexical(ctx, tenant, audience, localAgentDemoPolicyProbe, "person", reader)
		if err != nil || len(hits) != 1 || hits[0].DocumentID != documentID {
			t.Fatalf("reader %s audience search=%#v err=%v", reader, hits, err)
		}
	}
	changed, err = ensureLocalAgentDemoPolicyDocument(ctx, documents, chat, tenant, admin, audience, []string{audience, second}, now)
	if err != nil || changed != 1 {
		t.Fatalf("missing-scope placement count=%d err=%v", changed, err)
	}
	for _, scope := range []string{audience, second} {
		for _, reader := range []string{admin, "member"} {
			hits, err := documents.SearchOfficialPlacementLexical(ctx, tenant, scope, localAgentDemoPolicyProbe, "person", reader)
			if err != nil || len(hits) != 1 || hits[0].DocumentID != documentID {
				t.Fatalf("scope=%s reader=%s search=%#v err=%v", scope, reader, hits, err)
			}
		}
	}
	changed, err = ensureLocalAgentDemoPolicyDocument(ctx, documents, chat, tenant, admin, audience, []string{audience, second}, now)
	if err != nil || changed != 0 {
		t.Fatalf("idempotent placement count=%d err=%v", changed, err)
	}
}

func TestTodo_AGENTUX_PATH_T5_DemoPolicyRequiresIndependentReviewer_Integration(t *testing.T) {
	ctx := context.Background()
	documents := documentServiceFixture(t).store
	chatDB := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, chatDB)
	chat, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, chatDB.URL, chatDB.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chat.Close)
	const tenant, admin, audience = "agentux-path-no-reviewer", "admin", "audience"
	members := []chatcore.Membership{
		{ConversationID: audience, TenantID: tenant, HomeTenantID: tenant, SubjectID: admin, Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory},
		{ConversationID: audience, TenantID: tenant, HomeTenantID: tenant, SubjectID: localAgentDemoAgentID, Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
	}
	if _, err := chatstore.NewAdapter(chat).CreateConversation(ctx, chatcore.Conversation{ID: audience, TenantID: tenant, Kind: chatcore.PrivateChannel, Name: "Audience", OwnerID: admin}, members, "agentux-path-no-reviewer"); err != nil {
		t.Fatal(err)
	}
	if changed, err := ensureLocalAgentDemoPolicyDocument(ctx, documents, chat, tenant, admin, audience, []string{audience}, time.Now().UTC()); err == nil || changed != 0 {
		t.Fatalf("conversation without independent reviewer changed=%d err=%v", changed, err)
	}
}
