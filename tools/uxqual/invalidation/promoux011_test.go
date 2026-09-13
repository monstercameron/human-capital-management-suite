package invalidation

// TestTodo_PROMOUX_011_Recovery proves that a promotion region's reconnect
// catch-up converges without duplicate renders, by counting renders rather
// than inspecting the converged state: a client that rendered twice for the
// same transition would still converge to the correct final view (a double
// render collapses to the same state as a single one), so only a count of
// how many times Refetch actually ran can tell the two cases apart.
//
// The scenario: a promotion Detail region's live connection delivers
// transition 11 and then ends cleanly (modeling a connection that drops
// before the server confirms the client applied it). On reconnect, the
// server -- uncertain whether the dropped connection's client ever saw 11 --
// redelivers it before sending the genuinely new transition 12. This reuses
// tools/uxqual/invalidation's own RunReconnect and the admission-sequence
// check already proven by TestTodo_WEB_036's suite (this file adds no new
// invalidation transport or admission logic): a redelivered message whose
// SourceSequence does not exceed the client's already-admitted position is
// rejected in processing.go's handle(), so the second connection's replay of
// 11 must produce zero renders while its genuinely new 12 must produce
// exactly one.
import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTodo_PROMOUX_011_Recovery(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-0000000ee001")
	var mu sync.Mutex
	var rendered []uint64
	client, err := New(testScope(subject), func(_ context.Context, refresh Refresh) error {
		mu.Lock()
		rendered = append(rendered, refresh.SourceSequence)
		mu.Unlock()
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	var cursorsSeen []uint64
	var attempt atomic.Int32
	firstOpened := make(chan struct{})
	secondOpened := make(chan struct{})
	thirdOpened := make(chan struct{})

	result := make(chan error, 1)
	go func() {
		result <- client.RunReconnect(context.Background(), func(_ context.Context, cursor Cursor) (CloseStream, error) {
			mu.Lock()
			cursorsSeen = append(cursorsSeen, cursor.Sequence())
			mu.Unlock()
			switch attempt.Add(1) {
			case 1:
				defer close(firstOpened)
				// The first connection delivers transition 11 once, then
				// ends cleanly -- modeling a drop the server could not
				// confirm the client survived.
				return newReconnectTestStream(false, testMessage(t, 11, subject)), nil
			case 2:
				defer close(secondOpened)
				// The reconnect: the server redelivers 11 (uncertain whether
				// it was applied) before the genuinely new transition 12.
				return newReconnectTestStream(false, testMessage(t, 11, subject), testMessage(t, 12, subject)), nil
			default:
				defer close(thirdOpened)
				// A third, stable connection that just holds open until the
				// test closes the client.
				return newReconnectTestStream(true), nil
			}
		}, func(context.Context, CatchUpRequest) (CatchUpResult, error) {
			return CatchUpResult{}, errors.New("contiguous redelivery must not require catch-up")
		}, ReconnectOptions{MaxAttempts: 4, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
	}()

	select {
	case <-thirdOpened:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect sequence did not reach its third, stable connection")
	}
	// Every render this scenario will ever produce has already happened by
	// the time the third connection opens (opening it requires the prior
	// generation to have ended, which requires its Recv loop -- and the
	// synchronous managed refetch each message triggers -- to have finished).
	waitSnapshot(t, client, func(s Snapshot) bool { return s.Refetched == 2 })

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waitReconnectResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("terminal = %v, want cancellation", err)
	}

	mu.Lock()
	gotCursors := append([]uint64(nil), cursorsSeen...)
	gotRendered := append([]uint64(nil), rendered...)
	mu.Unlock()

	if len(gotCursors) < 2 || gotCursors[0] != 10 || gotCursors[1] != 11 {
		t.Fatalf("reconnect checkpoints = %v, want to start at baseline 10 then resume from the committed 11", gotCursors)
	}
	// The decisive count: exactly two renders (11, then 12), never three.
	// A client that duplicated the render for the redelivered 11 would still
	// converge to the same final state as a correct one -- the count is what
	// distinguishes them.
	if len(gotRendered) != 2 || gotRendered[0] != 11 || gotRendered[1] != 12 {
		t.Fatalf("rendered sequence = %v, want exactly [11, 12] -- the redelivered 11 must not render twice", gotRendered)
	}
	if snapshot := client.Snapshot(); snapshot.Refetched != 2 || snapshot.LastSourceSequence != 12 {
		t.Fatalf("snapshot = %+v, want exactly 2 committed refetches converged on 12", snapshot)
	}

	// Boundary this test does not reach: DOM scroll position and focus are
	// browser-runtime state with no representation in this package's Go
	// types, so "lost scroll/focus" is provable only in a real DOM (the
	// Browser test's SSR rendering proves regions are structurally
	// independent containers, which is what a real client needs in order to
	// patch one region without disturbing another's scroll or focus, but it
	// cannot itself drive scrolling or focus in a headless Go test).
}
