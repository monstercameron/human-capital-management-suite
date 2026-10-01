package agentrun

import (
	"context"
	"errors"
	"testing"
	"time"
)

type receiptOwner struct {
	observation EffectObservation
	err         error
	calls       int
}

func (o *receiptOwner) Reconcile(context.Context, AgentTask, PlanStep) (EffectObservation, error) {
	o.calls++
	return o.observation, o.err
}

func abandonedTask(t *testing.T, tier Tier) (*Runtime, AgentTask) {
	t.Helper()
	s := step("effect", StepSubmit, tier)
	if tier == TierRead {
		s.Type = StepRead
	}
	runtime, task := newTestRuntime(t, planForTest(t, s))
	var err error
	task, err = runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	prior := task.Version
	task.Plan.Steps[0].State = StepRunning
	task.Plan.Steps[0].Attempt = 1
	task.WorkerLease = "abandoned-worker"
	task.Version++
	if err := runtime.store.Save(context.Background(), task, prior); err != nil {
		t.Fatal(err)
	}
	return runtime, task
}

func TestTodo_AGENT_016_RecoveryEffectReceipt(t *testing.T) {
	ctx := context.Background()
	for _, applied := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "applied"}[applied], func(t *testing.T) {
			runtime, task := abandonedTask(t, TierSubmitGoverned)
			recovered, err := runtime.RecoverStale(ctx, task.ID, task.Version, testNow.Add(time.Minute))
			if err != nil || recovered.State != StatePaused || recovered.FailureCode != "AMBIGUOUS_EFFECT" || recovered.Plan.Steps[0].State != StepRunning || recovered.WorkerLease != "" {
				t.Fatalf("ambiguous recovery = %+v, %v", recovered, err)
			}
			if _, err := runtime.Resume(ctx, task.ID, recovered.Version, testNow.Add(2*time.Minute)); !errors.Is(err, ErrReconciliationRequired) {
				t.Fatalf("resume uncertain effect = %v", err)
			}
			status := EffectNotApplied
			if applied {
				status = EffectApplied
			}
			owner := &receiptOwner{observation: EffectObservation{Status: status, Result: StepResult{Ref: "owner:receipt", Digest: digestText("receipt")}}}
			resolved, err := runtime.ReconcileEffect(ctx, task.ID, recovered.Version, owner, testNow.Add(3*time.Minute))
			if err != nil || owner.calls != 1 {
				t.Fatalf("owner reconciliation = %+v, %v", resolved, err)
			}
			if applied {
				if resolved.State != StateCompleted || resolved.Plan.Steps[0].ResultRef != "owner:receipt" || resolved.Plan.Steps[0].Attempt != 1 {
					t.Fatalf("applied receipt replayed or lost: %+v", resolved)
				}
			} else if resolved.State != StatePaused || resolved.Plan.Steps[0].State != StepPending || resolved.FailureCode != "" {
				t.Fatalf("proven absent effect = %+v", resolved)
			}
		})
	}
}

func TestTodo_AGENT_016_FaultReconciliationFailsClosed(t *testing.T) {
	ctx := context.Background()
	runtime, task := abandonedTask(t, TierExternalWrite)
	paused, err := runtime.RecoverStale(ctx, task.ID, task.Version, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []*receiptOwner{{err: errors.New("owner unavailable")}, {}, {observation: EffectObservation{Status: EffectApplied}}} {
		if _, err := runtime.ReconcileEffect(ctx, task.ID, paused.Version, owner, testNow.Add(2*time.Minute)); err == nil {
			t.Fatal("uncertain or evidence-free receipt advanced task")
		}
		stored, _ := runtime.GetTask(ctx, task.ID)
		if stored.Version != paused.Version || stored.State != StatePaused || stored.Plan.Steps[0].Attempt != 1 {
			t.Fatalf("failed reconciliation mutated checkpoint: %+v", stored)
		}
	}
}

func TestTodo_AGENT2_011_ExpiryBeforeWork(t *testing.T) {
	ctx := context.Background()
	runtime, task := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead)))
	task, err := runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	exec := &fakeExecutor{}
	expired, err := runtime.ExecuteNext(ctx, task.ID, task.Version, exec, nil, task.ExpiresAt)
	if !errors.Is(err, ErrTerminal) || expired.State != StateExpired || len(exec.steps) != 0 {
		t.Fatalf("work at deadline = %+v, %v, calls=%d", expired, err, len(exec.steps))
	}
	runtime, task = newTestRuntime(t, planForTest(t, step("ask", StepAskUser, TierRead)))
	task, _ = runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, testNow)
	task, _ = runtime.ParkStep(ctx, task.ID, task.Version, WakeCondition{Kind: WakeUserReply, Key: "reply"}, testNow)
	wake, err := runtime.Wake(ctx, task.ID, WakeEvent{ID: "late", Kind: WakeUserReply, Key: "reply", OccurredAt: task.ExpiresAt})
	if err != nil || wake.Accepted || wake.Task.State != StateExpired || wake.Task.LastWake != nil {
		t.Fatalf("late wake = %+v, %v", wake, err)
	}
}

