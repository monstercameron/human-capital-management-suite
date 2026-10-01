package punchpolicy

import (
	"errors"
	"testing"
	"time"
)

func TestCheckClockIn_AllowedInsideWindow(t *testing.T) {
	policy := fixturePolicy()
	shift := fixtureShift(true)
	decision, err := CheckClockIn(policy, shift.Interval.Start.Add(-2*time.Minute), shift, Override{})
	if err != nil {
		t.Fatalf("CheckClockIn: %v", err)
	}
	if decision.Status != Allowed {
		t.Fatalf("a punch inside the lockout window should be allowed, got %s", decision.Status)
	}
	if decision.PolicyID != policy.ID || decision.PolicyVersion != policy.Version {
		t.Fatalf("decision must pin the policy identity")
	}
}

func TestCheckClockIn_UnrecognisedOverrideStaysRequiresOverride(t *testing.T) {
	policy := fixturePolicy()
	shift := fixtureShift(false) // unpublished -> locked
	decision, err := CheckClockIn(policy, shift.Interval.Start, shift, Override{Role: "CASHIER", ApprovedBy: "someone"})
	if err != nil {
		t.Fatalf("CheckClockIn: %v", err)
	}
	if decision.Status != RequiresOverride {
		t.Fatalf("an override role the policy does not name must not clear the lockout, got %s", decision.Status)
	}
}

func TestCheckClockIn_NoLockoutConfigured(t *testing.T) {
	policy := fixturePolicy()
	policy.Lockout = Lockout{}
	shift := fixtureShift(false)
	decision, err := CheckClockIn(policy, shift.Interval.Start.Add(-2*time.Hour), shift, Override{})
	if err != nil {
		t.Fatalf("CheckClockIn: %v", err)
	}
	if decision.Status != Allowed {
		t.Fatalf("with no lockout configured every punch should be allowed, got %s", decision.Status)
	}
}

func TestCheckClockIn_InvalidInputs(t *testing.T) {
	shift := fixtureShift(true)
	if _, err := CheckClockIn(Policy{}, shift.Interval.Start, shift, Override{}); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("invalid policy should fail with ErrInvalidPolicy, got %v", err)
	}
	if _, err := CheckClockIn(fixturePolicy(), time.Time{}, shift, Override{}); !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("zero punch time should fail with ErrInvalidInterval, got %v", err)
	}
	badShift := shift
	badShift.Interval = Interval{}
	if _, err := CheckClockIn(fixturePolicy(), shift.Interval.Start, badShift, Override{}); !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("zero shift interval under an early-lockout policy should fail with ErrInvalidInterval, got %v", err)
	}
}

func TestOverride_Empty(t *testing.T) {
	if !(Override{}).empty() {
		t.Fatalf("zero-value Override should be empty")
	}
	if (Override{Role: "SUPERVISOR"}).empty() {
		t.Fatalf("Override with a role should not be empty")
	}
}
