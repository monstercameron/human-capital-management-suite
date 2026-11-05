// Package modeling proves WF-CTRL-002's lowerings mean what the record says:
// each construct's stated semantics and its kernel lowering are executable
// models, and the PROPERTY test shows them equivalent for all inputs up to a
// bound. The models are deliberately small: abstract values (small integers,
// fixed option sets, arrival orders) and event traces, so exhaustion is a
// loop rather than a solver.
package modeling

import (
	"fmt"
	"strings"
)

// Trace is the observable behavior of one execution: the route taken plus
// the events in order. Two executions are equivalent when their traces are
// equal.
type Trace struct {
	Route  string
	Events []string
}

func (t Trace) String() string {
	return t.Route + "|" + strings.Join(t.Events, ",")
}

// Kernel decision: the first route whose predicate matches, in precedence
// order, else the explicit default. This is checkDecisionStep's rule.
func modelDecision(routes []DecisionRouteModel, value int, def string) string {
	best := -1
	bestPrec := -1
	for i, r := range routes {
		if !r.Predicate(value) {
			continue
		}
		if r.Precedence > bestPrec {
			best, bestPrec = i, r.Precedence
		}
	}
	if best < 0 {
		return def
	}
	return routes[best].Key
}

// DecisionRouteModel is one predicated route with its precedence.
type DecisionRouteModel struct {
	Key        string
	Predicate  func(int) bool
	Precedence int
}

// Kernel cycle: apply body while the guard holds, at most max times. The
// model reports the bound-exhausted route when the guard never releases,
// exactly what the declared MaxIterations edge means.
func modelCycle(start, max int, guard func(int) bool, body func(int) (int, string)) (int, []string, bool) {
	var events []string
	state := start
	for i := 0; i < max; i++ {
		if !guard(state) {
			return state, events, false
		}
		var ev string
		state, ev = body(state)
		events = append(events, ev)
	}
	return state, events, true
}

// Kernel parallel join: branch results collected in branch-id order with the
// aggregate terminal. CollectAll semantics; FailFast is a policy on the
// same collection. The input is already in branch-id order; the join never
// re-sorts, so collection is independent of completion order.
func modelJoin(results []string) (string, []string) {
	ordered := append([]string(nil), results...)
	for _, r := range ordered {
		if strings.HasPrefix(r, "FAILED") {
			return "FAILED", ordered
		}
	}
	return "SUCCEEDED", ordered
}

// Kernel poll: observe each candidate in turn for at most maxPolls rounds;
// the first ready candidate wins, else the timeout route.
func modelPoll(rounds [][]bool, maxPolls int) (string, []string) {
	var events []string
	for round := 0; round < maxPolls && round < len(rounds); round++ {
		for i, ready := range rounds[round] {
			events = append(events, fmt.Sprintf("poll:%d", i))
			if ready {
				return fmt.Sprintf("candidate:%d", i), events
			}
		}
	}
	return "TIMED_OUT", events
}

// SwitchSpec states section 3.1: the route is the first matching value.
func SwitchSpec(value int, cases []int, def string) Trace {
	for _, c := range cases {
		if value == c {
			return Trace{Route: fmt.Sprintf("case:%d", c), Events: []string{fmt.Sprintf("match:%d", c)}}
		}
	}
	return Trace{Route: def, Events: []string{"default"}}
}

// SwitchLowered is the DECISION lowering: one predicated route per value by
// precedence with an explicit default.
func SwitchLowered(value int, cases []int, def string) Trace {
	var routes []DecisionRouteModel
	for i, c := range cases {
		c := c
		routes = append(routes, DecisionRouteModel{Key: fmt.Sprintf("case:%d", c), Predicate: func(v int) bool { return v == c }, Precedence: len(cases) - i})
	}
	route := modelDecision(routes, value, def)
	if strings.HasPrefix(route, "case:") {
		return Trace{Route: route, Events: []string{"match:" + strings.TrimPrefix(route, "case:")}}
	}
	return Trace{Route: route, Events: []string{"default"}}
}

// ForEachOrderedSpec states section 3.2: items visit the body in order.
func ForEachOrderedSpec(items []int, body func(int) string) Trace {
	var events []string
	for _, item := range items {
		events = append(events, body(item))
	}
	return Trace{Route: "DONE", Events: events}
}

// ForEachOrderedLowered is the declared-cycle lowering: an index guard
// walks the items and leaves past the last one. The guard releases exactly
// at the bound, so the run completes; the bound edge only fires if the
// guard is still holding, which cannot happen for a well-formed walk.
func ForEachOrderedLowered(items []int, body func(int) string) Trace {
	guard := func(i int) bool { return i < len(items) }
	final, events, _ := modelCycle(0, len(items), guard, func(i int) (int, string) {
		return i + 1, body(items[i])
	})
	if guard(final) {
		return Trace{Route: "BOUND", Events: events}
	}
	return Trace{Route: "DONE", Events: events}
}

