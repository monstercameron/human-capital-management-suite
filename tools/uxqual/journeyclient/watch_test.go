package journeyclient

import (
	"context"
	"io"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestQuietWatchRolloverPreservesFailureBudgetForRealOutages(t *testing.T) {
	for _, err := range []error{io.EOF, context.DeadlineExceeded, status.Error(codes.DeadlineExceeded, "deadline")} {
		if !quietWatchRollover(err, 30*time.Second) {
			t.Fatalf("quiet transport rotation counted as outage: %v", err)
		}
		if quietWatchRollover(err, time.Millisecond) {
			t.Fatalf("immediate failure bypassed retry budget: %v", err)
		}
	}
	for _, err := range []error{nil, context.Canceled, status.Error(codes.Unavailable, "offline"), status.Error(codes.PermissionDenied, "denied")} {
		if quietWatchRollover(err, time.Minute) {
			t.Fatalf("non-rotation error masked: %v", err)
		}
	}
}

// openWatchedJourney puts the client on one journey's detail route with its
// change feed running, and returns the first stream.
func openWatchedJourney(t *testing.T, h *harness) *fakeStream {
	t.Helper()
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
	h.app.Start(context.Background(), DetailHref(testIntentID))
	h.awaitPage(t, "the detail", detailShown)
	return h.svc.awaitStream(t, 1)
}

// TestWatchRedrawsThePageWhenTheEngineChanges is the whole point of the
// stream: the reader is looking at a journey somebody else decides, and the
// page follows without a reload.
func TestWatchRedrawsThePageWhenTheEngineChanges(t *testing.T) {
	h := newHarness(t)
	stream := openWatchedJourney(t, h)
	h.app.show(&journey.Notice{Tone: toneSuccess, Title: "Approvals complete", Detail: "Waiting for the effective date"})

	completed := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	completed.DetailDigest = "sha256:bbbb2222"
	stream.updates <- completed

	p := h.awaitPage(t, "the completed journey", func(p journey.Page) bool {
		return p.Detail != nil && p.Detail.Journey.Stage == stageCompleted
	})
	if p.Detail.Ledger == nil {
		t.Error("the update did not bring the ledger fact with it")
	}
	if p.Notice == nil || p.Notice.Title != "Promotion recorded" {
		t.Fatal("terminal update retained a stale waiting notice")
	}
	for _, a := range p.Detail.Actions {
		if a.ID == ActionApprove || a.ID == ActionReject || a.ID == ActionExecute {
			t.Errorf("the completed journey still offers a decision after the update: %+v", a)
		}
		if !a.Disabled {
			t.Errorf("action %q is not disabled on the completed journey after the update: %+v", a.ID, a)
		}
	}
}

// TestWatchReopensCarryingTheDigestItHolds covers the server's stream
// ceiling: the stream ends with OK, and the client opens another one saying
// what it already has so it is not re-sent.
func TestWatchReopensCarryingTheDigestItHolds(t *testing.T) {
	h := newHarness(t)
	stream := openWatchedJourney(t, h)

	updated := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	updated.DetailDigest = "sha256:cccc3333"
	stream.updates <- updated
	h.awaitPage(t, "the update", func(p journey.Page) bool {
		return p.Detail != nil && p.Detail.Journey.Stage == stageCompleted
	})

	stream.finish()

	second := h.svc.awaitStream(t, 2)
	if got, want := second.req.GetSinceDigest(), "sha256:cccc3333"; got != want {
		t.Errorf("the re-opened stream's since_digest = %q, want the digest the client holds (%q)", got, want)
	}
	if got := second.req.GetIntentId(); got != testIntentID {
		t.Errorf("the re-opened stream asked for %q", got)
	}
}

// TestWatchKeepsTheNoticeTheReaderIsLookingAt: an approval writes a success
// notice, and the same approval then arrives down the stream. The update
// must not wipe the sentence explaining what just happened.
func TestWatchKeepsTheNoticeTheReaderIsLookingAt(t *testing.T) {
	h := newHarness(t)
	h.svc.decided = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	stream := openWatchedJourney(t, h)

	h.app.Submit(ActionApprove, map[string]string{NameDecisionReason: "Approved."})
	h.awaitPage(t, "the approval", noticeTitled("Promotion recorded"))

	// The streamed detail is made distinguishable from the one the decision
	// already applied, so waiting for it proves the update landed rather
	// than merely that the decision did.
	streamed := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	streamed.DetailDigest = "sha256:dddd4444"
	streamed.Journey.WorkerName = "Omar Reyes (corrected)"
	stream.updates <- streamed

	p := h.awaitPage(t, "the streamed update", func(p journey.Page) bool {
		return p.Detail != nil && p.Detail.Journey.WorkerName == "Omar Reyes (corrected)"
	})
	if p.Notice == nil || p.Notice.Title != "Promotion recorded" {
		t.Errorf("notice = %+v after a live update, want the approval kept", p.Notice)
	}
}

// TestWatchStopsWithTheRoute: leaving a journey ends its stream, and no
// further update from it can redraw the page.
func TestWatchStopsWithTheRoute(t *testing.T) {
	h := newHarness(t)
	h.svc.list = []*journeyv1.Journey{testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)}
	stream := openWatchedJourney(t, h)

	h.app.OnHashChange(ListHref())
	h.awaitPage(t, "the list", listLoaded)

	select {
	case <-stream.ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the stream was not cancelled when the route changed")
	}

	// An update pushed into the abandoned stream must not reach the page.
	stream.updates <- testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	time.Sleep(20 * time.Millisecond)
	if p := h.store.Page(); p.Detail != nil {
		t.Error("an abandoned journey's update redrew the page the reader had moved to")
	}
}