func TestTodo_AGENT2_011_RecoveryPreservesWakeReceipt(t *testing.T) {
	ctx := context.Background()
	runtime, task := newTestRuntime(t, planForTest(t, step("ask", StepAskUser, TierRead)))
	task, _ = runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, testNow)
	task, _ = runtime.ParkStep(ctx, task.ID, task.Version, WakeCondition{Kind: WakeUserReply, Key: "reply"}, testNow)
	event := WakeEvent{ID: "reply-post", Kind: WakeUserReply, Key: "reply", PayloadRef: "chat:durable-reply", OccurredAt: testNow.Add(time.Minute)}
	accepted, err := runtime.Wake(ctx, task.ID, event)
	if err != nil || !accepted.Accepted || accepted.Task.LastWake == nil || *accepted.Task.LastWake != event {
		t.Fatalf("accepted receipt = %+v, %v", accepted, err)
	}
	accepted.Task.LastWake.PayloadRef = "mutated"
	stored, _ := runtime.GetTask(ctx, task.ID)
	if stored.LastWake == nil || stored.LastWake.PayloadRef != event.PayloadRef {
		t.Fatal("returned wake shares persisted receipt memory")
	}
}

func TestTodo_AGENT2_013_SecurityMultipleSourceRevocation(t *testing.T) {
	ctx := context.Background()
	runtime, task := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead)))
	task.Ledger.Entries = append(task.Ledger.Entries, LedgerEntry{Kind: "STEP_RESULT", Ref: "derived:both", SourceID: "source-a", SourceIDs: []string{"source-a", "source-b"}, Taint: []string{"EXTERNAL_UNTRUSTED"}})
	task.Version++
	if err := runtime.store.Save(ctx, task, task.Version-1); err != nil {
		t.Fatal(err)
	}
	task, err := runtime.AddModelNote(ctx, task.ID, task.Version, "note:derived", "sha256:derived", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Ledger.Entries[len(task.Ledger.Entries)-1].SourceIDs) != 2 {
		t.Fatal("derived model note lost source dependencies")
	}
	if _, err := runtime.RebuildContext(ctx, task.ID, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("source context without current owner = %v", err)
	}
	view, err := runtime.RebuildContext(ctx, task.ID, mapReader{"source-a": {SourceID: "source-a", ValueRef: "fresh-a"}, "source-b": {SourceID: "source-b", Revoked: true}})
	if err != nil || len(view.Results) != 0 {
		t.Fatalf("revoking second source kept derived data: %+v, %v", view, err)
	}
	if _, err := runtime.RebuildContext(ctx, task.ID, mapReader{"source-a": {SourceID: "wrong-source"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("owner source substitution = %v", err)
	}
	revoked, err := runtime.RevokeSource(ctx, task.ID, "source-b", task.Version, testNow)
	if err != nil || !revoked.Ledger.Entries[len(revoked.Ledger.Entries)-1].Revoked {
		t.Fatalf("durable second-source revocation = %+v, %v", revoked, err)
	}
}

func TestTodo_AGENT2_020_SecurityWakeVoidsApproval(t *testing.T) {
	ctx := context.Background()
	runtime, task := newTestRuntime(t, planForTest(t, step("submit", StepSubmit, TierSubmitGoverned)))
	task, _ = runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, testNow)
	task, err := runtime.ExecuteNext(ctx, task.ID, task.Version, &fakeExecutor{}, nil, testNow)
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatal(err)
	}
	digest := task.Plan.Steps[0].ApprovalDigest
	approved, err := runtime.ApproveStep(ctx, task.ID, "submit", digest, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := runtime.PauseAuthority(ctx, task.ID, approved.Version, PauseGrantRevoked, testNow.Add(time.Minute))
	if err != nil || paused.State != StatePaused || paused.Plan.Steps[0].Approved || paused.Plan.Steps[0].ApprovalDigest != "" || paused.WorkerLease != "" {
		t.Fatalf("authority pause retained approval: %+v, %v", paused, err)
	}
	events, err := runtime.store.ListEvents(ctx, task.ID)
	if err != nil || events[len(events)-1].Outcome != "VOIDED_AUTHORITY" || events[len(events)-1].ApprovalDigest != digest {
		t.Fatalf("voided approval evidence = %+v, %v", events, err)
	}
	if _, err := runtime.ApproveStep(ctx, task.ID, "submit", digest, paused.Version, testNow.Add(2*time.Minute)); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("voided approval replay = %v", err)
	}
	resumed, err := runtime.Resume(ctx, task.ID, paused.Version, testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := runtime.ExecuteNext(ctx, task.ID, resumed.Version, &fakeExecutor{}, nil, testNow.Add(3*time.Minute))
	if !errors.Is(err, ErrApprovalRequired) || fresh.Plan.Steps[0].ApprovalDigest == digest {
		t.Fatalf("voided approval digest reused: %+v,%v", fresh, err)
	}
	if _, err := runtime.ApproveStep(ctx, task.ID, "submit", digest, fresh.Version, testNow.Add(4*time.Minute)); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("old approval accepted against fresh task version: %v", err)
	}
}
