package clockservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type selfWorkerResolverFake struct {
	gotTenant, gotSubject string
	worker                SelfWorker
}

func (f *selfWorkerResolverFake) ResolveSelfWorker(_ context.Context, tenant, subject string) (SelfWorker, error) {
	f.gotTenant, f.gotSubject = tenant, subject
	return f.worker, nil
}

type selfProfileFake struct{ profile timeprofile.TimeProfile }

func (f selfProfileFake) ResolvePublishedProfile(context.Context, string, string, string, time.Time) (timeprofile.TimeProfile, error) {
	return f.profile, nil
}

type selfStoreFake struct{ status SelfClockStatus }

func (f selfStoreFake) ReadSelfClock(context.Context, string, string, string) (SelfClockStatus, error) {
	return f.status, nil
}

type selfActionFake struct{ got SelfClockActionRequest }

func (f *selfActionFake) ExecutePublishedClockAction(_ context.Context, _ *trust.Principal, req SelfClockActionRequest) (SelfClockActionResult, error) {
	f.got = req
	return SelfClockActionResult{ReceiptID: "receipt-1", WorkerRef: "worker-1", AssignmentRef: "assignment-1", WorkflowInstanceRef: "00000000-0000-0000-0000-000000000001", PublishedPlanRef: "plan-1", WorkflowID: "hcmnext.workflows.time.clock_in_out", WorkflowTraceID: "trace-1", WorkflowNodeID: "commit_punch", WorkflowAttempt: 1, WorkflowInstanceVersion: 2, Status: SelfClockStatus{WorkerLabel: "Taylor", ScheduleLabel: "Day", StatusLabel: "Clocked in", LastEventLabel: "now", Revision: req.ExpectedRevision + 1}}, nil
}

func TestTodo_TCLOCK_WorkerSelfUsesTrustedSubjectAndPublishedProjection(t *testing.T) {
	resolver := &selfWorkerResolverFake{worker: SelfWorker{WorkerRef: "worker-1", DisplayName: "Taylor", AssignmentRef: "assignment-1", ScheduleLabel: "Day", Active: true}}
	svc := WorkerSelfService{Workers: resolver, Profiles: selfProfileFake{profile: selfPunchProfile()}, Store: selfStoreFake{status: SelfClockStatus{StatusLabel: "Clocked out", LastEventLabel: "No event", Revision: 3}}, Clock: func() time.Time { return time.Unix(10, 0) }}
	status, err := svc.GetSelfClock(context.Background(), selfPrincipal(t))
	if err != nil || status.WorkerLabel != "Taylor" || status.ScheduleLabel != "Day" || resolver.gotTenant != "tenant" || resolver.gotSubject != "worker-subject" {
		t.Fatalf("status=%+v err=%v resolver=%+v", status, err, resolver)
	}
}

func TestTodo_TCLOCK_WorkerSelfActionPinsResolvedIdentityRevisionAndIdempotency(t *testing.T) {
	actions := &selfActionFake{}
	svc := WorkerSelfService{Workers: &selfWorkerResolverFake{worker: SelfWorker{WorkerRef: "worker-1", AssignmentRef: "assignment-1", Active: true}}, Profiles: selfProfileFake{profile: selfPunchProfile()}, Store: selfStoreFake{status: SelfClockStatus{Revision: 3}}, Actions: actions, Clock: func() time.Time { return time.Unix(10, 0) }}
	result, err := svc.ExecuteSelfClockAction(context.Background(), selfPrincipal(t), SelfClockActionRequest{Action: "in", WorkerRef: "forged", AssignmentRef: "forged", ExpectedRevision: 3, IdempotencyKey: "idem-1"})
	if err != nil || result.ReceiptID != "receipt-1" || actions.got.Action != "in" || actions.got.WorkerRef != "worker-1" || actions.got.AssignmentRef != "assignment-1" || actions.got.ExpectedRevision != 3 {
		t.Fatalf("result=%+v action=%+v err=%v", result, actions.got, err)
	}
}

