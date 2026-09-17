package schedopt

import (
	"errors"
	"sync"
	"testing"
	"time"
)

var schedopt006At = time.Date(2026, 3, 7, 9, 0, 0, 0, time.UTC)

func schedopt006Schedule() CandidateSchedule {
	return CandidateSchedule{
		Tenant: "acme", Revision: "candidate-9", RuleDigest: "sha256:rules-4",
		Assignments: []ReviewAssignment{
			{AssignmentID: "a-1", WorkerRef: "worker-1", DemandRef: "demand-er", WindowRef: "window-mon-am"},
			{AssignmentID: "a-2", WorkerRef: "worker-2", DemandRef: "demand-er", WindowRef: "window-mon-am"},
		},
		Rules: []ReviewRule{
			{Kind: "MAX_PER_WINDOW", WindowRef: "window-mon-am", Max: 3},
			{Kind: "BANNED_PAIR", WorkerRef: "worker-9", DemandRef: "demand-er"},
		},
	}
}

// TestTodo_SCHED_OPT_006 is the PRIMARY contract: manual changes are
// revalidated against hard constraints with reason and authority
// recorded, and approval binds the exact schedule revision.
func TestTodo_SCHED_OPT_006(t *testing.T) {
	changes := []ManualChange{
		{Kind: ChangeAdd, AssignmentID: "a-3", WorkerRef: "worker-3", DemandRef: "demand-er", WindowRef: "window-mon-am", Reason: "cover surge", AuthorityRef: "scheduler:maya"},
	}
	got, err := ApplyReview(schedopt006Schedule(), changes, "scheduler:maya", schedopt006At)
	if err != nil {
		t.Fatalf("ApplyReview: %v", err)
	}
	if len(got.Assignments) != 3 || got.BoundDigest == "" {
		t.Fatalf("approved schedule must hold all assignments and seal: %+v", got)
	}
	if got.Revision != "candidate-9" || got.RuleDigest != "sha256:rules-4" {
		t.Fatalf("approval must bind the exact revision: %+v", got)
	}

	t.Run("double booking is revalidated out", func(t *testing.T) {
		bad := []ManualChange{
			{Kind: ChangeMove, AssignmentID: "a-2", WorkerRef: "worker-1", WindowRef: "window-mon-am", Reason: "prefer senior", AuthorityRef: "scheduler:maya"},
		}
		if _, err := ApplyReview(schedopt006Schedule(), bad, "scheduler:maya", schedopt006At); !errors.Is(err, ErrReviewRejected) {
			t.Fatalf("double booking must be SCHED_OPT_006_REJECTED, got %v", err)
		}
	})

	t.Run("banned pair and over-capacity hold", func(t *testing.T) {
		banned := []ManualChange{
			{Kind: ChangeAdd, AssignmentID: "a-9", WorkerRef: "worker-9", DemandRef: "demand-er", WindowRef: "window-mon-pm", Reason: "override", AuthorityRef: "scheduler:maya"},
		}
		if _, err := ApplyReview(schedopt006Schedule(), banned, "scheduler:maya", schedopt006At); !errors.Is(err, ErrReviewRejected) {
			t.Fatalf("banned pair must be SCHED_OPT_006_REJECTED")
		}
		full := schedopt006Schedule()
		full.Rules = []ReviewRule{{Kind: "MAX_PER_WINDOW", WindowRef: "window-mon-am", Max: 1}}
		if _, err := ApplyReview(full, nil, "scheduler:maya", schedopt006At); !errors.Is(err, ErrReviewRejected) {
			t.Fatalf("over-capacity candidate must be SCHED_OPT_006_REJECTED")
		}
	})

	t.Run("reason and authority are mandatory", func(t *testing.T) {
		noreason := []ManualChange{
			{Kind: ChangeRemove, AssignmentID: "a-1", AuthorityRef: "scheduler:maya"},
		}
		if _, err := ApplyReview(schedopt006Schedule(), noreason, "scheduler:maya", schedopt006At); !errors.Is(err, ErrReviewRejected) {
			t.Fatalf("reason-free change must be SCHED_OPT_006_REJECTED")
		}
		noauth := []ManualChange{
			{Kind: ChangeRemove, AssignmentID: "a-1", Reason: "duplicate"},
		}
		var rej *ReviewRejection
		_, err := ApplyReview(schedopt006Schedule(), noauth, "scheduler:maya", schedopt006At)
		if !errors.As(err, &rej) || rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("authority-free change must name field/state/version: %v", err)
		}
	})
}

