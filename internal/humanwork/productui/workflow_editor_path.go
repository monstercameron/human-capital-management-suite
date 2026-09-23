package productui

import (
	"sort"
	"strings"
)

// workflowPath is the editor's reading of a draft: the steps in the order work
// reaches them, the terminal exits those steps can leave through, and the
// routes that do not simply continue to the next row. It is presentation
// only. Every route comes from draft.Edges and every step from draft.Nodes;
// nothing here is an execution rule.
//
// The previous canvas placed one card per node in depth columns and listed
// every route as text inside each card. A governed HR workflow is one path
// people care about plus many exception routes that all end in a handful of
// terminal states, so that layout spent most of its ink on "Rejected goes to
// End Rejected". Here the path is a single column, an exception route is a
// coloured mark on its step, and the exits are named once.
type workflowPath struct {
	Steps []workflowPathStep
	Loose []workflowPathStep
	Exits []workflowPathExit
	Lanes int
}

type workflowPathStep struct {
	Node  WorkflowDraftNode
	Label string
	Start bool
	// Group names the library fragment this step arrived with, so steps that
	// were added together still read as one unit.
	Group string
	// Continues names the route that leads to the step directly below, so
	// the connector between two rows can say why work moves on.
	Continues string
	// ContinuesTo is the step directly below when Continues leads to it.
	ContinuesTo WorkflowDraftNode
	Exits       []workflowPathExitRoute
	Jumps       []workflowPathJump
	// Arrivals are the routes that land on this step from somewhere other
	// than the row above. Without them a step under "Nothing continues from
	// here" looks orphaned when it is in fact reached by a route on the rail.
	Arrivals []workflowPathArrival
	Open     []string
	Unbound  []string
	// Rail is the state of every jump lane as it crosses this row.
	Rail []workflowRailCell
}

// workflowPathExit is one terminal state. Tone says what kind of ending it is
// and Shape tells apart exits that share a tone, so a mark identifies exactly
// one exit without relying on hue alone.
type workflowPathExit struct {
	Node   WorkflowDraftNode
	Label  string
	Tone   string
	Shape  int
	Routes int
}

// workflowPathExitRoute is every route from one step into one exit. A step
// that reaches the same exit three ways carries one mark, not three.
type workflowPathExitRoute struct {
	RouteKeys []string
	ExitID    string
	ExitNode  WorkflowDraftNode
	Tone      string
	Shape     int
}

// workflowPathJump is a route to a step other than the next row: a skip
// forward, a side branch, or a loop back.
type workflowPathJump struct {
	// RouteKeys is every result of the step that leads to ToID. Two results
	// that go to the same place are one line on the rail and one sentence.
	RouteKeys []string
	ToID      string
	ToNode    WorkflowDraftNode
	Back      bool
	Lane      int
}

type workflowPathArrival struct {
	From      WorkflowDraftNode
	RouteKeys []string
	Back      bool
}

// workflowRailCell is one lane of the jump rail as it crosses one row. Up and
// Down say whether the lane continues above and below the row's middle; Turn
// says the lane meets this row's step, and Arrives that the step is where the
// route lands.
type workflowRailCell struct {
	Up, Down, Turn, Arrives bool
	Back, Active            bool
}

func (c workflowRailCell) used() bool { return c.Up || c.Down || c.Turn }

type workflowPathProblem struct {
	NodeID string
	Key    string
	// Count, when set, makes Key a plural message.
	Count int64
	Vars  map[string]string
}

func workflowStepLabel(node WorkflowDraftNode) string {
	if label := strings.TrimSpace(node.Label); label != "" {
		return label
	}
	// An unnamed exit is labelled from its id, and ids of exits conventionally
	// begin "end_". Under a heading that already says these are exits, "End
	// Rejected" says "end" twice.
	if id := strings.TrimSpace(node.ID); workflowIsExit(node) && len(id) > 4 && strings.EqualFold(id[:4], "end_") && strings.Trim(id[4:], "0123456789_") != "" {
		return DisplayLabel(id[4:])
	}
	return DisplayLabel(node.ID)
}

