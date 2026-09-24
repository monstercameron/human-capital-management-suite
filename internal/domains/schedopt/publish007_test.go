package schedopt

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func schedopt007Approved(t *testing.T) ApprovedSchedule {
	t.Helper()
	got, err := ApplyReview(schedopt006Schedule(), nil, "scheduler:maya", schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestTodo_SCHED_OPT_007 is the PRIMARY contract: publication creates
// governed assignment, message and integration effects exactly once, and
// observed coverage and failures stay multidimensional and repairable.
func TestTodo_SCHED_OPT_007(t *testing.T) {
	approved := schedopt007Approved(t)
	pub, err := PublishSchedule(approved, "idem-pub-001", map[string]string{}, schedopt006At)
	if err != nil {
		t.Fatalf("PublishSchedule: %v", err)
	}
	if len(pub.Assignments) != 2 || len(pub.Messages) != 2 || len(pub.IntegrationEffects) != 2 {
		t.Fatalf("publication must describe every effect once: %+v", pub)
	}
	if pub.Digest == "" {
		t.Fatalf("publication must seal a digest")
	}

	t.Run("same approval and key republishes identically", func(t *testing.T) {
		seen := map[string]string{"idem-pub-001": approved.BoundDigest}
		again, err := PublishSchedule(approved, "idem-pub-001", seen, schedopt006At)
		if err != nil {
			t.Fatal(err)
		}
		if again.Digest != pub.Digest {
			t.Fatalf("idempotent republish must agree: %+v", again)
		}
		changed, err := ApplyPrepublicationReview(schedopt006Schedule(), []ManualChange{
			{Kind: ChangeRemove, AssignmentID: "a-2", Reason: "sick", AuthorityRef: "scheduler:maya"},
		}, "scheduler:maya", schedopt006At)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := PublishSchedule(changed, "idem-pub-001", seen, schedopt006At); !errors.Is(err, ErrPublishRejected) {
			t.Fatalf("reused key with changed approval must be SCHED_OPT_007_REJECTED, got %v", err)
		}
	})

	t.Run("coverage reconciles multidimensionally", func(t *testing.T) {
		res, err := ReconcileSchedule(pub, []ObservedCoverage{
			{WorkerRef: "worker-1", WindowRef: "window-mon-am", DemandRef: "demand-er", Channel: "roster", State: "COVERED"},
			{WorkerRef: "worker-2", WindowRef: "window-mon-am", DemandRef: "demand-er", Channel: "roster", State: "MISSING"},
			{WorkerRef: "worker-2", WindowRef: "window-mon-am", DemandRef: "demand-er", Channel: "payroll-bridge", State: "FAILED"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Matched != 1 {
			t.Fatalf("one assignment must match: %+v", res)
		}
		dimensions := map[string]bool{}
		for _, f := range res.Failures {
			dimensions[f.Dimension] = true
			if f.Repair == "" {
				t.Fatalf("every failure needs a repair: %+v", f)
			}
		}
		if !dimensions["assignment"] || !dimensions["channel"] {
			t.Fatalf("failures must stay dimensional: %+v", res.Failures)
		}
		if len(res.Repair) != len(res.Failures) || res.Digest == "" {
			t.Fatalf("repairs must cover every failure with a digest: %+v", res)
		}
	})

	t.Run("unbound approvals never publish", func(t *testing.T) {
		bad := schedopt007Approved(t)
		bad.BoundDigest = "sha256:forged"
		if _, err := PublishSchedule(bad, "idem-pub-x", map[string]string{}, schedopt006At); !errors.Is(err, ErrPublishRejected) {
			t.Fatalf("unbound approval must be SCHED_OPT_007_REJECTED")
		}
	})
}

func TestTodo_SCHED_OPT_007_Property(t *testing.T) {
	approved := schedopt007Approved(t)
	a, err := PublishSchedule(approved, "idem-pub-001", map[string]string{}, schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	b, err := PublishSchedule(approved, "idem-pub-002", map[string]string{}, schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest == b.Digest {
		t.Fatalf("distinct idempotency keys must seal distinctly")
	}
	// Full observation converges to zero failures.
	res, err := ReconcileSchedule(a, []ObservedCoverage{
		{WorkerRef: "worker-1", WindowRef: "window-mon-am", DemandRef: "demand-er", Channel: "roster", State: "COVERED"},
		{WorkerRef: "worker-2", WindowRef: "window-mon-am", DemandRef: "demand-er", Channel: "roster", State: "COVERED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 2 || len(res.Failures) != 0 {
		t.Fatalf("full coverage must match all: %+v", res)
	}
}

func TestTodo_SCHED_OPT_007_Race(t *testing.T) {
	approved := schedopt007Approved(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pub, err := PublishSchedule(approved, "idem-pub-001", map[string]string{}, schedopt006At)
			if err != nil {
				t.Error(err)
				return
			}
			if len(pub.Messages) != 2 {
				t.Errorf("concurrent publish diverged: %+v", pub)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_SCHED_OPT_007_Fault(t *testing.T) {
	approved := schedopt007Approved(t)
	if _, err := PublishSchedule(approved, "", map[string]string{}, schedopt006At); !errors.Is(err, ErrPublishRejected) {
		t.Fatalf("keyless publish must be SCHED_OPT_007_REJECTED")
	}
	empty := approved
	empty.Assignments = nil
	empty.BoundDigest = empty.computedDigest()
	if _, err := PublishSchedule(empty, "idem-empty", map[string]string{}, schedopt006At); !errors.Is(err, ErrPublishRejected) {
		t.Fatalf("empty schedule must be SCHED_OPT_007_REJECTED")
	}
	pub, err := PublishSchedule(approved, "idem-pub-001", map[string]string{}, schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileSchedule(pub, nil); !errors.Is(err, ErrPublishRejected) {
		t.Fatalf("observation-free reconcile must be SCHED_OPT_007_REJECTED")
	}
	badObs := []ObservedCoverage{{WorkerRef: "worker-1", WindowRef: "window-mon-am", DemandRef: "demand-er", Channel: "roster", State: "MAYBE"}}
	if _, err := ReconcileSchedule(pub, badObs); !errors.Is(err, ErrPublishRejected) {
		t.Fatalf("undeclared state must be SCHED_OPT_007_REJECTED")
	}
}

func TestTodo_SCHED_OPT_007_Mutation(t *testing.T) {
	approved := schedopt007Approved(t)
	pub, err := PublishSchedule(approved, "idem-pub-001", map[string]string{}, schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	mutated := pub
	mutated.Messages = append(append([]string(nil), pub.Messages...), "notify:worker-9:ghost")
	if mutated.computedDigest() == pub.Digest {
		t.Fatalf("effect mutation must move the digest")
	}
	// UNKNOWN channels degrade without failing the matched assignment.
	res, err := ReconcileSchedule(pub, []ObservedCoverage{
		{WorkerRef: "worker-1", WindowRef: "window-mon-am", DemandRef: "demand-er", Channel: "roster", State: "COVERED"},
		{WorkerRef: "worker-2", WindowRef: "window-mon-am", DemandRef: "demand-er", Channel: "roster", State: "COVERED"},
		{WorkerRef: "worker-1", WindowRef: "window-mon-am", DemandRef: "demand-er", Channel: "payroll-bridge", State: "UNKNOWN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 2 || len(res.Failures) != 1 || res.Failures[0].Dimension != "channel" {
		t.Fatalf("unknown channel must degrade dimensionally: %+v", res)
	}
	_ = time.Now
}
