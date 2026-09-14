package artifacts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// DefaultPorts returns the durable implementation of every port: the
// scheduling-state stores WF-RUN-004 and WF-RUN-002 already own, the work-item
// store internal/humanwork owns, and internal/workflow/runtime's own
// continuation ledger. It holds no state, so a caller constructs it per call.
//
// Nothing here opens a transaction or reads a clock; every method takes the
// caller's [Executor] and, where an instant is needed, the caller's instant.
func DefaultPorts() Ports {
	return Ports{
		Leases:        storeLeases{},
		Timers:        storeTimers{},
		Signals:       storeSignals{},
		ReadyWork:     storeReadyWork{},
		Approvals:     storeApprovals{},
		Children:      storeChildren{},
		Continuations: storeContinuations{},
	}
}

// --- leases -----------------------------------------------------------------

type storeLeases struct{ store runtimestate.LeaseStore }

func (s storeLeases) Current(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (LeaseRow, bool, error) {
	// CurrentForUpdate, not Current: a fence verified against an unlocked
	// read can be superseded between the read and the writes it authorizes,
	// which is precisely the "accepts stale lease" failure this ticket names.
	row, err := s.store.CurrentForUpdate(ctx, ex, tenantID, runtimestate.LeaseWorkflowInstance, instanceID.String())
	if err != nil {
		if errors.Is(err, runtimestate.ErrNotFound) {
			return LeaseRow{}, false, nil
		}
		return LeaseRow{}, false, err
	}
	return LeaseRow{
		LeaseID: row.LeaseID, Holder: row.HolderID, FenceToken: row.FenceToken,
		ExpiresAt: row.ExpiresAt.UTC(),
	}, true, nil
}

// --- timers -----------------------------------------------------------------

type storeTimers struct{ store runtimestate.TimerStore }

func (s storeTimers) Pending(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]TimerRow, error) {
	rows, err := s.store.PendingForInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return nil, err
	}
	out := make([]TimerRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, TimerRow{
			TimerID: row.TimerID, NodeID: row.NodeID, Key: row.Key, Kind: row.Kind,
			FiresAt: row.FiresAt.UTC(), Version: row.Version,
		})
	}
	return out, nil
}

func (s storeTimers) Schedule(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, row TimerRow, at time.Time) (ret0 bool, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.schedule_timer", observe.Attrs{observe.KeyTenant: tenantID.String()}, observe.Attrs{observe.KeyInstance: instanceID.String()}, row)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	err := s.store.Set(ctx, ex, runtimestate.Timer{
		TenantID: tenantID, TimerID: row.TimerID, InstanceID: instanceID, NodeID: row.NodeID,
		Key: row.Key, Kind: row.Kind, FiresAt: row.FiresAt.UTC(), CreatedAt: at,
	})
	if errors.Is(err, runtimestate.ErrDuplicate) {
		return true, nil
	}
	return false, err
}

func (s storeTimers) Cancel(ctx context.Context, ex Executor, tenantID, timerID uuid.UUID, version uint64, at time.Time, _ string) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.cancel_timer", observe.Attrs{observe.KeyTenant: tenantID.String()}, observe.Attrs{observe.KeyTimer: timerID.String()})
	defer func() { observe.DoneWith(obsOp, retErr) }()
	return s.store.Cancel(ctx, ex, tenantID, timerID, version, at)
}

// --- signal subscriptions ---------------------------------------------------

type storeSignals struct{ store runtimestate.SignalStore }

func (s storeSignals) Open(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]SubscriptionRow, error) {
	rows, err := s.store.OpenForInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return nil, err
	}
	out := make([]SubscriptionRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, SubscriptionRow{
			SubscriptionID: row.SubscriptionID, NodeID: row.NodeID, SignalName: row.SignalName,
			CorrelationKey: row.CorrelationKey, Version: row.Version,
		})
	}
	return out, nil
}

func (s storeSignals) Subscribe(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, row SubscriptionRow, at time.Time) (ret0 bool, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.subscribe_signal", observe.Attrs{observe.KeyTenant: tenantID.String()}, observe.Attrs{observe.KeyInstance: instanceID.String()}, row)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	err := s.store.Subscribe(ctx, ex, runtimestate.Subscription{
		TenantID: tenantID, SubscriptionID: row.SubscriptionID, InstanceID: instanceID,
		NodeID: row.NodeID, SignalName: row.SignalName, CorrelationKey: row.CorrelationKey,
		CreatedAt: at,
	})
	if errors.Is(err, runtimestate.ErrDuplicate) {
		return true, nil
	}
	return false, err
}