func workflowIsExit(node WorkflowDraftNode) bool {
	return strings.EqualFold(strings.TrimSpace(node.StepType), "END")
}

// buildWorkflowPath orders a draft for reading. Steps are ranked by the
// longest route from the start node, ignoring loops, which is the same notion
// of depth the compiler publishes for a released definition; ties keep the
// authoring order. A step the start node cannot reach is never ranked beside
// the start: it is returned in Loose so the editor can say it is not
// connected yet.
func buildWorkflowPath(draft WorkflowDraftView, selectedID string) workflowPath {
	byID := make(map[string]WorkflowDraftNode, len(draft.Nodes))
	position := make(map[string]int, len(draft.Nodes))
	for index, node := range draft.Nodes {
		byID[node.ID], position[node.ID] = node, index
	}
	out := make(map[string][]WorkflowDraftEdge, len(draft.Nodes))
	for _, edge := range draft.Edges {
		if _, ok := byID[edge.FromID]; !ok {
			continue
		}
		if _, ok := byID[edge.ToID]; !ok {
			continue
		}
		out[edge.FromID] = append(out[edge.FromID], edge)
	}
	for id := range out {
		edges := out[id]
		sort.SliceStable(edges, func(i, j int) bool {
			if edges[i].RouteKey == edges[j].RouteKey {
				return position[edges[i].ToID] < position[edges[j].ToID]
			}
			return edges[i].RouteKey < edges[j].RouteKey
		})
	}

	start := strings.TrimSpace(draft.StartNodeID)
	if _, ok := byID[start]; !ok {
		start = ""
	}
	depth, back := workflowPathDepths(draft, out, start)

	var result workflowPath
	exitTone := make(map[string]string)
	exitRoutes := make(map[string]int)
	for _, node := range draft.Nodes {
		if !workflowIsExit(node) {
			continue
		}
		keys := make([]string, 0)
		for _, edge := range draft.Edges {
			if edge.ToID == node.ID {
				keys = append(keys, edge.RouteKey)
				exitRoutes[node.ID]++
			}
		}
		exitTone[node.ID] = workflowExitToneFor(node, keys)
	}
	exitShape := make(map[string]int)
	perTone := make(map[string]int)
	for _, node := range draft.Nodes {
		if workflowIsExit(node) {
			tone := exitTone[node.ID]
			exitShape[node.ID] = perTone[tone]
			perTone[tone]++
			result.Exits = append(result.Exits, workflowPathExit{Node: node, Label: workflowStepLabel(node), Tone: tone, Shape: exitShape[node.ID], Routes: exitRoutes[node.ID]})
		}
	}

	sort.SliceStable(result.Exits, func(i, j int) bool {
		// The key runs in the same order as the chips on a step, worst
		// first, so matching one against the other is one scan, not two.
		if left, right := workflowSeverityRank(result.Exits[i].Tone), workflowSeverityRank(result.Exits[j].Tone); left != right {
			return left < right
		}
		return result.Exits[i].Routes > result.Exits[j].Routes
	})
	for index := range result.Exits {
		result.Exits[index].Shape = 0
	}
	perTone = make(map[string]int)
	for index := range result.Exits {
		tone := result.Exits[index].Tone
		result.Exits[index].Shape = perTone[tone]
		exitShape[result.Exits[index].Node.ID] = perTone[tone]
		perTone[tone]++
	}

	// A step's chips follow the key's order exactly, not merely its tone
	// buckets, so matching a chip to the key is one scan.
	exitOrder := make(map[string]int, len(result.Exits))
	for index, exit := range result.Exits {
		exitOrder[exit.Node.ID] = index
	}

	ordered := make([]string, 0, len(draft.Nodes))
	loose := make([]string, 0)
	for _, node := range draft.Nodes {
		if workflowIsExit(node) {
			continue
		}
		if _, reached := depth[node.ID]; reached {
			ordered = append(ordered, node.ID)
		} else {
			loose = append(loose, node.ID)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if depth[ordered[i]] == depth[ordered[j]] {
			return position[ordered[i]] < position[ordered[j]]
		}
		return depth[ordered[i]] < depth[ordered[j]]
	})

	row := make(map[string]int, len(ordered))
	for index, id := range ordered {
		row[id] = index
	}
	groupName := make(map[string]string, len(draft.Groups))
	for _, group := range draft.Groups {
		groupName[group.ID] = strings.TrimSpace(group.Name)
	}
	build := func(id string, next string) workflowPathStep {
		node := byID[id]
		step := workflowPathStep{Node: node, Label: workflowStepLabel(node), Start: id == start, Group: groupName[node.GroupID]}
		connected := make(map[string]bool, len(node.Outcomes))
		for _, edge := range out[id] {
			connected[edge.RouteKey] = true
			target := byID[edge.ToID]
			switch {
			case workflowIsExit(target):
				merged := false
				for index := range step.Exits {
					if step.Exits[index].ExitID == edge.ToID {
						step.Exits[index].RouteKeys = append(step.Exits[index].RouteKeys, edge.RouteKey)
						merged = true
					}
				}
				if !merged {
					step.Exits = append(step.Exits, workflowPathExitRoute{RouteKeys: []string{edge.RouteKey}, ExitID: edge.ToID, ExitNode: target, Tone: exitTone[edge.ToID], Shape: exitShape[edge.ToID]})
				}
			case edge.ToID == next && step.Continues == "":
				step.Continues, step.ContinuesTo = edge.RouteKey, target
			default:
				merged := false
				for index := range step.Jumps {
					if step.Jumps[index].ToID == edge.ToID {
						step.Jumps[index].RouteKeys = append(step.Jumps[index].RouteKeys, edge.RouteKey)
						merged = true
					}
				}
				if !merged {
					step.Jumps = append(step.Jumps, workflowPathJump{RouteKeys: []string{edge.RouteKey}, ToID: edge.ToID, ToNode: target, Back: back[workflowEdgeKey(edge)], Lane: -1})
				}
			}
		}
		// The exit an author most needs to see is the one that says the work
		// was turned down. Sorted by route name it was always the fourth of
		// four, behind "+1 more".
		sort.SliceStable(step.Exits, func(i, j int) bool {
			return exitOrder[step.Exits[i].ExitID] < exitOrder[step.Exits[j].ExitID]
		})
		for _, outcome := range node.Outcomes {
			if !connected[outcome.RouteKey] {
				step.Open = append(step.Open, outcome.RouteKey)
			}
		}
		for _, binding := range node.Bindings {
			if strings.TrimSpace(binding.SourceKind) == "" {
				step.Unbound = append(step.Unbound, binding.TargetPath)
			}
		}
		return step
	}
	for index, id := range ordered {
		next := ""
		if index+1 < len(ordered) {
			next = ordered[index+1]
		}
		result.Steps = append(result.Steps, build(id, next))
	}
	for _, id := range loose {
		result.Loose = append(result.Loose, build(id, ""))
	}
	for _, source := range result.Steps {
		for _, jump := range source.Jumps {
			if target, ok := row[jump.ToID]; ok {
				result.Steps[target].Arrivals = append(result.Steps[target].Arrivals, workflowPathArrival{From: source.Node, RouteKeys: jump.RouteKeys, Back: jump.Back})
			}
		}
	}
	result.Lanes = workflowAssignLanes(result.Steps, row, selectedID)
	return result
}

