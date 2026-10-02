package application

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type AgentAnnouncementOwnerAuthority struct {
	Personas   *agentpersonastore.Store
	Authorizer PersonaAdminCommandAuthorizer
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

func (a AgentAnnouncementOwnerAuthority) AgentAnnouncementActor(ctx context.Context) (AgentAnnouncementActor, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || a.Now == nil || a.TenantUUID == nil || p.SubjectKind() != trust.SubjectKindHuman || !p.ExpiresAt().After(a.Now()) {
		return AgentAnnouncementActor{}, ErrAgentAnnouncementDenied
	}
	actor := AgentAnnouncementActor{TenantID: p.Tenant().String(), TenantUUID: a.TenantUUID(p.Tenant()), SubjectID: p.Subject()}
	if actor.TenantUUID == uuid.Nil {
		return AgentAnnouncementActor{}, ErrAgentAnnouncementDenied
	}
	return actor, nil
}

func (a AgentAnnouncementOwnerAuthority) ManageAgentInstallation(ctx context.Context, actor AgentAnnouncementActor, installation, persona, conversation string) (bool, error) {
	current, err := a.AgentAnnouncementActor(ctx)
	if err != nil || current != actor || a.Personas == nil || a.Authorizer == nil {
		return false, ErrAgentAnnouncementDenied
	}
	p, _ := trust.FromContext(ctx)
	scoped, err := a.Personas.Scoped(p.Tenant())
	if err != nil {
		return false, err
	}
	placements, err := scoped.ListActiveInstallations(ctx, conversation)
	if err != nil {
		return false, err
	}
	for _, placement := range placements {
		if placement.InstallationID != installation || placement.PersonaID != persona {
			continue
		}
		commandActor := PersonaAdminCommandActor{Principal: p, Tenant: p.Tenant(), Subject: p.Subject()}
		if err := a.Authorizer.AuthorizePersonaAdminCommand(ctx, commandActor, PersonaAdminInstall, placement.PersonaID); err != nil {
			return false, ErrAgentAnnouncementDenied
		}
		return true, nil
	}
	return false, ErrAgentAnnouncementDenied
}
