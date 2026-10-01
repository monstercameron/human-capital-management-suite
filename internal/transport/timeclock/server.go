package timeclock

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server is the generated ClockDeviceService boundary.
type Server struct {
	timev1.UnimplementedClockDeviceServiceServer
	app Service
}

// New returns a server delegating all use cases to app.
func New(app Service) *Server { return &Server{app: app} }

// Register installs the service on a gRPC server.
func Register(s *grpc.Server, app Service) {
	if s != nil {
		timev1.RegisterClockDeviceServiceServer(s, New(app))
	}
}

func (s *Server) trusted(ctx context.Context) (*trust.Principal, error) {
	if s == nil || s.app == nil {
		return nil, clockservice.ErrUnavailable
	}
	return principal(ctx)
}

func (s *Server) CreateEnrollmentCode(ctx context.Context, in *timev1.CreateEnrollmentCodeRequest) (*timev1.CreateEnrollmentCodeResponse, error) {
	p, e := s.trusted(ctx)
	if e != nil {
		return nil, serviceError(e)
	}
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	r, e := s.app.CreateEnrollmentCode(ctx, p, clockservice.EnrollmentRequest{SiteID: in.GetSiteId(), ProfileID: in.GetProfileRef(), Timezone: in.GetTimezone(), TTL: time.Duration(in.GetTtlSeconds()) * time.Second})
	if e != nil {
		return nil, serviceError(e)
	}
	return &timev1.CreateEnrollmentCodeResponse{Code: r.Code, ExpiresAt: timestamppb.New(r.ExpiresAt)}, nil
}

func (s *Server) EnrollDevice(ctx context.Context, in *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error) {
	p, e := s.trusted(ctx)
	if e != nil {
		return nil, serviceError(e)
	}
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if len(in.GetPublicKey()) != ed25519.PublicKeySize || len(in.GetChallengeSignature()) != ed25519.SignatureSize {
		return nil, status.Error(codes.InvalidArgument, "Ed25519 enrollment proof is required")
	}
	deviceID := p.ClientID()
	if deviceID == "" {
		return nil, status.Error(codes.Unauthenticated, "enrolled device identity is required")
	}
	d, e := s.app.EnrollDevice(ctx, p, clockservice.DeviceEnrollmentRequest{Code: in.GetEnrollmentCode(), DeviceID: deviceID, PublicKey: ed25519.PublicKey(append([]byte(nil), in.GetPublicKey()...)), Signature: append([]byte(nil), in.GetChallengeSignature()...)})
	if e != nil {
		return nil, serviceError(e)
	}
	return &timev1.EnrollDeviceResponse{Device: device(d)}, nil
}

func (s *Server) RotateDeviceKey(ctx context.Context, in *timev1.RotateDeviceKeyRequest) (*timev1.RotateDeviceKeyResponse, error) {
	p, e := s.trusted(ctx)
	if e != nil {
		return nil, serviceError(e)
	}
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if len(in.GetNewPublicKey()) != ed25519.PublicKeySize || len(in.GetChallengeSignature()) != ed25519.SignatureSize {
		return nil, status.Error(codes.InvalidArgument, "Ed25519 rotation proof is required")
	}
	d, e := s.app.RotateDeviceKey(ctx, p, clockservice.DeviceKeyRotationRequest{DeviceID: in.GetDeviceId(), ExpectedRevision: int64(in.GetExpectedRevision()), PublicKey: ed25519.PublicKey(in.GetNewPublicKey()), Challenge: clockservice.RotationChallenge(string(p.Tenant()), in.GetDeviceId(), int64(in.GetExpectedRevision())), Signature: append([]byte(nil), in.GetChallengeSignature()...)})
	if e != nil {
		return nil, serviceError(e)
	}
	return &timev1.RotateDeviceKeyResponse{Device: device(d)}, nil
}

