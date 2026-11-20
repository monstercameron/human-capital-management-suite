package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestIssue3791_WorkLinksRecordOnceAndListNewestFirst_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	for i, id := range []string{"intent-1", "intent-2"} {
		if err := s.LinkWork(ctx, WorkLink{TenantID: "tenant-a", ConversationID: "c-1", Kind: WorkLinkIntentProposal, SourceID: id, ProposedByID: "u-1", AgentInstallationID: "tenant-a:c-1:agent", IntentType: "promotion", LinkedAt: at.Add(time.Duration(i) * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	// A retry of the same proposal changes nothing and is not an error.
	if err := s.LinkWork(ctx, WorkLink{TenantID: "tenant-a", ConversationID: "c-1", Kind: WorkLinkIntentProposal, SourceID: "intent-1", ProposedByID: "u-2", LinkedAt: at.Add(5 * time.Hour)}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	list, err := s.ListWorkLinks(ctx, "tenant-a", "c-1", 10)
	if err != nil || len(list) != 2 || list[0].SourceID != "intent-2" || list[1].SourceID != "intent-1" {
		t.Fatalf("list %+v %v", list, err)
	}
	if list[1].ProposedByID != "u-1" || list[1].IntentType != "promotion" || !list[1].LinkedAt.Equal(at) {
		t.Fatalf("retry rewrote the link: %+v", list[1])
	}
	if list, err = s.ListWorkLinks(ctx, "tenant-a", "c-1", 1); err != nil || len(list) != 1 {
		t.Fatalf("limit %+v %v", list, err)
	}
}

func TestIssue3791_WorkLinksStayInTheirConversationAndTenant_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	seedConversationRow(t, s, Conversation{ID: "c-2", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	ctx := context.Background()
	if err := s.LinkWork(ctx, WorkLink{TenantID: "tenant-a", ConversationID: "c-1", Kind: WorkLinkIntentProposal, SourceID: "intent-1", ProposedByID: "u-1"}); err != nil {
		t.Fatal(err)
	}
	if list, err := s.ListWorkLinks(ctx, "tenant-a", "c-2", 10); err != nil || len(list) != 0 {
		t.Fatalf("other conversation sees %+v %v", list, err)
	}
	if list, err := s.ListWorkLinks(ctx, "tenant-b", "c-1", 10); err != nil || len(list) != 0 {
		t.Fatalf("other tenant sees %+v %v", list, err)
	}
	// A link cannot be made for a conversation that is not the tenant's.
	if err := s.LinkWork(ctx, WorkLink{TenantID: "tenant-b", ConversationID: "c-1", Kind: WorkLinkIntentProposal, SourceID: "intent-9", ProposedByID: "u-1"}); err == nil {
		t.Fatal("linked work to another tenant's conversation")
	}
	for _, bad := range []WorkLink{
		{TenantID: "tenant-a", ConversationID: "c-1", Kind: WorkLinkIntentProposal, SourceID: " ", ProposedByID: "u-1"},
		{TenantID: "tenant-a", ConversationID: "c-1", Kind: "OTHER", SourceID: "x", ProposedByID: "u-1"},
		{TenantID: "", ConversationID: "c-1", Kind: WorkLinkIntentProposal, SourceID: "x", ProposedByID: "u-1"},
	} {
		if err := s.LinkWork(ctx, bad); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
}

func TestIssue3791_WorkLinksAreNeverEdited_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	if err := s.LinkWork(ctx, WorkLink{TenantID: "tenant-a", ConversationID: "c-1", Kind: WorkLinkIntentProposal, SourceID: "intent-1", ProposedByID: "u-1"}); err != nil {
		t.Fatal(err)
	}
	err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_work_link SET proposed_by_id='u-9' WHERE tenant_id='tenant-a'`)
		return err
	})
	if err == nil {
		t.Fatal("a link was edited")
	}
}
