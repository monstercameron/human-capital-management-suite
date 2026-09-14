package artifacts

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// migrationReason is the settle reason every predecessor artifact this
// package retires is stamped with. It is a code, never prose, so an operator
// reading a cancelled timer or a closed subscription can tell a migration
// apart from a cancellation.
const migrationReason = "VERSION_MIGRATION"

// --- lease ------------------------------------------------------------------

type leaseHandler struct{}

func (leaseHandler) Kind() Kind { return KindLease }

// Migrate establishes that the caller may migrate this instance at all.
//
// Ownership is the one artifact that is never re-keyed and never carried on
// trust: the fence in the scope is compared against the live lease, and every
// way that comparison can fail -- a token the resource has moved past, a
// different lease or holder, a lease that lapsed by the caller's own instant,
// or a live lease with no fence presented -- refuses the whole run with
// [CodeStaleLease]. WF-RUN-026's RED clause names "accepts stale lease" as a
// failure in its own right, so a stale lease may not be carried forward under
// the new epoch on the grounds that nothing else went wrong.
func (leaseHandler) Migrate(ctx context.Context, ex Executor, scope Scope, ports Ports) (ret0 []Entry, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.lease", scope)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	ref := scope.InstanceID.String()
	current, held, err := ports.Leases.Current(ctx, ex, scope.TenantID, scope.InstanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, KindLease, ref, err, "read the instance's live lease")
	}
	zeroFence := scope.Fence.LeaseID == uuid.Nil && scope.Fence.Token == 0
	if !held {
		if !zeroFence {
			return nil, refuse(CodeStaleLease, KindLease, ref,
				"fence token %d was presented but no live lease stands on the instance", scope.Fence.Token)
		}
		return nil, nil
	}
	if zeroFence {
		return nil, refuse(CodeStaleLease, KindLease, ref,
			"instance is held by %s under fence token %d; migrating another holder's artifacts is refused",
			current.Holder, current.FenceToken)
	}
	if err := scope.Fence.Validate(); err != nil {
		return nil, wrap(CodeStaleLease, KindLease, ref, err, "the presented fence is not well formed")
	}
	switch {
	case scope.Fence.TenantID != scope.TenantID:
		return nil, refuse(CodeStaleLease, KindLease, ref,
			"presented fence belongs to tenant %s, not %s", scope.Fence.TenantID, scope.TenantID)
	case scope.Fence.Resource.Kind != leaseResourceWorkflowInstance || scope.Fence.Resource.ID != ref:
		return nil, refuse(CodeStaleLease, KindLease, ref,
			"presented fence names resource %s, not this instance", scope.Fence.Resource)
	case current.FenceToken != scope.Fence.Token:
		return nil, refuse(CodeStaleLease, KindLease, ref,
			"presented fence token %d is not the resource's live token %d, held by %s",
			scope.Fence.Token, current.FenceToken, current.Holder)
	case current.LeaseID != scope.Fence.LeaseID:
		return nil, refuse(CodeStaleLease, KindLease, ref,
			"presented fence names lease %s; the live lease is %s", scope.Fence.LeaseID, current.LeaseID)
	case current.Holder != scope.Fence.Holder.HolderID():
		return nil, refuse(CodeStaleLease, KindLease, ref,
			"presented fence names holder %s; the live lease is held by %s",
			scope.Fence.Holder.HolderID(), current.Holder)
	case !current.ExpiresAt.After(scope.MigratedAt):
		return nil, refuse(CodeStaleLease, KindLease, ref,
			"lease expired at %s and the migration instant is %s; a holder past its own window may not migrate",
			instantText(current.ExpiresAt), instantText(scope.MigratedAt))
	}
	return []Entry{{
		Kind: KindLease, Identity: "instance:" + ref, Disposition: Carried,
		FromRef: current.LeaseID.String(), ToRef: current.LeaseID.String(),
		Owner: current.Holder, Deadline: current.ExpiresAt.UTC(),
	}}, nil
}

// --- timers -----------------------------------------------------------------

type timerHandler struct{}

func (timerHandler) Kind() Kind { return KindTimer }

