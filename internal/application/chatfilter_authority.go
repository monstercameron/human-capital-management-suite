package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// ChatFilterPermissions reads the stored "Manage filters" answers: for each
// role that has one, whether it holds the permission in a conversation ("" is
// the whole workspace). chatstore.FilterStore implements it.
type ChatFilterPermissions interface {
	ManageFiltersRows(ctx context.Context, tenant, conversation string) (map[string]bool, error)
}

// ChatFilterAuthority decides "Manage filters" (CHATMOD-005). The default is a
// workspace administrator everywhere and a channel's manager in that channel;
// a stored row for a role replaces the default for that role, in either
// direction, and a conversation's row replaces the workspace's. A person holds
// the permission when any of their current roles does. Read access is checked
// independently, so managing rules never buys private-room access.
type ChatFilterAuthority struct {
	Facts         ChatAuthorityFacts
	Conversations chat.ConversationService
	Membership    interface {
		GetMembership(context.Context, string, string, string, string) (chat.Membership, error)
	}
	// Permissions is the stored rows. Without it the defaults decide alone.
	Permissions ChatFilterPermissions
	Now         func() time.Time
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
	rows := map[string]bool{}
	if a.Permissions != nil {
		// A row that cannot be read may be the row that takes the permission
		// away, so the defaults do not stand in for it.
		if rows, err = a.Permissions.ManageFiltersRows(ctx, actor.Tenant, channel); err != nil {
			return chatfilter.ErrUnavailable
		}
	}
	holds := func(role string, member bool) bool {
		if allowed, stored := rows[role]; stored {
			return allowed
		}
		return chat.DefaultModerationPermission(role, member, chat.PermissionManageFilters)
	}
	// The person's workspace roles. An administrator's role is stored under the
	// name the moderation tables use for it.
	for _, role := range p.Roles {
		if role == chatpolicy.WorkspaceAdministratorRole {
			role = chat.WorkspaceAdministratorModerationRole
		}
		if holds(role, false) {
			return nil
		}
	}
	// Their role in the channel counts only for that channel, and only while
	// they can read it.
	if channel == "" || a.Membership == nil || !a.CanReadFilterConversation(ctx, actor, channel) {
		return chatfilter.ErrDenied
	}
	m, err := a.Membership.GetMembership(ctx, actor.Tenant, channel, actor.Tenant, actor.Subject)
	if err != nil || m.JoinedAt == nil || m.LeftAt != nil || !holds(string(m.Role), true) {
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
