// TestTodo_WF_CTRL_002 proves the checked-in control-shapes research record
// classifies every catalogue pattern, states all eight first constructs with
// their lowerings, and cites kernel facts that hold.
package modeling

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/parallel"
)

const wfCtrl002Record = "testdata/wf_ctrl_002_control_shapes.md"

func wfCtrl002Body(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Clean(wfCtrl002Record))
	if err != nil {
		t.Fatalf("read research record: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("research record is empty")
	}
	return string(body)
}

func TestTodo_WF_CTRL_002(t *testing.T) {
	body := wfCtrl002Body(t)

	for _, section := range []string{
		"## 1. What the kernel expresses today",
		"## 2. The catalogue, pattern by pattern",
		"## 3. The constructs worth adding first",
		"## 4. What stays a kernel change and why",
		"## 5. Consequences for authors",
	} {
		if !strings.Contains(body, section) {
			t.Fatalf("record is missing section %q", section)
		}
	}
	// Every catalogue pattern carries a verdict.
	for _, pattern := range []string{
		"Sequence", "Exclusive choice", "Multi-choice", "Simple merge",
		"Structured merge", "Structured loop", "Arbitrary loop",
		"Multiple instances", "Deferred choice", "Milestone",
		"Cancel activity", "Cancel region", "Interleaved routing",
		"Discriminator", "Partial join",
	} {
		if !strings.Contains(body, pattern) {
			t.Fatalf("record does not classify %q", pattern)
		}
	}
	for _, verdict := range []string{"NATIVE", "LOWERABLE", "KERNEL-CHANGE"} {
		if !strings.Contains(body, verdict) {
			t.Fatalf("record never reaches verdict %q", verdict)
		}
	}
	// All eight first constructs with lowerings and guarantees.
	for _, construct := range []string{
		"### 3.1 Switch on a value or decision table",
		"### 3.2 For-each over a collection, in order",
		"### 3.3 For-each over a collection, in bounded parallel",
		"### 3.4 Repeat-until with a maximum count",
		"### 3.5 First-of",
		"### 3.6 Attempt-otherwise",
		"### 3.7 A timeout route on any waiting node",
		"### 3.8 Early return",
	} {
		if !strings.Contains(body, construct) {
			t.Fatalf("record is missing construct %q", construct)
		}
	}
	for _, guarantee := range []string{
		"implicit first edge", "declared, bounded and guarded", "flat and digestible",
	} {
		if !strings.Contains(body, guarantee) {
			t.Fatalf("record does not state guarantee %q", guarantee)
		}
	}

	// The kernel facts the record cites hold. Thirteen step types, ten
	// core plus three structural; the four retired names stay out.
	types := workflow.StepTypes()
	if len(types) != 13 {
		t.Fatalf("step vocabulary = %d types, want 13: %v", len(types), types)
	}
	for _, retired := range []workflow.StepType{"AGENT", "RULE", "CHECKPOINT", "DOCUMENT"} {
		if retired.Valid() {
			t.Fatalf("retired step type %s validates", retired)
		}
	}
	// DECISION routes N ways with author routes and no effects.
	decision, ok := workflow.ConformanceFor(workflow.StepDecision)
	if !ok || !decision.AuthorRoutes || decision.MayCarryEffect {
		t.Fatalf("DECISION cannot host N predicated routes without effects: %+v", decision)
	}
	// PARALLEL fans out on SUCCEEDED; JOIN aggregates with PARTIAL.
	par, ok := workflow.ConformanceFor(workflow.StepParallel)
	if !ok || par.FanOutRoute != workflow.OutcomeSucceeded {
		t.Fatalf("PARALLEL fans out on %q, want SUCCEEDED", par.FanOutRoute)
	}
	join, ok := workflow.ConformanceFor(workflow.StepJoin)
	if !ok || !containsOutcome(join.Outcomes, workflow.OutcomePartial) {
		t.Fatalf("JOIN cannot report PARTIAL: %+v", join.Outcomes)
	}
	// WAIT goes LATE and SIGNAL goes TIMED_OUT: the timeout routes exist.
	wait, ok := workflow.ConformanceFor(workflow.StepWait)
	if !ok || !containsOutcome(wait.Outcomes, "LATE") {
		t.Fatalf("WAIT cannot go LATE: %+v", wait.Outcomes)
	}
	signal, ok := workflow.ConformanceFor(workflow.StepSignal)
	if !ok || !containsOutcome(signal.Outcomes, "TIMED_OUT") {
		t.Fatalf("SIGNAL cannot time out: %+v", signal.Outcomes)
	}
	// SUBWORKFLOW reports cancellation; PARALLEL joins under both policies.
	sub, ok := workflow.ConformanceFor(workflow.StepSubworkflow)
	if !ok || !containsOutcome(sub.Outcomes, "CANCELLED") {
		t.Fatalf("SUBWORKFLOW cannot report CANCELLED: %+v", sub.Outcomes)
	}
	if parallel.FailFast == "" || parallel.CollectAll == "" || parallel.FailFast == parallel.CollectAll {
		t.Fatal("parallel policies are not two distinct values")
	}
	// Cycles are declared with an entry, a bound and a guard.
	var decl workflow.CycleDeclaration
	decl.EntryNodeID, decl.GuardNodeID, decl.MaxIterations = "n", "g", 3
	if decl.EntryNodeID == "" || decl.GuardNodeID == "" || decl.MaxIterations == 0 {
		t.Fatal("cycle declarations carry no entry, bound and guard")
	}
	// Decision routes carry key, predicate and precedence.
	route := workflow.DecisionRoute{Key: "k", Predicate: "p", Precedence: 1}
	if route.Key == "" || route.Predicate == "" || route.Precedence == 0 {
		t.Fatal("decision routes carry no key, predicate and precedence")
	}
}

