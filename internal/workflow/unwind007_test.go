// WF-REV-007 RED: the compiler accepts a write node with no
// compensation or cancellation behaviour, and no plan records what
// cancelling at a given point would undo.
package workflow_test

import (
	"errors"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func rev007Graph() []workflow.WriteNode {
	return []workflow.WriteNode{
		{NodeID: "draft", Effect: workflow.EffectRecord{ID: "e-draft", Reversible: true}},
		{NodeID: "reserve", Effect: workflow.EffectRecord{ID: "e-reserve", Reversible: true}, After: []string{"draft"}, AtomicRegion: "booking"},
		{NodeID: "charge", Effect: workflow.EffectRecord{ID: "e-charge", Compensation: "refund-charge"}, After: []string{"reserve"}, AtomicRegion: "booking"},
		{NodeID: "notify", Effect: workflow.EffectRecord{ID: "e-notify", Reversible: true}, After: []string{"charge"}},
	}
}

func rev007Points() []workflow.SafePoint {
	return []workflow.SafePoint{
		{ID: "sp-after-reserve", ThroughNode: "reserve"},
		{ID: "sp-after-notify", ThroughNode: "notify"},
	}
}

// TestTodo_WF_REV_007 is the PRIMARY test: the compiled plan carries, for
// each safe point, the effects committed by then and their
// reverse-dependency unwind order with atomic regions grouped; a write
// block without a reversal contract is a compile error.
func TestTodo_WF_REV_007(t *testing.T) {
	plan, err := workflow.CompileUnwindPlan(rev007Graph(), rev007Points())
	if err != nil {
		t.Fatalf("CompileUnwindPlan: %v", err)
	}

	t.Run("each safe point names its committed effects", func(t *testing.T) {
		if len(plan.Points) != 2 {
			t.Fatalf("points = %d, want 2", len(plan.Points))
		}
		sp1 := plan.Points[0]
		if sp1.SafePointID != "sp-after-reserve" || len(sp1.Committed) != 2 ||
			sp1.Committed[0].NodeID != "draft" || sp1.Committed[1].NodeID != "reserve" {
			t.Fatalf("sp1 committed = %+v", sp1.Committed)
		}
		sp2 := plan.Points[1]
		if len(sp2.Committed) != 4 || sp2.Committed[3].NodeID != "notify" {
			t.Fatalf("sp2 committed = %+v", sp2.Committed)
		}
	})

	t.Run("unwind runs reverse with atomic regions grouped", func(t *testing.T) {
		sp2 := plan.Points[1]
		if len(sp2.Unwind) != 3 {
			t.Fatalf("sp2 groups = %+v, want 3 (notify, booking, draft)", sp2.Unwind)
		}
		if sp2.Unwind[0].Region != "" || sp2.Unwind[0].Effects[0].NodeID != "notify" {
			t.Fatalf("group 0 = %+v, want notify singleton", sp2.Unwind[0])
		}
		booking := sp2.Unwind[1]
		if booking.Region != "booking" || len(booking.Effects) != 2 ||
			booking.Effects[0].NodeID != "charge" || booking.Effects[1].NodeID != "reserve" {
			t.Fatalf("booking group = %+v, want [charge reserve]", booking)
		}
		if sp2.Unwind[2].Effects[0].NodeID != "draft" {
			t.Fatalf("group 2 = %+v, want draft", sp2.Unwind[2])
		}
		if booking.Effects[0].Action != "COMPENSATE:refund-charge" {
			t.Fatalf("charge action = %q, want COMPENSATE:refund-charge", booking.Effects[0].Action)
		}
		if booking.Effects[1].Action != "REVERT" {
			t.Fatalf("reserve action = %q, want REVERT", booking.Effects[1].Action)
		}
	})

	t.Run("a write without a reversal contract is a compile error", func(t *testing.T) {
		nodes := rev007Graph()
		nodes[2].Effect.Compensation = ""
		if _, err := workflow.CompileUnwindPlan(nodes, rev007Points()); !errors.Is(err, workflow.ErrNoReversalContract) {
			t.Fatalf("err = %v, want ErrNoReversalContract", err)
		}
	})

	t.Run("cycles, unknown refs and split regions fail closed", func(t *testing.T) {
		cyclic := rev007Graph()
		cyclic[0].After = []string{"notify"}
		if _, err := workflow.CompileUnwindPlan(cyclic, rev007Points()); !errors.Is(err, workflow.ErrUnwindCycle) {
			t.Fatalf("cycle err = %v, want ErrUnwindCycle", err)
		}
		dangling := rev007Graph()
		dangling[1].After = []string{"ghost"}
		if _, err := workflow.CompileUnwindPlan(dangling, rev007Points()); !errors.Is(err, workflow.ErrUnwindInvalid) {
			t.Fatalf("dangling err = %v, want ErrUnwindInvalid", err)
		}
		split := rev007Graph()
		split[2].AtomicRegion = ""
		split[3].AtomicRegion = "booking"
		if _, err := workflow.CompileUnwindPlan(split, rev007Points()); !errors.Is(err, workflow.ErrRegionSplit) {
			t.Fatalf("split err = %v, want ErrRegionSplit", err)
		}
	})
}

// TestTodo_WF_REV_007_Golden pins the canonical plan rendering.
func TestTodo_WF_REV_007_Golden(t *testing.T) {
	plan, err := workflow.CompileUnwindPlan(rev007Graph(), rev007Points())
	if err != nil {
		t.Fatalf("CompileUnwindPlan: %v", err)
	}
	const want = `commit-order: draft,reserve,charge,notify
safe-point: sp-after-reserve
  committed: draft[REVERT] reserve[REVERT]
  unwind:
    - region booking: reserve[REVERT]
    - draft[REVERT]
safe-point: sp-after-notify
  committed: draft[REVERT] reserve[REVERT] charge[COMPENSATE:refund-charge] notify[REVERT]
  unwind:
    - notify[REVERT]
    - region booking: charge[COMPENSATE:refund-charge] reserve[REVERT]
    - draft[REVERT]
`
	if got := plan.Canonical(); got != want {
		t.Fatalf("canonical:\n%s\nwant:\n%s", got, want)
	}
}

// TestTodo_WF_REV_007_Property proves unwind order is the reverse of a
// valid commit order on random DAGs: every dependency unwinds depended-on
// last, every safe point unwinds exactly its committed prefix, and every
// region stays contiguous.
func TestTodo_WF_REV_007_Property(t *testing.T) {
	for seed := int64(1); seed <= 300; seed++ {
		rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic property seed
		nodes, order := rev007DAG(rng)
		points := []workflow.SafePoint{{ID: "sp-all", ThroughNode: order[len(order)-1]}}
		if len(order) > 2 {
			points = append([]workflow.SafePoint{{ID: "sp-mid", ThroughNode: order[len(order)/2]}}, points...)
		}
		plan, err := workflow.CompileUnwindPlan(nodes, points)
		if err != nil {
			t.Fatalf("seed %d: CompileUnwindPlan: %v", seed, err)
		}
		after := map[string][]string{}
		for _, n := range nodes {
			after[n.NodeID] = append([]string(nil), n.After...)
		}
		for _, sp := range plan.Points {
			flat := flattenUnwind(sp.Unwind)
			// Reverse-valid: for every edge a->b (b after a), b
			// unwinds before a.
			pos := map[string]int{}
			for i, e := range flat {
				pos[e.NodeID] = i
			}
			committed := map[string]bool{}
			for _, e := range sp.Committed {
				committed[e.NodeID] = true
			}
			if len(flat) != len(sp.Committed) {
				t.Fatalf("seed %d %s: unwind %d effects, committed %d", seed, sp.SafePointID, len(flat), len(sp.Committed))
			}
			for b, deps := range after {
				if !committed[b] {
					continue
				}
				for _, a := range deps {
					if !committed[a] {
						continue
					}
					if pos[b] > pos[a] {
						t.Fatalf("seed %d %s: %s unwinds after its dependency %s", seed, sp.SafePointID, b, a)
					}
				}
			}
			// Regions contiguous in unwind: each region's
			// indices form one interval.
			idx := map[string][]int{}
			for i, e := range flat {
				if e.Region != "" {
					idx[e.Region] = append(idx[e.Region], i)
				}
			}
			for region, ii := range idx {
				if ii[len(ii)-1]-ii[0]+1 != len(ii) {
					t.Fatalf("seed %d %s: region %s split in unwind at %v", seed, sp.SafePointID, region, ii)
				}
			}
		}
		// Commit order itself is valid.
		cpos := map[string]int{}
		for i, id := range plan.CommitOrder {
			cpos[id] = i
		}
		for b, deps := range after {
			for _, a := range deps {
				if cpos[a] > cpos[b] {
					t.Fatalf("seed %d: commit order violates %s before %s", seed, a, b)
				}
			}
		}
	}
}

// rev007DAG builds a random DAG over a..k with contiguous atomic
// regions: edges only run forward along a random permutation, so the
// graph is acyclic by construction; regions are then cut over the
// planner's own commit order, so contiguity is structural.
func rev007DAG(rng *rand.Rand) ([]workflow.WriteNode, []string) {
	n := 2 + rng.Intn(6)
	ids := make([]string, n)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	perm := append([]string(nil), ids...)
	rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
	at := map[string]int{}
	for i, id := range perm {
		at[id] = i
	}
	var bare []workflow.WriteNode
	for _, id := range ids {
		var after []string
		for _, other := range ids {
			if at[other] < at[id] && rng.Intn(3) == 0 {
				after = append(after, other)
			}
		}
		sort.Strings(after)
		effect := workflow.EffectRecord{ID: "e-" + id, Reversible: true}
		if rng.Intn(4) == 0 {
			effect = workflow.EffectRecord{ID: "e-" + id, Compensation: "comp-" + id}
		}
		bare = append(bare, workflow.WriteNode{NodeID: id, Effect: effect, After: after})
	}
	commit, err := workflow.CompileUnwindPlan(bare, nil)
	if err != nil {
		panic(err)
	}
	order := commit.CommitOrder
	cuts := []int{0}
	for i := 1; i < len(order); i++ {
		if rng.Intn(3) == 0 {
			cuts = append(cuts, i)
		}
	}
	cuts = append(cuts, len(order))
	regionOf := map[string]string{}
	for c := 0; c+1 < len(cuts); c++ {
		if cuts[c+1]-cuts[c] < 2 || rng.Intn(2) == 0 {
			continue
		}
		name := "r" + string(rune('0'+c))
		for _, id := range order[cuts[c]:cuts[c+1]] {
			regionOf[id] = name
		}
	}
	nodes := make([]workflow.WriteNode, len(bare))
	copy(nodes, bare)
	for i := range nodes {
		nodes[i].AtomicRegion = regionOf[nodes[i].NodeID]
	}
	return nodes, order
}

func flattenUnwind(groups []workflow.UnwindGroup) []workflow.PlannedEffect {
	var out []workflow.PlannedEffect
	for _, g := range groups {
		out = append(out, g.Effects...)
	}
	return out
}

func TestTodo_WF_REV_007_EmptyGroup(t *testing.T) {
	// Regression: Canonical must not panic on an empty unwind group.
	plan := workflow.UnwindPlan{Points: []workflow.SafePointPlan{{SafePointID: "sp", Unwind: []workflow.UnwindGroup{{}, {Region: "r"}}}}}
	got := plan.Canonical()
	if !strings.Contains(got, "- empty") {
		t.Fatalf("canonical empty group = %q", got)
	}
}