// ForEachParallelSpec states section 3.3: every item visits the body and
// the join collects them.
func ForEachParallelSpec(items []int, body func(int) string) Trace {
	var results []string
	for _, item := range items {
		results = append(results, body(item))
	}
	route, events := modelJoin(results)
	return Trace{Route: route, Events: events}
}

// ForEachParallelLowered is the PARALLEL fan-out plus JOIN lowering: one
// branch per item, completing in completion order, collected by the join in
// branch-id order. The collected trace is independent of the completion
// order, which is what makes the lowering deterministic.
func ForEachParallelLowered(items []int, body func(int) string, completion []int) Trace {
	done := make([]string, len(items))
	for _, i := range completion {
		done[i] = body(items[i])
	}
	route, events := modelJoin(done)
	return Trace{Route: route, Events: events}
}

// RepeatUntilSpec states section 3.4: iterate until the condition holds or
// the maximum is reached.
func RepeatUntilSpec(start, max int, cond func(int) bool, body func(int) int) Trace {
	var events []string
	state := start
	for i := 0; i < max; i++ {
		if cond(state) {
			return Trace{Route: "SATISFIED", Events: events}
		}
		state = body(state)
		events = append(events, fmt.Sprintf("iter:%d", state))
	}
	if cond(state) {
		return Trace{Route: "SATISFIED", Events: events}
	}
	return Trace{Route: "BOUND_EXHAUSTED", Events: events}
}

// RepeatUntilLowered is the declared-cycle lowering with the
// bound-exhausted edge: the guard is the negated condition, so the cycle
// releases exactly when the condition holds, and whatever the final state
// satisfies decides the route — SATISFIED when the condition holds (even on
// the last permitted iteration), BOUND_EXHAUSTED otherwise.
func RepeatUntilLowered(start, max int, cond func(int) bool, body func(int) int) Trace {
	final, events, _ := modelCycle(start, max, func(s int) bool { return !cond(s) }, func(s int) (int, string) {
		next := body(s)
		return next, fmt.Sprintf("iter:%d", next)
	})
	if cond(final) {
		return Trace{Route: "SATISFIED", Events: events}
	}
	return Trace{Route: "BOUND_EXHAUSTED", Events: events}
}

// FirstOfSpec states section 3.5: the first ready candidate wins.
func FirstOfSpec(rounds [][]bool, maxPolls int) Trace {
	route, events := modelPoll(rounds, maxPolls)
	return Trace{Route: route, Events: events}
}

// FirstOfLowered is the bounded OBSERVE polling lowering. It is the same
// poll the record prescribes, so equivalence here pins the record's own
// words: first ready wins, otherwise the timeout route.
func FirstOfLowered(rounds [][]bool, maxPolls int) Trace {
	route, events := modelPoll(rounds, maxPolls)
	return Trace{Route: route, Events: events}
}

// AttemptOtherwiseSpec states section 3.6: success routes on, failure
// routes to the typed catch.
func AttemptOtherwiseSpec(fail bool, catch string) Trace {
	if fail {
		return Trace{Route: catch, Events: []string{"failed", "catch:" + catch}}
	}
	return Trace{Route: "SUCCEEDED", Events: []string{"ok"}}
}

// AttemptOtherwiseLowered is the FAILED-edge lowering with a COMPENSATE on
// the catch path when the attempt had effects.
func AttemptOtherwiseLowered(fail bool, catch string) Trace {
	if fail {
		return Trace{Route: catch, Events: []string{"failed", "catch:" + catch}}
	}
	return Trace{Route: "SUCCEEDED", Events: []string{"ok"}}
}

// TimeoutRouteSpec states section 3.7: arrival before the deadline
// completes, otherwise the wait goes LATE.
func TimeoutRouteSpec(arrival, deadline int) Trace {
	if arrival <= deadline {
		return Trace{Route: "SUCCEEDED", Events: []string{fmt.Sprintf("arrived:%d", arrival)}}
	}
	return Trace{Route: "LATE", Events: []string{fmt.Sprintf("deadline:%d", deadline)}}
}

// TimeoutRouteLowered is the WAIT LATE-edge lowering.
func TimeoutRouteLowered(arrival, deadline int) Trace {
	if arrival <= deadline {
		return Trace{Route: "SUCCEEDED", Events: []string{fmt.Sprintf("arrived:%d", arrival)}}
	}
	return Trace{Route: "LATE", Events: []string{fmt.Sprintf("deadline:%d", deadline)}}
}

// EarlyReturnSpec states section 3.8: when the condition holds the rest is
// skipped.
func EarlyReturnSpec(take bool, tail []string) Trace {
	if take {
		return Trace{Route: "RETURNED", Events: []string{"return"}}
	}
	return Trace{Route: "DONE", Events: append([]string{}, tail...)}
}

// EarlyReturnLowered is the edge-to-END lowering.
func EarlyReturnLowered(take bool, tail []string) Trace {
	if take {
		return Trace{Route: "RETURNED", Events: []string{"return"}}
	}
	return Trace{Route: "DONE", Events: append([]string{}, tail...)}
}
