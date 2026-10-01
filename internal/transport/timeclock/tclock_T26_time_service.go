package timeclock

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	timecardservice "github.com/monstercameron/human-capital-management-suite/internal/application/timecardservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TimeApplication is the single application port used by TimeService.  The
// transport deliberately has one call shape: composition code can route the
// operation to clockservice/timecardservice without making HTTP a second
// business-logic path.  The authenticated principal is always supplied by
// the server and the ScopeContext in the protobuf is never used as authority.
type TimeApplication interface {
	HandleTime(context.Context, *trust.Principal, TimeRequest) (TimeResult, error)
}

// TimeRequest is the transport-neutral projection of TimeService requests.
// Its fields are intentionally boring; authorization, tenant filtering,
// field masking, revision checks, idempotency and cursor validation remain in
// the application port.
type TimeRequest struct {
	Operation                                                                              string
	WorkerID, SessionID, TimecardID, ShiftID, SiteID, PunchID                              string
	JobID, CostCodeID, Timezone, SourceRef, Reason                                         string
	OccurredAt, StartsAt, EndsAt, OpenedAfter, OpenedBefore, StartingAfter, StartingBefore time.Time
	ExpectedRevision                                                                       uint64
	IdempotencyKey                                                                         string
	Status                                                                                 string
	Decision                                                                               string
	Answers                                                                                []TimeAnswer
	Evidence                                                                               []TimeEvidence
	EventTypes                                                                             []string
	PageSize                                                                               int32
	Cursor                                                                                 string
}

type TimeAnswer struct{ QuestionRef, Answer string }
type TimeEvidence struct{ EvidenceID, EvidenceKind, Digest string }

// TimeResult is the masked result returned by the application.  A result
// contains only the projection needed by its operation; the server never
// fetches a broader record to fill in a response.
type TimeResult struct {
	Session     *TimeSession
	Sessions    []TimeSession
	Timecard    *Timecard
	Timecards   []Timecard
	Shift       *TimeShift
	Shifts      []TimeShift
	Profile     *TimeProfile
	MissedPunch *TimeMissedPunch
	Events      []*timev1.ClockEvent
	NextCursor  string
}

type TimePunch struct {
	ID, EventType, JobID, CostCodeID, SourceRef, CorrectionOf, Reason string
	OccurredAt, RecordedAt                                            time.Time
}

type TimeSession struct {
	ID, WorkerID, Status, CurrentJobID, CurrentCostCodeID, Timezone string
	Events                                                          []TimePunch
	OpenedAt, ClosedAt                                              time.Time
	Revision                                                        uint64
	ETag                                                            string
}

type TimecardLine struct {
	Year, Month, Day                             int32
	CalendarRef, JobID, CostCodeID               string
	WorkedSeconds, BreakSeconds, OvertimeSeconds int64
	SourceSessionIDs                             []string
}

type Timecard struct {
	ID, WorkerID, Status, AttestedBy, ApprovedBy, ReopenReason, ETag string
	PeriodStart, PeriodEnd                                           time.Time
	Lines                                                            []TimecardLine
	AttestedAt, ApprovedAt                                           time.Time
	Revision                                                         uint64
}

type TimeShift struct {
	ID, SiteID, WorkerID, JobID, Status, Timezone, CancelReason, ETag string
	StartsAt, EndsAt                                                  time.Time
	Revision                                                          uint64
}

type TimeProfile struct {
	ID, WorkerID, CaptureMode, PayBasis, ExemptionStatus, WorkerCategory string
	Destination, OvertimeMethod, Template                                string
	ControlClasses                                                       []string
	Revision                                                             uint64
	ETag                                                                 string
}

type TimeMissedPunch struct {
	ID, WorkerID, SessionID, ClaimedEventType, Status, DecidedBy, DecisionReason, Reason, ETag string
	ClaimedAt                                                                                  time.Time
	Evidence                                                                                   []TimeEvidence
	Revision                                                                                   uint64
}

// TimeServer is the generated TimeService boundary.
type TimeServer struct {
	timev1.UnimplementedTimeServiceServer
	app TimeApplication
}

func NewTimeServer(app TimeApplication) *TimeServer { return &TimeServer{app: app} }

func RegisterTimeService(server *grpc.Server, app TimeApplication) {
	if server != nil {
		timev1.RegisterTimeServiceServer(server, NewTimeServer(app))
	}
}

// RegisterTime is the short form used by composition roots that group the
// time-clock services under one registrar.
func RegisterTime(server *grpc.Server, app TimeApplication) { RegisterTimeService(server, app) }

// TimeHTTPHandler returns the JSON projection without requiring callers to
// construct the generated server explicitly.
func TimeHTTPHandler(app TimeApplication) http.Handler { return NewTimeServer(app).HTTPHandler() }

func (s *TimeServer) trusted(ctx context.Context) (*trust.Principal, error) {
	if s == nil || s.app == nil {
		return nil, status.Error(codes.Unavailable, "time service is unavailable")
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, timeServiceError(err)
	}
	return p, nil
}

