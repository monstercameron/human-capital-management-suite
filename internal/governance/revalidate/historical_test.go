package revalidate_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/revalidate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// historicalBase is every input GOVERN-002 composes that revalidation does not
// re-read.
func historicalBase() decision.Inputs {
	return decision.Inputs{
		ProposalRevisionDigest: "sha256:proposal",
		Context: decision.Context{
			Principal: "principal:operator", Delegation: "delegation:instance", Capability: "promotion.execute",
			Resource: "worker:1", Fields: []string{"assignment.job_code"}, CurrentOrganization: "org:people-ops",
			TargetOrganization: "org:people-ops", Purpose: "compensation_review", Risk: "R3",
			Authority: "sha256:source|authority.local_master/v1", Legal: "sha256:legal",
		},
		ControlSnapshot: decision.ControlSnapshot{
			Digest: "sha256:control", Capability: "sha256:capability", PolicyBundle: "sha256:policy",
			LegalContext: "sha256:legal", Classification: "sha256:classification",
		},
		ApprovalRequirements: []decision.ApprovalRequirement{
			{ID: "approval:finance", Version: "1", Satisfaction: decision.ApprovalSatisfied},
			{ID: "approval:manager", Version: "1", Satisfaction: decision.ApprovalSatisfied},
		},
	}
}

func historicalFacts() revalidate.Facts {
	return revalidate.Facts{
		AuthZ:               revalidate.AuthZFact{Effect: decision.Allow, PolicyVersion: "sha256:policy"},
		Session:             revalidate.SessionFact{Effect: decision.Allow, SessionID: "session-1", Assurance: "substantial"},
		SourceAuthority:     revalidate.SourceAuthorityFact{Decision: "sha256:source|authority.local_master/v1"},
		FieldClassification: revalidate.FieldClassificationFact{Version: "sha256:classification"},
		LegalPolicy:         revalidate.LegalPolicyFact{LegalPackVersion: "sha256:legal", PolicyPackVersion: "sha256:policy"},
		BudgetPosition: revalidate.BudgetPositionFact{
			Budget:   revalidate.ResourceFact{Effect: decision.Allow, ObservationID: "budget:held"},
			Position: revalidate.ResourceFact{Effect: decision.Allow, ObservationID: "position:open"},
		},
		Conflict: revalidate.ConflictFact{Effect: decision.Allow, Classification: "NO_CONFLICT", EvidenceRef: "conflict:baseline"},
	}
}

func testClock() revalidate.Clock {
	return func() values.Instant { return values.NewInstant(time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)) }
}

