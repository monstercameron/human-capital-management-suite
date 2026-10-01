package application

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
)

func TestTodo_AGENT_034_CommonTaskSource_Golden(t *testing.T) {
	occurrence := CommonAgentTaskOccurrence{TaskID: "task:one", StepID: "step:one", PlanDigest: "sha256:" + strings.Repeat("a", 64),
		Skill: agentskills.SkillPin{ID: "workflow.start", Version: 1, Digest: strings.Repeat("b", 64)}}
	ref, err := CommonAgentTaskSourceRef(occurrence)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ref, commonAgentTaskSourcePrefix))
	want := `{"task_id":"task:one","step_id":"step:one","plan_digest":"sha256:` + strings.Repeat("a", 64) + `","skill":{"id":"workflow.start","version":1,"digest":"` + strings.Repeat("b", 64) + `"}}`
	if err != nil || string(raw) != want || len(ref) > 512 {
		t.Fatalf("task source canonical bytes = %s %v", raw, err)
	}
	for _, mutate := range []func(*CommonAgentTaskOccurrence){
		func(o *CommonAgentTaskOccurrence) { o.TaskID = "" },
		func(o *CommonAgentTaskOccurrence) { o.StepID = "step\nforged" },
		func(o *CommonAgentTaskOccurrence) { o.PlanDigest = "" },
		func(o *CommonAgentTaskOccurrence) { o.Skill.Digest = "sha256:" + o.Skill.Digest },
		func(o *CommonAgentTaskOccurrence) { o.Skill.Version = 0 },
		func(o *CommonAgentTaskOccurrence) { o.TaskID = strings.Repeat("a", 256) },
	} {
		changed := occurrence
		mutate(&changed)
		if _, err := CommonAgentTaskSourceRef(changed); !errors.Is(err, agentrun.ErrInvalidRequest) {
			t.Fatalf("invalid occurrence = %+v %v", changed, err)
		}
	}
}

func TestTodo_AGENT_034_CommonTaskSource_Integration(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	ctx := context.Background()
	started, err := f.runtime.Starter.StartTaskMode(ctx, agentTestPrincipal(t, f.tenant, agentTestWorker), "Read my own worker record", agentclient.StartLongTask)
	if err != nil {
		t.Fatal(err)
	}
	task := f.task(t, started.ID)
	if task.Plan.Confirmed {
		t.Fatal("long task unexpectedly confirmed")
	}
	step := task.Plan.Steps[0]
	occurrence := CommonAgentTaskOccurrence{TaskID: task.ID, StepID: step.ID, PlanDigest: task.Plan.Digest,
		Skill: agentskills.SkillPin{ID: step.SkillID, Version: step.SkillVersion, Digest: strings.Repeat("b", 64)}}
	ref, err := CommonAgentTaskSourceRef(occurrence)
	if err != nil {
		t.Fatal(err)
	}
	request := commonAgentTestRequest(time.Now().UTC())
	request.Source.Kind, request.Source.TenantID, request.Source.Ref, request.CauseID = agentrun.SourceAPI, f.tenant, ref, task.ID
	request.Principal = agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "agent-service", InvokerID: agentTestWorker, DelegatedCredentialRef: agentsystem.GrantID(task.ID)}
	request.Source, err = (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	owner := CommonAgentTaskSourceAuthority{Platform: f.runtime.Platform}
	err = owner.CheckRequest(ctx, request)
	var refusal *agentrun.AdmissionRefusal
	if !errors.As(err, &refusal) || refusal.Code != "TASK_SOURCE_NOT_CURRENT" {
		t.Fatalf("unconfirmed actual durable task accepted = %v", err)
	}
	request.CauseID = "forged-task"
	err = owner.CheckRequest(ctx, request)
	if !errors.As(err, &refusal) || refusal.Code != "TASK_SOURCE_DENIED" {
		t.Fatalf("forged cause accepted = %v", err)
	}
	request.CauseID = task.ID
	request.Source.Key = "forged-source-key"
	err = owner.CheckRequest(ctx, request)
	if !errors.As(err, &refusal) || refusal.Code != "TASK_SOURCE_DENIED" {
		t.Fatalf("forged canonical source accepted = %v", err)
	}
}
