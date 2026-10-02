package chatstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

// sideAdmit lets every conversation through: membership is the chat service's
// own rule and is proved elsewhere; this test is about the stored layout.
type sideAdmit struct{ chat.ConversationService }

func (sideAdmit) GetConversation(_ context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	return chat.Conversation{ID: r.ConversationID, TenantID: r.TenantID}, nil
}

// TestTodo_CHATSIDE_001_Integration drives the sidebar service over the real
// per-person layout store: a person makes a section, moves a conversation into
// it, stars another, is refused a duplicate name, deletes the section and finds
// its conversation back in the default section, while another member of the
// same workspace sees none of it and a second session sees the first one's
// changes.
func TestTodo_CHATSIDE_001_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	repo := NewRecipientStateStore(s)
	service := &chatrecipient.Service{Conversations: sideAdmit{}, Repo: repo}
	second := &chatrecipient.Service{Conversations: sideAdmit{}, Repo: NewRecipientStateStore(s)}
	ctx := context.Background()
	alice := chat.Principal{TenantID: "home", SubjectID: "alice"}
	bob := chat.Principal{TenantID: "home", SubjectID: "bob"}

	room := func(id string) string { return `{"hostTenantId":"home","conversationId":"` + id + `"}` }
	withSection := `{"sections":[{"id":"custom-1","name":"Projects","chats":[` + room("c-2") + `]},{"id":"channels","name":"Channels","chats":[` + room("c-1") + `,` + room("c-3") + `]}],"starred":["c-1"]}`
	saved, err := service.PutSidebar(ctx, alice, chatrecipient.Sidebar{Layout: []byte(withSection)}, 1)
	if err != nil || saved.Revision != 2 {
		t.Fatalf("saving a section and a favorite = %+v, %v", saved, err)
	}
	if _, err := service.PutSidebar(ctx, alice, chatrecipient.Sidebar{Layout: []byte(strings.Replace(withSection, `"channels","name":"Channels"`, `"custom-2","name":" projects"`, 1))}, 2); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("a second section with the same name was saved: %v", err)
	}

	seen, err := second.Sidebar(ctx, alice)
	if err != nil || seen.Revision != 2 || !strings.Contains(string(seen.Layout), `"Projects"`) || !strings.Contains(string(seen.Layout), `"starred": ["c-1"]`) {
		t.Fatalf("a second session of the same person reads %s (revision %d), %v", seen.Layout, seen.Revision, err)
	}
	other, err := second.Sidebar(ctx, bob)
	if err != nil || strings.Contains(string(other.Layout), "Projects") || strings.Contains(string(other.Layout), "c-1") || other.Revision != 1 {
		t.Fatalf("another member reads the first person's layout: %s (revision %d), %v", other.Layout, other.Revision, err)
	}

	// Deleting the section is a save without it: its conversation is named in the
	// default section again, and the favorite is untouched.
	deleted := `{"sections":[{"id":"channels","name":"Channels","chats":[` + room("c-1") + `,` + room("c-3") + `,` + room("c-2") + `]}],"starred":["c-1"]}`
	if _, err := service.PutSidebar(ctx, alice, chatrecipient.Sidebar{Layout: []byte(deleted)}, 2); err != nil {
		t.Fatalf("deleting the section: %v", err)
	}
	after, err := second.Sidebar(ctx, alice)
	if err != nil || strings.Contains(string(after.Layout), "Projects") || !strings.Contains(string(after.Layout), "c-2") || !strings.Contains(string(after.Layout), `"starred": ["c-1"]`) {
		t.Fatalf("after deleting the section the layout is %s, %v", after.Layout, err)
	}
}
