package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
)

// Moderation mutations use the same current placement authority as ordinary Chat writes.
type chatremoveRoutedStore struct {
	chat.ModerationStore
	Permissions ChatModerationAdmission
	Directory   chatrouting.Directory
	Cache       *chatrouting.RouteCache
	Now         func() time.Time
}

func (s *chatremoveRoutedStore) lease(ctx context.Context, tenant, conversation string) (context.Context, error) {
	lease, err := s.Cache.Resolve(ctx, s.Directory, conversation, tenant)
	if err != nil {
		if errors.Is(err, chatrouting.ErrNotFound) {
			return nil, chat.ErrNotFound
		}
		return nil, chat.ErrUnavailable
	}
	if err = s.Cache.CheckWrite(ctx, s.Directory, lease, tenant, s.Now()); err != nil {
		s.Cache.Invalidate(conversation, lease.Route.Epoch)
		return nil, chat.ErrUnavailable
	}
	return chatrouting.WithWriteLease(ctx, lease), nil
}

func (s *chatremoveRoutedStore) CommitRemoval(ctx context.Context, r chat.RemovalRequest, at time.Time) (int, error) {
	ctx, err := s.lease(ctx, r.TenantID, r.Selection.ConversationID)
	if err != nil {
		return 0, err
	}
	return s.ModerationStore.CommitRemoval(ctx, r, at)
}

func (s *chatremoveRoutedStore) ReviewRemoved(ctx context.Context, p chat.Principal, tenant, conversation, post, reason string, at time.Time) (chat.Post, error) {
	ctx, err := s.lease(ctx, tenant, conversation)
	if err != nil {
		return chat.Post{}, err
	}
	return s.ModerationStore.ReviewRemoved(ctx, p, tenant, conversation, post, reason, at)
}

func (s *chatremoveRoutedStore) AppealRemoval(ctx context.Context, p chat.Principal, tenant, conversation, post string, at time.Time) error {
	ctx, err := s.lease(ctx, tenant, conversation)
	if err != nil {
		return err
	}
	return s.ModerationStore.AppealRemoval(ctx, p, tenant, conversation, post, at)
}

func (s *chatremoveRoutedStore) ResolveModeration(ctx context.Context, p chat.Principal, tenant, id, action, reason string, at time.Time) error {
	items, err := s.ModerationStore.SearchModeration(ctx, p, tenant, "")
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		ctx, err = s.lease(ctx, tenant, item.ConversationID)
		if err != nil {
			return err
		}
		return s.ModerationStore.ResolveModeration(ctx, p, tenant, id, action, reason, at)
	}
	return chat.ErrNotFound
}

func (s *chatremoveRoutedStore) CanModerate(ctx context.Context, p chat.Principal, t, cid, permission string) error {
	return s.Permissions.CanModerate(ctx, p, t, cid, permission)
}
func (s *chatremoveRoutedStore) CanReportMessage(ctx context.Context, p chat.Principal, t, cid, post string) error {
	return s.Permissions.CanReportMessage(ctx, p, t, cid, post)
}
func (s *chatremoveRoutedStore) AssignModerationPermission(ctx context.Context, p chat.Principal, t, cid, role, permission string, allowed bool) error {
	if cid != "" {
		var err error
		ctx, err = s.lease(ctx, t, cid)
		if err != nil {
			return err
		}
	}
	return s.Permissions.AssignModerationPermission(ctx, p, t, cid, role, permission, allowed)
}