// TestNewHistoricalApprovalIsRevalidatedByItsOwnConstruction proves a record
// built by this package confirms against an unchanged world, and routes the
// typed requirement for each kind of change: a moved budget or conflict fact
// replans, a moved control version reapproves, and a fact that no longer
// allows blocks.
func TestNewHistoricalApprovalIsRevalidatedByItsOwnConstruction(t *testing.T) {
	const planDigest = "sha256:plan"
	record, err := revalidate.NewHistoricalApproval(historicalBase(), historicalFacts(), planDigest)
	if err != nil {
		t.Fatalf("NewHistoricalApproval: %v", err)
	}
	if !record.Allows() || record.Decision.Digest == "" || record.PlanDigest != planDigest {
		t.Fatalf("record = %+v, want an allowing decision bound to the plan", record.Decision.State)
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	confirmed, err := revalidate.Revalidate(testClock(), record, historicalFacts(), planDigest)
	if err != nil || !confirmed.Confirmed || confirmed.Requirement != revalidate.RequirementNone {
		t.Fatalf("unchanged world = %+v, %v; want a confirmation", confirmed, err)
	}

	for name, tc := range map[string]struct {
		mutate func(*revalidate.Facts)
		want   revalidate.RequirementKind
		input  revalidate.ChangedInput
	}{
		"a re-baselined budget observation": {
			mutate: func(f *revalidate.Facts) { f.BudgetPosition.Budget.ObservationID = "budget:rebaselined" },
			want:   revalidate.RequirementReplanRequired, input: revalidate.ChangedBudgetPosition,
		},
		"an intervening write": {
			mutate: func(f *revalidate.Facts) { f.Conflict.Classification = "INTERVENING_WRITE" },
			want:   revalidate.RequirementReplanRequired, input: revalidate.ChangedConflict,
		},
		"a republished policy bundle": {
			mutate: func(f *revalidate.Facts) { f.LegalPolicy.PolicyPackVersion = "sha256:policy-2" },
			want:   revalidate.RequirementReapprovalRequired, input: revalidate.ChangedLegalPolicyVersion,
		},
		"a revoked authorization": {
			mutate: func(f *revalidate.Facts) { f.AuthZ.Effect = decision.Deny },
			want:   revalidate.RequirementBlock, input: revalidate.ChangedAuthZ,
		},
		"a released budget hold": {
			mutate: func(f *revalidate.Facts) { f.BudgetPosition.Budget.Effect = decision.Deny },
			want:   revalidate.RequirementBlock, input: revalidate.ChangedBudgetPosition,
		},
	} {
		t.Run(name, func(t *testing.T) {
			current := historicalFacts()
			tc.mutate(&current)
			result, err := revalidate.Revalidate(testClock(), record, current, planDigest)
			if err != nil {
				t.Fatalf("Revalidate: %v", err)
			}
			if result.Confirmed || result.Requirement != tc.want {
				t.Fatalf("result = %+v, want %s", result, tc.want)
			}
			found := false
			for _, changed := range result.ChangedInputs {
				found = found || changed == tc.input
			}
			if !found {
				t.Fatalf("changed inputs = %v, want %s", result.ChangedInputs, tc.input)
			}
		})
	}

	// A plan recompiled since the approval refuses on that binding alone.
	if _, err := revalidate.Revalidate(testClock(), record, historicalFacts(), "sha256:plan-2"); err != nil {
		t.Fatalf("Revalidate against another plan: %v", err)
	} else if result, _ := revalidate.Revalidate(testClock(), record, historicalFacts(), "sha256:plan-2"); result.VerifyBoundPlan(planDigest) == nil {
		t.Fatal("a result computed for another plan verified against the original")
	}
}

// TestNewHistoricalApprovalRefusesIncompleteOrBlockingInput proves the
// constructor refuses an incomplete fact set and an unbound plan, and that a
// record whose composed decision denies is reported as not allowing rather
// than as an error.
func TestNewHistoricalApprovalRefusesIncompleteOrBlockingInput(t *testing.T) {
	if _, err := revalidate.NewHistoricalApproval(historicalBase(), revalidate.Facts{}, "sha256:plan"); !errors.Is(err, revalidate.ErrInvalidInput) {
		t.Fatalf("empty facts = %v, want ErrInvalidInput", err)
	}
	if _, err := revalidate.NewHistoricalApproval(historicalBase(), historicalFacts(), " "); !errors.Is(err, revalidate.ErrInvalidInput) {
		t.Fatalf("unbound plan = %v, want ErrInvalidInput", err)
	}
	denied := historicalFacts()
	denied.BudgetPosition.Budget.Effect = decision.Deny
	record, err := revalidate.NewHistoricalApproval(historicalBase(), denied, "sha256:plan")
	if err != nil {
		t.Fatalf("NewHistoricalApproval (denied): %v", err)
	}
	if record.Allows() {
		t.Fatalf("a denied decision reports as allowing: %s", record.Decision.State)
	}
	result, err := revalidate.Revalidate(testClock(), record, denied, "sha256:plan")
	if err != nil || !result.Confirmed {
		t.Fatalf("an unchanged denial = %+v, %v; want a confirmed (still blocking) recomposition", result, err)
	}
	tampered := record
	tampered.Decision.Digest = "sha256:tampered"
	if _, err := revalidate.Revalidate(testClock(), tampered, denied, "sha256:plan"); !errors.Is(err, revalidate.ErrTampered) {
		t.Fatalf("tampered record = %v, want ErrTampered", err)
	}
}
