package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// ModerationSummary and ModerationTarget reach the store that holds the queue
// through the same permission port the other moderation commands use.
func (s *chatremoveRoutedStore) ModerationSummary(ctx context.Context, p chat.Principal, tenant string) (chat.ModerationSummary, error) {
	store, ok := s.Permissions.(chat.ModerationSummaryStore)
	if !ok {
		return chat.ModerationSummary{}, chat.ErrUnavailable
	}
	return store.ModerationSummary(ctx, p, tenant)
}

func (s *chatremoveRoutedStore) ModerationTarget(ctx context.Context, p chat.Principal, tenant, conversation, post string) (chat.Post, error) {
	store, ok := s.Permissions.(chat.ModerationTargetStore)
	if !ok {
		return chat.Post{}, chat.ErrUnavailable
	}
	return store.ModerationTarget(ctx, p, tenant, conversation, post)
}

// chatmod005FilterQueue lets filter hits flagged for review into the one
// moderation queue. Reading needs no lease; a decision on a hit takes the lease
// of the hit's conversation exactly as a decision on a report does.
type chatmod005FilterQueue struct {
	Filters *chatstore.FilterModeration
	Routes  *chatremoveRoutedStore
}

func (q chatmod005FilterQueue) SearchModeration(ctx context.Context, p chat.Principal, tenant, query string) ([]chat.ModerationItem, error) {
	return q.Filters.SearchModeration(ctx, p, tenant, query)
}

func (q chatmod005FilterQueue) CountModeration(ctx context.Context, p chat.Principal, tenant string) (int, error) {
	return q.Filters.CountModeration(ctx, p, tenant)
}

func (q chatmod005FilterQueue) ResolveModeration(ctx context.Context, p chat.Principal, tenant, id, action, reason string, at time.Time) error {
	conversation, err := q.Filters.ConversationOf(ctx, tenant, id)
	if err != nil {
		return err
	}
	ctx, err = q.Routes.lease(ctx, tenant, conversation)
	if err != nil {
		return err
	}
	return q.Filters.ResolveModeration(ctx, p, tenant, id, action, reason, at)
}

var _ chat.FilterModerationPort = chatmod005FilterQueue{}
var _ chat.FilterModerationCounter = chatmod005FilterQueue{}