func (s *TimeServer) call(ctx context.Context, in TimeRequest) (TimeResult, error) {
	p, err := s.trusted(ctx)
	if err != nil {
		return TimeResult{}, err
	}
	out, err := s.app.HandleTime(ctx, p, in)
	if err != nil {
		return TimeResult{}, timeServiceError(err)
	}
	return out, nil
}

func requireTimeRequest(in proto.Message) error {
	if in == nil {
		return status.Error(codes.InvalidArgument, "request is required")
	}
	return nil
}

func requireTimeWrite(idempotency string) error {
	if strings.TrimSpace(idempotency) == "" {
		return status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	return nil
}

func requireTimeRevision(revision uint64) error {
	if revision == 0 {
		return status.Error(codes.InvalidArgument, "expected_revision is required")
	}
	return nil
}

func requireTimeTimestamp(ts interface{ CheckValid() error }, name string) (time.Time, error) {
	if ts == nil {
		return time.Time{}, status.Errorf(codes.InvalidArgument, "%s is required", name)
	}
	if err := ts.CheckValid(); err != nil {
		return time.Time{}, status.Errorf(codes.InvalidArgument, "%s is invalid", name)
	}
	return ts.(*timestamppb.Timestamp).AsTime(), nil
}

func pageSize(size int32) (int32, error) {
	if size == 0 {
		return 100, nil
	}
	if size < 1 || size > 1000 {
		return 0, status.Error(codes.InvalidArgument, "page_size must be between 1 and 1000")
	}
	return size, nil
}

func (s *TimeServer) ClockIn(ctx context.Context, in *timev1.ClockInRequest) (*timev1.ClockInResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	at, err := requireTimeTimestamp(in.GetOccurredAt(), "occurred_at")
	if err != nil {
		return nil, err
	}
	if err = requireTimeWrite(in.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "ClockIn", WorkerID: in.GetWorkerId(), JobID: in.GetJobId(), CostCodeID: in.GetCostCodeId(), Timezone: in.GetTimezone(), SourceRef: in.GetSourceRef(), OccurredAt: at, IdempotencyKey: in.GetIdempotencyKey()})
	if err != nil {
		return nil, err
	}
	return &timev1.ClockInResponse{Session: timeSession(r.Session)}, nil
}

func (s *TimeServer) StartBreak(ctx context.Context, in *timev1.StartBreakRequest) (*timev1.StartBreakResponse, error) {
	r, err := s.punchCommand(ctx, "StartBreak", in.GetSessionId(), in.GetExpectedRevision(), in.GetOccurredAt(), in.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &timev1.StartBreakResponse{Session: timeSession(r.Session)}, nil
}

func (s *TimeServer) EndBreak(ctx context.Context, in *timev1.EndBreakRequest) (*timev1.EndBreakResponse, error) {
	r, err := s.punchCommand(ctx, "EndBreak", in.GetSessionId(), in.GetExpectedRevision(), in.GetOccurredAt(), in.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &timev1.EndBreakResponse{Session: timeSession(r.Session)}, nil
}

func (s *TimeServer) ClockOut(ctx context.Context, in *timev1.ClockOutRequest) (*timev1.ClockOutResponse, error) {
	r, err := s.punchCommand(ctx, "ClockOut", in.GetSessionId(), in.GetExpectedRevision(), in.GetOccurredAt(), in.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &timev1.ClockOutResponse{Session: timeSession(r.Session)}, nil
}

func (s *TimeServer) punchCommand(ctx context.Context, operation, sessionID string, revision uint64, ts *timestamppb.Timestamp, key string) (TimeResult, error) {
	if ts == nil {
		return TimeResult{}, status.Error(codes.InvalidArgument, "occurred_at is required")
	}
	at, err := requireTimeTimestamp(ts, "occurred_at")
	if err != nil {
		return TimeResult{}, err
	}
	if sessionID == "" {
		return TimeResult{}, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if err := requireTimeRevision(revision); err != nil {
		return TimeResult{}, err
	}
	if err := requireTimeWrite(key); err != nil {
		return TimeResult{}, err
	}
	return s.callWith(ctx, TimeRequest{Operation: operation, SessionID: sessionID, ExpectedRevision: revision, OccurredAt: at, IdempotencyKey: key})
}

// callWith is used by the small shared command validator above; it preserves
// the receiver's application port while avoiding a second transport path.
func (s *TimeServer) callWith(ctx context.Context, in TimeRequest) (TimeResult, error) {
	p, err := s.trusted(ctx)
	if err != nil {
		return TimeResult{}, err
	}
	out, err := s.app.HandleTime(ctx, p, in)
	if err != nil {
		return TimeResult{}, timeServiceError(err)
	}
	return out, nil
}

func (s *TimeServer) TransferJob(ctx context.Context, in *timev1.TransferJobRequest) (*timev1.TransferJobResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	at, err := requireTimeTimestamp(in.GetOccurredAt(), "occurred_at")
	if err != nil {
		return nil, err
	}
	if in.GetSessionId() == "" || in.GetJobId() == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id and job_id are required")
	}
	if err = requireTimeRevision(in.GetExpectedRevision()); err != nil {
		return nil, err
	}
	if err = requireTimeWrite(in.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "TransferJob", SessionID: in.GetSessionId(), JobID: in.GetJobId(), CostCodeID: in.GetCostCodeId(), ExpectedRevision: in.GetExpectedRevision(), OccurredAt: at, IdempotencyKey: in.GetIdempotencyKey()})
	if err != nil {
		return nil, err
	}
	return &timev1.TransferJobResponse{Session: timeSession(r.Session)}, nil
}

func (s *TimeServer) GetCurrentSession(ctx context.Context, in *timev1.GetCurrentSessionRequest) (*timev1.GetCurrentSessionResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "GetCurrentSession", WorkerID: in.GetWorkerId()})
	if err != nil {
		return nil, err
	}
	return &timev1.GetCurrentSessionResponse{Session: timeSession(r.Session)}, nil
}

