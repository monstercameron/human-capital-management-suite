package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errPersonaDurableSecurityFence = errors.New("application: durable persona security fence unavailable")

// PersonaSecurityStepStore durably records a security step while holding the
// lease's revocation-scope locks.
type PersonaSecurityStepStore interface {
	RunPersonaSecurityStep(context.Context, uuid.UUID, string, string, time.Time, func(context.Context) error) error
}

// PersonaRunLeaseFence applies the process-local kill switch to a bound run.
// DatabasePersonaRunSecurityFence composes it with durable store fencing.
type PersonaRunLeaseFence interface {
	Bind(agentsecurity.PersonaRunID, agentsecurity.KillSwitchLeaseID) error
	RunStep(agentsecurity.PersonaRunID, func() error) (agentsecurity.Fallback, error)
}

// DatabasePersonaRunSecurityFenceConfig contains only shared fence ports and a
// server-owned tenant mapping and clock. Tenant and lease values reach it from
// the accepted request and the independently resolved durable lease.
type DatabasePersonaRunSecurityFenceConfig struct {
	Steps      PersonaSecurityStepStore
	LeaseFence PersonaRunLeaseFence
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

type personaDurableRunBinding struct {
	tenant uuid.UUID
	lease  agentsecurity.KillSwitchLeaseID
}

// DatabasePersonaRunSecurityFence requires both the durable lease store and
// the process-local kill switch; neither authority can substitute for the other.
type DatabasePersonaRunSecurityFence struct {
	steps      PersonaSecurityStepStore
	leaseFence PersonaRunLeaseFence
	tenantUUID func(values.TenantId) uuid.UUID
	now        func() time.Time
	mu         sync.RWMutex
	bindings   map[agentsecurity.PersonaRunID]personaDurableRunBinding
}

// NewDatabasePersonaRunSecurityFence composes durable per-step fencing with
// the existing kill-switch fence. No local-only fallback is supplied.
func NewDatabasePersonaRunSecurityFence(cfg DatabasePersonaRunSecurityFenceConfig) (*DatabasePersonaRunSecurityFence, error) {
	if isNilPersonaOutputPort(cfg.Steps) || isNilPersonaOutputPort(cfg.LeaseFence) || cfg.TenantUUID == nil || cfg.Now == nil {
		return nil, errPersonaDurableSecurityFence
	}
	return &DatabasePersonaRunSecurityFence{
		steps: cfg.Steps, leaseFence: cfg.LeaseFence, tenantUUID: cfg.TenantUUID,
		now: cfg.Now, bindings: make(map[agentsecurity.PersonaRunID]personaDurableRunBinding),
	}, nil
}

// Bind pins one run to its admitted tenant and independently resolved lease.
func (f *DatabasePersonaRunSecurityFence) Bind(runID agentsecurity.PersonaRunID, tenant string, leaseID agentsecurity.KillSwitchLeaseID) error {
	if f == nil || isNilPersonaOutputPort(f.steps) || isNilPersonaOutputPort(f.leaseFence) ||
		f.tenantUUID == nil || strings.TrimSpace(string(runID)) == "" || tenant == "" || strings.TrimSpace(tenant) != tenant ||
		strings.TrimSpace(string(leaseID)) == "" {
		return errPersonaDurableSecurityFence
	}
	tenantID := f.tenantUUID(values.TenantId(tenant))
	if tenantID == uuid.Nil {
		return errPersonaDurableSecurityFence
	}
	binding := personaDurableRunBinding{tenant: tenantID, lease: leaseID}
	if err := f.leaseFence.Bind(runID, leaseID); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.bindings[runID]; ok && existing != binding {
		return errPersonaDurableSecurityFence
	}
	f.bindings[runID] = binding
	return nil
}

// RunStep executes only after the process-local fence accepts the run and the
// durable store records the unique step under tenant and lease scope locks.
func (f *DatabasePersonaRunSecurityFence) RunStep(ctx context.Context, runID agentsecurity.PersonaRunID, stepID string, work func(context.Context) error) (agentsecurity.Fallback, error) {
	if f == nil || ctx == nil || isNilPersonaOutputPort(f.steps) || isNilPersonaOutputPort(f.leaseFence) ||
		f.now == nil || strings.TrimSpace(string(runID)) == "" || strings.TrimSpace(stepID) == "" || work == nil {
		return agentsecurity.Fallback{}, errPersonaDurableSecurityFence
	}
	f.mu.RLock()
	binding, ok := f.bindings[runID]
	f.mu.RUnlock()
	if !ok || binding.tenant == uuid.Nil || binding.lease == "" {
		return agentsecurity.Fallback{}, errPersonaDurableSecurityFence
	}
	done := agentUXSpeedEvent(ctx, "store.security_step")
	defer done()
	return f.leaseFence.RunStep(runID, func() error {
		return f.steps.RunPersonaSecurityStep(ctx, binding.tenant, string(binding.lease), stepID, f.now().UTC(), work)
	})
}

var _ PersonaRunSecurityFence = (*DatabasePersonaRunSecurityFence)(nil)
