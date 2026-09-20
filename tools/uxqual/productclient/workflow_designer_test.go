package productclient

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTodo_WF_UI_005_ProductClientLoadsDurableDraftProjection(t *testing.T) {
	service := Service{
		ListWorkflowPublications: func(context.Context, *workflowv1.ListWorkflowPublicationsRequest) (*workflowv1.ListWorkflowPublicationsResponse, error) {
			return &workflowv1.ListWorkflowPublicationsResponse{}, nil
		},
		ListWorkflowBlocks: func(context.Context, *workflowv1.ListWorkflowBlocksRequest) (*workflowv1.ListWorkflowBlocksResponse, error) {
			return &workflowv1.ListWorkflowBlocksResponse{}, nil
		},
		GetWorkflowDraft: func(_ context.Context, request *workflowv1.GetWorkflowDraftRequest) (*workflowv1.GetWorkflowDraftResponse, error) {
			if request.GetDraftId() != "draft-42" {
				t.Fatalf("draft request = %+v", request)
			}
			return &workflowv1.GetWorkflowDraftResponse{Draft: wfui005ClientDraft()}, nil
		},
	}
	session := Session{Tenant: "tenant-a", Principal: "author-a"}
	baseline := LoadingView(session, State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome}})
	state := State{Page: productui.PageWorkflowDesigner, Request: productui.PageRequest{Page: productui.PageWorkflowDesigner, WorkflowDraftID: "draft-42"}}
	view, err := LoadWithBaseline(context.Background(), service, session, state, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if view.WorkflowDraft == nil || view.SelectedWorkflowDraftID != "draft-42" || view.WorkflowDraft.Revision != 4 || len(view.WorkflowDraft.Groups) != 1 || !view.WorkflowDraft.Groups[0].Collapsed {
		t.Fatalf("draft projection = %+v", view.WorkflowDraft)
	}
}

func TestTodo_WF_UI_005_ProductClientRejectsMalformedDraftProjection(t *testing.T) {
	draft := wfui005ClientDraft()
	draft.Groups[0].NodeIds = append(draft.Groups[0].NodeIds, "missing")
	if _, err := projectWorkflowDraft(draft); err == nil || !strings.Contains(err.Error(), "unknown node") {
		t.Fatalf("malformed group error = %v", err)
	}
	draft = wfui005ClientDraft()
	draft.SemanticVersion = "v1"
	if _, err := projectWorkflowDraft(draft); err == nil {
		t.Fatal("draft projection accepted an invalid semantic version")
	}
	draft = wfui005ClientDraft()
	draft.MatchesBaseDefinition = true
	draft.BaseDefinitionDigest = "sha256:other"
	if _, err := projectWorkflowDraft(draft); err == nil {
		t.Fatal("draft projection accepted a false exact-match claim")
	}
	draft = wfui005ClientDraft()
	draft.MatchesTemplateDefinition = true
	draft.TemplateDefinitionDigest = "sha256:other"
	if _, err := projectWorkflowDraft(draft); err == nil {
		t.Fatal("draft projection accepted a false executable-template match claim")
	}
}

