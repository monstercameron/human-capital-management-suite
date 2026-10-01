package timeclock

import (
	"context"
	"strings"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// WorkerClockServer is the thin gRPC adapter for the authenticated worker
// self-service clock. Authorization and workflow execution remain in app.
type WorkerClockServer struct {
	timev1.UnimplementedWorkerClockServiceServer
	app WorkerSelfService
}

// NewWorkerClockServer constructs the worker self-service gRPC boundary.
func NewWorkerClockServer(app WorkerSelfService) *WorkerClockServer {
	return &WorkerClockServer{app: app}
}

// RegisterWorkerClock installs the worker self-service service on a gRPC
// server without changing the existing device service registration.
func RegisterWorkerClock(server *grpc.Server, app WorkerSelfService) {
	if server != nil {
		timev1.RegisterWorkerClockServiceServer(server, NewWorkerClockServer(app))
	}
}

func (s *WorkerClockServer) trustedWorker(ctx context.Context) (*trust.Principal, error) {
	if s == nil || s.app == nil {
		return nil, clockservice.ErrUnavailable
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if p.SubjectKind() != trust.SubjectKindHuman {
		return nil, clockservice.ErrInvalidPrincipal
	}
	return p, nil
}

// GetSelfClock returns the server-authorized current worker projection.
func (s *WorkerClockServer) GetSelfClock(ctx context.Context, in *timev1.GetSelfClockRequest) (*timev1.GetSelfClockResponse, error) {
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	p, err := s.trustedWorker(ctx)
	if err != nil {
		return nil, serviceError(err)
	}
	projection, err := s.app.GetSelfClock(ctx, p)
	if err != nil {
		return nil, selfServiceError(err)
	}
	return &timev1.GetSelfClockResponse{WorkerLabel: projection.WorkerLabel, ScheduleLabel: projection.ScheduleLabel, StatusLabel: projection.StatusLabel, LastEventLabel: projection.LastEventLabel, Revision: projection.Revision, StatusCode: projection.StatusCode}, nil
}

// ExecuteSelfClockAction submits a worker self action to the published
// workflow adapter. Worker and assignment identity are never accepted from
// the wire.
func (s *WorkerClockServer) ExecuteSelfClockAction(ctx context.Context, in *timev1.ExecuteSelfClockActionRequest) (*timev1.ExecuteSelfClockActionResponse, error) {
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	p, err := s.trustedWorker(ctx)
	if err != nil {
		return nil, serviceError(err)
	}
	var action string
	switch in.GetAction() {
	case timev1.ExecuteSelfClockActionRequest_ACTION_IN:
		action = "IN"
	case timev1.ExecuteSelfClockActionRequest_ACTION_OUT:
		action = "OUT"
	case timev1.ExecuteSelfClockActionRequest_ACTION_START_BREAK:
		action = "START_BREAK"
	case timev1.ExecuteSelfClockActionRequest_ACTION_END_BREAK:
		action = "END_BREAK"
	default:
		return nil, serviceError(clockservice.ErrInvalidRequest)
	}
	result, err := s.app.ExecuteSelfClockAction(ctx, p, clockservice.SelfClockActionRequest{Action: action, ExpectedRevision: in.GetExpectedRevision(), IdempotencyKey: in.GetIdempotencyKey()})
	if err != nil {
		return nil, selfServiceError(err)
	}
	return &timev1.ExecuteSelfClockActionResponse{ReceiptId: result.ReceiptID, WorkerRef: result.WorkerRef, AssignmentRef: result.AssignmentRef, WorkflowInstanceRef: result.WorkflowInstanceRef, PublishedPlanRef: result.PublishedPlanRef, Status: &timev1.SelfClockProjection{WorkerLabel: result.Status.WorkerLabel, ScheduleLabel: result.Status.ScheduleLabel, StatusLabel: result.Status.StatusLabel, LastEventLabel: result.Status.LastEventLabel, Revision: result.Status.Revision, StatusCode: result.Status.StatusCode}, WorkflowTraceId: result.WorkflowTraceID, WorkflowNodeId: result.WorkflowNodeID, WorkflowAttempt: int32(result.WorkflowAttempt), WorkflowInstanceVersion: result.WorkflowInstanceVersion, WorkflowId: result.WorkflowID}, nil
}

var _ timev1.WorkerClockServiceServer = (*WorkerClockServer)(nil)

// NotEnabledWorkerClock is the worker clock of a workspace that does not run
// the time clock. It answers every call with the closed "not turned on" reason,
// so the browser can say so; a generated Unimplemented stub would be coerced to
// a generic outage by the transport and the page could not tell the two apart.
type NotEnabledWorkerClock struct {
	timev1.UnimplementedWorkerClockServiceServer
}

func notEnabledError() error {
	return envelope.New(envelope.CodeFailedPrecondition, SelfClockReasonRefPrefix+strings.ToLower(string(clockservice.ReasonNotEnabled)), "the time clock is not turned on for this workspace")
}

// GetSelfClock reports that the clock is not turned on.
func (NotEnabledWorkerClock) GetSelfClock(context.Context, *timev1.GetSelfClockRequest) (*timev1.GetSelfClockResponse, error) {
	return nil, notEnabledError()
}

// ExecuteSelfClockAction reports that the clock is not turned on.
func (NotEnabledWorkerClock) ExecuteSelfClockAction(context.Context, *timev1.ExecuteSelfClockActionRequest) (*timev1.ExecuteSelfClockActionResponse, error) {
	return nil, notEnabledError()
}

// RegisterNotEnabledWorkerClock installs [NotEnabledWorkerClock] on a server.
func RegisterNotEnabledWorkerClock(server *grpc.Server) {
	if server != nil {
		timev1.RegisterWorkerClockServiceServer(server, NotEnabledWorkerClock{})
	}
}

var _ timev1.WorkerClockServiceServer = NotEnabledWorkerClock{}

// SelfClockReasonRefPrefix starts the reason reference of a self-clock refusal
// that is a decision about the worker (a FailedPrecondition). The remainder is
// one closed clockservice.SelfClockReason in lower case. The browser client
// reads it from the platform's canonical error detail, so the text is a
// contract; it carries no worker, assignment or profile identity.
const SelfClockReasonRefPrefix = "time.clock.self."

// selfServiceError maps a self-clock failure to a gRPC status. An ineligibility
// decision keeps its closed reason; every other failure keeps the shared
// mapping, so an outage is not mistaken for a decision about the worker.
func selfServiceError(err error) error {
	if reason, ok := clockservice.ReasonOf(err); ok {
		return envelope.New(envelope.CodeFailedPrecondition, SelfClockReasonRefPrefix+strings.ToLower(string(reason)), "the time clock is not available for this worker")
	}
	return serviceError(err)
}
