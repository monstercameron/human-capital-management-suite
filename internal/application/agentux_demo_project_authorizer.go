package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type AgentUXDemoSupportProjectGrants interface {
	SupportProjectCreateAllowed(context.Context, uuid.UUID, string, string) (bool, error)
}

// The service exception is exactly CreateTask on a named project. It never
// assigns a contributor role (which would also grant edit/move/delete).
type AgentUXDemoSupportProjectAuthorizer struct {
	Human      projectservice.Authorizer
	Grants     AgentUXDemoSupportProjectGrants
	TenantUUID func(values.TenantId) uuid.UUID
}

func (a AgentUXDemoSupportProjectAuthorizer) Authorize(ctx context.Context, p *trust.Principal, project string, action projectaccess.Capability) error {
	if p == nil || ctx == nil {
		return projectaccess.ErrUnauthorized
	}
	if p.SubjectKind() == trust.SubjectKindHuman {
		if a.Human == nil {
			return projectservice.ErrUnavailable
		}
		return a.Human.Authorize(ctx, p, project, action)
	}
	if p.SubjectKind() != trust.SubjectKindService || action != projectaccess.CreateTask || a.Grants == nil || a.TenantUUID == nil {
		return projectaccess.ErrUnauthorized
	}
	allowed, err := a.Grants.SupportProjectCreateAllowed(ctx, a.TenantUUID(p.Tenant()), p.Subject(), project)
	if err != nil || !allowed {
		return projectaccess.ErrUnauthorized
	}
	return nil
}

func (a AgentUXDemoSupportProjectAuthorizer) AuthorizeCreate(ctx context.Context, p *trust.Principal) error {
	if p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return projectaccess.ErrUnauthorized
	}
	if a.Human == nil {
		return projectservice.ErrUnavailable
	}
	return a.Human.AuthorizeCreate(ctx, p)
}

func (a AgentUXDemoSupportProjectAuthorizer) AuthorizeListProjects(ctx context.Context, p *trust.Principal) error {
	if p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return projectaccess.ErrUnauthorized
	}
	if a.Human == nil {
		return projectservice.ErrUnavailable
	}
	return a.Human.AuthorizeListProjects(ctx, p)
}

var _ projectservice.Authorizer = AgentUXDemoSupportProjectAuthorizer{}