// workflowToneRank orders the exits key from the ending everyone wants to the
// ones nobody does.
func workflowToneRank(tone string) int {
	switch tone {
	case "positive":
		return 0
	case "neutral":
		return 1
	case "info":
		return 2
	case "warning":
		return 3
	default:
		return 4
	}
}

// workflowSeverityRank orders a step's exit chips worst first, so truncation
// hides the least important one.
func workflowSeverityRank(tone string) int { return 4 - workflowToneRank(tone) }

func workflowEdgeKey(edge WorkflowDraftEdge) string {
	return edge.FromID + "\x00" + edge.RouteKey + "\x00" + edge.ToID
}

// workflowPathDepths walks the step graph (exits excluded) from the start
// node. An edge into a node still on the walk's stack is a loop and is left
// out of the ranking; every other edge pushes its target at least one row
// below its source.
func workflowPathDepths(draft WorkflowDraftView, out map[string][]WorkflowDraftEdge, start string) (map[string]int, map[string]bool) {
	depth := make(map[string]int)
	back := make(map[string]bool)
	if start == "" {
		return depth, back
	}
	exit := make(map[string]bool)
	for _, node := range draft.Nodes {
		if workflowIsExit(node) {
			exit[node.ID] = true
		}
	}
	const (
		unseen = iota
		open
		done
	)
	state := make(map[string]int)
	order := make([]string, 0, len(draft.Nodes))
	var visit func(string)
	visit = func(id string) {
		state[id] = open
		for _, edge := range out[id] {
			if exit[edge.ToID] {
				continue
			}
			switch state[edge.ToID] {
			case unseen:
				visit(edge.ToID)
			case open:
				back[workflowEdgeKey(edge)] = true
			}
		}
		state[id] = done
		order = append(order, id)
	}
	visit(start)
	depth[start] = 0
	for index := len(order) - 1; index >= 0; index-- {
		id := order[index]
		for _, edge := range out[id] {
			if exit[edge.ToID] || back[workflowEdgeKey(edge)] {
				continue
			}
			if candidate := depth[id] + 1; candidate > depth[edge.ToID] {
				depth[edge.ToID] = candidate
			}
		}
	}
	return depth, back
}

