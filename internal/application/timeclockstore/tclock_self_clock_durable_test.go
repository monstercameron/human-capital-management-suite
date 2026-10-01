package timeclockstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type durableSelfWorkerDirectory struct{}

func (durableSelfWorkerDirectory) ResolveWorker(_ context.Context, tenant, worker string) (string, bool, error) {
	if tenant != "tenant-adapter" {
		return "", false, fmt.Errorf("unexpected tenant %q", tenant)
	}
	return worker, true, nil
}

func (durableSelfWorkerDirectory) ResolveAssignment(_ context.Context, _, worker, assignment string) (string, string, bool, error) {
	return "project-1", "site-1", worker == "worker-1" && assignment == "assignment-1", nil
}

type durableSelfAuthorizer struct{}

func (durableSelfAuthorizer) AuthorizePunch(context.Context, *trust.Principal, string, string, string) (bool, error) {
	return false, nil
}
func (durableSelfAuthorizer) AuthorizeDeviceAdmin(context.Context, *trust.Principal, string, string) error {
	return nil
}
func (durableSelfAuthorizer) AuthorizeSupervisorOverride(context.Context, *trust.Principal, string, string) error {
	return nil
}

type durableSelfIDs struct{}

func (durableSelfIDs) Deterministic(parts ...string) string { return "id-" + strings.Join(parts, "-") }
func (durableSelfIDs) Random() string                       { return "random-id" }

type durableSelfPunchWorkflow struct{ adapter Adapter }

func (w durableSelfPunchWorkflow) ExecutePunch(ctx context.Context, tenant string, work clockservice.PunchWork) (clockservice.WorkflowPunchResult, error) {
	committed, err := w.adapter.CommitPunch(ctx, tenant, work)
	if err != nil {
		return clockservice.WorkflowPunchResult{}, err
	}
	node := "commit_punch"
	if work.Observation.EventType == "OUT" {
		node = "commit_clock_out"
	}
	return clockservice.WorkflowPunchResult{
		PunchResult: committed, InstanceID: uuid.New(), WorkflowID: "hcmnext.workflows.time.clock_in_out",
		PlanDigest: "plan-digest", StartKey: work.Observation.IdempotencyKey, TraceID: "trace-id",
		NodeID: node, Attempt: 1, InstanceVersion: 1, Committed: true,
	}, nil
}

func TestTodo_FTIME_003_SelfBreakActionsCommitDurablyWithCASAndIdempotency(t *testing.T) {
	ctx := context.Background()
	store, tenant := adapterFixture(t)
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	adapter := Adapter{Store: store}
	labels := CatalogClockLabels{Locale: workspace.ResolveLocale("en-US"), WorkerLabel: func(string) string { return "Ana Flores" }, ScheduleLabel: func(string) string { return "Ironridge day shift" }}
	projection := SelfProjectionStore{Store: store, Labels: labels}
	initial, err := projection.ReadSelfClock(ctx, tenant, "worker-1", "assignment-1")
	if err != nil || initial.StatusCode != "CLOCKED_OUT" || initial.Revision == 0 {
		t.Fatalf("initial projection=%+v err=%v", initial, err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: "worker-1", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-1", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential-digest"})
	if err != nil {
		t.Fatal(err)
	}
	service := clockservice.Service{
		Workers: durableSelfWorkerDirectory{}, Auth: durableSelfAuthorizer{},
		Sessions: SessionAdapter{Store: store}, Observations: ObservationAdapter{Store: store},
		PunchWorkflow: durableSelfPunchWorkflow{adapter: adapter}, IDs: durableSelfIDs{}, Clock: func() time.Time { return at },
	}
	executor := WorkflowActionExecutor{Service: service, Clock: func() time.Time { return at }, Projection: projection}
	act := func(action, key string, revision uint64) (clockservice.SelfClockActionResult, error) {
		return executor.ExecutePublishedClockAction(ctx, principal, clockservice.SelfClockActionRequest{Action: action, WorkerRef: "worker-1", AssignmentRef: "assignment-1", IdempotencyKey: key, ExpectedRevision: revision})
	}
	in, err := act("IN", "in-key", initial.Revision)
	if err != nil || in.Status.StatusCode != "CLOCKED_IN" || in.Status.Revision != initial.Revision+1 {
		t.Fatalf("clock in=%+v err=%v", in, err)
	}
	if _, err := act("START_BREAK", "stale-break-key", initial.Revision); err == nil {
		t.Fatal("start break with a stale projection revision succeeded")
	}
	unchanged, err := projection.ReadSelfClock(ctx, tenant, "worker-1", "assignment-1")
	if err != nil || unchanged.StatusCode != "CLOCKED_IN" || unchanged.Revision != in.Status.Revision {
		t.Fatalf("stale write changed projection=%+v err=%v", unchanged, err)
	}
	started, err := act("START_BREAK", "break-start-key", unchanged.Revision)
	if err != nil || started.Status.StatusCode != "ON_BREAK" || started.Status.Revision != unchanged.Revision+1 {
		t.Fatalf("start break=%+v err=%v", started, err)
	}
	replayed, err := act("START_BREAK", "break-start-key", unchanged.Revision)
	if err != nil || replayed.ReceiptID != started.ReceiptID || replayed.Status.StatusCode != "ON_BREAK" || replayed.Status.Revision != started.Status.Revision {
		t.Fatalf("idempotent break replay=%+v original=%+v err=%v", replayed, started, err)
	}
	ended, err := act("END_BREAK", "break-end-key", started.Status.Revision)
	if err != nil || ended.Status.StatusCode != "CLOCKED_IN" || ended.Status.Revision != started.Status.Revision+1 {
		t.Fatalf("end break=%+v err=%v", ended, err)
	}
	out, err := act("OUT", "out-key", ended.Status.Revision)
	if err != nil || out.Status.StatusCode != "CLOCKED_OUT" || out.Status.Revision != ended.Status.Revision+1 || out.WorkflowNodeID != "commit_clock_out" {
		t.Fatalf("clock out=%+v err=%v", out, err)
	}
	observations, _, err := (ObservationAdapter{Store: store}).ListObservations(ctx, tenant, "worker-1", time.Time{}, time.Time{}, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, observation := range observations {
		counts[observation.EventType]++
	}
	if counts["IN"] != 1 || counts["BREAK_START"] != 1 || counts["BREAK_END"] != 1 || counts["OUT"] != 1 {
		t.Fatalf("durable observations by event=%v", counts)
	}
	if _, err := (SessionAdapter{Store: store}).CurrentSession(ctx, tenant, "worker-1", "assignment-1"); !errors.Is(err, timestore.ErrNotFound) {
		t.Fatalf("clocked-out session lookup error=%v, want not found", err)
	}
}
