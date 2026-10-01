package clockservice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type batchDeviceFake struct{ d DeviceRecord }

func (f batchDeviceFake) GetDevice(_ context.Context, tenant, id string) (DeviceRecord, error) {
	if tenant != f.d.TenantID || id != f.d.ID {
		return DeviceRecord{}, ErrDeviceNotEligible
	}
	return f.d, nil
}
func (f batchDeviceFake) DevicesBySite(context.Context, string, string, int) ([]DeviceRecord, error) {
	return nil, nil
}
func (f batchDeviceFake) RotateDeviceKey(context.Context, string, string, []byte, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, errors.New("unused")
}
func (f batchDeviceFake) SuspendDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, errors.New("unused")
}
func (f batchDeviceFake) ResumeDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, errors.New("unused")
}
func (f batchDeviceFake) RevokeDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, errors.New("unused")
}
func (f batchDeviceFake) ReassignDeviceSite(context.Context, string, string, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, errors.New("unused")
}

type batchRosterFake struct{}

func (batchRosterFake) Delta(context.Context, string, string, string, string, time.Time) (RosterDelta, error) {
	return RosterDelta{SnapshotRevision: "r1", MaxOfflineAge: time.Hour, PunchPolicyVersion: 1, Workers: []RosterWorker{{WorkerRef: "w1", BadgeID: "b1"}}}, nil
}

type batchRosterMissingFake struct{}

func (batchRosterMissingFake) Delta(context.Context, string, string, string, string, time.Time) (RosterDelta, error) {
	return RosterDelta{SnapshotRevision: "r1", MaxOfflineAge: time.Hour, PunchPolicyVersion: 1, Workers: []RosterWorker{{WorkerRef: "other-worker", BadgeID: "b2"}}}, nil
}

type batchWorkersFake struct{}

func (batchWorkersFake) ResolveWorker(context.Context, string, string) (string, bool, error) {
	return "w1", true, nil
}
func (batchWorkersFake) ResolveAssignment(context.Context, string, string, string) (string, string, bool, error) {
	return "project", "site", true, nil
}

type batchAuthFake struct{}

func (batchAuthFake) AuthorizePunch(context.Context, *trust.Principal, string, string, string) (bool, error) {
	return true, nil
}
func (batchAuthFake) AuthorizeDeviceAdmin(context.Context, *trust.Principal, string, string) error {
	return nil
}
func (batchAuthFake) AuthorizeSupervisorOverride(context.Context, *trust.Principal, string, string) error {
	return nil
}

type batchIDsFake struct{}

func (batchIDsFake) Deterministic(parts ...string) string { return parts[len(parts)-1] + "-id" }
func (batchIDsFake) Random() string                       { return "random" }

type batchCommitFake struct{ entries []BatchCommitEntry }

type batchWorkflowFake struct{ store *batchCommitFake }

func (f batchWorkflowFake) ExecutePunch(context.Context, string, PunchWork) (WorkflowPunchResult, error) {
	return WorkflowPunchResult{}, ErrUnavailable
}

func (f batchWorkflowFake) ExecutePunchBatch(ctx context.Context, tenant, device string, entries []BatchCommitEntry) (BatchCommitResult, error) {
	result, err := f.store.CommitPunchBatch(ctx, tenant, device, entries)
	for _, entry := range entries {
		if entry.Work != nil {
			node := "commit_punch"
			if !entry.Work.SessionIsNew {
				node = "commit_clock_out"
			}
			result.WorkflowBindings = append(result.WorkflowBindings, WorkflowReceiptBinding{TenantID: tenant, DeviceSequence: entry.Sequence, ObservationID: entry.Work.Observation.ID, SessionID: entry.Work.Session.ID, InstanceID: "00000000-0000-0000-0000-000000000001", WorkflowID: "workflow-1", PlanDigest: "plan-1", StartKey: "start-1", Committed: true, NodeID: node, Attempt: 1, InstanceVersion: 1, TraceID: "trace-1"})
		}
	}
	return result, err
}

type batchLookupFake struct {
	*batchCommitFake
	receipt ReceiptRecord
	found   bool
}

type batchMissingReceiptFake struct{}

func (batchMissingReceiptFake) ExecutePunch(context.Context, string, PunchWork) (WorkflowPunchResult, error) {
	return WorkflowPunchResult{}, ErrUnavailable
}
func (batchMissingReceiptFake) ExecutePunchBatch(context.Context, string, string, []BatchCommitEntry) (BatchCommitResult, error) {
	return BatchCommitResult{}, nil
}

