package application

import (
	"context"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type PersonaOnboardingAuthority interface {
	AuthorizePersonaOnboarding(context.Context, *trust.Principal, values.EntityRef) error
}

type PersonaOnboardingDirectory interface {
	AgentRoleDirectory
	AgentOrganizationDirectory
	AgentSubjectDirectory
	AgentFieldPolicy
}

// CurrentPersonaOnboardingAuthority resolves current directory grants for each
// exact target. Token roles and same-tenant worker presence grant no target scope.
type CurrentPersonaOnboardingAuthority struct {
	directory PersonaOnboardingDirectory
	now       func() time.Time
}

func NewCurrentPersonaOnboardingAuthority(directory PersonaOnboardingDirectory, now func() time.Time) (*CurrentPersonaOnboardingAuthority, error) {
	if isNilPersonaOutputPort(directory) || now == nil {
		return nil, ErrPersonaOnboardingFacts
	}
	return &CurrentPersonaOnboardingAuthority{directory: directory, now: now}, nil
}

func (a *CurrentPersonaOnboardingAuthority) AuthorizePersonaOnboarding(ctx context.Context, p *trust.Principal, worker values.EntityRef) error {
	if a == nil || ctx == nil || p == nil || a.now == nil || isNilPersonaOutputPort(a.directory) || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant() != worker.Tenant || worker.Validate() != nil || worker.Kind != "worker" || !p.AuthorizesPurpose("persona-mention") {
		return ErrPersonaOnboardingFacts
	}
	current, ok := trust.FromContext(ctx)
	now := a.now().UTC()
	if !ok || current != p || now.Before(p.IssuedAt()) || !now.Before(p.ExpiresAt()) {
		return ErrPersonaOnboardingFacts
	}
	roles, err := a.directory.CurrentRoles(ctx, worker.Tenant, p.Subject())
	if err != nil || (!slices.Contains(roles, string(authz.RoleManager)) && !slices.Contains(roles, string(authz.RoleHRPartner))) {
		return ErrPersonaOnboardingFacts
	}
	organizations, err := a.directory.CurrentOrganizationScopes(ctx, worker.Tenant, p.Subject(), roles)
	if err != nil || organizations == nil {
		return ErrPersonaOnboardingFacts
	}
	subjects, err := a.directory.CurrentSubjects(ctx, worker.Tenant, p.Subject(), "persona-mention", roles, organizations)
	if err != nil {
		return ErrPersonaOnboardingFacts
	}
	selected := []agentgate.Subject{}
	for _, subject := range subjects {
		if subject.Ref == worker && subject.Organization.Tenant == worker.Tenant && !subject.Organization.IsZero() {
			selected = append(selected, subject)
		}
	}
	if len(selected) != 1 {
		return ErrPersonaOnboardingFacts
	}
	fields, err := a.directory.CurrentFields(ctx, p, "persona-mention", roles, organizations, selected)
	// Existing policy's core field proves basic target disclosure. Protected
	// requirement accounts remain redacted under the lifecycle manager view.
	if err != nil || !slices.Contains(fields, authz.FieldWorkerNumber) {
		return ErrPersonaOnboardingFacts
	}
	return nil
}
