package agentsystem

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func TestTodo_AGENT_040_SecurityTaskMemoryCurrentAuthority(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	task := f.start(t, "task-memory-authority", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	if _, err := f.platform.ResolveTaskMemoryInvocation(context.Background(), task, task.Plan.Steps[0]); !errors.Is(err, ErrDenied) {
		t.Fatalf("pending step minted source authority=%v", err)
	}
	var checks int
	f.owner.result = func(call Invocation) (Result, error) {
		checks++
		current, err := f.platform.ResolveTaskMemoryInvocation(context.Background(), call.Task, call.Step)
		if err != nil || current.Credential == "" || current.Claims.Subject != call.Task.UserID || current.Claims.Actor.RunID != call.Task.ID || current.Skill.Digest != call.Skill.Digest {
			t.Fatalf("source authority=%+v,%v", current, err)
		}
		forged := call.Task
		forged.UserID = "other-user"
		if _, err := f.platform.ResolveTaskMemoryInvocation(context.Background(), forged, call.Step); !errors.Is(err, ErrDenied) {
			t.Fatalf("other user's source accepted=%v", err)
		}
		forgedStep := call.Step
		forgedStep.SkillID = "skill.update"
		if _, err := f.platform.ResolveTaskMemoryInvocation(context.Background(), call.Task, forgedStep); !errors.Is(err, ErrDenied) {
			t.Fatalf("source skill replacement accepted=%v", err)
		}
		return Result{Ref: "owner:source", Digest: "sha256:source"}, nil
	}
	task, err := f.runner.Step(context.Background(), task.ID, ModeOnBehalfOf)
	if err != nil || checks != 1 {
		t.Fatalf("source execution=%+v,%v", task, err)
	}
	// A source reread is scoped to the completed original step while a later
	// checkpoint remains live; it uses the current delegated grant again.
	if _, err := f.platform.ResolveTaskMemoryInvocation(context.Background(), task, task.Plan.Steps[0]); err != nil {
		t.Fatal(err)
	}
	f.resolver.deactivate()
	if _, err := f.platform.ResolveTaskMemoryInvocation(context.Background(), task, task.Plan.Steps[0]); !errors.Is(err, ErrDenied) {
		t.Fatalf("deactivated source authority=%v", err)
	}
}
