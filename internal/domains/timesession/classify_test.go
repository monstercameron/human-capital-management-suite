package timesession

import (
	"sync"
	"testing"
)

// TestTodo_WTIME_003 is the PRIMARY acceptance test for the pure
// classifier: every named reason routes on its own, and the fixed
// precedence table decides ties exactly once, in the declared order.
func TestTodo_WTIME_003(t *testing.T) {
	cases := []struct {
		name  string
		facts PunchFacts
		want  Classification
	}{
		{"clean punch accepts", PunchFacts{ClockSkewWithinTolerance: true}, Classification{Decision: DecisionAccept, Precedence: 100}},
		{"duplicate", PunchFacts{DuplicatePunchDetected: true}, Classification{Decision: DecisionDuplicate, Reason: ReasonDuplicatePunch, Precedence: 10}},
		{"rest breach", PunchFacts{RestBreachDetected: true, ClockSkewWithinTolerance: true}, Classification{Decision: DecisionHold, Reason: ReasonRestBreach, Precedence: 20}},
		{"minor window", PunchFacts{MinorWindowViolation: true, ClockSkewWithinTolerance: true}, Classification{Decision: DecisionHold, Reason: ReasonMinorWindow, Precedence: 30}},
		{"early lockout", PunchFacts{EarlyLockout: true, ClockSkewWithinTolerance: true}, Classification{Decision: DecisionHold, Reason: ReasonEarlyLockout, Precedence: 40}},
		{"geofence", PunchFacts{OutsideGeofence: true, ClockSkewWithinTolerance: true}, Classification{Decision: DecisionHold, Reason: ReasonGeofence, Precedence: 50}},
		{"spoof suspected", PunchFacts{SharedDeviceSpoofSuspected: true, ClockSkewWithinTolerance: true}, Classification{Decision: DecisionReview, Reason: ReasonSpoofSuspected, Precedence: 60}},
		{"offline replay", PunchFacts{OfflineReplayDetected: true, ClockSkewWithinTolerance: true}, Classification{Decision: DecisionReview, Reason: ReasonOfflineReplay, Precedence: 70}},
		{"dst fold", PunchFacts{DSTFoldAmbiguous: true, ClockSkewWithinTolerance: true}, Classification{Decision: DecisionReview, Reason: ReasonDSTFold, Precedence: 80}},
		{"clock skew out of tolerance", PunchFacts{ClockSkewWithinTolerance: false}, Classification{Decision: DecisionReview, Reason: ReasonClockSkew, Precedence: 90}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.facts); got != c.want {
				t.Fatalf("Classify(%+v) = %+v, want %+v", c.facts, got, c.want)
			}
		})
	}

	// Precedence is fixed: every flag set together still resolves to the
	// single highest-precedence reason (duplicate beats every hold and
	// review), never a combined or ambiguous result.
	everything := PunchFacts{
		DuplicatePunchDetected: true, RestBreachDetected: true, MinorWindowViolation: true, EarlyLockout: true,
		OutsideGeofence: true, SharedDeviceSpoofSuspected: true, OfflineReplayDetected: true, DSTFoldAmbiguous: true,
		ClockSkewWithinTolerance: false,
	}
	if got := Classify(everything); got.Decision != DecisionDuplicate || got.Reason != ReasonDuplicatePunch || got.Precedence != 10 {
		t.Fatalf("Classify(everything) = %+v, want duplicate at precedence 10", got)
	}

	// A hold beats a review when both are present without a duplicate.
	holdAndReview := PunchFacts{OutsideGeofence: true, DSTFoldAmbiguous: true, ClockSkewWithinTolerance: true}
	if got := Classify(holdAndReview); got.Decision != DecisionHold || got.Reason != ReasonGeofence {
		t.Fatalf("Classify(hold+review) = %+v, want HOLD/GEOFENCE", got)
	}
}

// TestTodo_WTIME_003_Security asserts the classifier never lets a
// higher-risk fact (spoof suspected, or a legal hold) be silently
// downgraded to ACCEPT by the presence of an unrelated benign fact, and
// that the classifier is a total function: every zero-value PunchFacts
// accepts rather than erroring or panicking (a caller with no signal must
// get an explicit ACCEPT, never an implicit hold).
func TestTodo_WTIME_003_Security(t *testing.T) {
	// The zero value of ClockSkewWithinTolerance is false, so unresolved
	// skew evidence fails closed to REVIEW rather than silently accepting:
	// a caller that forgot to resolve this fact gets a reviewable punch,
	// never an implicit ACCEPT.
	if got := Classify(PunchFacts{}); got.Decision != DecisionReview || got.Reason != ReasonClockSkew {
		t.Fatalf("Classify(zero facts) = %+v, want REVIEW/REVIEW_CLOCK_SKEW (fail closed on unresolved skew)", got)
	}

	spoofWithGoodSkew := PunchFacts{SharedDeviceSpoofSuspected: true, ClockSkewWithinTolerance: true}
	got := Classify(spoofWithGoodSkew)
	if got.Decision != DecisionReview || got.Reason != ReasonSpoofSuspected {
		t.Fatalf("Classify(spoof, good skew) = %+v, want REVIEW/SPOOF_SUSPECTED regardless of skew", got)
	}

	geofenceWithGoodEverythingElse := PunchFacts{OutsideGeofence: true, ClockSkewWithinTolerance: true}
	got = Classify(geofenceWithGoodEverythingElse)
	if got.Decision != DecisionHold {
		t.Fatalf("Classify(outside geofence) = %+v, want HOLD even with every other fact clean", got)
	}
}

// TestTodo_WTIME_003_Race runs many concurrent Classify calls over the same
// and different fact sets to prove the pure function has no shared mutable
// state; run with `go test -race`.
func TestTodo_WTIME_003_Race(t *testing.T) {
	inputs := []PunchFacts{
		{},
		{DuplicatePunchDetected: true},
		{RestBreachDetected: true},
		{OutsideGeofence: true, ClockSkewWithinTolerance: true},
		{ClockSkewWithinTolerance: false},
	}
	want := make([]Classification, len(inputs))
	for i, f := range inputs {
		want[i] = Classify(f)
	}

	const rounds = 200
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				for i, f := range inputs {
					if got := Classify(f); got != want[i] {
						t.Errorf("Classify(%+v) = %+v, want %+v", f, got, want[i])
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkTodo_WTIME_003(b *testing.B) {
	facts := PunchFacts{ClockSkewWithinTolerance: true}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Classify(facts)
	}
}
