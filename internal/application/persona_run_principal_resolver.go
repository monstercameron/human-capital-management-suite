package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/governance"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaRunPrincipal = errors.New("application: persona run principal unavailable")

// PersonaAgentPrincipalBindingReader reads one exact, tenant-scoped persona
// version binding from the agent control store.
type PersonaAgentPrincipalBindingReader interface {
	ResolvePersonaAgentPrincipal(context.Context, values.TenantId, string, int64) (agentpersonastore.PersonaAgentPrincipalBinding, error)
}

// AgentPersonaRunPrincipalBindingReader scopes each principal binding read to
// the invocation tenant in the isolated agent store.
type AgentPersonaRunPrincipalBindingReader struct{ Store *agentpersonastore.Store }

// ResolvePersonaAgentPrincipal reads a binding from a tenant-scoped agent
// store transaction.
func (r AgentPersonaRunPrincipalBindingReader) ResolvePersonaAgentPrincipal(ctx context.Context, tenant values.TenantId, personaID string, version int64) (agentpersonastore.PersonaAgentPrincipalBinding, error) {
	if r.Store == nil || ctx == nil || tenant.Validate() != nil {
		return agentpersonastore.PersonaAgentPrincipalBinding{}, errPersonaRunPrincipal
	}
	scoped, err := r.Store.Scoped(tenant)
	if err != nil {
		return agentpersonastore.PersonaAgentPrincipalBinding{}, err
	}
	return scoped.ResolvePersonaAgentPrincipal(ctx, personaID, version)
}

// PersonaPrincipalAuthority loads a current principal from the trust owner.
type PersonaPrincipalAuthority interface {
	CurrentPrincipal(context.Context, values.TenantId, uuid.UUID) (governance.Principal, error)
}

// PersonaAgentPrincipalBindingWriter creates one immutable binding after
// principal authority has been verified.
type PersonaAgentPrincipalBindingWriter interface {
	RegisterPersonaAgentPrincipal(context.Context, values.TenantId, string, int64, uuid.UUID, time.Time) error
}

// AgentPersonaRunPrincipalBindingWriter scopes principal binding writes to
// the tenant's isolated agent store.
type AgentPersonaRunPrincipalBindingWriter struct{ Store *agentpersonastore.Store }

// RegisterPersonaAgentPrincipal records the exact binding through the
// tenant-scoped agent store.
func (w AgentPersonaRunPrincipalBindingWriter) RegisterPersonaAgentPrincipal(ctx context.Context, tenant values.TenantId, personaID string, version int64, principalID uuid.UUID, at time.Time) error {
	if w.Store == nil || ctx == nil || tenant.Validate() != nil {
		return errPersonaRunPrincipal
	}
	scoped, err := w.Store.Scoped(tenant)
	if err != nil {
		return err
	}
	return scoped.RegisterPersonaAgentPrincipal(ctx, personaID, version, principalID, at)
}

// PersonaAgentPrincipalProvisioner adds an exact persona-version binding only
// after confirming that the referenced tenant principal is an active service.
type PersonaAgentPrincipalProvisioner struct {
	Bindings   PersonaAgentPrincipalBindingWriter
	Principals PersonaPrincipalAuthority
	Now        func() time.Time
}

