// Package workflow exposes the workflow inspection surface and the governed
// operator controls (EP-WF-002).
package workflow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workflowview"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

const (
	GetWorkflowProcedure        = "/hcmnext.workflow.v1.WorkflowService/GetWorkflow"
	ListNodeExecutionsProcedure = "/hcmnext.workflow.v1.WorkflowService/ListNodeExecutions"
	ActionGetWorkflow           = "get_workflow"
	ActionListNodeExecutions    = "list_node_executions"
	// ActionInspectAnySubject is the operator admission for instance reads:
	// the durable operator or administrator grant to read an instance the
	// caller neither participates in nor supervises. It is checked after
	// the record loads so a refusal can answer NOT_FOUND without disclosing
	// that the instance exists.
	ActionInspectAnySubject               = "inspect_any_subject"
	ListWorkflowPublicationsProcedure     = "/hcmnext.workflow.v1.WorkflowService/ListWorkflowPublications"
	GetWorkflowDefinitionViewProcedure    = "/hcmnext.workflow.v1.WorkflowService/GetWorkflowDefinitionView"
	CompileWorkflowDraftProcedure         = "/hcmnext.workflow.v1.WorkflowService/CompileWorkflowDraft"
	ListWorkflowBlocksProcedure           = "/hcmnext.workflow.v1.WorkflowService/ListWorkflowBlocks"
	CreateWorkflowDraftProcedure          = "/hcmnext.workflow.v1.WorkflowService/CreateWorkflowDraft"
	GetWorkflowDraftProcedure             = "/hcmnext.workflow.v1.WorkflowService/GetWorkflowDraft"
	InsertWorkflowPaletteEntryProcedure   = "/hcmnext.workflow.v1.WorkflowService/InsertWorkflowPaletteEntry"
	UpdateWorkflowDraftNodeProcedure      = "/hcmnext.workflow.v1.WorkflowService/UpdateWorkflowDraftNode"
	SetWorkflowDraftOutcomeProcedure      = "/hcmnext.workflow.v1.WorkflowService/SetWorkflowDraftOutcome"
	BindWorkflowDraftInputProcedure       = "/hcmnext.workflow.v1.WorkflowService/BindWorkflowDraftInput"
	MoveWorkflowDraftNodeProcedure        = "/hcmnext.workflow.v1.WorkflowService/MoveWorkflowDraftNode"
	RemoveWorkflowDraftNodeProcedure      = "/hcmnext.workflow.v1.WorkflowService/RemoveWorkflowDraftNode"
	ClearWorkflowDraftOutcomeProcedure    = "/hcmnext.workflow.v1.WorkflowService/ClearWorkflowDraftOutcome"
	RenameWorkflowDraftProcedure          = "/hcmnext.workflow.v1.WorkflowService/RenameWorkflowDraft"
	NavigateWorkflowDraftHistoryProcedure = "/hcmnext.workflow.v1.WorkflowService/NavigateWorkflowDraftHistory"
	ApplyWorkflowTemplateOverlayProcedure = "/hcmnext.workflow.v1.WorkflowService/ApplyWorkflowTemplateOverlay"
	ActionListWorkflowPublications        = "list_workflow_publications"
	ActionGetWorkflowDefinitionView       = "get_workflow_definition_view"
	ActionCompileWorkflowDraft            = "compile_workflow_draft"
	ActionListWorkflowBlocks              = "list_workflow_blocks"
	ActionCreateWorkflowDraft             = "create_workflow_draft"
	ActionGetWorkflowDraft                = "get_workflow_draft"
	ActionInsertWorkflowPaletteEntry      = "insert_workflow_palette_entry"
	ActionUpdateWorkflowDraftNode         = "update_workflow_draft_node"
	ActionSetWorkflowDraftOutcome         = "set_workflow_draft_outcome"
	ActionBindWorkflowDraftInput          = "bind_workflow_draft_input"
	ActionMoveWorkflowDraftNode           = "move_workflow_draft_node"
	ActionRemoveWorkflowDraftNode         = "remove_workflow_draft_node"
	ActionClearWorkflowDraftOutcome       = "clear_workflow_draft_outcome"
	ActionRenameWorkflowDraft             = "rename_workflow_draft"
	ActionNavigateWorkflowDraftHistory    = "navigate_workflow_draft_history"
	ActionApplyWorkflowTemplateOverlay    = "apply_workflow_template_overlay"
	defaultPageSize                       = 100
	maxPageSize                           = 1000
)

var (
	ErrNotFound                  = errors.New("workflow: instance not found")
	ErrInvalidRecord             = errors.New("workflow: inspection record is invalid")
	ErrInvalidCursor             = errors.New("workflow: inspection cursor is invalid")
	ErrCursorKeyUnset            = errors.New("workflow: inspection cursor key is unset")
	errDraftCompilerUnavailable  = errors.New("workflow: draft compiler is not configured")
	errPaletteUnavailable        = errors.New("workflow: workflow palette is not configured")
	errDraftAuthoringUnavailable = errors.New("workflow: draft authoring is not configured")
)

