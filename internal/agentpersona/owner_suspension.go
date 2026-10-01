package agentpersona

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// OwnerReassignmentGrace is the time an owner has to be replaced after the
// identity authority says the owner is inactive or no longer has the owner
// role.
const OwnerReassignmentGrace = 14 * 24 * time.Hour

var (
	// ErrOwnerSuspensionInvalid identifies malformed coordinator input.
	ErrOwnerSuspensionInvalid = errors.New("agentpersona: invalid owner suspension request")
	// ErrOwnerSuspensionUnavailable identifies a missing fail-closed adapter.
	ErrOwnerSuspensionUnavailable = errors.New("agentpersona: owner suspension adapter unavailable")
)

// OwnerSuspensionScope identifies the smallest durable grant set to fence.
type OwnerSuspensionScope string

const (
	OwnerSuspensionVersion      OwnerSuspensionScope = "VERSION"
	OwnerSuspensionPersona      OwnerSuspensionScope = "PERSONA"
	OwnerSuspensionInstallation OwnerSuspensionScope = "INSTALLATION"
	OwnerSuspensionTenant       OwnerSuspensionScope = "TENANT"
)

// OwnerSuspensionRequest is the lifecycle mutation submitted after its lease
// fence has been committed.
type OwnerSuspensionRequest struct {
	Scope          OwnerSuspensionScope
	TenantID       string
	PersonaID      string
	PersonaVersion uint64
	InstallationID string
	Reason         string
	At             time.Time
}

// OwnerSuspender applies a lifecycle suspension and pauses all affected work.
// Implementations must make this operation idempotent.
type OwnerSuspender interface {
	Suspend(context.Context, OwnerSuspensionRequest) error
}

// OwnerLeaseFence advances the durable revocation epoch before suspension.
// The implementation must commit the epoch with the identity or lifecycle
// event so a worker cannot begin a step after the fence is visible.
type OwnerLeaseFence interface {
	Fence(context.Context, OwnerSuspensionRequest) (uint64, error)
}

// OwnerSuspensionCandidate is the durable owner projection consumed by the
// recovery sweep. Revision is an optimistic concurrency token owned by the
// adapter; the coordinator never invents or increments it.
type OwnerSuspensionCandidate struct {
	TenantID        string
	PersonaID       string
	PersonaVersion  uint64
	InstallationID  string
	OwnerID         string
	OwnerActive     bool
	OwnerHasRole    bool
	InvalidSince    time.Time
	ReassignmentDue time.Time
	Revision        uint64
}

// OwnerSuspensionStore records owner changes and lists projections when an
// identity event was lost. Save must use expectedRevision atomically.
type OwnerSuspensionStore interface {
	RecordOwnerChange(context.Context, OwnerSuspensionCandidate) error
	ClearOwnerChange(context.Context, string, string, string) error
	ListOwnerSuspensionCandidates(context.Context) ([]OwnerSuspensionCandidate, error)
	MarkSuspended(context.Context, OwnerSuspensionCandidate, uint64) error
}

// OwnerSuspensionNotifier informs the steward and tenant administrators after
// a durable suspension. Notification failure is returned to the caller so it
// can retry without pretending the operational handoff completed.
type OwnerSuspensionNotifier interface {
	NotifyOwnerSuspended(context.Context, OwnerSuspensionCandidate, string) error
}

// OwnerChange is the identity event delivered to the coordinator. A valid
// owner clears an existing reassignment window; an invalid owner starts one.
type OwnerChange struct {
	TenantID       string
	PersonaID      string
	PersonaVersion uint64
	InstallationID string
	OwnerID        string
	Active         bool
	HasOwnerRole   bool
	OccurredAt     time.Time
}

// OwnerSuspensionConfig wires the durable recovery and lifecycle boundaries.
// Store, Suspender, and Fence are required: omitting any one would make the
// coordinator able to acknowledge an unsafe partial state.
type OwnerSuspensionConfig struct {
	Clock     func() time.Time
	Store     OwnerSuspensionStore
	Suspender OwnerSuspender
	Fence     OwnerLeaseFence
	Notifier  OwnerSuspensionNotifier
}

// OwnerSuspensionCoordinator coordinates owner loss and expiry recovery.
// Its mutex deduplicates concurrent sweeps in one process; durable adapters
// remain responsible for cross-process optimistic fencing.
type OwnerSuspensionCoordinator struct {
	clock     func() time.Time
	store     OwnerSuspensionStore
	suspender OwnerSuspender
	fence     OwnerLeaseFence
	notifier  OwnerSuspensionNotifier
	mu        sync.Mutex
}