// Provision validates the live core principal and stores its exact version
// binding. Duplicate bindings remain errors and cannot replace identity.
func (p PersonaAgentPrincipalProvisioner) Provision(ctx context.Context, tenant values.TenantId, personaID string, version int64, principalID uuid.UUID) error {
	if ctx == nil || tenant.Validate() != nil || strings.TrimSpace(string(tenant)) != string(tenant) || strings.TrimSpace(personaID) == "" || version <= 0 || principalID == uuid.Nil || p.Bindings == nil || p.Principals == nil || p.Now == nil {
		return errPersonaRunPrincipal
	}
	principal, err := p.Principals.CurrentPrincipal(ctx, tenant, principalID)
	if err != nil {
		return fmt.Errorf("%w: validate provisioned principal: %w", errPersonaRunPrincipal, err)
	}
	now := p.Now().UTC()
	if principal.TenantID == uuid.Nil || principal.PrincipalID != principalID || principal.Kind != "SERVICE" || principal.Lifecycle != "ACTIVE" || now.IsZero() || principal.ExpiresAt != nil && !principal.ExpiresAt.After(now) {
		return fmt.Errorf("%w: principal is not an active tenant service identity", errPersonaRunPrincipal)
	}
	if err := p.Bindings.RegisterPersonaAgentPrincipal(ctx, tenant, personaID, version, principalID, now); err != nil {
		return fmt.Errorf("%w: persist version binding: %w", errPersonaRunPrincipal, err)
	}
	return nil
}

// GovernancePersonaPrincipalAuthority reads the principal from the core trust
// database with tenant RLS established on the same transaction.
type GovernancePersonaPrincipalAuthority struct {
	DB         dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
}

