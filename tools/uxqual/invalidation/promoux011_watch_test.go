package invalidation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_PROMOUX_011 proves that an invalidation which is outside the
// currently authorized projection cannot poison the sequence checkpoint. A
// later authorized transition at the next sequence is still admitted.
func TestTodo_PROMOUX_011(t *testing.T) {
	visible := testSubject("00000000-0000-4000-8000-000000000201")
	foreign := visible
	foreign.Tenant = values.TenantId("other")
	var applied atomic.Int32
	var latest uint64
	client, err := New(testScope(visible), func(_ context.Context, refresh Refresh) error {
		latest = refresh.SourceSequence
		applied.Add(1)
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{
		testMessageRevision(t, foreign.Tenant, "worker_summary", 11, 11, foreign),
		testMessage(t, 12, visible),
	}}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if applied.Load() != 1 || latest != 12 {
		t.Fatalf("authorized applications = %d at sequence %d, want one at 12", applied.Load(), latest)
	}
	if got := client.Snapshot(); got.Rejected != 1 || got.Accepted != 1 || got.Refetched != 1 || got.LastSourceSequence != 12 {
		t.Fatalf("snapshot = %+v, want rejected foreign hint and committed authorized hint", got)
	}
}

// TestTodo_PROMOUX_011_Integration proves that a reconnect catches up the
// exact missing contiguous range before admitting the live hint, and that the
// authoritative catch-up checkpoint is the one used for the next generation.
func TestTodo_PROMOUX_011_Integration(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000202")
	first := &sliceStream{values: [][]byte{testMessage(t, 13, subject)}}
	second := newBlockingStream()
	var catchUps atomic.Int32
	var opened atomic.Int32
	var requested CatchUpRequest
	client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.RunReconnect(ctx, func(_ context.Context, cursor Cursor) (CloseStream, error) {
			switch opened.Add(1) {
			case 1:
				if cursor.Sequence() != 10 {
					return nil, errors.New("unexpected initial cursor")
				}
				return first, nil
			default:
				if cursor.Sequence() != 13 {
					return nil, errors.New("live hint cursor was not committed")
				}
				return second, nil
			}
		}, func(_ context.Context, request CatchUpRequest) (CatchUpResult, error) {
			requested = request
			catchUps.Add(1)
			return CatchUpResult{SourceSequence: request.ToSequence, Watermark: request.ToSequence}, nil
		}, fastReconnect)
	}()
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.LastSourceSequence == 13 && snapshot.Refetched == 1 })
	cancel()
	if got := <-done; !errors.Is(got, context.Canceled) {
		t.Fatalf("RunReconnect = %v, want cancellation", got)
	}
	if catchUps.Load() != 1 || requested.From.Sequence() != 10 || requested.ToSequence != 12 {
		t.Fatalf("catch-up count/request = %d/%+v, want one exact range 11..12", catchUps.Load(), requested)
	}
	if got := client.Snapshot(); got.CatchUps != 1 || got.CatchUpErrors != 0 || got.LastSourceSequence != 13 {
		t.Fatalf("snapshot = %+v, want committed catch-up and live hint", got)
	}
}

// TestTodo_PROMOUX_011_WatchRecovery proves a failed authoritative refetch does
// not advance the local checkpoint; the same durable transition can then be
// retried after reconnect without being discarded as stale.
func TestTodo_PROMOUX_011_WatchRecovery(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000203")
	var attempts atomic.Int32
	client, err := New(testScope(subject), func(context.Context, Refresh) error {
		if attempts.Add(1) == 1 {
			return errors.New("temporary read failure")
		}
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	raw := testMessage(t, 11, subject)
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{raw}}); err != nil {
		t.Fatal(err)
	}
	if got := client.Snapshot(); got.LastSourceSequence != 10 || got.Refetched != 0 || got.RefetchErrors != 1 {
		t.Fatalf("failed transition snapshot = %+v, want uncommitted sequence", got)
	}
	if err := client.Run(context.Background(), &sliceStream{values: [][]byte{raw}}); err != nil {
		t.Fatal(err)
	}
	if got := client.Snapshot(); got.LastSourceSequence != 11 || got.Refetched != 1 || attempts.Load() != 2 {
		t.Fatalf("recovery snapshot = %+v, attempts=%d; want one committed retry", got, attempts.Load())
	}
}

// TestTodo_PROMOUX_011_Race exercises concurrent observation while a bounded
// stream admits a burst of durable transitions. The callback and counters are
// deliberately atomic so this test remains useful under -race.
func TestTodo_PROMOUX_011_Race(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000204")
	var applied atomic.Int32
	client, err := New(testScope(subject), func(context.Context, Refresh) error {
		applied.Add(1)
		return nil
	}, Options{MaxQueue: 16})
	if err != nil {
		t.Fatal(err)
	}
	stream := newChannelStream(16)
	done, err := client.Start(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	for sequence := uint64(11); sequence <= 18; sequence++ {
		stream.messages <- testMessage(t, sequence, subject)
	}
	for i := 0; i < 8; i++ {
		go func() {
			for j := 0; j < 32; j++ {
				_ = client.Snapshot()
			}
		}()
	}
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Refetched == 8 })
	if applied.Load() != 8 || client.Snapshot().LastSourceSequence != 18 {
		t.Fatalf("burst snapshot = %+v, applications=%d; want eight committed transitions", client.Snapshot(), applied.Load())
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("race subscription did not close")
	}
}

// TestTodo_PROMOUX_011_Performance keeps the invalidation lane bounded while
// processing a representative burst. It guards the observable contract that
// durable transitions converge promptly without an unbounded queue.
func TestTodo_PROMOUX_011_Performance(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000205")
	client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{MaxQueue: 64})
	if err != nil {
		t.Fatal(err)
	}
	stream := newChannelStream(64)
	done, err := client.Start(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	for sequence := uint64(11); sequence <= 74; sequence++ {
		stream.messages <- testMessage(t, sequence, subject)
	}
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Refetched == 64 })
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("64-transition invalidation burst took %s", elapsed)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("performance subscription did not close")
	}
}
