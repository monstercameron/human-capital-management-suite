package chatrecipient

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_CHAT_032_SidebarReloadPreservesPersonalOrderAndFiltersRevokedRooms(t *testing.T) {
	stored := Sidebar{Layout: []byte(`{"sections":[{"id":"projects","name":"Projects","collapsed":false,"chats":[{"hostTenantId":"host","conversationId":"open"},{"hostTenantId":"host","conversationId":"revoked"}]},{"id":"direct","name":"Direct","chats":[{"hostTenantId":"host","conversationId":"second"}]}],"starred":["open","revoked"],"filters":{"projects":"unread"}}`), Revision: 8}
	db := &repo{sidebar: stored}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	s := &Service{Conversations: conversations{allowed: true, denied: map[string]bool{"host/revoked": true}}, Repo: db}

	got, err := s.Sidebar(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	var layout struct {
		Sections []struct {
			ID        string `json:"id"`
			Collapsed bool   `json:"collapsed"`
			Chats     []struct {
				ConversationID string `json:"conversationId"`
			} `json:"chats"`
		} `json:"sections"`
		Starred []string          `json:"starred"`
		Filters map[string]string `json:"filters"`
	}
	if err := json.Unmarshal(got.Layout, &layout); err != nil {
		t.Fatal(err)
	}
	if got.Revision != 8 || len(layout.Sections) != 2 || layout.Sections[0].ID != "projects" || layout.Sections[0].Collapsed || len(layout.Sections[0].Chats) != 1 || layout.Sections[0].Chats[0].ConversationID != "open" || layout.Sections[1].ID != "direct" || layout.Sections[1].Chats[0].ConversationID != "second" {
		t.Fatalf("sidebar order or authorized rooms changed: %+v", layout.Sections)
	}
	if len(layout.Starred) != 1 || layout.Starred[0] != "open" || layout.Filters["projects"] != "unread" {
		t.Fatalf("personal star/filter state = %+v / %+v", layout.Starred, layout.Filters)
	}
	if string(db.sidebar.Layout) != string(stored.Layout) {
		t.Fatal("read-time authorization filtering mutated the stored owner state")
	}
}