// CurrentPrincipal loads one tenant-owned trust principal. It does not accept
// an unscoped query handle or a caller-supplied lifecycle claim.
func (a GovernancePersonaPrincipalAuthority) CurrentPrincipal(ctx context.Context, tenant values.TenantId, principalID uuid.UUID) (governance.Principal, error) {
	if ctx == nil || tenant.Validate() != nil || principalID == uuid.Nil || a.DB == nil || a.TenantUUID == nil {
		return governance.Principal{}, errPersonaRunPrincipal
	}
	tenantID := a.TenantUUID(tenant)
	if tenantID == uuid.Nil {
		return governance.Principal{}, errPersonaRunPrincipal
	}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return governance.Principal{}, fmt.Errorf("begin trust principal read: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return governance.Principal{}, fmt.Errorf("scope trust principal read: %w", err)
	}
	principal, err := governance.LoadPrincipal(ctx, tx, tenantID, principalID)
	if err != nil {
		return governance.Principal{}, fmt.Errorf("load trust principal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return governance.Principal{}, fmt.Errorf("commit trust principal read: %w", err)
	}
	return principal, nil
}

// PersonaRunAgentPrincipalResolver validates the exact version binding against
// the tenant's live trust principal before returning its canonical ID.
type PersonaRunAgentPrincipalResolver struct {
	Bindings   PersonaAgentPrincipalBindingReader
	Principals PersonaPrincipalAuthority
	Now        func() time.Time
}

// Resolve returns a provisioned active SERVICE principal for this exact
// persona version. Persona IDs and chat Agent IDs are never treated as
// principal identities.
func (r PersonaRunAgentPrincipalResolver) Resolve(ctx context.Context, tenant values.TenantId, personaID string, version int64) (string, error) {
	if ctx == nil || tenant.Validate() != nil || strings.TrimSpace(string(tenant)) != string(tenant) || strings.TrimSpace(personaID) == "" || version <= 0 || r.Bindings == nil || r.Principals == nil || r.Now == nil {
		return "", errPersonaRunPrincipal
	}
	binding, err := r.Bindings.ResolvePersonaAgentPrincipal(ctx, tenant, personaID, version)
	if err != nil {
		return "", fmt.Errorf("%w: resolve exact version binding: %w", errPersonaRunPrincipal, err)
	}
	if binding.TenantID != string(tenant) || binding.PersonaID != personaID || binding.PersonaVersion != version || binding.PrincipalID == uuid.Nil {
		return "", fmt.Errorf("%w: version binding is outside tenant or persona scope", errPersonaRunPrincipal)
	}
	principal, err := r.Principals.CurrentPrincipal(ctx, tenant, binding.PrincipalID)
	if err != nil {
		return "", fmt.Errorf("%w: load trust principal: %w", errPersonaRunPrincipal, err)
	}
	now := r.Now().UTC()
	if principal.TenantID == uuid.Nil || principal.PrincipalID != binding.PrincipalID || principal.Kind != "SERVICE" || principal.Lifecycle != "ACTIVE" || now.IsZero() || principal.ExpiresAt != nil && !principal.ExpiresAt.After(now) {
		return "", fmt.Errorf("%w: bound principal is not an active tenant service identity", errPersonaRunPrincipal)
	}
	return principal.PrincipalID.String(), nil
}

var _ PersonaPrincipalAuthority = GovernancePersonaPrincipalAuthority{}

// PersonaRunLegalEntityResolver derives the invoking human's legal entity
// from its current workforce identity and tenant-owned employment records.
type PersonaRunLegalEntityResolver struct {
	DB         dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

// Resolve returns the one active legal entity recorded for the verified
// invoker. Missing or multiple current employments are rejected as ambiguous.
func (r PersonaRunLegalEntityResolver) Resolve(ctx context.Context, invocation agentinvoke.RunRequest) (string, error) {
	if ctx == nil || r.DB == nil || r.TenantUUID == nil || r.Now == nil || invocation.Mode != agentinvoke.OnBehalfOf || strings.TrimSpace(invocation.TenantID) == "" || strings.TrimSpace(invocation.InvokerID) == "" {
		return "", errPersonaRunPrincipal
	}
	tenant := values.TenantId(invocation.TenantID)
	if tenant.Validate() != nil || strings.TrimSpace(string(tenant)) != string(tenant) {
		return "", errPersonaRunPrincipal
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Tenant() != tenant || verified.Subject() != invocation.InvokerID {
		return "", fmt.Errorf("%w: verified invoker required", errPersonaRunPrincipal)
	}
	tenantID := r.TenantUUID(tenant)
	now := r.Now().UTC()
	if tenantID == uuid.Nil || now.IsZero() {
		return "", errPersonaRunPrincipal
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: begin workforce authority read: %w", errPersonaRunPrincipal, err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return "", fmt.Errorf("%w: scope workforce authority read: %w", errPersonaRunPrincipal, err)
	}
	worker, found, err := (workforce.Store{}).Get(ctx, tx, tenantID, invocation.InvokerID)
	if err != nil || !found || worker.TenantID != tenantID || !strings.EqualFold(worker.LifecycleStatus, "active") {
		return "", fmt.Errorf("%w: current invoker workforce identity unavailable", errPersonaRunPrincipal)
	}
	employments, err := (aggregates.PeopleStore{}).ActiveEmploymentsForWorker(ctx, tx, tenantID, worker.WorkerID, now)
	if err != nil {
		return "", fmt.Errorf("%w: resolve current employment: %w", errPersonaRunPrincipal, err)
	}
	legalEntityID, err := personaRunLegalEntityFromEmployment(tenantID, employments)
	if err != nil {
		return "", err
	}
	legal, err := (aggregates.OrganizationStore{}).CurrentLegalEntity(ctx, tx, tenantID, legalEntityID, now)
	if err != nil || legal.Tenant != tenantID || legal.EntityID != legalEntityID || legal.LifecycleState != "ACTIVE" {
		return "", fmt.Errorf("%w: current employing legal entity unavailable", errPersonaRunPrincipal)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("%w: commit owner fact read: %w", errPersonaRunPrincipal, err)
	}
	return legal.EntityID.String(), nil
}

func personaRunLegalEntityFromEmployment(tenant uuid.UUID, employments []aggregates.Employment) (uuid.UUID, error) {
	if len(employments) != 1 || (employments[0].EmploymentStatus != "ACTIVE" && employments[0].EmploymentStatus != "REINSTATED") || employments[0].Tenant != tenant || employments[0].LegalEntityRef == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: invoker does not have exactly one active current employment", errPersonaRunPrincipal)
	}
	return employments[0].LegalEntityRef, nil
}
