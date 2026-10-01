package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/resources"
)

var ErrAgentResourceIdentity = errors.New("application: agent resource identity is not bound to durable work")

// DefaultAgentResourcePolicy is the conservative local cell capacity envelope.
// The composition root may supply a separately reviewed deployment policy.
func DefaultAgentResourcePolicy(cell string) resources.Policy {
	if cell == "" {
		cell = "cell-local"
	}
	return resources.Policy{CellID: cell, MaxConcurrent: 16, MaxConcurrentPerTenant: 4, MaxConcurrentPerUser: 2, MaxConcurrentPerTask: 1,
		MaxQueued: 128, MaxQueuedPerTenant: 32, MaxQueuedPerUser: 8, MaxQueuedPerTask: 2, ReservedInteractive: 4,
		ReservedInteractivePerTenant: 1, ReservedInteractivePerUser: 1,
		LaneConcurrency:     map[resources.Lane]int{resources.LaneInteractive: 12, resources.LaneAutonomous: 4, resources.LaneEvaluation: 2, resources.LaneMaintenance: 1, resources.LaneRollout: 1},
		ProviderConcurrency: map[string]int{"openai": 8, "local-dev-deterministic-fake": 4}}
}

// AgentResourceIdentity is resolved by the server from admitted work, never
// from a prompt or client-selected pool. User and task counters are tenant scoped.
type AgentResourceIdentity struct {
	TenantID, UserID, TaskID string
	Lane                     resources.Lane
}

type AgentResourceRuntimeConfig struct {
	Policy           resources.Policy
	ResolveIdentity  func(context.Context, string, string) (AgentResourceIdentity, error)
	ObserveAdmission func(AgentResourceObservation)
}

// AgentResourceObservation records one actual admission attempt. PoolWait
// measures waiting at this boundary; no workflow/chat SLO is inferred from it.
type AgentResourceObservation struct {
	Identity   AgentResourceIdentity
	ProviderID string
	PoolWait   time.Duration
	Outcome    string
}

// AgentResourceRuntime shares worker capacity between persona model calls and
// durable task steps. The provider pool is separate so a step holding worker
// capacity can reserve its selected provider without reserving a second worker.
type AgentResourceRuntime struct {
	cell            string
	work, providers *resources.Coordinator
	resolve         func(context.Context, string, string) (AgentResourceIdentity, error)
	observe         func(AgentResourceObservation)
}

func NewAgentResourceRuntime(cfg AgentResourceRuntimeConfig) (*AgentResourceRuntime, error) {
	if cfg.Policy.MaxConcurrentPerUser < 1 || cfg.Policy.MaxConcurrentPerTask < 1 || cfg.Policy.MaxQueuedPerUser < 1 || cfg.Policy.MaxQueuedPerTask < 1 {
		return nil, resources.ErrInvalid
	}
	workPolicy := cfg.Policy
	workPolicy.ProviderConcurrency = nil
	work, err := resources.New(workPolicy)
	if err != nil {
		return nil, err
	}
	providers, err := resources.New(cfg.Policy)
	if err != nil {
		return nil, err
	}
	return &AgentResourceRuntime{cell: cfg.Policy.CellID, work: work, providers: providers, resolve: cfg.ResolveIdentity, observe: cfg.ObserveAdmission}, nil
}

// AgentResourceLease releases both reservations exactly once. PoolWait is
// observed with the monotonic clock around admission; it is not an SLO claim.
type AgentResourceLease struct {
	PoolWait       time.Duration
	runtime        *AgentResourceRuntime
	identity       AgentResourceIdentity
	work, provider *resources.Lease
	providerID     string
	once           sync.Once
	mu             sync.Mutex
	released       bool
}

func (l *AgentResourceLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.mu.Lock()
		l.released = true
		l.mu.Unlock()
		if l.provider != nil {
			l.provider.Release()
		}
		if l.work != nil {
			l.work.Release()
		}
	})
}

type agentResourceContextKey struct{}

// Context propagates an active worker reservation only through server code.
func (l *AgentResourceLease) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, agentResourceContextKey{}, l)
}

func (l *AgentResourceLease) matches(runtime *AgentResourceRuntime, identity AgentResourceIdentity) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return !l.released && l.runtime == runtime && l.identity == identity && l.work != nil
}

