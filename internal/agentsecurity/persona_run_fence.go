package agentsecurity

import (
	"strings"
	"sync"
)

// PersonaRunID identifies a server-owned persona execution run.
type PersonaRunID string

// KillSwitchLeaseID identifies a security lease and is distinct from model
// provider credential lease identifiers.
type KillSwitchLeaseID string

// PersonaRunFence binds a persona run to exactly one kill-switch lease.
// Bindings are immutable so retries cannot redirect a run after authorization.
type PersonaRunFence struct {
	switchBoard *KillSwitch
	mu          sync.RWMutex
	runLeases   map[PersonaRunID]KillSwitchLeaseID
	leaseRuns   map[KillSwitchLeaseID]PersonaRunID
}

// NewPersonaRunFence creates a persona-run binding registry for one switch.
func NewPersonaRunFence(switchBoard *KillSwitch) *PersonaRunFence {
	return &PersonaRunFence{
		switchBoard: switchBoard,
		runLeases:   make(map[PersonaRunID]KillSwitchLeaseID),
		leaseRuns:   make(map[KillSwitchLeaseID]PersonaRunID),
	}
}

// Bind associates one server-owned persona run with one active security
// lease. Repeating the exact active pair is idempotent for safe retries; a
// run or lease cannot be rebound to a different counterpart.
func (f *PersonaRunFence) Bind(runID PersonaRunID, leaseID KillSwitchLeaseID) error {
	if f == nil || f.switchBoard == nil {
		return refusal(RefusalInvalid, "persona_run_fence", "kill switch is required")
	}
	if strings.TrimSpace(string(runID)) == "" || strings.TrimSpace(string(leaseID)) == "" {
		return refusal(RefusalInvalid, "persona_run_fence", "run and kill-switch lease identifiers are required")
	}
	// Hold the switch lock through insertion: Disable cannot commit between
	// active-lease validation and publication of this binding.
	f.switchBoard.mu.Lock()
	defer f.switchBoard.mu.Unlock()
	lease, ok := f.switchBoard.leases[string(leaseID)]
	if !ok {
		return refusal(RefusalCapability, "lease", "unknown kill-switch lease")
	}
	if lease.revoked {
		return refusal(RefusalEffectClass, "lease", "revoked kill-switch lease cannot bind a run")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, exists := f.runLeases[runID]; exists {
		if existing == leaseID && f.leaseRuns[leaseID] == runID {
			return nil
		}
		return refusal(RefusalInvalid, "persona_run_fence", "persona run is already bound to another lease")
	}
	if _, exists := f.leaseRuns[leaseID]; exists {
		return refusal(RefusalInvalid, "persona_run_fence", "kill-switch lease is already bound to another run")
	}
	f.runLeases[runID] = leaseID
	f.leaseRuns[leaseID] = runID
	return nil
}

// RunStep executes one bounded step under the run's bound kill-switch lease.
// Unknown runs and revoked leases fail closed.
func (f *PersonaRunFence) RunStep(runID PersonaRunID, step func() error) (Fallback, error) {
	if f == nil || f.switchBoard == nil {
		return Fallback{}, refusal(RefusalInvalid, "persona_run_fence", "kill switch is required")
	}
	f.mu.RLock()
	leaseID, ok := f.runLeases[runID]
	f.mu.RUnlock()
	if !ok {
		return Fallback{}, refusal(RefusalCapability, "persona_run_fence", "persona run has no kill-switch binding")
	}
	return f.switchBoard.RunStep(string(leaseID), step)
}
