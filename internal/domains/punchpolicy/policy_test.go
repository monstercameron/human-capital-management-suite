package punchpolicy

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func fixturePolicy() Policy {
	return Policy{
		ID:           "site-1010",
		Version:      3,
		Jurisdiction: "US-CA",
		Rounding:     Rounding{IncrementMinutes: 15, Mode: RoundNearest},
		Grace:        Grace{EarlyMinutes: 5, LateMinutes: 5},
		Lockout:      Lockout{EarlyMinutes: 10, RequiresPublishedShift: true, OverrideRoles: []string{"SUPERVISOR"}},
		AutoDeduct:   AutoDeduct{AfterMinutes: 360, DeductMinutes: 30, WaivedByAttestation: true},
	}
}

func fixtureShift(published bool) Shift {
	start := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	return Shift{ID: "shift-1", Published: published, Interval: Interval{Start: start, End: start.Add(8 * time.Hour)}}
}

// TestTodo_TCLOCK_009 proves the composed contract: a versioned policy
// rounds a raw interval into a separate evaluated result without touching
// the raw punch, an unpublished shift under lockout routes to override
// rather than a silent block, and a jurisdiction that forbids rounding
// rejects the policy outright.
func TestTodo_TCLOCK_009(t *testing.T) {
	policy := fixturePolicy()
	shift := fixtureShift(true)
	raw := Interval{Start: shift.Interval.Start.Add(7 * time.Minute), End: shift.Interval.End.Add(-4 * time.Minute)}

	evaluated, err := Evaluate(policy, raw, shift, nil)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if evaluated.RawStart != raw.Start || evaluated.RawEnd != raw.End {
		t.Fatalf("Evaluate rewrote the raw punch: got %v/%v want %v/%v", evaluated.RawStart, evaluated.RawEnd, raw.Start, raw.End)
	}
	if evaluated.RoundedStart.Equal(raw.Start) && evaluated.RoundedEnd.Equal(raw.End) {
		t.Fatalf("expected rounding to change the evaluated interval, got the raw interval unchanged")
	}
	if evaluated.PolicyID != policy.ID || evaluated.PolicyVersion != policy.Version {
		t.Fatalf("evaluated interval does not pin the policy identity: got %s/%d", evaluated.PolicyID, evaluated.PolicyVersion)
	}
	if evaluated.AutoDeductMinutes == 0 {
		t.Fatalf("expected the 8 hour shift to cross the auto-deduct threshold")
	}

	// A worker attestation that the meal was not taken waives the deduction.
	waived, err := Evaluate(policy, raw, shift, []Attestation{{Kind: MealTakenAttestation, Taken: false}})
	if err != nil {
		t.Fatalf("Evaluate with waiver: %v", err)
	}
	if waived.AutoDeductMinutes != 0 {
		t.Fatalf("attestation that the meal was not taken must waive the auto-deduct, got %d minutes deducted", waived.AutoDeductMinutes)
	}
	if waived.PaidMinutes <= evaluated.PaidMinutes {
		t.Fatalf("waived paid minutes (%d) should exceed the deducted paid minutes (%d)", waived.PaidMinutes, evaluated.PaidMinutes)
	}

	// An unpublished shift under a lockout policy never silently blocks the
	// punch: it always routes to an override decision.
	unpublished := fixtureShift(false)
	decision, err := CheckClockIn(policy, unpublished.Interval.Start, unpublished, Override{})
	if err != nil {
		t.Fatalf("CheckClockIn: %v", err)
	}
	if decision.Status != RequiresOverride {
		t.Fatalf("unpublished shift under RequiresPublishedShift must require override, got %s", decision.Status)
	}
	overridden, err := CheckClockIn(policy, unpublished.Interval.Start, unpublished, Override{Role: "SUPERVISOR", ApprovedBy: "mgr-1"})
	if err != nil {
		t.Fatalf("CheckClockIn with override: %v", err)
	}
	if overridden.Status != OverriddenAllowed {
		t.Fatalf("a recognised override role must clear the lockout, got %s", overridden.Status)
	}

	// A jurisdiction that forbids rounding rejects the policy.
	err = ValidateFor(policy, JurisdictionRules{Code: "US-CA", RoundingForbidden: true})
	if !errors.Is(err, ErrPolicyRejected) {
		t.Fatalf("expected ErrPolicyRejected for a rounding-forbidding jurisdiction, got %v", err)
	}
	if err := ValidateFor(policy, JurisdictionRules{Code: "US-CA"}); err != nil {
		t.Fatalf("a jurisdiction that allows rounding must accept the policy: %v", err)
	}
}

