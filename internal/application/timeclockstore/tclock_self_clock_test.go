package timeclockstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type selfSourceFake struct{}

type selfClockPunchFake struct {
	action string
	punch  clockservice.PunchRequest
}

func (f *selfClockPunchFake) ClockIn(_ context.Context, _ *trust.Principal, punch clockservice.PunchRequest) (clockservice.PunchReceipt, error) {
	return f.record("IN", punch)
}
func (f *selfClockPunchFake) ClockOut(_ context.Context, _ *trust.Principal, punch clockservice.ClockOutRequest) (clockservice.PunchReceipt, error) {
	return f.record("OUT", punch.PunchRequest)
}
func (f *selfClockPunchFake) StartBreak(_ context.Context, _ *trust.Principal, punch clockservice.PunchRequest) (clockservice.PunchReceipt, error) {
	return f.record("START_BREAK", punch)
}
func (f *selfClockPunchFake) EndBreak(_ context.Context, _ *trust.Principal, punch clockservice.PunchRequest) (clockservice.PunchReceipt, error) {
	return f.record("END_BREAK", punch)
}
func (f *selfClockPunchFake) record(action string, punch clockservice.PunchRequest) (clockservice.PunchReceipt, error) {
	f.action, f.punch = action, punch
	return clockservice.PunchReceipt{ObservationID: action}, nil
}

func TestTodo_FTIME_003_WorkflowActionExecutorRoutesBreaksThroughPunchService(t *testing.T) {
	for _, action := range []string{"START_BREAK", "END_BREAK"} {
		t.Run(action, func(t *testing.T) {
			fake := &selfClockPunchFake{}
			punch := clockservice.PunchRequest{ClaimedWorkerRef: "worker", AssignmentRef: "assignment", IdempotencyKey: "stable", ExpectedProjectionRevision: 9}
			receipt, err := executeSelfClockPunch(fake, context.Background(), nil, action, punch)
			if err != nil || fake.action != action || fake.punch != punch || receipt.ObservationID != action {
				t.Fatalf("action=%s punch=%+v receipt=%+v err=%v", fake.action, fake.punch, receipt, err)
			}
		})
	}
	if _, err := executeSelfClockPunch(&selfClockPunchFake{}, context.Background(), nil, "BREAK", clockservice.PunchRequest{}); err != clockservice.ErrInvalidRequest {
		t.Fatalf("unknown action error=%v", err)
	}
}

func (selfSourceFake) ResolveSelfWorker(context.Context, string, string) (clockservice.SelfWorker, error) {
	return clockservice.SelfWorker{WorkerRef: "worker-1", AssignmentRef: "assignment-1", Active: true}, nil
}
func (selfSourceFake) ResolvePublishedProfile(context.Context, string, string, string, time.Time) (timeprofile.TimeProfile, error) {
	return timeprofile.TimeProfile{}, nil
}
func (selfSourceFake) ReadSelfClock(context.Context, string, string, string) (clockservice.SelfClockStatus, error) {
	return clockservice.SelfClockStatus{Revision: 4}, nil
}

func TestSelfClockAdaptersFailClosedWithoutAuthoritativeSources(t *testing.T) {
	if _, err := (WorkerResolver{}).ResolveSelfWorker(context.Background(), "tenant", "subject"); err != clockservice.ErrUnavailable {
		t.Fatalf("worker error=%v", err)
	}
	if _, err := (ProfileResolver{}).ResolvePublishedProfile(context.Background(), "tenant", "worker", "assignment", time.Now()); err != clockservice.ErrUnavailable {
		t.Fatalf("profile error=%v", err)
	}
	if _, err := (ProjectionStore{}).ReadSelfClock(context.Background(), "tenant", "worker", "assignment"); err != clockservice.ErrUnavailable {
		t.Fatalf("projection error=%v", err)
	}
}

func TestSelfClockAdaptersDelegateAuthoritativeSources(t *testing.T) {
	source := selfSourceFake{}
	worker, err := (WorkerResolver{Source: source}).ResolveSelfWorker(context.Background(), "tenant", "subject")
	if err != nil || worker.WorkerRef != "worker-1" {
		t.Fatalf("worker=%+v err=%v", worker, err)
	}
	status, err := (ProjectionStore{Source: source}).ReadSelfClock(context.Background(), "tenant", "worker", "assignment")
	if err != nil || status.Revision != 4 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}
