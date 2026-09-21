package productclient

import (
	"errors"
	"fmt"
	"strings"
	"time"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workflowview"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func projectWorkflowCatalog(values []*workflowv1.WorkflowPublicationSummary) []productui.WorkflowCatalogItem {
	result := make([]productui.WorkflowCatalogItem, 0, len(values))
	for _, value := range values {
		if value == nil || strings.TrimSpace(value.GetWorkflowId()) == "" || workflowversion.ValidateSemanticVersion(value.GetSemanticVersion()) != nil {
			continue
		}
		result = append(result, productui.WorkflowCatalogItem{
			WorkflowID: value.GetWorkflowId(), Name: value.GetName(), Version: value.GetDefinitionVersion(),
			SemanticVersion: value.GetSemanticVersion(), Status: value.GetStatus(),
		})
	}
	return result
}

func projectWorkflowPalette(values []*workflowv1.WorkflowPaletteEntry) []productui.WorkflowPaletteItem {
	result := make([]productui.WorkflowPaletteItem, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == nil || strings.TrimSpace(value.GetId()) == "" || value.GetVersion() == 0 ||
			strings.TrimSpace(value.GetName()) == "" || strings.TrimSpace(value.GetDomain()) == "" ||
			strings.TrimSpace(value.GetEffectClass()) == "" || strings.TrimSpace(value.GetReversal()) == "" {
			continue
		}
		kind := strings.ToUpper(strings.TrimSpace(value.GetKind()))
		if kind != "BLOCK" && kind != "FRAGMENT" && kind != "TEMPLATE" {
			continue
		}
		key := fmt.Sprintf("%s@%d", value.GetId(), value.GetVersion())
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, productui.WorkflowPaletteItem{
			ID: strings.TrimSpace(value.GetId()), Version: value.GetVersion(), Name: strings.TrimSpace(value.GetName()), Kind: kind,
			Domain: value.GetDomain(), Description: value.GetDescription(), EffectClass: value.GetEffectClass(),
			Reversal: value.GetReversal(), Status: value.GetStatus(), StepType: value.GetStepType(),
		})
	}
	return result
}

