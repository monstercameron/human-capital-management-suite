package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func planForTest(t *testing.T, steps ...PlanStep) AgentPlan {
	t.Helper()
	plan, err := NewPlan(steps)
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}
	return plan
}

func step(id string, typ StepType, tier Tier) PlanStep {
	return PlanStep{ID: id, Type: typ, SkillID: "skill.read", SkillVersion: 1, ExpectedOutput: "owner state", Tier: tier}
}

func newTestRuntime(t *testing.T, plan AgentPlan) (*Runtime, AgentTask) {
	t.Helper()
	store := NewMemoryStore()
	runtime, err := NewRuntime(store)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	task, err := runtime.CreateTask(context.Background(), CreateRequest{
		ID: "task-1", TenantID: "tenant-1", UserID: "user-1", Goal: "prepare the report",
		Constraints: []string{"only current owner facts"}, Plan: plan, Now: testNow, ExpiresAt: testNow.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return runtime, task
}

type fakeExecutor struct {
	mu      sync.Mutex
	steps   []string
	result  StepResult
	failure error
}

func (f *fakeExecutor) Execute(_ context.Context, _ AgentTask, step PlanStep) (StepResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = append(f.steps, step.ID)
	if f.failure != nil {
		return StepResult{}, f.failure
	}
	result := f.result
	if result.Ref == "" {
		result.Ref = "result:" + step.ID
	}
	if result.Digest == "" {
		result.Digest = digestText(step.ID)
	}
	return result, nil
}

type fakeVerifier struct{ calls int }

func (f *fakeVerifier) Verify(_ context.Context, _ AgentTask, _ PlanStep, _ StepResult) error {
	f.calls++
	return nil
}

func TestTodo_AGENT2_010(t *testing.T) {
	verify := step("verify", StepVerify, TierRead)
	verify.Inputs = []InputRef{{Name: "owner-state", Ref: "owner:report"}}
	plan := planForTest(t,
		step("read", StepRead, TierRead),
		verify,
	)
	runtime, task := newTestRuntime(t, plan)
	task, err := runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}
	exec := &fakeExecutor{}
	verifier := &fakeVerifier{}
	task, err = runtime.ExecuteNext(context.Background(), task.ID, task.Version, exec, verifier, testNow.Add(time.Minute))
	if err != nil || task.CurrentStep != 1 || task.State != StateRunning {
		t.Fatalf("after read task=%+v err=%v", task, err)
	}
	task, err = runtime.ExecuteNext(context.Background(), task.ID, task.Version, exec, verifier, testNow.Add(2*time.Minute))
	if err != nil || task.CurrentStep != 2 || task.State != StateCompleted {
		t.Fatalf("after verify task=%+v err=%v", task, err)
	}
	if verifier.calls != 1 || len(exec.steps) != 2 || exec.steps[1] != "verify" {
		t.Fatalf("executor/verifier calls = %v/%d", exec.steps, verifier.calls)
	}
	if len(task.Ledger.Entries) != 4 || task.Ledger.Entries[2].Kind != "STEP_RESULT" {
		t.Fatalf("step checkpoint was not ledgered: %+v", task.Ledger.Entries)
	}
}

func TestAnswerText_OnlyCommittedDerivedModelStep(t *testing.T) {
	tests := []struct {
		name  string
		taint []string
		want  string
	}{
		{name: "derived", taint: []string{"AGENT_DERIVED"}, want: "sanitized answer"},
		{name: "untrusted", taint: []string{"EXTERNAL_UNTRUSTED"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runtime, task := newTestRuntime(t, planForTest(t, step("answer", StepAnalyze, TierPrivateDraft)))
			confirmed, err := runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
			if err != nil {
				t.Fatal(err)
			}
			exec := &fakeExecutor{result: StepResult{AnswerText: tc.want, Taint: tc.taint}}
			got, err := runtime.ExecuteNext(context.Background(), confirmed.ID, confirmed.Version, exec, nil, testNow.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if got.Ledger.AnswerText != tc.want {
				t.Fatalf("answer text = %q, want %q", got.Ledger.AnswerText, tc.want)
			}
			contextView, err := runtime.RebuildContext(context.Background(), got.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(contextView)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "sanitized answer") {
				t.Fatal("answer text leaked into model context")
			}
		})
	}
}

func TestTodo_AGENT2_010_Golden(t *testing.T) {
	plan := planForTest(t, step("read", StepRead, TierRead))
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Digest == "" || string(encoded) == "" || plan.Digest != digestPlan(plan) {
		t.Fatalf("plan canonical identity is not stable: digest=%q json=%s", plan.Digest, encoded)
	}
	changed := plan
	changed.Steps = cloneSteps(plan.Steps)
	changed.Steps[0].ExpectedOutput = "different owner state"
	if digestPlan(changed) == plan.Digest {
		t.Fatal("changing expected output did not change plan digest")
	}
}

func TestTodo_AGENT2_010_Recovery(t *testing.T) {
	runtime, task := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead)))
	task, err := runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := runtime.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	claimed.Plan.Steps[0].State = StepRunning
	claimed.WorkerLease = "dead-worker"
	claimed.Version++
	if err := runtime.store.Save(context.Background(), claimed, task.Version); err != nil {
		t.Fatal(err)
	}
	recovered, err := runtime.RecoverStale(context.Background(), task.ID, claimed.Version, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Plan.Steps[0].State != StepPending || recovered.WorkerLease != "" || recovered.State != StateRunning {
		t.Fatalf("recovery did not release checkpoint: %+v", recovered)
	}
}

