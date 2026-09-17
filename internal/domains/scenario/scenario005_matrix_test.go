package scenario

import (
	"testing"
)

// TestTodo_SCENARIO_005_Property: approval is deterministic, bound to the
// exact revision and decision, and immutable — later input edits never
// move a frozen Plan.
func TestTodo_SCENARIO_005_Property(t *testing.T) {
	revision := baseScenario(t)
	approval := approval005(revision)
	now := planInstant("2026-01-15T12:00:00Z")

	first := mustApprove005(t, revision, approval, now)
	second := mustApprove005(t, revision, approval, now)
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("identical approval inputs replayed to different digests")
	}
	if first.RevisionDigest != revision.CanonicalDigest {
		t.Fatal("plan did not preserve the revision digest")
	}
	if !first.Decision.Valid() {
		t.Fatalf("decision outside the closed vocabulary: %v", first.Decision)
	}

	// The decision is part of the binding: recording a rejection instead
	// of an approval moves the digest.
	denied := approval005(revision)
	denied.Decision = PlanRejected
	deniedPlan := mustApprove005(t, revision, denied, now)
	if deniedPlan.Decision != PlanRejected {
		t.Fatalf("plan decision = %v", deniedPlan.Decision)
	}
	if deniedPlan.CanonicalDigest == first.CanonicalDigest {
		t.Fatal("approval and rejection share a digest")
	}
	if err := deniedPlan.Validate(); err != nil {
		t.Fatal(err)
	}

	// Immutability: editing the caller's revision and approval after the
	// fact never moves the frozen Plan. The tampered revision no longer
	// validates against its own digest, while the Plan still does.
	revision.Assumptions[0].ProvenanceRefs[0] = "tampered"
	approval.Statement = "tampered"
	if err := revision.Validate(); err == nil {
		t.Fatal("tampered revision still validates")
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
	again, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if again != first.CanonicalDigest {
		t.Fatal("plan digest is not stable")
	}
	if first.Assumptions[0].ProvenanceRefs[0] == "tampered" {
		t.Fatal("plan assumptions alias the caller slice")
	}
	if first.AssumptionsCopy()[0].Key != "headcount.target" {
		t.Fatalf("plan assumptions = %+v", first.Assumptions)
	}
}

// TestTodo_SCENARIO_005_Mutation: seeded semantic mutants are killed — a
// future-dated decision, a missing approver or authority, and a tampered
// plan digest all refuse or fail validation.
func TestTodo_SCENARIO_005_Mutation(t *testing.T) {
	revision := baseScenario(t)
	now := planInstant("2026-01-15T12:00:00Z")

	// The decision cannot come from the future: DecidedAt past the
	// injected clock refuses.
	future := approval005(revision)
	future.DecidedAt = planInstant("2026-02-01T00:00:00Z")
	if _, err := ApprovePlan(revision, future, now); err == nil {
		t.Fatal("future-dated decision approved without error")
	}

	// A missing approver or authority refuses.
	nameless := approval005(revision)
	nameless.Approver = ""
	if _, err := ApprovePlan(revision, nameless, now); err == nil {
		t.Fatal("approver-less decision approved without error")
	}
	unauthorized := approval005(revision)
	unauthorized.AuthorityRef = ""
	if _, err := ApprovePlan(revision, unauthorized, now); err == nil {
		t.Fatal("authority-less decision approved without error")
	}

	// A tampered plan digest fails validation instead of verifying.
	plan := mustApprove005(t, revision, approval005(revision), now)
	tampered := plan
	tampered.Approver = "director-9"
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered plan validated without error")
	}
	if plan.Approver != "director-2" {
		t.Fatal("validation probe mutated the plan")
	}

	// The plan validity is exactly the approved horizon, never wider.
	if plan.Validity.String() != revision.Horizon.String() {
		t.Fatalf("plan validity = %v", plan.Validity)
	}
}