// workflowAssignLanes gives every jump the innermost rail lane that is free
// for the rows it spans, then records what each lane looks like as it crosses
// each row. It returns the number of lanes used.
func workflowAssignLanes(steps []workflowPathStep, row map[string]int, selectedID string) int {
	type span struct{ step, jump, low, high int }
	spans := make([]span, 0)
	for stepIndex := range steps {
		for jumpIndex, jump := range steps[stepIndex].Jumps {
			target, ok := row[jump.ToID]
			if !ok {
				continue
			}
			low, high := stepIndex, target
			if low > high {
				low, high = high, low
			}
			spans = append(spans, span{step: stepIndex, jump: jumpIndex, low: low, high: high})
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if left, right := spans[i].high-spans[i].low, spans[j].high-spans[j].low; left != right {
			return left < right
		}
		return spans[i].low < spans[j].low
	})
	occupied := make([][]span, 0)
	for _, candidate := range spans {
		lane := -1
		for index := range occupied {
			free := true
			for _, taken := range occupied[index] {
				if candidate.low <= taken.high && taken.low <= candidate.high {
					free = false
					break
				}
			}
			if free {
				lane = index
				break
			}
		}
		if lane < 0 {
			occupied = append(occupied, nil)
			lane = len(occupied) - 1
		}
		occupied[lane] = append(occupied[lane], candidate)
		steps[candidate.step].Jumps[candidate.jump].Lane = lane
	}
	for index := range steps {
		steps[index].Rail = make([]workflowRailCell, len(occupied))
	}
	for lane, taken := range occupied {
		for _, item := range taken {
			jump := steps[item.step].Jumps[item.jump]
			target := row[jump.ToID]
			active := selectedID != "" && (steps[item.step].Node.ID == selectedID || jump.ToID == selectedID)
			for at := item.low; at <= item.high; at++ {
				cell := &steps[at].Rail[lane]
				cell.Up, cell.Down = at > item.low, at < item.high
				cell.Turn = at == item.step || at == target
				cell.Arrives = at == target
				cell.Back, cell.Active = jump.Back, active
			}
		}
	}
	return len(occupied)
}

