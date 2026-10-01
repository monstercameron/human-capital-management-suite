package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errPersonaRunSecurityLeaseResolver = errors.New("application: durable persona security lease unavailable")

// PersonaSecurityLeaseRepository resolves a durable admission lease only when
// its current revocation scopes and expiry remain valid.
type PersonaSecurityLeaseRepository interface {
	ResolveActivePersonaSecurityLease(context.Context, uuid.UUID, string, time.Time) (agentstore.PersonaSecurityLease, error)
}

// PersonaRunSecurityLeaseResolverConfig supplies trusted tenant identity
// conversion and a clock to the database-backed lease resolver.
type PersonaRunSecurityLeaseResolverConfig struct {
	Leases     PersonaSecurityLeaseRepository
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

// DatabasePersonaRunSecurityLeaseResolver adapts accepted durable admission
// and run evidence to the security-fence lease identifier.
type DatabasePersonaRunSecurityLeaseResolver struct {
	leases     PersonaSecurityLeaseRepository
	tenantUUID func(values.TenantId) uuid.UUID
	now        func() time.Time
}

// NewDatabasePersonaRunSecurityLeaseResolver builds a fail-closed production
// resolver. Pass agentstore.Store as Leases; it never derives authority from a
// provider credential or caller-selected lease identifier.
func NewDatabasePersonaRunSecurityLeaseResolver(cfg PersonaRunSecurityLeaseResolverConfig) (*DatabasePersonaRunSecurityLeaseResolver, error) {
	if isNilPersonaOutputPort(cfg.Leases) || cfg.TenantUUID == nil || cfg.Now == nil {
		return nil, errPersonaRunSecurityLeaseResolver
	}
	return &DatabasePersonaRunSecurityLeaseResolver{leases: cfg.Leases, tenantUUID: cfg.TenantUUID, now: cfg.Now}, nil
}

// ResolvePersonaRunSecurityLease verifies exact admission/run/tenant/persona
// bindings, then returns the active durable lease ID for the run fence.
func (r *DatabasePersonaRunSecurityLeaseResolver) ResolvePersonaRunSecurityLease(ctx context.Context, admission agentrun.Record, run runstate.Run) (agentsecurity.KillSwitchLeaseID, error) {
	if r == nil || ctx == nil || isNilPersonaOutputPort(r.leases) || r.tenantUUID == nil || r.now == nil ||
		!validPersonaRunLeaseEvidence(admission, run) {
		return "", errPersonaRunSecurityLeaseResolver
	}
	tenantID := r.tenantUUID(values.TenantId(admission.Request.Source.TenantID))
	if tenantID == uuid.Nil {
		return "", errPersonaRunSecurityLeaseResolver
	}
	lease, err := r.leases.ResolveActivePersonaSecurityLease(ctx, tenantID, admission.ID, r.now().UTC())
	if err != nil {
		return "", err
	}
	if !personaRunLeaseMatches(lease, tenantID, admission, run) {
		return "", errPersonaRunSecurityLeaseResolver
	}
	return agentsecurity.KillSwitchLeaseID(lease.LeaseID), nil
}

func validPersonaRunLeaseEvidence(admission agentrun.Record, run runstate.Run) bool {
	request := admission.Request
	if admission.Decision != agentrun.DecisionAccepted || strings.TrimSpace(admission.ID) == "" ||
		request.Source.Kind != agentrun.SourcePersonaMention || strings.TrimSpace(request.Source.TenantID) == "" ||
		request.Persona == nil || request.Principal.Mode != agentrun.ModeOnBehalfOf ||
		strings.TrimSpace(request.Principal.InvokerID) == "" || strings.TrimSpace(request.InstallationID) == "" ||
		strings.TrimSpace(admission.Authority.GrantRef) == "" || strings.TrimSpace(admission.Authority.PolicyDigest) == "" {
		return false
	}
	return run.ID == admission.ID && run.AdmissionID == admission.ID &&
		run.TenantID == request.Source.TenantID && run.PrincipalMode == request.Principal.Mode &&
		run.ActorID == request.Principal.InvokerID && run.AgentID == request.Agent.AgentID &&
		run.AgentVersion == request.Agent.Version && run.AgentDigest == request.Agent.Digest &&
		run.RequestDigest == admission.RequestDigest && run.Deadline.Equal(request.Deadline) &&
		request.Agent == admission.Authority.Agent && request.InstallationID == admission.Authority.InstallationID &&
		request.Principal.Mode == admission.Authority.Principal.Mode &&
		request.Principal.InvokerID == admission.Authority.Principal.InvokerID &&
		request.Audience == admission.Authority.Audience && request.Context == admission.Authority.Context
}

func personaRunLeaseMatches(lease agentstore.PersonaSecurityLease, tenantID uuid.UUID, admission agentrun.Record, run runstate.Run) bool {
	request := admission.Request
	return lease.LeaseID != "" && lease.TenantID == tenantID.String() && lease.AdmissionID == admission.ID &&
		lease.RunID == run.ID && lease.InvocationID != "" && lease.IssuerID != request.Principal.InvokerID &&
		lease.AuthorityRef == admission.Authority.GrantRef && lease.PolicyDigest == admission.Authority.PolicyDigest &&
		lease.PrincipalID == admission.Authority.Principal.InvokerID && lease.PersonaID == request.Persona.ID &&
		lease.PersonaVersion == request.Persona.Version && lease.InstallationID == request.InstallationID &&
		lease.Mode == string(agentrun.ModeOnBehalfOf) && lease.AdmissionDecision == string(agentrun.DecisionAccepted) &&
		lease.AdmissionDeadline.Equal(request.Deadline) && lease.ExpiresAt.After(lease.IssuedAt) &&
		lease.ExpiresAt.After(time.Time{})
}

var _ PersonaRunSecurityLeaseResolver = (*DatabasePersonaRunSecurityLeaseResolver)(nil)
