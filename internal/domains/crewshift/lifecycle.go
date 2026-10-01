package crewshift

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// LifecycleAction is the closed set of state-changing actions a published
// shift can undergo after it is first published.
type LifecycleAction string

const (
	ActionPublish  LifecycleAction = "PUBLISH"
	ActionCancel   LifecycleAction = "CANCEL"
	ActionReassign LifecycleAction = "REASSIGN"
)

// Valid reports whether a is a declared lifecycle action.
func (a LifecycleAction) Valid() bool {
	switch a {
	case ActionPublish, ActionCancel, ActionReassign:
		return true
	}
	return false
}

// Grant is the caller's current authorization snapshot for one actor and
// scope, evaluated fresh for every lifecycle attempt. A grant fetched before
// a revocation and reused afterward is a stale grant, not a valid one; the
// caller is responsible for supplying the live value.
type Grant struct {
	ActorRef values.EntityRef
	Scope    LifecycleAction
	Revoked  bool
}

// allows reports whether the grant is live and covers the requested action.
func (g Grant) allows(action LifecycleAction) bool {
	return !g.Revoked && g.ActorRef.Validate() == nil && g.Scope == action
}

// LifecycleRejection is the stable FTIME-007 failure shape.
type LifecycleRejection struct {
	Field  string
	State  string
	Reason string
}

func (r *LifecycleRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s: %s", ErrLifecycleRejected, r.Field, r.State, r.Reason)
}

// Unwrap exposes the CREWSHIFT_LIFECYCLE_REJECTED sentinel to errors.Is.
func (r *LifecycleRejection) Unwrap() error { return ErrLifecycleRejected }

func lifecycleReject(field, state, reason string) error {
	return &LifecycleRejection{Field: field, State: state, Reason: reason}
}

// Notification is one computed effect for one worker affected by a
// lifecycle transition. It describes the effect; it does not send it.
type Notification struct {
	WorkerRef values.EntityRef
	ShiftID   string
	Revision  int64
	Action    LifecycleAction
	Reason    string
}

// History is every prior revision of one shift, oldest first. ApplyLifecycle
// never drops a version: a caller that needs "what did revision 3 say"
// always has it.
type History struct {
	Versions []Shift
}

// Append records s as the next entry. It is a value-receiver copy-out helper:
// callers thread the returned History forward rather than mutating a shared
// value, which keeps the package free of package-level mutable state.
func (h History) Append(s Shift) History {
	return History{Versions: append(append([]Shift(nil), h.Versions...), s)}
}

// At returns the version recorded at the given revision, if any.
func (h History) At(revision int64) (Shift, bool) {
	for _, v := range h.Versions {
		if v.Revision == revision {
			return v, true
		}
	}
	return Shift{}, false
}

// LifecycleRequest is one publish, cancel or reassign attempt against the
// stored (server-known) view of a shift.
type LifecycleRequest struct {
	Action           LifecycleAction
	Current          Shift
	ExpectedRevision int64
	Grant            Grant
	Now              time.Time
	// NewWorkerRef and NewWorkerEligibility apply to ActionReassign only.
	NewWorkerRef         values.EntityRef
	NewWorkerEligibility EligibilityFacts
	Reason               string
}

// LifecycleResult is the outcome of one successful ApplyLifecycle call.
type LifecycleResult struct {
	Updated       Shift
	History       History
	Notifications []Notification
}

// ApplyLifecycle advances a published shift through cancel or reassign, or
// re-runs publish's approval bookkeeping for a re-publish of an already
// published shift (for example after a break-plan correction that does not
// change who or when). Every call requires the caller's ExpectedRevision to
// match Current.Revision and a live grant for the exact action: a stale
// expected revision or a revoked grant is rejected before anything else is
// evaluated, so a worker whose grant was revoked cannot keep a shift by
// replaying an earlier, still-valid-looking publish. The prior version is
// always appended to History before the new one, so no version is lost.
func ApplyLifecycle(req LifecycleRequest, history History) (LifecycleResult, error) {
	if !req.Action.Valid() {
		return LifecycleResult{}, lifecycleReject("action", "UNDECLARED", fmt.Sprintf("action %q is not declared", req.Action))
	}
	if err := req.Current.Validate(); err != nil {
		return LifecycleResult{}, err
	}
	if req.Current.Status != StatusPublished {
		return LifecycleResult{}, lifecycleReject("current.status", "NOT_PUBLISHED", "only a published shift has a lifecycle to advance")
	}
	if req.ExpectedRevision != req.Current.Revision {
		return LifecycleResult{}, lifecycleReject("expected_revision", "STALE",
			fmt.Sprintf("expected revision %d does not match current revision %d", req.ExpectedRevision, req.Current.Revision))
	}
	if !req.Grant.allows(req.Action) {
		state := "DENIED"
		if req.Grant.Revoked {
			state = "REVOKED"
		}
		return LifecycleResult{}, lifecycleReject("grant", state, fmt.Sprintf("no live grant for %s", req.Action))
	}
	if req.Now.IsZero() {
		return LifecycleResult{}, lifecycleReject("now", "MISSING", "the lifecycle instant is required")
	}
	if req.Reason == "" {
		return LifecycleResult{}, lifecycleReject("reason", "MISSING", "a lifecycle change records its reason")
	}

	next := req.Current
	next.Revision = req.Current.Revision + 1
	next.ApprovedBy = req.Grant.ActorRef
	next.ApprovedAt = req.Now

	var notifications []Notification
	switch req.Action {
	case ActionCancel:
		next.Status = StatusCancelled
		notifications = append(notifications, Notification{
			WorkerRef: req.Current.WorkerRef, ShiftID: req.Current.ID, Revision: next.Revision,
			Action: ActionCancel, Reason: req.Reason,
		})
	case ActionReassign:
		if err := req.NewWorkerRef.Validate(); err != nil || req.NewWorkerRef.Tenant != req.Current.Tenant {
			return LifecycleResult{}, lifecycleReject("new_worker", "INVALID", "reassignment requires a valid same-tenant worker reference")
		}
		if req.NewWorkerEligibility.Revoked || !req.NewWorkerEligibility.Active {
			return LifecycleResult{}, lifecycleReject("new_worker.eligibility", "INELIGIBLE", "the reassigned worker is not currently eligible")
		}
		previousWorker := req.Current.WorkerRef
		next.WorkerRef = req.NewWorkerRef
		notifications = append(notifications,
			Notification{WorkerRef: previousWorker, ShiftID: req.Current.ID, Revision: next.Revision, Action: ActionReassign, Reason: req.Reason},
			Notification{WorkerRef: req.NewWorkerRef, ShiftID: req.Current.ID, Revision: next.Revision, Action: ActionReassign, Reason: req.Reason},
		)
	case ActionPublish:
		// Re-approving an already published shift: status is unchanged, but
		// the revision, approver and instant advance so the change has its
		// own recorded authority.
		notifications = append(notifications, Notification{
			WorkerRef: req.Current.WorkerRef, ShiftID: req.Current.ID, Revision: next.Revision,
			Action: ActionPublish, Reason: req.Reason,
		})
	}

	if err := next.Validate(); err != nil {
		return LifecycleResult{}, err
	}
	return LifecycleResult{
		Updated:       next,
		History:       history.Append(req.Current),
		Notifications: notifications,
	}, nil
}
