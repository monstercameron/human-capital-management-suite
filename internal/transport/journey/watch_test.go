package journey_test

import (
	"context"
	"errors"
	"io"
	"runtime"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// testPollInterval is the interval the watch tests compose the service with.
// It is short so a change is observed in milliseconds rather than in the
// production beat; the handler's behaviour is identical either way, because
// the interval is the only thing it changes.
const testPollInterval = 20 * time.Millisecond

// watchDeps returns the dependencies a watch test composes: the engine under
// test, polled fast.
func watchDeps(engine *fakeEngine) journey.Dependencies {
	return journey.Dependencies{Engine: engine, PollInterval: testPollInterval}
}

// event is one thing that happened on a watch stream: a message, or the
// error that ended it.
type event struct {
	msg *journeyv1.WatchJourneyResponse
	err error
}

// watcher is one open WatchJourney stream, drained by exactly one goroutine.
//
// The single reader is the whole point. A gRPC client stream does not admit
// concurrent Recv calls, so a helper that spawns a fresh reader per
// assertion would have an abandoned one still holding the stream, and the
// message a test is waiting for would be delivered to nobody. One reader
// pushes everything into a channel and every assertion reads from there.
type watcher struct {
	t      *testing.T
	stream journeyv1.JourneyService_WatchJourneyClient
	events chan event
}

// openWatch opens one WatchJourney stream and starts draining it. Failures
// arrive as events rather than from this call: grpc-go returns from the
// client method before the server has said anything at all.
func openWatch(t *testing.T, ctx context.Context, client journeyv1.JourneyServiceClient, req *journeyv1.WatchJourneyRequest) *watcher {
	t.Helper()
	stream, err := client.WatchJourney(ctx, req)
	if err != nil {
		t.Fatalf("open WatchJourney: %v", err)
	}
	w := &watcher{t: t, stream: stream, events: make(chan event, 8)}
	go func() {
		defer close(w.events)
		for {
			msg, err := stream.Recv()
			w.events <- event{msg, err}
			if err != nil {
				return
			}
		}
	}()
	return w
}

// next returns the stream's next event, failing the test if none arrives
// inside d.
func (w *watcher) next(d time.Duration) (*journeyv1.WatchJourneyResponse, error) {
	w.t.Helper()
	select {
	case e, ok := <-w.events:
		if !ok {
			w.t.Fatal("the watch stream's reader ended without reporting why")
		}
		return e.msg, e.err
	case <-time.After(d):
		w.t.Fatalf("nothing arrived on the watch stream within %v", d)
		return nil, nil
	}
}

// recv returns the stream's next message, failing the test when the stream
// ended instead of delivering one.
func (w *watcher) recv(d time.Duration) *journeyv1.WatchJourneyResponse {
	w.t.Helper()
	msg, err := w.next(d)
	if err != nil {
		w.t.Fatalf("the watch stream ended instead of delivering a message: %v", err)
	}
	return msg
}

// silent asserts that nothing arrives for d. It is how "an unchanged journey
// produces no traffic" is checked, which no assertion about a message that
// did arrive can establish.
func (w *watcher) silent(d time.Duration) {
	w.t.Helper()
	select {
	case e, ok := <-w.events:
		if !ok {
			w.t.Fatal("the watch stream's reader ended while the journey was merely unchanged")
		}
		if e.err != nil {
			w.t.Fatalf("the stream ended while the journey was merely unchanged: %v", e.err)
		}
		w.t.Fatalf("the stream emitted a message for an unchanged journey: digest %q",
			e.msg.GetDetail().GetDetailDigest())
	case <-time.After(d):
	}
}

// changedDetail is the fixture detail with two fields moved, which is a real
// change to the digest rather than a re-stamp of the same bytes.
func changedDetail() workspace.JourneyDetail {
	changed := fixtureDetail()
	changed.Summary.Stage = workspace.JourneyStageCompleted
	changed.Approver = "approver-kim"
	return changed
}

// TestWatchPollIntervalIsAComposedKnobWithASaneDefault pins the one timing
// decision that moved off the wire: a composition that names no interval
// polls at the compiled-in default, and one that names an interval gets
// exactly it. Nothing a client sends can change either.
func TestWatchPollIntervalIsAComposedKnobWithASaneDefault(t *testing.T) {
	defaultPoll, maxLifetime := journey.WatchBoundsForTest()
	if defaultPoll <= 0 || maxLifetime <= 0 {
		t.Fatalf("watch bounds must be positive, got poll=%v ceiling=%v", defaultPoll, maxLifetime)
	}
	if maxLifetime != 15*time.Minute {
		t.Fatalf("the stream ceiling is %v; the proto declares fifteen minutes", maxLifetime)
	}
	if defaultPoll >= maxLifetime {
		t.Fatalf("the poll interval %v must be far under the ceiling %v", defaultPoll, maxLifetime)
	}

	cases := []struct {
		name string
		deps journey.Dependencies
		want time.Duration
	}{
		{"an unset interval gets the default", journey.Dependencies{}, defaultPoll},
		{"a zero interval gets the default", journey.Dependencies{PollInterval: 0}, defaultPoll},
		{"a negative interval gets the default", journey.Dependencies{PollInterval: -time.Second}, defaultPoll},
		{"a named interval is honoured", journey.Dependencies{PollInterval: 5 * time.Millisecond}, 5 * time.Millisecond},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := journey.EffectivePollIntervalForTest(c.deps); got != c.want {
				t.Fatalf("pollInterval = %v, want %v", got, c.want)
			}
		})
	}
}

