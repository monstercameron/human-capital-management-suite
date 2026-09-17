package benefits

import (
	"errors"
	"testing"
	"time"
)

func ben006Input() QLEInput {
	return QLEInput{
		Tenant: "acme", WorkerRef: "worker-1", Event: LifeEventBirth,
		EventDate:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		ReportDate: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
		Evidence:   EvidenceProvided, EvidenceRef: "birth-certificate:2026-114",
		WindowDays: 30, PreservedTier: TierEmployeeOnly, DependentRef: "dep-child-2",
	}
}

// TestTodo_BEN_006 is the PRIMARY contract: event type, date, evidence
// and window determine the permitted election delta; late, missing or
// uncertain evidence preserves existing coverage under review.
func TestTodo_BEN_006(t *testing.T) {
	got, err := ProcessQLE(ben006Input())
	if err != nil {
		t.Fatalf("ProcessQLE: %v", err)
	}
	if got.Status != QLEPermitted || got.Delta != DeltaAddDependent {
		t.Fatalf("birth with evidence must permit ADD_DEPENDENT: %+v", got)
	}
	if !got.WindowEnd.Equal(got.WindowStart.AddDate(0, 0, 30)) {
		t.Fatalf("window must span WindowDays: %+v", got)
	}
	if got.Digest == "" {
		t.Fatalf("decision must seal a digest")
	}

	t.Run("late report preserves coverage under review", func(t *testing.T) {
		in := ben006Input()
		in.ReportDate = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		got, err := ProcessQLE(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != QLEReviewRequired || got.Delta != DeltaNone {
			t.Fatalf("late report must hold review with no delta: %+v", got)
		}
		if got.PreservedTier != TierEmployeeOnly {
			t.Fatalf("existing coverage must be preserved: %+v", got)
		}
	})

	for _, ev := range []EvidenceState{EvidenceMissing, EvidenceUncertain} {
		in := ben006Input()
		in.Evidence = ev
		in.EvidenceRef = ""
		got, err := ProcessQLE(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != QLEReviewRequired || got.Delta != DeltaNone {
			t.Fatalf("evidence %s must hold review with no delta: %+v", ev, got)
		}
	}

	t.Run("marriage maps to tier change, divorce to removal", func(t *testing.T) {
		marriage := ben006Input()
		marriage.Event = LifeEventMarriage
		marriage.DependentRef = "dep-spouse"
		got, err := ProcessQLE(marriage)
		if err != nil {
			t.Fatal(err)
		}
		if got.Delta != DeltaChangeTier {
			t.Fatalf("marriage must permit CHANGE_TIER: %+v", got)
		}
		divorce := ben006Input()
		divorce.Event = LifeEventDivorce
		divorce.DependentRef = "dep-spouse"
		got, err = ProcessQLE(divorce)
		if err != nil {
			t.Fatal(err)
		}
		if got.Delta != DeltaRemoveDependent {
			t.Fatalf("divorce must permit REMOVE_DEPENDENT: %+v", got)
		}
	})
}

func TestTodo_BEN_006_Property(t *testing.T) {
	base := ben006Input()
	a, err := ProcessQLE(base)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ProcessQLE(base)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("identical reports must decide identically")
	}
	// Earlier reporting within the window never weakens the outcome.
	early := base
	early.ReportDate = base.EventDate
	earlyOut, err := ProcessQLE(early)
	if err != nil {
		t.Fatal(err)
	}
	if earlyOut.Status != QLEPermitted {
		t.Fatalf("immediate report must permit: %+v", earlyOut)
	}
	// Every declared event maps to exactly one delta.
	seen := map[LifeEventType]ElectionDelta{}
	for _, ev := range []LifeEventType{LifeEventBirth, LifeEventAdoption, LifeEventMarriage, LifeEventDivorce, LifeEventDependentDeath, LifeEventLossOfCoverage, LifeEventGainOfCoverage} {
		in := base
		in.Event = ev
		in.DependentRef = "dep-x"
		got, err := ProcessQLE(in)
		if err != nil {
			t.Fatalf("event %s: %v", ev, err)
		}
		seen[ev] = got.Delta
	}
	if seen[LifeEventBirth] != DeltaAddDependent || seen[LifeEventGainOfCoverage] != DeltaDropCoverage {
		t.Fatalf("event-to-delta map drifted: %v", seen)
	}
	// Malformed reports are refused, never defaulted.
	for name, mutate := range map[string]func(*QLEInput){
		"event":    func(in *QLEInput) { in.Event = "LOTTERY_WIN" },
		"evidence": func(in *QLEInput) { in.Evidence = "RUMOR" },
		"window":   func(in *QLEInput) { in.WindowDays = 0 },
		"tenant":   func(in *QLEInput) { in.Tenant = "" },
	} {
		in := base
		mutate(&in)
		if _, err := ProcessQLE(in); !errors.Is(err, ErrQLERejected) {
			t.Fatalf("malformed %s must be BEN_006_REJECTED", name)
		}
	}
	backdated := base
	backdated.ReportDate = base.EventDate.AddDate(0, 0, -1)
	if _, err := ProcessQLE(backdated); !errors.Is(err, ErrQLERejected) {
		t.Fatalf("report before event must be BEN_006_REJECTED")
	}
}
