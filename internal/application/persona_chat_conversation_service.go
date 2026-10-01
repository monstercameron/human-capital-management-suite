package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// personaChatConversationService preserves the full served chat surface while
// routing committed human sends through the post-commit persona coordinator.
// Composition supplies the streaming service as both forwarding interfaces.
type personaChatConversationService struct {
	chat.ConversationService
	chat.ReferenceService
	invocation *personaChatInvocation
	ephemeral  chat.EphemeralService
}

func newPersonaChatConversationService(
	conversation chat.ConversationService,
	references chat.ReferenceService,
	invocation *personaChatInvocation,
	ephemeral chat.EphemeralService,
) (*personaChatConversationService, error) {
	if conversation == nil || references == nil || invocation == nil {
		return nil, errPersonaChatInvocation
	}
	invocation.chat = personaChatConversationPostWriter{conversation: conversation, references: references}
	return &personaChatConversationService{
		ConversationService: conversation,
		ReferenceService:    references,
		invocation:          invocation,
		ephemeral:           ephemeral,
	}, nil
}

type personaChatConversationPostWriter struct {
	conversation chat.ConversationService
	references   chat.ReferenceService
}

func (w personaChatConversationPostWriter) SendPost(ctx context.Context, request chat.SendPostRequest) (chat.Post, error) {
	if len(request.References) > 0 && w.references != nil {
		return w.references.SendPostWithReferences(ctx, chat.SendPostWithReferencesRequest{
			SendPostRequest: request,
			References:      append([]chat.Reference(nil), request.References...),
		})
	}
	return w.conversation.SendPost(ctx, request)
}

func (s *personaChatConversationService) SendPost(ctx context.Context, request chat.SendPostRequest) (chat.Post, error) {
	if s == nil || s.invocation == nil {
		return chat.Post{}, errPersonaChatInvocation
	}
	return s.invocation.SendPost(ctx, request)
}

func (s *personaChatConversationService) SendPostWithReferences(ctx context.Context, request chat.SendPostWithReferencesRequest) (chat.Post, error) {
	if s == nil || s.invocation == nil {
		return chat.Post{}, errPersonaChatInvocation
	}
	request.SendPostRequest.References = append([]chat.Reference(nil), request.References...)
	return s.invocation.SendPost(ctx, request.SendPostRequest)
}

func (s *personaChatConversationService) SendEphemeralPost(ctx context.Context, request chat.SendEphemeralPostRequest) (chat.EphemeralPost, error) {
	if s == nil || s.ephemeral == nil {
		return chat.EphemeralPost{}, chat.ErrUnavailable
	}
	return s.ephemeral.SendEphemeralPost(ctx, request)
}

var _ chat.ConversationService = (*personaChatConversationService)(nil)
var _ chat.ReferenceService = (*personaChatConversationService)(nil)
var _ chat.EphemeralService = (*personaChatConversationService)(nil)