// Migrate re-keys the instance's pending wake promises onto the new epoch
// without moving a single wake instant.
//
// A timer's durable key is the wake requirement's content digest (WF-RUN-004),
// so a target plan that resolves the same wake under a different zone,
// calendar or reference-update policy produces a different key even though it
// promises the same instant. That is exactly what "re-keyed by the new plan's
// requirement digest without changing the wake instant" means, and it is
// enforced rather than assumed: a replacement requirement whose FireAt is not
// the promise already on the table is [CodeWakeInstantMoved].
//
// A timer raised on a node the migration is not moving off is carried
// untouched, whatever the requirement map says about it.
func (timerHandler) Migrate(ctx context.Context, ex Executor, scope Scope, ports Ports) (ret0 []Entry, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.timer", scope)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	rows, err := ports.Timers.Pending(ctx, ex, scope.TenantID, scope.InstanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, KindTimer, scope.InstanceID.String(), err,
			"read the instance's pending timers")
	}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		identity := row.Kind + "@" + instantText(row.FiresAt)
		ref := row.TimerID.String()

		if row.NodeID != scope.From.NodeID {
			// The promise belongs to a node this migration does not touch.
			entries = append(entries, carriedTimer(identity, ref, row))
			continue
		}
		requirement, replacing := scope.Requirements[row.Key]
		if !replacing {
			if scope.Relocating() {
				return nil, refuse(CodeNotRelocatable, KindTimer, ref,
					"timer on %s promises %s but the frontier moves to %s and no replacement wake requirement was supplied for key %s",
					row.NodeID, instantText(row.FiresAt), scope.To.NodeID, row.Key)
			}
			entries = append(entries, carriedTimer(identity, ref, row))
			continue
		}
		if requirement.Digest == "" {
			return nil, refuse(CodeInvalidRequest, KindTimer, ref,
				"replacement wake requirement for key %s carries no digest; it was not produced by wait.ComputeTimerRequirement",
				row.Key)
		}
		if !requirement.FireAt.IsSet() || !requirement.FireAt.Time().UTC().Equal(row.FiresAt.UTC()) {
			return nil, refuse(CodeWakeInstantMoved, KindTimer, ref,
				"promise wakes at %s; the replacement requirement resolves to %s -- a migration re-keys a promise, it never reschedules one",
				instantText(row.FiresAt), instantText(requirement.FireAt.Time()))
		}

		next := TimerRow{
			TimerID: timer.TimerID(scope.TenantID, scope.InstanceID, scope.To.NodeID, requirement.Digest),
			NodeID:  scope.To.NodeID, Key: requirement.Digest, Kind: row.Kind, FiresAt: row.FiresAt.UTC(),
		}
		if next.TimerID == row.TimerID {
			entries = append(entries, carriedTimer(identity, ref, row))
			continue
		}
		replay, err := ports.Timers.Schedule(ctx, ex, scope.TenantID, scope.InstanceID, next, scope.MigratedAt)
		if err != nil {
			return nil, wrap(CodeStorageFailed, KindTimer, ref, err,
				"write the re-keyed timer for %s under the new epoch", scope.To.NodeID)
		}
		if err := ports.Timers.Cancel(ctx, ex, scope.TenantID, row.TimerID, row.Version,
			scope.MigratedAt, migrationReason); err != nil {
			return nil, wrap(CodeStorageFailed, KindTimer, ref, err, "retire the predecessor timer")
		}
		entries = append(entries, Entry{
			Kind: KindTimer, Identity: identity, Disposition: dispositionFor(replay),
			FromRef: ref, ToRef: next.TimerID.String(), Deadline: row.FiresAt.UTC(),
		})
	}
	return entries, nil
}

func carriedTimer(identity, ref string, row TimerRow) Entry {
	return Entry{
		Kind: KindTimer, Identity: identity, Disposition: Carried,
		FromRef: ref, ToRef: ref, Deadline: row.FiresAt.UTC(),
	}
}

// --- signal subscriptions ---------------------------------------------------

type signalHandler struct{}

func (signalHandler) Kind() Kind { return KindSignal }

