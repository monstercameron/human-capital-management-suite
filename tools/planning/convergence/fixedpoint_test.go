package convergence

import (
	"strings"
	"testing"
)

func TestDiffNamesAddedRemovedAndChangedIdentities(t *testing.T) {
	prev := Report{Gaps: []Gap{
		{Identity: "GAP-a", Scope: ScopeSelected, Resolution: Resolution{Kind: ResolutionProposedTodo, TodoID: "CONV-1"}},
		{Identity: "GAP-b", Scope: ScopeSelected},
		{Identity: "GAP-c", Scope: ScopeUnknown},
	}}
	next := Report{Gaps: []Gap{
		{Identity: "GAP-a", Scope: ScopeSelected, Resolution: Resolution{Kind: ResolutionExistingTodo, TodoID: "CONV-1"}},
		{Identity: "GAP-c", Scope: ScopeUnknown},
		{Identity: "GAP-d", Scope: ScopeSelected},
	}}
	d := Diff(prev, next)
	if strings.Join(d.Added, ",") != "GAP-d" || strings.Join(d.Removed, ",") != "GAP-b" || strings.Join(d.ResolutionChanged, ",") != "GAP-a" || d.Empty() {
		t.Fatalf("delta = %+v", d)
	}
	if !Diff(next, next).Empty() {
		t.Fatal("self diff is not empty")
	}
}

func TestAdoptNeverMutatesItsInput(t *testing.T) {
	snap := fixtureSnapshot()
	proposals := Converge(snap).Proposals
	adopted := Adopt(snap, proposals)
	if len(snap.Todos) != 2 || len(snap.Claims) != 1 {
		t.Fatal("Adopt mutated the input snapshot")
	}
	if len(adopted.Todos) != 2+len(proposals) || len(adopted.Claims) != 1+len(proposals) {
		t.Fatalf("adopted todos=%d claims=%d", len(adopted.Todos), len(adopted.Claims))
	}
}

// TestRunToFixedPointDetectsAnImpureCompiler proves the RED clause
// "unchanged second pass produces new identities" is detected, not assumed:
// a compiler that mints one fresh identity per call is reported unstable.
func TestRunToFixedPointDetectsAnImpureCompiler(t *testing.T) {
	calls := 0
	impure := func(s Snapshot) Report {
		calls++
		s.Observations = append(append([]Observation(nil), s.Observations...), Observation{Compiler: "flaky", Code: "X", Owner: fxAlpha, Contract: ContractTest, Subject: strings.Repeat("n", calls)})
		return Converge(s)
	}
	fp := runToFixedPoint(fixtureSnapshot(), 1, impure)
	if fp.Stable || len(fp.Passes) != 3 {
		t.Fatalf("impure compiler judged stable in %d passes", len(fp.Passes))
	}
	joined := strings.Join(fp.Reasons, "\n")
	for _, want := range []string{"not byte-identical", "unchanged second pass added gap identities", "pass 2 added gap identities", "removed gap identities", "no two consecutive passes"} {
		if !strings.Contains(joined, want) {
			t.Errorf("reasons missing %q:\n%s", want, joined)
		}
	}
}

// TestRunToFixedPointDetectsReproposal proves an adoption that does not
// resolve its gap (a second todo for the same gap) is refused.
func TestRunToFixedPointDetectsReproposal(t *testing.T) {
	sticky := func(Snapshot) Report {
		return Converge(fixtureSnapshot()) // adoption is ignored, so the same proposals return
	}
	fp := runToFixedPoint(fixtureSnapshot(), 3, sticky)
	if fp.Stable || !strings.Contains(strings.Join(fp.Reasons, "\n"), "re-proposed already adopted todo") {
		t.Fatalf("re-proposal not detected: %v", fp.Reasons)
	}
}
