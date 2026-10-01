package timecardservice

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/agencytime"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/attendance"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/contractortime"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/crewshift"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timecard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeexport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Capability is the closed set of actions Authorizer evaluates. It mirrors
// the shape of internal/domains/workorderaccess.Capability without importing
// a work-order-specific vocabulary into this package.
type Capability string

const (
	CapReviewTimecard    Capability = "TIMECARD_REVIEW"
	CapApproveTimecard   Capability = "TIMECARD_APPROVE"
	CapCorrectTimecard   Capability = "TIMECARD_CORRECT"
	CapAllocateTime      Capability = "TIME_ALLOCATE"
	CapPublishShift      Capability = "SHIFT_PUBLISH"
	CapCancelShift       Capability = "SHIFT_CANCEL"
	CapReassignShift     Capability = "SHIFT_REASSIGN"
	CapReconcileShift    Capability = "SHIFT_RECONCILE"
	CapDecideMissedPunch Capability = "MISSED_PUNCH_DECIDE"
	CapAssignProfile     Capability = "TIME_PROFILE_ASSIGN"
	CapRouteTime         Capability = "TIME_ROUTE"
)

// Authorizer evaluates whether the actor may act on one worker's scoped
// time-keeping records. It must fail closed and must be called before every
// side effect, never after.
type Authorizer interface {
	// Authorize reports whether actor may exercise cap over workerRef in
	// tenant. Implementations enforce segregation of duties: an actor who is
	// the same person as workerRef never holds an approval-class capability
	// over their own record.
	Authorize(ctx context.Context, actor *trust.Principal, tenant, workerRef string, cap Capability) error
}

// WorkerDirectory resolves worker identity and the supervisor scope an
// approving actor holds, so a manager outside their scope is refused before
// any side effect.
type WorkerDirectory interface {
	// ResolveWorker reports whether workerRef exists in tenant.
	ResolveWorker(ctx context.Context, tenant, workerRef string) (bool, error)
	// InScope reports whether supervisorRef currently supervises workerRef in
	// tenant. A stale or cached answer is a defect in the implementation, not
	// a case this port compensates for.
	InScope(ctx context.Context, tenant, supervisorRef, workerRef string) (bool, error)
}

// Notifier emits a computed effect for an affected worker. Implementations
// must not block the command that produced the notification; this port is
// fire-and-forget from the service's point of view.
type Notifier interface {
	NotifyShift(ctx context.Context, tenant string, n crewshift.Notification) error
	NotifyMissedPunch(ctx context.Context, tenant, workerRef, requestID, outcome string) error
}

// IDs generates the identifiers this service assigns to new aggregates. It
// is deterministic per (tenant, actor, operation, idempotency key) so a
// retried create is never assigned two different identities.
type IDs interface {
	NewID(tenant, actor, operation, idempotencyKey string) string
}

// TimecardStore is the WF-CAP-007 / FTIME-004 / FTIME-005 port. Execute loads
// the current aggregate, checks the idempotency key against digest before
// checking the expected revision (a replay of the same key and digest
// returns the original result without calling mutate again), applies mutate
// inside one store transaction, and persists the result. internal/data/
// timestore's timecards.go satisfies this through a thin adapter that maps
// timecard.Timecard to and from its own Timecard/TimecardEvent rows.
type TimecardStore interface {
	Create(ctx context.Context, tenant string, tc timecard.Timecard, idempotencyKey string) (timecard.Timecard, error)
	Get(ctx context.Context, tenant, id string) (timecard.Timecard, error)
	Execute(ctx context.Context, tenant, id, actor, idempotencyKey, digest string, expectedRevision uint64,
		mutate func(timecard.Timecard) (timecard.Timecard, error)) (timecard.Timecard, error)
}

// ShiftStore is the FTIME-006 / FTIME-007 port, shaped so internal/data/
// timestore's shifts.go (CreateShift/PublishShift/CancelShift/ReassignShift)
// can satisfy it through a thin adapter that maps crewshift.Shift to and
// from its own Shift row and the crewshift.History it retains.
type ShiftStore interface {
	Create(ctx context.Context, tenant string, sh crewshift.Shift, idempotencyKey string) (crewshift.Shift, error)
	Get(ctx context.Context, tenant, id string) (crewshift.Shift, error)
	History(ctx context.Context, tenant, id string) (crewshift.History, error)
	Execute(ctx context.Context, tenant, id, actor, idempotencyKey, digest string, expectedRevision int64,
		mutate func(crewshift.Shift, crewshift.History) (crewshift.LifecycleResult, error)) (crewshift.LifecycleResult, error)
}

// ProfileStore is the WTIME-001/002 port: resolve an assignment's time
// profile by eligibility rule and pin the resolved version for the lifetime
// of one session or period run. internal/data/timestore's profiles.go
// (PublishProfileVersion/ProfileVersionAt/PinAssignmentProfile) satisfies
// this through a thin adapter.
type ProfileStore interface {
	Rules(ctx context.Context, tenant string) ([]timeprofile.EligibilityRule, error)
	PinFor(ctx context.Context, tenant, assignmentRef string) (ProfilePin, bool, error)
	Pin(ctx context.Context, tenant, assignmentRef string, profile timeprofile.TimeProfile, expectedRevision int64, resolvedAt time.Time) (ProfilePin, error)
}