func (batchMissingReceiptFake) Punch(context.Context, string, PunchWork) (PunchResult, error) {
	return PunchResult{}, ErrUnavailable
}
func (batchMissingReceiptFake) Batch(context.Context, string, []PunchWork) ([]PunchResult, error) {
	return nil, ErrUnavailable
}
func (batchMissingReceiptFake) CommitPunchBatch(context.Context, string, string, []BatchCommitEntry) (BatchCommitResult, error) {
	return BatchCommitResult{}, nil
}

func (f *batchLookupFake) LookupPunchReceipt(context.Context, string, string, int64) (ReceiptRecord, bool, error) {
	return f.receipt, f.found, nil
}

func (f *batchLookupFake) CommitPunchBatch(_ context.Context, _, _ string, entries []BatchCommitEntry) (BatchCommitResult, error) {
	f.entries = entries
	return BatchCommitResult{Receipts: []ReceiptRecord{f.receipt}, HighestContiguous: 1, Duplicates: map[int64]bool{1: true}}, nil
}

func (f *batchCommitFake) Punch(context.Context, string, PunchWork) (PunchResult, error) {
	return PunchResult{}, ErrUnavailable
}

func (f *batchCommitFake) Batch(context.Context, string, []PunchWork) ([]PunchResult, error) {
	return nil, ErrUnavailable
}

func (f *batchCommitFake) CommitPunchBatch(_ context.Context, _, _ string, entries []BatchCommitEntry) (BatchCommitResult, error) {
	f.entries = entries
	out := make([]ReceiptRecord, len(entries))
	for i, e := range entries {
		out[i] = e.Receipt
	}
	return BatchCommitResult{Receipts: out, HighestContiguous: 1}, nil
}

func batchPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant"), Subject: "device", ClientID: "dev-1", SubjectKind: trust.SubjectKindService, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-device", CredentialDigest: "credential-device", IssuedAt: time.Unix(10, 0), ExpiresAt: time.Unix(100, 0)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func batchService(fake *batchCommitFake) Service {
	return Service{Devices: batchDeviceFake{d: DeviceRecord{TenantID: "tenant", ID: "dev-1", SiteID: "site", ProfileID: "profile", Timezone: "UTC", State: "ACTIVE", Revision: 1, PublicKey: make([]byte, 32)}}, Roster: batchRosterFake{}, Workers: batchWorkersFake{}, Auth: batchAuthFake{}, IDs: batchIDsFake{}, Work: fake, PunchWorkflow: batchWorkflowFake{store: fake}, BatchWorkflow: batchWorkflowFake{store: fake}, Clock: func() time.Time { return time.Unix(20, 0) }, Sessions: batchSessionUnavailable{}}
}

type batchSessionUnavailable struct{}

func (batchSessionUnavailable) OpenSession(context.Context, string, SessionRecord, SessionEvent) (SessionRecord, error) {
	return SessionRecord{}, ErrUnavailable
}
func (batchSessionUnavailable) ApplyTransition(context.Context, string, string, uint64, SessionRecord, []SessionEvent) (SessionRecord, error) {
	return SessionRecord{}, ErrUnavailable
}
func (batchSessionUnavailable) CurrentSession(context.Context, string, string, string) (SessionRecord, error) {
	return SessionRecord{}, ErrInvalidRequest
}

