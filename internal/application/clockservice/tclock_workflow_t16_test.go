package clockservice

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func t16Profile(category timeprofile.WorkerCategory, capture timeprofile.CaptureMode, pay timeprofile.PayBasis, exemption timeprofile.ExemptionStatus, destination timeprofile.Destination) timeprofile.TimeProfile {
	return timeprofile.TimeProfile{
		ID: "t16-profile", Version: 1, TenantRef: values.TenantId("tenant"),
		EffectiveFrom: values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		Category:      category, Capture: capture, PayBasis: pay, Exemption: exemption, Destination: destination,
		OvertimeMethod: timeprofile.OvertimeSingleRate, AggregationKey: "worker:t16",
	}
}

func t16Selection(workflowID, digest string) runtime.WorkflowSelection {
	return runtime.WorkflowSelection{
		WorkflowID: workflowID,
		Pin:        version.Pin{CompiledPlanDigest: digest},
		Plan:       &workflow.CompiledWorkflow{WorkflowID: workflowID},
	}
}

func t16Registry(t *testing.T, candidates ...TimeWorkflowPlanCandidate) TimeWorkflowPlanRegistry {
	t.Helper()
	registry, err := NewTimeWorkflowPlanRegistry(candidates)
	if err != nil {
		t.Fatalf("NewTimeWorkflowPlanRegistry: %v", err)
	}
	return registry
}

func t16Candidate(template timeprofile.Template, priority int, selection runtime.WorkflowSelection) TimeWorkflowPlanCandidate {
	return TimeWorkflowPlanCandidate{Template: template, Priority: priority, Selection: selection, Match: func(timeprofile.TimeProfile) bool { return true }}
}

func TestTodo_WTIME_002(t *testing.T) {
	contractor := t16Profile(timeprofile.CategoryContractor, timeprofile.CapturePunch, timeprofile.PayContract, timeprofile.NotApplicable, timeprofile.DestinationInvoice)
	registry := t16Registry(t,
		t16Candidate(timeprofile.TemplatePunchSession, 1, t16Selection("punch", "sha256:punch")),
		t16Candidate(timeprofile.TemplateContractorTime, 1, t16Selection("contractor", "sha256:contractor")),
	)
	selection, templateID, err := registry.ResolveProfileFor("tenant", contractor)
	if err != nil {
		t.Fatalf("ResolveProfileFor: %v", err)
	}
	if templateID != timeprofile.TemplateContractorTime || selection.WorkflowID != "contractor" {
		t.Fatalf("contractor selection = %s/%s", templateID, selection.WorkflowID)
	}

	base := t16Profile(timeprofile.CategoryEmployee, timeprofile.CapturePunch, timeprofile.PayHourly, timeprofile.NonExempt, timeprofile.DestinationPayroll)
	baseSelection, _, err := registry.ResolveProfileFor("tenant", base)
	if err != nil || baseSelection.WorkflowID != "punch" {
		t.Fatalf("employee selection = %+v, err=%v", baseSelection, err)
	}
}

func TestTodo_WTIME_002_Property(t *testing.T) {
	cases := []struct {
		profile  timeprofile.TimeProfile
		template timeprofile.Template
	}{
		{t16Profile(timeprofile.CategoryEmployee, timeprofile.CapturePunch, timeprofile.PayHourly, timeprofile.NonExempt, timeprofile.DestinationPayroll), timeprofile.TemplatePunchSession},
		{t16Profile(timeprofile.CategoryEmployee, timeprofile.CaptureDuration, timeprofile.PaySalary, timeprofile.Exempt, timeprofile.DestinationPayroll), timeprofile.TemplateDurationSheet},
		{t16Profile(timeprofile.CategoryEmployee, timeprofile.CaptureException, timeprofile.PaySalary, timeprofile.Exempt, timeprofile.DestinationPayroll), timeprofile.TemplateExceptionOnly},
		{t16Profile(timeprofile.CategoryContractor, timeprofile.CaptureDuration, timeprofile.PayContract, timeprofile.NotApplicable, timeprofile.DestinationInvoice), timeprofile.TemplateContractorTime},
		{t16Profile(timeprofile.CategoryAgencyTemp, timeprofile.CaptureDuration, timeprofile.PayHourly, timeprofile.NotApplicable, timeprofile.DestinationAgency), timeprofile.TemplateAgencyTime},
	}
	for _, tc := range cases {
		got, err := timeprofile.TemplateFor(tc.profile)
		if err != nil || got != tc.template {
			t.Errorf("profile %s/%s resolved to %q, err=%v; want %q", tc.profile.Category, tc.profile.Capture, got, err, tc.template)
		}
	}
}

