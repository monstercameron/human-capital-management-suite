package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

type ChatSearchConversationReader interface {
	GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error)
}

// NewChatSearch composes the existing chat policy authority with each store's
// row authority. Artifact/document/segment access is deliberately a required
// port, rather than assuming channel membership grants every attached object.
func NewChatSearch(store *chatstore.Store, reader ChatSearchConversationReader, rows chatstore.ChatSearchAuthority) (*chatsearch.Registry, error) {
	return NewChatSearchRendering(store, reader, rows, nil)
}

func NewChatSearchRendering(store *chatstore.Store, reader ChatSearchConversationReader, rows chatstore.ChatSearchAuthority, rendering chatstore.ChatSearchRendering) (*chatsearch.Registry, error) {
	if store == nil || reader == nil || rows == nil {
		return nil, chatsearch.ErrInvalid
	}
	registry := chatsearch.NewRegistry()
	authority := func(ctx context.Context, a chatsearch.Actor, row chatsearch.Row) (bool, error) {
		// The reader's access to a conversation is asked once per conversation in
		// each phase of a search, not once per row of that conversation.
		_, err := chatsearch.Memo(ctx, "conversation\x00"+a.TenantID+"\x00"+a.HomeTenantID+"\x00"+a.PersonID+"\x00"+row.Target.ConversationID, func() (any, error) {
			_, err := reader.GetConversation(ctx, chat.GetConversationRequest{Principal: chat.Principal{TenantID: a.HomeTenantID, SubjectID: a.PersonID}, TenantID: a.TenantID, ConversationID: row.Target.ConversationID})
			return nil, err
		})
		if errors.Is(err, chat.ErrPermissionDenied) || errors.Is(err, chat.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if row.ID == "" {
			return true, nil
		}
		return rows(ctx, a, row)
	}
	if err := store.RegisterChatSearchRendering(registry, authority, rendering); err != nil {
		return nil, err
	}
	if people, ok := reader.(ChatSearchPeopleReader); ok {
		if err := RegisterChatSearchPeople(registry, people); err != nil {
			return nil, err
		}
	}
	if saved, ok := reader.(ChatSearchSavedReader); ok {
		if err := RegisterChatSearchSaved(registry, saved); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