func (s storeSignals) Close(ctx context.Context, ex Executor, tenantID, subscriptionID uuid.UUID, version uint64, at time.Time) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.close_signal", observe.Attrs{observe.KeyTenant: tenantID.String()})
	defer func() { observe.DoneWith(obsOp, retErr) }()
	return s.store.CloseSubscription(ctx, ex, tenantID, subscriptionID, version, runtimestate.SubscriptionCancelled, at)
}

// --- ready work -------------------------------------------------------------

type storeReadyWork struct{ store runtimestate.ReadyWorkStore }

func (s storeReadyWork) Pending(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]ReadyRow, error) {
	rows, err := s.store.PendingForInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return nil, err
	}
	out := make([]ReadyRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, ReadyRow{
			ReadyWorkID: row.ReadyWorkID, NodeID: row.NodeID, Attempt: row.Attempt,
			Priority: row.Priority, State: row.State, EligibleAt: row.EligibleAt.UTC(), Version: row.Version,
		})
	}
	return out, nil
}

func (s storeReadyWork) Enqueue(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, row ReadyRow, at time.Time) (ret0 bool, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.enqueue_ready_work", observe.Attrs{observe.KeyTenant: tenantID.String()}, observe.Attrs{observe.KeyInstance: instanceID.String()}, row)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	err := s.store.Enqueue(ctx, ex, runtimestate.ReadyWork{
		TenantID: tenantID, ReadyWorkID: row.ReadyWorkID, InstanceID: instanceID,
		NodeID: row.NodeID, Attempt: row.Attempt, State: runtimestate.ReadyReady,
		Priority: row.Priority, EligibleAt: row.EligibleAt.UTC(), EnqueuedAt: at,
	})
	if errors.Is(err, runtimestate.ErrDuplicate) {
		return true, nil
	}
	return false, err
}

func (s storeReadyWork) Cancel(ctx context.Context, ex Executor, tenantID, readyWorkID uuid.UUID, version uint64, at time.Time) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.cancel_ready_work", observe.Attrs{observe.KeyTenant: tenantID.String()})
	defer func() { observe.DoneWith(obsOp, retErr) }()
	return s.store.Transition(ctx, ex, tenantID, readyWorkID, version, runtimestate.ReadyCancelled, at)
}

// --- approvals --------------------------------------------------------------

type storeApprovals struct{ store workitem.Store }

// Pending returns the instance's approval work items that are not terminal.
// A COMPLETED, EXPIRED or CANCELLED item is history: it is not something the
// instance is still waiting on and not something a migration has to move.
func (s storeApprovals) Pending(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]ApprovalRow, error) {
	items, err := s.store.ListForInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalRow, 0, len(items))
	for _, item := range items {
		if item.Status.Terminal() {
			continue
		}
		out = append(out, ApprovalRow{
			WorkItemID: item.WorkItemID, NodeID: item.NodeID,
			Owner:  string(item.OwnerKind) + ":" + item.OwnerRef,
			Status: string(item.Status), DeadlineAt: item.DeadlineAt.UTC(),
		})
	}
	return out, nil
}

// --- child links ------------------------------------------------------------

type storeChildren struct{ store runtimestate.ChildLinkStore }

func (s storeChildren) Links(ctx context.Context, ex Executor, tenantID, parent uuid.UUID) ([]ChildRow, error) {
	rows, err := s.store.LinksForParent(ctx, ex, tenantID, parent)
	if err != nil {
		return nil, err
	}
	out := make([]ChildRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, ChildRow{
			Child: row.Child, ParentNodeID: row.ParentNodeID, Ordinal: row.Ordinal, Mode: row.Mode,
		})
	}
	return out, nil
}

// --- continuations ----------------------------------------------------------

type storeContinuations struct{ store runtime.ContinuationStore }

// Record writes the one READY continuation ledger row that says the migrated
// parent's new frontier node is what its awaited children report back to.
//
// [frontier.IntentReady] is the kind because that is what a child's
// completion produces for its parent: the parent's node becomes ready again.
// The row's identity is derived from the instance, the node, the attempt and
// that kind, so however many children are awaited there is exactly one row,
// and a replayed migration inserts none.
func (s storeContinuations) Record(ctx context.Context, ex Executor, in ContinuationIntent) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.migrate.artifacts.record_continuation", in)
	defer func() { observe.DoneWith(obsOp, retErr) }()
	return s.store.MarkReady(ctx, ex, runtime.ContinuationRecord{
		TenantID: in.TenantID, InstanceID: in.InstanceID,
		SourceNodeID: in.NodeID, SourceAttempt: in.Attempt,
		TargetNodeID: in.NodeID, Kind: frontier.IntentReady,
		Ref: in.Ref, RecordedAt: in.RecordedAt,
	})
}
