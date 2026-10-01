package clockservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type tclockPeriodStore struct {
	run       PeriodRun
	hasRun    bool
	putStates []PeriodRunState
}

func (s *tclockPeriodStore) Get(_ context.Context, tenant, period string) (PeriodRun, error) {
	if !s.hasRun || s.run.Trigger.TenantID != tenant || s.run.Trigger.PeriodID != period {
		return PeriodRun{}, errors.New("period run not found")
	}
	return s.run, nil
}

func (s *tclockPeriodStore) Put(_ context.Context, run PeriodRun) error {
	s.run = run
	s.hasRun = true
	s.putStates = append(s.putStates, run.State)
	return nil
}

type tclockPeriodPremiums struct{}

func (tclockPeriodPremiums) Compute(_ context.Context, _ timeprofile.TimeProfile, aggregate PeriodAggregate) (PeriodPremium, error) {
	return PeriodPremium{AggregateDigest: aggregate.Digest, RegularMinutes: aggregate.TotalMinutes, Digest: aggregate.Digest}, nil
}

type tclockPeriodAttestor struct{}

func (tclockPeriodAttestor) Attest(_ context.Context, tenant, period, worker, actor, digest string) (PeriodWorkerAttestation, error) {
	return PeriodWorkerAttestation{TenantID: tenant, PeriodID: period, WorkerRef: worker, AggregateDigest: digest, By: actor, At: time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)}, nil
}

type tclockPeriodApprover struct{}

func (tclockPeriodApprover) Approve(_ context.Context, tenant, period, worker, actor, digest, _ string) (PeriodApproval, error) {
	return PeriodApproval{TenantID: tenant, PeriodID: period, WorkerRef: worker, AggregateDigest: digest, By: actor, At: time.Date(2026, 9, 28, 10, 1, 0, 0, time.UTC)}, nil
}

type tclockPeriodDestination struct {
	store    *tclockPeriodStore
	reject   bool
	calls    int
	lastBody PeriodDestinationPayload
}

func (d *tclockPeriodDestination) Dispatch(_ context.Context, body PeriodDestinationPayload) (PeriodDestinationAcceptance, error) {
	d.calls++
	d.lastBody = body
	if d.store.run.State != PeriodRunLocked {
		return PeriodDestinationAcceptance{}, errors.New("dispatch was not preceded by a locked run")
	}
	return PeriodDestinationAcceptance{TenantID: body.TenantID, PeriodID: body.PeriodID, AggregateDigest: body.Aggregate.Digest, ReceiptRef: "receipt-1", Accepted: !d.reject, Reason: "accepted", At: time.Date(2026, 9, 28, 10, 2, 0, 0, time.UTC)}, nil
}

type tclockPeriodBindings struct{ destination *tclockPeriodDestination }

func (b tclockPeriodBindings) Bind(_ context.Context, tenant string, destination timeprofile.Destination) (PeriodDestination, error) {
	if tenant != "tenant-a" || destination != timeprofile.DestinationPayroll {
		return nil, errors.New("unexpected destination binding")
	}
	return b.destination, nil
}

func tclockPeriodProfile(t *testing.T) timeprofile.TimeProfile {
	t.Helper()
	return timeprofile.TimeProfile{ID: "profile-1", Version: 1, TenantRef: values.TenantId("tenant-a"), EffectiveFrom: values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), Capture: timeprofile.CapturePunch, PayBasis: timeprofile.PayHourly, Exemption: timeprofile.NonExempt, Category: timeprofile.CategoryEmployee, OvertimeMethod: timeprofile.OvertimeSingleRate, AggregationKey: "worker-1", Destination: timeprofile.DestinationPayroll}
}

func tclockPeriodRequest(t *testing.T, inputs []PeriodObligation) PeriodTimecardRequest {
	t.Helper()
	start := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	return PeriodTimecardRequest{Trigger: PeriodTrigger{TenantID: "tenant-a", PeriodID: "period-1", AssignmentGroup: "hourly-us", PeriodStart: start, PeriodEnd: start.Add(24 * time.Hour), At: start.Add(24 * time.Hour)}, WorkerRef: "worker-1", AssignmentRef: "assignment-1", Profile: tclockPeriodProfile(t), Inputs: inputs, RulesRef: "rules-1", WorkerActor: "worker-1", ApproverActor: "manager-1", IdempotencyKey: "period-1-v1"}
}

func tclockPeriodInput(id string, minutes int) PeriodObligation {
	start := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	return PeriodObligation{ID: id, TenantID: "tenant-a", WorkerRef: "worker-1", AssignmentRef: "assignment-1", Kind: PeriodInputSession, Minutes: minutes, Start: start, End: start.Add(time.Duration(minutes) * time.Minute), SourceRefs: []string{"session-" + id}}
}

func tclockPeriodService(store *tclockPeriodStore, destination *tclockPeriodDestination) PeriodTimecardService {
	destination.store = store
	return PeriodTimecardService{Reducers: DefaultPeriodReducers(), Premiums: tclockPeriodPremiums{}, Attestations: tclockPeriodAttestor{}, Approvals: tclockPeriodApprover{}, Destinations: tclockPeriodBindings{destination: destination}, Runs: store, Clock: func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }}
}

