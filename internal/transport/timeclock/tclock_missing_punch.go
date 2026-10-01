package timeclock

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/google/uuid"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MissingPunchSubmit is the principal-bound input for a correction request.
// Worker and tenant identity are deliberately absent; the application derives
// them from the authenticated human principal.
type MissingPunchSubmit struct {
	SessionID, ClaimedEventType, Reason, IdempotencyKey string
	ClaimedOccurredAt                                   time.Time
	ExpectedRevision                                    uint64
}

// MissingPunchReview is the principal-bound supervisor decision input.
type MissingPunchReview struct {
	RequestID, Reason, IdempotencyKey string
	ExpectedRevision                  uint64
	Decision                          string
}

// MissingPunchReceipt is the durable workflow evidence returned with a
// successful command.
type MissingPunchReceipt struct {
	ReceiptID, WorkflowInstanceRef, WorkflowTraceID, WorkflowNodeID string
	WorkflowID, WorkflowPlanDigest                                  string
	WorkflowAttempt                                                 int32
	WorkflowInstanceVersion                                         int64
}

// MissingPunchCorrection is the server-owned correction projection.
type MissingPunchCorrection struct {
	RequestID, WorkerRef, SessionID, ClaimedEventType string
	ClaimedOccurredAt                                 time.Time
	RequestReason, Status, DecisionReason             string
	Revision                                          uint64
	WorkflowReceipt                                   MissingPunchReceipt
}

// MissingPunchApplication is the application port for the thin boundary.
// Implementations perform authorization, tenant scoping, workflow execution,
// revision checks and persistence.
type MissingPunchApplication interface {
	SubmitCorrection(context.Context, *trust.Principal, MissingPunchSubmit) (MissingPunchCorrection, error)
	ReviewCorrection(context.Context, *trust.Principal, MissingPunchReview) (MissingPunchCorrection, error)
}

// MissingPunchServer implements the generated MissingPunchService boundary.
type MissingPunchServer struct {
	timev1.UnimplementedMissingPunchServiceServer
	app MissingPunchApplication
}

// NewMissingPunchServer constructs a missing-punch boundary.
func NewMissingPunchServer(app MissingPunchApplication) *MissingPunchServer {
	return &MissingPunchServer{app: app}
}

// RegisterMissingPunch installs the service on a gRPC server.
func RegisterMissingPunch(server *grpc.Server, app MissingPunchApplication) {
	if server != nil {
		timev1.RegisterMissingPunchServiceServer(server, NewMissingPunchServer(app))
	}
}

func (s *MissingPunchServer) trustedHuman(ctx context.Context) (*trust.Principal, error) {
	if s == nil || s.app == nil {
		return nil, status.Error(codes.Unavailable, "missing punch service is unavailable")
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, serviceError(err)
	}
	if p.SubjectKind() != trust.SubjectKindHuman {
		return nil, status.Error(codes.Unauthenticated, "trusted human principal required")
	}
	return p, nil
}

// SubmitCorrection records a worker's assertion without accepting worker or
// tenant authority from the request.
func (s *MissingPunchServer) SubmitCorrection(ctx context.Context, in *timev1.SubmitCorrectionRequest) (*timev1.SubmitCorrectionResponse, error) {
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	p, err := s.trustedHuman(ctx)
	if err != nil {
		return nil, err
	}
	if in.GetClaimedOccurredAt() == nil || in.GetReason() == "" || in.GetIdempotencyKey() == "" || in.GetExpectedRevision() == 0 || in.GetClaimedEventType() != "CLOCK_OUT" {
		return nil, status.Error(codes.InvalidArgument, "claimed_occurred_at, reason and idempotency_key are required")
	}
	if err := in.GetClaimedOccurredAt().CheckValid(); err != nil {
		return nil, status.Error(codes.InvalidArgument, "claimed_occurred_at is invalid")
	}
	result, err := s.app.SubmitCorrection(ctx, p, MissingPunchSubmit{SessionID: in.GetSessionId(), ClaimedEventType: in.GetClaimedEventType(), Reason: in.GetReason(), IdempotencyKey: in.GetIdempotencyKey(), ClaimedOccurredAt: in.GetClaimedOccurredAt().AsTime(), ExpectedRevision: in.GetExpectedRevision()})
	if err != nil {
		return nil, missingPunchError(err)
	}
	if err := validateMissingPunchReceipt(result.WorkflowReceipt); err != nil {
		return nil, err
	}
	return &timev1.SubmitCorrectionResponse{Correction: missingPunchCorrection(result)}, nil
}

// ReviewCorrection records an independent approval or rejection.
func (s *MissingPunchServer) ReviewCorrection(ctx context.Context, in *timev1.ReviewCorrectionRequest) (*timev1.ReviewCorrectionResponse, error) {
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	p, err := s.trustedHuman(ctx)
	if err != nil {
		return nil, err
	}
	decision := ""
	switch in.GetDecision() {
	case timev1.MissingPunchDecision_MISSING_PUNCH_DECISION_APPROVED:
		decision = "APPROVED"
	case timev1.MissingPunchDecision_MISSING_PUNCH_DECISION_REJECTED:
		decision = "REJECTED"
	default:
		return nil, status.Error(codes.InvalidArgument, "decision must be approved or rejected")
	}
	if in.GetRequestId() == "" || in.GetExpectedRevision() == 0 || in.GetReason() == "" || in.GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "request_id, expected_revision, reason and idempotency_key are required")
	}
	result, err := s.app.ReviewCorrection(ctx, p, MissingPunchReview{RequestID: in.GetRequestId(), ExpectedRevision: in.GetExpectedRevision(), Decision: decision, Reason: in.GetReason(), IdempotencyKey: in.GetIdempotencyKey()})
	if err != nil {
		return nil, missingPunchError(err)
	}
	if err := validateMissingPunchReceipt(result.WorkflowReceipt); err != nil {
		return nil, err
	}
	return &timev1.ReviewCorrectionResponse{Correction: missingPunchCorrection(result)}, nil
}

