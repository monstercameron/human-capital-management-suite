package projectservice

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChatLinkedTaskRecord is one live task made from a message of a conversation.
type ChatLinkedTaskRecord struct {
	PostID, ProjectID, TaskID, Title, StatusID, AssigneeID, DueDate string
}

// ChatLinkRepository is the reverse read of chat message links by conversation.
type ChatLinkRepository interface {
	ListTasksLinkedToChatConversation(context.Context, string, string, int) ([]ChatLinkedTaskRecord, error)
}

// ListTasksLinkedToChatConversation returns the tasks made from messages of one
// conversation, limited to projects the viewer may read. The caller must have
// checked that the viewer may read the conversation itself. Tasks in other
// projects are omitted, never reported as restricted.
func (s Service) ListTasksLinkedToChatConversation(ctx context.Context, p *trust.Principal, conversationID string, limit int) ([]ChatLinkedTaskRecord, error) {
	if err := validPrincipal(p); err != nil {
		return nil, err
	}
	repo, ok := s.Links.(ChatLinkRepository)
	if s.Auth == nil || !ok {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(conversationID) == "" || limit < 1 || limit > 500 {
		return nil, ErrInvalidRequest
	}
	records, err := repo.ListTasksLinkedToChatConversation(ctx, tenant(p), conversationID, 500)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	out := make([]ChatLinkedTaskRecord, 0, len(records))
	for _, record := range records {
		ok, seen := allowed[record.ProjectID]
		if !seen {
			ok = s.Auth.Authorize(ctx, p, record.ProjectID, projectaccess.ReadTask) == nil
			allowed[record.ProjectID] = ok
		}
		if ok {
			out = append(out, record)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// ListTasksLinkedToChatConversation implements ChatLinkRepository.
func (a *StoreAdapter) ListTasksLinkedToChatConversation(ctx context.Context, tenantID, conversationID string, limit int) ([]ChatLinkedTaskRecord, error) {
	if a == nil || a.Links == nil {
		return nil, ErrUnavailable
	}
	rows, err := a.Links.ListByChatConversation(ctx, tenantID, conversationID, int32(limit))
	if err != nil {
		return nil, err
	}
	out := make([]ChatLinkedTaskRecord, len(rows))
	for i, r := range rows {
		out[i] = ChatLinkedTaskRecord{PostID: r.PostID, ProjectID: r.ProjectID, TaskID: r.TaskID, Title: r.Title, StatusID: r.StatusID, AssigneeID: r.AssigneeID, DueDate: r.DueDate}
	}
	return out, nil
}
