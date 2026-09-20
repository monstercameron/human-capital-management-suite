package designeredit

import (
	"context"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

type SetOutcomeRequest struct {
	DraftID          string
	ExpectedRevision uint64
	FromNodeID       string
	RouteKey         string
	ToNodeID         string
}

type BindInputRequest struct {
	DraftID          string
	ExpectedRevision uint64
	TargetNodeID     string
	TargetPath       string
	SourceNodeID     string
	SourcePath       string
}

type OutcomeView struct {
	RouteKey      string
	TargetNodeIDs []string
}

type BindingCandidateView struct {
	NodeID, Path, Type string
}

type BindingView struct {
	TargetPath, TargetType               string
	SourceKind, SourceNodeID, SourcePath string
	Candidates                           []BindingCandidateView
}

// SetOutcome connects one declared outcome port to an existing node. Normal
// outcomes replace their one continuation; a declared fan-out outcome adds a
// distinct target. The edit remains a draft and is compiled separately.
func (s Service) SetOutcome(ctx context.Context, tenant values.TenantId, author string, request SetOutcomeRequest) (Change, error) {
	if request.ExpectedRevision == 0 {
		return Change{}, ErrInvalid
	}
	draft, definition, err := s.loadEditable(ctx, tenant, author, request.DraftID, request.ExpectedRevision)
	if err != nil {
		return Change{}, err
	}
	fromIndex, toIndex := nodePosition(definition.Nodes, request.FromNodeID), nodePosition(definition.Nodes, request.ToNodeID)
	if fromIndex < 0 || toIndex < 0 || strings.TrimSpace(request.RouteKey) == "" {
		return Change{}, ErrNotFound
	}
	from := definition.Nodes[fromIndex]
	if !containsString(workflow.OutcomeRoutes(from), request.RouteKey) {
		return Change{}, ErrInvalid
	}
	multiple := workflow.OutcomeRouteAllowsMultipleTargets(from, request.RouteKey)
	updated := make([]workflow.Edge, 0, len(definition.Edges)+1)
	changed, inserted := false, false
	for _, edge := range definition.Edges {
		if edge.From != request.FromNodeID || edge.RouteKey != request.RouteKey {
			updated = append(updated, edge)
			continue
		}
		if multiple {
			updated = append(updated, edge)
			if edge.To == request.ToNodeID {
				inserted = true
			}
			continue
		}
		if !inserted {
			updated = append(updated, workflow.Edge{From: request.FromNodeID, To: request.ToNodeID, RouteKey: request.RouteKey})
			inserted = true
			changed = changed || edge.To != request.ToNodeID
		} else {
			changed = true
		}
	}
	if !inserted {
		updated = append(updated, workflow.Edge{From: request.FromNodeID, To: request.ToNodeID, RouteKey: request.RouteKey})
		changed = true
	}
	if !changed {
		view, projectErr := s.projectDraft(ctx, tenant, draft)
		return Change{Draft: view}, projectErr
	}
	definition.Edges = updated
	return s.saveDefinition(ctx, tenant, draft, definition, "Connect "+request.FromNodeID+" "+request.RouteKey)
}

// BindInput binds a target input only to a candidate returned by the
// compiler-owned dominance and assignability query.
func (s Service) BindInput(ctx context.Context, tenant values.TenantId, author string, request BindInputRequest) (Change, error) {
	if request.ExpectedRevision == 0 {
		return Change{}, ErrInvalid
	}
	draft, definition, err := s.loadEditable(ctx, tenant, author, request.DraftID, request.ExpectedRevision)
	if err != nil {
		return Change{}, err
	}
	targetIndex := nodePosition(definition.Nodes, request.TargetNodeID)
	if targetIndex < 0 || strings.TrimSpace(request.TargetPath) == "" {
		return Change{}, ErrNotFound
	}
	admitted := false
	for _, candidate := range workflow.OutputBindingCandidates(definition, request.TargetNodeID, request.TargetPath) {
		if candidate.NodeID == request.SourceNodeID && candidate.Path == request.SourcePath {
			admitted = true
			break
		}
	}
	if !admitted {
		return Change{}, ErrInvalid
	}
	target := &definition.Nodes[targetIndex]
	source := workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: request.SourceNodeID, Path: request.SourcePath}
	for index := range target.InputMappings {
		if target.InputMappings[index].Target != request.TargetPath {
			continue
		}
		if target.InputMappings[index].Source == source {
			view, projectErr := s.projectDraft(ctx, tenant, draft)
			return Change{Draft: view}, projectErr
		}
		target.InputMappings[index].Source = source
		return s.saveDefinition(ctx, tenant, draft, definition, "Bind "+request.TargetNodeID+"."+request.TargetPath)
	}
	target.InputMappings = append(target.InputMappings, workflow.Mapping{Target: request.TargetPath, Source: source})
	return s.saveDefinition(ctx, tenant, draft, definition, "Bind "+request.TargetNodeID+"."+request.TargetPath)
}

func outcomeViews(definition workflow.Definition, node workflow.Node) []OutcomeView {
	byRoute := make(map[string][]string)
	for _, edge := range definition.Edges {
		if edge.From == node.ID {
			byRoute[edge.RouteKey] = append(byRoute[edge.RouteKey], edge.To)
		}
	}
	result := make([]OutcomeView, 0)
	for _, route := range workflow.OutcomeRoutes(node) {
		targets := append([]string(nil), byRoute[route]...)
		sort.Strings(targets)
		result = append(result, OutcomeView{RouteKey: route, TargetNodeIDs: targets})
	}
	return result
}

func bindingViews(definition workflow.Definition, node workflow.Node) []BindingView {
	current := make(map[string]workflow.Source, len(node.InputMappings))
	for _, mapping := range node.InputMappings {
		current[mapping.Target] = mapping.Source
	}
	result := make([]BindingView, 0, len(node.Inputs))
	for _, input := range node.Inputs {
		source := current[input.Path]
		view := BindingView{TargetPath: input.Path, TargetType: input.Type.String(), SourceKind: string(source.Kind), SourceNodeID: source.NodeID, SourcePath: source.Path}
		for _, candidate := range workflow.OutputBindingCandidates(definition, node.ID, input.Path) {
			view.Candidates = append(view.Candidates, BindingCandidateView{NodeID: candidate.NodeID, Path: candidate.Path, Type: candidate.Type.String()})
		}
		result = append(result, view)
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