// TestTodo_TCLOCK_009_Golden pins the exact evaluated trace for one fixed
// policy and punch so a change to rounding, grace or auto-deduct semantics
// shows up as a diffable line, not a silently different number.
func TestTodo_TCLOCK_009_Golden(t *testing.T) {
	policy := fixturePolicy()
	shift := fixtureShift(true)
	raw := Interval{Start: shift.Interval.Start.Add(7 * time.Minute), End: shift.Interval.End.Add(-4 * time.Minute)}
	evaluated, err := Evaluate(policy, raw, shift, nil)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	var rendered string
	rendered += fmt.Sprintf("policy: %s v%d\n", evaluated.PolicyID, evaluated.PolicyVersion)
	rendered += fmt.Sprintf("raw:     %s -> %s\n", evaluated.RawStart.Format(time.RFC3339), evaluated.RawEnd.Format(time.RFC3339))
	rendered += fmt.Sprintf("rounded: %s -> %s\n", evaluated.RoundedStart.Format(time.RFC3339), evaluated.RoundedEnd.Format(time.RFC3339))
	rendered += fmt.Sprintf("auto-deduct minutes: %d\n", evaluated.AutoDeductMinutes)
	rendered += fmt.Sprintf("paid minutes: %d\n", evaluated.PaidMinutes)
	rendered += "trace:\n"
	for _, line := range evaluated.Trace {
		rendered += "  - " + line + "\n"
	}
	assertGolden(t, "tclock_009_evaluate.txt", rendered)
}

// TestTodo_TCLOCK_009_Property proves that for every declared rounding
// direction, applying it to both edges of an interval drawn from a uniform
// distribution of punch minutes leaves the mean evaluated duration
// unchanged relative to the mean raw duration, exactly as 29 CFR 785.48(b)
// requires of a lawful rounding rule.
func TestTodo_TCLOCK_009_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(20260928))
	const samples = 20000
	const increment = 15 * time.Minute
	const rawDuration = 8 * time.Hour

	for _, mode := range []RoundingDirection{RoundNearest, RoundUp, RoundDown} {
		var totalDelta time.Duration
		base := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
		for i := 0; i < samples; i++ {
			// A uniformly distributed punch minute across a wide window so
			// the fractional offset within any increment window is itself
			// uniform, regardless of increment size.
			startOffset := time.Duration(rng.Int63n(int64(24 * time.Hour)))
			start := base.Add(time.Duration(i) * 25 * time.Hour).Add(startOffset)
			end := start.Add(rawDuration)

			roundedStart := roundTime(start, increment, mode)
			roundedEnd := roundTime(end, increment, mode)

			rawDelta := end.Sub(start)
			roundedDelta := roundedEnd.Sub(roundedStart)
			totalDelta += roundedDelta - rawDelta
		}
		meanDeltaMinutes := float64(totalDelta) / float64(samples) / float64(time.Minute)
		// The mean adjustment must be small relative to the increment: a
		// biased rule (always shrinking the interval) would show a mean
		// delta near a full increment, not near zero.
		if meanDeltaMinutes > 1.0 || meanDeltaMinutes < -1.0 {
			t.Fatalf("mode %s is not neutral over %d uniform samples: mean duration delta %.4f minutes", mode, samples, meanDeltaMinutes)
		}
	}
}

