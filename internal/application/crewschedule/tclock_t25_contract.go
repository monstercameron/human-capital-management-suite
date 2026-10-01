// Package crewschedule is the application boundary for revisioned crew
// shifts. It keeps authorization and optimistic-concurrency checks at the
// edge while delegating schedule rules and attendance comparison to the pure
// crewshift domain.
package crewschedule

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/crewshift"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const contractVersion = 1

// Version reports the application contract version.
func Version() int { return contractVersion }

var (
	ErrInvalidPrincipal = errors.New("crewschedule: trusted principal required")
	ErrInvalidRequest   = errors.New("crewschedule: invalid request")
	ErrUnavailable      = errors.New("crewschedule: required port unavailable")
	ErrForbidden        = errors.New("crewschedule: forbidden")
	ErrNotFound         = errors.New("crewschedule: not found")
	ErrRevisionConflict = errors.New("crewschedule: revision conflict")
)

// Capability is the closed application authorization vocabulary for schedule
// mutations and reconciliation reads.
type Capability string

const (
	CapabilityPublish   Capability = "SHIFT_PUBLISH"
	CapabilityCancel    Capability = "SHIFT_CANCEL"
	CapabilityReassign  Capability = "SHIFT_REASSIGN"
	CapabilityReconcile Capability = "SHIFT_RECONCILE"
)

// Authorizer resolves current grants. Implementations must perform the check
// against trusted tenant and worker scope and return before a store mutation.
type Authorizer interface {
	Authorize(context.Context, *trust.Principal, string, string, Capability) error
}

// ShiftStore is the transactional persistence seam. Execute must check the
// idempotency digest before the expected revision and commit the returned
// lifecycle result atomically with its retained history.
type ShiftStore interface {
	Create(context.Context, string, crewshift.Shift, string) (crewshift.Shift, error)
	Get(context.Context, string, string) (crewshift.Shift, error)
	History(context.Context, string, string) (crewshift.History, error)
	Execute(context.Context, string, string, string, string, string, int64, func(crewshift.Shift, crewshift.History) (crewshift.LifecycleResult, error)) (crewshift.LifecycleResult, error)
}

// PublishedScheduleReader supplies the current worker schedule used for
// cross-project overlap and rest checks. A caller-provided snapshot remains a
// useful test fallback, but production wiring should provide this port.
type PublishedScheduleReader interface {
	PublishedForWorker(context.Context, string, string) ([]crewshift.Shift, error)
}

// Notifier describes the worker notification effect. The application emits
// computed notifications only after the store reports a committed result.
type Notifier interface {
	NotifyShift(context.Context, string, crewshift.Notification) error
}