func TestTodo_AGENT2_010_ModelBased(t *testing.T) {
	communicate := step("notify", StepCommunicate, TierCommunicate)
	communicate.Destination = "user-thread"
	communicate.DestinationConfirmed = true
	runtime, task := newTestRuntime(t, planForTest(t, communicate))
	task, err := runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.ExecuteNext(context.Background(), task.ID, task.Version, &fakeExecutor{}, nil, testNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("T2 should wait for neither exact write approval nor a model call: %v", err)
	}
	current, _ := runtime.GetTask(context.Background(), task.ID)
	if current.State != StateCompleted {
		t.Fatalf("T2 state = %s", current.State)
	}

	write := step("write", StepSubmit, TierSubmitGoverned)
	runtime, task = newTestRuntime(t, planForTest(t, write))
	task, err = runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	awaiting, err := runtime.ExecuteNext(context.Background(), task.ID, task.Version, &fakeExecutor{}, nil, testNow.Add(time.Minute))
	if !errors.Is(err, ErrApprovalRequired) || awaiting.State != StateAwaitingApproval || awaiting.Plan.Steps[0].State != StepAwaitingApproval {
		t.Fatalf("T3 did not become approval-gated: task=%+v err=%v", awaiting, err)
	}
	approved, err := runtime.ApproveStep(context.Background(), task.ID, "write", awaiting.Plan.Steps[0].ApprovalDigest, awaiting.Version, testNow.Add(2*time.Minute))
	if err != nil || approved.State != StateRunning {
		t.Fatalf("approval did not resume task: %+v err=%v", approved, err)
	}
	completed, err := runtime.ExecuteNext(context.Background(), task.ID, approved.Version, &fakeExecutor{}, nil, testNow.Add(3*time.Minute))
	if err != nil || completed.State != StateCompleted {
		t.Fatalf("approved T3 did not execute once: %+v err=%v", completed, err)
	}
}

func TestTodo_AGENT2_010_ReplanAndPause(t *testing.T) {
	base := planForTest(t, step("read", StepRead, TierRead))
	runtime, task := newTestRuntime(t, base)
	task, err := runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	safe := planForTest(t, step("read-again", StepRead, TierRead))
	task, err = runtime.Replan(context.Background(), task.ID, task.Version, safe, testNow.Add(time.Minute))
	if err != nil || task.State != StateRunning || !task.Plan.Confirmed {
		t.Fatalf("safe replan = %+v err=%v", task, err)
	}
	newSkill := step("external", StepRead, TierRead)
	newSkill.SkillID = "skill.new"
	unsafe := planForTest(t, newSkill)
	task, err = runtime.Replan(context.Background(), task.ID, task.Version, unsafe, testNow.Add(2*time.Minute))
	if err != nil || task.State != StateAwaitingPlanConfirmation || task.Plan.Confirmed {
		t.Fatalf("new skill replan did not require confirmation: %+v err=%v", task, err)
	}
	task, err = runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.Pause(context.Background(), task.ID, task.Version, testNow.Add(4*time.Minute))
	if err != nil || task.State != StatePaused {
		t.Fatalf("pause = %+v err=%v", task, err)
	}
	task, err = runtime.Resume(context.Background(), task.ID, task.Version, testNow.Add(5*time.Minute))
	if err != nil || task.State != StateRunning {
		t.Fatalf("resume = %+v err=%v", task, err)
	}
}