// Migrate re-keys the instance's open subscriptions onto the new epoch.
//
// A subscription's semantic identity is the signal it waits for and the
// correlation key it waits under; the node is epoch state, and the durable
// row is unique per (instance, node, signal). So a migration that moves the
// frontier opens the identical wait at the new node and closes the old row as
// CANCELLED. Closing rather than deleting is deliberate: the fact that this
// instance once waited for this signal at that node is history, and a
// migration does not get to rewrite history.
func (signalHandler) Migrate(ctx context.Context, ex Executor, scope Scope, ports Ports) (ret0 []Entry, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.signal", scope)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	rows, err := ports.Signals.Open(ctx, ex, scope.TenantID, scope.InstanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, KindSignal, scope.InstanceID.String(), err,
			"read the instance's open signal subscriptions")
	}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		identity := row.SignalName + "/" + row.CorrelationKey
		ref := row.SubscriptionID.String()
		if row.NodeID != scope.From.NodeID || !scope.Relocating() {
			entries = append(entries, Entry{
				Kind: KindSignal, Identity: identity, Disposition: Carried, FromRef: ref, ToRef: ref,
			})
			continue
		}
		next := SubscriptionRow{
			SubscriptionID: SubscriptionID(scope.TenantID, scope.InstanceID, scope.To.NodeID, row.SignalName),
			NodeID:         scope.To.NodeID, SignalName: row.SignalName, CorrelationKey: row.CorrelationKey,
		}
		if next.SubscriptionID == row.SubscriptionID {
			entries = append(entries, Entry{
				Kind: KindSignal, Identity: identity, Disposition: Carried, FromRef: ref, ToRef: ref,
			})
			continue
		}
		replay, err := ports.Signals.Subscribe(ctx, ex, scope.TenantID, scope.InstanceID, next, scope.MigratedAt)
		if err != nil {
			return nil, wrap(CodeStorageFailed, KindSignal, ref, err,
				"open the re-keyed subscription for %s under the new epoch", scope.To.NodeID)
		}
		if err := ports.Signals.Close(ctx, ex, scope.TenantID, row.SubscriptionID, row.Version, scope.MigratedAt); err != nil {
			return nil, wrap(CodeStorageFailed, KindSignal, ref, err, "close the predecessor subscription")
		}
		entries = append(entries, Entry{
			Kind: KindSignal, Identity: identity, Disposition: dispositionFor(replay),
			FromRef: ref, ToRef: next.SubscriptionID.String(),
		})
	}
	return entries, nil
}

// --- ready work -------------------------------------------------------------

type readyWorkHandler struct{}

func (readyWorkHandler) Kind() Kind { return KindReadyWork }

// Migrate re-keys unsettled ready work onto the new epoch's node and attempt,
// preserving the eligibility instant and the priority exactly.
//
// The replacement row's identity is [timer.ReadyWorkID] of the new node and
// attempt -- the same derivation WF-RUN-004 uses when a fired timer enqueues
// work -- so a migration and a subsequent timer fire address one row rather
// than racing to create two.
func (readyWorkHandler) Migrate(ctx context.Context, ex Executor, scope Scope, ports Ports) (ret0 []Entry, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.ready_work", scope)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	rows, err := ports.ReadyWork.Pending(ctx, ex, scope.TenantID, scope.InstanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, KindReadyWork, scope.InstanceID.String(), err,
			"read the instance's pending ready work")
	}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		identity := "eligible@" + instantText(row.EligibleAt)
		ref := row.ReadyWorkID.String()
		if row.NodeID != scope.From.NodeID || !scope.Relocating() {
			entries = append(entries, carriedReady(identity, ref, row))
			continue
		}
		next := ReadyRow{
			ReadyWorkID: timer.ReadyWorkID(scope.TenantID, scope.InstanceID, scope.To.NodeID, scope.To.Attempt),
			NodeID:      scope.To.NodeID, Attempt: scope.To.Attempt, Priority: row.Priority,
			State: row.State, EligibleAt: row.EligibleAt.UTC(),
		}
		if next.ReadyWorkID == row.ReadyWorkID {
			entries = append(entries, carriedReady(identity, ref, row))
			continue
		}
		replay, err := ports.ReadyWork.Enqueue(ctx, ex, scope.TenantID, scope.InstanceID, next, scope.MigratedAt)
		if err != nil {
			return nil, wrap(CodeStorageFailed, KindReadyWork, ref, err,
				"enqueue the re-keyed ready work for %s under the new epoch", scope.To.NodeID)
		}
		if err := ports.ReadyWork.Cancel(ctx, ex, scope.TenantID, row.ReadyWorkID, row.Version, scope.MigratedAt); err != nil {
			return nil, wrap(CodeStorageFailed, KindReadyWork, ref, err, "retire the predecessor ready work")
		}
		entries = append(entries, Entry{
			Kind: KindReadyWork, Identity: identity, Disposition: dispositionFor(replay),
			FromRef: ref, ToRef: next.ReadyWorkID.String(), Deadline: row.EligibleAt.UTC(),
		})
	}
	return entries, nil
}

func carriedReady(identity, ref string, row ReadyRow) Entry {
	return Entry{
		Kind: KindReadyWork, Identity: identity, Disposition: Carried,
		FromRef: ref, ToRef: ref, Deadline: row.EligibleAt.UTC(),
	}
}

// --- approvals --------------------------------------------------------------

type approvalHandler struct{}

func (approvalHandler) Kind() Kind { return KindApproval }