// TestTodo_WTIME_005 is the PRIMARY proof: inputs are folded, premiums are
// pinned, worker and manager evidence are collected, then a generic bound
// connector receives the approved aggregate.
func TestTodo_WTIME_005(t *testing.T) {
	store := &tclockPeriodStore{}
	destination := &tclockPeriodDestination{}
	got, err := tclockPeriodService(store, destination).RunPeriodTimecard(context.Background(), tclockPeriodRequest(t, []PeriodObligation{tclockPeriodInput("s1", 120), tclockPeriodInput("s2", 60)}))
	if err != nil {
		t.Fatalf("RunPeriodTimecard: %v", err)
	}
	if got.State != PeriodRunClosed || got.Aggregate.TotalMinutes != 180 || got.Acceptance.ReceiptRef != "receipt-1" || destination.calls != 1 {
		t.Fatalf("period run = %+v, destination calls=%d", got, destination.calls)
	}
	if got.Attestation.By != "worker-1" || got.Approval.By != "manager-1" || got.Profile.Destination != timeprofile.DestinationPayroll {
		t.Fatalf("approval evidence or destination not pinned: %+v", got)
	}
}

func TestTodo_WTIME_005_Integration(t *testing.T) {
	store := &tclockPeriodStore{}
	destination := &tclockPeriodDestination{}
	service := tclockPeriodService(store, destination)
	_, err := service.RunPeriodTimecard(context.Background(), tclockPeriodRequest(t, []PeriodObligation{tclockPeriodInput("s1", 90)}))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.putStates) < 2 || store.putStates[0] != PeriodRunLocked || store.putStates[len(store.putStates)-1] != PeriodRunClosed {
		t.Fatalf("store lifecycle = %v, want LOCKED then CLOSED", store.putStates)
	}
	if destination.lastBody.Destination != timeprofile.DestinationPayroll || destination.lastBody.Aggregate.TotalMinutes != 90 {
		t.Fatalf("connector payload = %+v", destination.lastBody)
	}
}

func TestTodo_WTIME_005_Property(t *testing.T) {
	inputs := []PeriodObligation{tclockPeriodInput("s1", 30), tclockPeriodInput("s2", 45), tclockPeriodInput("s3", 15)}
	store := &tclockPeriodStore{}
	first, err := tclockPeriodService(store, &tclockPeriodDestination{}).RunPeriodTimecard(context.Background(), tclockPeriodRequest(t, inputs))
	if err != nil {
		t.Fatal(err)
	}
	secondInputs := []PeriodObligation{inputs[2], inputs[0], inputs[1]}
	secondReq := tclockPeriodRequest(t, secondInputs)
	secondReq.IdempotencyKey = "period-1-permuted"
	second, err := tclockPeriodService(&tclockPeriodStore{}, &tclockPeriodDestination{}).RunPeriodTimecard(context.Background(), secondReq)
	if err != nil {
		t.Fatal(err)
	}
	if first.Aggregate.TotalMinutes != 90 || second.Aggregate.TotalMinutes != 90 || first.Aggregate.Digest != second.Aggregate.Digest {
		t.Fatalf("reduced totals/digests differ: first=%+v second=%+v", first.Aggregate, second.Aggregate)
	}
}

func TestTodo_WTIME_005_Recovery(t *testing.T) {
	store := &tclockPeriodStore{}
	destination := &tclockPeriodDestination{reject: true}
	_, err := tclockPeriodService(store, destination).RunPeriodTimecard(context.Background(), tclockPeriodRequest(t, []PeriodObligation{tclockPeriodInput("s1", 60)}))
	if !errors.Is(err, ErrPeriodNotAccepted) {
		t.Fatalf("destination refusal = %v, want ErrPeriodNotAccepted", err)
	}
	if !store.hasRun || store.run.State != PeriodRunLocked {
		t.Fatalf("failed round-trip lost the locked aggregate: %+v", store.run)
	}
}

func TestTodo_WTIME_005_Security(t *testing.T) {
	store := &tclockPeriodStore{}
	destination := &tclockPeriodDestination{}
	service := tclockPeriodService(store, destination)
	req := tclockPeriodRequest(t, []PeriodObligation{tclockPeriodInput("s1", 60)})
	req.Inputs[0].TenantID = "tenant-b"
	if _, err := service.RunPeriodTimecard(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cross-tenant obligation = %v, want ErrInvalidRequest", err)
	}
	req = tclockPeriodRequest(t, []PeriodObligation{tclockPeriodInput("s1", 60)})
	req.ApproverActor = req.WorkerActor
	if _, err := service.RunPeriodTimecard(context.Background(), req); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("self approval = %v, want ErrSelfApproval", err)
	}
}

func TestTodo_WTIME_005_LateSessionReopensByType(t *testing.T) {
	store := &tclockPeriodStore{}
	destination := &tclockPeriodDestination{}
	service := tclockPeriodService(store, destination)
	if _, err := service.RunPeriodTimecard(context.Background(), tclockPeriodRequest(t, []PeriodObligation{tclockPeriodInput("s1", 60)})); err != nil {
		t.Fatal(err)
	}
	reopened, err := service.ReopenPeriodTimecard(context.Background(), PeriodReopenRequest{TenantID: "tenant-a", PeriodID: "period-1", Reason: PeriodReopenLateSession, LateInput: tclockPeriodInput("late", 30), ActorRef: "timekeeper-1", WorkerActor: "worker-1", ApproverActor: "manager-1", IdempotencyKey: "period-1-reopen-2"})
	if err != nil {
		t.Fatalf("ReopenPeriodTimecard: %v", err)
	}
	if reopened.State != PeriodRunClosed || reopened.Revision != 2 || reopened.Aggregate.TotalMinutes != 90 || reopened.ReopenRef != "LATE_SESSION:timekeeper-1" {
		t.Fatalf("reopened run = %+v", reopened)
	}
}
