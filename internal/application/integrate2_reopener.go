package application

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"time"
)

// A removal checks the target's persisted grants under the acting person's session.
// It never manufactures a session belonging to the target.
func (a chatCurrentAuthority) ChannelStatusPermissionsForRemoval(ctx context.Context, p chat.Principal, c chat.Conversation, at time.Time) (chatpolicy.StatusPermissions, error) {
	permissions := chatpolicy.StatusPermissions{}
	actor, ok := trust.FromContext(ctx)
	if !ok || actor == nil || actor.Tenant().String() != c.TenantID || p.TenantID != c.TenantID {
		return permissions, chat.ErrPermissionDenied
	}
	source := a.source
	if cached, ok := source.facts.(cachedChatFacts); ok {
		source.facts = cached.inner
	}
	if _, err := source.Resolve(ctx, actor.Tenant().String(), actor.Subject(), at); err != nil {
		return permissions, err
	}
	facts, ok := source.facts.(chatstateCandidateSource)
	if !ok {
		return permissions, chat.ErrUnavailable
	}
	candidates, err := facts.ChannelStatusCandidates(ctx, c.TenantID, at)
	if err != nil {
		return permissions, err
	}
	roles := []string{}
	for _, candidate := range candidates {
		if candidate.ID == p.SubjectID && candidate.Tenant == p.TenantID && candidate.Current(at) {
			roles = candidate.Roles
		}
	}
	store, ok := a.members.(chat.ChannelStatusPermissionStore)
	if !ok {
		return permissions, chat.ErrUnavailable
	}
	assigned, err := store.ChannelStatusRolePermissions(ctx, c.TenantID, c.ID, roles)
	if err != nil {
		return permissions, err
	}
	permissions = assigned
	for _, role := range roles {
		permissions.WorkspaceAdmin = permissions.WorkspaceAdmin || role == chatpolicy.WorkspaceAdministratorRole
	}
	member, err := a.members.GetMembership(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
	if err != nil && !errors.Is(err, chat.ErrNotFound) {
		return permissions, err
	}
	if err == nil && member.Role == chat.Manager && member.JoinedAt != nil && member.LeftAt == nil {
		permissions.ChannelAdmin, err = store.ChannelStatusMembershipCurrent(ctx, c.TenantID, c.ID, p.TenantID, p.SubjectID)
	}
	if errors.Is(err, chat.ErrNotFound) {
		err = nil
	}
	return permissions, err
}
