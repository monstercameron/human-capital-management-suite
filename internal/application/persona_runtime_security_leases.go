package application

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaRuntimeSecurityLeaseStore interface {
	PersonaSecurityLeaseRepository
	IssuePersonaSecurityLease(context.Context, agentstore.PersonaSecurityLeaseRequest) (agentstore.PersonaSecurityLease, error)
}

// personaRuntimeSecurityLeases issues a durable lease from an accepted stored
// admission before registering its process-local projection. Durable revocation
// remains authoritative at every model, tool, output, and delivery step.
type personaRuntimeSecurityLeases struct {
	store       personaRuntimeSecurityLeaseStore
	switchBoard *agentsecurity.KillSwitch
	tenantUUID  func(values.TenantId) uuid.UUID
	now         func() time.Time
	mu          sync.Mutex
	registered  map[string]agentstore.PersonaSecurityLease
}

func (s *personaRuntimeSecurityLeases) ResolvePersonaRunSecurityLease(ctx context.Context, admission agentrun.Record, run runstate.Run) (agentsecurity.KillSwitchLeaseID, error) {
	if s == nil || ctx == nil || s.store == nil || s.switchBoard == nil || s.tenantUUID == nil || s.now == nil || !validPersonaRunLeaseEvidence(admission, run) {
		return "", errPersonaRunSecurityLeaseResolver
	}
	tenant := s.tenantUUID(values.TenantId(admission.Request.Source.TenantID))
	if tenant == uuid.Nil {
		return "", errPersonaRunSecurityLeaseResolver
	}
	now := s.now().UTC()
	if now.IsZero() || !admission.Request.Deadline.After(now) {
		return "", errPersonaRunSecurityLeaseResolver
	}
	lease, err := s.store.IssuePersonaSecurityLease(ctx, agentstore.PersonaSecurityLeaseRequest{TenantID: tenant, AdmissionID: admission.ID, IssuedAt: now, ExpiresAt: admission.Request.Deadline})
	if err != nil {
		return "", err
	}
	active, err := s.store.ResolveActivePersonaSecurityLease(ctx, tenant, admission.ID, now)
	if err != nil || active.LeaseID != lease.LeaseID || !personaRunLeaseMatches(active, tenant, admission, run) {
		return "", errPersonaRunSecurityLeaseResolver
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.registered == nil {
		s.registered = make(map[string]agentstore.PersonaSecurityLease)
	}
	if held, ok := s.registered[active.LeaseID]; ok {
		if held != active {
			return "", errPersonaRunSecurityLeaseResolver
		}
	} else {
		if err := s.switchBoard.Grant(agentsecurity.Lease{ID: active.LeaseID, Agent: admission.Request.Agent.AgentID, AgentVersion: admission.Request.Agent.Version,
			PersonaID: active.PersonaID, PersonaVersion: active.PersonaVersion, InstallationID: active.InstallationID, Tenant: admission.Request.Source.TenantID}); err != nil {
			return "", err
		}
		s.registered[active.LeaseID] = active
	}
	return agentsecurity.KillSwitchLeaseID(active.LeaseID), nil
}

var _ PersonaRunSecurityLeaseResolver = (*personaRuntimeSecurityLeases)(nil)