func TestTodo_TCLOCK_003(t *testing.T) {
	store := &batchCommitFake{}
	s := batchService(store)
	got, err := s.SubmitPunches(context.Background(), batchPrincipal(t), BatchRequest{DeviceID: "dev-1", Punches: []BatchPunch{{DeviceSequence: 1, EventType: timesession.PunchIn, WorkerCredentialRef: "badge-ref", AssignmentRef: "assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: "p1", Scheduled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Receipts) != 1 || got.Receipts[0].Status != BatchAccepted || len(store.entries) != 1 || store.entries[0].Work == nil {
		t.Fatalf("response=%+v entries=%+v", got, store.entries)
	}
	if !store.entries[0].Work.Observation.OccurredAt.Equal(time.Unix(15, 0)) || !store.entries[0].Work.Observation.ReceivedAt.Equal(time.Unix(20, 0)) {
		t.Fatal("device occurred time was used as receipt time")
	}
}

func TestTodo_TCLOCK_003_InvalidRowIsPerPunch(t *testing.T) {
	store := &batchCommitFake{}
	s := batchService(store)
	got, err := s.SubmitPunches(context.Background(), batchPrincipal(t), BatchRequest{DeviceID: "dev-1", Punches: []BatchPunch{{DeviceSequence: 1, EventType: timesession.PunchIn, WorkerCredentialRef: "badge-ref", AssignmentRef: "assignment", Source: "KIOSK", DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: "p1"}, {DeviceSequence: 2, EventType: timesession.PunchOut, WorkerCredentialRef: "badge-ref", AssignmentRef: "assignment", Source: "KIOSK", DeviceOccurredAt: time.Unix(16, 0), IdempotencyKey: "p2"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Receipts[0].Status != BatchRejected || got.Receipts[1].Status != BatchRejected || len(store.entries) != 2 {
		t.Fatalf("response=%+v", got)
	}
}

func TestTodo_TCLOCK_003_PreparesInThenOutAgainstPendingSession(t *testing.T) {
	store := &batchCommitFake{}
	s := batchService(store)
	got, err := s.SubmitPunches(context.Background(), batchPrincipal(t), BatchRequest{DeviceID: "dev-1", Punches: []BatchPunch{
		{DeviceSequence: 1, EventType: timesession.PunchIn, WorkerCredentialRef: "badge-ref", AssignmentRef: "assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: "p1", Scheduled: true},
		{DeviceSequence: 2, EventType: timesession.PunchOut, WorkerCredentialRef: "badge-ref", AssignmentRef: "assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(16, 0), IdempotencyKey: "p2", Scheduled: true},
	}})
	if err != nil || len(got.Receipts) != 2 || got.Receipts[1].Status != BatchAccepted {
		t.Fatalf("response=%+v err=%v", got, err)
	}
	if len(store.entries) != 2 || store.entries[0].Work == nil || store.entries[1].Work == nil || !store.entries[0].Work.SessionIsNew || store.entries[1].Work.SessionIsNew {
		t.Fatalf("entries=%+v", store.entries)
	}
	if store.entries[0].Work.Session.ID != store.entries[1].Work.Session.ID || store.entries[1].Work.ExpectedRevision != 1 {
		t.Fatalf("pending session was not carried forward: %+v", store.entries)
	}
}

func TestTodo_TCLOCK_003_ReplayReturnsOriginalBeforeSessionResolution(t *testing.T) {
	store := &batchLookupFake{batchCommitFake: &batchCommitFake{}, found: true, receipt: ReceiptRecord{DeviceSequence: 1, Status: string(BatchRejected), Reason: "WORKER_NOT_ON_ROSTER", Payload: []byte(`{"input_digest":"sha256:placeholder"}`)}}
	// Build the exact immutable digest from the request through a first pass.
	s := batchService(store.batchCommitFake)
	in := BatchPunch{DeviceSequence: 1, EventType: timesession.PunchIn, WorkerCredentialRef: "badge-ref", AssignmentRef: "assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: "p1", Scheduled: true}
	store.receipt.Payload = []byte(`{"input_digest":"` + batchInputDigest("tenant", "dev-1", in) + `"}`)
	s.Work = store
	got, err := s.SubmitPunches(context.Background(), batchPrincipal(t), BatchRequest{DeviceID: "dev-1", Punches: []BatchPunch{in}})
	if err != nil || len(got.Receipts) != 1 || got.Receipts[0].Status != BatchDuplicate || len(store.entries) != 1 || store.entries[0].Work != nil {
		t.Fatalf("response=%+v entries=%+v err=%v", got, store.entries, err)
	}
}

func TestTodo_TCLOCK_003_MissingCommittedReceiptFailsClosed(t *testing.T) {
	s := batchService(&batchCommitFake{})
	s.Work = &batchMissingReceiptFake{}
	s.PunchWorkflow = &batchMissingReceiptFake{}
	s.BatchWorkflow = &batchMissingReceiptFake{}
	_, err := s.SubmitPunches(context.Background(), batchPrincipal(t), BatchRequest{DeviceID: "dev-1", Punches: []BatchPunch{{DeviceSequence: 1, EventType: timesession.PunchIn, WorkerCredentialRef: "badge-ref", AssignmentRef: "assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: "p1", Scheduled: true}}})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err=%v, want ErrUnavailable", err)
	}
}

func TestTodo_TCLOCK_003_WorkflowBindingMustMatchPreparedWork(t *testing.T) {
	work := &PunchWork{Session: SessionRecord{ID: "session-1"}, SessionIsNew: true, Observation: ObservationRecord{ID: "observation-1"}}
	entries := []BatchCommitEntry{{Sequence: 1, Work: work}}
	valid := BatchCommitResult{WorkflowBindings: []WorkflowReceiptBinding{{TenantID: "tenant", DeviceSequence: 1, ObservationID: "observation-1", SessionID: "session-1", InstanceID: "00000000-0000-0000-0000-000000000001", WorkflowID: "workflow-1", PlanDigest: "plan-1", StartKey: "start-1", Committed: true, NodeID: "commit_punch", Attempt: 1, InstanceVersion: 1, TraceID: "trace-1"}}}
	if err := validateWorkflowBindings("tenant", entries, valid); err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	for name, mutate := range map[string]func(*WorkflowReceiptBinding){
		"missing commit":     func(b *WorkflowReceiptBinding) { b.Committed = false },
		"forged observation": func(b *WorkflowReceiptBinding) { b.ObservationID = "other" },
		"missing instance":   func(b *WorkflowReceiptBinding) { b.InstanceID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			binding := valid.WorkflowBindings[0]
			mutate(&binding)
			if err := validateWorkflowBindings("tenant", entries, BatchCommitResult{WorkflowBindings: []WorkflowReceiptBinding{binding}}); err == nil {
				t.Fatal("invalid workflow binding accepted")
			}
		})
	}
}

func TestTodo_TCLOCK_003_Bounds(t *testing.T) {
	s := Service{}
	_, err := s.SubmitPunches(context.Background(), nil, BatchRequest{Punches: make([]BatchPunch, maxPunchBatch+1)})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
}

func TestTodo_TCLOCK_003_Integration(t *testing.T) {
	store := &batchCommitFake{}
	got, err := batchService(store).SubmitPunches(context.Background(), batchPrincipal(t), BatchRequest{DeviceID: "dev-1", Punches: []BatchPunch{{DeviceSequence: 1, EventType: timesession.PunchIn, WorkerCredentialRef: "badge-ref", AssignmentRef: "assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: "integration-1"}}})
	if err != nil || len(got.Receipts) != 1 || got.Receipts[0].Status != BatchAccepted || len(store.entries) != 1 {
		t.Fatalf("response=%+v entries=%+v err=%v", got, store.entries, err)
	}
}

func TestTodo_TCLOCK_003_Security(t *testing.T) {
	service := batchService(&batchCommitFake{})
	service.Roster = batchRosterMissingFake{}
	got, err := service.SubmitPunches(context.Background(), batchPrincipal(t), BatchRequest{DeviceID: "dev-1", Punches: []BatchPunch{{DeviceSequence: 1, EventType: timesession.PunchIn, WorkerCredentialRef: "badge-ref", AssignmentRef: "assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: "security-1"}}})
	if err != nil || len(got.Receipts) != 1 || got.Receipts[0].Status != BatchRejected || got.Receipts[0].Reason != "WORKER_NOT_ON_ROSTER" {
		t.Fatalf("response=%+v err=%v", got, err)
	}
}

func TestTodo_TCLOCK_003_Fault(t *testing.T) {
	service := batchService(&batchCommitFake{})
	service.BatchWorkflow = &batchMissingReceiptFake{}
	_, err := service.SubmitPunches(context.Background(), batchPrincipal(t), BatchRequest{DeviceID: "dev-1", Punches: []BatchPunch{{DeviceSequence: 1, EventType: timesession.PunchIn, WorkerCredentialRef: "badge-ref", AssignmentRef: "assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: "fault-1"}}})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err=%v, want unavailable when the durable workflow returns no receipt", err)
	}
}

func TestTodo_TCLOCK_003_Race(t *testing.T) {
	in := BatchPunch{DeviceSequence: 1, EventType: timesession.PunchIn, WorkerCredentialRef: "badge-ref", AssignmentRef: "assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: "race-1"}
	want := batchInputDigest("tenant", "dev-1", in)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := batchInputDigest("tenant", "dev-1", in); got != want {
				t.Errorf("digest=%q want %q", got, want)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_TCLOCK_003_Conformance(t *testing.T) {
	for _, kind := range []timesession.PunchKind{timesession.PunchIn, timesession.PunchOut, timesession.PunchBreakStart, timesession.PunchBreakEnd, timesession.PunchMealStart, timesession.PunchMealEnd, timesession.PunchTransfer} {
		if event, ok := observationEvent(kind); !ok || event == "" {
			t.Fatalf("punch kind %q is not mapped into the signed observation vocabulary", kind)
		}
	}
}

func FuzzTodo_TCLOCK_003(f *testing.F) {
	f.Add(int64(1), "punch-1")
	f.Fuzz(func(t *testing.T, sequence int64, key string) {
		if sequence <= 0 {
			return
		}
		in := BatchPunch{DeviceSequence: sequence, EventType: timesession.PunchIn, WorkerCredentialRef: "worker", AssignmentRef: "assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: key}
		if first, second := batchInputDigest("tenant", "device", in), batchInputDigest("tenant", "device", in); first == "" || first != second {
			t.Fatalf("batch digest is not deterministic: %q %q", first, second)
		}
	})
}
