package chatrecipient

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHATSIDE_001_ManualFlagSurvivesFiltering: the flag that says a person
// ordered a section by hand is kept when the read rewrites the layout to leave
// out a conversation they can no longer see.
func TestTodo_CHATSIDE_001_ManualFlagSurvivesFiltering(t *testing.T) {
	stored := Sidebar{Layout: []byte(`{"sections":[{"id":"channels","name":"Channels","manual":true,"chats":[{"hostTenantId":"host","conversationId":"open"},{"hostTenantId":"host","conversationId":"revoked"}]},{"id":"custom-1","name":"Reading","chats":[]}]}`), Revision: 3}
	s := &Service{Conversations: conversations{allowed: true, denied: map[string]bool{"host/revoked": true}}, Repo: &repo{sidebar: stored}}
	got, err := s.Sidebar(context.Background(), chat.Principal{TenantID: "home", SubjectID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	var layout struct {
		Sections []struct {
			ID     string `json:"id"`
			Manual bool   `json:"manual"`
			Chats  []struct {
				ConversationID string `json:"conversationId"`
			} `json:"chats"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(got.Layout, &layout); err != nil {
		t.Fatal(err)
	}
	if len(layout.Sections) != 2 || !layout.Sections[0].Manual || layout.Sections[1].Manual || len(layout.Sections[0].Chats) != 1 || layout.Sections[0].Chats[0].ConversationID != "open" {
		t.Fatalf("filtered layout = %+v", layout.Sections)
	}
}