func TestWorkflowDesignerRouteDoesNotRetainAStaleDraft(t *testing.T) {
	service := Service{
		ListWorkflowPublications: func(context.Context, *workflowv1.ListWorkflowPublicationsRequest) (*workflowv1.ListWorkflowPublicationsResponse, error) {
			return &workflowv1.ListWorkflowPublicationsResponse{Publications: []*workflowv1.WorkflowPublicationSummary{{
				WorkflowId: "workflow.promotion", Name: "Promotion", DefinitionVersion: 3, SemanticVersion: "3.1.0", Status: "ACTIVE",
			}}}, nil
		},
		GetWorkflowDefinitionView: func(context.Context, *workflowv1.GetWorkflowDefinitionViewRequest) (*workflowv1.GetWorkflowDefinitionViewResponse, error) {
			return &workflowv1.GetWorkflowDefinitionViewResponse{View: wfui002ClientView()}, nil
		},
		ListWorkflowBlocks: func(context.Context, *workflowv1.ListWorkflowBlocksRequest) (*workflowv1.ListWorkflowBlocksResponse, error) {
			return &workflowv1.ListWorkflowBlocksResponse{}, nil
		},
	}
	session := Session{Tenant: "tenant-a", Principal: "author-a"}
	baseline := LoadingView(session, State{Page: productui.PageWorkflowDesigner, Request: productui.PageRequest{Page: productui.PageWorkflowDesigner, WorkflowDraftID: "draft-42"}})
	draft := productui.WorkflowDraftView{DraftID: "draft-42", WorkflowID: "workflow.promotion", SemanticVersion: "3.1.1", Revision: 1}
	baseline.WorkflowDraft, baseline.SelectedWorkflowDraftID = &draft, draft.DraftID

	state := State{Page: productui.PageWorkflowDesigner, Request: productui.PageRequest{Page: productui.PageWorkflowDesigner, WorkflowID: "workflow.promotion"}}
	view, err := LoadWithBaseline(context.Background(), service, session, state, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if view.WorkflowDraft != nil || view.SelectedWorkflowDraftID != "" || view.WorkflowView == nil {
		t.Fatalf("published route retained stale draft state: draft=%+v selected=%q view=%+v", view.WorkflowDraft, view.SelectedWorkflowDraftID, view.WorkflowView)
	}
}

func TestTodo_WF_UI_002_ProductClientLoadsCatalogAndSelectedGraph(t *testing.T) {
	var catalogCalls, viewCalls atomic.Int64
	service := Service{
		ListWorkflowPublications: func(context.Context, *workflowv1.ListWorkflowPublicationsRequest) (*workflowv1.ListWorkflowPublicationsResponse, error) {
			catalogCalls.Add(1)
			return &workflowv1.ListWorkflowPublicationsResponse{Publications: []*workflowv1.WorkflowPublicationSummary{{
				WorkflowId: "workflow.promotion", Name: "Promotion", DefinitionVersion: 3, SemanticVersion: "3.1.0", Status: "ACTIVE",
			}}}, nil
		},
		GetWorkflowDefinitionView: func(_ context.Context, request *workflowv1.GetWorkflowDefinitionViewRequest) (*workflowv1.GetWorkflowDefinitionViewResponse, error) {
			viewCalls.Add(1)
			if request.GetWorkflowId() != "workflow.promotion" || request.GetInstanceId() != "" {
				t.Fatalf("view request = %+v", request)
			}
			return &workflowv1.GetWorkflowDefinitionViewResponse{View: wfui002ClientView()}, nil
		},
		ListWorkflowBlocks: func(context.Context, *workflowv1.ListWorkflowBlocksRequest) (*workflowv1.ListWorkflowBlocksResponse, error) {
			return &workflowv1.ListWorkflowBlocksResponse{Entries: []*workflowv1.WorkflowPaletteEntry{{Id: "kernel.approval", Version: 1, Name: "Approval", Kind: "BLOCK", Domain: "Control flow", EffectClass: "PURE", Reversal: "NO_EFFECT", Status: "ACTIVE", StepType: "APPROVAL"}}}, nil
		},
	}
	session := Session{Tenant: "tenant-a", Principal: "author-a"}
	baseline := LoadingView(session, State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome}})
	state := State{Page: productui.PageWorkflowDesigner, Request: productui.PageRequest{Page: productui.PageWorkflowDesigner}}

	view, err := LoadWithBaseline(context.Background(), service, session, state, baseline)
	if err != nil {
		t.Fatalf("LoadWithBaseline: %v", err)
	}
	if catalogCalls.Load() != 1 || viewCalls.Load() != 1 {
		t.Fatalf("RPC calls = catalog %d, view %d", catalogCalls.Load(), viewCalls.Load())
	}
	if len(view.PublishedWorkflows) != 1 || view.PublishedWorkflows[0].WorkflowID != "workflow.promotion" {
		t.Fatalf("catalog projection = %+v", view.PublishedWorkflows)
	}
	if view.WorkflowView == nil || view.SelectedWorkflowID != "workflow.promotion" || len(view.WorkflowView.Nodes) != 2 || len(view.WorkflowView.Edges) != 1 {
		t.Fatalf("selected projection = %+v", view.WorkflowView)
	}
	if len(view.WorkflowPalette) != 1 || view.WorkflowPalette[0].ID != "kernel.approval" {
		t.Fatalf("palette projection = %+v", view.WorkflowPalette)
	}
}

