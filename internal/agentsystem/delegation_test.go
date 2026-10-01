package agentsystem

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func specialistRequest(t *testing.T, f *fixture, parent agentrun.AgentTask, id string) DelegateRequest {
	t.Helper()
	grant, err := f.runner.grants.Get(GrantID(parent.ID))
	if err != nil {
		t.Fatal(err)
	}
	step := parent.Plan.Steps[parent.CurrentStep]
	credential, err := f.runner.Delegation.Exchange(agentdelegation.ExchangeRequest{SubjectToken: grant.GrantID, SubjectTokenType: agentdelegation.DelegationGrantTokenType, RunID: parent.ID, StepID: step.ID, Skill: step.SkillID, Scope: grant.SkillScopes[step.SkillID], Audience: f.platform.cfg.Audience, Sender: f.platform.cfg.Workload})
	if err != nil {
		t.Fatal(err)
	}
	return DelegateRequest{ParentTaskID: parent.ID, ParentCredential: credential.Raw, TaskID: id, AgentID: "specialist", AgentVersion: "specialist@1", InstallationID: "specialist-install", Goal: "summarize own worker", Steps: []agentrun.PlanStep{planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft)}, Authority: agentdelegation.InheritedAuthority(grant), Deadline: fixedNow.Add(time.Hour)}
}

func TestTodo_AGENT_050_Conformance(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	parent := f.start(t, "parent", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	child, err := f.runner.Delegate(context.Background(), specialistRequest(t, f, parent, "child"))
	if err != nil {
		t.Fatal(err)
	}
	if !child.Plan.Confirmed || child.ParentTaskID != parent.ID || child.RootTaskID != parent.ID || child.DelegationDepth != 1 {
		t.Fatalf("child = %+v", child)
	}
	completed, err := f.runner.Step(context.Background(), child.ID, ModeOnBehalfOf)
	if err != nil || completed.State != agentrun.StateCompleted {
		t.Fatalf("child execution = %s, %v", completed.State, err)
	}
	views, err := f.audit.Query(context.Background(), agentaudit.Query{Viewer: agentaudit.Viewer{TenantID: tenantKey, UserID: "user-42", Role: agentaudit.ViewerUser}, TaskID: child.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(views) == 0 {
		t.Fatal("child model call has no audit")
	}
	for _, view := range views {
		if view.Actor.SubAgentDepth != 1 || view.Actor.AgentVersion != "specialist@1" || view.Actor.UserID != parent.UserID {
			t.Fatalf("child audit actor = %+v", view.Actor)
		}
	}
	for _, budget := range f.ledger.Snapshot().Tasks {
		if budget.ID == parent.ID && budget.Used.Steps == 0 {
			t.Fatal("root did not inherit child model usage")
		}
	}
}

func TestTodo_AGENT_050_ParentStop(t *testing.T) {
	for _, state := range []agentrun.TaskState{agentrun.StatePaused, agentrun.StateCancelled, agentrun.StateWaiting} {
		t.Run(string(state), func(t *testing.T) {
			f := newFixture(t, nil)
			f.defaultOwner(t)
			ctx := context.Background()
			parent := f.start(t, "parent", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
			child, err := f.runner.Delegate(ctx, specialistRequest(t, f, parent, "child"))
			if err != nil {
				t.Fatal(err)
			}
			parent.State = state
			parent.Version++
			if err := f.runner.tasks.Save(ctx, parent, parent.Version-1); err != nil {
				t.Fatal(err)
			}
			_, err = f.runner.Step(ctx, child.ID, ModeOnBehalfOf)
			if !errors.Is(err, ErrParentStopped) {
				t.Fatalf("stopped parent = %v", err)
			}
			stored, err := f.runner.Runtime.GetTask(ctx, child.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := agentrun.StatePaused
			if state == agentrun.StateCancelled {
				want = agentrun.StateCancelled
			}
			if stored.State != want || f.owner.calls() != 0 {
				t.Fatalf("child state = %s owner calls %d", stored.State, f.owner.calls())
			}
		})
	}
}

func TestTodo_AGENT_050_RuntimeSecurity(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*DelegateRequest)
	}{
		{"deadline", func(r *DelegateRequest) { r.Deadline = fixedNow.Add(48 * time.Hour) }},
		{"source", func(r *DelegateRequest) { r.Authority.Resources = []string{"worker:99"} }},
		{"tier", func(r *DelegateRequest) { r.Steps[0].Tier = agentrun.TierSubmitGoverned }},
		{"unconfirmed skill", func(r *DelegateRequest) { r.Steps[0].SkillID = "skill.update" }},
		{"cycle", func(r *DelegateRequest) { r.AgentVersion = "agent-v1" }},
		{"parent credential", func(r *DelegateRequest) { r.ParentCredential = "browser-session" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, nil)
			f.defaultOwner(t)
			parent := f.start(t, "parent", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
			req := specialistRequest(t, f, parent, "child")
			test.change(&req)
			if _, err := f.runner.Delegate(context.Background(), req); err == nil {
				t.Fatal("expanded specialist admitted")
			}
			if f.owner.calls() != 0 {
				t.Fatal("effect before admission")
			}
		})
	}
}

func TestTodo_AGENT_050_RuntimeRace(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	ctx := context.Background()
	parent := f.start(t, "parent", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	req := specialistRequest(t, f, parent, "child")
	var wait sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() { defer wait.Done(); _, err := f.runner.Delegate(ctx, req); results <- err }()
	}
	wait.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("raced child = %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("created children = %d", winners)
	}
}
