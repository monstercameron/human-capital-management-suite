package frontier

import (
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Advance derives the exact successors, node states, join counters, terminal
// dimensions and scheduling intents that follow from one node outcome.
//
// It is pure. The only inputs are the pinned compiled plan, a value snapshot
// of the instance and the typed outcome a step handler returned; the only
// output is a value. Nothing is read from storage, nothing is enqueued,
// nothing is invoked, and no clock or random source is consulted. Calling it
// twice with the same three arguments produces transitions with the same
// [Transition.Digest].
//
// Every refusal is typed ([Error], classified by [CodeOf]). In particular this
// function never picks a route nobody declared: a missing edge, an undeclared
// DECISION result with no default, a JOIN with no declared strategy and an END
// that would leave work on the frontier are all refusals, not defaults.
func Advance(plan *workflow.CompiledWorkflow, state InstanceState, completed NodeOutcome) (Transition, error) {
	a, err := newAdvance(plan, state, completed)
	if err != nil {
		return Transition{}, err
	}
	return a.run()
}

// advance is one evaluation's working set. It is local to the call, so two
// concurrent calls share nothing: the plan is read-only, the incoming state is
// cloned before a single field is touched, and every result is freshly built.
type advance struct {
	plan    *workflow.CompiledWorkflow
	node    workflow.CompiledNode
	status  NodeStatus
	outcome NodeOutcome

	next InstanceState
	t    Transition
}

// newAdvance validates the three arguments against each other before any of
// them is interpreted. A state that disagrees with its own plan, its own
// frontier or its own node vocabulary is refused rather than repaired.
func newAdvance(plan *workflow.CompiledWorkflow, state InstanceState, completed NodeOutcome) (*advance, error) {
	if plan == nil {
		return nil, refuse(CodeInvalidPlan, "", "no compiled plan")
	}
	if state.PlanDigest != plan.Digest() {
		return nil, refuse(CodePlanMismatch, "",
			"instance state pins plan digest %q; the plan supplied digests to %q",
			state.PlanDigest, plan.Digest())
	}
	if state.Completed {
		return nil, refuse(CodeAlreadyComplete, state.Terminal.NodeID,
			"instance already reached terminal %q", state.Terminal.TerminalCode)
	}
	if err := validateState(plan, state); err != nil {
		return nil, err
	}

	node, ok := plan.Node(completed.NodeID)
	if !ok {
		return nil, refuse(CodeUnknownNode, completed.NodeID, "plan declares no such node")
	}
	status, ok := state.Node(completed.NodeID)
	if !ok {
		return nil, refuse(CodeNodeNotActive, completed.NodeID,
			"the instance has not activated this node")
	}
	if !status.State.Active() {
		return nil, refuse(CodeNodeNotActive, completed.NodeID,
			"node is %s, which is settled; a settled node does not advance again", status.State)
	}
	if completed.Await != AwaitNone && completed.Failed {
		return nil, refuse(CodeAwaitNotAdmitted, completed.NodeID,
			"outcome both awaits %s and reports failure", completed.Await)
	}

	a := &advance{plan: plan, node: node, status: status, outcome: completed, next: state.Clone()}
	a.next.Sequence = state.Sequence + 1
	return a, nil
}

// validateState checks that a snapshot means what it says: every node it
// mentions is in the plan, every state is a declared one, and the frontier is
// exactly the active set. A frontier edited apart from the node states it
// summarizes would let an instance carry work nobody can see.
func validateState(plan *workflow.CompiledWorkflow, state InstanceState) error {
	for i, n := range state.Nodes {
		if _, ok := plan.Node(n.NodeID); !ok {
			return refuse(CodeInvalidState, n.NodeID, "state carries a node the plan does not declare")
		}
		if !n.State.Valid() {
			return refuse(CodeInvalidState, n.NodeID, "state %q is not a declared node state", n.State)
		}
		if i > 0 && state.Nodes[i-1].NodeID >= n.NodeID {
			return refuse(CodeInvalidState, n.NodeID, "node statuses are not sorted and deduplicated")
		}
	}
	derived := state
	derived.recomputeFrontier()
	if strings.Join(derived.Frontier, ",") != strings.Join(state.Frontier, ",") {
		return refuse(CodeInvalidState, "",
			"frontier %v does not match the active node states %v", state.Frontier, derived.Frontier)
	}
	for i, j := range state.Joins {
		if _, ok := plan.Node(j.NodeID); !ok {
			return refuse(CodeInvalidState, j.NodeID, "join counter for a node the plan does not declare")
		}
		if !j.Strategy.Valid() {
			return refuse(CodeInvalidState, j.NodeID, "strategy %q is not a declared join strategy", j.Strategy)
		}
		if i > 0 && state.Joins[i-1].NodeID >= j.NodeID {
			return refuse(CodeInvalidState, j.NodeID, "join counters are not sorted and deduplicated")
		}
	}
	return nil
}

// run dispatches on what the outcome says happened, in the one order that is
// meaningful: a node that is waiting did not complete, a node that failed
// produced no outcome to route on, an END ends the instance, and everything
// else routes.
func (a *advance) run() (Transition, error) {
	a.t = Transition{
		InstanceID:      a.next.InstanceID,
		WorkflowID:      a.plan.WorkflowID,
		Version:         a.plan.Version,
		PlanDigest:      a.plan.Digest(),
		Sequence:        a.next.Sequence,
		CompletedNodeID: a.node.ID,
		CompletedType:   a.node.Type,
		OutputDigest:    a.outcome.OutputDigest,
	}

	var err error
	switch {
	case a.outcome.Await != AwaitNone:
		err = a.await()
	case a.outcome.Failed:
		err = a.fail()
	case a.node.Type == workflow.StepEnd:
		err = a.end()
	default:
		err = a.route()
	}
	if err != nil {
		return Transition{}, err
	}
	return a.finish(), nil
}

// await suspends the node in place. No successor is activated and no route is
// taken: the node has not produced an outcome yet, it has said what it is
// waiting for.
func (a *advance) await() error {
	if a.outcome.Outcome != "" {
		return refuse(CodeAwaitNotAdmitted, a.node.ID,
			"outcome %q both completes the node and awaits %s", a.outcome.Outcome, a.outcome.Await)
	}
	if !awaitAdmitted(a.node.Type, a.outcome.Await) {
		return refuse(CodeAwaitNotAdmitted, a.node.ID,
			"%s cannot await %s", a.node.Type, a.outcome.Await)
	}
	a.status.State = NodeWaiting
	a.next.setNode(a.status)
	a.t.CompletedState = NodeWaiting
	a.t.Intents = append(a.t.Intents, Intent{
		Kind:   intentForAwait(a.outcome.Await),
		NodeID: a.node.ID,
		Type:   a.node.Type,
		Ref:    a.outcome.AwaitRef,
	})
	return nil
}

// fail applies the node's declared retry budget and then its declared failure
// route. A failure never takes an outcome route, because a step that did not
// run produced no outcome.
func (a *advance) fail() error {
	attempts := a.status.Attempts + 1
	var budget uint32
	if a.node.Retry != nil {
		budget = a.node.Retry.MaxAttempts
	}
	if attempts < budget && !a.outcome.RetryTerminal {
		a.status.State = NodeRetrying
		a.status.Attempts = attempts
		a.next.setNode(a.status)
		a.t.CompletedState = NodeRetrying
		a.t.Intents = append(a.t.Intents, Intent{
			Kind:   IntentReady,
			NodeID: a.node.ID,
			Type:   a.node.Type,
			Ref:    a.outcome.ErrorClass,
		})
		return nil
	}
	if a.node.FailureRoute == "" {
		return refuse(CodeNoFailureRoute, a.node.ID,
			"attempt %d of %d failed (%s) and the node declares no failure_route",
			attempts, budget, a.outcome.ErrorClass)
	}
	a.status.State = NodeFailed
	a.status.Attempts = attempts
	a.next.setNode(a.status)
	a.t.CompletedState = NodeFailed
	return a.activate(a.node.FailureRoute, "", false, true)
}

// end closes the instance at a compiled terminal. The plan states the five
// lifecycle dimensions; a handler may assert them, and a handler that asserts
// something else is refused rather than believed.
func (a *advance) end() error {
	if a.node.Terminal == nil {
		return refuse(CodeMissingTerminal, a.node.ID, "END node carries no compiled terminal artifact")
	}
	record := terminalRecordOf(*a.node.Terminal)
	if err := a.checkAssertedTerminal(record); err != nil {
		return err
	}

	a.status.State = NodeSucceeded
	a.status.OutputDigest = a.outcome.OutputDigest
	a.next.setNode(a.status)
	a.t.CompletedState = NodeSucceeded

	a.next.recomputeFrontier()
	if len(a.next.Frontier) > 0 {
		return refuse(CodeTerminalFrontierRemains, a.node.ID,
			"terminal %q reached while %d node(s) remain on the frontier: %s",
			record.TerminalCode, len(a.next.Frontier), strings.Join(a.next.Frontier, ", "))
	}

	a.next.Completed = true
	a.next.Terminal = record
	a.t.Complete = true
	a.t.Terminal = record
	a.t.Intents = append(a.t.Intents, Intent{
		Kind:         IntentComplete,
		NodeID:       a.node.ID,
		Type:         a.node.Type,
		TerminalCode: record.TerminalCode,
	})
	return nil
}

// checkAssertedTerminal compares a handler's reported terminal with the one
// the plan compiled. Anything the handler leaves empty is not an assertion.
func (a *advance) checkAssertedTerminal(want TerminalRecord) error {
	got := a.outcome.Terminal
	if got.TerminalCode != "" && got.TerminalCode != want.TerminalCode {
		return refuse(CodeTerminalMismatch, a.node.ID,
			"handler reports terminal_code %q; the plan compiled %q", got.TerminalCode, want.TerminalCode)
	}
	if got.RuntimeStatus != "" && got.RuntimeStatus != want.RuntimeStatus {
		return refuse(CodeTerminalMismatch, a.node.ID,
			"handler reports runtime_status %q; the plan compiled %q", got.RuntimeStatus, want.RuntimeStatus)
	}
	if got.Asserted {
		if asserted := lifecycleStateOf(got.Dimensions); asserted != want.Lifecycle {
			return refuse(CodeTerminalMismatch, a.node.ID,
				"handler asserts lifecycle %+v; the plan compiled %+v", asserted, want.Lifecycle)
		}
	}
	return nil
}

// route resolves the one explicit edge the outcome selects and activates its
// target. There is no implicit first edge, and a default route is applied
// because the node declared one, never because a route was missing.
func (a *advance) route() error {
	if a.outcome.Outcome == "" {
		return refuse(CodeUnknownOutcome, a.node.ID,
			"%s completed without an outcome route key", a.node.Type)
	}
	key := string(a.outcome.Outcome)
	viaDefault := false

	if !a.declares(key) {
		decision := a.node.Type == workflow.StepDecision && a.node.Decision != nil
		if !decision {
			return refuse(CodeUnknownOutcome, a.node.ID,
				"%s cannot produce outcome %q; its declared outcomes are %s",
				a.node.Type, key, strings.Join(a.declaredOutcomes(), ", "))
		}
		if a.node.Decision.DefaultRoute == "" {
			return refuse(CodeNoMatchingRoute, a.node.ID,
				"evaluator produced route %q, which matches no declared route (%s), and the node declares no default_route",
				key, strings.Join(a.declaredOutcomes(), ", "))
		}
		key = a.node.Decision.DefaultRoute
		viaDefault = true
	}

	target, ok := a.edge(key)
	if !ok {
		return refuse(CodeMissingRoute, a.node.ID,
			"route %q has no outgoing edge", key)
	}

	a.status.State = NodeSucceeded
	a.status.RouteKey = key
	a.status.OutputDigest = a.outcome.OutputDigest
	a.next.setNode(a.status)
	a.t.CompletedState = NodeSucceeded
	a.t.RouteKey = key

	if err := a.activate(target, key, viaDefault, false); err != nil {
		return err
	}
	return a.skipExcluded(key, target)
}

// declares reports whether the node's compiled step type or declared decision
// routes admit this route key.
func (a *advance) declares(key string) bool {
	conf, ok := workflow.ConformanceFor(a.node.Type)
	if ok {
		for _, o := range conf.Outcomes {
			if string(o) == key {
				return true
			}
		}
	}
	if a.node.Decision != nil {
		for _, r := range a.node.Decision.Routes {
			if r.Key == key {
				return true
			}
		}
	}
	return false
}

// declaredOutcomes lists every route key the node may produce, sorted, so a
// refusal can state the whole vocabulary rather than only what was expected.
func (a *advance) declaredOutcomes() []string {
	var out []string
	if conf, ok := workflow.ConformanceFor(a.node.Type); ok {
		for _, o := range conf.Outcomes {
			out = insertSorted(out, string(o))
		}
	}
	if a.node.Decision != nil {
		for _, r := range a.node.Decision.Routes {
			out = insertSorted(out, r.Key)
		}
	}
	return out
}

// edge returns the target of the node's outgoing edge for a route key.
func (a *advance) edge(key string) (string, bool) {
	for _, e := range a.plan.Edges {
		if e.From == a.node.ID && e.RouteKey == key {
			return e.To, true
		}
	}
	return "", false
}

// activate moves one successor into the state its compiled step type calls
// for and records the scheduling intent that state needs.
func (a *advance) activate(targetID, routeKey string, viaDefault, viaFailure bool) error {
	target, ok := a.plan.Node(targetID)
	if !ok {
		return refuse(CodeUnknownNode, targetID, "edge from %q names a node the plan does not declare", a.node.ID)
	}
	if target.Type == workflow.StepJoin {
		return a.activateJoin(target, routeKey, viaDefault, viaFailure)
	}

	state, intent := activationFor(target.Type)
	// A re-entered node starts a fresh node execution: its retry budget is a
	// property of the attempt, not of the instance's whole history.
	a.next.setNode(NodeStatus{NodeID: target.ID, State: state})
	a.t.Successors = append(a.t.Successors, Successor{
		NodeID:     target.ID,
		Type:       target.Type,
		State:      state,
		RouteKey:   routeKey,
		ViaDefault: viaDefault,
		ViaFailure: viaFailure,
	})
	a.t.Intents = append(a.t.Intents, Intent{
		Kind:     intent,
		NodeID:   target.ID,
		Type:     target.Type,
		RouteKey: routeKey,
	})
	return nil
}

// activateJoin records one branch arrival and activates the JOIN only when its
// declared strategy is met. A JOIN whose strategy is not met is a successor in
// WAITING with no intent, so no work is scheduled on a partial result.
func (a *advance) activateJoin(target workflow.CompiledNode, routeKey string, viaDefault, viaFailure bool) error {
	counter, ok := a.next.Join(target.ID)
	if !ok {
		return refuse(CodeJoinNotDeclared, target.ID,
			"JOIN reached with no declared strategy; a join strategy is declared at start, never inferred")
	}
	if !contains(counter.Branches, a.node.ID) {
		return refuse(CodeInvalidState, target.ID,
			"%q is not a declared incoming branch of this JOIN", a.node.ID)
	}
	counter.Arrived = insertSorted(counter.Arrived, a.node.ID)
	counter.Satisfied = insertSorted(counter.Satisfied, a.node.ID)
	a.settleJoin(&counter)

	state := NodeWaiting
	pending := true
	if counter.Activated {
		state, pending = NodeReady, false
	}
	a.next.setNode(NodeStatus{NodeID: target.ID, State: state})
	a.next.setJoin(counter)
	a.t.Joins = append(a.t.Joins, counter.clone())
	a.t.Successors = append(a.t.Successors, Successor{
		NodeID:      target.ID,
		Type:        target.Type,
		State:       state,
		RouteKey:    routeKey,
		ViaDefault:  viaDefault,
		ViaFailure:  viaFailure,
		JoinPending: pending,
	})
	if !pending {
		a.t.Intents = append(a.t.Intents, Intent{
			Kind:     IntentReady,
			NodeID:   target.ID,
			Type:     target.Type,
			RouteKey: routeKey,
		})
	}
	return nil
}

// settleJoin applies the declared strategy to the counter's arrivals. It never
// turns a missing or skipped mandatory branch into success; where the strategy
// can no longer be met it says so, and the JOIN stays where it is.
func (a *advance) settleJoin(c *JoinCounter) {
	accounted := len(c.Arrived)+len(c.Skipped) >= len(c.Branches)
	switch c.Strategy {
	case JoinAll, JoinAny, JoinQuorum:
		c.Activated = c.Activated || uint32(len(c.Satisfied)) >= c.Required
	case JoinRequiredSet:
		met := true
		for _, b := range c.RequiredBranches {
			if !contains(c.Satisfied, b) {
				met = false
				break
			}
		}
		c.Activated = c.Activated || met
	case JoinBestEffort:
		c.Activated = c.Activated || accounted
	}
	c.Blocked = !c.Activated && accounted
}

// skipExcluded settles the branches the taken route excluded. A target of
// another route key is excluded only when no node still on the frontier can
// reach it: a terminal two routes share is not skipped merely because this
// route did not take it.
func (a *advance) skipExcluded(takenRoute, takenTarget string) error {
	a.next.recomputeFrontier()
	live := reachableFrom(a.plan, a.next.Frontier)

	candidates := make([]string, 0, 4)
	for _, e := range a.plan.Edges {
		if e.From != a.node.ID || e.RouteKey == takenRoute || e.To == takenTarget {
			continue
		}
		if live[e.To] {
			continue
		}
		if _, touched := a.next.Node(e.To); touched {
			// The instance already has a state for it; that state stands.
			continue
		}
		candidates = insertSorted(candidates, e.To)
	}

	for _, id := range candidates {
		if _, ok := a.plan.Node(id); !ok {
			return refuse(CodeUnknownNode, id, "edge from %q names a node the plan does not declare", a.node.ID)
		}
		a.next.setNode(NodeStatus{NodeID: id, State: NodeSkipped})
		a.t.Skipped = insertSorted(a.t.Skipped, id)
		a.recordSkippedBranch(id)
	}
	return nil
}

// recordSkippedBranch tells every JOIN downstream of a skipped node that the
// branch will never arrive. A JOIN that can no longer meet its strategy is
// marked blocked rather than left counting forever.
func (a *advance) recordSkippedBranch(skipped string) {
	for _, e := range a.plan.Edges {
		if e.From != skipped {
			continue
		}
		counter, ok := a.next.Join(e.To)
		if !ok || !contains(counter.Branches, skipped) {
			continue
		}
		counter.Skipped = insertSorted(counter.Skipped, skipped)
		a.settleJoin(&counter)
		a.next.setJoin(counter)
		a.t.Joins = appendJoin(a.t.Joins, counter.clone())
	}
}

// appendJoin replaces a counter already recorded for a node, so the transition
// carries one entry per JOIN rather than a history of intermediate values.
func appendJoin(list []JoinCounter, c JoinCounter) []JoinCounter {
	for i := range list {
		if list[i].NodeID == c.NodeID {
			list[i] = c
			return list
		}
	}
	return append(list, c)
}

// reachableFrom returns every node reachable from any of the given nodes,
// including the nodes themselves.
func reachableFrom(plan *workflow.CompiledWorkflow, from []string) map[string]bool {
	seen := make(map[string]bool, len(plan.Nodes))
	stack := append([]string(nil), from...)
	for _, id := range from {
		seen[id] = true
	}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range plan.Edges {
			if e.From != id || seen[e.To] {
				continue
			}
			seen[e.To] = true
			stack = append(stack, e.To)
		}
	}
	return seen
}

// finish normalizes every ordered field, derives the resulting frontier and
// mints the transition's content digest.
func (a *advance) finish() Transition {
	a.next.recomputeFrontier()
	sortSuccessors(a.t.Successors)
	sortIntents(a.t.Intents)
	sort.Strings(a.t.Skipped)
	sort.Slice(a.t.Joins, func(i, j int) bool { return a.t.Joins[i].NodeID < a.t.Joins[j].NodeID })
	a.t.Frontier = append([]string(nil), a.next.Frontier...)
	a.t.Next = a.next
	a.t.digest = computeTransitionDigest(a.t)
	return a.t
}