func TestTodo_WF_UI_002_ProductClientRejectsMalformedGraph(t *testing.T) {
	malformed := wfui002ClientView()
	malformed.Nodes = append(malformed.Nodes, malformed.Nodes[0])
	if _, err := projectWorkflowView(malformed); err == nil || !strings.Contains(err.Error(), "duplicate node") {
		t.Fatalf("duplicate-node error = %v", err)
	}

	malformed = wfui002ClientView()
	malformed.Edges[0].ToId = "missing"
	if _, err := projectWorkflowView(malformed); err == nil || !strings.Contains(err.Error(), "invalid edge") {
		t.Fatalf("invalid-edge error = %v", err)
	}

	malformed = wfui002ClientView()
	malformed.SemanticVersion = "3.1"
	if _, err := projectWorkflowView(malformed); err == nil {
		t.Fatal("workflow projection accepted an invalid semantic version")
	}
}

func TestTodo_WF_UI_002_UnrelatedRouteDoesNotReadWorkflowCatalog(t *testing.T) {
	service := Service{ListWorkflowPublications: func(context.Context, *workflowv1.ListWorkflowPublicationsRequest) (*workflowv1.ListWorkflowPublicationsResponse, error) {
		t.Fatal("unrelated route read workflow catalog")
		return nil, nil
	}, ListWorkflowBlocks: func(context.Context, *workflowv1.ListWorkflowBlocksRequest) (*workflowv1.ListWorkflowBlocksResponse, error) {
		t.Fatal("unrelated route read workflow palette")
		return nil, nil
	}}
	session := Session{Tenant: "tenant-a", Principal: "author-a"}
	baseline := LoadingView(session, State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome}})
	if _, err := LoadWithBaseline(context.Background(), service, session, State{Page: productui.PageSettings, Request: productui.PageRequest{Page: productui.PageSettings}}, baseline); err != nil {
		t.Fatalf("LoadWithBaseline settings: %v", err)
	}
}

func TestTodo_WF_UI_005_ProductClientProjectsOnlyValidServerEntries(t *testing.T) {
	values := []*workflowv1.WorkflowPaletteEntry{
		{Id: "kernel.approval", Version: 1, Name: "Approval", Kind: "block", Domain: "Control flow", EffectClass: "PURE", Reversal: "NO_EFFECT", Status: "ACTIVE", StepType: "APPROVAL"},
		{Id: "fragment.review", Version: 2, Name: "Review", Kind: "FRAGMENT", Domain: "People", EffectClass: "PURE", Reversal: "NO_EFFECT", Status: "ACTIVE"},
		{Id: "fragment.review", Version: 2, Name: "Duplicate", Kind: "FRAGMENT", Domain: "People", EffectClass: "PURE", Reversal: "NO_EFFECT"},
		{Id: "template.hidden", Version: 1, Name: "Unknown kind", Kind: "EXECUTABLE", Domain: "People", EffectClass: "PURE", Reversal: "NO_EFFECT"},
		{Id: "template.malformed", Version: 0, Name: "Malformed", Kind: "TEMPLATE", Domain: "People", EffectClass: "PURE", Reversal: "NO_EFFECT"},
		nil,
	}
	projected := projectWorkflowPalette(values)
	if len(projected) != 2 {
		t.Fatalf("projected palette = %+v", projected)
	}
	if projected[0].ID != "kernel.approval" || projected[0].Kind != "BLOCK" || projected[1].ID != "fragment.review" {
		t.Fatalf("projected palette identities = %+v", projected)
	}
	values[0].Name = "mutated after projection"
	if projected[0].Name != "Approval" {
		t.Fatalf("projection aliases response values: %+v", projected[0])
	}
}