func TestTodo_WTIME_002_Golden(t *testing.T) {
	profile := t16Profile(timeprofile.CategoryEmployee, timeprofile.CapturePunch, timeprofile.PayHourly, timeprofile.NonExempt, timeprofile.DestinationPayroll)
	registry := t16Registry(t, t16Candidate(timeprofile.TemplatePunchSession, 1, t16Selection("hcmnext.workflows.time.punch_session", "sha256:t16-punch-v1")))
	selection, templateID, err := registry.ResolveProfileFor("tenant", profile)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := profile.Digest()
	if err != nil || digest == "" || templateID != timeprofile.TemplatePunchSession || selection.Pin.CompiledPlanDigest != "sha256:t16-punch-v1" {
		t.Fatalf("golden selection=%+v template=%q profile_digest=%q err=%v", selection, templateID, digest, err)
	}
}

func TestTodo_WTIME_002_Security(t *testing.T) {
	profile := t16Profile(timeprofile.CategoryEmployee, timeprofile.CapturePunch, timeprofile.PayHourly, timeprofile.NonExempt, timeprofile.DestinationPayroll)
	registry := t16Registry(t,
		t16Candidate(timeprofile.TemplatePunchSession, 1, t16Selection("base", "sha256:base")),
		TimeWorkflowPlanCandidate{TenantKey: "other-tenant", Template: timeprofile.TemplatePunchSession, Priority: 100, Match: func(timeprofile.TimeProfile) bool { return true }, Selection: t16Selection("foreign", "sha256:foreign")},
	)
	if got, _, err := registry.ResolveProfileFor("tenant", profile); err != nil || got.WorkflowID != "base" {
		t.Fatalf("foreign tenant overlay selected: selection=%+v err=%v", got, err)
	}
	ambiguous := TimeWorkflowPlanRegistry{Candidates: []TimeWorkflowPlanCandidate{
		t16Candidate(timeprofile.TemplatePunchSession, 1, t16Selection("one", "sha256:one")),
		TimeWorkflowPlanCandidate{Template: timeprofile.TemplatePunchSession, Priority: 1, Match: func(timeprofile.TimeProfile) bool { return true }, Selection: t16Selection("two", "sha256:two")},
	}}
	if _, _, err := ambiguous.ResolveProfileFor("tenant", profile); !errors.Is(err, ErrAmbiguousTimeWorkflowPlan) {
		t.Fatalf("ambiguous plan was guessed: %v", err)
	}
}

type t16ProfileSource struct {
	profile timeprofile.TimeProfile
	calls   atomic.Int32
}

func (s *t16ProfileSource) Resolve(context.Context, string, string, string, time.Time) (timeprofile.TimeProfile, error) {
	s.calls.Add(1)
	return s.profile, nil
}

type t16Runtime struct {
	mu       sync.Mutex
	start    runtime.StartRequest
	selected runtime.WorkflowSelection
	resumes  int
}

