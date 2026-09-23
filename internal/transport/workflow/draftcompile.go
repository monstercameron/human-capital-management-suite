package workflow

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/draftcompile"
)

// Draft is the transport-safe portion of one stored workflow draft. The
// adapter intentionally omits tenant UUIDs and retention metadata.
type Draft struct {
	DraftID   string
	AuthorRef string
	Revision  uint64
	Document  json.RawMessage
}

// DraftReader loads one tenant-scoped durable draft. Implementations must
// return ErrNotFound for malformed, cross-tenant, or absent identifiers.
type DraftReader interface {
	ReadWorkflowDraft(context.Context, values.TenantId, string) (Draft, error)
}

// DraftCompiler is the production compiler projection used by the API.
type DraftCompiler interface {
	CompileDocument(context.Context, values.TenantId, json.RawMessage) draftcompile.Result
}

func (s *server) CompileWorkflowDraft(ctx context.Context, req *workflowv1.CompileWorkflowDraftRequest) (*workflowv1.CompileWorkflowDraftResponse, error) {
	principal, invocation, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	draftID := ""
	if req != nil {
		draftID = strings.TrimSpace(req.GetDraftId())
	}
	if draftID == "" {
		return nil, invalid(invocation, "draft_id")
	}
	if !s.authorized(ctx, principal, ActionCompileWorkflowDraft) {
		return nil, denied(invocation, principal)
	}
	if s.deps.Drafts == nil || s.deps.DraftCompiler == nil {
		return nil, projectReadError(errDraftCompilerUnavailable, invocation, principal)
	}
	draft, readErr := s.deps.Drafts.ReadWorkflowDraft(ctx, principal.Tenant(), draftID)
	if readErr != nil {
		return nil, projectReadError(readErr, invocation, principal)
	}
	// Draft ownership is checked after the tenant-scoped lookup and before
	// compilation. A mismatch is projected as not found, so the API does not
	// disclose another author's draft identity.
	if draft.DraftID != draftID || draft.AuthorRef != principal.Subject() {
		return nil, projectReadError(ErrNotFound, invocation, principal)
	}
	result := s.deps.DraftCompiler.CompileDocument(ctx, principal.Tenant(), append(json.RawMessage(nil), draft.Document...))
	return projectDraftCompile(draft, result), nil
}

func projectDraftCompile(draft Draft, result draftcompile.Result) *workflowv1.CompileWorkflowDraftResponse {
	response := &workflowv1.CompileWorkflowDraftResponse{
		DraftId: draft.DraftID, Revision: draft.Revision, Valid: result.Valid,
		CompiledPlanDigest: result.PlanDigest,
		Effects:            &workflowv1.WorkflowEffectSummary{ZeroEffect: result.Effects.ZeroEffect},
		Unwind:             &workflowv1.WorkflowUnwindSummary{Complete: result.Unwind.Complete},
	}
	for _, diagnostic := range result.Diagnostics {
		response.Diagnostics = append(response.Diagnostics, &workflowv1.WorkflowDraftDiagnostic{
			Code: diagnostic.Code, NodeId: diagnostic.NodeID, EdgeId: diagnostic.EdgeID,
			EdgeFrom: diagnostic.EdgeFrom, EdgeTo: diagnostic.EdgeTo, RouteKey: diagnostic.RouteKey,
			Field: diagnostic.Field, Ref: diagnostic.Ref, Detail: diagnostic.Detail,
		})
	}
	classes := make([]string, 0, len(result.Effects.NodesByClass))
	for effectClass := range result.Effects.NodesByClass {
		classes = append(classes, effectClass)
	}
	sort.Strings(classes)
	for _, effectClass := range classes {
		nodeIDs := append([]string(nil), result.Effects.NodesByClass[effectClass]...)
		sort.Strings(nodeIDs)
		response.Effects.NodesByClass = append(response.Effects.NodesByClass, &workflowv1.WorkflowEffectNodeGroup{
			EffectClass: effectClass, NodeIds: nodeIDs,
		})
	}
	response.Effects.EffectKeys = append([]string(nil), result.Effects.EffectKeys...)
	response.Effects.IrreversibleNodeIds = append([]string(nil), result.Effects.IrreversibleNodes...)
	for _, mode := range result.Effects.AllowedModes {
		response.Effects.AllowedModes = append(response.Effects.AllowedModes, string(mode))
	}
	for _, step := range result.Unwind.Steps {
		response.Unwind.Steps = append(response.Unwind.Steps, &workflowv1.WorkflowUnwindStep{
			NodeId: step.NodeID, EffectClass: step.EffectClass, Behavior: string(step.Behavior), CompensationRef: step.CompensationRef,
		})
	}
	return response
}
