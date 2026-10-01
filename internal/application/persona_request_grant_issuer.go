package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrRequestScopedPersonaGrantIssuer reports a missing or mismatched request
// identity or an unavailable tenant-scoped grant store.
var ErrRequestScopedPersonaGrantIssuer = errors.New("application: request-scoped persona grant issuer unavailable")

// PersonaGrantTenantStoreFactory creates a grant store whose database calls
// use the supplied request context and whose rows are restricted to tenant.
type PersonaGrantTenantStoreFactory interface {
	ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error)
}

// RequestScopedPersonaGrantIssuerConfig supplies the live authority sources
// and tenant-store factory used to issue persona grants.
type RequestScopedPersonaGrantIssuerConfig struct {
	Personas PersonaAuthoritySource
	Identity TrustedInvokerSource
	Invokers InvokerAuthoritySource
	Stores   PersonaGrantTenantStoreFactory
	Now      func() time.Time
}

// RequestScopedPersonaGrantIssuer issues persona grants using a fresh
// tenant-bound store for each authenticated request. It retains no request
// context or tenant between calls.
type RequestScopedPersonaGrantIssuer struct {
	personas PersonaAuthoritySource
	identity TrustedInvokerSource
	invokers InvokerAuthoritySource
	stores   PersonaGrantTenantStoreFactory
	now      func() time.Time
}

var _ agentinvoke.GrantIssuer = (*RequestScopedPersonaGrantIssuer)(nil)

// NewRequestScopedPersonaGrantIssuer validates the production sources needed
// to resolve current persona authority and persist each grant per request.
func NewRequestScopedPersonaGrantIssuer(cfg RequestScopedPersonaGrantIssuerConfig) (*RequestScopedPersonaGrantIssuer, error) {
	if cfg.Personas == nil || cfg.Identity == nil || cfg.Invokers == nil || cfg.Stores == nil || cfg.Now == nil {
		return nil, fmt.Errorf("%w: persona, identity, invoker, tenant store, and clock sources are required", ErrRequestScopedPersonaGrantIssuer)
	}
	return &RequestScopedPersonaGrantIssuer{personas: cfg.Personas, identity: cfg.Identity, invokers: cfg.Invokers, stores: cfg.Stores, now: cfg.Now}, nil
}

// CreateOnBehalfOfGrant requires the request's verified human identity to
// match the grant subject and tenant, then scopes every durable read/write to
// that authenticated tenant and request context.
func (i *RequestScopedPersonaGrantIssuer) CreateOnBehalfOfGrant(ctx context.Context, request agentinvoke.GrantRequest) (agentinvoke.DelegationGrant, error) {
	if i == nil || i.stores == nil || i.now == nil || ctx == nil {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: issuer and request context are required", ErrRequestScopedPersonaGrantIssuer)
	}
	if err := ctx.Err(); err != nil {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: %v", ErrRequestScopedPersonaGrantIssuer, err)
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal.SubjectKind() != trust.SubjectKindHuman || principal.Subject() != request.UserID || principal.Tenant() != values.TenantId(request.TenantID) {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: authenticated human must match grant subject and tenant", ErrRequestScopedPersonaGrantIssuer)
	}
	if strings.TrimSpace(request.TargetAgentID) == "" {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: exact target agent is required", agentinvoke.ErrInvalidRequest)
	}
	now := i.now().UTC()
	if now.Before(principal.IssuedAt()) || !now.Before(principal.ExpiresAt()) {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: authenticated identity is outside its validity window", ErrRequestScopedPersonaGrantIssuer)
	}
	store, err := i.stores.ForTenant(ctx, principal.Tenant())
	if err != nil {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("scope persona grants to authenticated tenant: %w", err)
	}
	if store == nil {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: tenant store factory returned nil", ErrRequestScopedPersonaGrantIssuer)
	}
	service, err := agentdelegation.NewService(agentdelegation.Config{
		Store: store, Authority: agentdelegation.ResolverFunc(unusedPersonaGrantResolver), Now: i.now,
	})
	if err != nil {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("initialize request-scoped delegation service: %w", err)
	}
	adapter := PersonaAuthorityAdapter{Personas: i.personas, Identity: i.identity, Invokers: i.invokers, Grants: service, Store: store, Now: i.now}
	grant, err := adapter.CreateOnBehalfOfGrant(ctx, request)
	if err != nil {
		return agentinvoke.DelegationGrant{}, err
	}
	stored, err := store.Get(grant.ID)
	if err != nil {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("confirm durable persona grant: %w", err)
	}
	epoch := store.CurrentRevocationEpoch(principal.Tenant(), principal.Subject())
	if epoch == 0 || stored.RevocationEpoch != epoch || stored.Revoked || stored.GrantID != grant.ID ||
		stored.Tenant != principal.Tenant() || stored.UserID != principal.Subject() || stored.AgentVersion != request.AgentVersion ||
		stored.TargetAgentID != request.TargetAgentID || stored.Authority.Delegate != request.TargetAgentID ||
		stored.InstallationID != request.InstallationID || stored.TaskID != request.InvocationID ||
		grant.UserID != principal.Subject() || grant.TenantID != string(principal.Tenant()) ||
		grant.TaskID != request.InvocationID || grant.TargetAgentID != request.TargetAgentID || !grant.ExpiresAt.Equal(stored.ExpiresAt) {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: persisted grant binding or revocation epoch changed", ErrRequestScopedPersonaGrantIssuer)
	}
	return grant, nil
}

// unusedPersonaGrantResolver fails closed if the scoped creation service is
// ever asked to resolve authority through its legacy flat-authority API.
func unusedPersonaGrantResolver(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
	return agentdelegation.UserAuthority{}, fmt.Errorf("%w: request-scoped persona grants require per-skill authority", ErrRequestScopedPersonaGrantIssuer)
}
