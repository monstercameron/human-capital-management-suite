package application

import (
	"context"
)

// A background occurrence reads the audience as the installed service. It
// never manufactures an authenticated owner context to read a conversation.
type announcementRuntimeGate struct{ runtime *AgentAnnouncementRuntime }

func (g announcementRuntimeGate) AuthorizeAnnouncementDocuments(ctx context.Context, tenant, conversation string, documents []AgentAnnouncementResolvedDocument, citations []string) (bool, int, error) {
	r := g.runtime
	current, err := r.currentConversation(ctx, tenant, conversation)
	if err != nil {
		return false, len(documents), err
	}
	snapshot, err := r.Audience.CurrentAudience(ctx, current)
	if err != nil {
		return false, len(documents), err
	}
	return authorizeAnnouncementAudience(ctx, tenant, conversation, r.DocumentAuthority, snapshot, documents, citations)
}
