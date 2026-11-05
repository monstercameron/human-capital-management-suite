// WF-REV-007: compile an unwind plan for every safe point and reject
// writes without a reversal contract.
//
// The spec already requires irreversible effects to declare cancellation
// behaviour, but only the failure route is checked and
// CancellationPolicyRef is never evaluated. This planner closes that gap
// at compile time: every write block must carry a reversal contract — a
// reversible effect, a published compensation, or a forward correction —
// and the compiled plan carries, for each safe point, the effects
// committed by then with their reverse-dependency unwind order and atomic
// regions grouped. A write without a contract is a compile error, not a
// runtime surprise.
//
// The planner is the unwind half of the compiler pipeline's contract
// check; it reads write nodes and safe points and emits the plan. Wiring
// it into compile.go's own pass list is the pipeline's one-line call.
package workflow

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Unwind errors. All are matchable with errors.Is.
var (
	// ErrUnwindInvalid is returned for a malformed unwind request.
	ErrUnwindInvalid = errors.New("workflow: invalid unwind compilation")
	// ErrNoReversalContract is returned for a write block with no
	// reversal contract: not reversible, with no compensation and no
	// correction. This is the compile error WF-REV-007 requires.
	ErrNoReversalContract = errors.New("workflow: write block declares no reversal contract")
	// ErrUnwindCycle is returned when commit dependencies are cyclic.
	ErrUnwindCycle = errors.New("workflow: commit dependencies are cyclic")
	// ErrRegionSplit is returned when one atomic region's effects are not
	// contiguous in commit order and so cannot unwind as one group.
	ErrRegionSplit = errors.New("workflow: atomic region is split in commit order")
)

// WriteNode is one write block in the compiled graph: its effect's
// reversal contract, its commit dependencies, and its atomic region.
type WriteNode struct {
	NodeID       string
	Effect       EffectRecord
	After        []string
	AtomicRegion string
}

// SafePoint is one point a cancel can name: the effects committed up to
// and including ThroughNode.
type SafePoint struct {
	ID          string
	ThroughNode string
}

// PlannedEffect is one effect in a safe-point plan with its unwind
// action: REVERT, COMPENSATE:<id> or CORRECT:<id>.
type PlannedEffect struct {
	NodeID   string
	EffectID string
	Region   string
	Action   string
}

// UnwindGroup is one unwind unit: a same-region run, or a singleton with
// an empty region.
type UnwindGroup struct {
	Region  string
	Effects []PlannedEffect
}

// SafePointPlan is the compiled answer to "what would cancelling here
// undo": the effects committed by then in commit order, and their unwind
// order with atomic regions grouped.
type SafePointPlan struct {
	SafePointID string
	Committed   []PlannedEffect
	Unwind      []UnwindGroup
}

// UnwindPlan is the compiled unwind contract: the valid commit order and
// one plan per safe point.
type UnwindPlan struct {
	CommitOrder []string
	Points      []SafePointPlan
}

// CompileUnwindPlan compiles the unwind contract: it rejects writes
// without reversal contracts, orders commits topologically, refuses
// cyclic dependencies and split atomic regions, and emits one plan per
// safe point.
func CompileUnwindPlan(nodes []WriteNode, points []SafePoint) (UnwindPlan, error) {
	byID := make(map[string]WriteNode, len(nodes))
	for _, n := range nodes {
		if strings.TrimSpace(n.NodeID) == "" {
			return UnwindPlan{}, fmt.Errorf("%w: blank node id", ErrUnwindInvalid)
		}
		if _, dup := byID[n.NodeID]; dup {
			return UnwindPlan{}, fmt.Errorf("%w: duplicate node %q", ErrUnwindInvalid, n.NodeID)
		}
		if strings.TrimSpace(n.Effect.ID) == "" {
			return UnwindPlan{}, fmt.Errorf("%w: node %q names no effect", ErrUnwindInvalid, n.NodeID)
		}
		if !n.Effect.Reversible && strings.TrimSpace(n.Effect.Compensation) == "" && strings.TrimSpace(n.Effect.Correction) == "" {
			return UnwindPlan{}, fmt.Errorf("%w: node %q effect %q", ErrNoReversalContract, n.NodeID, n.Effect.ID)
		}
		byID[n.NodeID] = n
	}
	for _, n := range nodes {
		for _, dep := range n.After {
			if _, ok := byID[dep]; !ok {
				return UnwindPlan{}, fmt.Errorf("%w: node %q depends on unknown %q", ErrUnwindInvalid, n.NodeID, dep)
			}
			if dep == n.NodeID {
				return UnwindPlan{}, fmt.Errorf("%w: node %q depends on itself", ErrUnwindInvalid, n.NodeID)
			}
		}
	}
	seen := make(map[string]bool, len(points))
	for _, p := range points {
		if strings.TrimSpace(p.ID) == "" || seen[p.ID] {
			return UnwindPlan{}, fmt.Errorf("%w: blank or duplicate safe point %q", ErrUnwindInvalid, p.ID)
		}
		seen[p.ID] = true
		if _, ok := byID[p.ThroughNode]; !ok {
			return UnwindPlan{}, fmt.Errorf("%w: safe point %q names unknown node %q", ErrUnwindInvalid, p.ID, p.ThroughNode)
		}
	}

	order, err := commitOrder(nodes)
	if err != nil {
		return UnwindPlan{}, err
	}
	if err := checkRegionsContiguous(order, byID); err != nil {
		return UnwindPlan{}, err
	}

	planned := make(map[string]PlannedEffect, len(nodes))
	for _, n := range nodes {
		planned[n.NodeID] = PlannedEffect{
			NodeID: n.NodeID, EffectID: n.Effect.ID,
			Region: n.AtomicRegion, Action: unwindAction(n.Effect),
		}
	}
	plan := UnwindPlan{CommitOrder: order}
	for _, p := range points {
		var committed []PlannedEffect
		for _, id := range order {
			committed = append(committed, planned[id])
			if id == p.ThroughNode {
				break
			}
		}
		plan.Points = append(plan.Points, SafePointPlan{
			SafePointID: p.ID,
			Committed:   committed,
			Unwind:      groupUnwind(committed),
		})
	}
	return plan, nil
}

