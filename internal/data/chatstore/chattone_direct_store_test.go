package chatstore

import (
	"context"
	"testing"
)

// TestChattoneConversationIsDirect: a direct or group conversation is told apart
// from a channel, and a conversation that does not exist is not direct.
func TestChattoneConversationIsDirect(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	for id, kind := range map[string]string{"dm": "DIRECT", "group": "GROUP", "public": "PUBLIC_CHANNEL", "private": "PRIVATE_CHANNEL"} {
		seedConversationRow(t, s, Conversation{ID: id, TenantID: "tenant-a", Kind: kind, OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	}
	for id, want := range map[string]bool{"dm": true, "group": true, "public": false, "private": false, "missing": false} {
		got, err := s.ConversationIsDirect(ctx, "tenant-a", id)
		if err != nil || got != want {
			t.Fatalf("%s: %v %v", id, got, err)
		}
	}
	// Another workspace cannot see this one's conversations.
	if got, err := s.ConversationIsDirect(ctx, "tenant-b", "dm"); err != nil || got {
		t.Fatalf("a conversation of another workspace: %v %v", got, err)
	}
}
