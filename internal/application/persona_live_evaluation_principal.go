package application

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/data/governance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PersonaCandidatePrincipalResolver uses the reviewed version's immutable
// production identity binding while executing as its separately provisioned
// synthetic-tenant service identity. It does not create a synthetic published
// version or installation. Both tenant-owned trust records must be current.
type PersonaCandidatePrincipalResolver struct {
	Target     agenteval.PersonaEvaluationTarget
	Scope      PersonaCandidateScope
	Bindings   PersonaAgentPrincipalBindingReader
	Principals PersonaPrincipalAuthority
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

func (r *PersonaCandidatePrincipalResolver) Resolve(ctx context.Context, tenant values.TenantId, persona string, version int64) (string, error) {
	if r == nil || ctx == nil || r.Scope == nil || r.Bindings == nil || r.Principals == nil || r.TenantUUID == nil || r.Now == nil ||
		tenant.String() != r.Target.SyntheticTenantID || tenant.Validate() != nil ||
		r.Target.TenantID == r.Target.SyntheticTenantID || values.TenantId(r.Target.TenantID).Validate() != nil ||
		persona != r.Target.PersonaID || !required(persona) || version != r.Target.PersonaVersion || version <= 0 {
		return "", agenteval.ErrPersonaEvaluation
	}
	now := r.Now().UTC()
	verified, ok := trust.FromContext(ctx)
	if now.IsZero() || !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Tenant() != tenant ||
		verified.Subject() != r.Target.InvokerID || !verified.AuthorizesPurpose("persona-mention") ||
		verified.IssuedAt().After(now) || !verified.ExpiresAt().After(now) {
		return "", agenteval.ErrPersonaEvaluation
	}
	production := values.TenantId(r.Target.TenantID)
	productionID, syntheticID := r.TenantUUID(production), r.TenantUUID(tenant)
	if productionID == uuid.Nil || syntheticID == uuid.Nil || productionID == syntheticID {
		return "", agenteval.ErrPersonaEvaluation
	}
	if err := r.Scope.AuthorizeSyntheticPersonaEvaluation(ctx, r.Target); err != nil {
		return "", agenteval.ErrPersonaEvaluation
	}
	binding, err := r.Bindings.ResolvePersonaAgentPrincipal(ctx, production, persona, version)
	if err != nil || binding.TenantID != production.String() || binding.PersonaID != persona || binding.PersonaVersion != version ||
		binding.PrincipalID == uuid.Nil || binding.ProvisionedAt.IsZero() || binding.ProvisionedAt.After(now) {
		return "", agenteval.ErrPersonaEvaluation
	}
	for _, owner := range []struct {
		tenant values.TenantId
		id     uuid.UUID
	}{{production, productionID}, {tenant, syntheticID}} {
		principal, err := r.Principals.CurrentPrincipal(ctx, owner.tenant, binding.PrincipalID)
		if err != nil || !candidatePrincipalCurrent(principal, owner.id, binding.PrincipalID, now) {
			return "", agenteval.ErrPersonaEvaluation
		}
	}
	return binding.PrincipalID.String(), nil
}

func candidatePrincipalCurrent(principal governance.Principal, tenant, id uuid.UUID, now time.Time) bool {
	return principal.TenantID == tenant && principal.PrincipalID == id && principal.Kind == "SERVICE" &&
		principal.Lifecycle == "ACTIVE" && (principal.ExpiresAt == nil || principal.ExpiresAt.After(now))
}
