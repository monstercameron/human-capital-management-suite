package chatroutingadapter

import (
	"context"
	"errors"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
)

func TestTodo_CHAT_002_UnknownRouteDoesNotReachChatStorage(t *testing.T) {
	f := &fakeService{}
	s, _ := newAdapter(t, f)
	_, err := s.SendPost(context.Background(), chat.SendPostRequest{
		TenantID:       "tenant-a",
		ConversationID: "guessed-conversation",
	})
	if !errors.Is(err, chatrouting.ErrNotFound) {
		t.Fatalf("unknown route error = %v, want %v", err, chatrouting.ErrNotFound)
	}
	if f.sendCalls != 0 {
		t.Fatalf("chat service received %d calls for unknown route", f.sendCalls)
	}
}
