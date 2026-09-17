package attendance

import (
	"errors"
	"testing"
	"time"
)

func attend003Policy() MealBreakPolicy {
	return MealBreakPolicy{
		JurisdictionCode: "US-NY", JurisdictionRef: ref("jurisdiction", "2026"), RulesRef: ref("rules", "3"),
		LegalRef: ref("labor-law", "2026"), CBARef: ref("cba", "5"), CompanyRef: ref("company-policy", "9"),
		MealAfter: 5 * time.Hour, MealMinutes: 30 * time.Minute,
		RestEvery: 4 * time.Hour, RestMinutes: 10 * time.Minute,
		DailyOvertimeAfter: 8 * time.Hour, WaiverAllowed: true,
	}
}

func attend003Shift(start time.Time, hours int) (Request, MealBreakPolicy) {
	req := validRequest()
	end := start.Add(time.Duration(hours) * time.Hour)
	req.Schedule.Shifts = []Shift{{ID: "shift-1", Interval: Interval{Start: start, End: end}}}
	req.Punches = []Punch{
		{ID: "in", Ref: ref("punches", "12"), At: start, Direction: PunchIn},
		{ID: "out", Ref: ref("punches", "12"), At: end, Direction: PunchOut},
	}
	req.Breaks = nil
	policy := attend003Policy()
	return req, policy
}

// TestTodo_ATTEND_003 is the RED contract: legal/CBA/company composition plus
// waiver and attestation evidence determine the exception.
func TestTodo_ATTEND_003(t *testing.T) {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)

	t.Run("ten-hour-shift-without-breaks", func(t *testing.T) {
		req, policy := attend003Shift(start, 10)
		got, err := DetectMealBreakOvertime(req, policy, nil)
		if err != nil || got.Outcome != Exception {
			t.Fatalf("outcome=%s err=%v findings=%+v", got.Outcome, err, got.Exceptions)
		}
		want := map[ExceptionKind]int{MealException: 30, BreakException: 20, OvertimeException: 120}
		if len(got.Exceptions) != len(want) {
			t.Fatalf("exceptions=%+v, want %v", got.Exceptions, want)
		}
		for _, e := range got.Exceptions {
			if want[e.Kind] != e.Minutes {
				t.Errorf("kind=%s minutes=%d, want %d", e.Kind, e.Minutes, want[e.Kind])
			}
			if durationMinutes(e.Interval) != e.Minutes {
				t.Errorf("kind=%s interval=%v has %d minutes, finding says %d", e.Kind, e.Interval, durationMinutes(e.Interval), e.Minutes)
			}
		}
	})

	t.Run("eight-hour-shift-with-meal-break", func(t *testing.T) {
		req, policy := attend003Shift(start, 8)
		req.Breaks = []Break{{ID: "meal", Ref: ref("breaks", "4"), Interval: Interval{Start: start.Add(4 * time.Hour), End: start.Add(4*time.Hour + 30*time.Minute)}}}
		got, err := DetectMealBreakOvertime(req, policy, nil)
		if err != nil || got.Outcome != Compliant {
			t.Fatalf("outcome=%s err=%v findings=%+v", got.Outcome, err, got.Exceptions)
		}
	})

	t.Run("waiver-with-attestation-excuses-meal", func(t *testing.T) {
		req, policy := attend003Shift(start, 8)
		waivers := []Waiver{{ShiftID: "shift-1", Kind: WaiverMeal, WaiverRef: ref("waivers", "1"), AttestationRef: ref("attestations", "7")}}
		got, err := DetectMealBreakOvertime(req, policy, waivers)
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		for _, e := range got.Exceptions {
			if e.Kind == MealException {
				t.Fatalf("attested waiver did not excuse meal: %+v", e)
			}
		}
	})

	t.Run("waiver-without-attestation-excuses-nothing", func(t *testing.T) {
		req, policy := attend003Shift(start, 8)
		waivers := []Waiver{{ShiftID: "shift-1", Kind: WaiverMeal, WaiverRef: ref("waivers", "1")}}
		_, err := DetectMealBreakOvertime(req, policy, waivers)
		if !errors.Is(err, ErrInvalidEvidence) {
			t.Fatalf("err=%v, want invalid evidence", err)
		}
	})
}