// ProfilePin is the assignment's current resolved-and-pinned profile.
type ProfilePin struct {
	AssignmentRef string
	Revision      int64
	Profile       timeprofile.TimeProfile
	ResolvedAt    time.Time
}

// MissedPunchStore is the TCLOCK-011 port, shaped so internal/data/
// timestore's missedpunch.go (SubmitMissedPunchRequest/DecideMissedPunch)
// can satisfy it through a thin adapter.
type MissedPunchStore interface {
	Create(ctx context.Context, tenant string, r timesession.MissedPunchRequest, sessionID, idempotencyKey string) (timesession.MissedPunchRequest, string, error)
	Get(ctx context.Context, tenant, requestID string) (timesession.MissedPunchRequest, error)
	Decide(ctx context.Context, tenant, requestID string, decided timesession.MissedPunchRequest, decidedBy, reopenRef string) (timesession.MissedPunchRequest, error)
}

// AllocationStore is the FTIME-005 port: it persists one timecard's current
// allocation and every correction against it, keyed so no source ever
// appears charged in two stored allocations at once.
type AllocationStore interface {
	Get(ctx context.Context, tenant, timecardID string) (timecard.TimeAllocation, bool, error)
	Save(ctx context.Context, tenant string, alloc timecard.TimeAllocation, idempotencyKey string) (timecard.TimeAllocation, error)
}

// LedgerStore is the shared append-only evidence trail for allocation
// corrections, premium-calculation traces (WTIME-013/014) and destination
// acceptance/rejection receipts (WTIME-009-012, TCLOCK-015). It never allows
// an existing entry to be rewritten.
type LedgerStore interface {
	RecordAllocationCorrection(ctx context.Context, tenant string, c timecard.AllocationCorrection, idempotencyKey string) error
	RecordPremiumTrace(ctx context.Context, tenant, timecardID string, trace PremiumTrace, idempotencyKey string) error
	RecordReceipt(ctx context.Context, tenant string, r DestinationReceipt, idempotencyKey string) error
}

// DestinationStore resolves which Destination binds to which downstream
// system for a tenant, and which Dispatcher key in Service.Destinations
// serves it. It is separate from the Destinations map itself so a tenant's
// binding (which connector instance) can change without redeploying code.
type DestinationStore interface {
	DispatcherKeyFor(ctx context.Context, tenant string, dest timeprofile.Destination) (string, error)
}

// DestinationPayload is the generic shape routed to a Dispatcher. Exactly
// one of the typed payload fields is set, matching the destination.
type DestinationPayload struct {
	Destination     timeprofile.Destination
	PayrollTimeCard *timeexport.TimeCard
	ContractorDraft *contractortime.InvoiceDraft
	AgencyExport    *agencytime.ExportPayload
}

// DestinationReceipt is the observed outcome of one dispatch attempt.
type DestinationReceipt struct {
	TimecardID  string
	Destination timeprofile.Destination
	Accepted    bool
	ReceiptRef  string
	Reason      string
	At          time.Time
}

// Dispatcher delivers one routed payload to its destination system (payroll
// export, contractor AP, or agency/VMS) and observes its acceptance or
// rejection. Implementations live outside this package as connector
// bindings; the service never knows a destination's wire format.
type Dispatcher interface {
	Dispatch(ctx context.Context, tenant string, payload DestinationPayload) (DestinationReceipt, error)
}

// PremiumCalculator wraps the WTIME-013/014 classification engine
// (internal/domains/timecalc) behind a narrow port. Wave 2 notes timecalc
// "may still be finishing"; if its exported API shape moves, only this
// interface and its one adapter need to change.
type PremiumCalculator interface {
	Calculate(ctx context.Context, profile timeprofile.TimeProfile, req PremiumRequest) (PremiumTrace, error)
}

// PremiumRequest is the minimal input the calculator needs: the timecard's
// worked intervals and the calendar/rule parameters that select a RuleSet.
// It is defined locally, the same way contractortime.ApprovedLine and
// agencytime.ApprovedLine are, so this package does not depend on timecalc's
// still-moving request shape beyond this one narrow translation point.
type PremiumRequest struct {
	TimecardID string
	WorkerRef  string
	Intervals  []PremiumInterval
	RuleSetRef attendance.VersionedRef
}

// PremiumInterval is one worked span priced by the calculator.
type PremiumInterval struct {
	Start, End time.Time
	Zone       string
	Tag        string // differential/on-call tag, "" for ordinary work
}

// PremiumTrace is the ARCH-GO-009 evidence of one premium calculation: the
// classified totals and the rule trace, kept verbatim in the ledger.
type PremiumTrace struct {
	TimecardID        string
	RegularMinutes    int64
	OvertimeMinutes   int64
	DoubleTimeMinutes int64
	PremiumMinutes    int64
	Trace             []string
	Digest            string
}

// clockPunch is the minimal punch evidence FTIME-004 review reads. It is
// re-exported so callers outside this package's tests never import
// internal/domains/clock just to build a review request.
type ClockPunch = clock.TimeObservation