func TestTodo_SCHED_OPT_006_Property(t *testing.T) {
	a, err := ApplyReview(schedopt006Schedule(), nil, "scheduler:maya", schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ApplyReview(schedopt006Schedule(), nil, "scheduler:maya", schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	if a.BoundDigest != b.BoundDigest {
		t.Fatalf("identical reviews must bind identically")
	}
	// Change order across independent assignments converges.
	first := []ManualChange{
		{Kind: ChangeAdd, AssignmentID: "a-3", WorkerRef: "worker-3", DemandRef: "demand-er", WindowRef: "window-mon-pm", Reason: "r1", AuthorityRef: "scheduler:maya"},
		{Kind: ChangeAdd, AssignmentID: "a-4", WorkerRef: "worker-4", DemandRef: "demand-er", WindowRef: "window-mon-pm", Reason: "r2", AuthorityRef: "scheduler:maya"},
	}
	second := []ManualChange{first[1], first[0]}
	c, err := ApplyReview(schedopt006Schedule(), first, "scheduler:maya", schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	d, err := ApplyReview(schedopt006Schedule(), second, "scheduler:maya", schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	// Digests bind the change reasons, so orders differ textually; both
	// must still hold the same four assignments.
	if len(c.Assignments) != 4 || len(d.Assignments) != 4 {
		t.Fatalf("both orders must converge on four assignments: %+v / %+v", c, d)
	}
}

func TestTodo_SCHED_OPT_006_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ApplyReview(schedopt006Schedule(), nil, "scheduler:maya", schedopt006At)
			if err != nil {
				t.Error(err)
				return
			}
			if len(got.Assignments) != 2 {
				t.Errorf("concurrent review diverged: %+v", got)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_SCHED_OPT_006_Fault(t *testing.T) {
	ghost := []ManualChange{
		{Kind: ChangeRemove, AssignmentID: "a-404", Reason: "cleanup", AuthorityRef: "scheduler:maya"},
	}
	if _, err := ApplyReview(schedopt006Schedule(), ghost, "scheduler:maya", schedopt006At); !errors.Is(err, ErrReviewRejected) {
		t.Fatalf("removing a ghost must be SCHED_OPT_006_REJECTED")
	}
	dupe := []ManualChange{
		{Kind: ChangeAdd, AssignmentID: "a-1", WorkerRef: "worker-3", DemandRef: "demand-er", WindowRef: "window-mon-pm", Reason: "dup", AuthorityRef: "scheduler:maya"},
	}
	if _, err := ApplyReview(schedopt006Schedule(), dupe, "scheduler:maya", schedopt006At); !errors.Is(err, ErrReviewRejected) {
		t.Fatalf("duplicate add must be SCHED_OPT_006_REJECTED")
	}
	weird := schedopt006Schedule()
	weird.Rules = []ReviewRule{{Kind: "PREFERENCE", WindowRef: "window-mon-am", Max: 9}}
	if _, err := ApplyReview(weird, nil, "scheduler:maya", schedopt006At); !errors.Is(err, ErrReviewRejected) {
		t.Fatalf("undeclared rule kind must be SCHED_OPT_006_REJECTED")
	}
	if _, err := ApplyReview(schedopt006Schedule(), nil, "", schedopt006At); !errors.Is(err, ErrReviewRejected) {
		t.Fatalf("approver-free review must be SCHED_OPT_006_REJECTED")
	}
}

func TestTodo_SCHED_OPT_006_Mutation(t *testing.T) {
	a, err := ApplyReview(schedopt006Schedule(), nil, "scheduler:maya", schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	mutated := a
	mutated.Assignments = append(append([]ReviewAssignment(nil), a.Assignments...), ReviewAssignment{AssignmentID: "a-x", WorkerRef: "worker-x", DemandRef: "demand-er", WindowRef: "window-mon-am"})
	if mutated.computedDigest() == a.BoundDigest {
		t.Fatalf("assignment mutation must move the bound digest")
	}
	schedule := schedopt006Schedule()
	schedule.Revision = "candidate-10"
	b, err := ApplyReview(schedule, nil, "scheduler:maya", schedopt006At)
	if err != nil {
		t.Fatal(err)
	}
	if b.BoundDigest == a.BoundDigest {
		t.Fatalf("revision change must move the bound digest")
	}
}
