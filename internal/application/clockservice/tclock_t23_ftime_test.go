package clockservice

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
)

func TestTodo_FTIME_003(t *testing.T) {
	work := newRegWork()
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, work)
	p := regPrincipal(t, regWorker)

	inReq := regPunch("ftime-in", regNow)
	inReq.Scheduled = false
	in, err := svc.ClockIn(context.Background(), p, inReq)
	if err != nil {
		t.Fatalf("ClockIn: %v", err)
	}
	if in.Status != ReceiptAccepted || in.State != timesession.StateOpen || in.Exception == nil || in.Exception.Kind != timesession.ExceptionUnscheduledWork {
		t.Fatalf("ClockIn receipt = %+v; want accepted open session with unscheduled exception", in)
	}

	start, err := svc.StartBreak(context.Background(), p, regPunch("ftime-break-start", regNow.Add(time.Hour)))
	if err != nil || start.State != timesession.StateOnBreak {
		t.Fatalf("StartBreak = %+v, %v", start, err)
	}
	end, err := svc.EndBreak(context.Background(), p, regPunch("ftime-break-end", regNow.Add(2*time.Hour)))
	if err != nil || end.State != timesession.StateOpen {
		t.Fatalf("EndBreak = %+v, %v", end, err)
	}
	out, err := svc.ClockOut(context.Background(), p, ClockOutRequest{PunchRequest: regPunch("ftime-out", regNow.Add(3*time.Hour))})
	if err != nil || out.Status != ReceiptAccepted || out.State != timesession.StateClosed {
		t.Fatalf("ClockOut = %+v, %v", out, err)
	}

	var captured timesession.Session
	if err := json.Unmarshal(work.records[in.SessionID].Payload, &captured); err != nil {
		t.Fatal(err)
	}
	if captured.Tenant != regTenant || captured.Worker != regWorker || captured.Assignment != regAssign || len(captured.Segments) != 3 {
		t.Fatalf("captured session = %+v", captured)
	}
	first := work.observations[inReq.IdempotencyKey]
	if first.ReceivedAt != regNow || !first.OccurredAt.Equal(inReq.DeviceTime) || first.ProjectRef != "project-clock-reg" || first.Source != inReq.DeviceKind {
		t.Fatalf("observation = %+v; want server receipt, device occurrence, resolved project and source", first)
	}
}

func TestTodo_FTIME_003_Security(t *testing.T) {
	work := newRegWork()
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, work)
	_, err := svc.ClockIn(context.Background(), regPrincipal(t, regActor), regPunch("ftime-unauthorized", regNow))
	if !errors.Is(err, ErrDelegationRequired) {
		t.Fatalf("unauthorized ClockIn error = %v, want delegation conflict", err)
	}
	if work.workCalls != 0 {
		t.Fatalf("unauthorized ClockIn reached commit port %d times", work.workCalls)
	}
}

type ftimeRaceWork struct {
	mu     sync.Mutex
	active bool
}

func (w *ftimeRaceWork) Punch(_ context.Context, _ string, work PunchWork) (PunchResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.active {
		return PunchResult{}, timesession.ErrAlreadyOpen
	}
	w.active = true
	return PunchResult{Session: work.Session, Observation: work.Observation}, nil
}

func (*ftimeRaceWork) Batch(context.Context, string, []PunchWork) ([]PunchResult, error) {
	return nil, errors.New("batch not used by FTIME-003 race")
}

func TestTodo_FTIME_003_Race(t *testing.T) {
	work := &ftimeRaceWork{}
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, newRegWork())
	svc.PunchWorkflow = ftimeRaceWorkflow{work: work}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, key := range []string{"ftime-race-a", "ftime-race-b"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			_, err := svc.ClockIn(context.Background(), regPrincipal(t, regWorker), regPunch(key, regNow))
			results <- err
		}(key)
	}
	wg.Wait()
	close(results)
	var accepted, conflicts int
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, timesession.ErrAlreadyOpen) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent ClockIn error: %v", err)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("concurrent ClockIn results: accepted=%d conflicts=%d", accepted, conflicts)
	}
}

type ftimeRaceWorkflow struct{ work *ftimeRaceWork }

func (w ftimeRaceWorkflow) ExecutePunch(ctx context.Context, tenant string, work PunchWork) (WorkflowPunchResult, error) {
	result, err := w.work.Punch(ctx, tenant, work)
	if err != nil {
		return WorkflowPunchResult{}, err
	}
	return WorkflowPunchResult{
		PunchResult:     result,
		InstanceID:      regWorkflowInstanceID,
		WorkflowID:      "published-clock",
		PlanDigest:      "sha256:ftime-003",
		StartKey:        work.Observation.IdempotencyKey,
		TraceID:         "trace-ftime-003",
		NodeID:          "commit_punch",
		Attempt:         1,
		InstanceVersion: 1,
		Committed:       true,
	}, nil
}

var regWorkflowInstanceID = uuid.MustParse("6e280766-6a9a-4da8-90ce-9802eb3d6945")

func TestTodo_FTIME_003_Fault(t *testing.T) {
	work := newRegWork()
	work.fail = ErrRetryLater
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, work)
	receipt, err := svc.ClockIn(context.Background(), regPrincipal(t, regWorker), regPunch("ftime-fault", regNow))
	if !errors.Is(err, ErrRetryLater) || receipt.Status != ReceiptRetryLater || len(work.records) != 0 || len(work.observations) != 0 {
		t.Fatalf("fault result = %+v, %v; want retry-later with no persisted state", receipt, err)
	}

	missingSource := regPunch("ftime-missing-source", regNow)
	missingSource.DeviceKind = ""
	if _, err := svc.ClockIn(context.Background(), regPrincipal(t, regWorker), missingSource); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing source error = %v, want ErrInvalidRequest", err)
	}
}