// Migrate carries the instance's live approval work items onto the new epoch
// with their owner, deadline and own identity untouched -- and refuses,
// rather than approximating, an approval whose node the migration moves off.
//
// A work item is where responsibility currently sits. Its node is fixed at
// creation and internal/humanwork/workitem exposes no way to move it, for the
// good reason that responsibility is not a column a migration may quietly
// rewrite. Minting a replacement item would mint a second unit of human
// responsibility for one decision -- exactly the duplication WF-RUN-026's RED
// clause names -- and cancelling the original would drop a claim somebody may
// already be working. So a relocation is [CodeNotRelocatable]: the operator
// decides whether to complete the approval first, or to migrate to a target
// that keeps the node.
func (approvalHandler) Migrate(ctx context.Context, ex Executor, scope Scope, ports Ports) (ret0 []Entry, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.approval", scope)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	rows, err := ports.Approvals.Pending(ctx, ex, scope.TenantID, scope.InstanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, KindApproval, scope.InstanceID.String(), err,
			"read the instance's live approval work items")
	}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		ref := row.WorkItemID.String()
		if row.NodeID == scope.From.NodeID && scope.Relocating() {
			return nil, refuse(CodeNotRelocatable, KindApproval, ref,
				"approval on %s is %s and owned by %s; the frontier moves to %s and a routed approval's node is immutable",
				row.NodeID, row.Status, row.Owner, scope.To.NodeID)
		}
		entries = append(entries, Entry{
			Kind: KindApproval, Identity: "work_item:" + ref, Disposition: Carried,
			FromRef: ref, ToRef: ref, Owner: row.Owner, Deadline: row.DeadlineAt.UTC(),
		})
	}
	return entries, nil
}

// --- child continuations ----------------------------------------------------

type childHandler struct{}

func (childHandler) Kind() Kind { return KindChildContinuation }

// Migrate carries the instance's child links onto the new epoch and records
// the one continuation ledger row saying the parent still awaits them.
//
// A child link's parent node is immutable and a child that reported back to a
// node the parent no longer sits on would report back to nothing, so an owed
// child whose parent node this migration moves off is [CodeNotRelocatable].
// A DETACH child owes the parent nothing and is carried regardless.
//
// The ledger row is where "zero duplicate continuation" is made structural
// rather than promised. Its identity is derived from (instance, source node,
// attempt, target node, kind), so a parent awaiting five children records one
// row, not five, and a replayed migration writes none: the derived identity
// collides and the insert is a no-op.
func (childHandler) Migrate(ctx context.Context, ex Executor, scope Scope, ports Ports) (ret0 []Entry, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.child", scope)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	links, err := ports.Children.Links(ctx, ex, scope.TenantID, scope.InstanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, KindChildContinuation, scope.InstanceID.String(), err,
			"read the instance's child links")
	}
	entries := make([]Entry, 0, len(links)+1)
	owed := make([]string, 0, len(links))
	for _, link := range links {
		ref := link.Child.String()
		if link.Owed() {
			if link.ParentNodeID == scope.From.NodeID && scope.Relocating() {
				return nil, refuse(CodeNotRelocatable, KindChildContinuation, ref,
					"child %s is awaited by %s under mode %s; the frontier moves to %s and a child link's parent node is immutable",
					ref, link.ParentNodeID, link.Mode, scope.To.NodeID)
			}
			owed = append(owed, ref)
		}
		entries = append(entries, Entry{
			Kind: KindChildContinuation, Identity: "child:" + ref, Disposition: Carried,
			FromRef: ref, ToRef: ref, Owner: link.ParentNodeID,
		})
	}
	if len(owed) == 0 {
		return entries, nil
	}
	sort.Strings(owed)
	joined := strings.Join(owed, ",")
	if err := ports.Continuations.Record(ctx, ex, ContinuationIntent{
		TenantID: scope.TenantID, InstanceID: scope.InstanceID,
		NodeID: scope.To.NodeID, Attempt: scope.To.Attempt,
		Ref: joined, RecordedAt: scope.MigratedAt,
	}); err != nil {
		return nil, wrap(CodeStorageFailed, KindChildContinuation, scope.InstanceID.String(), err,
			"record the awaiting continuation under the new epoch")
	}
	entries = append(entries, Entry{
		Kind: KindChildContinuation, Identity: "awaiting", Disposition: continuationDisposition(scope),
		FromRef: scope.From.NodeID, ToRef: scope.To.NodeID, Owner: joined,
	})
	return entries, nil
}

func continuationDisposition(scope Scope) Disposition {
	if scope.Relocating() {
		return Rekeyed
	}
	return Carried
}

func dispositionFor(replay bool) Disposition {
	if replay {
		return Deduplicated
	}
	return Rekeyed
}