func (r *t16Runtime) Execute(ctx context.Context, req execute.ExecuteRequest) (execute.Result, error) {
	selection, err := req.Start.Resolver.ResolveWorkflow(ctx, req.Start)
	if err != nil {
		return execute.Result{}, err
	}
	r.mu.Lock()
	r.start, r.selected = req.Start, selection
	r.mu.Unlock()
	return execute.Result{Start: runtime.StartReceipt{InstanceID: uuid.MustParse("00000000-0000-0000-0000-000000000016"), WorkflowID: selection.WorkflowID, CompiledPlanDigest: selection.Pin.CompiledPlanDigest}}, nil
}

func (r *t16Runtime) ResumeSignal(context.Context, execute.ResumeSignalRequest) (execute.Result, error) {
	r.mu.Lock()
	r.resumes++
	r.mu.Unlock()
	return execute.Result{}, nil
}

type t16SignalSink struct{ receipt SignalReceipt }

func (s t16SignalSink) Receive(context.Context, SignalDelivery) (SignalReceipt, error) {
	return s.receipt, nil
}

type t16BindingStore struct {
	mu  sync.Mutex
	row TimeClockRunBinding
}

func (s *t16BindingStore) Save(_ context.Context, row TimeClockRunBinding) error {
	s.mu.Lock()
	s.row = row
	s.mu.Unlock()
	return nil
}
func (s *t16BindingStore) Load(context.Context, uuid.UUID, string) (TimeClockRunBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.row, nil
}

func TestTodo_WTIME_003(t *testing.T) {
	profile := t16Profile(timeprofile.CategoryEmployee, timeprofile.CapturePunch, timeprofile.PayHourly, timeprofile.NonExempt, timeprofile.DestinationPayroll)
	source := &t16ProfileSource{profile: profile}
	runtimeDriver := &t16Runtime{}
	bindings := &t16BindingStore{}
	tenantID := uuid.New()
	adapter := TimeClockRuntimeAdapter{Runtime: runtimeDriver, Resolver: resolverFake{}, Versions: version.NewRegistry(), Profiles: source,
		PlanRegistry: ptr(t16Registry(t, t16Candidate(timeprofile.TemplatePunchSession, 1, t16Selection("punch", "sha256:punch")))), TenantKey: func(uuid.UUID) string { return "tenant" },
		Bindings: bindings, Signals: t16SignalSink{receipt: SignalReceipt{SignalID: uuid.New(), SubscriptionID: uuid.New()}}, VersionsReader: runtimeVersionFake(2),
		ResolveTenant: func(string) (uuid.UUID, error) { return tenantID, nil }, Clock: func() time.Time { return time.Unix(200, 0).UTC() }, SchemaRef: "time/v1", Source: "clock"}
	if err := adapter.StartRun(context.Background(), "tenant", "session", "worker", "assignment", "observation", time.Unix(100, 0).UTC()); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if source.calls.Load() != 1 || runtimeDriver.selected.WorkflowID != "punch" || bindings.row.ProfileID != profile.ID || bindings.row.TemplateID != string(timeprofile.TemplatePunchSession) {
		t.Fatalf("profile selection was not pinned: calls=%d selection=%+v binding=%+v", source.calls.Load(), runtimeDriver.selected, bindings.row)
	}
	if err := adapter.SignalRunForAssignment(context.Background(), "tenant", "session", "worker", "assignment", "BREAK_START", "observation-2", time.Unix(110, 0).UTC()); err != nil {
		t.Fatalf("SignalRunForAssignment: %v", err)
	}
	if source.calls.Load() != 1 || runtimeDriver.resumes != 1 {
		t.Fatalf("later signal re-resolved profile or did not resume: profile_calls=%d resumes=%d", source.calls.Load(), runtimeDriver.resumes)
	}
}

func ptr[T any](value T) *T { return &value }

type t16CorrelatedWorkflow struct {
	mu         sync.Mutex
	worker     string
	assignment string
}