// TestWatchStopsAfterFruitlessReopens keeps a cell that closes every stream
// immediately from becoming a reconnect loop, and makes the page say that it
// is no longer live rather than quietly going stale.
func TestWatchStopsAfterFruitlessReopens(t *testing.T) {
	h := newHarness(t)
	h.svc.instantEOF = true
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)

	h.app.Start(context.Background(), DetailHref(testIntentID))

	p := h.awaitPage(t, "the live-updates warning", noticeTitled("Live updates stopped"))
	if p.Notice.Tone != toneWarning {
		t.Errorf("notice tone = %q, want warning", p.Notice.Tone)
	}
	h.svc.mu.Lock()
	opened := len(h.svc.streams)
	h.svc.mu.Unlock()
	if opened != maxWatchAttempts {
		t.Errorf("streams opened = %d, want %d before giving up", opened, maxWatchAttempts)
	}
	// The journey the reader is looking at is still on screen: only the
	// following of it stopped.
	if p.Detail == nil || p.Detail.Journey.IntentID != testIntentID {
		t.Error("giving up on the stream took the journey off the page")
	}
}

// TestWatchSurfacesARefusal: a stream the engine refuses is the same refusal
// InspectJourney would have given, so it is worth showing.
func TestWatchSurfacesARefusal(t *testing.T) {
	h := newHarness(t)
	h.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL)
	h.svc.watchErr = status.Error(codes.PermissionDenied, "the caller may not watch this journey")

	h.app.Start(context.Background(), DetailHref(testIntentID))

	p := h.awaitPage(t, "the refusal", noticeTitled("You can't complete this action"))
	if p.Notice.Tone != toneDanger {
		t.Errorf("notice tone = %q, want danger", p.Notice.Tone)
	}
	if h.svc.called("WatchJourney") != maxWatchAttempts {
		t.Errorf("WatchJourney attempted %d times, want %d", h.svc.called("WatchJourney"), maxWatchAttempts)
	}
}

func TestStartWatchDoesNothingWithoutAJourney(t *testing.T) {
	h := newHarness(t)
	h.app.startWatch(0, "", "")
	time.Sleep(10 * time.Millisecond)
	if h.svc.called("WatchJourney") != 0 {
		t.Error("a watch was opened for no journey at all")
	}
}

func TestSleepUntil(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !sleepUntil(ctx, 0) {
		t.Error("a zero wait on a live context reported the context ended")
	}
	if !sleepUntil(ctx, time.Millisecond) {
		t.Error("a short wait on a live context reported the context ended")
	}

	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if sleepUntil(cancelled, 0) {
		t.Error("a zero wait on a cancelled context reported it could continue")
	}
	if sleepUntil(cancelled, time.Hour) {
		t.Error("a long wait on a cancelled context did not return immediately")
	}
}
