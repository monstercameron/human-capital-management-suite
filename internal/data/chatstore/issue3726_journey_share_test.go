package chatstore

import (
	"context"
	"testing"
	"time"
)

func TestIssue3726_JourneySharesAreFoundBySource_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	seedConversationRow(t, s, Conversation{ID: "c-2", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	ctx := context.Background()
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	for i, c := range []string{"c-1", "c-2"} {
		if err := s.LinkWork(ctx, WorkLink{TenantID: "tenant-a", ConversationID: c, Kind: WorkLinkJourneyShare, SourceID: "journey-1", ProposedByID: "u-1", LinkedAt: at.Add(time.Duration(i) * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	// Sharing it again from the same conversation keeps the first record.
	if err := s.LinkWork(ctx, WorkLink{TenantID: "tenant-a", ConversationID: "c-1", Kind: WorkLinkJourneyShare, SourceID: "journey-1", ProposedByID: "u-2", LinkedAt: at.Add(9 * time.Hour)}); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	// A proposal with the same id is a different kind and is not a share.
	if err := s.LinkWork(ctx, WorkLink{TenantID: "tenant-a", ConversationID: "c-1", Kind: WorkLinkIntentProposal, SourceID: "journey-1", ProposedByID: "u-1", LinkedAt: at}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListWorkLinksBySource(ctx, "tenant-a", WorkLinkJourneyShare, "journey-1", 10)
	if err != nil || len(got) != 2 || got[0].ConversationID != "c-2" || got[1].ConversationID != "c-1" || got[1].ProposedByID != "u-1" {
		t.Fatalf("by source %+v %v", got, err)
	}
	if got, err = s.ListWorkLinksBySource(ctx, "tenant-a", WorkLinkJourneyShare, "journey-1", 1); err != nil || len(got) != 1 {
		t.Fatalf("limit %+v %v", got, err)
	}
}

func TestIssue3726_JourneySharesStayInTheirTenant_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	if err := s.LinkWork(ctx, WorkLink{TenantID: "tenant-a", ConversationID: "c-1", Kind: WorkLinkJourneyShare, SourceID: "journey-1", ProposedByID: "u-1"}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListWorkLinksBySource(ctx, "tenant-b", WorkLinkJourneyShare, "journey-1", 10); err != nil || len(got) != 0 {
		t.Fatalf("other tenant sees %+v %v", got, err)
	}
	if err := s.LinkWork(ctx, WorkLink{TenantID: "tenant-b", ConversationID: "c-1", Kind: WorkLinkJourneyShare, SourceID: "journey-9", ProposedByID: "u-1"}); err == nil {
		t.Fatal("linked a journey to another tenant's conversation")
	}
	if _, err := s.ListWorkLinksBySource(ctx, "tenant-a", "OTHER", "journey-1", 10); err == nil {
		t.Fatal("listed an unknown kind")
	}
	if _, err := s.ListWorkLinksBySource(ctx, "tenant-a", WorkLinkJourneyShare, " ", 10); err == nil {
		t.Fatal("listed a blank source")
	}
}