func missingPunchCorrection(in MissingPunchCorrection) *timev1.MissingPunchCorrection {
	return &timev1.MissingPunchCorrection{RequestId: in.RequestID, WorkerRef: in.WorkerRef, SessionId: in.SessionID, ClaimedEventType: in.ClaimedEventType, ClaimedOccurredAt: timestamppb.New(in.ClaimedOccurredAt), RequestReason: in.RequestReason, Status: in.Status, Revision: in.Revision, DecisionReason: in.DecisionReason, WorkflowReceipt: &timev1.WorkflowTrackedReceipt{ReceiptId: in.WorkflowReceipt.ReceiptID, WorkflowInstanceRef: in.WorkflowReceipt.WorkflowInstanceRef, WorkflowTraceId: in.WorkflowReceipt.WorkflowTraceID, WorkflowNodeId: in.WorkflowReceipt.WorkflowNodeID, WorkflowAttempt: in.WorkflowReceipt.WorkflowAttempt, WorkflowInstanceVersion: in.WorkflowReceipt.WorkflowInstanceVersion, WorkflowId: in.WorkflowReceipt.WorkflowID, WorkflowPlanDigest: in.WorkflowReceipt.WorkflowPlanDigest}}
}

func validateMissingPunchReceipt(r MissingPunchReceipt) error {
	if r.ReceiptID == "" || uuid.Validate(r.WorkflowInstanceRef) != nil || r.WorkflowTraceID == "" || r.WorkflowID != "hcmnext.workflows.time.fix_missing_punch" || r.WorkflowPlanDigest == "" || r.WorkflowAttempt <= 0 || r.WorkflowInstanceVersion <= 0 || (r.WorkflowNodeID != "commit_missing_punch_request" && r.WorkflowNodeID != "supervisor_approval" && r.WorkflowNodeID != "append_correction") {
		return status.Error(codes.Unavailable, "missing punch workflow receipt is incomplete")
	}
	return nil
}

func missingPunchError(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, clockservice.ErrMissingPunchForbidden), errors.Is(err, clockservice.ErrInvalidPrincipal):
		return status.Error(codes.PermissionDenied, "missing punch operation forbidden")
	case errors.Is(err, clockservice.ErrMissingPunchConflict), errors.Is(err, clockservice.ErrMissingPunchClosed):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, clockservice.ErrMissingPunchInvalid), errors.Is(err, clockservice.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, clockservice.ErrMissingPunchUnavailable), errors.Is(err, clockservice.ErrUnavailable), errors.Is(err, clockservice.ErrRetryLater):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return status.Error(codes.Internal, "missing punch operation failed")
	}
}

// HTTPHandler serves the canonical /v1/time/missing-punch projection.
func (s *MissingPunchServer) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/time/missing-punch/GetCorrectionContext" || r.URL.Path == "/v1/time/missing-punch/ListPendingCorrections" {
			s.readHTTPHandler().ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost || (r.URL.Path != "/v1/time/missing-punch/SubmitCorrection" && r.URL.Path != "/v1/time/missing-punch/ReviewCorrection") {
			writeAPIError(w, http.StatusNotFound, status.Error(codes.NotFound, "missing punch method not found"))
			return
		}
		if s == nil || s.app == nil {
			writeAPIError(w, http.StatusServiceUnavailable, status.Error(codes.Unavailable, "missing punch service is unavailable"))
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			writeAPIError(w, http.StatusRequestEntityTooLarge, status.Error(codes.ResourceExhausted, "request body exceeds 1 MiB"))
			return
		}
		var req, out interface{ protoreflect.ProtoMessage }
		if r.URL.Path == "/v1/time/missing-punch/SubmitCorrection" {
			req = &timev1.SubmitCorrectionRequest{}
		} else {
			req = &timev1.ReviewCorrectionRequest{}
		}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, req); err != nil {
			writeAPIError(w, http.StatusBadRequest, status.Error(codes.InvalidArgument, "invalid protobuf JSON"))
			return
		}
		if r.URL.Path == "/v1/time/missing-punch/SubmitCorrection" {
			out, err = s.SubmitCorrection(r.Context(), req.(*timev1.SubmitCorrectionRequest))
		} else {
			out, err = s.ReviewCorrection(r.Context(), req.(*timev1.ReviewCorrectionRequest))
		}
		if err != nil {
			writeAPIError(w, httpStatus(err), err)
			return
		}
		message, ok := out.(proto.Message)
		if !ok || message == nil || (reflect.ValueOf(message).Kind() == reflect.Ptr && reflect.ValueOf(message).IsNil()) {
			writeAPIError(w, http.StatusServiceUnavailable, status.Error(codes.Unavailable, "missing punch service returned no response"))
			return
		}
		payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(message)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, status.Error(codes.Internal, "encode response"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	})
}

var _ timev1.MissingPunchServiceServer = (*MissingPunchServer)(nil)