// Instance is the redacted, transport-safe instance projection supplied by a
// reader. It contains no payload bytes or provider response content.
type Instance struct {
	InstanceID            string
	TenantID              string
	CellID                string
	WorkflowID            string
	WorkflowVersion       uint32
	CompiledPlanDigest    string
	BusinessSubjectRefs   []string
	BusinessTransactionID string
	ExecutionMode         string
	RuntimeStatus         string
	RequestState          string
	ExecutionState        string
	BusinessState         string
	ConsistencyState      string
	ObligationState       string
	InputRef              string
	VariableRevisionHead  string
	CurrentNodeIDs        []string
	EffectiveContextRef   string
	LastCheckpointRef     string
	InstanceVersion       uint64
	CorrelationID         string
	CreatedAt             time.Time
	StartedAt             *time.Time
	CompletedAt           *time.Time
}

type NodeExecution struct {
	NodeExecutionID         string
	WorkflowInstanceID      string
	NodeID                  string
	Attempt                 uint32
	Status                  string
	InputSnapshotRef        string
	OutputArtifactRef       string
	CapabilityExecutionID   string
	AuthorizationDecisionID string
	DecisionID              string
	HumanTaskID             string
	ErrorClass              string
	RetryAt                 *time.Time
	ExecutionLeaseID        string
	StartedAt               *time.Time
	CompletedAt             *time.Time
	TraceID                 string
}

type Record struct {
	Instance Instance
	Nodes    []NodeExecution
	// Inspector is the authorization-shaped runtime projection built from the
	// same durable record. Definition-view endpoints consume it directly so
	// the product never invents node state from transport rows.
	Inspector *inspect.View
}

type Reader interface {
	ReadWorkflowInstance(context.Context, string, string) (Record, error)
}

// DefinitionReader is the immutable publication port used by the read-only
// workflow designer. The runtime keeps depending on workflowversion.Store;
// only surfaces that actually render a catalog require this wider read port.
type DefinitionReader interface {
	ListAll() ([]workflowversion.CompiledVersion, error)
	GetByDigest(string) (workflowversion.CompiledVersion, bool, error)
	GetActiveForWorkflow(string) (workflowversion.CompiledVersion, bool, error)
	List(string) ([]workflowversion.CompiledVersion, error)
}

type Dependencies struct {
	Instances      Reader
	Definitions    DefinitionReader
	Drafts         DraftReader
	DraftCompiler  DraftCompiler
	Palette        Palette
	DraftAuthoring *designeredit.Service
	// Authorize is the wire-level capability gate (RBAC-RT-004): it answers
	// whether the principal may call the named action at all, resolved from
	// the principal's durable role assignments, never from credential
	// claims. Nil denies every call: an unwired service is closed, not
	// open. Instance reads (GetWorkflow, ListNodeExecutions) additionally
	// require participation, supervision or an operator role via
	// [Dependencies.Supervision] and the durable operator grant, enforced in
	// the handlers after the record is loaded.
	Authorize func(context.Context, *trust.Principal, string) bool
	// Supervision reports whether supervisor stands in the management chain
	// of subject. It backs the supervision admission for instance reads
	// until the effective-dated relationship directory (RBAC-RT-007) lands;
	// nil skips the supervision admission without weakening the participant
	// or operator admissions.
	Supervision func(ctx context.Context, supervisor, subject string) (bool, error)
	CursorKey   []byte
	// PreviousCursorKey is the retired inspection-cursor signing key,
	// accepted for verification only while in-flight cursors minted under
	// it drain. New cursors are always minted under CursorKey.
	PreviousCursorKey []byte
	// Control runs governed Pause/Resume/Cancel/RetryNode controls. Nil (or a
	// nil TenantIDs) refuses every control with FAILED_PRECONDITION and
	// performs no transition.
	Control   ControlHandler
	TenantIDs workflowcontrol.TenantIDs
}

type server struct {
	workflowv1.UnimplementedWorkflowServiceServer
	deps Dependencies
}

func Version() int { return 1 }

func Explain(v *workflowv1.WorkflowInstance) string {
	if v == nil {
		return fmt.Sprintf("workflow inspection v%d empty", Version())
	}
	return fmt.Sprintf("workflow inspection v%d instance=%s status=%s version=%d", Version(), v.GetInstanceId(), v.GetRuntimeStatus(), v.GetInstanceVersion())
}

func Register(srv *grpc.Server, deps Dependencies) {
	workflowv1.RegisterWorkflowServiceServer(srv, &server{deps: deps})
}

func NewHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{deps: deps}
	mux := http.NewServeMux()
	mux.Handle(ListWorkflowPublicationsProcedure, connect.NewUnaryHandler(ListWorkflowPublicationsProcedure, func(ctx context.Context, req *connect.Request[workflowv1.ListWorkflowPublicationsRequest]) (*connect.Response[workflowv1.ListWorkflowPublicationsResponse], error) {
		res, err := s.ListWorkflowPublications(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(GetWorkflowDefinitionViewProcedure, connect.NewUnaryHandler(GetWorkflowDefinitionViewProcedure, func(ctx context.Context, req *connect.Request[workflowv1.GetWorkflowDefinitionViewRequest]) (*connect.Response[workflowv1.GetWorkflowDefinitionViewResponse], error) {
		res, err := s.GetWorkflowDefinitionView(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(CompileWorkflowDraftProcedure, connect.NewUnaryHandler(CompileWorkflowDraftProcedure, func(ctx context.Context, req *connect.Request[workflowv1.CompileWorkflowDraftRequest]) (*connect.Response[workflowv1.CompileWorkflowDraftResponse], error) {
		res, err := s.CompileWorkflowDraft(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(ListWorkflowBlocksProcedure, connect.NewUnaryHandler(ListWorkflowBlocksProcedure, func(ctx context.Context, req *connect.Request[workflowv1.ListWorkflowBlocksRequest]) (*connect.Response[workflowv1.ListWorkflowBlocksResponse], error) {
		res, err := s.ListWorkflowBlocks(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(CreateWorkflowDraftProcedure, connect.NewUnaryHandler(CreateWorkflowDraftProcedure, func(ctx context.Context, req *connect.Request[workflowv1.CreateWorkflowDraftRequest]) (*connect.Response[workflowv1.CreateWorkflowDraftResponse], error) {
		res, err := s.CreateWorkflowDraft(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(GetWorkflowDraftProcedure, connect.NewUnaryHandler(GetWorkflowDraftProcedure, func(ctx context.Context, req *connect.Request[workflowv1.GetWorkflowDraftRequest]) (*connect.Response[workflowv1.GetWorkflowDraftResponse], error) {
		res, err := s.GetWorkflowDraft(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(InsertWorkflowPaletteEntryProcedure, connect.NewUnaryHandler(InsertWorkflowPaletteEntryProcedure, func(ctx context.Context, req *connect.Request[workflowv1.InsertWorkflowPaletteEntryRequest]) (*connect.Response[workflowv1.InsertWorkflowPaletteEntryResponse], error) {
		res, err := s.InsertWorkflowPaletteEntry(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(UpdateWorkflowDraftNodeProcedure, connect.NewUnaryHandler(UpdateWorkflowDraftNodeProcedure, func(ctx context.Context, req *connect.Request[workflowv1.UpdateWorkflowDraftNodeRequest]) (*connect.Response[workflowv1.UpdateWorkflowDraftNodeResponse], error) {
		res, err := s.UpdateWorkflowDraftNode(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(SetWorkflowDraftOutcomeProcedure, connect.NewUnaryHandler(SetWorkflowDraftOutcomeProcedure, func(ctx context.Context, req *connect.Request[workflowv1.SetWorkflowDraftOutcomeRequest]) (*connect.Response[workflowv1.SetWorkflowDraftOutcomeResponse], error) {
		res, err := s.SetWorkflowDraftOutcome(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(BindWorkflowDraftInputProcedure, connect.NewUnaryHandler(BindWorkflowDraftInputProcedure, func(ctx context.Context, req *connect.Request[workflowv1.BindWorkflowDraftInputRequest]) (*connect.Response[workflowv1.BindWorkflowDraftInputResponse], error) {
		res, err := s.BindWorkflowDraftInput(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(MoveWorkflowDraftNodeProcedure, connect.NewUnaryHandler(MoveWorkflowDraftNodeProcedure, func(ctx context.Context, req *connect.Request[workflowv1.MoveWorkflowDraftNodeRequest]) (*connect.Response[workflowv1.MoveWorkflowDraftNodeResponse], error) {
		res, err := s.MoveWorkflowDraftNode(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(RemoveWorkflowDraftNodeProcedure, connect.NewUnaryHandler(RemoveWorkflowDraftNodeProcedure, func(ctx context.Context, req *connect.Request[workflowv1.RemoveWorkflowDraftNodeRequest]) (*connect.Response[workflowv1.RemoveWorkflowDraftNodeResponse], error) {
		res, err := s.RemoveWorkflowDraftNode(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(ClearWorkflowDraftOutcomeProcedure, connect.NewUnaryHandler(ClearWorkflowDraftOutcomeProcedure, func(ctx context.Context, req *connect.Request[workflowv1.ClearWorkflowDraftOutcomeRequest]) (*connect.Response[workflowv1.ClearWorkflowDraftOutcomeResponse], error) {
		res, err := s.ClearWorkflowDraftOutcome(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(RenameWorkflowDraftProcedure, connect.NewUnaryHandler(RenameWorkflowDraftProcedure, func(ctx context.Context, req *connect.Request[workflowv1.RenameWorkflowDraftRequest]) (*connect.Response[workflowv1.RenameWorkflowDraftResponse], error) {
		res, err := s.RenameWorkflowDraft(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(NavigateWorkflowDraftHistoryProcedure, connect.NewUnaryHandler(NavigateWorkflowDraftHistoryProcedure, func(ctx context.Context, req *connect.Request[workflowv1.NavigateWorkflowDraftHistoryRequest]) (*connect.Response[workflowv1.NavigateWorkflowDraftHistoryResponse], error) {
		res, err := s.NavigateWorkflowDraftHistory(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(ApplyWorkflowTemplateOverlayProcedure, connect.NewUnaryHandler(ApplyWorkflowTemplateOverlayProcedure, func(ctx context.Context, req *connect.Request[workflowv1.ApplyWorkflowTemplateOverlayRequest]) (*connect.Response[workflowv1.ApplyWorkflowTemplateOverlayResponse], error) {
		res, err := s.ApplyWorkflowTemplateOverlay(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(GetWorkflowProcedure, connect.NewUnaryHandler(GetWorkflowProcedure, func(ctx context.Context, req *connect.Request[workflowv1.GetWorkflowRequest]) (*connect.Response[workflowv1.GetWorkflowResponse], error) {
		res, err := s.GetWorkflow(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(ListNodeExecutionsProcedure, connect.NewUnaryHandler(ListNodeExecutionsProcedure, func(ctx context.Context, req *connect.Request[workflowv1.ListNodeExecutionsRequest]) (*connect.Response[workflowv1.ListNodeExecutionsResponse], error) {
		res, err := s.ListNodeExecutions(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	registerControlHandlers(mux, s, opts...)
	return mux
}

func (s *server) ListWorkflowPublications(ctx context.Context, _ *workflowv1.ListWorkflowPublicationsRequest) (*workflowv1.ListWorkflowPublicationsResponse, error) {
	p, inv, ownedErr := trustedContext(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	if !s.authorized(ctx, p, ActionListWorkflowPublications) {
		return nil, denied(inv, p)
	}
	if s.deps.Definitions == nil {
		return nil, projectReadError(errors.New("workflow publication catalog is not configured"), inv, p)
	}
	versions, err := s.deps.Definitions.ListAll()
	if err != nil {
		return nil, projectReadError(err, inv, p)
	}
	selected := selectCatalogVersions(versions)
	response := &workflowv1.ListWorkflowPublicationsResponse{Publications: make([]*workflowv1.WorkflowPublicationSummary, 0, len(selected))}
	for _, publication := range selected {
		view, err := workflowview.Build(publication, nil)
		if err != nil {
			return nil, projectReadError(err, inv, p)
		}
		response.Publications = append(response.Publications, projectPublication(publication, view.Name))
	}
	sort.SliceStable(response.Publications, func(i, j int) bool {
		left, right := response.Publications[i], response.Publications[j]
		if strings.EqualFold(left.GetName(), right.GetName()) {
			return left.GetWorkflowId() < right.GetWorkflowId()
		}
		return strings.ToLower(left.GetName()) < strings.ToLower(right.GetName())
	})
	return response, nil
}

func (s *server) GetWorkflowDefinitionView(ctx context.Context, req *workflowv1.GetWorkflowDefinitionViewRequest) (*workflowv1.GetWorkflowDefinitionViewResponse, error) {
	p, inv, ownedErr := trustedContext(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	if req == nil || strings.TrimSpace(req.GetWorkflowId()) == "" && strings.TrimSpace(req.GetInstanceId()) == "" {
		return nil, invalid(inv, "workflow_id")
	}
	if !s.authorized(ctx, p, ActionGetWorkflowDefinitionView) {
		return nil, denied(inv, p)
	}
	if s.deps.Definitions == nil {
		return nil, projectReadError(errors.New("workflow publication catalog is not configured"), inv, p)
	}

	workflowID := strings.TrimSpace(req.GetWorkflowId())
	var live *inspect.View
	var pinnedDigest string
	if instanceID := strings.TrimSpace(req.GetInstanceId()); instanceID != "" {
		record, readErr := s.read(ctx, p.Tenant().String(), instanceID)
		if readErr != nil {
			return nil, projectReadError(readErr, inv, p)
		}
		if err := validateRecord(record, p.Tenant().String(), instanceID); err != nil {
			return nil, projectReadError(err, inv, p)
		}
		if record.Inspector == nil {
			return nil, projectReadError(errors.New("workflow inspector projection is unavailable"), inv, p)
		}
		if workflowID != "" && workflowID != record.Instance.WorkflowID {
			return nil, projectReadError(ErrNotFound, inv, p)
		}
		workflowID, pinnedDigest, live = record.Instance.WorkflowID, record.Instance.CompiledPlanDigest, record.Inspector
	}

	publication, found, readErr := resolvePublication(s.deps.Definitions, workflowID, pinnedDigest)
	if readErr != nil {
		return nil, projectReadError(readErr, inv, p)
	}
	if !found {
		return nil, projectReadError(ErrNotFound, inv, p)
	}
	view, err := workflowview.Build(publication, live)
	if err != nil {
		return nil, projectReadError(err, inv, p)
	}
	return &workflowv1.GetWorkflowDefinitionViewResponse{View: projectDefinitionView(view)}, nil
}

func selectCatalogVersions(versions []workflowversion.CompiledVersion) []workflowversion.CompiledVersion {
	selected := make(map[string]workflowversion.CompiledVersion)
	for _, candidate := range versions {
		current, ok := selected[candidate.WorkflowID]
		if !ok || preferCatalogVersion(candidate, current) {
			selected[candidate.WorkflowID] = candidate
		}
	}
	result := make([]workflowversion.CompiledVersion, 0, len(selected))
	for _, publication := range selected {
		result = append(result, publication)
	}
	return result
}

func preferCatalogVersion(candidate, current workflowversion.CompiledVersion) bool {
	if candidate.Status == workflowversion.StatusActive && current.Status != workflowversion.StatusActive {
		return true
	}
	if candidate.Status != workflowversion.StatusActive && current.Status == workflowversion.StatusActive {
		return false
	}
	if candidate.PublishedAt.Equal(current.PublishedAt) {
		return candidate.CompiledPlanDigest > current.CompiledPlanDigest
	}
	return candidate.PublishedAt.After(current.PublishedAt)
}

func resolvePublication(reader DefinitionReader, workflowID, digest string) (workflowversion.CompiledVersion, bool, error) {
	if digest != "" {
		publication, found, err := reader.GetByDigest(digest)
		if err != nil || !found || publication.WorkflowID != workflowID {
			return workflowversion.CompiledVersion{}, found && publication.WorkflowID == workflowID, err
		}
		return publication, true, nil
	}
	publication, found, err := reader.GetActiveForWorkflow(workflowID)
	if err != nil || found {
		return publication, found, err
	}
	versions, err := reader.List(workflowID)
	if err != nil || len(versions) == 0 {
		return workflowversion.CompiledVersion{}, false, err
	}
	return selectCatalogVersions(versions)[0], true, nil
}

func projectPublication(publication workflowversion.CompiledVersion, name string) *workflowv1.WorkflowPublicationSummary {
	return &workflowv1.WorkflowPublicationSummary{
		WorkflowId: publication.WorkflowID, Name: name, DefinitionVersion: publication.DefinitionVersion,
		SemanticVersion: publication.SemanticVersion, CompiledPlanDigest: publication.CompiledPlanDigest,
		Status: string(publication.Status), PublishedBy: publication.PublishedBy, PublishedAt: timestamp(publication.PublishedAt),
	}
}

func projectDefinitionView(view workflowview.View) *workflowv1.WorkflowDefinitionView {
	out := &workflowv1.WorkflowDefinitionView{
		WorkflowId: view.WorkflowID, Name: view.Name, DefinitionVersion: view.Version,
		SemanticVersion: view.SemanticVersion, CompiledPlanDigest: view.PlanDigest,
		PublicationStatus: view.PublicationStatus, HasRun: view.HasRun, RunDisclosed: view.RunDisclosed,
		InstanceId: view.InstanceID, RuntimeStatus: view.RuntimeStatus, Complete: view.Completeness,
		Redactions: append([]string(nil), view.Redactions...), Gaps: append([]string(nil), view.Gaps...),
		MaxDepth: int32(view.MaxDepth), MaxLane: int32(view.MaxLane),
		Nodes: make([]*workflowv1.WorkflowViewNode, 0, len(view.Nodes)),
		Edges: make([]*workflowv1.WorkflowViewEdge, 0, len(view.Edges)),
	}
	for _, node := range view.Nodes {
		projected := &workflowv1.WorkflowViewNode{
			Id: node.ID, Label: node.Label, StepType: node.StepType, Depth: int32(node.Depth), Lane: int32(node.Lane),
			Start: node.Start, Terminal: node.Terminal, Current: node.Current, Attempt: int32(node.Attempt),
			Status: node.Status, State: string(node.State), StartedAt: timestampPtr(node.StartedAt),
			CompletedAt: timestampPtr(node.CompletedAt), RuntimeKnown: node.RuntimeKnown, RuntimeGap: node.RuntimeGap,
			Routes: make([]*workflowv1.WorkflowViewRoute, 0, len(node.Routes)),
		}
		for _, route := range node.Routes {
			projected.Routes = append(projected.Routes, &workflowv1.WorkflowViewRoute{Key: route.Key, TargetId: route.TargetID})
		}
		out.Nodes = append(out.Nodes, projected)
	}
	for _, edge := range view.Edges {
		out.Edges = append(out.Edges, &workflowv1.WorkflowViewEdge{Id: edge.ID, FromId: edge.FromID, ToId: edge.ToID, RouteKey: edge.RouteKey})
	}
	return out
}

func (s *server) GetWorkflow(ctx context.Context, req *workflowv1.GetWorkflowRequest) (*workflowv1.GetWorkflowResponse, error) {
	p, inv, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetInstanceId()) == "" {
		return nil, invalid(inv, "instance_id")
	}
	if !s.authorized(ctx, p, ActionGetWorkflow) {
		return nil, denied(inv, p)
	}
	record, readErr := s.read(ctx, p.Tenant().String(), req.GetInstanceId())
	if readErr != nil {
		return nil, projectReadError(readErr, inv, p)
	}
	if err := validateRecord(record, p.Tenant().String(), req.GetInstanceId()); err != nil {
		return nil, projectReadError(err, inv, p)
	}
	// RBAC-RT-004: capability is not enough for instance reads. Past this
	// point the caller must participate in the instance, supervise one of
	// its subjects, or hold the durable operator grant; anything else gets
	// the same NOT_FOUND as a genuinely absent instance.
	if !s.authorized(ctx, p, ActionInspectAnySubject) && !s.mayInspectSubject(ctx, p, record.Instance.BusinessSubjectRefs) {
		return nil, projectReadError(ErrNotFound, inv, p)
	}
	return &workflowv1.GetWorkflowResponse{Instance: ProjectInstance(record.Instance)}, nil
}

func (s *server) ListNodeExecutions(ctx context.Context, req *workflowv1.ListNodeExecutionsRequest) (*workflowv1.ListNodeExecutionsResponse, error) {
	p, inv, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetInstanceId()) == "" {
		return nil, invalid(inv, "instance_id")
	}
	if !s.authorized(ctx, p, ActionListNodeExecutions) {
		return nil, denied(inv, p)
	}
	record, readErr := s.read(ctx, p.Tenant().String(), req.GetInstanceId())
	if readErr != nil {
		return nil, projectReadError(readErr, inv, p)
	}
	if err := validateRecord(record, p.Tenant().String(), req.GetInstanceId()); err != nil {
		return nil, projectReadError(err, inv, p)
	}
	if !s.authorized(ctx, p, ActionInspectAnySubject) && !s.mayInspectSubject(ctx, p, record.Instance.BusinessSubjectRefs) {
		return nil, projectReadError(ErrNotFound, inv, p)
	}
	nodes := append([]NodeExecution(nil), record.Nodes...)
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].NodeID != nodes[j].NodeID {
			return nodes[i].NodeID < nodes[j].NodeID
		}
		if nodes[i].Attempt != nodes[j].Attempt {
			return nodes[i].Attempt < nodes[j].Attempt
		}
		return nodes[i].NodeExecutionID < nodes[j].NodeExecutionID
	})
	page := req.GetPage()
	pageSize, pageErr := pageSizeOf(page)
	if pageErr != nil {
		return nil, invalid(inv, "page.page_size")
	}
	start, cursorErr := decodeSnapshotCursor(page, s.deps.CursorKey, s.deps.PreviousCursorKey, p.Tenant().String(), req.GetInstanceId(), record.Instance.InstanceVersion)
	if cursorErr != nil {
		return nil, invalid(inv, "page.cursor")
	}
	if start > len(nodes) {
		return nil, invalid(inv, "page.cursor")
	}
	end := start + pageSize
	if end > len(nodes) {
		end = len(nodes)
	}
	res := &workflowv1.ListNodeExecutionsResponse{Page: &commonv1.PageResponse{}}
	for _, node := range nodes[start:end] {
		res.NodeExecutions = append(res.NodeExecutions, ProjectNodeExecution(node))
	}
	if end < len(nodes) {
		cursor, encodeErr := encodeSnapshotCursor(snapshotCursor{
			TenantID: p.Tenant().String(), InstanceID: req.GetInstanceId(),
			InstanceVersion: record.Instance.InstanceVersion, Index: end,
		}, s.deps.CursorKey)
		if encodeErr != nil {
			return nil, projectReadError(encodeErr, inv, p)
		}
		res.Page.NextCursor = cursor
	}
	return res, nil
}

func pageSizeOf(page *commonv1.PageRequest) (int, error) {
	if page == nil || page.GetPageSize() == 0 {
		return defaultPageSize, nil
	}
	if page.GetPageSize() < 0 || page.GetPageSize() > maxPageSize {
		return 0, ErrInvalidCursor
	}
	return int(page.GetPageSize()), nil
}

func validateRecord(record Record, tenant, requestedID string) error {
	if record.Instance.TenantID != "" && record.Instance.TenantID != tenant {
		return ErrNotFound
	}
	if record.Instance.InstanceID != "" && record.Instance.InstanceID != requestedID {
		return ErrNotFound
	}
	for _, node := range record.Nodes {
		if node.WorkflowInstanceID != "" && node.WorkflowInstanceID != requestedID {
			return ErrInvalidRecord
		}
	}
	return nil
}

func (s *server) read(ctx context.Context, tenant, id string) (Record, error) {
	if s.deps.Instances == nil {
		return Record{}, errors.New("workflow inspection reader is not configured")
	}
	return s.deps.Instances.ReadWorkflowInstance(ctx, tenant, id)
}
func (s *server) authorized(ctx context.Context, p *trust.Principal, action string) bool {
	if s.deps.Authorize == nil {
		return false
	}
	return s.deps.Authorize(ctx, p, action)
}

// mayInspectSubject reports whether p may read an instance whose business
// subjects are refs: a participant named by the record, a supervisor of a
// named subject, or a caller the capability gate already admitted for
// operator inspection. Supervision errors fail closed to the remaining
// admissions, never to an allow.
func (s *server) mayInspectSubject(ctx context.Context, p *trust.Principal, refs []string) bool {
	if p == nil {
		return false
	}
	if participantOf(p.Subject(), refs) {
		return true
	}
	if s.deps.Supervision != nil {
		for _, ref := range refs {
			ok, err := s.deps.Supervision(ctx, p.Subject(), ref)
			if err != nil || !ok {
				continue
			}
			return true
		}
	}
	return false
}

// participantOf reports whether subject is named by the instance's business
// subject refs. Refs are short kind:id pairs (worker:jane,
// employment:doe-1); the principal subject is the bare identity, so a ref
// matches on the whole string or on its id part, case-insensitively. An
// empty ref names no one.
func participantOf(subject string, refs []string) bool {
	subject = strings.ToLower(strings.TrimSpace(subject))
	if subject == "" {
		return false
	}
	for _, ref := range refs {
		candidate := strings.ToLower(strings.TrimSpace(ref))
		if candidate == "" {
			continue
		}
		if candidate == subject {
			return true
		}
		if strings.Contains(candidate, ":") {
			parts := strings.Split(candidate, ":")
			if strings.TrimSpace(parts[len(parts)-1]) == subject {
				return true
			}
		}
	}
	return false
}

func trustedContext(ctx context.Context) (*trust.Principal, *transport.Invocation, *envelope.Error) {
	inv, ok := transport.InvocationFromContext(ctx)
	if !ok {
		return nil, nil, envelope.New(envelope.CodeUnauthenticated, "workflow.no_trusted_context", "the request carries no trusted context")
	}
	p, ok := trust.FromContext(ctx)
	if !ok {
		return nil, inv, envelope.New(envelope.CodeUnauthenticated, "workflow.no_principal", "the request carries no authenticated principal").WithCorrelation(inv.RequestID())
	}
	return p, inv, nil
}
func invalid(inv *transport.Invocation, field string) *envelope.Error {
	err := envelope.New(envelope.CodeInvalidArgument, "workflow.invalid_request", "the request is invalid").WithViolation(field, "the field is required or malformed", "workflow.request")
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	return err
}
func denied(inv *transport.Invocation, p *trust.Principal) *envelope.Error {
	err := envelope.New(envelope.CodePermissionDenied, "workflow.inspection_denied", "the caller is not authorized to inspect this workflow")
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	if p != nil {
		err.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authentication"})
	}
	return err
}
func projectReadError(err error, inv *transport.Invocation, p *trust.Principal) *envelope.Error {
	code, reason, message := envelope.CodeUnavailable, "workflow.inspection_unavailable", "workflow inspection is unavailable"
	if errors.Is(err, ErrNotFound) {
		code, reason, message = envelope.CodeNotFound, "workflow.not_found", "the workflow does not exist or is not visible"
	}
	out := envelope.New(code, reason, message).WithDiagnostic(err)
	if inv != nil {
		out.WithCorrelation(inv.RequestID())
	}
	if p != nil {
		out.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authentication"})
	}
	return out
}

func ProjectInstance(i Instance) *workflowv1.WorkflowInstance {
	refs := make([]*commonv1.EntityRef, 0, len(i.BusinessSubjectRefs))
	for _, ref := range i.BusinessSubjectRefs {
		refs = append(refs, &commonv1.EntityRef{TenantId: i.TenantID, Kind: "business_subject", Id: ref})
	}
	out := &workflowv1.WorkflowInstance{InstanceId: i.InstanceID, TenantId: i.TenantID, Definition: &workflowv1.WorkflowDefinitionReference{WorkflowId: i.WorkflowID, Version: i.WorkflowVersion}, CompiledPlanDigest: i.CompiledPlanDigest, BusinessSubjectRefs: refs, ExecutionMode: projectExecutionMode(i.ExecutionMode), RuntimeStatus: projectRuntimeStatus(i.RuntimeStatus), Lifecycle: &intentsv1.LifecycleDimensions{Request: requestState(i.RequestState), Execution: executionState(i.ExecutionState), Business: businessState(i.BusinessState), Consistency: consistencyState(i.ConsistencyState), Obligation: obligationState(i.ObligationState)}, InputRef: i.InputRef, CurrentNodeIds: append([]string(nil), i.CurrentNodeIDs...), InstanceVersion: i.InstanceVersion, CorrelationId: i.CorrelationID, CreatedAt: timestamp(i.CreatedAt), StartedAt: timestampPtr(i.StartedAt), CompletedAt: timestampPtr(i.CompletedAt)}
	if i.CellID != "" {
		out.CellId = &i.CellID
	}
	if i.BusinessTransactionID != "" {
		out.BusinessTransactionId = &i.BusinessTransactionID
	}
	if i.VariableRevisionHead != "" {
		out.VariableRevisionHead = &i.VariableRevisionHead
	}
	if i.EffectiveContextRef != "" {
		out.EffectiveContextRef = &i.EffectiveContextRef
	}
	if i.LastCheckpointRef != "" {
		out.LastCheckpointRef = &i.LastCheckpointRef
	}
	return out
}

func ProjectNodeExecution(n NodeExecution) *workflowv1.NodeExecution {
	return &workflowv1.NodeExecution{NodeExecutionId: n.NodeExecutionID, WorkflowInstanceId: n.WorkflowInstanceID, NodeId: n.NodeID, Attempt: n.Attempt, Status: projectNodeStatus(n.Status), InputSnapshotRef: n.InputSnapshotRef, OutputArtifactRef: optional(n.OutputArtifactRef), CapabilityExecutionId: optional(n.CapabilityExecutionID), AuthorizationDecisionId: optional(n.AuthorizationDecisionID), DecisionId: optional(n.DecisionID), HumanTaskId: optional(n.HumanTaskID), ErrorClass: optional(n.ErrorClass), RetryAt: timestampPtr(n.RetryAt), ExecutionLeaseId: optional(n.ExecutionLeaseID), StartedAt: timestampPtr(n.StartedAt), CompletedAt: timestampPtr(n.CompletedAt), TraceId: n.TraceID}
}
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func timestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t.UTC())
}
func timestampPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamp(*t)
}
func projectExecutionMode(s string) intentsv1.ExecutionMode {
	switch s {
	case "SIMULATE":
		return intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE
	case "EXECUTE":
		return intentsv1.ExecutionMode_EXECUTION_MODE_EXECUTE
	case "REPLAY":
		return intentsv1.ExecutionMode_EXECUTION_MODE_REPLAY
	case "REPAIR":
		return intentsv1.ExecutionMode_EXECUTION_MODE_REPAIR
	case "SHADOW":
		return intentsv1.ExecutionMode_EXECUTION_MODE_SHADOW
	default:
		return intentsv1.ExecutionMode_EXECUTION_MODE_UNSPECIFIED
	}
}
func projectRuntimeStatus(s string) workflowv1.RuntimeStatus {
	for n, v := range workflowv1.RuntimeStatus_value {
		if strings.HasSuffix(n, "_"+s) {
			return workflowv1.RuntimeStatus(v)
		}
	}
	return workflowv1.RuntimeStatus_RUNTIME_STATUS_UNSPECIFIED
}
func projectNodeStatus(s string) workflowv1.NodeExecutionStatus {
	for n, v := range workflowv1.NodeExecutionStatus_value {
		if strings.HasSuffix(n, "_"+s) {
			return workflowv1.NodeExecutionStatus(v)
		}
	}
	return workflowv1.NodeExecutionStatus_NODE_EXECUTION_STATUS_UNSPECIFIED
}
func requestState(s string) intentsv1.RequestState {
	return intentsv1.RequestState(enumValue(intentsv1.RequestState_value, s))
}
func executionState(s string) intentsv1.ExecutionState {
	return intentsv1.ExecutionState(enumValue(intentsv1.ExecutionState_value, s))
}
func businessState(s string) intentsv1.BusinessState {
	return intentsv1.BusinessState(enumValue(intentsv1.BusinessState_value, s))
}
func consistencyState(s string) intentsv1.ConsistencyState {
	return intentsv1.ConsistencyState(enumValue(intentsv1.ConsistencyState_value, s))
}
func obligationState(s string) intentsv1.ObligationState {
	return intentsv1.ObligationState(enumValue(intentsv1.ObligationState_value, s))
}
func enumValue(values map[string]int32, state string) int32 {
	for name, value := range values {
		if state == "" && strings.HasSuffix(name, "UNSPECIFIED") || strings.HasSuffix(name, "_"+state) {
			return value
		}
	}
	return 0
}

type snapshotCursor struct {
	TenantID        string `json:"tenant_id"`
	InstanceID      string `json:"instance_id"`
	InstanceVersion uint64 `json:"instance_version"`
	Index           int    `json:"index"`
}

func encodeSnapshotCursor(cursor snapshotCursor, key []byte) (string, error) {
	secret, err := cursorKey(key)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("workflow: encode inspection cursor: %w", err)
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString(append(append(payload, '.'), []byte(sig)...)), nil
}

// decodeSnapshotCursor verifies an inspection cursor minted by
// [encodeSnapshotCursor]. key is the active page-cursor key; previous is
// the retired key, accepted for verification only while in-flight cursors
// minted under it drain. A cursor from any other key fails closed.
func decodeSnapshotCursor(page *commonv1.PageRequest, key, previous []byte, tenant, instance string, version uint64) (int, error) {
	if page == nil || page.GetCursor() == "" {
		return 0, nil
	}
	if len(key) == 0 {
		return 0, ErrCursorKeyUnset
	}
	raw, err := base64.RawURLEncoding.DecodeString(page.GetCursor())
	if err != nil {
		return 0, ErrInvalidCursor
	}
	parts := strings.SplitN(string(raw), ".", 2)
	if len(parts) != 2 {
		return 0, ErrInvalidCursor
	}
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, ErrInvalidCursor
	}
	if !verifySnapshotMAC([]byte(parts[0]), got, key) && !verifySnapshotMAC([]byte(parts[0]), got, previous) {
		return 0, ErrInvalidCursor
	}
	var cursor snapshotCursor
	if err := json.Unmarshal([]byte(parts[0]), &cursor); err != nil || cursor.TenantID != tenant || cursor.InstanceID != instance || cursor.InstanceVersion != version || cursor.Index < 0 {
		return 0, ErrInvalidCursor
	}
	return cursor.Index, nil
}
func cursorKey(key []byte) ([]byte, error) {
	if len(key) != 0 {
		return key, nil
	}
	return nil, ErrCursorKeyUnset
}

// verifySnapshotMAC reports whether sig is the HMAC-SHA256 of raw under
// key. An empty key never verifies: rotation acceptance comes only from an
// explicitly configured retired key, never from a missing one.
func verifySnapshotMAC(raw, sig, key []byte) bool {
	if len(key) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	return hmac.Equal(mac.Sum(nil), sig)
}
