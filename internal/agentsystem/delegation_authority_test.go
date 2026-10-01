package agentsystem

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

type childAuthorityFunc func(context.Context, agentrun.AgentTask, agentdelegation.Grant) error

func (f childAuthorityFunc) CheckChildTask(ctx context.Context, task agentrun.AgentTask, grant agentdelegation.Grant) error {
	return f(ctx, task, grant)
}

func TestTodo_AGENT_050_CurrentChildAuthority(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	ctx := context.Background()
	parent := f.start(t, "parent-current", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	request := specialistRequest(t, f, parent, "child-current")
	request.AdmissionID = "admitted-child-current"
	if _, err := f.runner.Delegate(ctx, request); !errors.Is(err, ErrDenied) {
		t.Fatalf("missing source authority = %v", err)
	}
	if _, err := f.runner.grants.Get(GrantID(request.TaskID)); err == nil {
		t.Fatal("source authority missing but child grant persisted")
	}
	active := false
	checks := 0
	f.platform.cfg.ChildAuthority = childAuthorityFunc(func(_ context.Context, task agentrun.AgentTask, grant agentdelegation.Grant) error {
		checks++
		if task.ID != request.TaskID || task.ParentTaskID != parent.ID || grant.CommonAdmissionID != request.AdmissionID || grant.TargetAgentID != request.AgentID || grant.TaskID != task.ID || !active {
			return ErrDenied
		}
		return nil
	})
	if _, err := f.runner.Delegate(ctx, request); !errors.Is(err, ErrDenied) {
		t.Fatalf("disabled child target = %v", err)
	}
	if _, err := f.runner.grants.Get(GrantID(request.TaskID)); err == nil {
		t.Fatal("target denied before admission but grant persisted")
	}
	active = true
	child, err := f.runner.Delegate(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	active = false
	if _, err := f.runner.Step(ctx, child.ID, ModeOnBehalfOf); !errors.Is(err, ErrDenied) {
		t.Fatalf("target revocation before step = %v", err)
	}
	if err := (wakeRechecker{runner: f.runner}).RecheckWake(ctx, child, agentrun.WakeEvent{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("target revocation before wake = %v", err)
	}
	if checks < 3 || f.owner.calls() != 0 {
		t.Fatalf("source checks=%d owner calls=%d", checks, f.owner.calls())
	}
	active = true
	child, err = f.runner.Runtime.GetTask(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.State == agentrun.StateFailed {
		// A denied executable checkpoint is terminal; no retry invents a new
		// authority snapshot. Test output revocation on a separately created run.
		request.TaskID, request.AdmissionID = "child-result-current", "admitted-child-result-current"
		child, err = f.runner.Delegate(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.runner.Step(ctx, child.ID, ModeOnBehalfOf); err != nil {
		t.Fatal(err)
	}
	active = false
	if _, err := f.runner.SpecialistResult(ctx, parent.ID, request.ParentCredential, child.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("target revocation before result = %v", err)
	}
}
