package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatstateCandidateSource interface {
	ChannelStatusCandidates(context.Context, string, time.Time) ([]chatpolicy.Principal, error)
}

// ChannelStatusCandidates uses the same current assignment and active-worker
// facts as ResolveChatFacts, without impersonating a different request subject.
func (f currentRoleChatFacts) ChannelStatusCandidates(ctx context.Context, tenant string, at time.Time) ([]chatpolicy.Principal, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.Tenant().String() != tenant || !at.Before(p.ExpiresAt()) || f.roles == nil {
		return nil, errChatIdentity
	}
	if err := f.CheckChatSession(ctx, tenant, p.Subject(), at); err != nil {
		return nil, err
	}
	snapshot, err := f.roles.Load(ctx, values.TenantId(tenant), p.OrganizationScopeID())
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	for _, role := range snapshot.Roles {
		if role.Active {
			active[role.ID] = true
		}
	}
	var tx dbport.Tx
	if f.workers != nil {
		if f.tenantUUID == nil {
			return nil, errChatIdentity
		}
		tx, err = f.workers.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		if err = tenancy.WithTenant(ctx, tx, f.tenantUUID(values.TenantId(tenant))); err != nil {
			return nil, err
		}
	}
	candidates := []chatpolicy.Principal{}
	for _, assignment := range snapshot.Assignments {
		if assignment.Version <= 0 || assignment.WorkerRef == "" {
			continue
		}
		if tx != nil {
			worker, found, err := (workforce.Store{}).Get(ctx, tx, f.tenantUUID(values.TenantId(tenant)), assignment.WorkerRef)
			if err != nil {
				return nil, err
			}
			if !found || !strings.EqualFold(worker.LifecycleStatus, "active") {
				continue
			}
		}
		candidate := chatpolicy.Principal{ID: assignment.WorkerRef, Tenant: tenant, Active: true, AuthorityRevision: uint64(assignment.Version)}
		for _, role := range assignment.RoleIDs {
			if active[role] {
				candidate.Roles = append(candidate.Roles, role)
			}
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func (a chatCurrentAuthority) HasOtherChannelReopener(ctx context.Context, p chat.Principal, c chat.Conversation, at time.Time) (bool, error) {
	return a.HasOtherChannelReopenerWithoutRole(ctx, p, c, "", at)
}

func (a chatCurrentAuthority) HasOtherChannelReopenerWithoutRole(ctx context.Context, p chat.Principal, c chat.Conversation, excludedRole string, at time.Time) (bool, error) {
	source := a.source
	if cached, ok := source.facts.(cachedChatFacts); ok {
		source.facts = cached.inner
	}
	actor, ok := trust.FromContext(ctx)
	if !ok || actor == nil || p.TenantID != c.TenantID || actor.Tenant().String() != c.TenantID {
		return false, chat.ErrPermissionDenied
	}
	if _, err := source.Resolve(ctx, actor.Tenant().String(), actor.Subject(), at); err != nil {
		return false, err
	}
	facts, ok := source.facts.(chatstateCandidateSource)
	if !ok {
		return false, chat.ErrUnavailable
	}
	candidates, err := facts.ChannelStatusCandidates(ctx, c.TenantID, at)
	if err != nil {
		return false, err
	}
	store, ok := a.members.(chat.ChannelStatusPermissionStore)
	if !ok {
		return false, chat.ErrUnavailable
	}
	channel := chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Private: c.Kind == chat.PrivateChannel, Enabled: true, Revision: c.Revision}
	workspace := c.ID == "chat:workspace-status-permission"
	currentStatus := chatpolicy.StatusLocked
	if !workspace {
		statuses, ok := a.members.(chat.ChannelStatusStore)
		if !ok {
			return false, chat.ErrUnavailable
		}
		status, err := statuses.ReadChannelStatus(ctx, c.TenantID, c.ID)
		if err != nil {
			return false, err
		}
		currentStatus = status.Effective(at)
		if currentStatus == chatpolicy.StatusOpen {
			currentStatus = chatpolicy.StatusAnnouncements
		}
		if a.policy == nil {
			return false, chat.ErrUnavailable
		}
		policy, err := a.policy.Policy(ctx, c.TenantID, c.ID)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return false, err
		}
		if err == nil {
			channel.RequiredRoles, channel.RoleMode, channel.RequiredQualifications = policy.RequiredRoles, policy.RoleMode, policy.RequiredQualifications
			channel.AllowedPrincipals, channel.AllowedTenants, channel.DeniedPrincipals, channel.DeniedTenants = policy.AllowedPrincipals, policy.AllowedTenants, policy.DeniedPrincipals, policy.DeniedTenants
		}
	}
	for _, candidate := range candidates {
		if candidate.ID == p.SubjectID || candidate.Tenant != c.TenantID || !candidate.Current(at) {
			continue
		}
		roles := []string{}
		admin := false
		for _, role := range candidate.Roles {
			if role == chatpolicy.WorkspaceAdministratorRole {
				admin = true
			}
			if role != excludedRole {
				roles = append(roles, role)
			}
		}
		permissions, err := store.ChannelStatusRolePermissions(ctx, c.TenantID, c.ID, roles)
		if err != nil {
			return false, err
		}
		permissions.WorkspaceAdmin = admin
		if workspace {
			if admin || permissions.ChangeRestricted {
				return true, nil
			}
			continue
		}
		in := chatpolicy.Input{Principal: candidate, Channel: channel, Now: at}
		member, err := a.members.GetMembership(ctx, c.TenantID, c.ID, candidate.Tenant, candidate.ID)
		if err != nil && !errors.Is(err, chat.ErrNotFound) {
			return false, err
		}
		if err == nil {
			state := chatpolicy.MembershipCurrent
			if member.LeftAt != nil {
				state = chatpolicy.MembershipLeft
			}
			in.HasMembership = true
			in.Membership = chatpolicy.Membership{ConversationID: member.ConversationID, PrincipalID: member.SubjectID, Tenant: member.HomeTenantID, State: state, Revision: member.Revision, JoinedAt: valueChatTime(member.JoinedAt), LeftAt: valueChatTime(member.LeftAt)}
			permissions.ChannelAdmin = strings.EqualFold(string(member.Role), string(chat.Manager)) && in.Membership.CurrentAt(c.ID, candidate.ID, candidate.Tenant, at)
			active, err := store.ChannelStatusMembershipCurrent(ctx, c.TenantID, c.ID, candidate.Tenant, candidate.ID)
			if err != nil {
				return false, err
			}
			if !active {
				in.Membership.State = chatpolicy.MembershipSuspended
				permissions.ChannelAdmin = false
			}
		}
		if !chatpolicy.CanChangeChannelStatus(currentStatus, chatpolicy.StatusOpen, permissions) {
			continue
		}
		if _, err = chatpolicy.Evaluate(chatpolicy.ActionRead, in); err == nil {
			return true, nil
		}
	}
	return false, nil
}

var _ chat.ChannelReopenerAuthority = chatCurrentAuthority{}
var _ chat.ChannelRoleReopenerAuthority = chatCurrentAuthority{}
