// ATTEND-005 RED: after an exception is resolved, the dependency graph must
// identify the hours/overtime/balances/pay inputs, the recalculation must
// post each consumer delta once, and reconciliation must verify all
// consumers.
package attendance

import (
	"errors"
	"testing"
	"time"
)

func attend005Pair(t *testing.T) (prior, fresh Result) {
	t.Helper()
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	late := validRequest()
	late.Punches[0].At = start.Add(20 * time.Minute)
	prior, err := Evaluate(late)
	if err != nil || prior.Outcome != Exception {
		t.Fatalf("setup prior: outcome=%s err=%v", prior.Outcome, err)
	}
	fresh, err = Evaluate(validRequest())
	if err != nil || fresh.Outcome != Compliant {
		t.Fatalf("setup fresh: outcome=%s err=%v", fresh.Outcome, err)
	}
	if prior.InputDigest == fresh.InputDigest {
		t.Fatal("setup: prior and fresh digests must differ")
	}
	return prior, fresh
}

// TestTodo_ATTEND_005 is the PRIMARY contract: the dependency graph names
// every downstream input, recalculation posts each consumer delta exactly
// once, and reconciliation verifies every consumer.
func TestTodo_ATTEND_005(t *testing.T) {
	prior, fresh := attend005Pair(t)

	t.Run("dependency graph identifies every downstream input", func(t *testing.T) {
		graph := ConsumerInputs()
		for _, consumer := range []DownstreamConsumer{ConsumerHours, ConsumerOvertime, ConsumerBalances, ConsumerPay} {
			inputs, ok := graph[consumer]
			if !ok || len(inputs) == 0 {
				t.Fatalf("consumer %q has no declared inputs", consumer)
			}
		}
		hours := map[ExceptionKind]bool{}
		for _, k := range graph[ConsumerHours] {
			hours[k] = true
		}
		if !hours[LateException] {
			t.Fatalf("hours must consume late exceptions: %v", graph[ConsumerHours])
		}
		overtime := map[ExceptionKind]bool{}
		for _, k := range graph[ConsumerOvertime] {
			overtime[k] = true
		}
		if !overtime[OvertimeException] {
			t.Fatalf("overtime must consume overtime exceptions: %v", graph[ConsumerOvertime])
		}
	})

	t.Run("recalculation posts each consumer delta once", func(t *testing.T) {
		recalc, err := RecalculateDownstream("exception-work-shift-1-LATE-0", prior, fresh)
		if err != nil {
			t.Fatalf("RecalculateDownstream: %v", err)
		}
		if recalc.PriorDigest != prior.InputDigest || recalc.NewDigest != fresh.InputDigest {
			t.Fatalf("recalculation does not bind prior and fresh: %+v", recalc)
		}
		if recalc.Receipt == "" {
			t.Fatal("recalculation must carry a receipt")
		}
		if len(recalc.Deltas) != 4 {
			t.Fatalf("deltas=%d want one per consumer", len(recalc.Deltas))
		}
		seen := map[DownstreamConsumer]bool{}
		for _, d := range recalc.Deltas {
			if seen[d.Consumer] {
				t.Fatalf("duplicate delta for %q", d.Consumer)
			}
			seen[d.Consumer] = true
			if d.DeltaMinutes != d.NewMinutes-d.PriorMinutes {
				t.Fatalf("delta arithmetic broken for %q: %+v", d.Consumer, d)
			}
		}
		var hours ConsumerDelta
		for _, d := range recalc.Deltas {
			if d.Consumer == ConsumerHours {
				hours = d
			}
		}
		if hours.PriorMinutes != 15 || hours.NewMinutes != 0 || hours.DeltaMinutes != -15 {
			t.Fatalf("hours delta=%+v want prior=15 new=0 delta=-15", hours)
		}

		ledger := NewRecalcLedger()
		stored, already := ledger.Post(recalc)
		if already || stored.Receipt != recalc.Receipt {
			t.Fatalf("first post must store: stored=%+v already=%v", stored, already)
		}
		again, already := ledger.Post(recalc)
		if !already || again.Receipt != recalc.Receipt || ledger.Count() != 1 {
			t.Fatalf("second post must be a no-op: again=%+v already=%v count=%d", again, already, ledger.Count())
		}
	})

	t.Run("reconciliation verifies all consumers", func(t *testing.T) {
		recalc, err := RecalculateDownstream("exception-work-shift-1-LATE-0", prior, fresh)
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyReconciliation(recalc); err != nil {
			t.Fatalf("VerifyReconciliation: %v", err)
		}
		short := recalc
		short.Deltas = short.Deltas[:2]
		if err := VerifyReconciliation(short); !errors.Is(err, ErrRecalculationRejected) {
			t.Fatalf("partial coverage err=%v, want ATTEND_005_REJECTED", err)
		}
	})

	t.Run("identical digests are rejected", func(t *testing.T) {
		if _, err := RecalculateDownstream("work-1", prior, prior); !errors.Is(err, ErrRecalculationRejected) {
			t.Fatalf("no-op recalculation err=%v, want ATTEND_005_REJECTED", err)
		}
		if _, err := RecalculateDownstream("", prior, fresh); !errors.Is(err, ErrRecalculationRejected) {
			t.Fatalf("unscoped recalculation err=%v, want ATTEND_005_REJECTED", err)
		}
	})
}

