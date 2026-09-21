package workflow

import (
	"context"
	"errors"
	"strings"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func (s *server) CreateWorkflowDraft(ctx context.Context, req *workflowv1.CreateWorkflowDraftRequest) (*workflowv1.CreateWorkflowDraftResponse, error) {
	principal, invocation, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if !s.authorized(principal, ActionCreateWorkflowDraft) {
		return nil, denied(invocation, principal)
	}
	if s.deps.DraftAuthoring == nil {
		return nil, projectDraftAuthoringError(errDraftAuthoringUnavailable, invocation, principal)
	}
	if req == nil || (strings.TrimSpace(req.GetTemplateId()) == "") != (req.GetTemplateVersion() == 0) {
		return nil, invalid(invocation, "template_id")
	}
	workflowID, semanticVersion, baseVersionDigest, identityErr := s.resolveDraftIdentity(ctx, principal, req)
	if identityErr != nil {
		return nil, projectDraftAuthoringError(identityErr, invocation, principal)
	}
	templateID, templateVersion := strings.TrimSpace(req.GetTemplateId()), req.GetTemplateVersion()
	if templateID == "" && baseVersionDigest != "" {
		var templateErr error
		templateID, templateVersion, templateErr = s.templateForPublishedBase(ctx, principal.Tenant(), workflowID, baseVersionDigest)
		if templateErr != nil {
			return nil, projectDraftAuthoringError(templateErr, invocation, principal)
		}
	}
	change, createErr := s.deps.DraftAuthoring.Create(ctx, principal.Tenant(), principal.Subject(), designeredit.CreateRequest{
		WorkflowID: workflowID, Name: req.GetName(), SemanticVersion: semanticVersion, BaseVersionDigest: baseVersionDigest,
		TemplateID: templateID, TemplateVersion: templateVersion,
	})
	if createErr != nil {
		return nil, projectDraftAuthoringError(createErr, invocation, principal)
	}
	return &workflowv1.CreateWorkflowDraftResponse{Draft: s.projectDraftView(ctx, principal.Tenant(), change.Draft), InsertedNodeIds: append([]string(nil), change.InsertedNodeIDs...), GroupId: change.GroupID}, nil
}

// templateForPublishedBase keeps immutable shipped workflows editable only by
// successor: when the tenant-authorized palette contains the exact source
// definition that produced the selected publication, the new draft starts as
// a byte-for-byte copy of that definition. A workflow ID match alone is not
// enough; the definition digest is the proof that the editor and runtime are
// describing the same artifact.
func (s *server) templateForPublishedBase(ctx context.Context, tenant values.TenantId, workflowID, baseVersionDigest string) (string, uint32, error) {
	if s == nil || s.deps.Definitions == nil || s.deps.Palette == nil {
		return "", 0, nil
	}
	base, found, err := s.deps.Definitions.GetByDigest(baseVersionDigest)
	if err != nil {
		return "", 0, err
	}
	if !found || base.WorkflowID != workflowID {
		return "", 0, designeredit.ErrInvalid
	}
	var matched designerpalette.Entry
	for _, entry := range s.deps.Palette.List(ctx, tenant) {
		if entry.Kind != designerpalette.KindTemplate || entry.Expansion.Template == nil || entry.Expansion.Template.WorkflowID != workflowID {
			continue
		}
		exactSource := workflowversion.DefinitionDigest(*entry.Expansion.Template) == base.DefinitionDigest
		exactPlan := strings.TrimSpace(entry.PublishedPlanDigest) != "" && entry.PublishedPlanDigest == base.CompiledPlanDigest
		if !exactSource && !exactPlan {
			continue
		}
		if matched.ID == "" || entry.Version > matched.Version {
			matched = entry
		}
	}
	return matched.ID, matched.Version, nil
}

func (s *server) resolveDraftIdentity(ctx context.Context, principal *trust.Principal, req *workflowv1.CreateWorkflowDraftRequest) (string, string, string, error) {
	workflowID := strings.TrimSpace(req.GetWorkflowId())
	semanticVersion := strings.TrimSpace(req.GetSemanticVersion())
	baseVersionDigest := strings.TrimSpace(req.GetBaseVersionDigest())
	if semanticVersion != "" && workflowversion.ValidateSemanticVersion(semanticVersion) != nil {
		return "", "", "", designeredit.ErrInvalid
	}
	if workflowID == "" && strings.TrimSpace(req.GetTemplateId()) != "" && s.deps.Palette != nil {
		for _, entry := range s.deps.Palette.List(ctx, principal.Tenant()) {
			if entry.ID == req.GetTemplateId() && entry.Version == req.GetTemplateVersion() && entry.Kind == designerpalette.KindTemplate && entry.Expansion.Template != nil {
				workflowID = strings.TrimSpace(entry.Expansion.Template.WorkflowID)
				break
			}
		}
	}
	if baseVersionDigest != "" {
		if s.deps.Definitions == nil {
			return "", "", "", designeredit.ErrInvalid
		}
		base, found, err := s.deps.Definitions.GetByDigest(baseVersionDigest)
		if err != nil {
			return "", "", "", err
		}
		if !found || workflowID == "" || base.WorkflowID != workflowID {
			return "", "", "", designeredit.ErrInvalid
		}
		if semanticVersion == "" {
			semanticVersion, err = workflowversion.NextPatchVersion(base.SemanticVersion)
			if err != nil {
				return "", "", "", designeredit.ErrInvalid
			}
		}
		order, err := workflowversion.CompareSemanticVersions(semanticVersion, base.SemanticVersion)
		if err != nil || order <= 0 {
			return "", "", "", designeredit.ErrInvalid
		}
		return workflowID, semanticVersion, baseVersionDigest, nil
	}
	if workflowID != "" && s.deps.Definitions != nil {
		base, found, err := resolvePublication(s.deps.Definitions, workflowID, "")
		if err != nil {
			return "", "", "", err
		}
		if found {
			baseVersionDigest = base.CompiledPlanDigest
			if semanticVersion == "" {
				semanticVersion, err = workflowversion.NextPatchVersion(base.SemanticVersion)
				if err != nil {
					return "", "", "", designeredit.ErrInvalid
				}
			}
			order, err := workflowversion.CompareSemanticVersions(semanticVersion, base.SemanticVersion)
			if err != nil || order <= 0 {
				return "", "", "", designeredit.ErrInvalid
			}
		}
	}
	return workflowID, semanticVersion, baseVersionDigest, nil
}

func (s *server) GetWorkflowDraft(ctx context.Context, req *workflowv1.GetWorkflowDraftRequest) (*workflowv1.GetWorkflowDraftResponse, error) {
	principal, invocation, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if !s.authorized(principal, ActionGetWorkflowDraft) {
		return nil, denied(invocation, principal)
	}
	if s.deps.DraftAuthoring == nil {
		return nil, projectDraftAuthoringError(errDraftAuthoringUnavailable, invocation, principal)
	}
	if req == nil || strings.TrimSpace(req.GetDraftId()) == "" {
		return nil, invalid(invocation, "draft_id")
	}
	view, getErr := s.deps.DraftAuthoring.Get(ctx, principal.Tenant(), principal.Subject(), req.GetDraftId())
	if getErr != nil {
		return nil, projectDraftAuthoringError(getErr, invocation, principal)
	}
	return &workflowv1.GetWorkflowDraftResponse{Draft: s.projectDraftView(ctx, principal.Tenant(), view)}, nil
}

func (s *server) InsertWorkflowPaletteEntry(ctx context.Context, req *workflowv1.InsertWorkflowPaletteEntryRequest) (*workflowv1.InsertWorkflowPaletteEntryResponse, error) {
	principal, invocation, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if !s.authorized(principal, ActionInsertWorkflowPaletteEntry) {
		return nil, denied(invocation, principal)
	}
	if s.deps.DraftAuthoring == nil {
		return nil, projectDraftAuthoringError(errDraftAuthoringUnavailable, invocation, principal)
	}
	if req == nil || strings.TrimSpace(req.GetDraftId()) == "" || req.GetExpectedRevision() == 0 || strings.TrimSpace(req.GetEntryId()) == "" || req.GetEntryVersion() == 0 {
		return nil, invalid(invocation, "draft_edit")
	}
	change, insertErr := s.deps.DraftAuthoring.Insert(ctx, principal.Tenant(), principal.Subject(), designeredit.InsertRequest{
		DraftID: req.GetDraftId(), ExpectedRevision: req.GetExpectedRevision(), EntryID: req.GetEntryId(), EntryVersion: req.GetEntryVersion(),
	})
	if insertErr != nil {
		return nil, projectDraftAuthoringError(insertErr, invocation, principal)
	}
	return &workflowv1.InsertWorkflowPaletteEntryResponse{Draft: s.projectDraftView(ctx, principal.Tenant(), change.Draft), InsertedNodeIds: append([]string(nil), change.InsertedNodeIDs...), GroupId: change.GroupID}, nil
}

func (s *server) UpdateWorkflowDraftNode(ctx context.Context, req *workflowv1.UpdateWorkflowDraftNodeRequest) (*workflowv1.UpdateWorkflowDraftNodeResponse, error) {
	principal, invocation, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if !s.authorized(principal, ActionUpdateWorkflowDraftNode) {
		return nil, denied(invocation, principal)
	}
	if s.deps.DraftAuthoring == nil {
		return nil, projectDraftAuthoringError(errDraftAuthoringUnavailable, invocation, principal)
	}
	if req == nil || strings.TrimSpace(req.GetDraftId()) == "" || req.GetExpectedRevision() == 0 || strings.TrimSpace(req.GetNodeId()) == "" || len(req.GetValues()) == 0 {
		return nil, invalid(invocation, "draft_node_parameters")
	}
	change, updateErr := s.deps.DraftAuthoring.UpdateNodeParameters(ctx, principal.Tenant(), principal.Subject(), designeredit.UpdateNodeParametersRequest{
		DraftID: req.GetDraftId(), ExpectedRevision: req.GetExpectedRevision(), NodeID: req.GetNodeId(), Values: req.GetValues(),
	})
	if updateErr != nil {
		return nil, projectDraftAuthoringError(updateErr, invocation, principal)
	}
	return &workflowv1.UpdateWorkflowDraftNodeResponse{Draft: s.projectDraftView(ctx, principal.Tenant(), change.Draft)}, nil
}

func (s *server) SetWorkflowDraftOutcome(ctx context.Context, req *workflowv1.SetWorkflowDraftOutcomeRequest) (*workflowv1.SetWorkflowDraftOutcomeResponse, error) {
	principal, invocation, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if !s.authorized(principal, ActionSetWorkflowDraftOutcome) {
		return nil, denied(invocation, principal)
	}
	if s.deps.DraftAuthoring == nil {
		return nil, projectDraftAuthoringError(errDraftAuthoringUnavailable, invocation, principal)
	}
	if req == nil || strings.TrimSpace(req.GetDraftId()) == "" || req.GetExpectedRevision() == 0 || strings.TrimSpace(req.GetFromNodeId()) == "" || strings.TrimSpace(req.GetRouteKey()) == "" || strings.TrimSpace(req.GetToNodeId()) == "" {
		return nil, invalid(invocation, "draft_outcome")
	}
	change, setErr := s.deps.DraftAuthoring.SetOutcome(ctx, principal.Tenant(), principal.Subject(), designeredit.SetOutcomeRequest{
		DraftID: req.GetDraftId(), ExpectedRevision: req.GetExpectedRevision(), FromNodeID: req.GetFromNodeId(), RouteKey: req.GetRouteKey(), ToNodeID: req.GetToNodeId(),
	})
	if setErr != nil {
		return nil, projectDraftAuthoringError(setErr, invocation, principal)
	}
	return &workflowv1.SetWorkflowDraftOutcomeResponse{Draft: s.projectDraftView(ctx, principal.Tenant(), change.Draft)}, nil
}

func (s *server) BindWorkflowDraftInput(ctx context.Context, req *workflowv1.BindWorkflowDraftInputRequest) (*workflowv1.BindWorkflowDraftInputResponse, error) {
	principal, invocation, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if !s.authorized(principal, ActionBindWorkflowDraftInput) {
		return nil, denied(invocation, principal)
	}
	if s.deps.DraftAuthoring == nil {
		return nil, projectDraftAuthoringError(errDraftAuthoringUnavailable, invocation, principal)
	}
	if req == nil || strings.TrimSpace(req.GetDraftId()) == "" || req.GetExpectedRevision() == 0 || strings.TrimSpace(req.GetTargetNodeId()) == "" || strings.TrimSpace(req.GetTargetPath()) == "" || strings.TrimSpace(req.GetSourceNodeId()) == "" || strings.TrimSpace(req.GetSourcePath()) == "" {
		return nil, invalid(invocation, "draft_input_binding")
	}
	change, bindErr := s.deps.DraftAuthoring.BindInput(ctx, principal.Tenant(), principal.Subject(), designeredit.BindInputRequest{
		DraftID: req.GetDraftId(), ExpectedRevision: req.GetExpectedRevision(), TargetNodeID: req.GetTargetNodeId(), TargetPath: req.GetTargetPath(), SourceNodeID: req.GetSourceNodeId(), SourcePath: req.GetSourcePath(),
	})
	if bindErr != nil {
		return nil, projectDraftAuthoringError(bindErr, invocation, principal)
	}
	return &workflowv1.BindWorkflowDraftInputResponse{Draft: s.projectDraftView(ctx, principal.Tenant(), change.Draft)}, nil
}

func (s *server) MoveWorkflowDraftNode(ctx context.Context, req *workflowv1.MoveWorkflowDraftNodeRequest) (*workflowv1.MoveWorkflowDraftNodeResponse, error) {
	principal, invocation, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if !s.authorized(principal, ActionMoveWorkflowDraftNode) {
		return nil, denied(invocation, principal)
	}
	if s.deps.DraftAuthoring == nil {
		return nil, projectDraftAuthoringError(errDraftAuthoringUnavailable, invocation, principal)
	}
	direction := designeredit.MoveDirection(strings.ToUpper(strings.TrimSpace(req.GetDirection())))
	if req == nil || strings.TrimSpace(req.GetDraftId()) == "" || req.GetExpectedRevision() == 0 || strings.TrimSpace(req.GetNodeId()) == "" || (direction != designeredit.MoveEarlier && direction != designeredit.MoveLater) {
		return nil, invalid(invocation, "draft_node_order")
	}
	change, moveErr := s.deps.DraftAuthoring.MoveNode(ctx, principal.Tenant(), principal.Subject(), designeredit.MoveNodeRequest{
		DraftID: req.GetDraftId(), ExpectedRevision: req.GetExpectedRevision(), NodeID: req.GetNodeId(), Direction: direction,
	})
	if moveErr != nil {
		return nil, projectDraftAuthoringError(moveErr, invocation, principal)
	}
	return &workflowv1.MoveWorkflowDraftNodeResponse{Draft: s.projectDraftView(ctx, principal.Tenant(), change.Draft)}, nil
}

func (s *server) NavigateWorkflowDraftHistory(ctx context.Context, req *workflowv1.NavigateWorkflowDraftHistoryRequest) (*workflowv1.NavigateWorkflowDraftHistoryResponse, error) {
	principal, invocation, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if !s.authorized(principal, ActionNavigateWorkflowDraftHistory) {
		return nil, denied(invocation, principal)
	}
	if s.deps.DraftAuthoring == nil {
		return nil, projectDraftAuthoringError(errDraftAuthoringUnavailable, invocation, principal)
	}
	direction := designeredit.HistoryDirection(strings.ToUpper(strings.TrimSpace(req.GetDirection())))
	if req == nil || strings.TrimSpace(req.GetDraftId()) == "" || req.GetExpectedRevision() == 0 ||
		(direction != designeredit.HistoryUndo && direction != designeredit.HistoryRedo) {
		return nil, invalid(invocation, "draft_history_direction")
	}
	change, navigateErr := s.deps.DraftAuthoring.NavigateHistory(ctx, principal.Tenant(), principal.Subject(), designeredit.NavigateRequest{
		DraftID: req.GetDraftId(), ExpectedRevision: req.GetExpectedRevision(), Direction: direction,
	})
	if navigateErr != nil {
		return nil, projectDraftAuthoringError(navigateErr, invocation, principal)
	}
	return &workflowv1.NavigateWorkflowDraftHistoryResponse{Draft: s.projectDraftView(ctx, principal.Tenant(), change.Draft)}, nil
}

func (s *server) ApplyWorkflowTemplateOverlay(ctx context.Context, req *workflowv1.ApplyWorkflowTemplateOverlayRequest) (*workflowv1.ApplyWorkflowTemplateOverlayResponse, error) {
	principal, invocation, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if !s.authorized(principal, ActionApplyWorkflowTemplateOverlay) {
		return nil, denied(invocation, principal)
	}
	if s.deps.DraftAuthoring == nil {
		return nil, projectDraftAuthoringError(errDraftAuthoringUnavailable, invocation, principal)
	}
	if req == nil || strings.TrimSpace(req.GetDraftId()) == "" || req.GetExpectedRevision() == 0 {
		return nil, invalid(invocation, "template_overlay")
	}
	change, overlayErr := s.deps.DraftAuthoring.ApplyTemplateOverlay(ctx, principal.Tenant(), principal.Subject(), designeredit.ApplyTemplateOverlayRequest{
		DraftID: req.GetDraftId(), ExpectedRevision: req.GetExpectedRevision(), Operation: designeredit.OverlayOperation(req.GetOperation()),
		TargetNodeID: req.GetTargetNodeId(), EntryID: req.GetEntryId(), EntryVersion: req.GetEntryVersion(), Reason: req.GetReason(),
	})
	if overlayErr != nil {
		return nil, projectDraftAuthoringError(overlayErr, invocation, principal)
	}
	return &workflowv1.ApplyWorkflowTemplateOverlayResponse{Draft: s.projectDraftView(ctx, principal.Tenant(), change.Draft), InsertedNodeIds: append([]string(nil), change.InsertedNodeIDs...), GroupId: change.GroupID}, nil
}

func (s *server) projectDraftView(ctx context.Context, tenant values.TenantId, view designeredit.View) *workflowv1.WorkflowDraftView {
	baseDefinitionDigest := ""
	if strings.TrimSpace(view.BaseVersionDigest) != "" && s != nil && s.deps.Definitions != nil {
		if base, found, err := s.deps.Definitions.GetByDigest(view.BaseVersionDigest); err == nil && found {
			baseDefinitionDigest = base.DefinitionDigest
		}
	}
	templateID, templateDefinitionDigest := "", ""
	var templateVersion uint32
	if s != nil && s.deps.Palette != nil {
		for _, entry := range s.deps.Palette.List(ctx, tenant) {
			if entry.Kind != designerpalette.KindTemplate || entry.Expansion.Template == nil || entry.Expansion.Template.WorkflowID != view.WorkflowID {
				continue
			}
			digest := workflowversion.DefinitionDigest(*entry.Expansion.Template)
			if digest == view.DefinitionDigest || templateID == "" || entry.Version > templateVersion {
				templateID, templateVersion, templateDefinitionDigest = entry.ID, entry.Version, digest
			}
			if digest == view.DefinitionDigest {
				break
			}
		}
	}
	result := &workflowv1.WorkflowDraftView{
		DraftId: view.DraftID, WorkflowId: view.WorkflowID, Name: view.Name, BaseVersionDigest: view.BaseVersionDigest,
		SemanticVersion: view.SemanticVersion, Revision: view.Revision, ExpiresAt: timestamp(view.ExpiresAt),
		StartNodeId: view.StartNodeID, DefinitionDigest: view.DefinitionDigest, BaseDefinitionDigest: baseDefinitionDigest,
		MatchesBaseDefinition:    baseDefinitionDigest != "" && baseDefinitionDigest == view.DefinitionDigest,
		TemplateDefinitionDigest: templateDefinitionDigest, MatchesTemplateDefinition: templateDefinitionDigest != "" && templateDefinitionDigest == view.DefinitionDigest,
		TemplateId: templateID, TemplateVersion: templateVersion,
		CanUndo: view.CanUndo, CanRedo: view.CanRedo, HistoryPosition: view.HistoryPosition, HistoryLength: view.HistoryLength,
		HistoryLabel: view.HistoryLabel, LayoutMode: view.LayoutMode,
		Nodes: make([]*workflowv1.WorkflowDraftNode, 0, len(view.Nodes)), Edges: make([]*workflowv1.WorkflowDraftEdge, 0, len(view.Edges)), Groups: make([]*workflowv1.WorkflowDraftGroup, 0, len(view.Groups)),
		Overlays:        make([]*workflowv1.WorkflowTemplateOverlay, 0, len(view.Overlays)),
		SemanticChanges: make([]*workflowv1.WorkflowDraftSemanticChange, 0, len(view.Changes)),
	}
	for _, node := range view.Nodes {
		projected := &workflowv1.WorkflowDraftNode{Id: node.ID, StepType: node.StepType, GroupId: node.GroupID, Label: node.Label, Locked: node.Locked, LockKind: node.LockKind, Parameters: make([]*workflowv1.WorkflowDraftParameter, 0, len(node.Parameters)), Outcomes: make([]*workflowv1.WorkflowDraftOutcome, 0, len(node.Outcomes)), Bindings: make([]*workflowv1.WorkflowDraftBinding, 0, len(node.Bindings))}
		for _, parameter := range node.Parameters {
			projected.Parameters = append(projected.Parameters, &workflowv1.WorkflowDraftParameter{Id: parameter.ID, Label: parameter.Label, Kind: string(parameter.Kind), Value: parameter.Value, Required: parameter.Required, Minimum: parameter.Minimum, Maximum: parameter.Maximum, Options: append([]string(nil), parameter.Options...)})
		}
		for _, outcome := range node.Outcomes {
			projected.Outcomes = append(projected.Outcomes, &workflowv1.WorkflowDraftOutcome{RouteKey: outcome.RouteKey, TargetNodeIds: append([]string(nil), outcome.TargetNodeIDs...)})
		}
		for _, binding := range node.Bindings {
			projectedBinding := &workflowv1.WorkflowDraftBinding{TargetPath: binding.TargetPath, TargetType: binding.TargetType, SourceKind: binding.SourceKind, SourceNodeId: binding.SourceNodeID, SourcePath: binding.SourcePath, Candidates: make([]*workflowv1.WorkflowDraftBindingCandidate, 0, len(binding.Candidates))}
			for _, candidate := range binding.Candidates {
				projectedBinding.Candidates = append(projectedBinding.Candidates, &workflowv1.WorkflowDraftBindingCandidate{SourceNodeId: candidate.NodeID, SourcePath: candidate.Path, ValueType: candidate.Type})
			}
			projected.Bindings = append(projected.Bindings, projectedBinding)
		}
		result.Nodes = append(result.Nodes, projected)
	}
	for _, edge := range view.Edges {
		result.Edges = append(result.Edges, &workflowv1.WorkflowDraftEdge{FromId: edge.From, ToId: edge.To, RouteKey: edge.RouteKey})
	}
	for _, group := range view.Groups {
		result.Groups = append(result.Groups, &workflowv1.WorkflowDraftGroup{Id: group.ID, Name: group.Name, EntryId: group.EntryID, EntryVersion: group.EntryVersion, Collapsed: group.Collapsed, NodeIds: append([]string(nil), group.NodeIDs...)})
	}
	for _, overlay := range view.Overlays {
		result.Overlays = append(result.Overlays, &workflowv1.WorkflowTemplateOverlay{Operation: string(overlay.Operation), TargetNodeId: overlay.TargetNodeID, EntryId: overlay.EntryID, EntryVersion: overlay.EntryVersion, Reason: overlay.Reason})
	}
	for _, change := range view.Changes {
		result.SemanticChanges = append(result.SemanticChanges, &workflowv1.WorkflowDraftSemanticChange{
			Kind: string(change.Kind), Operation: string(change.Operation), SubjectId: change.SubjectID,
			Field: change.Field, Before: change.Before, After: change.After,
		})
	}
	return result
}

func projectDraftAuthoringError(err error, invocation *transport.Invocation, principal *trust.Principal) *envelope.Error {
	code, reason, message := envelope.CodeUnavailable, "workflow.draft_authoring_unavailable", "workflow draft authoring is unavailable"
	switch {
	case errors.Is(err, designeredit.ErrInvalid):
		code, reason, message = envelope.CodeInvalidArgument, "workflow.draft_edit_invalid", "the workflow draft edit is invalid"
	case errors.Is(err, designeredit.ErrNotFound):
		code, reason, message = envelope.CodeNotFound, "workflow.draft_not_found", "the workflow draft or palette entry does not exist or is not visible"
	case errors.Is(err, designeredit.ErrConflict):
		code, reason, message = envelope.CodeAborted, "workflow.draft_revision_conflict", "the workflow draft changed; reload it before editing"
	}
	out := envelope.New(code, reason, message).WithDiagnostic(err)
	if invocation != nil {
		out.WithCorrelation(invocation.RequestID())
	}
	if principal != nil {
		out.WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	return out
}
