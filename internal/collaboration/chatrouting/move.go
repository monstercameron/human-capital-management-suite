package chatrouting

import (
	"context"
	"errors"
	"time"
)

// ShardMover owns copy verification and draining in the chat database. The
// source shard must reject writes once the route enters MOVING; the destination
// receives copied history only through this port.
type ShardMover interface {
	CopyConversation(context.Context, MovePlan) error
	VerifyConversation(context.Context, MovePlan) error
	DrainConversation(context.Context, MovePlan) error
}

// ShardFence is required for every move. The shard implements it
// by changing a local epoch row in the same database whose send transactions
// lock/check that row. This closes the route-directory-to-chat-database TOCTOU
// window: an in-flight old-shard transaction cannot commit after fencing.
type ShardFence interface {
	FenceWrites(context.Context, MovePlan) error
	AbortWrites(context.Context, MovePlan, uint64) error
}

type MoveCoordinator struct {
	Directory Directory
	Shards    ShardMover
}

func (m MoveCoordinator) Move(ctx context.Context, conversationID, tenant string, expectedEpoch uint64, target string) (Route, error) {
	if m.Directory == nil || m.Shards == nil || conversationID == "" || target == "" {
		return Route{}, ErrInvalid
	}
	fence, ok := m.Shards.(ShardFence)
	if !ok {
		return Route{}, ErrInvalid
	}
	plan, err := m.Directory.BeginMove(ctx, conversationID, tenant, expectedEpoch, target)
	if err != nil {
		return Route{}, err
	}
	if err = fence.FenceWrites(ctx, plan); err != nil {
		return Route{}, errors.Join(err, m.abort(ctx, plan, fence))
	}
	if err = m.Shards.CopyConversation(ctx, plan); err != nil {
		return Route{}, errors.Join(err, m.abort(ctx, plan, fence))
	}
	if err = m.Shards.VerifyConversation(ctx, plan); err != nil {
		return Route{}, errors.Join(err, m.abort(ctx, plan, fence))
	}
	r, err := m.Directory.Cutover(ctx, conversationID, tenant, plan.MoveEpoch)
	if err != nil {
		return Route{}, err
	}
	if err = m.Shards.DrainConversation(ctx, plan); err != nil {
		return r, err
	}
	return r, nil
}

func (m MoveCoordinator) abort(ctx context.Context, plan MovePlan, fence ShardFence) error {
	// Restore the two durable authorities even when the caller disconnected.
	// The directory CAS runs first: a completed cutover must never reopen its
	// source shard, and a failed CAS leaves the local fence closed.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	aborted, err := m.Directory.AbortMove(cleanup, plan.ConversationID, plan.HostTenantID, plan.MoveEpoch)
	if err != nil {
		return err
	}
	return fence.AbortWrites(cleanup, plan, aborted.Epoch)
}
