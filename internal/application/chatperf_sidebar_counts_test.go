package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

// TestTodo_CHATBUG_014_SidebarCounts: the extension service hands a sidebar's
// counts to the recipient service, which admits the reader to each conversation
// before anything is read.
func TestTodo_CHATBUG_014_SidebarCounts(t *testing.T) {
	ctx := context.Background()
	p := chat.Principal{TenantID: "home", SubjectID: "member"}
	repo := &extensionRecipientRepo{t: t}
	s := &ChatExtensions{Recipients: &chatrecipient.Service{Repo: repo, Conversations: extensionConversations{}}}

	counts, err := s.SidebarCounts(ctx, p, "host", []string{"conv", "conv"})
	if err != nil || len(counts) != 1 || counts["conv"] != (chatrecipient.Counts{Unread: 3, Mentions: 1}) {
		t.Fatalf("counts=%+v err=%v", counts, err)
	}
	if repo.calls != 1 {
		t.Fatalf("one conversation named twice was read %d times", repo.calls)
	}

	// A reader who is not admitted gets an empty answer and no store read.
	p.SubjectID = "outsider"
	counts, err = s.SidebarCounts(ctx, p, "host", []string{"conv"})
	if err != nil || len(counts) != 0 || repo.calls != 1 {
		t.Fatalf("an outsider read counts=%+v err=%v store reads=%d", counts, err, repo.calls)
	}

	// A composition without the recipient service says so.
	if _, err := (&ChatExtensions{}).SidebarCounts(ctx, p, "host", []string{"conv"}); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("an unwired service: %v", err)
	}
	var missing *ChatExtensions
	if _, err := missing.SidebarCounts(ctx, p, "host", []string{"conv"}); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("a nil service: %v", err)
	}
}
