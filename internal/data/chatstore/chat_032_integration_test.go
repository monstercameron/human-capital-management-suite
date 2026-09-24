package chatstore

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

// TestTodo_CHAT_032_Integration proves personal sidebar sections and manual
// chat order persist per person against the real recipient store and sync
// across sessions: a write from one session is visible to a second session
// opened afterward for the same person, a further reorder lands as the next
// revision and is again visible, and none of it bleeds into another person's
// sidebar.
func TestTodo_CHAT_032_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	sessionOne := NewRecipientStateStore(s)
	sessionTwo := NewRecipientStateStore(s)
	ctx := context.Background()

	layout := []byte(`{"sections":[{"id":"pinned","name":"Pinned","chats":[{"hostTenantId":"home","conversationId":"c-2"},{"hostTenantId":"home","conversationId":"c-1"}]},{"id":"team","name":"Team"}],"starred":["c-2"]}`)
	saved, err := sessionOne.PutSidebar(ctx, "home", "alice", chatrecipient.Sidebar{Layout: layout}, 1)
	if err != nil || saved.Revision != 2 {
		t.Fatalf("save=%+v err=%v", saved, err)
	}

	// A second session for the same person, opened after the write against the
	// same store, sees the persisted section membership and manual chat order.
	reloaded, err := sessionTwo.Sidebar(ctx, "home", "alice")
	if err != nil {
		t.Fatalf("reload for second session: %v", err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(reloaded.Layout, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(layout, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || reloaded.Revision != 2 {
		t.Fatalf("second session sidebar=%+v revision=%d, want %+v revision=2", got, reloaded.Revision, want)
	}

	// Another person's sidebar in the same home tenant is unaffected.
	other, err := sessionOne.Sidebar(ctx, "home", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if other.Revision != 1 {
		t.Fatalf("unrelated person's sidebar was touched: %+v", other)
	}

	// A manual reorder from the original session lands as the next revision
	// and is visible from the second session, proving order (not just section
	// membership) persists and syncs.
	reordered := []byte(`{"sections":[{"id":"pinned","name":"Pinned","chats":[{"hostTenantId":"home","conversationId":"c-1"},{"hostTenantId":"home","conversationId":"c-2"}]},{"id":"team","name":"Team"}],"starred":["c-2"]}`)
	if _, err := sessionOne.PutSidebar(ctx, "home", "alice", chatrecipient.Sidebar{Layout: reordered}, 2); err != nil {
		t.Fatal(err)
	}
	final, err := sessionTwo.Sidebar(ctx, "home", "alice")
	if err != nil {
		t.Fatal(err)
	}
	var finalLayout struct {
		Sections []struct {
			ID    string `json:"id"`
			Chats []struct {
				ConversationID string `json:"conversationId"`
			} `json:"chats"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(final.Layout, &finalLayout); err != nil {
		t.Fatal(err)
	}
	if len(finalLayout.Sections) == 0 || len(finalLayout.Sections[0].Chats) == 0 || finalLayout.Sections[0].Chats[0].ConversationID != "c-1" {
		t.Fatalf("manual chat order did not persist across the reorder: %+v", finalLayout)
	}
	if final.Revision != 3 {
		t.Fatalf("reorder revision=%d, want 3", final.Revision)
	}
}
