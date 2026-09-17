package eligibility_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// elig006Prior evaluates the shared fixture plan to an ELIGIBLE baseline.
func elig006Prior(t *testing.T) (eligibility.Request, eligibility.Result) {
	t.Helper()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	facts := newFakeFacts().with(subject(1), "grade", values.Value("P3"))
	rules := newFakeRules().with(subject(1), "manager-attestation", "1", eligibility.RuleOutcomePass)
	prior, err := eligibility.Evaluate(context.Background(), facts, rules, req, plan)
	if err != nil {
		t.Fatal(err)
	}
	if prior.Status != eligibility.StatusEligible {
		t.Fatalf("baseline status = %s, want ELIGIBLE", prior.Status)
	}
	return req, prior
}

// TestTodo_ELIG_006 is the RED contract: a material fact change that leaves
// the stale result consumable must be rejected with ELIG_006_REJECTED naming
// the offending field/state/version, and must emit no reevaluation value.
func TestTodo_ELIG_006(t *testing.T) {
	_, prior := elig006Prior(t)
	plan := mustPlan(t, validCriteria())
	facts := newFakeFacts().with(subject(1), "grade", values.Value("P3"))
	rules := newFakeRules().with(subject(1), "manager-attestation", "1", eligibility.RuleOutcomePass)

	// Seeded defect: the fact snapshot moved but the caller reuses the stale
	// request, leaving the old ELIGIBLE result consumable.
	stale := validRequest()
	changes := []eligibility.InputChange{{Kind: eligibility.ChangeFact, Ref: "grade", FromVersion: "fact-snap-1", ToVersion: "fact-snap-2"}}
	got, err := eligibility.Reevaluate(context.Background(), facts, rules, stale, prior, plan, changes)
	var rej *eligibility.ReevaluationRejection
	if !errors.As(err, &rej) || rej.Code != "ELIG_006_REJECTED" {
		t.Fatalf("err=%v, want ELIG_006_REJECTED", err)
	}
	if rej.Field == "" || rej.State == "" || rej.Version == "" {
		t.Fatalf("rejection hides provenance: %+v", rej)
	}
	if !errors.Is(err, eligibility.ErrReevaluationRejected) {
		t.Fatalf("err=%v does not match ErrReevaluationRejected", err)
	}
	if !reflect.DeepEqual(got, eligibility.Reevaluation{}) {
		t.Fatalf("rejected reevaluation emitted a value: %+v", got)
	}

	// Declared change pinned into the new request reevaluates exactly once.
	next := validRequest()
	next.Snapshots.FactSnapshotRef = "fact-snap-2"
	changed, err := eligibility.Reevaluate(context.Background(), facts, rules, next, prior, plan, changes)
	if err != nil {
		t.Fatalf("pinned reevaluation: %v", err)
	}
	if changed.PriorStatus != eligibility.StatusEligible || changed.CurrentStatus != eligibility.StatusEligible {
		t.Fatalf("statuses = %s/%s, want ELIGIBLE/ELIGIBLE", changed.PriorStatus, changed.CurrentStatus)
	}
	if changed.Changed || changed.ParticipationChanged {
		t.Fatal("identical outcome must not report a change")
	}
	if len(changed.Obligations) == 0 {
		t.Fatal("bounded reevaluation must carry obligations")
	}
}

