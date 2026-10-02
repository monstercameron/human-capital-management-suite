package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

type chatsaveAuthority struct{ current chat.ConversationService }

func (a chatsaveAuthority) Authorize(context.Context, chat.Principal, chat.Conversation, chatpolicy.Action, time.Time) (chatpolicy.Input, error) {
	return chatpolicy.Input{}, chat.ErrUnavailable
}

func (a chatsaveAuthority) ReadSavedConversation(ctx context.Context, req chat.GetConversationRequest) (chat.Conversation, error) {
	if a.current == nil {
		return chat.Conversation{}, chat.ErrUnavailable
	}
	return a.current.GetConversation(ctx, req)
}

// SavedMessages keeps persistence in the chat database and reuses the routed
// service's current authorization rather than trusting cached client claims.
func (s *ChatExtensions) SavedMessages() *chat.Service {
	if s == nil || s.TodoStore == nil || s.Conversations == nil {
		return nil
	}
	service := chat.NewService(chatstore.NewModeratedAdapter(chatstore.NewAdapter(s.TodoStore)), time.Now)
	service.SetAuthority(chatsaveAuthority{current: s.Conversations})
	return service
}
