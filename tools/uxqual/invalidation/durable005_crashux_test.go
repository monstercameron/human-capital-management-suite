package invalidation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestTodo_DURABLE_005 is the PRIMARY test for the crash-UX record: what a
// person sees across a crash, and how the browser carries on. It covers
// reconnection with backoff and no reload, resubscription of everything on
// screen from stored state, the quiet gap UI, drafts/forms/recordings kept
// across the gap and a reload, sessions surviving a restart, and actions
// during the gap queued once or refused plainly.
func TestTodo_DURABLE_005(t *testing.T) {
	areas := DurableCrashUX()
	if len(areas) == 0 {
		t.Fatal("crash-UX record is empty")
	}
	wantAreas := []string{
		"reconnect",
		"resubscribe",
		"gap-ui",
		"drafts-forms",
		"voice-recording",
		"sessions",
		"disconnected-actions",
	}
	byID := make(map[string]DurableCrashArea, len(areas))
	for _, area := range areas {
		if area.ID == "" || area.Title == "" {
			t.Fatalf("area without identity: %+v", area)
		}
		if _, dup := byID[area.ID]; dup {
			t.Fatalf("duplicate area %q", area.ID)
		}
		byID[area.ID] = area
		for field, value := range map[string]string{"current": area.Current, "gap": area.Gap, "contract": area.Contract} {
			if strings.TrimSpace(value) == "" {
				t.Fatalf("area %q leaves %s unanswered", area.ID, field)
			}
		}
	}
	for _, want := range wantAreas {
		if _, ok := byID[want]; !ok {
			t.Fatalf("record omits GREEN area %q", want)
		}
	}

	// The record cross-checks the reconnect vocabulary it cites: these
	// identifiers must keep existing or the record is stale.
	_ = DefaultMaxMessageBytes
	_ = EventReconnected
	_ = EventReconnectErr
	_ = EventCaughtUp
	_ = ErrReconnectExhausted

	// The bounded consecutive-failure budget the record promises is real:
	// a permanently dead server exhausts instead of retrying forever.
	subject := testSubject("00000000-0000-4000-8000-000000000d05")
	client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	errDead := errors.New("durable005: server is dead")
	done := make(chan error, 1)
	go func() {
		done <- client.RunReconnect(context.Background(), func(context.Context, Cursor) (CloseStream, error) {
			return nil, errDead
		}, func(context.Context, CatchUpRequest) (CatchUpResult, error) {
			return CatchUpResult{}, nil
		}, ReconnectOptions{MaxAttempts: 2, InitialBackoff: time.Nanosecond, MaxBackoff: time.Nanosecond})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrReconnectExhausted) {
			t.Fatalf("dead server ended with %v, want exhaustion", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reconnect against a dead server never exhausted its budget")
	}

	text := RenderDurableCrashUX()
	for _, want := range []string{
		"Reconnecting", "backoff", "checkpoint", "catch-up", "cursor",
		"draft", "PutSidebar", "session", "idempotency", "DURABLE-004",
		"watch", "reload", "queued",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered record omits %q", want)
		}
	}
}

// TestTodo_DURABLE_005_Golden pins the rendered record bytes.
func TestTodo_DURABLE_005_Golden(t *testing.T) {
	const golden = "testdata/durable005_crashux.golden"
	want, err := os.ReadFile(filepath.Clean(golden))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got := RenderDurableCrashUX(); got != string(want) {
		t.Fatalf("rendered record differs from %s (%d bytes vs %d)", golden, len(got), len(want))
	}
}