// Suspend fences a version, persona, installation, or tenant and then hands
// the mutation to the lifecycle adapter. It is the fail-closed path for an
// explicit owner termination or administrator kill.
func (c *OwnerSuspensionCoordinator) Suspend(ctx context.Context, req OwnerSuspensionRequest) error {
	if c == nil {
		return ErrOwnerSuspensionUnavailable
	}
	if err := validateOwnerSuspensionRequest(req); err != nil {
		return err
	}
	if req.At.IsZero() {
		req.At = c.clock().UTC()
	} else {
		req.At = req.At.UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.fence.Fence(ctx, req); err != nil {
		return err
	}
	return c.suspender.Suspend(ctx, req)
}

// NewOwnerSuspensionCoordinator validates the fail-closed wiring.
func NewOwnerSuspensionCoordinator(cfg OwnerSuspensionConfig) (*OwnerSuspensionCoordinator, error) {
	if cfg.Store == nil || cfg.Suspender == nil || cfg.Fence == nil {
		return nil, fmt.Errorf("%w: store, suspender and fence are required", ErrOwnerSuspensionUnavailable)
	}
	if cfg.Clock == nil {
		cfg.Clock = func() time.Time { return time.Now().UTC() }
	}
	return &OwnerSuspensionCoordinator{clock: cfg.Clock, store: cfg.Store, suspender: cfg.Suspender, fence: cfg.Fence, notifier: cfg.Notifier}, nil
}

// ApplyOwnerChange persists an owner reassignment window. It never suspends
// immediately: the owner has the full grace period to be replaced. A lost
// event is recovered by Sweep.
func (c *OwnerSuspensionCoordinator) ApplyOwnerChange(ctx context.Context, change OwnerChange) error {
	if c == nil {
		return ErrOwnerSuspensionUnavailable
	}
	if err := validateOwnerChange(change); err != nil {
		return err
	}
	at := change.OccurredAt.UTC()
	if at.IsZero() {
		at = c.clock().UTC()
	}
	if change.Active && change.HasOwnerRole {
		return c.store.ClearOwnerChange(ctx, change.TenantID, change.PersonaID, change.OwnerID)
	}
	candidate := OwnerSuspensionCandidate{TenantID: change.TenantID, PersonaID: change.PersonaID, PersonaVersion: change.PersonaVersion, InstallationID: change.InstallationID, OwnerID: change.OwnerID, OwnerActive: change.Active, OwnerHasRole: change.HasOwnerRole, InvalidSince: at, ReassignmentDue: at.Add(OwnerReassignmentGrace)}
	return c.store.RecordOwnerChange(ctx, candidate)
}

// Sweep suspends every expired orphan whose owner has not been reassigned.
// A failed fence or suspension aborts before the candidate is marked, leaving
// it available to a later retry. Other candidates are still attempted and the
// first error is returned.
func (c *OwnerSuspensionCoordinator) Sweep(ctx context.Context, now time.Time) error {
	if c == nil {
		return ErrOwnerSuspensionUnavailable
	}
	if now.IsZero() {
		now = c.clock()
	}
	now = now.UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	candidates, err := c.store.ListOwnerSuspensionCandidates(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, candidate := range candidates {
		if candidate.OwnerActive && candidate.OwnerHasRole || !expiredOwner(candidate, now) {
			continue
		}
		req := requestForCandidate(candidate, now)
		if _, err := c.fence.Fence(ctx, req); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := c.suspender.Suspend(ctx, req); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := c.store.MarkSuspended(ctx, candidate, candidate.Revision); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if c.notifier != nil {
			if err := c.notifier.NotifyOwnerSuspended(ctx, candidate, "OWNER_REASSIGNMENT_EXPIRED"); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func requestForCandidate(candidate OwnerSuspensionCandidate, at time.Time) OwnerSuspensionRequest {
	scope := OwnerSuspensionPersona
	if candidate.InstallationID != "" {
		scope = OwnerSuspensionInstallation
	}
	return OwnerSuspensionRequest{Scope: scope, TenantID: candidate.TenantID, PersonaID: candidate.PersonaID, PersonaVersion: candidate.PersonaVersion, InstallationID: candidate.InstallationID, Reason: "OWNER_REASSIGNMENT_EXPIRED", At: at}
}

func expiredOwner(candidate OwnerSuspensionCandidate, now time.Time) bool {
	due := candidate.ReassignmentDue
	if due.IsZero() {
		if candidate.InvalidSince.IsZero() {
			return false
		}
		due = candidate.InvalidSince.Add(OwnerReassignmentGrace)
	}
	return !now.Before(due)
}

func validateOwnerChange(change OwnerChange) error {
	if strings.TrimSpace(change.TenantID) == "" || strings.TrimSpace(change.PersonaID) == "" || strings.TrimSpace(change.OwnerID) == "" || change.PersonaVersion == 0 {
		return fmt.Errorf("%w: tenant, persona, version and owner are required", ErrOwnerSuspensionInvalid)
	}
	if change.InstallationID != "" && strings.TrimSpace(change.InstallationID) == "" {
		return fmt.Errorf("%w: installation id is invalid", ErrOwnerSuspensionInvalid)
	}
	return nil
}

func validateOwnerSuspensionRequest(req OwnerSuspensionRequest) error {
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.Reason) == "" {
		return fmt.Errorf("%w: tenant and reason are required", ErrOwnerSuspensionInvalid)
	}
	switch req.Scope {
	case OwnerSuspensionTenant:
	case OwnerSuspensionPersona:
		if strings.TrimSpace(req.PersonaID) == "" {
			return fmt.Errorf("%w: persona is required", ErrOwnerSuspensionInvalid)
		}
	case OwnerSuspensionVersion:
		if strings.TrimSpace(req.PersonaID) == "" || req.PersonaVersion == 0 {
			return fmt.Errorf("%w: persona and version are required", ErrOwnerSuspensionInvalid)
		}
	case OwnerSuspensionInstallation:
		if strings.TrimSpace(req.InstallationID) == "" {
			return fmt.Errorf("%w: installation is required", ErrOwnerSuspensionInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown scope %q", ErrOwnerSuspensionInvalid, req.Scope)
	}
	return nil
}