// TestTodo_ATTEND_005_Property holds the recalculation invariants: delta
// arithmetic is exact across lateness variants, the receipt is stable per
// input and sensitive to the fresh evidence, and equal findings post zero
// deltas exactly once.
func TestTodo_ATTEND_005_Property(t *testing.T) {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)

	t.Run("delta arithmetic is exact across variants", func(t *testing.T) {
		for _, late := range []struct {
			arrival time.Duration
			minutes int
		}{
			{10 * time.Minute, 5},
			{20 * time.Minute, 15},
			{60 * time.Minute, 55},
		} {
			req := validRequest()
			req.Punches[0].At = start.Add(late.arrival)
			prior, err := Evaluate(req)
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := Evaluate(validRequest())
			if err != nil {
				t.Fatal(err)
			}
			recalc, err := RecalculateDownstream("work-variant", prior, fresh)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range recalc.Deltas {
				if d.Consumer != ConsumerHours && d.Consumer != ConsumerPay {
					continue
				}
				if d.PriorMinutes != late.minutes || d.DeltaMinutes != -late.minutes {
					t.Fatalf("arrival+%v: %+v want prior=%d", late.arrival, d, late.minutes)
				}
			}
		}
	})

	t.Run("receipt is stable and input sensitive", func(t *testing.T) {
		prior, fresh := attend005Pair(t)
		first, err := RecalculateDownstream("work-1", prior, fresh)
		if err != nil {
			t.Fatal(err)
		}
		second, err := RecalculateDownstream("work-1", prior, fresh)
		if err != nil {
			t.Fatal(err)
		}
		if first.Receipt != second.Receipt {
			t.Fatal("same recalculation must produce a stable receipt")
		}
		other := validRequest()
		other.Punches[0].At = start.Add(30 * time.Minute)
		alt, err := Evaluate(other)
		if err != nil {
			t.Fatal(err)
		}
		third, err := RecalculateDownstream("work-1", prior, alt)
		if err != nil {
			t.Fatal(err)
		}
		if third.Receipt == first.Receipt {
			t.Fatal("different fresh evidence must produce a different receipt")
		}
	})

	t.Run("equal findings post zero deltas once", func(t *testing.T) {
		prior, _ := attend005Pair(t)
		again := validRequest()
		again.Punches[0].ID = "punch-1b"
		again.Punches[0].At = time.Date(2026, 9, 3, 9, 20, 0, 0, time.UTC)
		alt, err := Evaluate(again)
		if err != nil {
			t.Fatal(err)
		}
		if alt.InputDigest == prior.InputDigest {
			t.Fatal("setup: distinct punch evidence must produce a distinct digest")
		}
		recalc, err := RecalculateDownstream("work-equal", prior, alt)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range recalc.Deltas {
			if d.DeltaMinutes != 0 {
				t.Fatalf("equal findings must post zero delta: %+v", d)
			}
		}
		ledger := NewRecalcLedger()
		if _, already := ledger.Post(recalc); already {
			t.Fatal("first post must store")
		}
		if _, already := ledger.Post(recalc); !already {
			t.Fatal("second post must be a no-op")
		}
	})
}

func TestTodo_REV_045_01_Recalculation(t *testing.T) {
	prior, _ := attend005Pair(t)
	fresh := Result{Outcome: Exception, InputDigest: "fresh-meal-rest-evaluation", Exceptions: []Finding{
		{Kind: LateException, ShiftID: "s1", Minutes: 7},
		{Kind: MealException, ShiftID: "s1", Minutes: 30},
		{Kind: BreakException, ShiftID: "s1", Minutes: 10},
	}}
	rule := PremiumRule{JurisdictionCode: "US-CA", RuleRef: VersionedRef{ID: "ca-meal-rest", Version: "2026"}, MealHours: 1, RestHours: 1, MaxMealPerWorkday: 1, MaxRestPerWorkday: 1}
	workdays := map[string]string{"s1": "2026-09-03"}
	recalc, state, err := RecalculateDownstreamWithPremiums("work-1", "US-CA", prior, fresh, rule, workdays)
	if err != nil || state != Exception {
		t.Fatalf("state=%s err=%v", state, err)
	}
	var pay ConsumerDelta
	for _, delta := range recalc.Deltas {
		if delta.Consumer == ConsumerPay {
			pay = delta
			break
		}
	}
	if pay.NewMinutes != 7 || pay.DeltaMinutes != -8 {
		t.Fatalf("minute pay delta=%+v, want new=7 delta=-8 with break premiums separate", pay)
	}
	want := []PremiumLine{{WorkdayID: "2026-09-03", Kind: BreakException, Count: 1, Hours: 1}, {WorkdayID: "2026-09-03", Kind: MealException, Count: 1, Hours: 1}}
	if len(recalc.PremiumLines) != len(want) {
		t.Fatalf("premium lines=%+v want=%+v", recalc.PremiumLines, want)
	}
	for i := range want {
		if recalc.PremiumLines[i] != want[i] {
			t.Fatalf("premium[%d]=%+v want=%+v", i, recalc.PremiumLines[i], want[i])
		}
	}
	if recalc.Receipt == "" {
		t.Fatal("receipt missing")
	}
	otherRule := rule
	otherRule.RuleRef.Version = "2026.1"
	other, _, err := RecalculateDownstreamWithPremiums("work-1", "US-CA", prior, fresh, otherRule, workdays)
	if err != nil {
		t.Fatal(err)
	}
	if other.Receipt == recalc.Receipt {
		t.Fatal("receipt must bind the premium rule revision")
	}
	unknown, outcome, err := RecalculateDownstreamWithPremiums("work-1", "US-NY", prior, fresh, rule, workdays)
	if err != nil || outcome != Unknown || unknown.Receipt != "" {
		t.Fatalf("unknown jurisdiction recalc=%+v outcome=%s err=%v", unknown, outcome, err)
	}
	unknown, outcome, err = RecalculateDownstreamWithPremiums("work-1", "US-CA", prior, fresh, rule, nil)
	if err != nil || outcome != Unknown || unknown.Receipt != "" {
		t.Fatalf("missing workday recalc=%+v outcome=%s err=%v", unknown, outcome, err)
	}
}