// TestTodo_ELIG_006_Property proves every governed input kind reevaluates
// deterministically once pinned, and rejects when the new request omits the
// declared to-version.
func TestTodo_ELIG_006_Property(t *testing.T) {
	_, prior := elig006Prior(t)
	plan := mustPlan(t, validCriteria())
	facts := newFakeFacts().with(subject(1), "grade", values.Value("P3"))
	rules := newFakeRules().with(subject(1), "manager-attestation", "1", eligibility.RuleOutcomePass)

	cases := []struct {
		name   string
		change eligibility.InputChange
		pin    func(*eligibility.Request)
	}{
		{"fact", eligibility.InputChange{Kind: eligibility.ChangeFact, Ref: "grade", FromVersion: "fact-snap-1", ToVersion: "fact-snap-2"}, func(r *eligibility.Request) { r.Snapshots.FactSnapshotRef = "fact-snap-2" }},
		{"rule", eligibility.InputChange{Kind: eligibility.ChangeRule, Ref: "manager-attestation", FromVersion: "rule-snap-1", ToVersion: "rule-snap-2"}, func(r *eligibility.Request) { r.Snapshots.RuleSnapshotRef = "rule-snap-2" }},
		{"population", eligibility.InputChange{Kind: eligibility.ChangePopulation, Ref: "pool", FromVersion: "pop-snap-1", ToVersion: "pop-snap-2"}, func(r *eligibility.Request) { r.Snapshots.PopulationSnapshotRef = "pop-snap-2" }},
		{"program", eligibility.InputChange{Kind: eligibility.ChangeProgram, Ref: "spot-bonus", FromVersion: "2026.1", ToVersion: "2026.2"}, func(r *eligibility.Request) { r.SubjectMatter.Revision = "2026.2" }},
		{"time", eligibility.InputChange{Kind: eligibility.ChangeTime, Ref: "effective", FromVersion: "interval-1", ToVersion: "interval-2"}, func(r *eligibility.Request) { r.EffectiveInterval = mustInterval(t, 2_000_000, 3_000_000) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pinned := validRequest()
			tc.pin(&pinned)
			first, err := eligibility.Reevaluate(context.Background(), facts, rules, pinned, prior, plan, []eligibility.InputChange{tc.change})
			if err != nil {
				t.Fatalf("pinned: %v", err)
			}
			second, err := eligibility.Reevaluate(context.Background(), facts, rules, pinned, prior, plan, []eligibility.InputChange{tc.change})
			if err != nil {
				t.Fatalf("repinned: %v", err)
			}
			if first.CurrentDigest != second.CurrentDigest {
				t.Fatal("identical inputs produced different digests")
			}
			stale := validRequest()
			if _, err := eligibility.Reevaluate(context.Background(), facts, rules, stale, prior, plan, []eligibility.InputChange{tc.change}); !errors.Is(err, eligibility.ErrReevaluationRejected) {
				t.Fatalf("unpinnned change err=%v, want rejection", err)
			}
		})
	}
}

// TestTodo_ELIG_006_Mutation proves a tampered change descriptor or a forged
// prior cannot slip through as a clean reevaluation.
func TestTodo_ELIG_006_Mutation(t *testing.T) {
	_, prior := elig006Prior(t)
	plan := mustPlan(t, validCriteria())
	facts := newFakeFacts().with(subject(1), "grade", values.Value("P3"))
	rules := newFakeRules().with(subject(1), "manager-attestation", "1", eligibility.RuleOutcomePass)
	next := validRequest()
	next.Snapshots.FactSnapshotRef = "fact-snap-2"
	good := []eligibility.InputChange{{Kind: eligibility.ChangeFact, Ref: "grade", FromVersion: "fact-snap-1", ToVersion: "fact-snap-2"}}

	mutations := []struct {
		name    string
		changes []eligibility.InputChange
		prior   eligibility.Result
		req     eligibility.Request
	}{
		{"empty", nil, prior, next},
		{"unknown-kind", []eligibility.InputChange{{Kind: eligibility.ChangeKind(99), Ref: "grade", FromVersion: "fact-snap-1", ToVersion: "fact-snap-2"}}, prior, next},
		{"from-mismatch", []eligibility.InputChange{{Kind: eligibility.ChangeFact, Ref: "grade", FromVersion: "fact-snap-0", ToVersion: "fact-snap-2"}}, prior, next},
		{"invalid-prior", good, eligibility.Result{}, next},
		{"unbounded", append(append([]eligibility.InputChange(nil), good...), good...), prior, next},
	}
	// Grow the unbounded case past the documented bound.
	for len(mutations[4].changes) <= eligibility.MaxReevaluationChanges {
		mutations[4].changes = append(mutations[4].changes, good[0])
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := eligibility.Reevaluate(context.Background(), facts, rules, tc.req, tc.prior, plan, tc.changes); !errors.Is(err, eligibility.ErrReevaluationRejected) {
				t.Fatalf("err=%v, want ELIG_006_REJECTED", err)
			}
		})
	}

	// A genuine outcome flip is reported, never applied silently: the
	// reevaluation carries participation obligations.
	flippedFacts := newFakeFacts().with(subject(1), "grade", values.Value("P1"))
	flipped, err := eligibility.Reevaluate(context.Background(), flippedFacts, rules, next, prior, plan, good)
	if err != nil {
		t.Fatalf("flip: %v", err)
	}
	if !flipped.Changed || !flipped.ParticipationChanged {
		t.Fatalf("flip not reported: %+v", flipped)
	}
	if len(flipped.Obligations) == 0 {
		t.Fatal("participation change without obligations is a silent change")
	}
}