func projectWorkflowDraft(value *workflowv1.WorkflowDraftView) (productui.WorkflowDraftView, error) {
	if value == nil || strings.TrimSpace(value.GetDraftId()) == "" || strings.TrimSpace(value.GetWorkflowId()) == "" || value.GetRevision() == 0 || workflowversion.ValidateSemanticVersion(value.GetSemanticVersion()) != nil {
		return productui.WorkflowDraftView{}, errors.New("project workflow draft: response has no draft identity")
	}
	result := productui.WorkflowDraftView{
		DraftID: value.GetDraftId(), WorkflowID: value.GetWorkflowId(), Name: value.GetName(), SemanticVersion: value.GetSemanticVersion(), BaseVersionDigest: value.GetBaseVersionDigest(), Revision: value.GetRevision(),
		StartNodeID: value.GetStartNodeId(), DefinitionDigest: value.GetDefinitionDigest(), BaseDefinitionDigest: value.GetBaseDefinitionDigest(), MatchesBaseDefinition: value.GetMatchesBaseDefinition(),
		TemplateDefinitionDigest: value.GetTemplateDefinitionDigest(), MatchesTemplateDefinition: value.GetMatchesTemplateDefinition(), TemplateID: value.GetTemplateId(), TemplateVersion: value.GetTemplateVersion(),
		CanUndo: value.GetCanUndo(), CanRedo: value.GetCanRedo(), HistoryPosition: value.GetHistoryPosition(), HistoryLength: value.GetHistoryLength(),
		HistoryLabel: value.GetHistoryLabel(), LayoutMode: value.GetLayoutMode(),
		Nodes: make([]productui.WorkflowDraftNode, 0, len(value.GetNodes())), Edges: make([]productui.WorkflowDraftEdge, 0, len(value.GetEdges())), Groups: make([]productui.WorkflowDraftGroup, 0, len(value.GetGroups())),
	}
	if result.HistoryPosition == 0 && result.HistoryLength == 0 {
		result.HistoryPosition, result.HistoryLength = 1, 1
	}
	if result.HistoryPosition == 0 || result.HistoryPosition > result.HistoryLength || result.CanUndo != (result.HistoryPosition > 1) || result.CanRedo != (result.HistoryPosition < result.HistoryLength) {
		return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains invalid history state")
	}
	if result.LayoutMode == "" {
		result.LayoutMode = "AUTO"
	}
	if result.LayoutMode != "AUTO" {
		return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an unsupported layout mode")
	}
	for _, change := range value.GetSemanticChanges() {
		if change == nil || strings.TrimSpace(change.GetSubjectId()) == "" {
			return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid semantic change")
		}
		kind, operation := strings.ToUpper(strings.TrimSpace(change.GetKind())), strings.ToUpper(strings.TrimSpace(change.GetOperation()))
		if (kind != "NODE" && kind != "EDGE" && kind != "BINDING" && kind != "PARAMETER") ||
			(operation != "ADDED" && operation != "REMOVED" && operation != "UPDATED") {
			return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid semantic change kind")
		}
		result.Changes = append(result.Changes, productui.WorkflowDraftSemanticChange{
			Kind: kind, Operation: operation, SubjectID: change.GetSubjectId(), Field: change.GetField(), Before: change.GetBefore(), After: change.GetAfter(),
		})
	}
	if value.GetExpiresAt() != nil && value.GetExpiresAt().CheckValid() == nil {
		result.ExpiresAt = value.GetExpiresAt().AsTime().UTC().Format(time.RFC3339)
	}
	nodes := make(map[string]bool, len(value.GetNodes()))
	for _, node := range value.GetNodes() {
		if node == nil || strings.TrimSpace(node.GetId()) == "" || nodes[node.GetId()] {
			return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid node")
		}
		nodes[node.GetId()] = true
	}
	for _, node := range value.GetNodes() {
		projected := productui.WorkflowDraftNode{ID: node.GetId(), StepType: node.GetStepType(), GroupID: node.GetGroupId(), Label: node.GetLabel(), Locked: node.GetLocked(), LockKind: node.GetLockKind(), Parameters: make([]productui.WorkflowNodeParameter, 0, len(node.GetParameters())), Outcomes: make([]productui.WorkflowDraftOutcome, 0, len(node.GetOutcomes())), Bindings: make([]productui.WorkflowDraftBinding, 0, len(node.GetBindings()))}
		for _, parameter := range node.GetParameters() {
			if parameter == nil || strings.TrimSpace(parameter.GetId()) == "" || strings.TrimSpace(parameter.GetLabel()) == "" {
				return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid parameter")
			}
			kind := strings.ToUpper(strings.TrimSpace(parameter.GetKind()))
			if kind != "TEXT" && kind != "INTEGER" && kind != "ENUM" {
				return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid parameter kind")
			}
			projected.Parameters = append(projected.Parameters, productui.WorkflowNodeParameter{ID: parameter.GetId(), Label: parameter.GetLabel(), Kind: kind, Value: parameter.GetValue(), Required: parameter.GetRequired(), Minimum: parameter.GetMinimum(), Maximum: parameter.GetMaximum(), Options: append([]string(nil), parameter.GetOptions()...)})
		}
		seenOutcomes := make(map[string]bool, len(node.GetOutcomes()))
		for _, outcome := range node.GetOutcomes() {
			if outcome == nil || strings.TrimSpace(outcome.GetRouteKey()) == "" || seenOutcomes[outcome.GetRouteKey()] {
				return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid outcome")
			}
			seenOutcomes[outcome.GetRouteKey()] = true
			for _, target := range outcome.GetTargetNodeIds() {
				if !nodes[target] {
					return productui.WorkflowDraftView{}, errors.New("project workflow draft: outcome references an unknown node")
				}
			}
			projected.Outcomes = append(projected.Outcomes, productui.WorkflowDraftOutcome{RouteKey: outcome.GetRouteKey(), TargetNodeIDs: append([]string(nil), outcome.GetTargetNodeIds()...)})
		}
		seenBindings := make(map[string]bool, len(node.GetBindings()))
		for _, binding := range node.GetBindings() {
			if binding == nil || strings.TrimSpace(binding.GetTargetPath()) == "" || strings.TrimSpace(binding.GetTargetType()) == "" || seenBindings[binding.GetTargetPath()] {
				return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid binding")
			}
			seenBindings[binding.GetTargetPath()] = true
			if binding.GetSourceNodeId() != "" && !nodes[binding.GetSourceNodeId()] {
				return productui.WorkflowDraftView{}, errors.New("project workflow draft: binding references an unknown source node")
			}
			projectedBinding := productui.WorkflowDraftBinding{TargetPath: binding.GetTargetPath(), TargetType: binding.GetTargetType(), SourceKind: binding.GetSourceKind(), SourceNodeID: binding.GetSourceNodeId(), SourcePath: binding.GetSourcePath(), Candidates: make([]productui.WorkflowDraftBindingCandidate, 0, len(binding.GetCandidates()))}
			seenCandidates := make(map[string]bool, len(binding.GetCandidates()))
			for _, candidate := range binding.GetCandidates() {
				key := candidate.GetSourceNodeId() + "\x00" + candidate.GetSourcePath()
				if candidate == nil || !nodes[candidate.GetSourceNodeId()] || strings.TrimSpace(candidate.GetSourcePath()) == "" || strings.TrimSpace(candidate.GetValueType()) == "" || seenCandidates[key] {
					return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid binding candidate")
				}
				seenCandidates[key] = true
				projectedBinding.Candidates = append(projectedBinding.Candidates, productui.WorkflowDraftBindingCandidate{SourceNodeID: candidate.GetSourceNodeId(), SourcePath: candidate.GetSourcePath(), ValueType: candidate.GetValueType()})
			}
			projected.Bindings = append(projected.Bindings, projectedBinding)
		}
		result.Nodes = append(result.Nodes, projected)
	}
	if len(nodes) > 0 && !nodes[result.StartNodeID] {
		return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid start node")
	}
	if result.MatchesBaseDefinition && (strings.TrimSpace(result.DefinitionDigest) == "" || result.DefinitionDigest != result.BaseDefinitionDigest) {
		return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains a false base-definition match")
	}
	if result.MatchesTemplateDefinition && (strings.TrimSpace(result.TemplateID) == "" || result.TemplateVersion == 0 || strings.TrimSpace(result.DefinitionDigest) == "" || result.DefinitionDigest != result.TemplateDefinitionDigest) {
		return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains a false template-definition match")
	}
	for _, edge := range value.GetEdges() {
		if edge == nil || !nodes[edge.GetFromId()] || !nodes[edge.GetToId()] || strings.TrimSpace(edge.GetRouteKey()) == "" {
			return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid edge")
		}
		result.Edges = append(result.Edges, productui.WorkflowDraftEdge{FromID: edge.GetFromId(), ToID: edge.GetToId(), RouteKey: edge.GetRouteKey()})
	}
	seenGroups := make(map[string]bool, len(value.GetGroups()))
	for _, group := range value.GetGroups() {
		if group == nil || strings.TrimSpace(group.GetId()) == "" || seenGroups[group.GetId()] {
			return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid group")
		}
		seenGroups[group.GetId()] = true
		for _, nodeID := range group.GetNodeIds() {
			if !nodes[nodeID] {
				return productui.WorkflowDraftView{}, errors.New("project workflow draft: group references an unknown node")
			}
		}
		result.Groups = append(result.Groups, productui.WorkflowDraftGroup{ID: group.GetId(), Name: group.GetName(), EntryID: group.GetEntryId(), EntryVersion: group.GetEntryVersion(), Collapsed: group.GetCollapsed(), NodeIDs: append([]string(nil), group.GetNodeIds()...)})
	}
	for _, overlay := range value.GetOverlays() {
		if overlay == nil {
			return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid overlay")
		}
		operation := strings.ToUpper(strings.TrimSpace(overlay.GetOperation()))
		if operation != "ADD" && operation != "OMIT" && operation != "REPLACE" {
			return productui.WorkflowDraftView{}, errors.New("project workflow draft: response contains an invalid overlay operation")
		}
		result.Overlays = append(result.Overlays, productui.WorkflowTemplateOverlay{Operation: operation, TargetNodeID: overlay.GetTargetNodeId(), EntryID: overlay.GetEntryId(), EntryVersion: overlay.GetEntryVersion(), Reason: overlay.GetReason()})
	}
	return result, nil
}

