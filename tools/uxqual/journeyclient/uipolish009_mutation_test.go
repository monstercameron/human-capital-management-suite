package journeyclient

import (
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// TestTodo_UIPOLISH_009_StandaloneDuplicateExecuteIsOneMutation proves the
// native/standalone composition path, which has no bounded Tasks scheduler,
// still treats a double click as one in-flight mutation. The fake RPC is
// deliberately blocked so the assertion observes the overlap rather than
// merely counting two calls that happened to finish in sequence.
func TestTodo_UIPOLISH_009_StandaloneDuplicateExecuteIsOneMutation(t *testing.T) {
	svc := newFakeService()
	svc.executed = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	svc.gate = make(chan struct{})
	store := journey.NewStore(journey.Page{})
	app := New(testConfig(), svc, store, func() time.Time {
		return time.Date(2026, 9, 13, 14, 0, 0, 0, time.UTC)
	})
	// Record scheduling synchronously, while retaining a real goroutine for
	// the blocked RPC. Completion is recorded separately so the test can wait
	// for the whole first task before making its final RPC-count assertion.
	scheduled := make(chan struct{}, 2)
	completed := make(chan struct{}, 2)
	app.Async = func(work func()) {
		scheduled <- struct{}{}
		go func() {
			work()
			completed <- struct{}{}
		}()
	}
	app.route = Route{Kind: RouteDetail, IntentID: testIntentID}
	app.generation = 1

	app.Submit(ActionExecute, nil)
	select {
	case <-scheduled:
	case <-time.After(2 * time.Second):
		t.Fatal("first ExecuteJourney was not scheduled")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && svc.called("ExecuteJourney") == 0 {
		time.Sleep(time.Millisecond)
	}
	if svc.called("ExecuteJourney") != 1 {
		t.Fatalf("first ExecuteJourney did not enter the blocked RPC; calls=%d", svc.called("ExecuteJourney"))
	}

	secondSubmitDone := make(chan struct{})
	go func() {
		app.Submit(ActionExecute, nil)
		close(secondSubmitDone)
	}()
	select {
	case <-secondSubmitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("duplicate standalone submission did not return")
	}
	select {
	case <-scheduled:
		t.Fatal("duplicate standalone submission scheduled a second mutation")
	default:
	}
	if got := svc.called("ExecuteJourney"); got != 1 {
		t.Fatalf("duplicate standalone submission started %d ExecuteJourney RPCs while the first was blocked, want 1", got)
	}

	close(svc.gate)
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked ExecuteJourney did not complete")
	}
	if got := svc.called("ExecuteJourney"); got != 1 {
		t.Fatalf("standalone duplicate submission completed %d ExecuteJourney RPCs, want 1", got)
	}
	page := store.Page()
	if page.Notice == nil || page.Notice.Title != "Approval process started" {
		t.Fatalf("blocked ExecuteJourney did not publish success; page=%+v", page)
	}
}
