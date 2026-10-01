package parallel

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func joinResults() []BranchResult {
	return []BranchResult{
		{BranchID: "b1", IdempotencyKey: "k1", Outcome: OutcomeSucceeded},
		{BranchID: "b2", IdempotencyKey: "k2", Outcome: OutcomeSucceeded},
		{BranchID: "b3", IdempotencyKey: "k3", Outcome: OutcomeFailed},
		{BranchID: "b4", IdempotencyKey: "k4", Outcome: OutcomeUnknown},
	}
}

func TestTodo_WF_STEP_008(t *testing.T) {
	results := joinResults()
	// ALL over a mixed set reports failure, never success.
	all, err := Join(JoinPlan{Strategy: JoinAll, Version: "v1"}, results)
	if err != nil || all.Verdict != JoinFailed || all.Counted != 4 {
		t.Fatalf("all=%+v err=%v", all, err)
	}
	// Unknown without failure stays unknown: never coerced false.
	mixed := []BranchResult{
		{BranchID: "b1", IdempotencyKey: "k1", Outcome: OutcomeSucceeded},
		{BranchID: "b4", IdempotencyKey: "k4", Outcome: OutcomeUnknown},
	}
	unknownAll, err := Join(JoinPlan{Strategy: JoinAll, Version: "v1"}, mixed)
	if err != nil || unknownAll.Verdict != JoinUnknown || len(unknownAll.Unknown) != 1 || unknownAll.Unknown[0] != "b4" {
		t.Fatalf("unknownAll=%+v err=%v", unknownAll, err)
	}
	// ANY succeeds partially with one success among failures.
	any, err := Join(JoinPlan{Strategy: JoinAny, Version: "v1"}, results)
	if err != nil || any.Verdict != JoinPartial || !any.Degraded {
		t.Fatalf("any=%+v err=%v", any, err)
	}
	// QUORUM(2) holds with two successes; QUORUM(3) fails honestly.
	quorum, err := Join(JoinPlan{Strategy: JoinQuorum, Version: "v1", Quorum: 2}, results)
	if err != nil || quorum.Verdict != JoinPartial {
		t.Fatalf("quorum=%+v err=%v", quorum, err)
	}
	// REQUIRED_SET enforces its mandatory branches.
	required, err := Join(JoinPlan{Strategy: JoinRequiredSet, Version: "v1", RequiredID: []string{"b1", "b2"}}, results)
	if err != nil || required.Verdict != JoinPartial || !required.Degraded {
		t.Fatalf("required=%+v err=%v", required, err)
	}
	// BEST_EFFORT degrades instead of failing while anything succeeded.
	best, err := Join(JoinPlan{Strategy: JoinBestEffort, Version: "v1"}, results)
	if err != nil || best.Verdict != JoinPartial || len(best.Unknown) != 1 {
		t.Fatalf("best=%+v err=%v", best, err)
	}
	// RED: missing mandatory branches and impossible quorums never succeed.
	if _, err := Join(JoinPlan{Strategy: JoinRequiredSet, Version: "v1", RequiredID: []string{"ghost"}}, results); !errors.Is(err, ErrJoinMissing) {
		t.Fatalf("missing required err = %v", err)
	}
	if _, err := Join(JoinPlan{Strategy: JoinQuorum, Version: "v1", Quorum: 9}, results); err == nil {
		t.Fatal("impossible quorum joined")
	}
	if _, err := Join(JoinPlan{Strategy: JoinQuorum, Version: "v1"}, results); err == nil {
		t.Fatal("zero quorum joined")
	}
	if _, err := Join(JoinPlan{Strategy: "PLURALITY", Version: "v1"}, results); err == nil {
		t.Fatal("unknown strategy joined")
	}
	if _, err := Join(JoinPlan{Strategy: JoinAll}, results); err == nil {
		t.Fatal("unversioned plan joined")
	}
	if _, err := Join(JoinPlan{Strategy: JoinAll, Version: "v1"}, nil); err == nil {
		t.Fatal("empty results joined")
	}
	rogue := []BranchResult{{BranchID: "bx", IdempotencyKey: "kx", Outcome: "EVAPORATED"}}
	if _, err := Join(JoinPlan{Strategy: JoinAll, Version: "v1"}, rogue); err == nil {
		t.Fatal("off-vocabulary outcome joined")
	}
}

func TestTodo_WF_STEP_008_Golden(t *testing.T) {
	results := joinResults()
	var lines []string
	for _, plan := range []JoinPlan{
		{Strategy: JoinAll, Version: "v1"},
		{Strategy: JoinAny, Version: "v1"},
		{Strategy: JoinQuorum, Version: "v1", Quorum: 2},
		{Strategy: JoinRequiredSet, Version: "v1", RequiredID: []string{"b1", "b2"}},
		{Strategy: JoinBestEffort, Version: "v1"},
	} {
		outcome, err := Join(plan, results)
		if err != nil {
			t.Fatalf("Join(%s): %v", plan.Strategy, err)
		}
		lines = append(lines, plan.Strategy+"="+outcome.Verdict+"|degraded="+boolString(outcome.Degraded)+"|unknown="+strings.Join(outcome.Unknown, ","))
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "wf_step008_join.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestTodo_WF_STEP_008_Mutation(t *testing.T) {
	base := joinResults()
	plan := JoinPlan{Strategy: JoinAll, Version: "v1"}
	before, err := Join(plan, base)
	if err != nil || before.Verdict != JoinFailed {
		t.Fatalf("before=%+v err=%v", before, err)
	}
	// Repairing the failed branch flips ALL to UNKNOWN: the unknown
	// member still refuses to report success.
	repaired := joinResults()
	repaired[2].Outcome = OutcomeSucceeded
	after, err := Join(plan, repaired)
	if err != nil || after.Verdict != JoinUnknown {
		t.Fatalf("after=%+v err=%v", after, err)
	}
	// Resolving the unknown member too finally reports success.
	resolved := joinResults()
	resolved[2].Outcome = OutcomeSucceeded
	resolved[3].Outcome = OutcomeSucceeded
	complete, err := Join(plan, resolved)
	if err != nil || complete.Verdict != JoinSucceeded || complete.Degraded {
		t.Fatalf("complete=%+v err=%v", complete, err)
	}
	// QUORUM flips at its boundary.
	quorum := JoinPlan{Strategy: JoinQuorum, Version: "v1", Quorum: 2}
	clean := []BranchResult{
		{BranchID: "b1", IdempotencyKey: "k1", Outcome: OutcomeSucceeded},
		{BranchID: "b2", IdempotencyKey: "k2", Outcome: OutcomeFailed},
		{BranchID: "b3", IdempotencyKey: "k3", Outcome: OutcomeFailed},
	}
	short, err := Join(quorum, clean)
	if err != nil || short.Verdict != JoinFailed {
		t.Fatalf("short=%+v err=%v", short, err)
	}
	clean[1].Outcome = OutcomeSucceeded
	met, err := Join(quorum, clean)
	if err != nil || met.Verdict != JoinPartial {
		t.Fatalf("met=%+v err=%v", met, err)
	}
}

func TestTodo_CONF_022_ParallelServedPath(t *testing.T) {
	if ServingContractID == "" {
		t.Fatal("served contract id is empty")
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("serving contract: %v", err)
	}
}