func timePointer(value *timestamppb.Timestamp) *time.Time {
	if value == nil || value.CheckValid() != nil {
		return nil
	}
	resolved := value.AsTime().UTC()
	return &resolved
}

func projectWorkflowView(value *workflowv1.WorkflowDefinitionView) (workflowview.View, error) {
	if value == nil || strings.TrimSpace(value.GetWorkflowId()) == "" || strings.TrimSpace(value.GetCompiledPlanDigest()) == "" || workflowversion.ValidateSemanticVersion(value.GetSemanticVersion()) != nil {
		return workflowview.View{}, errors.New("project workflow definition view: response has no workflow identity")
	}
	result := workflowview.View{
		WorkflowID: value.GetWorkflowId(), Name: value.GetName(), Version: value.GetDefinitionVersion(),
		SemanticVersion: value.GetSemanticVersion(), PlanDigest: value.GetCompiledPlanDigest(),
		PublicationStatus: value.GetPublicationStatus(), HasRun: value.GetHasRun(), RunDisclosed: value.GetRunDisclosed(),
		InstanceID: value.GetInstanceId(), RuntimeStatus: value.GetRuntimeStatus(), Completeness: value.GetComplete(),
		Redactions: append([]string(nil), value.GetRedactions()...), Gaps: append([]string(nil), value.GetGaps()...),
		MaxDepth: int(value.GetMaxDepth()), MaxLane: int(value.GetMaxLane()),
		Nodes: make([]workflowview.Node, 0, len(value.GetNodes())), Edges: make([]workflowview.Edge, 0, len(value.GetEdges())),
	}
	seenNodes := make(map[string]bool, len(value.GetNodes()))
	for _, node := range value.GetNodes() {
		if node == nil || strings.TrimSpace(node.GetId()) == "" || seenNodes[node.GetId()] {
			return workflowview.View{}, fmt.Errorf("project workflow definition view: invalid or duplicate node %q", node.GetId())
		}
		seenNodes[node.GetId()] = true
		projected := workflowview.Node{
			ID: node.GetId(), Label: node.GetLabel(), StepType: node.GetStepType(), Depth: int(node.GetDepth()), Lane: int(node.GetLane()),
			Start: node.GetStart(), Terminal: node.GetTerminal(), Current: node.GetCurrent(), Attempt: int(node.GetAttempt()),
			Status: node.GetStatus(), State: workflowview.NodeState(node.GetState()), RuntimeKnown: node.GetRuntimeKnown(), RuntimeGap: node.GetRuntimeGap(),
			StartedAt: timePointer(node.GetStartedAt()), CompletedAt: timePointer(node.GetCompletedAt()),
			Routes: make([]workflowview.Route, 0, len(node.GetRoutes())),
		}
		for _, route := range node.GetRoutes() {
			if route == nil || strings.TrimSpace(route.GetKey()) == "" || strings.TrimSpace(route.GetTargetId()) == "" {
				return workflowview.View{}, fmt.Errorf("project workflow definition view: node %q has an invalid route", node.GetId())
			}
			projected.Routes = append(projected.Routes, workflowview.Route{Key: route.GetKey(), TargetID: route.GetTargetId()})
		}
		result.Nodes = append(result.Nodes, projected)
	}
	for _, edge := range value.GetEdges() {
		if edge == nil || !seenNodes[edge.GetFromId()] || !seenNodes[edge.GetToId()] || strings.TrimSpace(edge.GetRouteKey()) == "" {
			return workflowview.View{}, errors.New("project workflow definition view: response contains an invalid edge")
		}
		result.Edges = append(result.Edges, workflowview.Edge{ID: edge.GetId(), FromID: edge.GetFromId(), ToID: edge.GetToId(), RouteKey: edge.GetRouteKey()})
	}
	return result, nil
}
