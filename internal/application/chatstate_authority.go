package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChatStatusPublicDeliverySource resolves the governed public-delivery grant;
// installation write scope alone does not authorize a public announcement.
type ChatStatusPublicDeliverySource interface {
	ChannelPublicDeliveryAllowed(context.Context, string, string, string, time.Time) (bool, error)
}

func (a chatCurrentAuthority) ChannelStatusPermissions(ctx context.Context, p chat.Principal, c chat.Conversation, at time.Time) (chatpolicy.StatusPermissions, error) {
	permissions := chatpolicy.StatusPermissions{}
	if p.TenantID != c.TenantID {
		return permissions, chat.ErrPermissionDenied
	}
	verified, ok := trust.FromContext(ctx)
	if ok && verified != nil && (verified.SubjectKind() == trust.SubjectKindAgent || verified.SubjectKind() == trust.SubjectKindIntegration) {
		if c.Kind != chat.PublicChannel {
			return permissions, chat.ErrPermissionDenied
		}
		if _, _, err := a.machineAdmission(ctx, p, c, chatpolicy.ActionPost, at); err != nil {
			return permissions, err
		}
		grants, ok := a.apps.(ChatStatusPublicDeliverySource)
		if !ok {
			return permissions, chat.ErrUnavailable
		}
		allowed, err := grants.ChannelPublicDeliveryAllowed(ctx, c.TenantID, c.ID, p.SubjectID, at)
		permissions.PublicDelivery = allowed
		return permissions, err
	}
	// A status command must not reuse the ordinary short-lived role cache after
	// a revocation. Resolve the current persisted role facts for every check.
	source := a.source
	if cached, ok := source.facts.(cachedChatFacts); ok {
		source.facts = cached.inner
	}
	current, err := source.Resolve(ctx, p.TenantID, p.SubjectID, at)
	if err != nil {
		return permissions, err
	}
	if a.members == nil {
		return permissions, chat.ErrUnavailable
	}
	for _, role := range current.Roles {
		if role == chatpolicy.WorkspaceAdministratorRole {
			permissions.WorkspaceAdmin = true
		}
	}
	if c.ID != "chat:workspace-status-permission" {
		m, err := a.members.GetMembership(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
		if err != nil && !errors.Is(err, chat.ErrNotFound) {
			return permissions, err
		}
		permissions.ChannelAdmin = err == nil && strings.EqualFold(string(m.Role), string(chat.Manager)) && m.LeftAt == nil && m.JoinedAt != nil
	}
	store, ok := a.members.(chat.ChannelStatusPermissionStore)
	if !ok {
		return permissions, chat.ErrUnavailable
	}
	assigned, err := store.ChannelStatusRolePermissions(ctx, c.TenantID, c.ID, current.Roles)
	if err != nil {
		return permissions, err
	}
	if permissions.ChannelAdmin {
		active, err := store.ChannelStatusMembershipCurrent(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
		if err != nil {
			return permissions, err
		}
		permissions.ChannelAdmin = active
	}
	permissions.ChangeOpen, permissions.ChangeRestricted, permissions.PostAnnouncements = assigned.ChangeOpen, assigned.ChangeRestricted, assigned.PostAnnouncements
	return permissions, err
}

var _ chat.ChannelStatusAuthority = chatCurrentAuthority{}
