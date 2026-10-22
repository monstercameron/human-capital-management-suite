package application

import (
	"context"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// PostAudiences makes the served chat service the source of the tags of
// addressed messages the transport asks for (CHATGATE-014). The core service
// holds the store that keeps the audiences; it is the masker of this service in
// both compositions, so the read goes to it directly.
func (s *streamingChatService) PostAudiences(ctx context.Context, caller chatcore.Principal, tenantID string, ids []string) map[string]chatcore.PostAudienceInfo {
	if s == nil {
		return nil
	}
	lister, ok := s.masker.(chatcore.PostAudienceLister)
	if !ok {
		return nil
	}
	return lister.PostAudiences(ctx, caller, tenantID, ids)
}

var _ chatcore.PostAudienceLister = (*streamingChatService)(nil)