func (s *TimeServer) ListSessions(ctx context.Context, in *timev1.ListSessionsRequest) (*timev1.ListSessionsResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	size, err := pageSize(in.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "ListSessions", WorkerID: in.GetWorkerId(), Status: in.GetStatus().String(), OpenedAfter: timestampOrZero(in.GetOpenedAfter()), OpenedBefore: timestampOrZero(in.GetOpenedBefore()), PageSize: size, Cursor: in.GetPage().GetCursor()})
	if err != nil {
		return nil, err
	}
	out := &timev1.ListSessionsResponse{Page: &commonv1.PageResponse{NextCursor: r.NextCursor}}
	for i := range r.Sessions {
		out.Sessions = append(out.Sessions, timeSession(&r.Sessions[i]))
	}
	return out, nil
}

func (s *TimeServer) GetTimecard(ctx context.Context, in *timev1.GetTimecardRequest) (*timev1.GetTimecardResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	if in.GetTimecardId() == "" {
		return nil, status.Error(codes.InvalidArgument, "timecard_id is required")
	}
	r, err := s.call(ctx, TimeRequest{Operation: "GetTimecard", TimecardID: in.GetTimecardId()})
	if err != nil {
		return nil, err
	}
	return &timev1.GetTimecardResponse{Timecard: timecardProjection(r.Timecard)}, nil
}

func (s *TimeServer) ListTimecards(ctx context.Context, in *timev1.ListTimecardsRequest) (*timev1.ListTimecardsResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	size, err := pageSize(in.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "ListTimecards", WorkerID: in.GetWorkerId(), Status: in.GetStatus().String(), PageSize: size, Cursor: in.GetPage().GetCursor()})
	if err != nil {
		return nil, err
	}
	out := &timev1.ListTimecardsResponse{Page: &commonv1.PageResponse{NextCursor: r.NextCursor}}
	for i := range r.Timecards {
		out.Timecards = append(out.Timecards, timecardProjection(&r.Timecards[i]))
	}
	return out, nil
}

func (s *TimeServer) SubmitTimecard(ctx context.Context, in *timev1.SubmitTimecardRequest) (*timev1.SubmitTimecardResponse, error) {
	r, err := s.timecardCommand(ctx, "SubmitTimecard", in.GetTimecardId(), in.GetExpectedRevision(), in.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &timev1.SubmitTimecardResponse{Timecard: timecardProjection(r.Timecard)}, nil
}

func (s *TimeServer) AttestTimecard(ctx context.Context, in *timev1.AttestTimecardRequest) (*timev1.AttestTimecardResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	if err := requireTimeRevision(in.GetExpectedRevision()); err != nil {
		return nil, err
	}
	if err := requireTimeWrite(in.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	answers := make([]TimeAnswer, 0, len(in.GetAnswers()))
	for _, a := range in.GetAnswers() {
		if a == nil || a.GetQuestionRef() == "" {
			return nil, status.Error(codes.InvalidArgument, "answers must contain question_ref")
		}
		answers = append(answers, TimeAnswer{QuestionRef: a.GetQuestionRef(), Answer: a.GetAnswer()})
	}
	r, err := s.call(ctx, TimeRequest{Operation: "AttestTimecard", TimecardID: in.GetTimecardId(), ExpectedRevision: in.GetExpectedRevision(), IdempotencyKey: in.GetIdempotencyKey(), Answers: answers})
	if err != nil {
		return nil, err
	}
	return &timev1.AttestTimecardResponse{Timecard: timecardProjection(r.Timecard)}, nil
}

func (s *TimeServer) ApproveTimecard(ctx context.Context, in *timev1.ApproveTimecardRequest) (*timev1.ApproveTimecardResponse, error) {
	r, err := s.timecardCommand(ctx, "ApproveTimecard", in.GetTimecardId(), in.GetExpectedRevision(), in.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &timev1.ApproveTimecardResponse{Timecard: timecardProjection(r.Timecard)}, nil
}

func (s *TimeServer) ReopenTimecard(ctx context.Context, in *timev1.ReopenTimecardRequest) (*timev1.ReopenTimecardResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	if in.GetReason() == "" {
		return nil, status.Error(codes.InvalidArgument, "reason is required")
	}
	if err := requireTimeRevision(in.GetExpectedRevision()); err != nil {
		return nil, err
	}
	if err := requireTimeWrite(in.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "ReopenTimecard", TimecardID: in.GetTimecardId(), Reason: in.GetReason(), ExpectedRevision: in.GetExpectedRevision(), IdempotencyKey: in.GetIdempotencyKey()})
	if err != nil {
		return nil, err
	}
	return &timev1.ReopenTimecardResponse{Timecard: timecardProjection(r.Timecard)}, nil
}

func (s *TimeServer) timecardCommand(ctx context.Context, operation, id string, revision uint64, key string) (TimeResult, error) {
	if id == "" {
		return TimeResult{}, status.Error(codes.InvalidArgument, "timecard_id is required")
	}
	if err := requireTimeRevision(revision); err != nil {
		return TimeResult{}, err
	}
	if err := requireTimeWrite(key); err != nil {
		return TimeResult{}, err
	}
	return s.call(ctx, TimeRequest{Operation: operation, TimecardID: id, ExpectedRevision: revision, IdempotencyKey: key})
}

func (s *TimeServer) CorrectPunch(ctx context.Context, in *timev1.CorrectPunchRequest) (*timev1.CorrectPunchResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	at, err := requireTimeTimestamp(in.GetCorrectedOccurredAt(), "corrected_occurred_at")
	if err != nil {
		return nil, err
	}
	if in.GetSessionId() == "" || in.GetPunchId() == "" || in.GetReason() == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id, punch_id and reason are required")
	}
	if err = requireTimeRevision(in.GetExpectedRevision()); err != nil {
		return nil, err
	}
	if err = requireTimeWrite(in.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "CorrectPunch", SessionID: in.GetSessionId(), PunchID: in.GetPunchId(), Status: in.GetCorrectedEventType().String(), Reason: in.GetReason(), OccurredAt: at, ExpectedRevision: in.GetExpectedRevision(), IdempotencyKey: in.GetIdempotencyKey(), Evidence: evidence(in.GetEvidence())})
	if err != nil {
		return nil, err
	}
	return &timev1.CorrectPunchResponse{Session: timeSession(r.Session)}, nil
}