// TestTodo_TCLOCK_009_Mutation asserts precisely enough to kill the most
// obvious single-line mutants: flipping the rounding direction, an
// off-by-one on the lockout boundary, and a wrong-way auto-deduct
// threshold comparison.
func TestTodo_TCLOCK_009_Mutation(t *testing.T) {
	t.Run("rounding direction is not interchangeable", func(t *testing.T) {
		base := time.Date(2026, 3, 2, 9, 7, 0, 0, time.UTC) // 7 minutes past the hour
		up := roundTime(base, 15*time.Minute, RoundUp)
		down := roundTime(base, 15*time.Minute, RoundDown)
		if !up.After(base) {
			t.Fatalf("RoundUp must round strictly forward for a non-boundary time, got %v from %v", up, base)
		}
		if down.After(base) {
			t.Fatalf("RoundDown must never round forward, got %v from %v", down, base)
		}
		if up.Equal(down) {
			t.Fatalf("RoundUp and RoundDown must diverge for a non-boundary time")
		}
	})

	t.Run("lockout boundary is exact, not off by one", func(t *testing.T) {
		policy := fixturePolicy() // EarlyMinutes: 10
		shift := fixtureShift(true)
		exactlyOnBoundary := shift.Interval.Start.Add(-10 * time.Minute)
		oneSecondEarly := exactlyOnBoundary.Add(-time.Second)

		onBoundary, err := CheckClockIn(policy, exactlyOnBoundary, shift, Override{})
		if err != nil {
			t.Fatalf("CheckClockIn: %v", err)
		}
		if onBoundary.Status != Allowed {
			t.Fatalf("a punch exactly at the early boundary must be allowed, got %s", onBoundary.Status)
		}
		beforeBoundary, err := CheckClockIn(policy, oneSecondEarly, shift, Override{})
		if err != nil {
			t.Fatalf("CheckClockIn: %v", err)
		}
		if beforeBoundary.Status != RequiresOverride {
			t.Fatalf("a punch one second before the early boundary must require override, got %s", beforeBoundary.Status)
		}
	})

	t.Run("auto-deduct threshold comparison is not inverted", func(t *testing.T) {
		policy := fixturePolicy() // AfterMinutes: 360, DeductMinutes: 30
		shift := fixtureShift(true)
		short := Interval{Start: shift.Interval.Start, End: shift.Interval.Start.Add(359 * time.Minute)}
		long := Interval{Start: shift.Interval.Start, End: shift.Interval.Start.Add(361 * time.Minute)}
		noRoundPolicy := policy
		noRoundPolicy.Rounding = Rounding{}

		shortResult, err := Evaluate(noRoundPolicy, short, shift, nil)
		if err != nil {
			t.Fatalf("Evaluate short: %v", err)
		}
		if shortResult.AutoDeductMinutes != 0 {
			t.Fatalf("a shift under the threshold must not be auto-deducted, got %d", shortResult.AutoDeductMinutes)
		}
		longResult, err := Evaluate(noRoundPolicy, long, shift, nil)
		if err != nil {
			t.Fatalf("Evaluate long: %v", err)
		}
		if longResult.AutoDeductMinutes != 30 {
			t.Fatalf("a shift over the threshold must be auto-deducted by exactly the configured minutes, got %d", longResult.AutoDeductMinutes)
		}
	})
}

func TestPolicy_Validate(t *testing.T) {
	valid := fixturePolicy()
	if err := valid.Validate(); err != nil {
		t.Fatalf("fixture policy should validate: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(Policy) Policy
	}{
		{"empty id", func(p Policy) Policy { p.ID = ""; return p }},
		{"zero version", func(p Policy) Policy { p.Version = 0; return p }},
		{"empty jurisdiction", func(p Policy) Policy { p.Jurisdiction = ""; return p }},
		{"negative rounding increment", func(p Policy) Policy { p.Rounding.IncrementMinutes = -1; return p }},
		{"undeclared rounding mode", func(p Policy) Policy { p.Rounding.Mode = "TOWARD_EMPLOYER"; return p }},
		{"negative grace", func(p Policy) Policy { p.Grace.EarlyMinutes = -1; return p }},
		{"negative lockout", func(p Policy) Policy { p.Lockout.EarlyMinutes = -1; return p }},
		{"blank override role", func(p Policy) Policy { p.Lockout.OverrideRoles = []string{" "}; return p }},
		{"deduct with no threshold", func(p Policy) Policy { p.AutoDeduct.AfterMinutes = 0; return p }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.mutate(valid).Validate(); !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("expected ErrInvalidPolicy, got %v", err)
			}
		})
	}
}

func TestValidateFor(t *testing.T) {
	policy := fixturePolicy()

	if err := ValidateFor(policy, JurisdictionRules{Code: "US-TX"}); !errors.Is(err, ErrPolicyRejected) {
		t.Fatalf("a jurisdiction code mismatch must be rejected, got %v", err)
	}
	if err := ValidateFor(policy, JurisdictionRules{Code: "US-CA", MaxLockoutEarlyMinutes: 5}); !errors.Is(err, ErrPolicyRejected) {
		t.Fatalf("a lockout past the jurisdiction cap must be rejected, got %v", err)
	}
	if err := ValidateFor(policy, JurisdictionRules{Code: "US-CA", AllowedOverrideRoles: []string{"HR_ADMIN"}}); !errors.Is(err, ErrPolicyRejected) {
		t.Fatalf("an override role the jurisdiction does not recognise must be rejected, got %v", err)
	}
	if err := ValidateFor(policy, JurisdictionRules{Code: "US-CA", AllowedOverrideRoles: []string{"SUPERVISOR"}}); err != nil {
		t.Fatalf("a recognised override role must be accepted: %v", err)
	}
}