// TestWatchJourneyEmitsTheCurrentDetailImmediately is the stream's opening
// move: a client that holds nothing is sent what the journey looks like now,
// without waiting for a change it has no way to have already seen.
func TestWatchJourneyEmitsTheCurrentDetailImmediately(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, watchDeps(engine)))

	// assertDetailIsWhole checks PROMOUX-008's diagnostics-authorized-only
	// sections, so this stream opens as the authorized fixture identity.
	watch := openWatch(t, authorizedContext(t), client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
	assertDetailIsWhole(t, watch.recv(5*time.Second).GetDetail())

	engine.mu.Lock()
	gotID := engine.lastIntentID
	engine.mu.Unlock()
	if gotID != fixtureIntentID {
		t.Fatalf("the engine saw intent id %q, want %q", gotID, fixtureIntentID)
	}
}

// TestWatchJourneySkipsTheInitialEmissionWhenTheClientIsCurrent proves
// since_digest does the one job it is left with. A client that reconnects
// holding the digest it already has is not re-sent that detail, and the
// stream stays silent until the journey actually moves.
func TestWatchJourneySkipsTheInitialEmissionWhenTheClientIsCurrent(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, watchDeps(engine)))
	ctx := testContext(t)

	// Learn the digest the way a real client does: from a first stream.
	first := openWatch(t, ctx, client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
	held := first.recv(5 * time.Second).GetDetail().GetDetailDigest()
	if held == "" {
		t.Fatal("the opening emission carries no digest")
	}

	resumed := openWatch(t, ctx, client, &journeyv1.WatchJourneyRequest{
		IntentId:    fixtureIntentID,
		SinceDigest: held,
	})
	resumed.silent(20 * testPollInterval)

	engine.setDetail(changedDetail())
	msg := resumed.recv(5 * time.Second)
	if got := msg.GetDetail().GetDetailDigest(); got == held {
		t.Fatal("the stream emitted the digest the client already held")
	}
	if got := msg.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED {
		t.Fatalf("stage = %v, want COMPLETED", got)
	}
}

// TestWatchJourneyEmitsOneMessagePerChange is the feed's whole contract: the
// opening detail, then exactly one message each time the journey moves, and
// nothing at all in between.
func TestWatchJourneyEmitsOneMessagePerChange(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, watchDeps(engine)))

	watch := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})

	first := watch.recv(5 * time.Second).GetDetail().GetDetailDigest()

	// Nothing changed yet, so nothing more may arrive.
	watch.silent(10 * testPollInterval)

	engine.setDetail(changedDetail())
	second := watch.recv(5 * time.Second)
	secondDigest := second.GetDetail().GetDetailDigest()
	if secondDigest == first {
		t.Fatal("the second message carries the digest the first already delivered")
	}
	if second.GetDetail().GetApprover() != "approver-kim" {
		t.Fatalf("approver = %q, want the changed approver-kim", second.GetDetail().GetApprover())
	}

	// A change re-applied identically is not a change.
	engine.setDetail(changedDetail())
	watch.silent(10 * testPollInterval)

	third := changedDetail()
	third.Summary.Stage = workspace.JourneyStageRejected
	engine.setDetail(third)
	last := watch.recv(5 * time.Second)
	if got := last.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED {
		t.Fatalf("stage = %v, want REJECTED", got)
	}
	if last.GetDetail().GetDetailDigest() == secondDigest {
		t.Fatal("the third message carries the digest the second already delivered")
	}
}