func TestTodo_WF_UI_005_ProductClientDoesNotInventDeniedEntries(t *testing.T) {
	projected := projectWorkflowPalette([]*workflowv1.WorkflowPaletteEntry{{
		Id: "kernel.approval", Version: 1, Name: "Approval", Kind: "BLOCK", Domain: "Control flow", EffectClass: "PURE", Reversal: "NO_EFFECT",
	}})
	if len(projected) != 1 || projected[0].ID != "kernel.approval" {
		t.Fatalf("projection changed server-filtered catalog: %+v", projected)
	}
	for _, entry := range projected {
		if entry.ID == "hcmnext.rewards.simulate_compensation" {
			t.Fatal("client invented a capability absent from the server response")
		}
	}
}

func TestTodo_WF_UI_006_ProductClientProjectsTypedParametersLocksAndOverlays(t *testing.T) {
	draft := wfui005ClientDraft()
	draft.Nodes[0].Label = "Manager approval"
	draft.Nodes[0].Locked = true
	draft.Nodes[0].LockKind = "GOVERNANCE"
	draft.Nodes[0].Parameters = []*workflowv1.WorkflowDraftParameter{{Id: "retry_max_attempts", Label: "Maximum attempts", Kind: "INTEGER", Value: "2", Required: true, Minimum: 1, Maximum: 20}}
	draft.Overlays = []*workflowv1.WorkflowTemplateOverlay{{Operation: "REPLACE", TargetNodeId: "review__manager", EntryId: "kernel.task", EntryVersion: 1, Reason: "customer review"}}
	projected, err := projectWorkflowDraft(draft)
	if err != nil {
		t.Fatal(err)
	}
	if !projected.Nodes[0].Locked || projected.Nodes[0].LockKind != "GOVERNANCE" || len(projected.Nodes[0].Parameters) != 1 || projected.Nodes[0].Parameters[0].Maximum != 20 {
		t.Fatalf("projected node inspector = %+v", projected.Nodes[0])
	}
	if len(projected.Overlays) != 1 || projected.Overlays[0].Reason != "customer review" {
		t.Fatalf("projected overlays = %+v", projected.Overlays)
	}
}

func TestTodo_WF_UI_006_ProductClientRejectsMalformedInspectorClaims(t *testing.T) {
	draft := wfui005ClientDraft()
	draft.Nodes[0].Parameters = []*workflowv1.WorkflowDraftParameter{{Id: "field", Label: "Field", Kind: "SCRIPT"}}
	if _, err := projectWorkflowDraft(draft); err == nil {
		t.Fatal("malformed parameter kind was accepted")
	}
	draft = wfui005ClientDraft()
	draft.Overlays = []*workflowv1.WorkflowTemplateOverlay{{Operation: "DELETE"}}
	if _, err := projectWorkflowDraft(draft); err == nil {
		t.Fatal("malformed overlay operation was accepted")
	}
}

