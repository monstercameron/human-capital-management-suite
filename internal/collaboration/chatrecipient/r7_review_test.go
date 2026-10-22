package chatrecipient

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A person's sidebar document is bounded as a whole: the section, room and
// preference limits do not cover a draft or a filter value, so one layout could
// carry megabytes that every sidebar read then returns.
func TestR7_PutSidebarRefusesAnOversizedDocument(t *testing.T) {
	db := &repo{}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	huge := strings.Repeat("x", sidebarDocumentMax)
	layout := `{"sections":[{"id":"channels","chats":[{"hostTenantId":"home","conversationId":"room"}]}],"drafts":{"room":"` + huge + `"}}`
	if _, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: []byte(layout)}, 1); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("oversized layout = %v, want ErrInvalidArgument", err)
	}
	if db.calls != 0 {
		t.Fatal("an oversized layout reached the repository")
	}
	ok := `{"sections":[{"id":"channels","chats":[{"hostTenantId":"home","conversationId":"room"}]}],"drafts":{"room":"` + strings.Repeat("x", 4000) + `"}}`
	if _, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: []byte(ok)}, 1); err != nil {
		t.Fatalf("an ordinary draft was refused: %v", err)
	}
}