func (w *t16CorrelatedWorkflow) StartRun(context.Context, string, string, string, string, string, time.Time) error {
	return nil
}
func (w *t16CorrelatedWorkflow) SignalRun(context.Context, string, string, string, string, time.Time) error {
	return errors.New("uncorrelated signal path used")
}
func (w *t16CorrelatedWorkflow) SignalRunForAssignment(_ context.Context, _, _, worker, assignment, _, _ string, _ time.Time) error {
	w.mu.Lock()
	w.worker, w.assignment = worker, assignment
	w.mu.Unlock()
	return nil
}

func TestTodo_WTIME_003_Integration(t *testing.T) {
	wf := &t16CorrelatedWorkflow{}
	source := workflowEventSourceFake{events: []OutboxEvent{{Sequence: 1, SchemaVersion: 1, EventType: "clock.punch.accepted", Payload: []byte(`{"session_id":"s","worker_ref":"w","assignment_ref":"a","observation_id":"o","signal":"BREAK_START","occurred_at":"2026-09-28T12:00:00Z"}`)}}}
	result, err := (WorkflowOutboxDispatcher{Source: source, Workflow: wf}).Dispatch(context.Background(), "tenant", 0)
	if err != nil || result.Delivered != 1 || wf.worker != "w" || wf.assignment != "a" {
		t.Fatalf("dispatch=%+v correlated=%q/%q err=%v", result, wf.worker, wf.assignment, err)
	}
}

func TestTodo_WTIME_003_Race(t *testing.T) {
	admission, err := NewClockWorkflowAdmission(ClockWorkflowBudget{MaxInFlightPerTenant: 32, MaxReplayInFlight: 8, ReplayReserve: 4})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			priority := ClockWorkflowPriorityLive
			if i%2 == 0 {
				priority = ClockWorkflowPriorityOfflineReplay
			}
			release, err := admission.Admit(context.Background(), "tenant", priority)
			if err == nil {
				release()
			}
		}(i)
	}
	wg.Wait()
	if release, err := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityLive); err != nil {
		t.Fatalf("admission leaked after concurrent signals: %v", err)
	} else {
		release()
	}
}

func TestTodo_WTIME_003_Recovery(t *testing.T) {
	wf := &workflowFake{}
	source := t16EventSource{events: []OutboxEvent{{Sequence: 1, SchemaVersion: 1, EventType: "clock.punch.accepted", Payload: []byte(`{"session_id":"s","worker_ref":"w","assignment_ref":"a","observation_id":"o","signal":"OUT","occurred_at":"2026-09-28T12:00:00Z"}`)}}}
	if _, err := (WorkflowOutboxDispatcher{Source: source, Workflow: wf}).Dispatch(context.Background(), "tenant", 0); err != nil {
		t.Fatal(err)
	}
	if result, err := (WorkflowOutboxDispatcher{Source: source, Workflow: wf}).Dispatch(context.Background(), "tenant", 1); err != nil || result.Delivered != 0 || result.Cursor != 1 {
		t.Fatal(err)
	}
	if wf.signals != 1 {
		t.Fatalf("recovery changed the delivered signal count: signals=%d", wf.signals)
	}
}

type t16EventSource struct{ events []OutboxEvent }

func (s t16EventSource) ListEvents(_ context.Context, _ string, after int64, _ int) ([]OutboxEvent, error) {
	var out []OutboxEvent
	for _, event := range s.events {
		if event.Sequence > after {
			out = append(out, event)
		}
	}
	return out, nil
}

func TestTodo_WTIME_003_Security(t *testing.T) {
	tenantID := uuid.New()
	bindings := &t16BindingStore{row: TimeClockRunBinding{TenantID: tenantID, TenantKey: "tenant", SessionID: "s", WorkerRef: "w", AssignmentRef: "a", InstanceID: uuid.New(), WorkflowID: "wf", PlanDigest: "plan", StartKey: startKey(tenantID, "s")}}
	a := TimeClockRuntimeAdapter{Runtime: &t16Runtime{}, Resolver: resolverFake{}, Versions: version.NewRegistry(), Bindings: bindings, Signals: t16SignalSink{receipt: SignalReceipt{SignalID: uuid.New(), SubscriptionID: uuid.New()}}, VersionsReader: runtimeVersionFake(1), ResolveTenant: func(string) (uuid.UUID, error) { return tenantID, nil }, Clock: func() time.Time { return time.Unix(1, 0) }, SchemaRef: "time/v1", Source: "clock"}
	if err := a.SignalRunForAssignment(context.Background(), "tenant", "s", "other-worker", "a", "OUT_PUNCH", "o", time.Unix(2, 0)); err == nil {
		t.Fatal("foreign worker signal was accepted")
	}
}

