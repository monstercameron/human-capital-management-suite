package agentsystem

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

type invocationDispatch func(context.Context, agentmodel.Request, agentmodel.TypedModelInput) (agentmodel.ModelResult, error)

func (f invocationDispatch) DispatchTypedModel(ctx context.Context, req agentmodel.Request, input agentmodel.TypedModelInput) (agentmodel.ModelResult, error) {
	return f(ctx, req, input)
}

func TestTodo_AGENT_017_SecurityCurrentModelInvocation(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	var calls int
	var retainedContext context.Context
	var retainedRequest agentmodel.Request
	cfg := f.platform.cfg
	cfg.TypedDispatch = invocationDispatch(func(ctx context.Context, req agentmodel.Request, input agentmodel.TypedModelInput) (agentmodel.ModelResult, error) {
		calls++
		current, err := f.runner.ResolveModelInvocation(ctx, req)
		if err != nil || current.Task.ID != req.Actor.TaskID || current.Step.ID != req.Actor.StepID || current.Skill.Digest != req.Skill.Digest || current.Actor != req.Actor {
			t.Fatalf("current invocation=%+v,%v", current, err)
		}
		if _, err := f.runner.CurrentModelInvocation(ctx); err != nil {
			t.Fatal(err)
		}
		if owner, err := f.runner.ResolveTaskInvocation(ctx, req.Actor.TaskID, req.Actor.StepID, req.Actor.UserID, req.Skill); err != nil || owner.Grant.GrantID != req.Actor.DelegationGrantID {
			t.Fatalf("API source current authority=%+v,%v", owner, err)
		}
		if _, err := f.runner.ResolveTaskInvocation(ctx, req.Actor.TaskID, req.Actor.StepID, "other-user", req.Skill); !errors.Is(err, ErrDenied) {
			t.Fatalf("API source substituted invoker=%v", err)
		}
		if _, err := f.runner.ResolveModelInvocation(context.Background(), req); !errors.Is(err, ErrDenied) {
			t.Fatalf("fabricated context accepted: %v", err)
		}
		changed := req
		changed.Prompt += " bypass"
		if _, err := f.runner.ResolveModelInvocation(ctx, changed); !errors.Is(err, ErrDenied) {
			t.Fatalf("changed prompt accepted: %v", err)
		}
		changed = req
		changed.Actor.UserID = "other-user"
		if _, err := f.runner.ResolveModelInvocation(ctx, changed); !errors.Is(err, ErrDenied) {
			t.Fatalf("changed actor accepted: %v", err)
		}
		if !json.Valid(input.Schema) || input.Prompt == "" || input.WebSearch {
			t.Fatalf("typed input=%+v", input)
		}
		retainedContext, retainedRequest = ctx, req
		return agentmodel.ModelResult{Finish: agentmodel.FinishComplete, Structured: json.RawMessage(`{"text":"approved answer","citations":[]}`)}, nil
	})
	platform, err := NewPlatform(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.platform = platform
	f.runner, err = platform.ForTenant(context.Background(), f.runner.tenant)
	if err != nil {
		t.Fatal(err)
	}
	task := f.start(t, "model-invocation", planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	done, err := f.runner.Drive(context.Background(), task.ID, ModeOnBehalfOf)
	if err != nil || done.State != agentrun.StateCompleted || done.Ledger.AnswerText != "approved answer" || calls != 1 {
		t.Fatalf("model drive=%+v,%v,calls=%d", done, err, calls)
	}
	if _, err := f.runner.ResolveModelInvocation(retainedContext, retainedRequest); !errors.Is(err, ErrDenied) {
		t.Fatalf("completed checkpoint replay accepted: %v", err)
	}
}

func TestTodo_AGENT2_020_SecurityModelRevocationPausesCheckpoint(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	cfg := f.platform.cfg
	cfg.TypedDispatch = invocationDispatch(func(ctx context.Context, req agentmodel.Request, input agentmodel.TypedModelInput) (agentmodel.ModelResult, error) {
		f.resolver.deactivate()
		if _, err := f.runner.ResolveModelInvocation(ctx, req); !errors.Is(err, ErrDenied) {
			t.Fatalf("revoked model invocation=%v", err)
		}
		return agentmodel.ModelResult{}, agentmodel.ErrNotConfigured
	})
	platform, err := NewPlatform(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.platform = platform
	f.runner, err = platform.ForTenant(context.Background(), f.runner.tenant)
	if err != nil {
		t.Fatal(err)
	}
	task := f.start(t, "model-revoked", planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	paused, err := f.runner.Drive(context.Background(), task.ID, ModeOnBehalfOf)
	if err != nil || paused.State != agentrun.StatePaused || paused.FailureCode != string(agentrun.PauseUserInactive) || paused.Plan.Steps[0].State != agentrun.StepPending || f.owner.calls() != 0 {
		t.Fatalf("revoked model checkpoint=%+v,%v", paused, err)
	}
}
