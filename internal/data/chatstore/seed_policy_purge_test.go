package chatstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_AGENTP_PurgeConversationsDeletesAudiencePolicy(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "seeded-private", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	seedConversationRow(t, s, Conversation{ID: "other-private", TenantID: "tenant-b", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	if _, err := s.PutAudiencePolicy(ctx, "tenant-a", "seeded-private", 0, AudiencePolicy{RoleMode: 1, Classification: "T2", Residency: "us"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutAudiencePolicy(ctx, "tenant-b", "other-private", 0, AudiencePolicy{RoleMode: 1, Classification: "T3", Residency: "eu"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PurgeConversations(ctx, "tenant-a", []string{"seeded-private"}); err != nil {
		t.Fatalf("purge conversation with policy: %v", err)
	}
	if exists, err := s.ConversationExists(ctx, "tenant-a", "seeded-private"); err != nil || exists {
		t.Fatalf("conversation after purge: exists=%t err=%v", exists, err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_channel_policy WHERE tenant_id=$1 AND conversation_id=$2`, "tenant-a", "seeded-private").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("policy rows after purge = %d, want 0", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_channel_policy WHERE tenant_id=$1 AND conversation_id=$2`, "tenant-b", "other-private").Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("unrelated tenant policy rows after purge = %d, want 1", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