func TestTodo_FTIME_003_WorkerSelfOffersAndValidatesBreakActions(t *testing.T) {
	for _, action := range []string{"START_BREAK", "END_BREAK"} {
		t.Run(action, func(t *testing.T) {
			actions := &selfActionFake{}
			svc := WorkerSelfService{Workers: &selfWorkerResolverFake{worker: SelfWorker{WorkerRef: "worker-1", AssignmentRef: "assignment-1", Active: true}}, Profiles: selfProfileFake{profile: selfPunchProfile()}, Store: selfStoreFake{status: SelfClockStatus{Revision: 3}}, Actions: actions, Clock: func() time.Time { return time.Unix(10, 0) }}
			result, err := svc.ExecuteSelfClockAction(context.Background(), selfPrincipal(t), SelfClockActionRequest{Action: action, ExpectedRevision: 3, IdempotencyKey: "break-key"})
			if err != nil || actions.got.Action != action || actions.got.WorkerRef != "worker-1" || actions.got.AssignmentRef != "assignment-1" || actions.got.ExpectedRevision != 3 || result.Status.Revision != 4 {
				t.Fatalf("result=%+v request=%+v err=%v", result, actions.got, err)
			}
		})
	}
}

func TestTodo_TCLOCK_WorkerSelfRejectsNonPunchAndIncompleteReads(t *testing.T) {
	svc := WorkerSelfService{Workers: &selfWorkerResolverFake{worker: SelfWorker{WorkerRef: "worker-1", AssignmentRef: "assignment-1", Active: true}}, Profiles: selfProfileFake{profile: selfPunchProfile()}, Store: selfStoreFake{status: SelfClockStatus{StatusLabel: "Clocked out"}}, Clock: func() time.Time { return time.Unix(10, 0) }}
	if _, err := svc.GetSelfClock(context.Background(), selfPrincipal(t)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("incomplete read error=%v, want unavailable", err)
	}
	if _, err := svc.ExecuteSelfClockAction(context.Background(), selfPrincipal(t), SelfClockActionRequest{Action: "break", ExpectedRevision: 1, IdempotencyKey: "i"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid action error=%v, want invalid request", err)
	}
}

func TestTodo_TCLOCK_WorkerSelfRetryReachesWorkflowIdempotencyBeforeRevisionFence(t *testing.T) {
	actions := &selfActionFake{}
	svc := WorkerSelfService{Workers: &selfWorkerResolverFake{worker: SelfWorker{WorkerRef: "worker-1", AssignmentRef: "assignment-1", Active: true}}, Profiles: selfProfileFake{profile: selfPunchProfile()}, Store: selfStoreFake{status: SelfClockStatus{Revision: 4}}, Actions: actions, Clock: func() time.Time { return time.Unix(10, 0) }}
	result, err := svc.ExecuteSelfClockAction(context.Background(), selfPrincipal(t), SelfClockActionRequest{Action: "IN", ExpectedRevision: 3, IdempotencyKey: "original-action"})
	if err != nil || actions.got.ExpectedRevision != 3 || result.Status.Revision != 4 {
		t.Fatalf("result=%+v action=%+v err=%v", result, actions.got, err)
	}
}

func TestTodo_TCLOCK_WorkerSelfReceiptRejectsForeignWorkflowAndNilInstance(t *testing.T) {
	actions := &selfActionFake{}
	receipt, _ := actions.ExecutePublishedClockAction(context.Background(), nil, SelfClockActionRequest{ExpectedRevision: 3})
	for _, tc := range []struct {
		name               string
		workflow, instance string
	}{
		{"foreign workflow", "unrelated.workflow", receipt.WorkflowInstanceRef},
		{"nil instance", receipt.WorkflowID, "00000000-0000-0000-0000-000000000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forged := receipt
			forged.WorkflowID, forged.WorkflowInstanceRef = tc.workflow, tc.instance
			if validWorkflowReceipt(forged, "worker-1", "assignment-1", "IN") {
				t.Fatal("forged workflow receipt accepted")
			}
		})
	}
}

func selfPunchProfile() timeprofile.TimeProfile {
	return timeprofile.TimeProfile{ID: "profile-1", Version: 1, TenantRef: values.TenantId("tenant"), EffectiveFrom: values.NewInstant(time.Unix(1, 0)), Capture: timeprofile.CapturePunch, PayBasis: timeprofile.PayHourly, Exemption: timeprofile.NonExempt, Category: timeprofile.CategoryEmployee, Destination: timeprofile.DestinationPayroll, AggregationKey: "worker"}
}

func selfPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant"), Subject: "worker-subject", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
