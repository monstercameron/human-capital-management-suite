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
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	GetWorkflowProcedure        = "/hcmnext.workflow.v1.WorkflowService/GetWorkflow"
	ListNodeExecutionsProcedure = "/hcmnext.workflow.v1.WorkflowService/ListNodeExecutions"
	ActionGetWorkflow           = "get_workflow"
	ActionListNodeExecutions    = "list_node_executions"
	defaultPageSize             = 100
	maxPageSize                 = 1000
)

var (
	ErrNotFound       = errors.New("workflow: instance not found")
	ErrInvalidRecord  = errors.New("workflow: inspection record is invalid")
	ErrInvalidCursor  = errors.New("workflow: inspection cursor is invalid")
	ErrCursorKeyUnset = errors.New("workflow: inspection cursor key is unset")
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
}

type Reader interface {
	ReadWorkflowInstance(context.Context, string, string) (Record, error)
}

type Dependencies struct {
	Instances Reader
	Authorize func(*trust.Principal, string) bool
	CursorKey []byte
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

func (s *server) GetWorkflow(ctx context.Context, req *workflowv1.GetWorkflowRequest) (*workflowv1.GetWorkflowResponse, error) {
	p, inv, err := trustedContext(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetInstanceId()) == "" {
		return nil, invalid(inv, "instance_id")
	}
	if !s.authorized(p, ActionGetWorkflow) {
		return nil, denied(inv, p)
	}
	record, readErr := s.read(ctx, p.Tenant().String(), req.GetInstanceId())
	if readErr != nil {
		return nil, projectReadError(readErr, inv, p)
	}
	if err := validateRecord(record, p.Tenant().String(), req.GetInstanceId()); err != nil {
		return nil, projectReadError(err, inv, p)
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
	if !s.authorized(p, ActionListNodeExecutions) {
		return nil, denied(inv, p)
	}
	record, readErr := s.read(ctx, p.Tenant().String(), req.GetInstanceId())
	if readErr != nil {
		return nil, projectReadError(readErr, inv, p)
	}
	if err := validateRecord(record, p.Tenant().String(), req.GetInstanceId()); err != nil {
		return nil, projectReadError(err, inv, p)
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
	start, cursorErr := decodeSnapshotCursor(page, s.deps.CursorKey, p.Tenant().String(), req.GetInstanceId(), record.Instance.InstanceVersion)
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
func (s *server) authorized(p *trust.Principal, action string) bool {
	return s.deps.Authorize == nil || s.deps.Authorize(p, action)
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

func decodeSnapshotCursor(page *commonv1.PageRequest, key []byte, tenant, instance string, version uint64) (int, error) {
	if page == nil || page.GetCursor() == "" {
		return 0, nil
	}
	secret, keyErr := cursorKey(key)
	if keyErr != nil {
		return 0, keyErr
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
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(got, mac.Sum(nil)) {
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
