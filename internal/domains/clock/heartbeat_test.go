package clock

import (
	"errors"
	"testing"
	"time"
)

func fleetPolicy() FleetHealthPolicy {
	return FleetHealthPolicy{MaxDrift: 2 * time.Minute, MinimumVersion: "2.4.0", MissingHeartbeatLimit: 15 * time.Minute}
}

// TestTodo_TCLOCK_008 is the PRIMARY test: a healthy, current-version
// heartbeat within drift evaluates HIGH confidence and supported, a
// drifting heartbeat degrades confidence, and silence beyond the policy's
// limit during operating hours raises a missing-heartbeat alert.
func TestTodo_TCLOCK_008(t *testing.T) {
	policy := fleetPolicy()
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)

	healthy := Heartbeat{DeviceRef: "device-1", AppVersion: "2.4.1", QueueDepth: 3, OldestUnsentAge: time.Minute, BatteryPercent: 80, Power: PowerBattery, MeasuredOffset: 30 * time.Second, SentAt: now}
	eval, err := EvaluateHeartbeat(policy, healthy, now)
	if err != nil {
		t.Fatalf("EvaluateHeartbeat: %v", err)
	}
	if eval.Confidence != SyncConfidenceHigh || !eval.VersionSupported || eval.DriftExceeded {
		t.Fatalf("unexpected healthy evaluation %+v", eval)
	}

	drifting := healthy
	drifting.MeasuredOffset = 5 * time.Minute
	eval, err = EvaluateHeartbeat(policy, drifting, now)
	if err != nil {
		t.Fatalf("EvaluateHeartbeat (drifting): %v", err)
	}
	if eval.Confidence != SyncConfidenceDegraded || !eval.DriftExceeded {
		t.Fatalf("expected degraded confidence for drifting heartbeat, got %+v", eval)
	}

	alert, err := MissingHeartbeatAlert(policy, now.Add(-30*time.Minute), now, true)
	if err != nil {
		t.Fatalf("MissingHeartbeatAlert: %v", err)
	}
	if !alert {
		t.Fatalf("expected missing-heartbeat alert after 30 minutes of silence")
	}
	alert, err = MissingHeartbeatAlert(policy, now.Add(-30*time.Minute), now, false)
	if err != nil {
		t.Fatalf("MissingHeartbeatAlert: %v", err)
	}
	if alert {
		t.Fatalf("expected no alert outside operating hours")
	}
}

// TestTodo_TCLOCK_008_Security proves that a device below the minimum
// supported app version is flagged unsupported even when otherwise
// healthy, so a known-defective version cannot keep accepting punches
// unnoticed.
func TestTodo_TCLOCK_008_Security(t *testing.T) {
	policy := fleetPolicy()
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	stale := Heartbeat{DeviceRef: "device-2", AppVersion: "2.3.9", QueueDepth: 0, OldestUnsentAge: 0, BatteryPercent: -1, Power: PowerMains, MeasuredOffset: 0, SentAt: now}
	eval, err := EvaluateHeartbeat(policy, stale, now)
	if err != nil {
		t.Fatalf("EvaluateHeartbeat: %v", err)
	}
	if eval.VersionSupported {
		t.Fatalf("expected version 2.3.9 to be unsupported against minimum 2.4.0")
	}

	newer := stale
	newer.AppVersion = "2.4.0"
	eval, err = EvaluateHeartbeat(policy, newer, now)
	if err != nil {
		t.Fatalf("EvaluateHeartbeat: %v", err)
	}
	if !eval.VersionSupported {
		t.Fatalf("expected version exactly at the minimum to be supported")
	}
}

// TestTodo_TCLOCK_008_Fault proves the package fails closed on malformed
// inputs: a malformed version string, an invalid heartbeat and a server
// clock that precedes the last-seen instant are all rejected rather than
// silently coerced.
func TestTodo_TCLOCK_008_Fault(t *testing.T) {
	policy := fleetPolicy()
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)

	malformed := Heartbeat{DeviceRef: "device-3", AppVersion: "not-a-version", Power: PowerMains, SentAt: now}
	if _, err := EvaluateHeartbeat(policy, malformed, now); !errors.Is(err, ErrHeartbeatRejected) {
		t.Fatalf("expected malformed version to be rejected, got %v", err)
	}

	invalid := Heartbeat{DeviceRef: "", AppVersion: "1.0.0", Power: PowerMains, SentAt: now}
	if err := invalid.Validate(); !errors.Is(err, ErrHeartbeatRejected) {
		t.Fatalf("expected missing device ref to be rejected, got %v", err)
	}

	if _, err := MissingHeartbeatAlert(policy, now, now.Add(-time.Hour), true); !errors.Is(err, ErrHeartbeatRejected) {
		t.Fatalf("expected clock reversal to be rejected, got %v", err)
	}

	if _, err := VersionAtLeast("1..2", "1.0.0"); err == nil {
		t.Fatalf("expected malformed version segment to fail")
	}
}
