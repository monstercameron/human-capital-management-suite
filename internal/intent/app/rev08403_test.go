package app

import (
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func rev08403Action() AcceptedAction {
	action := align030Action()
	action.ActionID = "promotion.submit"
	return action
}

// TestTodo_REV_084_03 exercises semantic replay through the composed product
// application service, the entrypoint used after the work loop accepts an
// action.
func TestTodo_REV_084_03(t *testing.T) {
	service := newLifecycleHarness(t).Service
	action := rev08403Action()

	first, outcome, err := service.submitAcceptedProductAction(action)
	if err != nil {
		t.Fatalf("submitAcceptedProductAction: %v", err)
	}
	if outcome != SubmissionAccepted {
		t.Fatalf("first outcome = %s, want ACCEPTED", outcome)
	}

	// The accepted timestamp is receipt metadata rather than semantic content.
	// A retry with the same accepted action must return the first stored record.
	retry := action
	retry.AcceptedAt = values.NewInstant(align030Now.Time().Add(time.Second))
	second, outcome, err := service.submitAcceptedProductAction(retry)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if outcome != SubmissionReplayed {
		t.Fatalf("retry outcome = %s, want REPLAYED", outcome)
	}
	if second != first {
		t.Fatalf("retry returned %+v, want original record %+v", second, first)
	}
}

// TestTodo_REV_084_03_Race proves concurrent duplicates at the product
// application entrypoint admit exactly one record and all callers observe it.
func TestTodo_REV_084_03_Race(t *testing.T) {
	service := newLifecycleHarness(t).Service
	const callers = 24
	type result struct {
		record  SubmissionRecord
		outcome SubmissionOutcome
		err     error
	}
	results := make([]result, callers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i].record, results[i].outcome, results[i].err = service.submitAcceptedProductAction(rev08403Action())
		}(i)
	}
	wg.Wait()

	accepted := 0
	var first SubmissionRecord
	for i, got := range results {
		if got.err != nil {
			t.Fatalf("caller %d: %v", i, got.err)
		}
		if got.outcome == SubmissionAccepted {
			accepted++
		} else if got.outcome != SubmissionReplayed {
			t.Fatalf("caller %d outcome = %s, want ACCEPTED or REPLAYED", i, got.outcome)
		}
		if i == 0 {
			first = got.record
		} else if got.record != first {
			t.Fatalf("caller %d received %+v, want the shared record %+v", i, got.record, first)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted submissions = %d, want exactly 1", accepted)
	}
}
