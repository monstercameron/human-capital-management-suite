package punchpolicy

import (
	"errors"
	"testing"
	"time"
)

func TestEvaluate_InvalidInputs(t *testing.T) {
	policy := fixturePolicy()
	shift := fixtureShift(true)
	validRaw := Interval{Start: shift.Interval.Start, End: shift.Interval.End}

	if _, err := Evaluate(Policy{}, validRaw, shift, nil); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("invalid policy should fail with ErrInvalidPolicy, got %v", err)
	}
	if _, err := Evaluate(policy, Interval{}, shift, nil); !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("zero raw interval should fail with ErrInvalidInterval, got %v", err)
	}
	inverted := Interval{Start: validRaw.End, End: validRaw.Start}
	if _, err := Evaluate(policy, inverted, shift, nil); !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("inverted raw interval should fail with ErrInvalidInterval, got %v", err)
	}
	badShift := shift
	badShift.Interval = Interval{}
	if _, err := Evaluate(policy, validRaw, badShift, nil); !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("zero shift interval should fail with ErrInvalidInterval, got %v", err)
	}
}

func TestEvaluate_NoRoundingConfigured(t *testing.T) {
	policy := fixturePolicy()
	policy.Rounding = Rounding{}
	shift := fixtureShift(true)
	raw := Interval{Start: shift.Interval.Start.Add(3 * time.Minute), End: shift.Interval.Start.Add(4 * time.Hour)}
	evaluated, err := Evaluate(policy, raw, shift, nil)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !evaluated.RoundedStart.Equal(raw.Start) || !evaluated.RoundedEnd.Equal(raw.End) {
		t.Fatalf("with no rounding configured the evaluated interval must equal the raw interval, got %v/%v", evaluated.RoundedStart, evaluated.RoundedEnd)
	}
}

func TestEvaluate_RawIntervalNeverMutated(t *testing.T) {
	policy := fixturePolicy()
	shift := fixtureShift(true)
	raw := Interval{Start: shift.Interval.Start.Add(7 * time.Minute), End: shift.Interval.End.Add(-4 * time.Minute)}
	original := raw
	if _, err := Evaluate(policy, raw, shift, nil); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if raw != original {
		t.Fatalf("Evaluate must not mutate its raw interval argument")
	}
}

func TestRoundTime_ZeroIncrement(t *testing.T) {
	at := time.Date(2026, 1, 1, 12, 7, 0, 0, time.UTC)
	if got := roundTime(at, 0, RoundNearest); !got.Equal(at) {
		t.Fatalf("zero increment must return the time unchanged, got %v", got)
	}
}

func TestRoundTime_OnBoundaryIsUnchanged(t *testing.T) {
	at := time.Date(2026, 1, 1, 12, 15, 0, 0, time.UTC)
	for _, mode := range []RoundingDirection{RoundNearest, RoundUp, RoundDown} {
		if got := roundTime(at, 15*time.Minute, mode); !got.Equal(at) {
			t.Fatalf("mode %s should leave an on-boundary time unchanged, got %v", mode, got)
		}
	}
}

func TestInterval_DurationAndValidate(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	iv := Interval{Start: start, End: start.Add(2 * time.Hour)}
	if iv.Duration() != 2*time.Hour {
		t.Fatalf("Duration: got %v want 2h", iv.Duration())
	}
	if err := iv.validate(); err != nil {
		t.Fatalf("valid interval should validate: %v", err)
	}
	if err := (Interval{}).validate(); !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("zero interval should fail validate: %v", err)
	}
}
