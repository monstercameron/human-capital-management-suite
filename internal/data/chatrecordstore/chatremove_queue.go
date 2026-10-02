package chatrecordstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// SearchModeration exposes the reports already written by ReportAbuse through
// the same current-access filter used for removals and appeals.
func (s *Store) SearchModeration(ctx context.Context, p chat.Principal, tenant, query string) ([]chat.ModerationItem, error) {
	if s == nil || s.Chat == nil {
		return nil, chat.ErrUnavailable
	}
	return chatstore.NewModeratedAdapter(chatstore.NewAdapter(s.Chat)).SearchModeration(ctx, p, tenant, query)
}