// TestWatchJourneyEndsOnCancelWithoutLeakingGoroutines proves the disconnect
// path: a client that cancels ends the handler, and the process settles back
// to the goroutine count it started at. The watch runs on the stream's own
// goroutine and starts none of its own, so a leak here would mean the select
// stopped observing the stream context.
func TestWatchJourneyEndsOnCancelWithoutLeakingGoroutines(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, watchDeps(engine)))

	// One warm-up stream first, so grpc-go's own per-connection transport
	// goroutines exist before the baseline is read and the delta measures
	// this handler rather than the client's first connect.
	warmup, warmupCancel := context.WithCancel(withToken(context.Background(), fixtureManagerToken))
	warm := openWatch(t, warmup, client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
	warm.recv(5 * time.Second)
	warmupCancel()
	warm.next(5 * time.Second)

	settle(t)
	before := runtime.NumGoroutine()

	const rounds = 8
	for i := range rounds {
		ctx, cancel := context.WithCancel(withToken(context.Background(), fixtureManagerToken))
		watch := openWatch(t, ctx, client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
		watch.recv(5 * time.Second)

		cancel()
		// Cancellation surfaces as a CANCELED status over a real connection
		// rather than as the context sentinel; what matters is that the
		// stream ended, not how the ending is spelled.
		if _, err := watch.next(5 * time.Second); err == nil {
			t.Fatalf("round %d: a cancelled stream delivered another message", i)
		}
	}

	settle(t)
	after := runtime.NumGoroutine()
	// A small delta is normal: grpc-go recycles its own transport workers on
	// its own schedule. A leak of one goroutine per cancelled stream would be
	// at least `rounds`, which this bound excludes.
	if after > before+rounds/2 {
		t.Fatalf("goroutines grew from %d to %d across %d cancelled streams", before, after, rounds)
	}
}

// TestWatchJourneyRefusesAnUnauthenticatedStream is the assertion the move
// to streaming had to earn. The shared boundary now chains a stream
// interceptor, so opening a watch with no credential is refused before the
// handler runs and the engine is never touched.
func TestWatchJourneyRefusesAnUnauthenticatedStream(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, watchDeps(engine)))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	watch := openWatch(t, ctx, client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
	_, err := watch.next(5 * time.Second)
	assertOwnedCode(t, err, envelope.CodeUnauthenticated)

	if engine.inspectCount() != 0 {
		t.Fatalf("the engine saw %d Inspect calls on an unauthenticated stream, want 0", engine.inspectCount())
	}
}

// TestWatchJourneyEndsTheStreamOnAnEngineRefusal proves the error mapping
// reaches the streaming path unchanged, both before the first message and
// after it: an engine that refuses ends the stream with the projected
// condition rather than with a silent close.
func TestWatchJourneyEndsTheStreamOnAnEngineRefusal(t *testing.T) {
	t.Run("on the opening read", func(t *testing.T) {
		engine := newFakeEngine()
		engine.inspectErr = workspace.ErrJourneyUnknown
		client := dialJourneyClient(startTestServer(t, watchDeps(engine)))

		watch := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
		_, err := watch.next(5 * time.Second)
		owned := assertOwnedCode(t, err, envelope.CodeNotFound)
		if owned.CorrelationID() == "" {
			t.Fatal("a stream refusal must carry a correlation id")
		}
		if owned.EvidenceRef().ID == "" {
			t.Fatal("a stream refusal must carry the authentication evidence reference")
		}
	})

	t.Run("on a later poll", func(t *testing.T) {
		engine := newFakeEngine()
		client := dialJourneyClient(startTestServer(t, watchDeps(engine)))

		watch := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
		watch.recv(5 * time.Second)

		engine.mu.Lock()
		engine.inspectErr = workspace.ErrDenied
		engine.mu.Unlock()

		_, err := watch.next(5 * time.Second)
		assertOwnedCode(t, err, envelope.CodePermissionDenied)
	})
}

// TestWatchJourneyWithoutAnEngineEndsUnavailable is the FAULT case on the
// streaming path: a process hosting the service before the engine is wired
// ends the stream with a typed UNAVAILABLE rather than panicking on a nil
// port or holding the client open forever.
func TestWatchJourneyWithoutAnEngineEndsUnavailable(t *testing.T) {
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{PollInterval: testPollInterval}))

	watch := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
	_, err := watch.next(5 * time.Second)
	assertOwnedCode(t, err, envelope.CodeUnavailable)
	if errors.Is(err, io.EOF) {
		t.Fatal("the stream closed cleanly instead of reporting the missing engine")
	}
}

// settle gives grpc-go's own goroutines a moment to retire before a
// goroutine count is read, so the assertion measures this package's
// behaviour rather than the transport's bookkeeping.
func settle(t *testing.T) {
	t.Helper()
	for range 20 {
		runtime.GC()
		time.Sleep(25 * time.Millisecond)
	}
}
