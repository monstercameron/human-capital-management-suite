package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

var (
	// ErrModelLeaseNotConfigured means the source has no usable lease authority.
	ErrModelLeaseNotConfigured = errors.New("application: model credential lease source is not configured")
	// ErrModelLeaseProviderMissing means no credential authority is registered
	// for the requested provider. The wrapped error names only that provider.
	ErrModelLeaseProviderMissing = errors.New("application: model credential authority is missing")
	// ErrModelLeaseTaskBinding means the request is not bound to a trusted task.
	ErrModelLeaseTaskBinding = errors.New("application: model credential lease task binding is invalid")
	// ErrModelLeaseScope means the authority returned a handle outside the
	// requested tenant or region, or a use request has a mismatched scope.
	ErrModelLeaseScope = errors.New("application: model credential lease scope is invalid")
)

// TrustedModelTask is the server-derived identity to which a model lease is
// bound. It has no user credential or provider secret.
type TrustedModelTask struct {
	TenantID string
	TaskID   string
	AgentID  string
	Workload string
	trusted  bool
}

// NewTrustedModelTask constructs a task binding after the caller has resolved
// the current tenant, agent and workload authority.
func NewTrustedModelTask(tenantID, taskID, agentID, workload string) (TrustedModelTask, error) {
	for name, value := range map[string]string{"tenant": tenantID, "task": taskID, "agent": agentID, "workload": workload} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return TrustedModelTask{}, fmt.Errorf("%w: %s is required", ErrModelLeaseTaskBinding, name)
		}
	}
	return TrustedModelTask{TenantID: tenantID, TaskID: taskID, AgentID: agentID, Workload: workload, trusted: true}, nil
}

// ModelCredentialRequest asks a provider authority for an opaque custody
// handle. Implementations must never return credential bytes.
type ModelCredentialRequest struct {
	ProviderID  string
	TenantID    string
	TaskID      string
	AgentID     string
	Destination string
	Purpose     string
	Region      string
}

// ModelCredentialAuthority resolves a provider's current credential version
// for one trusted tenant/task scope. It is the only application dependency
// allowed to know how provider credentials are stored.
type ModelCredentialAuthority interface {
	ResolveModelCredential(context.Context, ModelCredentialRequest) (custody.Handle, error)
}

// ModelLeaseSourceConfig wires the provider authorities and single-use lease
// manager. There are no environment or raw-secret fallbacks.
type ModelLeaseSourceConfig struct {
	Authorities map[string]ModelCredentialAuthority
	Leases      *lease.Manager
	MaxTTL      time.Duration
}

// ModelLeaseRequest describes one model dispatch credential lease.
type ModelLeaseRequest struct {
	Task        TrustedModelTask
	ProviderID  string
	Destination string
	Purpose     string
	Region      string
	TTL         time.Duration
}

// ModelLeaseUseRequest binds consumption to the same task and provider scope
// used at issuance. The returned evidence contains no credential material.
type ModelLeaseUseRequest struct {
	Task        TrustedModelTask
	ProviderID  string
	Destination string
	Lease       lease.CredentialLease
}

// ModelLeaseSource resolves provider credentials into opaque, destination
// scoped, single-use leases. It retains only non-secret provider metadata.
type ModelLeaseSource struct {
	authorities map[string]ModelCredentialAuthority
	leases      *lease.Manager
	maxTTL      time.Duration

	mu     sync.Mutex
	issued map[string]issuedModelLease
}

type issuedModelLease struct {
	provider string
	task     TrustedModelTask
	region   string
}

// NewModelLeaseSource validates the production composition and copies the
// provider map so callers cannot change authority after construction.
func NewModelLeaseSource(cfg ModelLeaseSourceConfig) (*ModelLeaseSource, error) {
	if cfg.Leases == nil || len(cfg.Authorities) == 0 {
		return nil, ErrModelLeaseNotConfigured
	}
	maxTTL := cfg.MaxTTL
	if maxTTL <= 0 {
		maxTTL = 5 * time.Minute
	}
	authorities := make(map[string]ModelCredentialAuthority, len(cfg.Authorities))
	for provider, authority := range cfg.Authorities {
		if strings.TrimSpace(provider) == "" || authority == nil {
			return nil, fmt.Errorf("%w: provider %q", ErrModelLeaseNotConfigured, provider)
		}
		authorities[provider] = authority
	}
	return &ModelLeaseSource{authorities: authorities, leases: cfg.Leases, maxTTL: maxTTL, issued: make(map[string]issuedModelLease)}, nil
}