func (s *Server) RevokeDevice(ctx context.Context, in *timev1.RevokeDeviceRequest) (*timev1.RevokeDeviceResponse, error) {
	p, e := s.trusted(ctx)
	if e != nil {
		return nil, serviceError(e)
	}
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	d, e := s.app.RevokeDevice(ctx, p, clockservice.DeviceLifecycleRequest{DeviceID: in.GetDeviceId(), ExpectedRevision: int64(in.GetExpectedRevision()), Reason: in.GetReason()})
	if e != nil {
		return nil, serviceError(e)
	}
	return &timev1.RevokeDeviceResponse{Device: device(d)}, nil
}

func (s *Server) SyncRoster(ctx context.Context, in *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error) {
	p, e := s.trusted(ctx)
	if e != nil {
		return nil, serviceError(e)
	}
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if in.GetMaxResults() < 0 || in.GetMaxResults() > 1000 {
		return nil, status.Error(codes.InvalidArgument, "max_results must be between zero and 1000")
	}
	d, e := s.app.SyncRoster(ctx, p, in.GetDeviceId(), in.GetCursor())
	if e != nil {
		return nil, serviceError(e)
	}
	return &timev1.SyncRosterResponse{Snapshot: roster(d)}, nil
}

func (s *Server) IdentifyWorker(ctx context.Context, in *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error) {
	p, e := s.trusted(ctx)
	if e != nil {
		return nil, serviceError(e)
	}
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	m, e := identMethod(in.GetMethod())
	if e != nil {
		return nil, serviceError(e)
	}
	cred := in.GetPin()
	if cred == "" {
		cred = in.GetBadgeId()
	}
	if cred == "" {
		cred = in.GetQrKeyId()
	}
	if cred == "" {
		cred = in.GetSupervisorCredentialRef()
	}
	if cred == "" {
		return nil, status.Error(codes.InvalidArgument, "credential is required")
	}
	r, e := s.app.IdentifyDevice(ctx, p, IdentifyDeviceRequest{DeviceID: in.GetDeviceId(), Method: string(m), Credential: cred, OverrideReason: in.GetOverrideReason()})
	if e != nil {
		return nil, serviceError(e)
	}
	return &timev1.IdentifyWorkerResponse{PunchToken: r.Token.Value, ExpiresAt: timestamppb.New(r.Token.ExpiresAt)}, nil
}

func (s *Server) SubmitPunches(ctx context.Context, in *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error) {
	p, e := s.trusted(ctx)
	if e != nil {
		return nil, serviceError(e)
	}
	if in == nil || len(in.GetPunches()) == 0 || len(in.GetPunches()) > 200 {
		return nil, status.Error(codes.InvalidArgument, "punches must contain between one and 200 entries")
	}
	req := clockservice.BatchRequest{DeviceID: in.GetDeviceId(), Punches: make([]clockservice.BatchPunch, 0, len(in.GetPunches()))}
	for _, v := range in.GetPunches() {
		if v == nil {
			return nil, status.Error(codes.InvalidArgument, "punch cannot be nil")
		}
		at, e := validTimestamp(v.GetDeviceOccurredAt())
		if e != nil {
			return nil, serviceError(e)
		}
		kind, e := punchKind(v.GetEventType())
		if e != nil {
			return nil, serviceError(e)
		}
		worker := v.GetWorker().GetPunchToken()
		if worker == "" {
			worker = v.GetWorker().GetVerifierRef()
		}
		method := timesession.IdentDevice
		switch v.GetIdentificationMethod() {
		case timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN:
			method = timesession.IdentPIN
		case timev1.IdentificationMethod_IDENTIFICATION_METHOD_BADGE:
			method = timesession.IdentBadge
		case timev1.IdentificationMethod_IDENTIFICATION_METHOD_QR:
			method = timesession.IdentDevice
		}
		req.Punches = append(req.Punches, clockservice.BatchPunch{DeviceSequence: int64(v.GetDeviceSequence()), EventType: kind, WorkerCredentialRef: worker, AssignmentRef: "", Source: "DEVICE", Method: method, IdempotencyKey: fmt.Sprintf("%s:%d", in.GetDeviceId(), v.GetDeviceSequence()), JobRef: v.GetJobId(), CostCodeRef: v.GetCostCodeId(), DeviceOccurredAt: at, DeviceClockOffset: time.Duration(v.GetDeviceClockOffsetSeconds()) * time.Second, PhotoAttestationRef: v.GetPhotoArtifactRef()})
	}
	r, e := s.app.SubmitPunches(ctx, p, req)
	if e != nil {
		return nil, serviceError(e)
	}
	out := &timev1.SubmitPunchesResponse{HighestContiguousSequence: uint64(max64(r.HighestContiguous))}
	for _, v := range r.Receipts {
		st := timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_UNSPECIFIED
		switch v.Status {
		case clockservice.BatchAccepted:
			st = timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED
		case clockservice.BatchDuplicate:
			st = timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE
		case clockservice.BatchRejected:
			st = timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_REJECTED
		case clockservice.BatchHeld:
			st = timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_HELD_FOR_REVIEW
		}
		receipt := &timev1.PunchReceipt{DeviceSequence: uint64(v.DeviceSequence), Status: st, ReceiptId: v.ObservationID, RejectionDetail: v.Reason}
		if v.Original != nil {
			receipt.OriginalReceiptId = v.Original.ObservationID
		}
		out.Receipts = append(out.Receipts, receipt)
	}
	return out, nil
}