// unwindAction derives the one undo for a contracted effect.
func unwindAction(e EffectRecord) string {
	if e.Reversible {
		return "REVERT"
	}
	if strings.TrimSpace(e.Compensation) != "" {
		return "COMPENSATE:" + e.Compensation
	}
	return "CORRECT:" + e.Correction
}

// commitOrder returns a valid commit order: a topological order that is
// deterministic through lexicographic tie-breaking.
func commitOrder(nodes []WriteNode) ([]string, error) {
	indegree := make(map[string]int, len(nodes))
	dependents := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		indegree[n.NodeID] += 0
		for _, dep := range n.After {
			indegree[n.NodeID]++
			dependents[dep] = append(dependents[dep], n.NodeID)
		}
	}
	var ready []string
	for id, deg := range indegree {
		if deg == 0 {
			ready = append(ready, id)
		}
	}
	var order []string
	for len(ready) > 0 {
		sort.Strings(ready)
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		for _, next := range dependents[id] {
			indegree[next]--
			if indegree[next] == 0 {
				ready = append(ready, next)
			}
		}
	}
	if len(order) != len(nodes) {
		return nil, fmt.Errorf("%w: %d of %d nodes ordered", ErrUnwindCycle, len(order), len(nodes))
	}
	return order, nil
}

// checkRegionsContiguous refuses an atomic region whose effects are not
// contiguous in commit order: only a contiguous region can unwind as one
// group while the unwind stays the reverse of a valid commit order.
func checkRegionsContiguous(order []string, byID map[string]WriteNode) error {
	first := map[string]int{}
	last := map[string]int{}
	count := map[string]int{}
	for i, id := range order {
		region := byID[id].AtomicRegion
		if region == "" {
			continue
		}
		if _, ok := first[region]; !ok {
			first[region] = i
		}
		last[region] = i
		count[region]++
	}
	for region, n := range count {
		if last[region]-first[region]+1 != n {
			return fmt.Errorf("%w: %q", ErrRegionSplit, region)
		}
	}
	return nil
}

// groupUnwind reverses the committed prefix and groups maximal
// same-region runs; region-less effects unwind as singletons.
func groupUnwind(committed []PlannedEffect) []UnwindGroup {
	var groups []UnwindGroup
	for i := len(committed) - 1; i >= 0; {
		eff := committed[i]
		if eff.Region == "" {
			groups = append(groups, UnwindGroup{Effects: []PlannedEffect{eff}})
			i--
			continue
		}
		j := i
		for j >= 0 && committed[j].Region == eff.Region {
			j--
		}
		run := append([]PlannedEffect(nil), committed[j+1:i+1]...)
		for a, b := 0, len(run)-1; a < b; a, b = a+1, b-1 {
			run[a], run[b] = run[b], run[a]
		}
		groups = append(groups, UnwindGroup{Region: eff.Region, Effects: run})
		i = j
	}
	return groups
}

// Canonical renders the plan deterministically for golden pinning.
func (p UnwindPlan) Canonical() string {
	var b strings.Builder
	b.WriteString("commit-order: " + strings.Join(p.CommitOrder, ",") + "\n")
	for _, sp := range p.Points {
		b.WriteString("safe-point: " + sp.SafePointID + "\n")
		b.WriteString("  committed:")
		for _, e := range sp.Committed {
			fmt.Fprintf(&b, " %s[%s]", e.NodeID, e.Action)
		}
		b.WriteString("\n  unwind:\n")
		for _, g := range sp.Unwind {
			if len(g.Effects) == 0 {
				b.WriteString("    - empty\n")
				continue
			}
			if g.Region == "" {
				fmt.Fprintf(&b, "    - %s[%s]\n", g.Effects[0].NodeID, g.Effects[0].Action)
				continue
			}
			b.WriteString("    - region " + g.Region + ":")
			for _, e := range g.Effects {
				fmt.Fprintf(&b, " %s[%s]", e.NodeID, e.Action)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
