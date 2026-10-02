package chat

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type ChannelStatusSearcher interface {
	SearchChannelStatuses(context.Context, Principal, string, string, bool) ([]ChannelStatus, error)
}

func (s *Service) SearchChannelStatuses(ctx context.Context, p Principal, tenant, query string, archived bool) ([]ChannelStatus, error) {
	if err := validatePrincipal(p, tenant); err != nil {
		return nil, err
	}
	store, ok := s.store.(ChannelStatusSearcher)
	if !ok {
		return nil, ErrUnavailable
	}
	results, err := store.SearchChannelStatuses(ctx, p, tenant, query, archived)
	if err != nil {
		return nil, err
	}
	out := []ChannelStatus{}
	for _, result := range results {
		if result.TenantID != tenant {
			return nil, ErrUnavailable
		}
		_, err := s.statusConversation(ctx, GetConversationRequest{Principal: p, TenantID: tenant, ConversationID: result.ConversationID})
		if errors.Is(err, ErrPermissionDenied) || errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result.Status = result.Effective(s.now())
		out = append(out, result)
	}
	return out, nil
}

type ChannelStatusPermissionGrant struct {
	TenantID, ConversationID, RoleID                string
	ChangeOpen, ChangeRestricted, PostAnnouncements bool
	Revision                                        uint64
}

type ChannelStatusPermissionStore interface {
	ChannelStatusRolePermissions(context.Context, string, string, []string) (chatpolicy.StatusPermissions, error)
	ChannelStatusMembershipCurrent(context.Context, string, string, string, string) (bool, error)
	PutChannelStatusPermission(context.Context, Principal, ChannelStatusPermissionGrant, uint64, string, func(context.Context, ChannelStatusPermissionGrant) error) (ChannelStatusPermissionGrant, error)
}

type ChannelStatusPermissionSearcher interface {
	SearchChannelStatusPermissions(context.Context, string, string, func(context.Context) error) ([]ChannelStatusPermissionGrant, error)
}

func (s *Service) SearchChannelStatusPermissions(ctx context.Context, p Principal, tenant, query string) ([]ChannelStatusPermissionGrant, error) {
	if err := validatePrincipal(p, tenant); err != nil {
		return nil, err
	}
	if p.TenantID != tenant {
		return nil, ErrPermissionDenied
	}
	store, ok := s.store.(ChannelStatusPermissionSearcher)
	if !ok {
		return nil, ErrUnavailable
	}
	return store.SearchChannelStatusPermissions(ctx, tenant, query, func(ctx context.Context) error {
		permissions, err := s.statusPermissions(ctx, p, Conversation{TenantID: tenant, ID: "chat:workspace-status-permission", Kind: PublicChannel, Revision: 1})
		if err != nil {
			return err
		}
		if !permissions.WorkspaceAdmin {
			return ErrPermissionDenied
		}
		return nil
	})
}

// ChannelReopenerAuthority proves that a different current principal retains
// both read access and restricted-status permission, including private channels.
type ChannelReopenerAuthority interface {
	HasOtherChannelReopener(context.Context, Principal, Conversation, time.Time) (bool, error)
}

type ChannelRoleReopenerAuthority interface {
	HasOtherChannelReopenerWithoutRole(context.Context, Principal, Conversation, string, time.Time) (bool, error)
}

// CheckChannelReopenerRemoval is called before revoking membership or status
// permission. Without a current authority directory, removal fails closed.
func (s *Service) CheckChannelReopenerRemoval(ctx context.Context, p Principal, c Conversation) error {
	actor := p
	if verified, ok := trust.FromContext(ctx); ok && verified != nil {
		actor = Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject()}
	}
	if _, err := s.statusConversation(ctx, GetConversationRequest{Principal: actor, TenantID: c.TenantID, ConversationID: c.ID}); err != nil {
		return err
	}
	var permissions chatpolicy.StatusPermissions
	var err error
	if actor.TenantID != p.TenantID || actor.SubjectID != p.SubjectID {
		source, ok := s.authority.(interface {
			ChannelStatusPermissionsForRemoval(context.Context, Principal, Conversation, time.Time) (chatpolicy.StatusPermissions, error)
		})
		if !ok {
			return ErrUnavailable
		}
		permissions, err = source.ChannelStatusPermissionsForRemoval(ctx, p, c, s.now())
	} else {
		permissions, err = s.statusPermissions(ctx, p, c)
	}
	if err != nil {
		return err
	}
	if !permissions.WorkspaceAdmin && !permissions.ChangeRestricted && !permissions.ChannelAdmin && !permissions.ChangeOpen {
		return nil
	}
	a, ok := s.authority.(ChannelReopenerAuthority)
	if !ok {
		return ErrLastReopener
	}
	other, err := a.HasOtherChannelReopener(ctx, p, c, s.now())
	if err != nil {
		return err
	}
	if !other {
		return ErrLastReopener
	}
	return nil
}

func (s *Service) SetChannelStatusPermission(ctx context.Context, p Principal, grant ChannelStatusPermissionGrant, expected uint64, reason string) (ChannelStatusPermissionGrant, error) {
	if validatePrincipal(p, grant.TenantID) != nil || p.TenantID != grant.TenantID || strings.TrimSpace(grant.RoleID) == "" || strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return grant, ErrInvalidArgument
	}
	store, ok := s.store.(ChannelStatusPermissionStore)
	if !ok {
		return grant, ErrUnavailable
	}
	return store.PutChannelStatusPermission(ctx, p, grant, expected, reason, func(ctx context.Context, previous ChannelStatusPermissionGrant) error {
		// Workspace assignments need a tenant-wide administrator source. Channel
		// assignments use the same current authority as the status command.
		c := Conversation{ID: grant.ConversationID, TenantID: grant.TenantID, Kind: PublicChannel, Revision: 1}
		if grant.ConversationID == "" {
			c.ID = "chat:workspace-status-permission"
		} else {
			var err error
			c, err = s.statusConversation(ctx, GetConversationRequest{Principal: p, TenantID: grant.TenantID, ConversationID: grant.ConversationID})
			if err != nil {
				return err
			}
		}
		permissions, err := s.statusPermissions(ctx, p, c)
		if err != nil {
			return err
		}
		if !permissions.WorkspaceAdmin {
			return ErrPermissionDenied
		}
		if previous.ChangeRestricted && !grant.ChangeRestricted {
			a, ok := s.authority.(ChannelRoleReopenerAuthority)
			if !ok {
				return ErrLastReopener
			}
			other, err := a.HasOtherChannelReopenerWithoutRole(ctx, p, c, grant.RoleID, s.now())
			if err != nil {
				return err
			}
			if !other {
				return ErrLastReopener
			}
		}
		return nil
	})
}
