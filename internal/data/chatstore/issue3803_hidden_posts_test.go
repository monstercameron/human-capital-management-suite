package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestIssue3803_PostsHiddenFrom_Integration: an outcome receipt sits under a
// message, so the reader's audience decides which messages may carry one: a
// restricted message is hidden from a member it leaves out, never from its
// author or its audience, and a post of another tenant is never named.
func TestIssue3803_PostsHiddenFrom_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	adapter := NewAdapter(s)
	ctx := context.Background()
	const tenant, room = "tenant-a", "night"
	seedConversationRow(t, s, Conversation{ID: room, TenantID: tenant, Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, w8Members("owner", "ana", "ben"))
	send := func(ctx context.Context, key string) Post {
		t.Helper()
		post, err := s.sendPostRaw(ctx, SendRequest{TenantID: tenant, HomeTenantID: tenant, ConversationID: room, AuthorID: "owner", ClientKey: key, Body: key})
		if err != nil {
			t.Fatal(err)
		}
		return post
	}
	restricted := send(chat.WithPostAudience(ctx, chat.PostAudience{Segments: []string{"Night shift"}, Restricted: true, Members: []string{"ana", "owner"}}), "r1")
	open := send(ctx, "o1")
	addressed := send(chat.WithPostAudience(ctx, chat.PostAudience{Segments: []string{"Night shift"}}), "a1")
	ids := []string{restricted.ID, open.ID, addressed.ID}
	for person, want := range map[string]bool{"ben": true, "ana": false, "owner": false} {
		hidden, err := adapter.PostsHiddenFrom(ctx, tenant, person, ids)
		if err != nil {
			t.Fatal(err)
		}
		if hidden[restricted.ID] != want || hidden[open.ID] || hidden[addressed.ID] {
			t.Fatalf("%s: hidden=%v, restricted post hidden want %v", person, hidden, want)
		}
	}
	other, err := adapter.PostsHiddenFrom(ctx, "tenant-b", "ben", ids)
	if err == nil && other[restricted.ID] {
		t.Fatalf("another tenant must not be told about this tenant's posts: %v", other)
	}
	if _, err := adapter.PostsHiddenFrom(ctx, tenant, "", ids); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("a blank person must be refused: %v", err)
	}
	if hidden, err := adapter.PostsHiddenFrom(ctx, tenant, "ben", nil); err != nil || len(hidden) != 0 {
		t.Fatalf("no ids, no answer: %v %v", hidden, err)
	}
}
