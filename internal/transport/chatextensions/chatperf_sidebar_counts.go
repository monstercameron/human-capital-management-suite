package chatextensions

import (
	"context"
	"errors"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// sidebarCountsService is the part of the service that answers a whole sidebar
// in one call. It is asked for separately so a composition that does not have
// it still serves the same request, one conversation at a time.
type sidebarCountsService interface {
	SidebarCounts(context.Context, chat.Principal, string, []string) (map[string]chatrecipient.Counts, error)
}

// sidebarCounts answers a GetCounts request that names several conversations
// (CHATBUG-014). The answer has one entry per conversation the principal may
// read, in the order asked; the others are left out.
func (s *server) sidebarCounts(ctx context.Context, p chat.Principal, r *chatv1.GetCountsRequest) (*chatv1.GetCountsResponse, error) {
	ids := r.GetConversationIds()
	if r.GetConversationId() != "" || len(ids) > chatrecipient.SidebarCountsLimit {
		return nil, envelope.New(envelope.CodeInvalidArgument, "chat.counts.invalid_batch",
			"name either one conversation or up to one hundred, not both").
			WithViolation("conversation_ids", "at most one hundred conversations, with conversation_id empty", "chat.counts.batch_bounds")
	}
	var all map[string]chatrecipient.Counts
	if batch, ok := s.deps.Service.(sidebarCountsService); ok {
		counts, err := batch.SidebarCounts(ctx, p, r.GetTenantId(), ids)
		if err != nil {
			return nil, mapped(err)
		}
		all = counts
	} else {
		all = make(map[string]chatrecipient.Counts, len(ids))
		for _, id := range ids {
			counts, err := s.deps.Service.Counts(ctx, p, r.GetTenantId(), id)
			if errors.Is(err, chat.ErrPermissionDenied) || errors.Is(err, chat.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, mapped(err)
			}
			all[id] = counts
		}
	}
	out := &chatv1.GetCountsResponse{AllCounts: make([]*chatv1.ChatCounts, 0, len(all))}
	sent := make(map[string]struct{}, len(all))
	for _, id := range ids {
		counts, ok := all[id]
		if _, dup := sent[id]; !ok || dup {
			continue
		}
		sent[id] = struct{}{}
		out.AllCounts = append(out.AllCounts, &chatv1.ChatCounts{TenantId: r.GetTenantId(), ConversationId: id, SubjectId: p.SubjectID, HomeTenantId: p.TenantID, UnreadCount: counts.Unread, MentionCount: counts.Mentions})
	}
	return out, nil
}
