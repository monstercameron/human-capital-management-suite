package workflowview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var (
	// ErrInvalidPublication means the immutable record or its canonical plan
	// bytes do not describe one coherent publication.
	ErrInvalidPublication = errors.New("workflowview: invalid publication")
	// ErrRunMismatch means a disclosed live run pins a different workflow or
	// compiled plan. Combining them would falsely color the published graph.
	ErrRunMismatch = errors.New("workflowview: run does not match publication")
)

// Build creates the single read model used by both the visual graph and its
// accessible outline. A nil live view produces a published-definition view.
// A redacted live definition produces the same graph without a runtime
// overlay and preserves the inspector's redaction/completeness evidence.
func Build(publication version.CompiledVersion, live *inspect.View) (View, error) {
	if err := publication.Verify(); err != nil {
		return View{}, fmt.Errorf("%w: %v", ErrInvalidPublication, err)
	}
	plan, err := decodePlan(publication.CanonicalPlanBytes)
	if err != nil {
		return View{}, err
	}
	if plan.WorkflowID != publication.WorkflowID ||
		plan.Version != publication.DefinitionVersion ||
		plan.CompilerVersion != publication.CompilerVersion {
		return View{}, fmt.Errorf("%w: record and canonical plan identities differ", ErrInvalidPublication)
	}

	ordered, depths, err := orderedNodes(plan)
	if err != nil {
		return View{}, err
	}
	lanes := make(map[int]int)
	result := View{
		WorkflowID: publication.WorkflowID, Name: plan.Name,
		Version: publication.DefinitionVersion, SemanticVersion: publication.SemanticVersion,
		PlanDigest: publication.CompiledPlanDigest, PublicationStatus: string(publication.Status),
		Completeness: true,
	}
	nodeIndex := make(map[string]int, len(ordered))
	terminal := make(map[string]bool, len(plan.Terminals))
	for _, item := range plan.Terminals {
		terminal[item.NodeID] = true
	}
	for _, compiled := range ordered {
		depth := depths[compiled.ID]
		lane := lanes[depth]
		lanes[depth]++
		if depth > result.MaxDepth {
			result.MaxDepth = depth
		}
		if lane > result.MaxLane {
			result.MaxLane = lane
		}
		nodeIndex[compiled.ID] = len(result.Nodes)
		result.Nodes = append(result.Nodes, Node{
			ID: compiled.ID, Label: humanLabel(compiled.ID), StepType: string(compiled.Type),
			Depth: depth, Lane: lane, Start: compiled.ID == plan.StartNodeID,
			Terminal: terminal[compiled.ID], State: NodeNotStarted,
		})
	}
	for index, edge := range plan.Edges {
		from, fromOK := nodeIndex[edge.From]
		_, toOK := nodeIndex[edge.To]
		if !fromOK || !toOK || strings.TrimSpace(edge.RouteKey) == "" {
			return View{}, fmt.Errorf("%w: edge %d has an unknown endpoint or empty route", ErrInvalidPublication, index)
		}
		result.Edges = append(result.Edges, Edge{
			ID: edgeID(edge, index), FromID: edge.From, ToID: edge.To, RouteKey: edge.RouteKey,
		})
		result.Nodes[from].Routes = append(result.Nodes[from].Routes, Route{Key: edge.RouteKey, TargetID: edge.To})
	}
	for index := range result.Nodes {
		sort.SliceStable(result.Nodes[index].Routes, func(i, j int) bool {
			left, right := result.Nodes[index].Routes[i], result.Nodes[index].Routes[j]
			if left.Key == right.Key {
				return left.TargetID < right.TargetID
			}
			return left.Key < right.Key
		})
	}
	if live != nil {
		if err := overlay(&result, *live, nodeIndex); err != nil {
			return View{}, err
		}
	}
	return result, nil
}

func decodePlan(data []byte) (workflow.CompiledWorkflow, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return workflow.CompiledWorkflow{}, fmt.Errorf("%w: canonical plan is empty", ErrInvalidPublication)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var plan workflow.CompiledWorkflow
	if err := decoder.Decode(&plan); err != nil {
		return workflow.CompiledWorkflow{}, fmt.Errorf("%w: decode canonical plan: %v", ErrInvalidPublication, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return workflow.CompiledWorkflow{}, fmt.Errorf("%w: canonical plan has trailing data", ErrInvalidPublication)
	}
	if strings.TrimSpace(plan.WorkflowID) == "" || strings.TrimSpace(plan.StartNodeID) == "" || len(plan.Nodes) == 0 {
		return workflow.CompiledWorkflow{}, fmt.Errorf("%w: canonical plan lacks identity, start, or nodes", ErrInvalidPublication)
	}
	return plan, nil
}

func orderedNodes(plan workflow.CompiledWorkflow) ([]workflow.CompiledNode, map[string]int, error) {
	byID := make(map[string]workflow.CompiledNode, len(plan.Nodes))
	for _, node := range plan.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return nil, nil, fmt.Errorf("%w: canonical plan has a node without an id", ErrInvalidPublication)
		}
		if _, exists := byID[node.ID]; exists {
			return nil, nil, fmt.Errorf("%w: duplicate node %q", ErrInvalidPublication, node.ID)
		}
		byID[node.ID] = node
	}
	if _, ok := byID[plan.StartNodeID]; !ok {
		return nil, nil, fmt.Errorf("%w: start node %q is absent", ErrInvalidPublication, plan.StartNodeID)
	}

	order := append([]string(nil), plan.Reachability.Order...)
	if len(order) != len(plan.Nodes) {
		order = make([]string, 0, len(plan.Nodes))
		for _, node := range plan.Nodes {
			order = append(order, node.ID)
		}
	}
	seen := make(map[string]bool, len(order))
	ordered := make([]workflow.CompiledNode, 0, len(order))
	depths := make(map[string]int, len(order))
	for _, id := range order {
		node, ok := byID[id]
		if !ok || seen[id] {
			return nil, nil, fmt.Errorf("%w: reachability order is not a node permutation", ErrInvalidPublication)
		}
		seen[id] = true
		ordered = append(ordered, node)
		depths[id] = int(plan.Reachability.Depth[id])
	}
	return ordered, depths, nil
}