// workflowExitToneFor decides how an exit is coloured. An exit named for an
// outcome (end_rejected, end_complete) takes that outcome's tone for good. An
// exit with no such name is neutral: its tone used to be a vote over the names
// of the routes wired into it, so "End 1" turned from green to red as an
// author connected results to it.
func workflowExitToneFor(node WorkflowDraftNode, routeKeys []string) string {
	id := strings.TrimSpace(node.ID)
	if len(id) > 4 && strings.EqualFold(id[:4], "end_") && strings.Trim(id[4:], "0123456789_") != "" {
		if tone := workflowExitTone([]string{id[4:]}); tone != "info" {
			return tone
		}
		switch strings.ToUpper(id[4:]) {
		case "COMPLETE", "DONE", "SUCCESS":
			return "positive"
		case "REPAIR_PLAN", "REPAIR":
			return "warning"
		}
		return workflowExitTone(routeKeys)
	}
	return "neutral"
}

// workflowExitTone reads the compiler's outcome vocabulary, not an author's
// names, to decide how an exit is coloured.
func workflowExitTone(routeKeys []string) string {
	score := map[string]int{}
	for _, key := range routeKeys {
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "REJECTED", "FAIL", "FAILED", "BLOCKED", "DENIED":
			score["danger"]++
		case "EXPIRED", "TIMED_OUT", "LATE", "INVALIDATED", "PARTIAL", "DEGRADED", "REPAIR_REQUIRED", "UNKNOWN", "AMBIGUOUS":
			score["warning"]++
		case "CANCELLED", "WITHDRAWN":
			score["neutral"]++
		case "SUCCEEDED", "APPROVED", "PASS", "VALID", "CONSISTENT", "COMPLETED":
			score["positive"]++
		default:
			score["info"]++
		}
	}
	best, tone := 0, "neutral"
	for _, candidate := range []string{"danger", "warning", "positive", "info", "neutral"} {
		if score[candidate] > best {
			best, tone = score[candidate], candidate
		}
	}
	return tone
}

// workflowPathProblems lists what still stands between this draft and a
// workflow that can run, in the order an author meets the steps.
func workflowPathProblems(path workflowPath, name func(WorkflowDraftNode) string) []workflowPathProblem {
	if name == nil {
		name = workflowStepLabel
	}
	problems := make([]workflowPathProblem, 0)
	for _, step := range path.Loose {
		problems = append(problems, workflowPathProblem{NodeID: step.Node.ID, Key: "workflow_editor.problem_loose", Vars: map[string]string{"step": name(step.Node)}})
	}
	for _, group := range [][]workflowPathStep{path.Steps, path.Loose} {
		for _, step := range group {
			exceptions := 0
			for _, route := range step.Open {
				if workflowOutcomeIsException(route) {
					exceptions++
					continue
				}
				problems = append(problems, workflowPathProblem{NodeID: step.Node.ID, Key: "workflow_editor.problem_open", Vars: map[string]string{"step": name(step.Node), "route": route}})
			}
			// A new step declares four or five exception results. Counted one
			// by one they made the draft look worse each time the author did
			// the right thing; they are one thing to finish, and one select
			// in the step's settings finishes it.
			if exceptions > 0 {
				problems = append(problems, workflowPathProblem{NodeID: step.Node.ID, Key: "workflow_editor.problem_exceptions", Count: int64(exceptions), Vars: map[string]string{"step": name(step.Node)}})
			}
			for _, input := range step.Unbound {
				problems = append(problems, workflowPathProblem{NodeID: step.Node.ID, Key: "workflow_editor.problem_unbound", Vars: map[string]string{"step": name(step.Node), "input": DisplayLabel(input)}})
			}
		}
	}
	if len(path.Steps)+len(path.Loose) > 0 && len(path.Exits) == 0 {
		problems = append(problems, workflowPathProblem{Key: "workflow_editor.problem_no_exit"})
	}
	return problems
}