func (s *TimeServer) RequestMissedPunch(ctx context.Context, in *timev1.RequestMissedPunchRequest) (*timev1.RequestMissedPunchResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	at, err := requireTimeTimestamp(in.GetClaimedOccurredAt(), "claimed_occurred_at")
	if err != nil {
		return nil, err
	}
	if in.GetWorkerId() == "" || in.GetSessionId() == "" || in.GetReason() == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id, session_id and reason are required")
	}
	if err = requireTimeWrite(in.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "RequestMissedPunch", WorkerID: in.GetWorkerId(), SessionID: in.GetSessionId(), Status: in.GetClaimedEventType().String(), Reason: in.GetReason(), OccurredAt: at, IdempotencyKey: in.GetIdempotencyKey(), Evidence: evidence(in.GetEvidence())})
	if err != nil {
		return nil, err
	}
	return &timev1.RequestMissedPunchResponse{Request: missedPunch(r.MissedPunch)}, nil
}

func (s *TimeServer) DecideMissedPunch(ctx context.Context, in *timev1.DecideMissedPunchRequest) (*timev1.DecideMissedPunchResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	if in.GetRequestId() == "" || in.GetReason() == "" {
		return nil, status.Error(codes.InvalidArgument, "request_id and reason are required")
	}
	if err := requireTimeRevision(in.GetExpectedRevision()); err != nil {
		return nil, err
	}
	if err := requireTimeWrite(in.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	decision := in.GetDecision().String()
	if in.GetDecision() != timev1.MissedPunchRequestStatus_MISSED_PUNCH_REQUEST_STATUS_APPROVED && in.GetDecision() != timev1.MissedPunchRequestStatus_MISSED_PUNCH_REQUEST_STATUS_REJECTED {
		return nil, status.Error(codes.InvalidArgument, "decision must be approved or rejected")
	}
	r, err := s.call(ctx, TimeRequest{Operation: "DecideMissedPunch", TimecardID: in.GetRequestId(), Status: decision, Decision: decision, Reason: in.GetReason(), ExpectedRevision: in.GetExpectedRevision(), IdempotencyKey: in.GetIdempotencyKey()})
	if err != nil {
		return nil, err
	}
	return &timev1.DecideMissedPunchResponse{Request: missedPunch(r.MissedPunch)}, nil
}

func (s *TimeServer) CreateShift(ctx context.Context, in *timev1.CreateShiftRequest) (*timev1.CreateShiftResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	start, err := requireTimeTimestamp(in.GetStartsAt(), "starts_at")
	if err != nil {
		return nil, err
	}
	end, err := requireTimeTimestamp(in.GetEndsAt(), "ends_at")
	if err != nil {
		return nil, err
	}
	if in.GetSiteId() == "" || in.GetWorkerId() == "" || in.GetJobId() == "" || in.GetTimezone() == "" {
		return nil, status.Error(codes.InvalidArgument, "site_id, worker_id, job_id and timezone are required")
	}
	if err = requireTimeWrite(in.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "CreateShift", SiteID: in.GetSiteId(), WorkerID: in.GetWorkerId(), JobID: in.GetJobId(), Timezone: in.GetTimezone(), StartsAt: start, EndsAt: end, IdempotencyKey: in.GetIdempotencyKey()})
	if err != nil {
		return nil, err
	}
	return &timev1.CreateShiftResponse{Shift: shift(r.Shift)}, nil
}