// GetWorkerStatus resolves a scoped token through the application facade.
func (s *Server) GetWorkerStatus(ctx context.Context, in *timev1.GetWorkerStatusRequest) (*timev1.GetWorkerStatusResponse, error) {
	p, e := s.trusted(ctx)
	if e != nil {
		return nil, serviceError(e)
	}
	if in == nil || in.GetDeviceId() == "" || in.GetPunchToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id and punch_token are required")
	}
	v, e := s.app.GetWorkerStatus(ctx, p, WorkerStatusRequest{DeviceID: in.GetDeviceId(), PunchToken: in.GetPunchToken()})
	if e != nil {
		return nil, serviceError(e)
	}
	return &timev1.GetWorkerStatusResponse{Status: &timev1.WorkerPunchStatus{WorkerId: v.WorkerID, DisplayName: v.DisplayName, CurrentJobId: v.CurrentJobID, ActiveShiftId: v.ActiveShiftID, AllowedNextEvents: append([]string(nil), v.AllowedNextEvents...)}}, nil
}

func (s *Server) Heartbeat(ctx context.Context, in *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error) {
	p, e := s.trusted(ctx)
	if e != nil {
		return nil, serviceError(e)
	}
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	power := in.GetPowerState().String()
	r, e := s.app.Heartbeat(ctx, p, clockservice.HeartbeatRequest{DeviceID: in.GetDeviceId(), AppVersion: in.GetAppVersion(), PowerState: power, QueueDepth: int(in.GetQueueDepth()), OldestUnsentAgeSeconds: int(in.GetOldestUnsentAgeSeconds()), BatteryPercent: int(in.GetBatteryPercent()), HasBatteryPercent: in.GetBatteryPercent() != 0, OffsetMillis: in.GetMeasuredClockOffsetSeconds() * 1000})
	if e != nil {
		return nil, serviceError(e)
	}
	action := timev1.HeartbeatAction_HEARTBEAT_ACTION_NONE
	if !r.Evaluation.VersionSupported {
		action = timev1.HeartbeatAction_HEARTBEAT_ACTION_UPGRADE_REQUIRED
	}
	if r.Device.State == "REVOKED" {
		action = timev1.HeartbeatAction_HEARTBEAT_ACTION_REVOKED
	}
	return &timev1.HeartbeatResponse{ServerTime: timestamppb.New(r.RecordedAt), Action: action}, nil
}

var _ timev1.ClockDeviceServiceServer = (*Server)(nil)
