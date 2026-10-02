package chatroutingadapter

import (
	"context"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

func (s *Service) GetChannelStatus(ctx context.Context, r chat.GetConversationRequest) (chat.ChannelStatus, error) {
	if s.status == nil {
		return chat.ChannelStatus{}, chat.ErrUnavailable
	}
	return s.status.GetChannelStatus(ctx, r)
}
func (s *Service) AllowedStatusTransitions(ctx context.Context, r chat.GetConversationRequest) ([]chat.StatusTransition, error) {
	if s.status == nil {
		return nil, chat.ErrUnavailable
	}
	return s.status.AllowedStatusTransitions(ctx, r)
}
func (s *Service) GetChannelStatusSnapshot(ctx context.Context, r chat.GetConversationRequest) (chat.ChannelStatusSnapshot, error) {
	source, ok := s.status.(chat.ChannelStatusSnapshotService)
	if !ok {
		return chat.ChannelStatusSnapshot{}, chat.ErrUnavailable
	}
	return source.GetChannelStatusSnapshot(ctx, r)
}
func (s *Service) ChangeChannelStatus(ctx context.Context, r chat.ChangeChannelStatusRequest) (chat.ChannelStatus, error) {
	if s.status == nil {
		return chat.ChannelStatus{}, chat.ErrUnavailable
	}
	fenced, err := s.routeContext(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return chat.ChannelStatus{}, err
	}
	return s.status.ChangeChannelStatus(fenced, r)
}

func (s *Service) ChatWriteContext(ctx context.Context, tenant, conversation string) (context.Context, error) {
	return s.routeContext(ctx, tenant, conversation)
}

func (s *Service) statusMutation(ctx context.Context, p chat.Principal, tenant, room string, action chatpolicy.StatusAction, target *chat.Principal) context.Context {
	ctx = chat.WithChannelStatusAction(ctx, action)
	checker, ok := s.status.(interface {
		CheckChannelStatusAction(context.Context, chat.Principal, chat.Conversation, chatpolicy.StatusAction) error
		CheckChannelReopenerRemoval(context.Context, chat.Principal, chat.Conversation) error
	})
	if !ok {
		return ctx
	}
	return chat.WithChannelMutationCheck(ctx, func(c context.Context) error {
		conversation, err := s.ConversationService.GetConversation(chat.WithChannelStatusRead(c), chat.GetConversationRequest{Principal: p, TenantID: tenant, ConversationID: room})
		if err != nil {
			return err
		}
		if err = checker.CheckChannelStatusAction(c, p, conversation, action); err != nil {
			return err
		}
		if target != nil && (conversation.Kind == chat.PublicChannel || conversation.Kind == chat.PrivateChannel) {
			return checker.CheckChannelReopenerRemoval(c, *target, conversation)
		}
		return nil
	})
}

func (s *Service) statusAdmission(ctx context.Context, p chat.Principal, tenant, room string) context.Context {
	ctx = chat.WithChannelStatusAction(ctx, chatpolicy.StatusManageMembers)
	checker, ok := s.status.(interface {
		CheckChannelMembershipAdmissionByID(context.Context, chat.Principal, string, string) error
	})
	if !ok {
		return ctx
	}
	return chat.WithChannelMutationCheck(ctx, func(c context.Context) error { return checker.CheckChannelMembershipAdmissionByID(c, p, tenant, room) })
}

func (s *Service) SearchChannelStatuses(ctx context.Context, p chat.Principal, tenant, query string, archived bool) ([]chat.ChannelStatus, error) {
	source, ok := s.status.(chat.ChannelStatusSearcher)
	if !ok {
		return nil, chat.ErrUnavailable
	}
	return source.SearchChannelStatuses(ctx, p, tenant, query, archived)
}