func overlay(result *View, live inspect.View, nodeIndex map[string]int) error {
	result.HasRun = true
	result.RunDisclosed = live.Definition.Disclosed && live.Instance.Disclosed
	result.Completeness = live.Completeness.Complete
	result.Redactions = append([]string(nil), live.Completeness.Redactions...)
	result.Gaps = append([]string(nil), live.Completeness.Gaps...)
	if !result.RunDisclosed {
		return nil
	}
	if live.Definition.WorkflowID != result.WorkflowID ||
		live.Definition.WorkflowVersion != result.Version ||
		live.Definition.CompiledPlanHash != result.PlanDigest {
		return fmt.Errorf("%w: inspector pins %s@%d/%s, publication is %s@%d/%s",
			ErrRunMismatch, live.Definition.WorkflowID, live.Definition.WorkflowVersion,
			live.Definition.CompiledPlanHash, result.WorkflowID, result.Version, result.PlanDigest)
	}
	result.InstanceID = live.Instance.InstanceID
	result.RuntimeStatus = live.Instance.RuntimeStatus
	latest := make(map[string]inspect.NodeView, len(live.Nodes))
	for _, item := range live.Nodes {
		if previous, ok := latest[item.NodeID]; !ok || item.Attempt > previous.Attempt {
			latest[item.NodeID] = item
		}
	}
	for nodeID, item := range latest {
		index, ok := nodeIndex[nodeID]
		if !ok {
			result.Gaps = append(result.Gaps, "runtime node not present in publication: "+nodeID)
			result.Completeness = false
			continue
		}
		node := &result.Nodes[index]
		node.RuntimeKnown = true
		node.Attempt = item.Attempt
		node.Status = item.Status
		node.Current = item.Current
		node.State = visualState(item.Status, item.Current)
		node.StartedAt = cloneTime(item.StartedAt)
		node.CompletedAt = cloneTime(item.CompletedAt)
	}
	for _, frontier := range live.Frontier {
		index, ok := nodeIndex[frontier.NodeID]
		if !ok {
			result.Gaps = append(result.Gaps, "frontier node not present in publication: "+frontier.NodeID)
			result.Completeness = false
			continue
		}
		node := &result.Nodes[index]
		node.Current = true
		if !frontier.AttemptRecorded {
			node.RuntimeGap = true
			node.State = NodeUnknown
			result.Completeness = false
			continue
		}
		if !node.RuntimeKnown {
			node.RuntimeKnown = true
			node.Attempt = frontier.Attempt
			node.Status = frontier.Status
			node.State = visualState(frontier.Status, true)
		}
	}
	result.Gaps = uniqueSorted(result.Gaps)
	result.Redactions = uniqueSorted(result.Redactions)
	return nil
}

func visualState(status string, current bool) NodeState {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "COMPLETED", "COMPLETE", "SUCCEEDED", "SUCCESS":
		return NodeSucceeded
	case "FAILED", "ERROR", "DEAD_LETTERED":
		return NodeFailed
	case "CANCELLED", "CANCELED", "REVERSED":
		return NodeCancelled
	case "WAITING", "PENDING", "BLOCKED", "AWAITING_APPROVAL", "AWAITING_SIGNAL", "RETRY_BACKOFF":
		return NodeWaiting
	case "RUNNING", "STARTED", "EXECUTING", "CLAIMED":
		return NodeRunning
	case "":
		if current {
			return NodeCurrent
		}
		return NodeNotStarted
	default:
		if current {
			return NodeCurrent
		}
		return NodeUnknown
	}
}

func humanLabel(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '_' || r == '-' || r == '.' || r == '/' })
	for i, part := range parts {
		runes := []rune(strings.ToLower(part))
		if len(runes) > 0 {
			runes[0] = unicode.ToUpper(runes[0])
		}
		parts[i] = string(runes)
	}
	if len(parts) == 0 {
		return id
	}
	return strings.Join(parts, " ")
}

func edgeID(edge workflow.Edge, index int) string {
	return fmt.Sprintf("edge-%03d-%s-%s-%s", index, safeToken(edge.From), safeToken(edge.RouteKey), safeToken(edge.To))
}

func safeToken(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return unicode.ToLower(r)
		}
		return '-'
	}, value)
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
