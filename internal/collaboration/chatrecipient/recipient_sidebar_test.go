package chatrecipient

import (
	"context"
	"encoding/json"
	"errors"
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

// TestPutSidebarAcceptsAJustCreatedConversation is the server-side half of
// the CHAT-02 investigation: "after creating a private channel and sending
// its first message, PutSidebar rejects the layout." Service.admit (called
// per chat in PutSidebar, recipient.go) only ever refuses on
// s.Conversations.GetConversation failing, so a conversation the caller can
// see -- including one created moments earlier -- is always accepted here.
// This pins that the rejection CHAT-02 observed is not a stale-visibility
// server bug: a fresh, visible conversation is admitted every time.
func TestPutSidebarAcceptsAJustCreatedConversation(t *testing.T) {
	db := &repo{}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	layout := `{"sections":[{"id":"channels","name":"Channels","chats":[{"hostTenantId":"home","conversationId":"just-created"}]}]}`

	if _, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: []byte(layout)}, 1); err != nil {
		t.Fatalf("PutSidebar rejected a conversation the caller can see: %v", err)
	}
	if db.calls == 0 {
		t.Fatal("PutSidebar never reached the repository")
	}
}

// TestPutSidebarRejectsAmbiguousDraftHost pins the one validation in
// PutSidebar (recipient.go, the `hosts[id] == ""` check) that can reject a
// layout naming a conversation the caller can plainly see: a client-composed
// draft keyed by a conversation ID that two different sections claim under
// two different host tenants. That is a malformed client payload -- the
// conversation ID is genuinely ambiguous without a host to disambiguate it --
// so ErrInvalidArgument is the correct, typed answer, not a server defect.
func TestPutSidebarRejectsAmbiguousDraftHost(t *testing.T) {
	db := &repo{}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	layout := `{"sections":[` +
		`{"id":"a","chats":[{"hostTenantId":"host-1","conversationId":"room"}]},` +
		`{"id":"b","chats":[{"hostTenantId":"host-2","conversationId":"room"}]}` +
		`],"drafts":{"room":"still typing"}}`

	if _, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: []byte(layout)}, 1); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("ambiguous draft host = %v, want ErrInvalidArgument", err)
	}
	if db.calls != 0 {
		t.Fatal("an invalid layout must never reach the repository")
	}
}
