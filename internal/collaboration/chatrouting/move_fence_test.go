package chatrouting

import (
	"context"
	"errors"
	"testing"
)

type recoveringMover struct {
	fenceCalls, abortCalls       int
	abortedEpoch                 uint64
	fenceErr, copyErr, verifyErr error
}

type unfencedMover struct{}

func (unfencedMover) CopyConversation(context.Context, MovePlan) error   { return nil }
func (unfencedMover) VerifyConversation(context.Context, MovePlan) error { return nil }
func (unfencedMover) DrainConversation(context.Context, MovePlan) error  { return nil }

func TestTodo_CHAT_006_MoveRejectsUnfencedShard(t *testing.T) {
	ctx := context.Background()
	d := NewMemoryDirectory()
	r, err := d.Reserve(ctx, ReserveRequest{ConversationID: "c1", HostTenantID: "tenant-a", ShardID: "s1", IdempotencyKey: "create"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Activate(ctx, "c1", "tenant-a", r.Epoch); err != nil {
		t.Fatal(err)
	}
	if _, err = (MoveCoordinator{Directory: d, Shards: unfencedMover{}}).Move(ctx, "c1", "tenant-a", 1, "s2"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unfenced move error: %v", err)
	}
	current, err := d.Lookup(ctx, "c1", "tenant-a")
	if err != nil || current.State != StateActive || current.Epoch != 1 {
		t.Fatalf("route changed without a fence: %+v, %v", current, err)
	}
}

func (m *recoveringMover) FenceWrites(context.Context, MovePlan) error {
	m.fenceCalls++
	return m.fenceErr
}
func (m *recoveringMover) AbortWrites(_ context.Context, _ MovePlan, epoch uint64) error {
	m.abortCalls++
	m.abortedEpoch = epoch
	return nil
}
func (m *recoveringMover) CopyConversation(context.Context, MovePlan) error   { return m.copyErr }
func (m *recoveringMover) VerifyConversation(context.Context, MovePlan) error { return m.verifyErr }
func (m *recoveringMover) DrainConversation(context.Context, MovePlan) error  { return nil }

func TestTodo_CHAT_006_MoveFailureRestoresLocalFence(t *testing.T) {
	for _, stage := range []string{"fence", "copy", "verify"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			d := NewMemoryDirectory()
			r, err := d.Reserve(ctx, ReserveRequest{ConversationID: "c1", HostTenantID: "tenant-a", ShardID: "s1", IdempotencyKey: "create"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = d.Activate(ctx, "c1", "tenant-a", r.Epoch); err != nil {
				t.Fatal(err)
			}
			failure := errors.New(stage + " failed")
			mover := &recoveringMover{}
			if stage == "fence" {
				mover.fenceErr = failure
			} else if stage == "copy" {
				mover.copyErr = failure
			} else {
				mover.verifyErr = failure
			}
			_, err = (MoveCoordinator{Directory: d, Shards: mover}).Move(ctx, "c1", "tenant-a", 1, "s2")
			if !errors.Is(err, failure) {
				t.Fatalf("move error: %v", err)
			}
			current, err := d.Lookup(ctx, "c1", "tenant-a")
			if err != nil || current.State != StateActive || current.Epoch != 3 || current.ShardID != "s1" {
				t.Fatalf("directory after abort: %+v, %v", current, err)
			}
			if mover.fenceCalls != 1 || mover.abortCalls != 1 || mover.abortedEpoch != current.Epoch {
				t.Fatalf("local rollback: %+v", mover)
			}
		})
	}
}
