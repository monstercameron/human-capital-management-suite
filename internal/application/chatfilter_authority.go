package application

import (
	"context"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// ChatFilterAuthority implements Manage filters using the current workspace
// administrator role and current channel manager membership. Read access is
// checked independently, so managing rules never buys private-room access.
type ChatFilterAuthority struct {
	Facts         ChatAuthorityFacts
	Conversations chat.ConversationService
	Membership    interface {
		GetMembership(context.Context, string, string, string, string) (chat.Membership, error)
	}
	Now func() time.Time
}

func (a ChatFilterAuthority) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}
func (a ChatFilterAuthority) AuthorizeFilters(ctx context.Context, actor chatfilter.Actor, channel string) error {
	p, err := newChatAuthoritySource(a.Facts).Resolve(ctx, actor.Tenant, actor.Subject, a.now())
	if err != nil {
		return chatfilter.ErrDenied
	}
	if slices.Contains(p.Roles, "hcm_admin") {
		return nil
	}
	if channel == "" || a.Membership == nil || !a.CanReadFilterConversation(ctx, actor, channel) {
		return chatfilter.ErrDenied
	}
	m, err := a.Membership.GetMembership(ctx, actor.Tenant, channel, actor.Tenant, actor.Subject)
	if err != nil || m.Role != chat.Manager || m.JoinedAt == nil || m.LeftAt != nil {
		return chatfilter.ErrDenied
	}
	return nil
}
func (a ChatFilterAuthority) CanReadFilterConversation(ctx context.Context, actor chatfilter.Actor, channel string) bool {
	if a.Conversations == nil || channel == "" {
		return false
	}
	_, err := a.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: chat.Principal{TenantID: actor.Tenant, SubjectID: actor.Subject}, TenantID: actor.Tenant, ConversationID: channel})
	return err == nil
}