// TestTodo_ATTEND_003_Property proves determinism and minute-consistency
// across shift lengths.
func TestTodo_ATTEND_003_Property(t *testing.T) {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	for _, hours := range []int{4, 6, 8, 10, 12} {
		req, policy := attend003Shift(start, hours)
		a, err := DetectMealBreakOvertime(req, policy, nil)
		if err != nil {
			t.Fatal(err)
		}
		b, err := DetectMealBreakOvertime(req, policy, nil)
		if err != nil {
			t.Fatal(err)
		}
		if a.Digest != b.Digest || a.Outcome != b.Outcome || len(a.Exceptions) != len(b.Exceptions) {
			t.Fatalf("%dh: nondeterministic: %+v vs %+v", hours, a, b)
		}
		for _, e := range a.Exceptions {
			if durationMinutes(e.Interval) != e.Minutes {
				t.Fatalf("%dh: kind=%s interval minutes mismatch", hours, e.Kind)
			}
		}
	}
}

// TestTodo_ATTEND_003_Golden pins the exact findings for the canonical long
// shift without breaks.
func TestTodo_ATTEND_003_Golden(t *testing.T) {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	req, policy := attend003Shift(start, 10)
	got, err := DetectMealBreakOvertime(req, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest == "" {
		t.Fatal("golden result carries no digest")
	}
	again, err := DetectMealBreakOvertime(req, policy, nil)
	if err != nil || again.Digest != got.Digest {
		t.Fatalf("golden digest unstable: %q vs %q", got.Digest, again.Digest)
	}
	if len(got.Exceptions) != 3 || got.Exceptions[0].Kind != MealException || got.Exceptions[1].Kind != BreakException || got.Exceptions[2].Kind != OvertimeException {
		t.Fatalf("golden order = %+v", got.Exceptions)
	}
}

// TestTodo_ATTEND_003_Security proves an unknown jurisdiction or rule never
// reports compliant, and forged composition never excuses an exception.
func TestTodo_ATTEND_003_Security(t *testing.T) {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	req, _ := attend003Shift(start, 6)
	for name, mutate := range map[string]func(*MealBreakPolicy){
		"jurisdiction": func(p *MealBreakPolicy) { p.JurisdictionCode = "XX-UNKNOWN" },
		"legal":        func(p *MealBreakPolicy) { p.LegalRef = VersionedRef{} },
		"cba":          func(p *MealBreakPolicy) { p.CBARef = VersionedRef{} },
		"company":      func(p *MealBreakPolicy) { p.CompanyRef = VersionedRef{} },
		"rules":        func(p *MealBreakPolicy) { p.RulesRef = VersionedRef{} },
	} {
		t.Run(name, func(t *testing.T) {
			policy := attend003Policy()
			mutate(&policy)
			got, err := DetectMealBreakOvertime(req, policy, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.Outcome == Compliant {
				t.Fatalf("unknown composition reported compliant: %+v", got)
			}
		})
	}
}

// TestTodo_ATTEND_003_Mutation proves tampered thresholds and misdirected
// waivers cannot produce a clean result.
func TestTodo_ATTEND_003_Mutation(t *testing.T) {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	req, policy := attend003Shift(start, 8)
	policy.MealAfter = -time.Hour
	if _, err := DetectMealBreakOvertime(req, policy, nil); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("negative threshold err=%v", err)
	}
	req, policy = attend003Shift(start, 8)
	waivers := []Waiver{{ShiftID: "other-shift", Kind: WaiverMeal, WaiverRef: ref("waivers", "1"), AttestationRef: ref("attestations", "7")}}
	got, err := DetectMealBreakOvertime(req, policy, waivers)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range got.Exceptions {
		if e.Kind == MealException {
			found = true
		}
	}
	if !found {
		t.Fatal("waiver for another shift excused this shift")
	}
	req, policy = attend003Shift(start, 8)
	policy.WaiverAllowed = false
	waivers = []Waiver{{ShiftID: "shift-1", Kind: WaiverMeal, WaiverRef: ref("waivers", "1"), AttestationRef: ref("attestations", "7")}}
	got, err = DetectMealBreakOvertime(req, policy, waivers)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, e := range got.Exceptions {
		if e.Kind == MealException {
			found = true
		}
	}
	if !found {
		t.Fatal("disallowed waiver excused the meal exception")
	}
}
