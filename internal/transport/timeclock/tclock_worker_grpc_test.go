package timeclock

import (
	"context"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workerClockGRPCApp struct {
	projection clockservice.SelfClockStatus
	result     clockservice.SelfClockActionResult
	principal  *trust.Principal
	request    clockservice.SelfClockActionRequest
}

func (a *workerClockGRPCApp) GetSelfClock(_ context.Context, p *trust.Principal) (clockservice.SelfClockStatus, error) {
	a.principal = p
	return a.projection, nil
}

func (a *workerClockGRPCApp) ExecuteSelfClockAction(_ context.Context, p *trust.Principal, req clockservice.SelfClockActionRequest) (clockservice.SelfClockActionResult, error) {
	a.principal = p
	a.request = req
	return a.result, nil
}

func workerClockGRPCPrincipal(t *testing.T, kind trust.SubjectKind) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               "tenant-a",
		Subject:              "human-1",
		SubjectKind:          kind,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-1",
		IssuedAt:             time.Now().Add(-time.Minute),
		ExpiresAt:            time.Now().Add(time.Minute),
		CredentialDigest:     "digest-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWorkerClockServerGetSelfClockMapsProjection(t *testing.T) {
	app := &workerClockGRPCApp{projection: clockservice.SelfClockStatus{
		WorkerLabel: "Ada Lovelace", ScheduleLabel: "Day", StatusLabel: "Clocked in",
		LastEventLabel: "09:00", Revision: 7,
	}}
	p := workerClockGRPCPrincipal(t, trust.SubjectKindHuman)
	got, err := NewWorkerClockServer(app).GetSelfClock(trust.WithPrincipal(context.Background(), p), &timev1.GetSelfClockRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetWorkerLabel() != "Ada Lovelace" || got.GetRevision() != 7 || app.principal != p {
		t.Fatalf("projection=%v principal=%v", got, app.principal)
	}
}

func TestWorkerClockServerExecuteSelfClockActionMapsTrustedRequest(t *testing.T) {
	app := &workerClockGRPCApp{result: clockservice.SelfClockActionResult{
		ReceiptID: "receipt-1", WorkerRef: "worker-1", AssignmentRef: "assignment-1",
		WorkflowInstanceRef: "00000000-0000-0000-0000-000000000001", PublishedPlanRef: "plan-1",
		WorkflowTraceID: "trace-1", WorkflowNodeID: "commit_punch", WorkflowAttempt: 1, WorkflowInstanceVersion: 2,
		Status: clockservice.SelfClockStatus{WorkerLabel: "Ada", StatusLabel: "Clocked in", Revision: 8},
	}}
	p := workerClockGRPCPrincipal(t, trust.SubjectKindHuman)
	got, err := NewWorkerClockServer(app).ExecuteSelfClockAction(trust.WithPrincipal(context.Background(), p), &timev1.ExecuteSelfClockActionRequest{
		Action: timev1.ExecuteSelfClockActionRequest_ACTION_IN, ExpectedRevision: 7, IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetReceiptId() != "receipt-1" || got.GetStatus().GetRevision() != 8 {
		t.Fatalf("receipt=%v", got)
	}
	if app.request.Action != "IN" || app.request.ExpectedRevision != 7 || app.request.IdempotencyKey != "idem-1" || app.principal != p {
		t.Fatalf("request=%+v principal=%v", app.request, app.principal)
	}
}

func TestWorkerClockServerMapsBreakActions(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire timev1.ExecuteSelfClockActionRequest_Action
		want string
	}{{"start break", timev1.ExecuteSelfClockActionRequest_ACTION_START_BREAK, "START_BREAK"}, {"end break", timev1.ExecuteSelfClockActionRequest_ACTION_END_BREAK, "END_BREAK"}} {
		t.Run(tc.name, func(t *testing.T) {
			app := &workerClockGRPCApp{}
			request := &timev1.ExecuteSelfClockActionRequest{Action: tc.wire, ExpectedRevision: 4, IdempotencyKey: "break-key"}
			_, err := NewWorkerClockServer(app).ExecuteSelfClockAction(trust.WithPrincipal(context.Background(), workerClockGRPCPrincipal(t, trust.SubjectKindHuman)), request)
			if err != nil || app.request.Action != tc.want || app.request.ExpectedRevision != 4 || app.request.IdempotencyKey != "break-key" {
				t.Fatalf("request=%+v err=%v", app.request, err)
			}
		})
	}
}

func TestWorkerClockServerRejectsMissingOrUntrustedRequests(t *testing.T) {
	server := NewWorkerClockServer(&workerClockGRPCApp{})
	for _, tc := range []struct {
		name string
		call func() error
		code codes.Code
	}{
		{name: "nil get", call: func() error { _, err := server.GetSelfClock(context.Background(), nil); return err }, code: codes.InvalidArgument},
		{name: "missing principal", call: func() error {
			_, err := server.GetSelfClock(context.Background(), &timev1.GetSelfClockRequest{})
			return err
		}, code: codes.Unauthenticated},
		{name: "service principal", call: func() error {
			_, err := server.GetSelfClock(trust.WithPrincipal(context.Background(), workerClockGRPCPrincipal(t, trust.SubjectKindService)), &timev1.GetSelfClockRequest{})
			return err
		}, code: codes.Unauthenticated},
		{name: "nil action", call: func() error { _, err := server.ExecuteSelfClockAction(context.Background(), nil); return err }, code: codes.InvalidArgument},
		{name: "unspecified action", call: func() error {
			_, err := server.ExecuteSelfClockAction(trust.WithPrincipal(context.Background(), workerClockGRPCPrincipal(t, trust.SubjectKindHuman)), &timev1.ExecuteSelfClockActionRequest{})
			return err
		}, code: codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := status.Code(tc.call()); got != tc.code {
				t.Fatalf("code=%s want=%s", got, tc.code)
			}
		})
	}
}
