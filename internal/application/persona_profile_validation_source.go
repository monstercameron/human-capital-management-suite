package application

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaProfileValidationUnavailable = errors.New("application: tenant persona profile validator unavailable")

// TenantPersonaProfileBuilder resolves a fresh tenant-specific validator for
// every validation request. It cannot validate without authenticated context.
type TenantPersonaProfileBuilder struct{ Source PersonaProfileBuilderSource }

// Build is intentionally unavailable because this legacy port has no context
// from which to derive the validator's tenant.
func (TenantPersonaProfileBuilder) Build(agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error) {
	return agentpersona.PersonaVersion{}, errPersonaProfileValidationUnavailable
}

// BuildForTenant verifies the request principal and delegates through a
// validator created for that same tenant.
func (b TenantPersonaProfileBuilder) BuildForTenant(ctx context.Context, tenant values.TenantId, profile agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error) {
	principal, ok := trust.FromContext(ctx)
	if b.Source == nil || !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || tenant.Validate() != nil || principal.Tenant() != tenant {
		return agentpersona.PersonaVersion{}, errPersonaProfileValidationUnavailable
	}
	validator, err := b.Source.ForTenant(ctx, tenant)
	if err != nil || validator == nil {
		return agentpersona.PersonaVersion{}, errPersonaProfileValidationUnavailable
	}
	if scoped, ok := validator.(ContextPersonaProfileBuilder); ok {
		return scoped.BuildForTenant(ctx, tenant, profile)
	}
	return validator.Build(profile)
}

func buildPersonaProfile(ctx context.Context, builder PersonaProfileBuilder, tenant values.TenantId, profile agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error) {
	principal, ok := trust.FromContext(ctx)
	if builder == nil || !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || tenant.Validate() != nil || principal.Tenant() != tenant {
		return agentpersona.PersonaVersion{}, errPersonaProfileValidationUnavailable
	}
	if scoped, ok := builder.(ContextPersonaProfileBuilder); ok {
		return scoped.BuildForTenant(ctx, tenant, profile)
	}
	return builder.Build(profile)
}

// PersonaProfileBuilderSourceFunc adapts trusted server wiring into a tenant-
// scoped profile validator source.
type PersonaProfileBuilderSourceFunc func(context.Context, values.TenantId) (PersonaProfileBuilder, error)

// ForTenant resolves one tenant's profile validator.
func (f PersonaProfileBuilderSourceFunc) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaProfileBuilder, error) {
	if f == nil || ctx == nil || tenant.Validate() != nil {
		return nil, errPersonaProfileValidationUnavailable
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant() != tenant {
		return nil, errPersonaProfileValidationUnavailable
	}
	return f(ctx, tenant)
}

// PersonaStarterManifestResolverSource creates a starter manifest reader for
// one tenant. Its result must resolve that tenant's current published pointer.
type PersonaStarterManifestResolverSource interface {
	ForTenant(context.Context, values.TenantId) (PersonaStarterManifestResolver, error)
}

// PersonaStarterManifestResolverSourceFunc adapts server wiring to a
// tenant-scoped starter manifest resolver source.
type PersonaStarterManifestResolverSourceFunc func(context.Context, values.TenantId) (PersonaStarterManifestResolver, error)

// ForTenant resolves the manifest reader for an authenticated tenant.
func (f PersonaStarterManifestResolverSourceFunc) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaStarterManifestResolver, error) {
	if f == nil || ctx == nil || tenant.Validate() != nil {
		return nil, errPersonaProfileValidationUnavailable
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant() != tenant {
		return nil, errPersonaProfileValidationUnavailable
	}
	return f(ctx, tenant)
}

// TenantPersonaStarterManifestResolver adapts a tenant resolver factory to
// the starter builder interface by deriving tenant identity from trust context.
type TenantPersonaStarterManifestResolver struct {
	Source PersonaStarterManifestResolverSource
}

// ResolveCurrentPersonaManifest resolves through the current authenticated
// tenant's isolated manifest store.
func (r TenantPersonaStarterManifestResolver) ResolveCurrentPersonaManifest(ctx context.Context, manifestID string) (agentmanifest.Manifest, error) {
	principal, ok := trust.FromContext(ctx)
	if r.Source == nil || !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || strings.TrimSpace(manifestID) == "" {
		return agentmanifest.Manifest{}, errPersonaProfileValidationUnavailable
	}
	resolver, err := r.Source.ForTenant(ctx, principal.Tenant())
	if err != nil || resolver == nil {
		return agentmanifest.Manifest{}, errPersonaProfileValidationUnavailable
	}
	return resolver.ResolveCurrentPersonaManifest(ctx, manifestID)
}

// PersonaStarterInstructionsResolverSource creates a digest-bound instruction
// reader for one tenant.
type PersonaStarterInstructionsResolverSource interface {
	ForTenant(context.Context, values.TenantId) (PersonaStarterInstructionsResolver, error)
}

// PersonaStarterInstructionsResolverSourceFunc adapts server wiring to a
// tenant-scoped instruction source.
type PersonaStarterInstructionsResolverSourceFunc func(context.Context, values.TenantId) (PersonaStarterInstructionsResolver, error)

// ForTenant resolves the instruction source for an authenticated tenant.
func (f PersonaStarterInstructionsResolverSourceFunc) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaStarterInstructionsResolver, error) {
	if f == nil || ctx == nil || tenant.Validate() != nil {
		return nil, errPersonaProfileValidationUnavailable
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant() != tenant {
		return nil, errPersonaProfileValidationUnavailable
	}
	return f(ctx, tenant)
}

// TenantPersonaStarterInstructionsResolver resolves instructions through the
// current authenticated tenant's content authority.
type TenantPersonaStarterInstructionsResolver struct {
	Source PersonaStarterInstructionsResolverSource
}

// ResolvePersonaInstructions derives tenant from verified request context and
// delegates the exact manifest version and digest.
func (r TenantPersonaStarterInstructionsResolver) ResolvePersonaInstructions(ctx context.Context, id string, version uint64, digest string) (string, error) {
	principal, ok := trust.FromContext(ctx)
	if r.Source == nil || !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return "", errPersonaProfileValidationUnavailable
	}
	resolver, err := r.Source.ForTenant(ctx, principal.Tenant())
	if err != nil || resolver == nil {
		return "", errPersonaProfileValidationUnavailable
	}
	return resolver.ResolvePersonaInstructions(ctx, id, version, digest)
}
