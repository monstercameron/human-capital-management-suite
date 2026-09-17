// ATTEND-004 RED: routing an attendance exception must create scoped human
// work carrying evidence, a deadline and separation-of-duties identity, and a
// correction must reevaluate the affected interval while preserving the prior
// result.
package attendance

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func attend004LateResult(t *testing.T) (Request, Result) {
	t.Helper()
	req := validRequest()
	start := req.Schedule.Shifts[0].Interval.Start
	req.Punches[0].At = start.Add(20 * time.Minute)
	res, err := Evaluate(req)
	if err != nil || res.Outcome != Exception || len(res.Exceptions) == 0 {
		t.Fatalf("setup: outcome=%s err=%v findings=%+v", res.Outcome, err, res.Exceptions)
	}
	return req, res
}

func attend004Clock(t *testing.T) (time.Time, time.Duration) {
	t.Helper()
	req := validRequest()
	start := req.Schedule.Shifts[0].Interval.Start
	return start, 72 * time.Hour
}

// TestTodo_ATTEND_004 is the PRIMARY contract: exceptions route to scoped
// human work with evidence/deadline/SoD, and resolution reevaluates the
// affected interval while preserving the prior result.
func TestTodo_ATTEND_004(t *testing.T) {
	_, res := attend004LateResult(t)
	now, ttl := attend004Clock(t)

	t.Run("route-creates-scoped-work", func(t *testing.T) {
		items, err := RouteExceptionWork(res, "worker-1", "timekeeper-1", now, ttl)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != len(res.Exceptions) {
			t.Fatalf("work items=%d findings=%d", len(items), len(res.Exceptions))
		}
		seen := map[string]struct{}{}
		for i, item := range items {
			if item.WorkID == "" || item.State != WorkOpen {
				t.Fatalf("item %d not open scoped work: %+v", i, item)
			}
			if _, dup := seen[item.WorkID]; dup {
				t.Fatalf("duplicate work id %q", item.WorkID)
			}
			seen[item.WorkID] = struct{}{}
			if !reflect.DeepEqual(item.Finding, res.Exceptions[i]) {
				t.Fatalf("item %d finding=%+v want %+v", i, item.Finding, res.Exceptions[i])
			}
			if item.EvidenceDigest != res.InputDigest || item.EvidenceDigest == "" {
				t.Fatalf("item %d evidence=%q want %q", i, item.EvidenceDigest, res.InputDigest)
			}
			if !item.Deadline.Equal(now.Add(ttl)) {
				t.Fatalf("item %d deadline=%v want %v", i, item.Deadline, now.Add(ttl))
			}
			if item.Requester != "timekeeper-1" || item.WorkerID != "worker-1" {
				t.Fatalf("item %d scope=%+v", i, item)
			}
		}
	})

	t.Run("route-refuses-compliant", func(t *testing.T) {
		clean, err := Evaluate(validRequest())
		if err != nil || clean.Outcome != Compliant {
			t.Fatalf("setup: outcome=%s err=%v", clean.Outcome, err)
		}
		if _, err := RouteExceptionWork(clean, "worker-1", "timekeeper-1", now, ttl); !errors.Is(err, ErrInvalidExceptionWork) {
			t.Fatalf("compliant routing err=%v", err)
		}
	})

	t.Run("resolve-reevaluates-and-preserves-prior", func(t *testing.T) {
		items, err := RouteExceptionWork(res, "worker-1", "timekeeper-1", now, ttl)
		if err != nil {
			t.Fatal(err)
		}
		correction := ExceptionCorrection{
			Corrected:   validRequest(),
			EvidenceRef: ref("corrections", "1"),
			Resolver:    "supervisor-2",
			ResolvedAt:  now.Add(time.Hour),
		}
		got, err := ResolveExceptionWork(items[0], res, correction)
		if err != nil {
			t.Fatal(err)
		}
		if got.PriorDigest != res.InputDigest || got.PriorOutcome != Exception {
			t.Fatalf("prior not preserved: %+v", got)
		}
		if got.Result.Outcome != Compliant || got.Result.InputDigest == "" {
			t.Fatalf("no reevaluation: %+v", got.Result)
		}
		if got.Result.InputDigest == res.InputDigest {
			t.Fatal("reevaluation digest equals prior digest")
		}
		if got.State != WorkResolved || got.Resolver != "supervisor-2" || got.WorkID != items[0].WorkID {
			t.Fatalf("resolution=%+v", got)
		}
	})

	t.Run("resolve-enforces-separation-of-duties", func(t *testing.T) {
		items, err := RouteExceptionWork(res, "worker-1", "timekeeper-1", now, ttl)
		if err != nil {
			t.Fatal(err)
		}
		correction := ExceptionCorrection{
			Corrected:   validRequest(),
			EvidenceRef: ref("corrections", "1"),
			Resolver:    "timekeeper-1",
			ResolvedAt:  now.Add(time.Hour),
		}
		if _, err := ResolveExceptionWork(items[0], res, correction); !errors.Is(err, ErrSeparationOfDuties) {
			t.Fatalf("self-approval err=%v", err)
		}
	})

	t.Run("resolve-enforces-deadline", func(t *testing.T) {
		items, err := RouteExceptionWork(res, "worker-1", "timekeeper-1", now, ttl)
		if err != nil {
			t.Fatal(err)
		}
		correction := ExceptionCorrection{
			Corrected:   validRequest(),
			EvidenceRef: ref("corrections", "1"),
			Resolver:    "supervisor-2",
			ResolvedAt:  now.Add(ttl + time.Hour),
		}
		if _, err := ResolveExceptionWork(items[0], res, correction); !errors.Is(err, ErrResolutionRejected) {
			t.Fatalf("late resolution err=%v", err)
		}
	})
}