// Issue resolves and mints one decrypt lease for a trusted model task.
func (s *ModelLeaseSource) Issue(ctx context.Context, req ModelLeaseRequest) (lease.CredentialLease, error) {
	if s == nil || s.leases == nil {
		return lease.CredentialLease{}, ErrModelLeaseNotConfigured
	}
	if ctx == nil {
		return lease.CredentialLease{}, fmt.Errorf("%w: context is required", ErrModelLeaseTaskBinding)
	}
	if err := ctx.Err(); err != nil {
		return lease.CredentialLease{}, err
	}
	if err := validateModelLeaseRequest(req, s.maxTTL); err != nil {
		return lease.CredentialLease{}, err
	}
	authority, ok := s.authorities[req.ProviderID]
	if !ok {
		return lease.CredentialLease{}, fmt.Errorf("%w: provider %q", ErrModelLeaseProviderMissing, req.ProviderID)
	}
	handle, err := authority.ResolveModelCredential(ctx, ModelCredentialRequest{
		ProviderID: req.ProviderID, TenantID: req.Task.TenantID, TaskID: req.Task.TaskID,
		AgentID: req.Task.AgentID, Destination: req.Destination, Purpose: req.Purpose, Region: req.Region,
	})
	if err != nil {
		return lease.CredentialLease{}, fmt.Errorf("%w: provider %q", ErrModelLeaseProviderMissing, req.ProviderID)
	}
	if handle.Tenant != req.Task.TenantID || handle.Region != req.Region || handle.Validate() != nil {
		return lease.CredentialLease{}, fmt.Errorf("%w: provider %q returned an out-of-scope handle", ErrModelLeaseScope, req.ProviderID)
	}
	value, _, err := s.leases.Mint(lease.Request{Handle: handle, Workload: req.Task.Workload, Tenant: req.Task.TenantID, Purpose: req.Purpose, Destination: req.Destination, Operation: custody.Decrypt, TTL: req.TTL})
	if err != nil {
		return lease.CredentialLease{}, err
	}
	s.mu.Lock()
	s.issued[value.ID] = issuedModelLease{provider: req.ProviderID, task: req.Task, region: req.Region}
	s.mu.Unlock()
	return value, nil
}

// Use verifies and consumes a lease exactly once through the trust lease
// manager, after checking its provider, task, tenant and region binding.
func (s *ModelLeaseSource) Use(req ModelLeaseUseRequest) (lease.Evidence, error) {
	if s == nil || s.leases == nil {
		return lease.Evidence{}, ErrModelLeaseNotConfigured
	}
	if err := validateTrustedTask(req.Task); err != nil {
		return lease.Evidence{}, err
	}
	if strings.TrimSpace(req.ProviderID) == "" || strings.TrimSpace(req.Destination) == "" {
		return lease.Evidence{}, ErrModelLeaseScope
	}
	s.mu.Lock()
	issued, ok := s.issued[req.Lease.ID]
	s.mu.Unlock()
	if !ok || issued.provider != req.ProviderID || issued.task != req.Task || req.Lease.Destination != req.Destination || req.Lease.Tenant != req.Task.TenantID || req.Lease.Handle.Region != issued.region {
		return lease.Evidence{}, ErrModelLeaseScope
	}
	return s.leases.Use(req.Lease, req.Destination, custody.Decrypt)
}

func validateModelLeaseRequest(req ModelLeaseRequest, maxTTL time.Duration) error {
	if err := validateTrustedTask(req.Task); err != nil {
		return err
	}
	for name, value := range map[string]string{"provider": req.ProviderID, "destination": req.Destination, "purpose": req.Purpose, "region": req.Region} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required", ErrModelLeaseScope, name)
		}
	}
	if req.TTL <= 0 || req.TTL > maxTTL {
		return fmt.Errorf("%w: lease TTL exceeds source policy", ErrModelLeaseScope)
	}
	return nil
}

func validateTrustedTask(task TrustedModelTask) error {
	if !task.trusted || strings.TrimSpace(task.TenantID) == "" || strings.TrimSpace(task.TaskID) == "" || strings.TrimSpace(task.AgentID) == "" || strings.TrimSpace(task.Workload) == "" {
		return ErrModelLeaseTaskBinding
	}
	return nil
}