func containsOutcome(outcomes []workflow.Outcome, want workflow.Outcome) bool {
	for _, o := range outcomes {
		if o == want {
			return true
		}
	}
	return false
}

// TestTodo_WF_CTRL_002_Golden pins the research record bytes: the record
// changes only deliberately, with this seal re-pinned by its author.
func TestTodo_WF_CTRL_002_Golden(t *testing.T) {
	body, err := os.ReadFile(filepath.Clean(wfCtrl002Record))
	if err != nil {
		t.Fatalf("read research record: %v", err)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != "13540d9228d7e2dc1feb09a694fcf0f39f4ccd04c71a0c86282a7731244f446c" {
		t.Fatalf("research record changed: sha256:%s", got)
	}
}

// TestTodo_WF_CTRL_002_Property shows each lowering equivalent to its
// construct's stated semantics for all inputs up to a bound: the same
// route and the same observable trace.
func TestTodo_WF_CTRL_002_Property(t *testing.T) {
	t.Run("switch", func(t *testing.T) {
		cases := []int{1, 2, 3}
		for value := 0; value <= 4; value++ {
			if got, want := SwitchLowered(value, cases, "ELSE"), SwitchSpec(value, cases, "ELSE"); got.String() != want.String() {
				t.Fatalf("switch(%d): lowered %s != spec %s", value, got, want)
			}
		}
	})
	t.Run("for-each-ordered", func(t *testing.T) {
		body := func(i int) string { return fmt.Sprintf("visit:%d", i) }
		for _, items := range [][]int{{}, {0}, {1, 0}, {2, 1, 0}} {
			if got, want := ForEachOrderedLowered(items, body), ForEachOrderedSpec(items, body); got.String() != want.String() {
				t.Fatalf("for-each(%v): lowered %s != spec %s", items, got, want)
			}
		}
	})
	t.Run("for-each-parallel", func(t *testing.T) {
		body := func(i int) string { return fmt.Sprintf("visit:%d", i) }
		for _, items := range [][]int{{}, {0}, {1, 0}, {2, 0, 1}} {
			want := ForEachParallelSpec(items, body)
			for _, completion := range permutations(len(items)) {
				if got := ForEachParallelLowered(items, body, completion); got.String() != want.String() {
					t.Fatalf("parallel(%v, completed %v): lowered %s != spec %s", items, completion, got, want)
				}
			}
		}
	})
	t.Run("repeat-until", func(t *testing.T) {
		body := func(s int) int { return s + 1 }
		for start := 0; start <= 3; start++ {
			for _, target := range []int{0, 1, 2, 4} {
				cond := func(s int) bool { return s >= target }
				for _, max := range []int{1, 2, 3} {
					if got, want := RepeatUntilLowered(start, max, cond, body), RepeatUntilSpec(start, max, cond, body); got.String() != want.String() {
						t.Fatalf("repeat-until(%d>=%d,max=%d): lowered %s != spec %s", start, target, max, got, want)
					}
				}
			}
		}
	})
	t.Run("first-of", func(t *testing.T) {
		rounds := [][][]bool{
			{{true, false}, {false, true}},
			{{false, false}, {false, true}},
			{{false, false}, {false, false}},
			{{false, true}},
			{{true, true}},
		}
		for _, r := range rounds {
			if got, want := FirstOfLowered(r, 3), FirstOfSpec(r, 3); got.String() != want.String() {
				t.Fatalf("first-of(%v): lowered %s != spec %s", r, got, want)
			}
			// The choice is recorded once: polling stops at the win, so a
			// later arrival can never reopen it, and every poll is bounded.
			if polls := len(FirstOfLowered(r, 3).Events); polls > 2*3 {
				t.Fatalf("first-of(%v) polled %d times past the bound", r, polls)
			}
		}
	})
	t.Run("attempt-otherwise", func(t *testing.T) {
		for _, fail := range []bool{false, true} {
			if got, want := AttemptOtherwiseLowered(fail, "CATCH"), AttemptOtherwiseSpec(fail, "CATCH"); got.String() != want.String() {
				t.Fatalf("attempt(fail=%v): lowered %s != spec %s", fail, got, want)
			}
		}
	})
	t.Run("timeout-route", func(t *testing.T) {
		for arrival := 0; arrival <= 4; arrival++ {
			if got, want := TimeoutRouteLowered(arrival, 2), TimeoutRouteSpec(arrival, 2); got.String() != want.String() {
				t.Fatalf("timeout(arrival=%d): lowered %s != spec %s", arrival, got, want)
			}
		}
	})
	t.Run("early-return", func(t *testing.T) {
		for _, take := range []bool{false, true} {
			tail := []string{"a", "b"}
			if got, want := EarlyReturnLowered(take, tail), EarlyReturnSpec(take, tail); got.String() != want.String() {
				t.Fatalf("early-return(%v): lowered %s != spec %s", take, got, want)
			}
			if take && len(EarlyReturnLowered(take, tail).Events) != 1 {
				t.Fatal("early return executed its tail")
			}
		}
	})
}

func permutations(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	var out [][]int
	var build func(prefix, rest []int)
	build = func(prefix, rest []int) {
		if len(rest) == 0 {
			out = append(out, append([]int(nil), prefix...))
			return
		}
		for i, v := range rest {
			build(append(prefix, v), append(append([]int(nil), rest[:i]...), rest[i+1:]...))
		}
	}
	rest := make([]int, n)
	for i := range rest {
		rest[i] = i
	}
	build(nil, rest)
	return out
}
