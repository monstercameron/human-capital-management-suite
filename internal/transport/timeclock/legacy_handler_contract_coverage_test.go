package timeclock

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type legacyContractApp struct {
	request   TimeRequest
	principal *trust.Principal
	result    TimeResult
	err       error
}

func (a *legacyContractApp) HandleTime(_ context.Context, p *trust.Principal, request TimeRequest) (TimeResult, error) {
	a.principal, a.request = p, request
	return a.result, a.err
}

func TestTimeServerLegacyHandlersMapAuthorizedRequestsAndTypedResults(t *testing.T) {
	at := timestamppb.New(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))
	principal := testPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), principal)
	base := TimeResult{
		Session:     &TimeSession{ID: "session-1", WorkerID: "worker-1", Status: "ON_BREAK", Revision: 4, OpenedAt: at.AsTime()},
		Sessions:    []TimeSession{{ID: "session-1", WorkerID: "worker-1", Status: "ON_BREAK", Revision: 4}},
		Timecard:    &Timecard{ID: "card-1", WorkerID: "worker-1", Status: "SUBMITTED", Revision: 3},
		Timecards:   []Timecard{{ID: "card-1", WorkerID: "worker-1", Status: "SUBMITTED", Revision: 3}},
		Shift:       &TimeShift{ID: "shift-1", SiteID: "site-1", WorkerID: "worker-1", Status: "PUBLISHED", Revision: 2},
		Shifts:      []TimeShift{{ID: "shift-1", SiteID: "site-1", WorkerID: "worker-1", Status: "PUBLISHED", Revision: 2}},
		Profile:     &TimeProfile{ID: "profile-1", WorkerID: "worker-1", CaptureMode: "CLOCK", PayBasis: "HOURLY", Revision: 5},
		MissedPunch: &TimeMissedPunch{ID: "missed-1", WorkerID: "worker-1", SessionID: "session-1", Status: "PENDING", Revision: 2},
		NextCursor:  "next-page",
	}
	tests := []struct {
		name      string
		operation string
		call      func(*TimeServer) error
		verify    func(TimeRequest)
	}{
		{"break start", "StartBreak", func(s *TimeServer) error {
			_, e := s.StartBreak(ctx, &timev1.StartBreakRequest{SessionId: "session-1", ExpectedRevision: 3, OccurredAt: at, IdempotencyKey: "break-start"})
			return e
		}, func(r TimeRequest) {
			if r.SessionID != "session-1" || r.ExpectedRevision != 3 || r.IdempotencyKey != "break-start" || !r.OccurredAt.Equal(at.AsTime()) {
				t.Fatalf("break start request=%+v", r)
			}
		}},
		{"break end", "EndBreak", func(s *TimeServer) error {
			_, e := s.EndBreak(ctx, &timev1.EndBreakRequest{SessionId: "session-1", ExpectedRevision: 4, OccurredAt: at, IdempotencyKey: "break-end"})
			return e
		}, func(r TimeRequest) {
			if r.ExpectedRevision != 4 || r.IdempotencyKey != "break-end" {
				t.Fatalf("break end request=%+v", r)
			}
		}},
		{"clock out", "ClockOut", func(s *TimeServer) error {
			_, e := s.ClockOut(ctx, &timev1.ClockOutRequest{SessionId: "session-1", ExpectedRevision: 5, OccurredAt: at, IdempotencyKey: "clock-out"})
			return e
		}, func(r TimeRequest) {
			if r.ExpectedRevision != 5 || r.IdempotencyKey != "clock-out" {
				t.Fatalf("clock out request=%+v", r)
			}
		}},
		{"transfer", "TransferJob", func(s *TimeServer) error {
			_, e := s.TransferJob(ctx, &timev1.TransferJobRequest{SessionId: "session-1", JobId: "job-2", ExpectedRevision: 4, OccurredAt: at, IdempotencyKey: "transfer"})
			return e
		}, func(r TimeRequest) {
			if r.SessionID != "session-1" || r.JobID != "job-2" {
				t.Fatalf("transfer request=%+v", r)
			}
		}},
		{"current session", "GetCurrentSession", func(s *TimeServer) error {
			out, e := s.GetCurrentSession(ctx, &timev1.GetCurrentSessionRequest{WorkerId: "worker-1"})
			if e == nil && out.GetSession().GetSessionId() != "session-1" {
				t.Fatalf("session=%v", out.GetSession())
			}
			return e
		}, func(r TimeRequest) {
			if r.WorkerID != "worker-1" {
				t.Fatalf("current session request=%+v", r)
			}
		}},
		{"list sessions", "ListSessions", func(s *TimeServer) error {
			out, e := s.ListSessions(ctx, &timev1.ListSessionsRequest{WorkerId: "worker-1", Page: &commonv1.PageRequest{PageSize: 25, Cursor: "cursor"}})
			if e == nil && (len(out.GetSessions()) != 1 || out.GetPage().GetNextCursor() != "next-page") {
				t.Fatalf("sessions=%v", out)
			}
			return e
		}, func(r TimeRequest) {
			if r.PageSize != 25 || r.Cursor != "cursor" || r.WorkerID != "worker-1" {
				t.Fatalf("list sessions request=%+v", r)
			}
		}},
		{"get timecard", "GetTimecard", func(s *TimeServer) error {
			out, e := s.GetTimecard(ctx, &timev1.GetTimecardRequest{TimecardId: "card-1"})
			if e == nil && out.GetTimecard().GetTimecardId() != "card-1" {
				t.Fatalf("timecard=%v", out)
			}
			return e
		}, func(r TimeRequest) {
			if r.TimecardID != "card-1" {
				t.Fatalf("get timecard request=%+v", r)
			}
		}},
		{"list timecards", "ListTimecards", func(s *TimeServer) error {
			out, e := s.ListTimecards(ctx, &timev1.ListTimecardsRequest{WorkerId: "worker-1", Page: &commonv1.PageRequest{PageSize: 10}})
			if e == nil && len(out.GetTimecards()) != 1 {
				t.Fatalf("timecards=%v", out)
			}
			return e
		}, func(r TimeRequest) {
			if r.PageSize != 10 || r.WorkerID != "worker-1" {
				t.Fatalf("list timecards request=%+v", r)
			}
		}},
		{"submit timecard", "SubmitTimecard", func(s *TimeServer) error {
			_, e := s.SubmitTimecard(ctx, &timev1.SubmitTimecardRequest{TimecardId: "card-1", ExpectedRevision: 2, IdempotencyKey: "submit"})
			return e
		}, nil},
		{"attest timecard", "AttestTimecard", func(s *TimeServer) error {
			_, e := s.AttestTimecard(ctx, &timev1.AttestTimecardRequest{TimecardId: "card-1", ExpectedRevision: 2, IdempotencyKey: "attest"})
			return e
		}, nil},
		{"approve timecard", "ApproveTimecard", func(s *TimeServer) error {
			_, e := s.ApproveTimecard(ctx, &timev1.ApproveTimecardRequest{TimecardId: "card-1", ExpectedRevision: 3, IdempotencyKey: "approve"})
			return e
		}, nil},
		{"reopen timecard", "ReopenTimecard", func(s *TimeServer) error {
			_, e := s.ReopenTimecard(ctx, &timev1.ReopenTimecardRequest{TimecardId: "card-1", Reason: "correction", ExpectedRevision: 3, IdempotencyKey: "reopen"})
			return e
		}, nil},
		{"correct punch", "CorrectPunch", func(s *TimeServer) error {
			_, e := s.CorrectPunch(ctx, &timev1.CorrectPunchRequest{SessionId: "session-1", PunchId: "punch-1", Reason: "source correction", ExpectedRevision: 4, CorrectedOccurredAt: at, IdempotencyKey: "correct"})
			return e
		}, func(r TimeRequest) {
			if r.SessionID != "session-1" || r.PunchID != "punch-1" || r.Reason != "source correction" {
				t.Fatalf("correct punch request=%+v", r)
			}
		}},
		{"request missed punch", "RequestMissedPunch", func(s *TimeServer) error {
			_, e := s.RequestMissedPunch(ctx, &timev1.RequestMissedPunchRequest{WorkerId: "worker-1", SessionId: "session-1", Reason: "forgotten punch", ClaimedOccurredAt: at, IdempotencyKey: "missed"})
			return e
		}, func(r TimeRequest) {
			if r.WorkerID != "worker-1" || r.SessionID != "session-1" || r.Reason != "forgotten punch" {
				t.Fatalf("missed punch request=%+v", r)
			}
		}},
		{"decide missed punch", "DecideMissedPunch", func(s *TimeServer) error {
			_, e := s.DecideMissedPunch(ctx, &timev1.DecideMissedPunchRequest{RequestId: "missed-1", Reason: "evidence accepted", ExpectedRevision: 2, IdempotencyKey: "decision", Decision: timev1.MissedPunchRequestStatus_MISSED_PUNCH_REQUEST_STATUS_APPROVED})
			return e
		}, func(r TimeRequest) {
			if r.TimecardID != "missed-1" || r.Decision != "MISSED_PUNCH_REQUEST_STATUS_APPROVED" || r.ExpectedRevision != 2 {
				t.Fatalf("decision request=%+v", r)
			}
		}},
		{"create shift", "CreateShift", func(s *TimeServer) error {
			_, e := s.CreateShift(ctx, &timev1.CreateShiftRequest{SiteId: "site-1", WorkerId: "worker-1", JobId: "job-1", Timezone: "UTC", StartsAt: at, EndsAt: timestamppb.New(at.AsTime().Add(time.Hour)), IdempotencyKey: "create-shift"})
			return e
		}, func(r TimeRequest) {
			if r.SiteID != "site-1" || r.WorkerID != "worker-1" || r.JobID != "job-1" {
				t.Fatalf("create shift request=%+v", r)
			}
		}},
		{"publish shift", "PublishShift", func(s *TimeServer) error {
			_, e := s.PublishShift(ctx, &timev1.PublishShiftRequest{ShiftId: "shift-1", ExpectedRevision: 1, IdempotencyKey: "publish"})
			return e
		}, nil},
		{"cancel shift", "CancelShift", func(s *TimeServer) error {
			_, e := s.CancelShift(ctx, &timev1.CancelShiftRequest{ShiftId: "shift-1", Reason: "cancelled", ExpectedRevision: 1, IdempotencyKey: "cancel"})
			return e
		}, func(r TimeRequest) {
			if r.Reason != "cancelled" {
				t.Fatalf("cancel shift request=%+v", r)
			}
		}},
		{"list shifts", "ListShifts", func(s *TimeServer) error {
			out, e := s.ListShifts(ctx, &timev1.ListShiftsRequest{SiteId: "site-1", Page: &commonv1.PageRequest{PageSize: 5}})
			if e == nil && len(out.GetShifts()) != 1 {
				t.Fatalf("shifts=%v", out)
			}
			return e
		}, func(r TimeRequest) {
			if r.SiteID != "site-1" || r.PageSize != 5 {
				t.Fatalf("list shifts request=%+v", r)
			}
		}},
		{"get profile", "GetTimeProfile", func(s *TimeServer) error {
			out, e := s.GetTimeProfile(ctx, &timev1.GetTimeProfileRequest{WorkerId: "worker-1"})
			if e == nil && out.GetProfile().GetProfileId() != "profile-1" {
				t.Fatalf("profile=%v", out)
			}
			return e
		}, nil},
		{"assign profile", "AssignTimeProfile", func(s *TimeServer) error {
			_, e := s.AssignTimeProfile(ctx, &timev1.AssignTimeProfileRequest{WorkerId: "worker-1", IdempotencyKey: "assign"})
			return e
		}, nil},
		{"list events", "ListTimeEvents", func(s *TimeServer) error {
			_, e := s.ListTimeEvents(ctx, &timev1.ListTimeEventsRequest{Page: &commonv1.PageRequest{PageSize: 7}})
			return e
		}, func(r TimeRequest) {
			if r.PageSize != 7 {
				t.Fatalf("list events request=%+v", r)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := &legacyContractApp{result: base}
			server := NewTimeServer(app)
			if err := tc.call(server); err != nil {
				t.Fatal(err)
			}
			if app.request.Operation != tc.operation || app.principal != principal {
				t.Fatalf("request=%+v principal=%v", app.request, app.principal)
			}
			if tc.verify != nil {
				tc.verify(app.request)
			}
		})
	}
	app := &legacyContractApp{err: clockservice.ErrUnavailable}
	if _, err := NewTimeServer(app).GetCurrentSession(ctx, &timev1.GetCurrentSessionRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("typed transport error=%v", err)
	}
}