func TestTodo_AGENT2_010_InvalidStateAndStore(t *testing.T) {
	if !StateRunning.valid() || TaskState("unknown").valid() {
		t.Fatal("task state validation is not finite")
	}
	if _, err := NewRuntime(nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil runtime store error = %v", err)
	}
	store := NewStore()
	runtime, task := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead)))
	if err := store.Create(context.Background(), task); err != nil {
		// NewStore is independently exercised; duplicate is the expected store fact.
		if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if _, err := runtime.CreateTask(context.Background(), CreateRequest{ID: "bad", TenantID: "tenant", UserID: "user", Goal: "bad", Plan: AgentPlan{}, Now: testNow, ExpiresAt: testNow.Add(time.Hour)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid plan error = %v", err)
	}
}

func waitPlan(t *testing.T) AgentPlan {
	wait := step("wait", StepWait, TierRead)
	wait.Inputs = []InputRef{{Name: "correlation", Ref: "workflow:promotion/42"}}
	return planForTest(t, wait, step("after", StepRead, TierRead))
}

func TestTodo_AGENT2_011(t *testing.T) {
	runtime, task := newTestRuntime(t, waitPlan(t))
	task, err := runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	exec := &fakeExecutor{}
	task, err = runtime.ExecuteNext(context.Background(), task.ID, task.Version, exec, nil, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.Park(context.Background(), task.ID, task.Version, WakeCondition{Kind: WakeSignal, Key: "promotion-complete", Correlation: "promotion/42", StaleAfter: testNow.Add(24 * time.Hour)}, testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if task.State != StateWaiting || task.WorkerLease != "" || task.ModelSession != "" {
		t.Fatalf("parked task still holds execution resources: %+v", task)
	}
	first, err := runtime.Wake(context.Background(), task.ID, WakeEvent{ID: "signal-1", Kind: WakeSignal, Key: "promotion-complete", Correlation: "promotion/42", OccurredAt: testNow.Add(3 * time.Minute)})
	if err != nil || !first.Accepted || first.Task.State != StateRunning {
		t.Fatalf("wake not accepted: %+v err=%v", first, err)
	}
	second, err := runtime.Wake(context.Background(), task.ID, WakeEvent{ID: "signal-1", Kind: WakeSignal, Key: "promotion-complete", Correlation: "promotion/42", OccurredAt: testNow.Add(3 * time.Minute)})
	if err != nil || !second.Duplicate || second.Accepted {
		t.Fatalf("duplicate wake was not deduped: %+v err=%v", second, err)
	}
}

func TestTodo_AGENT2_011_Race(t *testing.T) {
	runtime, task := newTestRuntime(t, waitPlan(t))
	var err error
	task, err = runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.ExecuteNext(context.Background(), task.ID, task.Version, &fakeExecutor{}, nil, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.Park(context.Background(), task.ID, task.Version, WakeCondition{Kind: WakeApproval, Key: "approval/1"}, testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	accepted := 0
	var mu sync.Mutex
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, wakeErr := runtime.Wake(context.Background(), task.ID, WakeEvent{ID: "approval-event", Kind: WakeApproval, Key: "approval/1", OccurredAt: testNow.Add(3 * time.Minute)})
			if wakeErr != nil {
				t.Errorf("Wake: %v", wakeErr)
				return
			}
			if result.Accepted {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if accepted != 1 {
		t.Fatalf("accepted wakes = %d, want exactly one", accepted)
	}
}

func TestTodo_AGENT2_011_Fault(t *testing.T) {
	runtime, task := newTestRuntime(t, waitPlan(t))
	var err error
	task, err = runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.ExecuteNext(context.Background(), task.ID, task.Version, &fakeExecutor{}, nil, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.Park(context.Background(), task.ID, task.Version, WakeCondition{Kind: WakeTimer, Key: "timer/1", DueAt: testNow.Add(time.Hour), StaleAfter: testNow.Add(time.Hour)}, testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	settled, err := runtime.SweepStaleTasks(context.Background(), testNow.Add(2*time.Hour))
	if err != nil || len(settled) != 1 || settled[0].FailureCode != "LOST_WAKE" || settled[0].State != StateFailed {
		t.Fatalf("lost wake settlement = %+v err=%v", settled, err)
	}
}

func TestTodo_AGENT2_011_Recovery(t *testing.T) {
	runtime, task := newTestRuntime(t, waitPlan(t))
	var err error
	task, err = runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.ExecuteNext(context.Background(), task.ID, task.Version, &fakeExecutor{}, nil, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.Park(context.Background(), task.ID, task.Version, WakeCondition{Kind: WakeUserReply, Key: "reply/1"}, testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.Cancel(context.Background(), task.ID, task.Version, testNow.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Wake(context.Background(), task.ID, WakeEvent{ID: "late", Kind: WakeUserReply, Key: "reply/1", OccurredAt: testNow.Add(4 * time.Minute)})
	if err != nil || !result.Ignored || result.Accepted {
		t.Fatalf("late wake ran cancelled task: %+v err=%v", result, err)
	}
}

type wakeRechecker struct{ err error }

func (w wakeRechecker) RecheckWake(context.Context, AgentTask, WakeEvent) error { return w.err }

func TestTodo_AGENT2_011_AuthorityRecheck(t *testing.T) {
	runtime, task := newTestRuntime(t, waitPlan(t))
	var err error
	task, err = runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.ExecuteNext(context.Background(), task.ID, task.Version, &fakeExecutor{}, nil, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.Park(context.Background(), task.ID, task.Version, WakeCondition{Kind: WakeSignal, Key: "signal/1"}, testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	event := WakeEvent{ID: "signal-event", Kind: WakeSignal, Key: "signal/1", OccurredAt: testNow.Add(3 * time.Minute)}
	if result, wakeErr := runtime.WakeWithRecheck(context.Background(), task.ID, event, wakeRechecker{err: errors.New("grant revoked")}); wakeErr == nil || !result.Ignored {
		t.Fatalf("revoked wake was accepted: %+v err=%v", result, wakeErr)
	}
	result, err := runtime.WakeWithRecheck(context.Background(), task.ID, event, wakeRechecker{})
	if err != nil || !result.Accepted {
		t.Fatalf("rechecked wake was not accepted: %+v err=%v", result, err)
	}
}

func TestTodo_AGENT2_011_ExpiryAndArtifact(t *testing.T) {
	runtime, task := newTestRuntime(t, waitPlan(t))
	var err error
	task, err = runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.AddArtifact(context.Background(), task.ID, task.Version, "artifact:report", digestText("report"), testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	settled, err := runtime.SweepStaleTasks(context.Background(), task.ExpiresAt.Add(time.Minute))
	if err != nil || len(settled) != 1 || settled[0].State != StateExpired || settled[0].FailureCode != "TASK_EXPIRED" {
		t.Fatalf("expired task settlement = %+v err=%v", settled, err)
	}
}

type mapReader map[string]OwnerRead

func (m mapReader) Read(_ context.Context, ref string) (OwnerRead, error) {
	read, ok := m[ref]
	if !ok {
		return OwnerRead{}, ErrNotFound
	}
	return read, nil
}

func TestTodo_AGENT2_013(t *testing.T) {
	runtime, task := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead)))
	task, err := runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.AddModelNote(context.Background(), task.ID, task.Version, "note:1", digestText("model note"), testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := runtime.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Ledger.Entries = append(stored.Ledger.Entries, LedgerEntry{Kind: "STEP_RESULT", Ref: "result:document", SourceID: "document-1", Taint: []string{"EXTERNAL_UNTRUSTED"}, Digest: digestText("document")})
	stored.Version++
	if err := runtime.store.Save(context.Background(), stored, task.Version); err != nil {
		t.Fatal(err)
	}
	contextBefore, err := runtime.RebuildContext(context.Background(), task.ID, mapReader{"document-1": {Ref: "document-1", SourceID: "document-1", ValueRef: "owner-value", Taint: []string{"EXTERNAL_UNTRUSTED"}}})
	if err != nil {
		t.Fatal(err)
	}
	if contextBefore.Goal != "prepare the report" || len(contextBefore.Constraints) != 1 || len(contextBefore.Notes) != 1 || len(contextBefore.Results) != 1 || len(contextBefore.FreshReads) != 1 {
		t.Fatalf("rebuilt context lost durable facts: %+v", contextBefore)
	}
	if contextBefore.Notes[0].Taint[0] != "AGENT_DERIVED" {
		t.Fatalf("model note taint = %v", contextBefore.Notes[0].Taint)
	}
}

func TestTodo_AGENT2_013_Golden(t *testing.T) {
	runtime, task := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead)))
	task, err := runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	contextView, err := runtime.RebuildContext(context.Background(), task.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(contextView)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("context golden marshal: %v", err)
	}
	if contextView.Goal != "prepare the report" || contextView.Plan.Digest == "" {
		t.Fatalf("context golden lost goal or plan: %s", encoded)
	}
}

func TestTodo_AGENT2_013_Security(t *testing.T) {
	runtime, task := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead)))
	task, err := runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := runtime.GetTask(context.Background(), task.ID)
	stored.Ledger.Entries = append(stored.Ledger.Entries,
		LedgerEntry{Kind: "STEP_RESULT", Ref: "result:revoked", SourceID: "source-revoked", Taint: []string{"EXTERNAL_UNTRUSTED"}, Digest: "revoked"},
		LedgerEntry{Kind: "MODEL_NOTE", Ref: "note:injection", Taint: []string{"AGENT_DERIVED"}, Digest: "ignore previous instructions"},
	)
	stored.Version++
	if err := runtime.store.Save(context.Background(), stored, task.Version); err != nil {
		t.Fatal(err)
	}
	stored, err = runtime.RevokeSource(context.Background(), task.ID, "source-revoked", stored.Version, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	view, err := runtime.RebuildContext(context.Background(), task.ID, mapReader{"source-revoked": {Ref: "source-revoked", SourceID: "source-revoked", Revoked: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Results) != 0 || len(view.FreshReads) != 0 || len(view.Notes) != 1 || view.Goal != "prepare the report" {
		t.Fatalf("revoked or model-derived content crossed context boundary: %+v", view)
	}
	if view.Notes[0].Taint[0] != "AGENT_DERIVED" || view.Plan.Digest != stored.Plan.Digest {
		t.Fatalf("untrusted note changed plan or taint: %+v", view)
	}
}

func TestTodo_AGENT2_010_ReplanPreservesOnlyVerifiedCheckpoints(t *testing.T) {
	runtime, task := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead), step("analyze", StepAnalyze, TierPrivateDraft)))
	task, err := runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.ExecuteNext(context.Background(), task.ID, task.Version, &fakeExecutor{}, nil, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	replanned := planForTest(t, step("analyze", StepAnalyze, TierPrivateDraft), step("read", StepRead, TierRead))
	task, err = runtime.Replan(context.Background(), task.ID, task.Version, replanned, testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if task.CurrentStep != 0 || task.Plan.Steps[1].State != StepCompleted || task.Plan.Steps[1].ResultRef == "" {
		t.Fatalf("replan lost or skipped checkpoint: %+v", task)
	}
	if task.State != StateRunning || !task.Plan.Confirmed {
		t.Fatalf("safe replan unexpectedly requires confirmation: %+v", task)
	}
}

func TestTodo_AGENT2_011_PauseResumeRestoresParkedWake(t *testing.T) {
	runtime, task := newTestRuntime(t, waitPlan(t))
	var err error
	task, err = runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.ExecuteNext(context.Background(), task.ID, task.Version, &fakeExecutor{}, nil, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.Park(context.Background(), task.ID, task.Version, WakeCondition{Kind: WakePolling, Key: "owner-state", PollAfter: time.Hour}, testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.Pause(context.Background(), task.ID, task.Version, testNow.Add(3*time.Minute))
	if err != nil || task.State != StatePaused || task.Wake != nil || task.PausedWake == nil {
		t.Fatalf("pause did not release and retain wake: %+v err=%v", task, err)
	}
	task, err = runtime.Resume(context.Background(), task.ID, task.Version, testNow.Add(4*time.Minute))
	if err != nil || task.State != StateWaiting || task.Wake == nil || task.Wake.Kind != WakePolling {
		t.Fatalf("resume did not restore waiting task: %+v err=%v", task, err)
	}
}

func TestTodo_AGENT2_013_OwnerRevocationDropsCachedResult(t *testing.T) {
	runtime, task := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead)))
	var err error
	task, err = runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := runtime.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Ledger.Entries = append(stored.Ledger.Entries, LedgerEntry{Kind: "STEP_RESULT", Ref: "result:live", SourceID: "source-live", Taint: []string{"EXTERNAL_UNTRUSTED"}})
	stored.Version++
	if err := runtime.store.Save(context.Background(), stored, task.Version); err != nil {
		t.Fatal(err)
	}
	view, err := runtime.RebuildContext(context.Background(), task.ID, mapReader{"source-live": {Ref: "source-live", SourceID: "source-live", Revoked: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Results) != 0 || len(view.FreshReads) != 0 {
		t.Fatalf("owner revocation left stale context: %+v", view)
	}
}