// TestTodo_DURABLE_005_Browser drives a real page-side subscription through
// a scripted server outage: two failed generations (the kill window), then a
// recovery generation whose first hint jumps past the outage. It proves the
// person-visible properties for a mention answer, a task and an announcement
// post: resubscription resumes from the stored checkpoint, the gap is
// authoritatively caught up before hints are admitted, every on-screen
// subject reaches its true current state exactly once, and the outage never
// surfaces as a data error.
//
// Boundary, stated plainly: DURABLE-004's kill-the-real-server harness does
// not exist yet, so this test kills the transport instead of the process.
// The record names DURABLE-004 as the follow-up that replays these same
// assertions against a real kill with a page open.
func TestTodo_DURABLE_005_Browser(t *testing.T) {
	mention := testSubject("00000000-0000-4000-8000-000000000d11")
	task := testSubject("00000000-0000-4000-8000-000000000d12")
	announcement := testSubject("00000000-0000-4000-8000-000000000d13")

	var mu sync.Mutex
	var factoryCursors []uint64
	var catchUps []CatchUpRequest
	var refetches []Refresh
	var events []Event

	client, err := New(testScope(mention, task, announcement), func(_ context.Context, refresh Refresh) error {
		mu.Lock()
		refetches = append(refetches, refresh)
		mu.Unlock()
		return nil
	}, Options{Observe: func(event Event) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}

	errDead := errors.New("durable005: server killed")
	var attempts int
	done := make(chan error, 1)
	go func() {
		done <- client.RunReconnect(context.Background(), func(_ context.Context, cursor Cursor) (CloseStream, error) {
			mu.Lock()
			attempts++
			n := attempts
			factoryCursors = append(factoryCursors, cursor.Sequence())
			mu.Unlock()
			switch n {
			case 1, 2:
				return nil, errDead
			case 3:
				return &sliceStream{values: [][]byte{testMessage(t, 12, mention, task, announcement)}}, nil
			default:
				return newBlockingStream(), nil
			}
		}, func(_ context.Context, request CatchUpRequest) (CatchUpResult, error) {
			mu.Lock()
			catchUps = append(catchUps, request)
			mu.Unlock()
			return CatchUpResult{SourceSequence: request.ToSequence, Watermark: request.ToSequence}, nil
		}, ReconnectOptions{MaxAttempts: 8, InitialBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond})
	}()

	// The refetch callback fires before the client commits its counters, so
	// readiness waits for the committed snapshot, not the callback.
	waitSnapshot(t, client, func(snapshot Snapshot) bool {
		return snapshot.Refetched >= 1 && snapshot.LastSourceSequence == 12
	})
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("terminal result = %v, want context cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reconnect did not shut down")
	}

	// Event dispatch is asynchronous: wait for the kill-window and recovery
	// signals instead of asserting on whatever has arrived so far.
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		var errs, recovered int
		for _, event := range events {
			switch event.Kind {
			case EventReconnectErr:
				errs++
			case EventReconnected:
				recovered++
			}
		}
		mu.Unlock()
		if errs > 0 && recovered > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("kill-window and recovery signals never both arrived")
		}
		time.Sleep(time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(factoryCursors) < 3 {
		t.Fatalf("factory saw %d attempts, want the kill window plus recovery", len(factoryCursors))
	}
	for i, cursor := range factoryCursors[:3] {
		if cursor != 10 {
			t.Fatalf("attempt %d resubscribed from sequence %d, want the stored checkpoint 10", i+1, cursor)
		}
	}
	if len(catchUps) != 1 {
		t.Fatalf("catch-up ran %d times, want exactly the outage gap", len(catchUps))
	}
	if catchUps[0].From.Sequence() != 10 || catchUps[0].ToSequence != 11 {
		t.Fatalf("catch-up request = from %d to %d, want the missing range 10->11 before hint 12 is admitted",
			catchUps[0].From.Sequence(), catchUps[0].ToSequence)
	}
	seq12 := 0
	seen := map[string]bool{}
	for _, refresh := range refetches {
		if refresh.SourceSequence != 12 {
			t.Fatalf("refetch at sequence %d, want only the post-outage state 12", refresh.SourceSequence)
		}
		seq12++
		for _, subject := range refresh.Subjects {
			seen[subject.String()] = true
		}
	}
	if seq12 != 1 {
		t.Fatalf("sequence 12 refetched %d times, want exactly once", seq12)
	}
	for _, subject := range []string{mention.String(), task.String(), announcement.String()} {
		if !seen[subject] {
			t.Fatalf("subject %s never reached its true current state", subject)
		}
	}
	var reconnectErrs, reconnected int
	for _, event := range events {
		switch event.Kind {
		case EventReconnectErr:
			reconnectErrs++
		case EventReconnected:
			reconnected++
		case EventRefetchErr, EventQueueFull, EventCatchUpErr:
			t.Fatalf("outage surfaced as data error %q; a restart must never error the view", event.Kind)
		}
	}
	if reconnectErrs == 0 {
		t.Fatal("kill window left no reconnect signal")
	}
	if reconnected == 0 {
		t.Fatal("recovery left no reconnected signal")
	}
	snapshot := client.Snapshot()
	if snapshot.CatchUps != 1 || snapshot.Refetched < 1 || snapshot.LastSourceSequence != 12 {
		t.Fatalf("snapshot = %+v, want one catch-up and the committed post-outage state", snapshot)
	}
	if snapshot.RefetchErrors != 0 || snapshot.QueueFull != 0 || snapshot.CatchUpErrors != 0 {
		t.Fatalf("snapshot = %+v, want zero data errors across the outage", snapshot)
	}
}