func (s *TimeServer) PublishShift(ctx context.Context, in *timev1.PublishShiftRequest) (*timev1.PublishShiftResponse, error) {
	r, err := s.shiftCommand(ctx, "PublishShift", in.GetShiftId(), in.GetExpectedRevision(), in.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &timev1.PublishShiftResponse{Shift: shift(r.Shift)}, nil
}
func (s *TimeServer) CancelShift(ctx context.Context, in *timev1.CancelShiftRequest) (*timev1.CancelShiftResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	if in.GetReason() == "" {
		return nil, status.Error(codes.InvalidArgument, "reason is required")
	}
	r, err := s.shiftCommandWithReason(ctx, "CancelShift", in.GetShiftId(), in.GetExpectedRevision(), in.GetReason(), in.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &timev1.CancelShiftResponse{Shift: shift(r.Shift)}, nil
}

func (s *TimeServer) shiftCommand(ctx context.Context, operation, id string, revision uint64, key string) (TimeResult, error) {
	return s.shiftCommandWithReason(ctx, operation, id, revision, "", key)
}
func (s *TimeServer) shiftCommandWithReason(ctx context.Context, operation, id string, revision uint64, reason, key string) (TimeResult, error) {
	if id == "" {
		return TimeResult{}, status.Error(codes.InvalidArgument, "shift_id is required")
	}
	if err := requireTimeRevision(revision); err != nil {
		return TimeResult{}, err
	}
	if err := requireTimeWrite(key); err != nil {
		return TimeResult{}, err
	}
	return s.call(ctx, TimeRequest{Operation: operation, ShiftID: id, ExpectedRevision: revision, Reason: reason, IdempotencyKey: key})
}

func (s *TimeServer) ListShifts(ctx context.Context, in *timev1.ListShiftsRequest) (*timev1.ListShiftsResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	size, err := pageSize(in.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "ListShifts", SiteID: in.GetSiteId(), WorkerID: in.GetWorkerId(), Status: in.GetStatus().String(), StartingAfter: timestampOrZero(in.GetStartingAfter()), StartingBefore: timestampOrZero(in.GetStartingBefore()), PageSize: size, Cursor: in.GetPage().GetCursor()})
	if err != nil {
		return nil, err
	}
	out := &timev1.ListShiftsResponse{Page: &commonv1.PageResponse{NextCursor: r.NextCursor}}
	for i := range r.Shifts {
		out.Shifts = append(out.Shifts, shift(&r.Shifts[i]))
	}
	return out, nil
}

func (s *TimeServer) GetTimeProfile(ctx context.Context, in *timev1.GetTimeProfileRequest) (*timev1.GetTimeProfileResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "GetTimeProfile", WorkerID: in.GetWorkerId()})
	if err != nil {
		return nil, err
	}
	return &timev1.GetTimeProfileResponse{Profile: profile(r.Profile)}, nil
}
func (s *TimeServer) AssignTimeProfile(ctx context.Context, in *timev1.AssignTimeProfileRequest) (*timev1.AssignTimeProfileResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	if in.GetWorkerId() == "" || in.GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id and idempotency_key are required")
	}
	r, err := s.call(ctx, TimeRequest{Operation: "AssignTimeProfile", WorkerID: in.GetWorkerId(), Status: in.GetCaptureMode().String(), Reason: in.GetPayBasis().String(), SourceRef: in.GetTemplate().String(), ExpectedRevision: in.GetExpectedRevision(), IdempotencyKey: in.GetIdempotencyKey()})
	if err != nil {
		return nil, err
	}
	return &timev1.AssignTimeProfileResponse{Profile: profile(r.Profile)}, nil
}
func (s *TimeServer) ListTimeEvents(ctx context.Context, in *timev1.ListTimeEventsRequest) (*timev1.ListTimeEventsResponse, error) {
	if err := requireTimeRequest(in); err != nil {
		return nil, err
	}
	size, err := pageSize(in.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	r, err := s.call(ctx, TimeRequest{Operation: "ListTimeEvents", EventTypes: append([]string(nil), in.GetEventTypes()...), PageSize: size, Cursor: in.GetPage().GetCursor()})
	if err != nil {
		return nil, err
	}
	return &timev1.ListTimeEventsResponse{Events: r.Events, Page: &commonv1.PageResponse{NextCursor: r.NextCursor}}, nil
}

func timestampOrZero(ts *timestamppb.Timestamp) time.Time {
	if ts == nil || ts.CheckValid() != nil {
		return time.Time{}
	}
	return ts.AsTime()
}
func evidence(in []*commonv1.EvidenceRef) []TimeEvidence {
	out := make([]TimeEvidence, 0, len(in))
	for _, e := range in {
		if e != nil {
			out = append(out, TimeEvidence{EvidenceID: e.GetEvidenceId(), EvidenceKind: e.GetEvidenceKind(), Digest: e.GetDigest()})
		}
	}
	return out
}

func timeSession(in *TimeSession) *timev1.ClockSession {
	if in == nil {
		return nil
	}
	out := &timev1.ClockSession{SessionId: in.ID, WorkerId: in.WorkerID, Status: sessionStatus(in.Status), CurrentJobId: in.CurrentJobID, CurrentCostCodeId: in.CurrentCostCodeID, Timezone: in.Timezone, OpenedAt: timestamp(in.OpenedAt), ClosedAt: timestamp(in.ClosedAt), Revision: in.Revision, Etag: in.ETag}
	for _, p := range in.Events {
		out.Events = append(out.Events, &timev1.PunchEvent{PunchId: p.ID, EventType: punchEventType(p.EventType), OccurredAt: timestamp(p.OccurredAt), RecordedAt: timestamp(p.RecordedAt), JobId: p.JobID, CostCodeId: p.CostCodeID, SourceRef: p.SourceRef, CorrectionOfPunchId: p.CorrectionOf, Reason: p.Reason})
	}
	return out
}
func timecardProjection(in *Timecard) *timev1.Timecard {
	if in == nil {
		return nil
	}
	out := &timev1.Timecard{TimecardId: in.ID, WorkerId: in.WorkerID, Status: timecardStatus(in.Status), PeriodStart: localDate(in.PeriodStart), PeriodEnd: localDate(in.PeriodEnd), AttestedBy: in.AttestedBy, AttestedAt: timestamp(in.AttestedAt), ApprovedBy: in.ApprovedBy, ApprovedAt: timestamp(in.ApprovedAt), ReopenReason: in.ReopenReason, Revision: in.Revision, Etag: in.ETag}
	for _, l := range in.Lines {
		out.Lines = append(out.Lines, &timev1.TimecardLine{WorkDate: &commonv1.LocalDate{Year: l.Year, Month: l.Month, Day: l.Day, CalendarRef: l.CalendarRef}, JobId: l.JobID, CostCodeId: l.CostCodeID, WorkedSeconds: l.WorkedSeconds, BreakSeconds: l.BreakSeconds, OvertimeSeconds: l.OvertimeSeconds, SourceSessionIds: append([]string(nil), l.SourceSessionIDs...)})
	}
	return out
}
func shift(in *TimeShift) *timev1.Shift {
	if in == nil {
		return nil
	}
	return &timev1.Shift{ShiftId: in.ID, SiteId: in.SiteID, WorkerId: in.WorkerID, JobId: in.JobID, Status: shiftStatus(in.Status), StartsAt: timestamp(in.StartsAt), EndsAt: timestamp(in.EndsAt), Timezone: in.Timezone, CancelReason: in.CancelReason, Revision: in.Revision, Etag: in.ETag}
}
func profile(in *TimeProfile) *timev1.TimeProfile {
	if in == nil {
		return nil
	}
	controls := make([]timev1.ControlClass, 0, len(in.ControlClasses))
	for _, v := range in.ControlClasses {
		controls = append(controls, controlClass(v))
	}
	return &timev1.TimeProfile{ProfileId: in.ID, WorkerId: in.WorkerID, CaptureMode: captureMode(in.CaptureMode), PayBasis: payBasis(in.PayBasis), ExemptionStatus: exemptionStatus(in.ExemptionStatus), WorkerCategory: workerCategory(in.WorkerCategory), Destination: destination(in.Destination), OvertimeMethod: overtimeMethod(in.OvertimeMethod), Template: timeTemplate(in.Template), ControlClasses: controls, Revision: in.Revision, Etag: in.ETag}
}
func missedPunch(in *TimeMissedPunch) *timev1.MissedPunchRequest {
	if in == nil {
		return nil
	}
	out := &timev1.MissedPunchRequest{RequestId: in.ID, WorkerId: in.WorkerID, SessionId: in.SessionID, ClaimedEventType: punchEventType(in.ClaimedEventType), ClaimedOccurredAt: timestamp(in.ClaimedAt), Reason: in.Reason, Status: missedStatus(in.Status), DecidedBy: in.DecidedBy, DecisionReason: in.DecisionReason, Revision: in.Revision, Etag: in.ETag}
	for _, e := range in.Evidence {
		out.Evidence = append(out.Evidence, &commonv1.EvidenceRef{EvidenceId: e.EvidenceID, EvidenceKind: e.EvidenceKind, Digest: e.Digest})
	}
	return out
}
func timestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
func localDate(t time.Time) *commonv1.LocalDate {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &commonv1.LocalDate{Year: int32(u.Year()), Month: int32(u.Month()), Day: int32(u.Day())}
}
func token(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	v = strings.TrimPrefix(v, "SESSION_STATUS_")
	v = strings.TrimPrefix(v, "TIMECARD_STATUS_")
	v = strings.TrimPrefix(v, "SHIFT_STATUS_")
	v = strings.TrimPrefix(v, "PUNCH_EVENT_TYPE_")
	v = strings.TrimPrefix(v, "MISSED_PUNCH_REQUEST_STATUS_")
	v = strings.TrimPrefix(v, "CAPTURE_MODE_")
	v = strings.TrimPrefix(v, "PAY_BASIS_")
	v = strings.TrimPrefix(v, "EXEMPTION_STATUS_")
	v = strings.TrimPrefix(v, "WORKER_CATEGORY_")
	v = strings.TrimPrefix(v, "TIME_DESTINATION_")
	v = strings.TrimPrefix(v, "OVERTIME_METHOD_")
	v = strings.TrimPrefix(v, "TIME_TEMPLATE_")
	return v
}
func sessionStatus(v string) timev1.SessionStatus {
	return timev1.SessionStatus(timev1.SessionStatus_value["SESSION_STATUS_"+token(v)])
}
func punchEventType(v string) timev1.PunchEventType {
	return timev1.PunchEventType(timev1.PunchEventType_value["PUNCH_EVENT_TYPE_"+token(v)])
}
func timecardStatus(v string) timev1.TimecardStatus {
	return timev1.TimecardStatus(timev1.TimecardStatus_value["TIMECARD_STATUS_"+token(v)])
}
func shiftStatus(v string) timev1.ShiftStatus {
	return timev1.ShiftStatus(timev1.ShiftStatus_value["SHIFT_STATUS_"+token(v)])
}
func missedStatus(v string) timev1.MissedPunchRequestStatus {
	return timev1.MissedPunchRequestStatus(timev1.MissedPunchRequestStatus_value["MISSED_PUNCH_REQUEST_STATUS_"+token(v)])
}
func captureMode(v string) timev1.CaptureMode {
	return timev1.CaptureMode(timev1.CaptureMode_value["CAPTURE_MODE_"+token(v)])
}
func payBasis(v string) timev1.PayBasis {
	return timev1.PayBasis(timev1.PayBasis_value["PAY_BASIS_"+token(v)])
}
func exemptionStatus(v string) timev1.ExemptionStatus {
	return timev1.ExemptionStatus(timev1.ExemptionStatus_value["EXEMPTION_STATUS_"+token(v)])
}
func workerCategory(v string) timev1.WorkerCategory {
	return timev1.WorkerCategory(timev1.WorkerCategory_value["WORKER_CATEGORY_"+token(v)])
}
func destination(v string) timev1.TimeDestination {
	return timev1.TimeDestination(timev1.TimeDestination_value["TIME_DESTINATION_"+token(v)])
}
func overtimeMethod(v string) timev1.OvertimeMethod {
	return timev1.OvertimeMethod(timev1.OvertimeMethod_value["OVERTIME_METHOD_"+token(v)])
}
func timeTemplate(v string) timev1.TimeTemplate {
	return timev1.TimeTemplate(timev1.TimeTemplate_value["TIME_TEMPLATE_"+token(v)])
}
func controlClass(v string) timev1.ControlClass {
	return timev1.ControlClass(timev1.ControlClass_value["CONTROL_CLASS_"+token(v)])
}

func timeServiceError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, timecardservice.ErrInvalidPrincipal), errors.Is(err, clockservice.ErrInvalidPrincipal):
		return status.Error(codes.Unauthenticated, "trusted principal required")
	case errors.Is(err, timecardservice.ErrInvalidRequest), errors.Is(err, clockservice.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, timecardservice.ErrForbidden):
		return status.Error(codes.PermissionDenied, "time operation forbidden")
	case errors.Is(err, timecardservice.ErrNotFound):
		return status.Error(codes.NotFound, "time record not found")
	case errors.Is(err, timecardservice.ErrRevisionConflict):
		return status.Error(codes.Aborted, "revision conflict")
	case errors.Is(err, timecardservice.ErrIdempotencyConflict), errors.Is(err, clockservice.ErrPunchIdempotencyConflict):
		return status.Error(codes.AlreadyExists, "idempotency key conflict")
	case errors.Is(err, timecardservice.ErrUnavailable), errors.Is(err, clockservice.ErrUnavailable), errors.Is(err, clockservice.ErrRetryLater):
		return status.Error(codes.Unavailable, "time service unavailable")
	case errors.Is(err, timecardservice.ErrUnresolvedException):
		return status.Error(codes.FailedPrecondition, "unresolved exception blocks operation")
	default:
		return status.Error(codes.Internal, "time operation failed")
	}
}