func TestTodo_WF_UI_007_ProductClientProjectsOutcomeAndCompilerBindingClaims(t *testing.T) {
	draft := wfui005ClientDraft()
	draft.Nodes = append(draft.Nodes, &workflowv1.WorkflowDraftNode{Id: "commit", StepType: "ACTION"})
	draft.Nodes[0].Outcomes = []*workflowv1.WorkflowDraftOutcome{{RouteKey: "SUCCEEDED", TargetNodeIds: []string{draft.Nodes[1].GetId()}}}
	draft.Nodes[0].Bindings = []*workflowv1.WorkflowDraftBinding{{
		TargetPath: "worker_id", TargetType: "WorkerID", SourceKind: "NODE_OUTPUT", SourceNodeId: draft.Nodes[1].GetId(), SourcePath: "worker_id",
		Candidates: []*workflowv1.WorkflowDraftBindingCandidate{{SourceNodeId: draft.Nodes[1].GetId(), SourcePath: "worker_id", ValueType: "WorkerID"}},
	}}
	projected, err := projectWorkflowDraft(draft)
	if err != nil {
		t.Fatal(err)
	}
	if got := projected.Nodes[0]; len(got.Outcomes) != 1 || got.Outcomes[0].TargetNodeIDs[0] != draft.Nodes[1].GetId() || len(got.Bindings) != 1 || got.Bindings[0].Candidates[0].SourcePath != "worker_id" {
		t.Fatalf("projected link claims = %+v", got)
	}
}

func TestTodo_WF_UI_007_ProductClientRejectsMalformedLinkClaims(t *testing.T) {
	draft := wfui005ClientDraft()
	draft.Nodes[0].Outcomes = []*workflowv1.WorkflowDraftOutcome{{RouteKey: "SUCCEEDED", TargetNodeIds: []string{"missing"}}}
	if _, err := projectWorkflowDraft(draft); err == nil || !strings.Contains(err.Error(), "outcome references") {
		t.Fatalf("unknown outcome target error = %v", err)
	}
	draft = wfui005ClientDraft()
	draft.Nodes[0].Bindings = []*workflowv1.WorkflowDraftBinding{{TargetPath: "worker_id", TargetType: "WorkerID", Candidates: []*workflowv1.WorkflowDraftBindingCandidate{{SourceNodeId: "missing", SourcePath: "worker_id", ValueType: "WorkerID"}}}}
	if _, err := projectWorkflowDraft(draft); err == nil || !strings.Contains(err.Error(), "binding candidate") {
		t.Fatalf("unknown binding candidate error = %v", err)
	}
}

func wfui002ClientView() *workflowv1.WorkflowDefinitionView {
	return &workflowv1.WorkflowDefinitionView{
		WorkflowId: "workflow.promotion", Name: "Promotion", DefinitionVersion: 3, SemanticVersion: "3.1.0",
		CompiledPlanDigest: "sha256:plan", PublicationStatus: "ACTIVE", Complete: true, MaxDepth: 1,
		Nodes: []*workflowv1.WorkflowViewNode{
			{Id: "start", Label: "Start", StepType: "TASK", Start: true, State: "not-started", Routes: []*workflowv1.WorkflowViewRoute{{Key: "DONE", TargetId: "end"}}},
			{Id: "end", Label: "End", StepType: "END", Depth: 1, Terminal: true, State: "not-started"},
		},
		Edges: []*workflowv1.WorkflowViewEdge{{Id: "edge-1", FromId: "start", ToId: "end", RouteKey: "DONE"}},
	}
}

func wfui005ClientDraft() *workflowv1.WorkflowDraftView {
	return &workflowv1.WorkflowDraftView{
		DraftId: "draft-42", WorkflowId: "customer.workflow.42", Name: "Employee change", SemanticVersion: "0.1.0", Revision: 4,
		StartNodeId: "review__manager", DefinitionDigest: "sha256:draft", BaseDefinitionDigest: "sha256:draft", MatchesBaseDefinition: true,
		TemplateDefinitionDigest: "sha256:draft", MatchesTemplateDefinition: true, TemplateId: "template.employee-change", TemplateVersion: 1,
		ExpiresAt: timestamppb.New(time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)),
		Nodes:     []*workflowv1.WorkflowDraftNode{{Id: "review__manager", StepType: "APPROVAL", GroupId: "review"}},
		Groups:    []*workflowv1.WorkflowDraftGroup{{Id: "review", Name: "Review", EntryId: "fragment.review", EntryVersion: 1, Collapsed: true, NodeIds: []string{"review__manager"}}},
	}
}
