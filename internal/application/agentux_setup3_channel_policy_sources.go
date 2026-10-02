package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

func (s *streamingChatService) CapturePersonaChannelPolicy(ctx context.Context, tenant, conversation, manager string) (chatstore.PersonaChannelPolicySnapshot, error) {
	if s == nil {
		return chatstore.PersonaChannelPolicySnapshot{}, errPersonaAuthoritySourceUnavailable
	}
	source, ok := s.membership.(personaCurrentChannelPolicySource)
	if !ok {
		return chatstore.PersonaChannelPolicySnapshot{}, errPersonaAuthoritySourceUnavailable
	}
	return source.CapturePersonaChannelPolicy(ctx, tenant, conversation, manager)
}

func (s *personaChatConversationService) CapturePersonaChannelPolicy(ctx context.Context, tenant, conversation, manager string) (chatstore.PersonaChannelPolicySnapshot, error) {
	if s == nil {
		return chatstore.PersonaChannelPolicySnapshot{}, errPersonaAuthoritySourceUnavailable
	}
	source, ok := s.ConversationService.(personaCurrentChannelPolicySource)
	if !ok {
		return chatstore.PersonaChannelPolicySnapshot{}, errPersonaAuthoritySourceUnavailable
	}
	return source.CapturePersonaChannelPolicy(ctx, tenant, conversation, manager)
}

func (s *DatabasePersonaAudienceSource) CapturePersonaChannelPolicy(ctx context.Context, tenant, conversation, manager string) (chatstore.PersonaChannelPolicySnapshot, error) {
	if s == nil {
		return chatstore.PersonaChannelPolicySnapshot{}, errPersonaAuthoritySourceUnavailable
	}
	source, ok := s.Chat.(personaCurrentChannelPolicySource)
	if !ok {
		return chatstore.PersonaChannelPolicySnapshot{}, errPersonaAuthoritySourceUnavailable
	}
	return source.CapturePersonaChannelPolicy(ctx, tenant, conversation, manager)
}

func (a *PersonaPublicAudienceAuthority) CapturePersonaChannelPolicy(ctx context.Context, tenant, conversation, manager string) (chatstore.PersonaChannelPolicySnapshot, error) {
	if a == nil || a.Store == nil {
		return chatstore.PersonaChannelPolicySnapshot{}, ErrPersonaAudienceFloorUnavailable
	}
	return a.Store.CapturePersonaChannelPolicy(ctx, tenant, conversation, manager)
}

var _ personaCurrentChannelPolicySource = (*streamingChatService)(nil)
var _ personaCurrentChannelPolicySource = (*personaChatConversationService)(nil)
var _ personaCurrentChannelPolicySource = (*DatabasePersonaAudienceSource)(nil)
var _ personaCurrentChannelPolicySource = (*PersonaPublicAudienceAuthority)(nil)
