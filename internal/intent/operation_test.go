package intent

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// INTENT-022 RED: every material operator mutation resolves a governed
// intent with JIT authority before operator.go exists.
func TestOperatorMutationRequiresIntentAndJITAuthority(t *testing.T) {
	at, err := values.NewInstantFromUnix(1760000000, 0)
	if err != nil {
		t.Fatal(err)
	}
	grantExpiry, err := values.NewInstantFromUnix(1760003600, 0)
	if err != nil {
		t.Fatal(err)
	}
	base := OperatorActionRequest{
		IntentInstanceID: "intent-op-1", Operation: OperationConnectorRedrive,
		Tenant: "harborcare-demo", Target: "connector-incumbent",
		IdempotencyKey: "op-1", Simulated: true, SimulationRef: "sim-1",
		JITGrant: "jit-grant-1", JITExpires: grantExpiry, Approvers: []string{"operator-1"},
	}

	rec, err := AuthorizeOperatorAction(base, at)
	if err != nil {
		t.Fatalf("AuthorizeOperatorAction: %v", err)
	}
	if rec.Digest == "" || rec.IdempotencyKey != "op-1" {
		t.Fatalf("receipt=%+v, want digest and idempotency", rec)
	}

	// RED: database surgery through a privileged side door — no intent
	// instance, no JIT grant — is refused.
	sideDoor := base
	sideDoor.Operation = OperationDBSurgery
	sideDoor.IntentInstanceID = ""
	sideDoor.JITGrant = ""
	if _, err := AuthorizeOperatorAction(sideDoor, at); err == nil {
		t.Fatal("side-door database surgery authorized")
	}

	// RED: failover needs two distinct approvers (dual control).
	solo := base
	solo.Operation = OperationFailover
	if _, err := AuthorizeOperatorAction(solo, at); err == nil {
		t.Fatal("solo failover authorized, want dual control")
	}

	// RED: an expired JIT grant authorizes nothing.
	lapsed := base
	past, _ := values.NewInstantFromUnix(1759990000, 0)
	lapsed.JITExpires = past
	if _, err := AuthorizeOperatorAction(lapsed, at); err == nil {
		t.Fatal("expired JIT grant authorized")
	}

	// GREEN: emergency execution records its declared bypass reason and
	// mandatory review without rewriting business truth.
	reviewBy, _ := values.NewInstantFromUnix(1760086400, 0)
	emergency := base
	emergency.Operation = OperationBreakGlass
	emergency.Simulated = false
	emergency.Approvers = []string{"operator-1", "operator-2"}
	emergency.Emergency = &EmergencyBypass{Reason: "cell unreachable, restore fencing", ReviewBy: reviewBy}
	rec, err = AuthorizeOperatorAction(emergency, at)
	if err != nil {
		t.Fatalf("AuthorizeOperatorAction(emergency): %v", err)
	}
	if !rec.Emergency {
		t.Fatal("emergency receipt does not declare its bypass")
	}
}

func mustInstant(t *testing.T, sec int64) values.Instant {
	t.Helper()
	instant, err := values.NewInstantFromUnix(sec, 0)
	if err != nil {
		t.Fatal(err)
	}
	return instant
}
