package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

// SidebarCounts reads the principal's unread and mention counts for several
// conversations of one host tenant in one call (CHATBUG-014). Each conversation
// is admitted as a single Counts read admits it.
func (s *ChatExtensions) SidebarCounts(ctx context.Context, p chat.Principal, host string, conversations []string) (map[string]chatrecipient.Counts, error) {
	if s == nil || s.Recipients == nil {
		return nil, chat.ErrUnavailable
	}
	return s.Recipients.SidebarCounts(ctx, p, host, conversations)
}