// TestTodo_ATTEND_004_Property proves routing determinism and that a
// correction which still shows an exception stays open work with the prior
// preserved.
func TestTodo_ATTEND_004_Property(t *testing.T) {
	_, res := attend004LateResult(t)
	now, ttl := attend004Clock(t)

	first, err := RouteExceptionWork(res, "worker-1", "timekeeper-1", now, ttl)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RouteExceptionWork(res, "worker-1", "timekeeper-1", now, ttl)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("routing nondeterministic:\n%+v\n%+v", first, second)
	}

	stillLate := validRequest()
	start := stillLate.Schedule.Shifts[0].Interval.Start
	stillLate.Punches[0].At = start.Add(25 * time.Minute)
	got, err := ResolveExceptionWork(first[0], res, ExceptionCorrection{
		Corrected:   stillLate,
		EvidenceRef: ref("corrections", "2"),
		Resolver:    "supervisor-2",
		ResolvedAt:  now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Result.Outcome != Exception || got.State != WorkOpen {
		t.Fatalf("residual exception must stay open work: %+v", got)
	}
	if got.PriorDigest != res.InputDigest {
		t.Fatalf("prior not preserved: %+v", got)
	}
}

// TestTodo_ATTEND_004_Mutation proves tampered work, forged evidence, an
// uncorrected reevaluation and a mismatched prior are all rejected.
func TestTodo_ATTEND_004_Mutation(t *testing.T) {
	_, res := attend004LateResult(t)
	now, ttl := attend004Clock(t)
	items, err := RouteExceptionWork(res, "worker-1", "timekeeper-1", now, ttl)
	if err != nil {
		t.Fatal(err)
	}

	tampered := items[0]
	tampered.WorkID = ""
	if _, err := ResolveExceptionWork(tampered, res, ExceptionCorrection{
		Corrected:   validRequest(),
		EvidenceRef: ref("corrections", "1"),
		Resolver:    "supervisor-2",
		ResolvedAt:  now.Add(time.Hour),
	}); !errors.Is(err, ErrInvalidExceptionWork) {
		t.Fatalf("tampered work err=%v", err)
	}

	forged := ExceptionCorrection{
		Corrected:   validRequest(),
		EvidenceRef: VersionedRef{},
		Resolver:    "supervisor-2",
		ResolvedAt:  now.Add(time.Hour),
	}
	if _, err := ResolveExceptionWork(items[0], res, forged); !errors.Is(err, ErrResolutionRejected) {
		t.Fatalf("forged evidence err=%v", err)
	}

	uncorrected := ExceptionCorrection{
		Corrected:   attend004SamePunches(t, res),
		EvidenceRef: ref("corrections", "3"),
		Resolver:    "supervisor-2",
		ResolvedAt:  now.Add(time.Hour),
	}
	if _, err := ResolveExceptionWork(items[0], res, uncorrected); !errors.Is(err, ErrResolutionRejected) {
		t.Fatalf("uncorrected reevaluation err=%v", err)
	}

	other := res
	other.InputDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := ResolveExceptionWork(items[0], other, ExceptionCorrection{
		Corrected:   validRequest(),
		EvidenceRef: ref("corrections", "4"),
		Resolver:    "supervisor-2",
		ResolvedAt:  now.Add(time.Hour),
	}); !errors.Is(err, ErrResolutionRejected) {
		t.Fatalf("mismatched prior err=%v", err)
	}
}

func attend004SamePunches(t *testing.T, res Result) Request {
	t.Helper()
	req := validRequest()
	start := req.Schedule.Shifts[0].Interval.Start
	req.Punches[0].At = start.Add(20 * time.Minute)
	check, err := Evaluate(req)
	if err != nil || check.InputDigest != res.InputDigest {
		t.Fatalf("setup: digest=%q want %q err=%v", check.InputDigest, res.InputDigest, err)
	}
	return req
}
