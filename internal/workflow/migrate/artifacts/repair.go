package artifacts

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// repairDigestProfile is the profile [RepairRecord]'s content identity is
// hashed under.
const repairDigestProfile = "hcmnext.workflow.migrate.artifacts.Repair/v1"

// RepairRequest asks for one instance to be durably marked as needing repair
// because its artifact migration could not be completed or rolled back.
type RepairRequest struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID

	// Reason is a typed code, never prose: it is what an operator queue
	// groups by. The failed run's [CodeOf] is the obvious value.
	Reason string
	// Detail carries the failed run's own message, for the human reading one
	// row rather than the queue.
	Detail string

	RecordedBy string
	// RecordedAt is the caller's own instant. This package reads no clock.
	RecordedAt time.Time
}

func (r RepairRequest) validate() error {
	switch {
	case r.TenantID == uuid.Nil:
		return refuse(CodeInvalidRequest, "", "", "tenant id must not be the nil UUID")
	case r.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRequest, "", "", "instance id must not be the nil UUID")
	case r.Reason == "":
		return refuse(CodeInvalidRequest, "", r.InstanceID.String(), "a repair marker records why repair is required")
	case r.RecordedBy == "":
		return refuse(CodeInvalidRequest, "", r.InstanceID.String(), "a repair marker names who recorded it")
	case r.RecordedAt.IsZero():
		return refuse(CodeInvalidRequest, "", r.InstanceID.String(),
			"recorded_at must be supplied; this package never reads a wall clock")
	}
	return nil
}

// RepairRecord is the digested evidence one [MarkRepairRequired] call left
// behind: where the instance was, the exact legal path it was walked along,
// and where it ended.
type RepairRecord struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID

	From runtime.InstanceStatus
	// Path is every status recorded, in order, ending at REPAIR_REQUIRED.
	Path []runtime.InstanceStatus

	InstanceVersion int64

	Reason     string
	Detail     string
	RecordedBy string
	RecordedAt time.Time

	digest string
}

// Digest is the record's content identity.
func (r RepairRecord) Digest() string { return r.digest }

type repairIdentity struct {
	TenantID        string
	InstanceID      string
	From            runtime.InstanceStatus
	Path            []runtime.InstanceStatus
	InstanceVersion int64
	Reason          string
	RecordedBy      string
}

// MarkRepairRequired durably records that one instance's artifact migration
// failed in a way its caller could not undo.
//
// It is the second half of WF-RUN-026's failure clause. The first half is
// free: a [Migrate] that refuses has written nothing of its own, so a caller
// that can roll back its transaction leaves the instance on its old version
// with every promise it already had, and therefore runnable. This function is
// for the caller that cannot -- because the version move is already committed,
// or because the same failure would recur -- and it is deliberately a
// separate call in a separate transaction, since a marker written inside the
// transaction that is about to be rolled back would be rolled back with it.
//
// The instance is walked to REPAIR_REQUIRED along a path
// [runtime.LegalInstanceTransition] actually allows, one recorded transition
// per step, all inside tx. A PAUSED instance has no direct edge to
// REPAIR_REQUIRED in internal/workflow/runtime's state machine -- it reaches
// it through CANCELLING, which is the machine's own reading of "this is being
// torn down and something went wrong" -- so the intermediate statuses are
// recorded rather than skipped, and [RepairRecord.Path] reports exactly which
// ones a reader will find in the instance's history. An instance already at
// REPAIR_REQUIRED is re-recorded as itself, which the machine treats as
// idempotent, so the call is safe to repeat. A terminal instance with no path
// at all is [CodeIllegalRepairPath].
func MarkRepairRequired(ctx context.Context, tx Executor, req RepairRequest) (ret0 RepairRecord, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.mark_repair_required", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return RepairRecord{}, err
	}
	store := runtime.Store{}
	inst, err := store.LoadInstance(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return RepairRecord{}, wrap(CodeStorageFailed, "", req.InstanceID.String(), err,
			"read the instance to be marked for repair")
	}
	path, ok := repairPath(inst.RuntimeStatus)
	if !ok {
		return RepairRecord{}, refuse(CodeIllegalRepairPath, "", req.InstanceID.String(),
			"instance is %s, which has no legal path to %s",
			string(inst.RuntimeStatus), string(runtime.InstanceRepairRequired))
	}

	version := inst.InstanceVersion
	for _, status := range path {
		updated, err := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{
			TenantID: req.TenantID, InstanceID: req.InstanceID, ExpectedVersion: version,
			Status:               status,
			CurrentNodeIDs:       inst.CurrentNodeIDs,
			VariableRevisionHead: inst.VariableRevisionHead,
			EffectiveContextRef:  inst.EffectiveContextRef,
			LastCheckpointRef:    inst.LastCheckpointRef,
			CompletionDimensions: inst.CompletionDimensions,
		})
		if err != nil {
			return RepairRecord{}, wrap(CodeStorageFailed, "", req.InstanceID.String(), err,
				"record %s on the way to %s", string(status), string(runtime.InstanceRepairRequired))
		}
		version = updated.InstanceVersion
	}

	rec := RepairRecord{
		TenantID: req.TenantID, InstanceID: req.InstanceID,
		From: inst.RuntimeStatus, Path: path, InstanceVersion: version,
		Reason: req.Reason, Detail: req.Detail,
		RecordedBy: req.RecordedBy, RecordedAt: req.RecordedAt.UTC(),
	}
	rec.digest = canonicalDigest(repairDigestProfile, repairIdentity{
		TenantID: rec.TenantID.String(), InstanceID: rec.InstanceID.String(),
		From: rec.From, Path: append([]runtime.InstanceStatus(nil), rec.Path...),
		InstanceVersion: rec.InstanceVersion, Reason: rec.Reason, RecordedBy: rec.RecordedBy,
	})
	return rec, nil
}

// repairPath returns the statuses to record, in order, to move an instance
// from its current status to REPAIR_REQUIRED, or false when the state machine
// allows no such path.
//
// It is a two-step breadth-first walk rather than a hard-coded table on
// purpose: the path is whatever internal/workflow/runtime's own transition
// map says it is, so a later change to that map moves this function with it
// instead of leaving a stale route behind. No legal route is longer than two
// hops, and the search is bounded to that.
func repairPath(from runtime.InstanceStatus) ([]runtime.InstanceStatus, bool) {
	target := runtime.InstanceRepairRequired
	if from == target {
		return []runtime.InstanceStatus{target}, true
	}
	if runtime.LegalInstanceTransition(from, target) {
		return []runtime.InstanceStatus{target}, true
	}
	for _, mid := range runtime.InstanceStatuses() {
		if mid == from || mid == target {
			continue
		}
		if runtime.LegalInstanceTransition(from, mid) && runtime.LegalInstanceTransition(mid, target) {
			return []runtime.InstanceStatus{mid, target}, true
		}
	}
	return nil, false
}