func validAgentResourceIdentity(identity AgentResourceIdentity) bool {
	for _, value := range []string{identity.TenantID, identity.UserID, identity.TaskID} {
		if value == "" || strings.TrimSpace(value) != value {
			return false
		}
	}
	return identity.Lane != ""
}

// Acquire reserves a worker and optionally a selected provider. Cancellation
// while waiting for the provider releases the worker before returning.
func (r *AgentResourceRuntime) Acquire(ctx context.Context, identity AgentResourceIdentity, provider string) (acquired *AgentResourceLease, failure error) {
	if r == nil || ctx == nil || !validAgentResourceIdentity(identity) {
		return nil, ErrAgentResourceIdentity
	}
	started := time.Now()
	if r.observe != nil {
		defer func() {
			outcome := "ADMITTED"
			if failure != nil {
				outcome = "REFUSED"
				if errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded) {
					outcome = "CANCELLED"
				}
				if errors.Is(failure, resources.ErrPressureShed) || errors.Is(failure, resources.ErrQueueFull) {
					outcome = "SHED"
				}
			}
			r.observe(AgentResourceObservation{Identity: identity, ProviderID: provider, PoolWait: time.Since(started), Outcome: outcome})
		}()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request := resources.Request{CellID: r.cell, TenantID: identity.TenantID, UserID: identity.UserID, TaskID: identity.TaskID, Lane: identity.Lane}
	result := &AgentResourceLease{runtime: r, identity: identity}
	parent, _ := ctx.Value(agentResourceContextKey{}).(*AgentResourceLease)
	if !parent.matches(r, identity) {
		work, err := r.work.Acquire(ctx, request)
		if err != nil {
			return nil, err
		}
		result.work = work
	}
	if provider != "" {
		if parent.matches(r, identity) && parent.provider != nil && parent.providerID != provider {
			result.Release()
			return nil, ErrAgentResourceIdentity
		}
		if parent.matches(r, identity) && parent.providerID == provider && parent.provider != nil {
			result.PoolWait = time.Since(started)
			return result, nil
		}
		request.Provider = provider
		lease, err := r.providers.Acquire(ctx, request)
		if err != nil {
			result.Release()
			return nil, err
		}
		result.provider = lease
		result.providerID = provider
	}
	result.PoolWait = time.Since(started)
	return result, nil
}

// AcquireAgentModelResources verifies the model request against the durable
// actor before reserving the provider actually selected by the router.
func (r *AgentResourceRuntime) AcquireAgentModelResources(ctx context.Context, request AgentModelResourceRequest) (func(), error) {
	if r == nil || ctx == nil || request.ProviderID == "" {
		return nil, ErrAgentResourceIdentity
	}
	parent, _ := ctx.Value(agentResourceContextKey{}).(*AgentResourceLease)
	var identity AgentResourceIdentity
	if parent != nil && parent.runtime == r {
		identity = parent.identity
		if !parent.matches(r, identity) {
			return nil, ErrAgentResourceIdentity
		}
	} else {
		if r.resolve == nil {
			return nil, ErrAgentResourceIdentity
		}
		var err error
		identity, err = r.resolve(ctx, request.TenantID, request.TaskID)
		if err != nil {
			return nil, err
		}
	}
	if identity.TenantID != request.TenantID || identity.UserID != request.UserID || identity.TaskID != request.TaskID {
		return nil, ErrAgentResourceIdentity
	}
	lease, err := r.Acquire(ctx, identity, request.ProviderID)
	if err != nil {
		return nil, err
	}
	return lease.Release, nil
}

func (r *AgentResourceRuntime) SnapshotForTenant(tenant string) resources.TenantSnapshot {
	if r == nil {
		return resources.TenantSnapshot{}
	}
	return r.work.SnapshotForTenant(tenant)
}

func (r *AgentResourceRuntime) ProviderSnapshotForTenant(tenant string) resources.TenantSnapshot {
	if r == nil {
		return resources.TenantSnapshot{}
	}
	return r.providers.SnapshotForTenant(tenant)
}

func (r *AgentResourceRuntime) ApplyPressure(pressure resources.Pressure) error {
	if r == nil {
		return resources.ErrInvalid
	}
	if err := r.work.ApplyPressure(pressure); err != nil {
		return err
	}
	return r.providers.ApplyPressure(pressure)
}
