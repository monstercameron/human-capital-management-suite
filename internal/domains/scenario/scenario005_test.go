package scenario

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func planInstant(text string) values.Instant {
	t, err := time.Parse(time.RFC3339, text)
	if err != nil {
		panic(err)
	}
	return values.NewInstant(t)
}

// approval005 builds an approval that matches baseScenario: the planner is
// the author, so the approver must be someone else.
func approval005(revision ScenarioRevision) PlanApproval {
	return PlanApproval{
		Approver: "director-2", AuthorityRef: "authority:approve-scenario", AuthorityVersion: "v3",
		AuthorityValidThrough: planInstant("2026-03-01T00:00:00Z"),
		ExpectedDigest:        revision.CanonicalDigest, ExpectedBaseline: revision.BaselineSnapshotRef,
		DecidedAt: planInstant("2026-01-15T12:00:00Z"), Decision: PlanApproved,
		Statement: "approved for planning; simulation only",
	}
}

func mustApprove005(t *testing.T, revision ScenarioRevision, approval PlanApproval, now values.Instant) Plan {
	t.Helper()
	plan, err := ApprovePlan(revision, approval, now)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// TestTodo_SCENARIO_005: approving a selected scenario freezes an immutable
// Plan bound to the exact revision, assumptions, scope, decision and
// validity. A changed scenario or baseline, a stale approver and a
// self-approval all fail with a typed rejection and change nothing.
func TestTodo_SCENARIO_005(t *testing.T) {
	revision := baseScenario(t)
	approval := approval005(revision)
	now := planInstant("2026-01-15T12:00:00Z")
	before := revision.CanonicalDigest

	plan := mustApprove005(t, revision, approval, now)
	if plan.ScenarioID != revision.ScenarioID || plan.Revision != revision.Revision ||
		plan.RevisionDigest != revision.CanonicalDigest {
		t.Fatalf("plan does not bind the exact revision: %+v", plan)
	}
	if plan.BaselineSnapshotRef != revision.BaselineSnapshotRef || plan.Scope != revision.Scope {
		t.Fatalf("plan does not bind baseline and scope: %+v", plan)
	}
	if len(plan.Assumptions) != len(revision.Assumptions) {
		t.Fatalf("plan assumptions = %+v", plan.Assumptions)
	}
	for i, assumption := range plan.Assumptions {
		if assumption.Key != revision.Assumptions[i].Key {
			t.Fatalf("plan assumptions = %+v", plan.Assumptions)
		}
	}
	if plan.Approver != approval.Approver || plan.Decision != PlanApproved ||
		plan.DecidedAt.Compare(approval.DecidedAt) != 0 {
		t.Fatalf("plan does not bind the decision: %+v", plan)
	}
	if plan.Validity.String() != revision.Horizon.String() {
		t.Fatalf("plan validity = %v, want the scenario horizon", plan.Validity)
	}
	if plan.CanonicalDigest == "" || plan.Explain() == "" {
		t.Fatal("plan carries no digest or summary")
	}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}

	// Changed scenario: the revision moved after review, so the approval
	// no longer describes it.
	moved, err := revision.Fork(Assumption{
		Key: "headcount.target", Value: DecimalValue(values.MustDecimal("14", 0, values.RoundingHalfEven)),
		Unit: "HEAD", ProvenanceRefs: []string{"plan:change-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ApprovePlan(moved, approval, now)
	rejected, ok := AsPlanRejected(err)
	if !ok {
		t.Fatalf("changed scenario err = %v, want SCENARIO_005_REJECTED", err)
	}
	if rejected.Code != PlanRejectedCode || rejected.Field != "revision" || rejected.State != "changed-since-review" || rejected.Version != Version() {
		t.Fatalf("rejected = %+v", rejected)
	}
	if !errors.Is(err, ErrPlanRejected) {
		t.Fatal("rejection does not match ErrPlanRejected with errors.Is")
	}

	// Changed baseline: the approval cites a different snapshot.
	staleBaseline := approval005(revision)
	staleBaseline.ExpectedBaseline = "snapshot:other"
	_, err = ApprovePlan(revision, staleBaseline, now)
	if rejected, ok := AsPlanRejected(err); !ok || rejected.Field != "baseline" || rejected.State != "mismatch" {
		t.Fatalf("changed baseline err = %v, rejected = %+v", err, rejected)
	}

	// Stale approver: the authority lapsed before the decision.
	staleAuthority := approval005(revision)
	staleAuthority.AuthorityValidThrough = planInstant("2026-01-01T00:00:00Z")
	_, err = ApprovePlan(revision, staleAuthority, now)
	if rejected, ok := AsPlanRejected(err); !ok || rejected.Field != "approval.authority" || rejected.State != "stale" {
		t.Fatalf("stale approver err = %v, rejected = %+v", err, rejected)
	}

	// Self-approval: the author cannot approve their own scenario.
	self := approval005(revision)
	self.Approver = revision.Author
	_, err = ApprovePlan(revision, self, now)
	if rejected, ok := AsPlanRejected(err); !ok || rejected.Field != "approval.approver" || rejected.State != "self-approval" {
		t.Fatalf("self-approval err = %v, rejected = %+v", err, rejected)
	}

	// Zero-effect: refused approvals change neither input; the revision
	// still validates with its original digest.
	if revision.CanonicalDigest != before {
		t.Fatal("refused approval mutated the revision")
	}
	if err := revision.Validate(); err != nil {
		t.Fatal(err)
	}
}