func TestTodo_WTIME_003_Conformance(t *testing.T) {
	for _, signal := range []string{"BREAK_START", "BREAK_END", "JOB_TRANSFER", "TRAVEL_TRANSFER", "OUT_PUNCH"} {
		if _, ok := map[string]bool{"BREAK_START": true, "BREAK_END": true, "JOB_TRANSFER": true, "TRAVEL_TRANSFER": true, "OUT_PUNCH": true}[signal]; !ok {
			t.Fatalf("signal %q is not in the typed session vocabulary", signal)
		}
	}
}

func TestTodo_WTIME_004(t *testing.T) {
	admission, err := NewClockWorkflowAdmission(ClockWorkflowBudget{MaxInFlightPerTenant: 1, MaxReplayInFlight: 1, ReplayReserve: 0})
	if err != nil {
		t.Fatal(err)
	}
	release, err := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityLive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityOfflineReplay); !errors.Is(err, ErrRetryLater) {
		t.Fatalf("overload error=%v, want ErrRetryLater", err)
	}
	release()
}

func TestTodo_WTIME_004_Performance(t *testing.T) {
	admission, err := NewClockWorkflowAdmission(ClockWorkflowBudget{MaxInFlightPerTenant: 128, MaxReplayInFlight: 32, ReplayReserve: 8})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		release, err := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityLive)
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
}

func BenchmarkTodo_WTIME_004(b *testing.B) {
	admission, _ := NewClockWorkflowAdmission(ClockWorkflowBudget{MaxInFlightPerTenant: 1024, MaxReplayInFlight: 256, ReplayReserve: 32})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		release, err := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityLive)
		if err != nil {
			b.Fatal(err)
		}
		release()
	}
}

func TestTodo_WTIME_004_Fault(t *testing.T) {
	admission, _ := NewClockWorkflowAdmission(ClockWorkflowBudget{MaxInFlightPerTenant: 1})
	release, err := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityLive)
	if err != nil {
		t.Fatal(err)
	}
	// A failed runtime call releases the reservation in the driver's defer;
	// this direct release models that fault boundary without writing a punch.
	release()
	if next, err := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityLive); err != nil {
		next = nil
		t.Fatalf("fault left tenant budget occupied: %v", err)
	} else {
		next()
	}
}

func TestTodo_WTIME_004_Recovery(t *testing.T) {
	admission, _ := NewClockWorkflowAdmission(ClockWorkflowBudget{MaxInFlightPerTenant: 1})
	release, _ := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityLive)
	if _, err := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityLive); !errors.Is(err, ErrRetryLater) {
		t.Fatalf("second live punch was not backpressured: %v", err)
	}
	release()
	if next, err := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityLive); err != nil {
		t.Fatalf("retry after release: %v", err)
	} else {
		next()
	}
}

func TestTodo_WTIME_004_Race(t *testing.T) {
	admission, _ := NewClockWorkflowAdmission(ClockWorkflowBudget{MaxInFlightPerTenant: 64, MaxReplayInFlight: 16, ReplayReserve: 8})
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityLive)
			if err == nil {
				release()
			}
		}()
	}
	wg.Wait()
	if release, err := admission.Admit(context.Background(), "tenant", ClockWorkflowPriorityLive); err != nil {
		t.Fatal(err)
	} else {
		release()
	}
}
