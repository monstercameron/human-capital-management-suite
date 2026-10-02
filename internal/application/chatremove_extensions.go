package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ChatModerationExtensions keeps ReportAbuse on the existing Report service and
// gives ModerateAbuse the same queue command port as the JSON page. Legacy
// actions continue through ChatExtensions.Moderate.
type ChatModerationExtensions struct {
	*ChatExtensions
	Moderation *chat.ModerationService
}

func (s *ChatModerationExtensions) Moderate(ctx context.Context, p chat.Principal, cid, caseID, action, target, reason, evidence string) error {
	switch action {
	case "remove", "restore", "dismiss", "message_author":
	default:
		if s == nil || s.ChatExtensions == nil {
			return chat.ErrUnavailable
		}
		return s.ChatExtensions.Moderate(ctx, p, cid, caseID, action, target, reason, evidence)
	}
	if s == nil || s.Moderation == nil {
		return chat.ErrUnavailable
	}
	if strings.TrimSpace(evidence) == "" {
		return chat.ErrInvalidArgument
	}
	if !strings.HasPrefix(caseID, "report:") && !strings.HasPrefix(caseID, "removal:") && !strings.HasPrefix(caseID, "filter:") {
		caseID = "report:" + caseID
	}
	items, err := s.Moderation.Queue(ctx, p, p.TenantID, "")
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID == caseID && item.ConversationID == cid && item.PostID == target {
			return s.Moderation.Resolve(ctx, p, p.TenantID, caseID, action, reason)
		}
	}
	return chat.ErrPermissionDenied
}
