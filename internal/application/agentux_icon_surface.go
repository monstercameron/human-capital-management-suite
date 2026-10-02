package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AgentIconSurface is composed with the ordinary tenant store and trusted clock.
type AgentIconSurface struct {
	Store          *agentpersonastore.Store
	Administrators agentpersonastore.IconAdministrator
	Now            func() time.Time
}

func (s *AgentIconSurface) ChangeAgentIcon(ctx context.Context, command transport.AgentIconCommand) (transport.AgentIconReply, error) {
	return s.command(ctx, command, false)
}
func (s *AgentIconSurface) PreviewAgentIcon(ctx context.Context, command transport.AgentIconCommand) (transport.AgentIconReply, error) {
	return s.command(ctx, command, true)
}

func (s *AgentIconSurface) command(ctx context.Context, command transport.AgentIconCommand, preview bool) (transport.AgentIconReply, error) {
	if ctx == nil {
		return transport.AgentIconReply{}, transport.ErrAgentIconUnauthenticated
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return transport.AgentIconReply{}, transport.ErrAgentIconUnauthenticated
	}
	if p.SubjectKind() != trust.SubjectKindHuman {
		return transport.AgentIconReply{}, transport.ErrAgentIconDenied
	}
	if s == nil || s.Store == nil || s.Now == nil {
		return transport.AgentIconReply{}, transport.ErrAgentIconUnavailable
	}
	at := s.Now().UTC()
	if at.IsZero() || at.Before(p.IssuedAt()) || !at.Before(p.ExpiresAt()) {
		return transport.AgentIconReply{}, transport.ErrAgentIconUnauthenticated
	}
	scoped, err := s.Store.ForTenant(ctx, p.Tenant())
	if err != nil {
		return transport.AgentIconReply{}, transport.ErrAgentIconDenied
	}
	var result agentpersonastore.PersonaIcon
	if preview {
		result, err = scoped.PreviewIcon(ctx, command.PersonaID, command.ExpectedRevision, command.Action, p.Subject(), at, s.Administrators)
	} else {
		result, err = scoped.ChangeIcon(ctx, command.PersonaID, command.ExpectedRevision, command.Action, p.Subject(), at, s.Administrators)
	}
	if err != nil {
		return transport.AgentIconReply{}, agentIconError(err)
	}
	return transport.AgentIconReply{Icon: result.Value, Revision: result.Revision}, nil
}

func agentIconError(err error) error {
	switch {
	case errors.Is(err, agentpersonastore.ErrIconDenied), errors.Is(err, agentpersonastore.ErrNotFound):
		return transport.ErrAgentIconDenied
	case errors.Is(err, agentpersonastore.ErrConflict):
		return transport.ErrAgentIconConflict
	case errors.Is(err, agentpersonastore.ErrInvalid):
		return transport.ErrAgentIconInvalid
	default:
		return transport.ErrAgentIconUnavailable
	}
}

type AgentIconRoleAdministrator struct{ Roles roleaccess.Store }

func (a AgentIconRoleAdministrator) AuthorizeAgentIconAdministrator(ctx context.Context, tenant values.TenantId, actor string) error {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.Tenant() != tenant || p.Subject() != actor {
		return agentpersonastore.ErrIconDenied
	}
	err := (PersonaAdminCommandRoleAuthorizer{Roles: a.Roles}).AuthorizePersonaAdminCommand(ctx, PersonaAdminCommandActor{Principal: p, Tenant: tenant, Subject: actor}, PersonaAdminCreateVersion, "")
	if err != nil {
		return agentpersonastore.ErrIconDenied
	}
	return nil
}
