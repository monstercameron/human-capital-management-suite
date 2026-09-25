package projectservice

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// MembershipPolicy is implemented by the project membership store. It checks
// active membership and role grants from current server state.
type MembershipPolicy interface {
	Authorize(context.Context, string, string, string, projectaccess.Capability) error
	AuthorizeList(context.Context, string, string) error
}

type TenantProjectCreator interface {
	AuthorizeCreate(context.Context, string, string) error
}

// MembershipAuthorizer binds the service authorization port to the trusted
// tenant and subject in Principal. Caller supplied scope is never consulted.
type MembershipAuthorizer struct {
	Membership MembershipPolicy
	Creator    TenantProjectCreator
	Standing   InviteeEligibility
}

func (a MembershipAuthorizer) authorizeStanding(ctx context.Context, p *trust.Principal) error {
	if err := validPrincipal(p); err != nil {
		return err
	}
	if a.Standing == nil {
		return ErrUnavailable
	}
	_, err := a.Standing.CheckInvitee(ctx, p, p.Subject())
	return err
}

func (a MembershipAuthorizer) Authorize(ctx context.Context, p *trust.Principal, projectID string, cap projectaccess.Capability) error {
	if err := validPrincipal(p); err != nil {
		return err
	}
	if err := a.authorizeStanding(ctx, p); err != nil {
		return err
	}
	if a.Membership == nil {
		return ErrUnavailable
	}
	return a.Membership.Authorize(ctx, tenant(p), projectID, p.Subject(), cap)
}

func (a MembershipAuthorizer) AuthorizeCreate(ctx context.Context, p *trust.Principal) error {
	if err := validPrincipal(p); err != nil {
		return err
	}
	if err := a.authorizeStanding(ctx, p); err != nil {
		return err
	}
	if a.Creator == nil {
		return ErrUnavailable
	}
	return a.Creator.AuthorizeCreate(ctx, tenant(p), p.Subject())
}

func (a MembershipAuthorizer) AuthorizeListProjects(ctx context.Context, p *trust.Principal) error {
	if err := validPrincipal(p); err != nil {
		return err
	}
	if err := a.authorizeStanding(ctx, p); err != nil {
		return err
	}
	if a.Membership == nil {
		return ErrUnavailable
	}
	return a.Membership.AuthorizeList(ctx, tenant(p), p.Subject())
}
