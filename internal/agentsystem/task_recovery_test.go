package agentsystem

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

func TestTodo_AGENT2_020_SecurityExecutionVoidsRevokedApproval(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	task := f.start(t, "revoked-approved-step", planStep("update", agentrun.StepSubmit, "skill.update", agentrun.TierSubmitGoverned))
	awaiting, err := f.runner.Step(context.Background(), task.ID, ModeOnBehalfOf)
	if !errors.Is(err, agentrun.ErrApprovalRequired) {
		t.Fatal(err)
	}
	digest := awaiting.Plan.Steps[0].ApprovalDigest
	approved, err := f.runner.Runtime.ApproveStep(context.Background(), task.ID, "update", digest, awaiting.Version, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	prepare := f.owner.prepare
	f.owner.prepare = func(req PrepareRequest) (Prepared, error) {
		prepared, err := prepare(req)
		f.resolver.deactivate()
		return prepared, err
	}
	paused, err := f.runner.Step(context.Background(), approved.ID, ModeOnBehalfOf)
	if !errors.Is(err, agentrun.ErrStepPaused) || !errors.Is(err, agentdelegation.ErrUserInactive) || paused.State != agentrun.StatePaused || paused.FailureCode != string(agentrun.PauseUserInactive) || paused.Plan.Steps[0].State != agentrun.StepPending || paused.Plan.Steps[0].Approved || paused.Plan.Steps[0].ApprovalDigest != "" || f.owner.calls() != 0 {
		t.Fatalf("authority changed after prepare: %+v,%v,owner calls=%d", paused, err, f.owner.calls())
	}
	events, err := f.runner.tasks.ListEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var voided bool
	for _, event := range events {
		if event.Type == agentrun.TaskEventApprovalOutcome && event.Outcome == "VOIDED_AUTHORITY" && event.ApprovalDigest == digest {
			voided = true
		}
	}
	if !voided {
		t.Fatalf("no durable void receipt: %+v", events)
	}
}

func TestTodo_AGENT_040_SecurityKillSwitchVoidsApproval(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	task := f.start(t, "kill-switch-approved", planStep("update", agentrun.StepSubmit, "skill.update", agentrun.TierSubmitGoverned))
	awaiting, err := f.runner.Step(context.Background(), task.ID, ModeOnBehalfOf)
	if !errors.Is(err, agentrun.ErrApprovalRequired) {
		t.Fatal(err)
	}
	digest := awaiting.Plan.Steps[0].ApprovalDigest
	cfg := f.platform.cfg
	cfg.WakeGate = func(context.Context, string) bool { return false }
	stopped, err := NewPlatform(cfg)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := stopped.TickTenant(context.Background(), tenantKey, fixedNow)
	paused := f.get(t, task.ID)
	if err != nil || moved != 1 || paused.State != agentrun.StatePaused || paused.FailureCode != string(agentrun.PauseTenantDisabled) || paused.Plan.Steps[0].ApprovalDigest != "" || f.owner.calls() != 0 {
		t.Fatalf("kill switch=%+v,%d,%v", paused, moved, err)
	}
	if _, err := f.runner.Runtime.ApproveStep(context.Background(), task.ID, "update", digest, paused.Version, fixedNow); !errors.Is(err, agentrun.ErrApprovalRequired) {
		t.Fatalf("kill-switch approval replay=%v", err)
	}
	if moved, err := stopped.TickTenant(context.Background(), tenantKey, fixedNow); err != nil || moved != 0 {
		t.Fatalf("repeat kill switch=%d,%v", moved, err)
	}
}

func TestTodo_AGENT2_012_IntegrationBudgetPauseResume(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	f.owner.result = func(Invocation) (Result, error) { return Result{Ref: "owner:read", Digest: "sha256:receipt"}, nil }
	limit := testPolicy().TaskDefault
	limit.Steps = 1
	task, err := f.runner.StartTask(context.Background(), StartRequest{TaskID: "limited-task", UserID: "user-42", AgentVersion: "agent-v1", InstallationID: "install-1", Purpose: purposeKey, OrganizationScopeID: "org-west", Goal: "read twice", UserAuthority: userAuthority(true).Authority, Lifetime: time.Hour, Limit: limit, Steps: []agentrun.PlanStep{planStep("first", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("second", agentrun.StepRead, "skill.lookup", agentrun.TierRead)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.Runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, fixedNow); err != nil {
		t.Fatal(err)
	}
	paused, err := f.runner.Drive(context.Background(), task.ID, ModeOnBehalfOf)
	if err != nil || paused.State != agentrun.StatePaused || paused.FailureCode != string(agentbudget.PauseTaskSteps) || paused.Plan.Steps[1].State != agentrun.StepPending || f.owner.calls() != 1 {
		t.Fatalf("budget admission did not pause before second read: %+v, %v, owner=%d", paused, err, f.owner.calls())
	}
	var revision uint64
	for _, budget := range f.ledger.Snapshot().Tasks {
		if budget.ID == task.ID {
			revision = budget.Revision
		}
	}
	if _, err := f.ledger.AcceptExtensionCAS(agentbudget.ExtensionRequest{TaskID: task.ID, RequestID: "approved-extension", ExpectedRevision: revision, Additional: agentbudget.Limits{Steps: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.Runtime.Resume(context.Background(), task.ID, paused.Version, fixedNow); err != nil {
		t.Fatal(err)
	}
	done, err := f.runner.Drive(context.Background(), task.ID, ModeOnBehalfOf)
	if err != nil || done.State != agentrun.StateCompleted || f.owner.calls() != 2 {
		t.Fatalf("extended task = %+v, %v, owner=%d", done, err, f.owner.calls())
	}
}

type countedStepAdmission struct {
	identity TaskWorkIdentity
	calls    int
	releases int
	err      error
}

func (a *countedStepAdmission) AcquireTaskStep(ctx context.Context, identity TaskWorkIdentity) (context.Context, func(), error) {
	a.calls++
	a.identity = identity
	if a.err != nil {
		return ctx, nil, a.err
	}
	return ctx, func() { a.releases++ }, nil
}

func TestTodo_AGENT2_012_IntegrationTaskWorkerAdmission(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "admitted", true: "denied"}[denied], func(t *testing.T) {
			f := newFixture(t, nil)
			f.defaultOwner(t)
			f.owner.result = func(Invocation) (Result, error) { return Result{Ref: "owner:read", Digest: "sha256:receipt"}, nil }
			admission := &countedStepAdmission{}
			if denied {
				admission.err = errors.New("worker queue exhausted")
			}
			f.platform.cfg.StepAdmission = admission
			task := f.start(t, "bounded-worker", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
			result, err := f.runner.Step(context.Background(), task.ID, ModeOnBehalfOf)
			if admission.calls != 1 || admission.identity.UserID != task.UserID || admission.identity.TenantID != tenantKey || admission.identity.TaskID != task.ID || admission.identity.Mode != ModeOnBehalfOf {
				t.Fatalf("worker authority identity = %+v", admission)
			}
			if denied {
				if !errors.Is(err, admission.err) || f.owner.calls() != 0 || admission.releases != 0 || result.State != agentrun.StateFailed {
					t.Fatalf("queue denial = %+v, %v; owner calls=%d", result, err, f.owner.calls())
				}
			} else if err != nil || result.State != agentrun.StateCompleted || admission.releases != 1 || f.owner.calls() != 1 {
				t.Fatalf("worker admission = %+v, %v; admission=%+v", result, err, admission)
			}
		})
	}
}

func TestTodo_AGENT_016_RecoveryScheduler(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	task := f.start(t, "abandoned-read", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
	prior := task.Version
	task.Plan.Steps[0].State, task.Plan.Steps[0].Attempt = agentrun.StepRunning, 1
	task.WorkerLease = "dead-worker"
	task.UpdatedAt = fixedNow.Add(-10 * time.Minute)
	task.Version++
	if err := f.runner.tasks.Save(context.Background(), task, prior); err != nil {
		t.Fatal(err)
	}
	if moved, err := f.platform.TickTenant(context.Background(), tenantKey, fixedNow); err != nil || moved < 1 {
		t.Fatalf("recovery tick = %d, %v", moved, err)
	}
	got := f.get(t, task.ID)
	if got.State != agentrun.StateCompleted || got.WorkerLease != "" || got.Plan.Steps[0].Attempt != 2 || f.owner.calls() != 1 {
		t.Fatalf("abandoned read did not resume once: %+v, calls=%d", got, f.owner.calls())
	}
}

func TestTodo_AGENT2_013_IntegrationLedgerToModel(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	task := f.start(t, "ledger-context", planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	prior := task.Version
	task.Goal = "raw-goal-must-be-minimized"
	task.Constraints = []string{"do not contact anyone"}
	task.Version++
	if err := f.runner.tasks.Save(context.Background(), task, prior); err != nil {
		t.Fatal(err)
	}
	var gotContext *agentrun.TaskContext
	f.owner.prepare = func(req PrepareRequest) (Prepared, error) {
		gotContext = req.Context
		if req.Context == nil {
			t.Fatal("model step did not rebuild ledger")
		}
		if req.Context.Goal != task.Goal || len(req.Context.Constraints) != 1 || req.Context.Constraints[0] != task.Constraints[0] || req.Context.Plan.Digest != task.Plan.Digest {
			t.Fatalf("rebuilt ledger = %+v", req.Context)
		}
		// The owner minimizes the goal before declaring the model payload.
		minimized := *req.Context
		minimized.Goal = "approved goal"
		encoded, err := json.Marshal(minimized)
		if err != nil {
			return Prepared{}, err
		}
		return Prepared{Purpose: purposeKey, Egress: egressCall(agentegress.TargetModel, "model.eu", publicField("task_context", string(encoded)))}, nil
	}
	done, err := f.runner.Drive(context.Background(), task.ID, ModeOnBehalfOf)
	if err != nil || done.State != agentrun.StateCompleted || gotContext == nil {
		t.Fatalf("model context drive = %+v, %v", done, err)
	}
	requests := f.provider.Requests()
	if len(requests) != 1 || strings.Contains(requests[0].UserPrompt, task.Goal) || !strings.Contains(requests[0].UserPrompt, "approved goal") || !strings.Contains(requests[0].UserPrompt, task.Constraints[0]) || !strings.Contains(requests[0].UserPrompt, task.Plan.Digest) {
		t.Fatalf("model prompt bypassed declared context: %+v", requests)
	}
}

func TestTodo_AGENT2_011_RecoveryReplyProvenance(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	task := f.startParked(t, "reply-crash", planStep("ask", agentrun.StepAskUser, "skill.lookup", agentrun.TierRead))
	event := agentrun.WakeEvent{ID: "chat-post-123", Kind: agentrun.WakeUserReply, Key: task.Wake.Key, PayloadRef: "chat:retained:123", OccurredAt: fixedNow}
	accepted, err := f.runner.Wake(context.Background(), task.ID, event)
	if err != nil || !accepted.Accepted {
		t.Fatalf("wake = %+v, %v", accepted, err)
	}
	// Nothing invokes CompleteWait before this new runner resumes work.
	restarted, err := f.platform.ForTenant(context.Background(), f.runner.tenant)
	if err != nil {
		t.Fatal(err)
	}
	done, err := restarted.Drive(context.Background(), task.ID, ModeOnBehalfOf)
	if err != nil || done.State != agentrun.StateCompleted || done.Plan.Steps[0].ResultRef != event.PayloadRef || done.LastWake != nil {
		t.Fatalf("reply resume = %+v, %v", done, err)
	}
	var found bool
	for _, entry := range done.Ledger.Entries {
		if entry.Kind == "STEP_RESULT" {
			found = entry.Ref == event.PayloadRef && entry.Digest == wakeStepResult(event).Digest && len(entry.Taint) == 1 && entry.Taint[0] == string(agentsecurity.TaintHuman)
		}
	}
	if !found || f.owner.calls() != 0 {
		t.Fatalf("reply taint or digest lost: %+v; owner calls=%d", done.Ledger, f.owner.calls())
	}
}