// HTTPHandler is the JSON projection of the same TimeServer methods.  It
// accepts the canonical /v1/time/<Method> path plus the Connect-style service
// path so discovery and external clients can use one operation contract.
func (s *TimeServer) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, status.Error(codes.Unimplemented, "method not allowed"))
			return
		}
		method := timeHTTPMethod(r.URL.Path)
		if method == "" {
			writeAPIError(w, http.StatusNotFound, status.Error(codes.NotFound, "time method not found"))
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			writeAPIError(w, http.StatusRequestEntityTooLarge, status.Error(codes.ResourceExhausted, "request body exceeds 1 MiB"))
			return
		}
		in := timeRequestMessage(method)
		if in == nil {
			writeAPIError(w, http.StatusNotFound, status.Error(codes.NotFound, "time method not found"))
			return
		}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, in); err != nil {
			writeAPIError(w, http.StatusBadRequest, status.Error(codes.InvalidArgument, "invalid protobuf JSON"))
			return
		}
		out, err := s.invokeHTTP(r.Context(), method, in)
		if err != nil {
			writeAPIError(w, httpStatus(err), err)
			return
		}
		payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(out)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, status.Error(codes.Internal, "encode response"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	})
}

func timeHTTPMethod(path string) string {
	for _, prefix := range []string{"/v1/time/", "/hcmnext.time.v1.TimeService/"} {
		if strings.HasPrefix(path, prefix) {
			method := strings.TrimPrefix(path, prefix)
			if !strings.Contains(method, "/") {
				return method
			}
		}
	}
	return ""
}
func timeRequestMessage(method string) proto.Message {
	switch method {
	case "ClockIn":
		return &timev1.ClockInRequest{}
	case "StartBreak":
		return &timev1.StartBreakRequest{}
	case "EndBreak":
		return &timev1.EndBreakRequest{}
	case "TransferJob":
		return &timev1.TransferJobRequest{}
	case "ClockOut":
		return &timev1.ClockOutRequest{}
	case "GetCurrentSession":
		return &timev1.GetCurrentSessionRequest{}
	case "ListSessions":
		return &timev1.ListSessionsRequest{}
	case "GetTimecard":
		return &timev1.GetTimecardRequest{}
	case "ListTimecards":
		return &timev1.ListTimecardsRequest{}
	case "SubmitTimecard":
		return &timev1.SubmitTimecardRequest{}
	case "AttestTimecard":
		return &timev1.AttestTimecardRequest{}
	case "ApproveTimecard":
		return &timev1.ApproveTimecardRequest{}
	case "ReopenTimecard":
		return &timev1.ReopenTimecardRequest{}
	case "CorrectPunch":
		return &timev1.CorrectPunchRequest{}
	case "RequestMissedPunch":
		return &timev1.RequestMissedPunchRequest{}
	case "DecideMissedPunch":
		return &timev1.DecideMissedPunchRequest{}
	case "CreateShift":
		return &timev1.CreateShiftRequest{}
	case "PublishShift":
		return &timev1.PublishShiftRequest{}
	case "CancelShift":
		return &timev1.CancelShiftRequest{}
	case "ListShifts":
		return &timev1.ListShiftsRequest{}
	case "GetTimeProfile":
		return &timev1.GetTimeProfileRequest{}
	case "AssignTimeProfile":
		return &timev1.AssignTimeProfileRequest{}
	case "ListTimeEvents":
		return &timev1.ListTimeEventsRequest{}
	default:
		return nil
	}
}
func (s *TimeServer) invokeHTTP(ctx context.Context, method string, in proto.Message) (proto.Message, error) {
	switch v := in.(type) {
	case *timev1.ClockInRequest:
		return s.ClockIn(ctx, v)
	case *timev1.StartBreakRequest:
		return s.StartBreak(ctx, v)
	case *timev1.EndBreakRequest:
		return s.EndBreak(ctx, v)
	case *timev1.TransferJobRequest:
		return s.TransferJob(ctx, v)
	case *timev1.ClockOutRequest:
		return s.ClockOut(ctx, v)
	case *timev1.GetCurrentSessionRequest:
		return s.GetCurrentSession(ctx, v)
	case *timev1.ListSessionsRequest:
		return s.ListSessions(ctx, v)
	case *timev1.GetTimecardRequest:
		return s.GetTimecard(ctx, v)
	case *timev1.ListTimecardsRequest:
		return s.ListTimecards(ctx, v)
	case *timev1.SubmitTimecardRequest:
		return s.SubmitTimecard(ctx, v)
	case *timev1.AttestTimecardRequest:
		return s.AttestTimecard(ctx, v)
	case *timev1.ApproveTimecardRequest:
		return s.ApproveTimecard(ctx, v)
	case *timev1.ReopenTimecardRequest:
		return s.ReopenTimecard(ctx, v)
	case *timev1.CorrectPunchRequest:
		return s.CorrectPunch(ctx, v)
	case *timev1.RequestMissedPunchRequest:
		return s.RequestMissedPunch(ctx, v)
	case *timev1.DecideMissedPunchRequest:
		return s.DecideMissedPunch(ctx, v)
	case *timev1.CreateShiftRequest:
		return s.CreateShift(ctx, v)
	case *timev1.PublishShiftRequest:
		return s.PublishShift(ctx, v)
	case *timev1.CancelShiftRequest:
		return s.CancelShift(ctx, v)
	case *timev1.ListShiftsRequest:
		return s.ListShifts(ctx, v)
	case *timev1.GetTimeProfileRequest:
		return s.GetTimeProfile(ctx, v)
	case *timev1.AssignTimeProfileRequest:
		return s.AssignTimeProfile(ctx, v)
	case *timev1.ListTimeEventsRequest:
		return s.ListTimeEvents(ctx, v)
	default:
		return nil, status.Errorf(codes.NotFound, "time method %s not found", method)
	}
}

var _ timev1.TimeServiceServer = (*TimeServer)(nil)
