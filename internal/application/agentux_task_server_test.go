package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	intentapp "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentTaskPersonaResolverFunc func(context.Context, *trust.Principal, string) (agentrun.TaskAgentIdentity, error)

func (f agentTaskPersonaResolverFunc) ResolveAgentTaskPersona(ctx context.Context, principal *trust.Principal, id string) (agentrun.TaskAgentIdentity, error) {
	return f(ctx, principal, id)
}

type agentTaskAuthorityFunc func(context.Context, agentinvoke.AdmissionRequest) (agentinvoke.Admission, error)

func (f agentTaskAuthorityFunc) Resolve(ctx context.Context, request agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	return f(ctx, request)
}

type agentTaskAudienceFunc func(context.Context, *trust.Principal) ([]agentpersonastore.AvailableInstallation, error)

func (f agentTaskAudienceFunc) ResolveAvailablePersonaInstallations(ctx context.Context, principal *trust.Principal) ([]agentpersonastore.AvailableInstallation, error) {
	return f(ctx, principal)
}

type failingTaskStepExecutor struct{ err error }

func (f failingTaskStepExecutor) Execute(context.Context, agentrun.AgentTask, agentrun.PlanStep) (agentrun.StepResult, error) {
	return agentrun.StepResult{}, f.err
}

func TestTodo_AGENTUX_009(t *testing.T) {
	placement := agentrun.TaskAgentIdentity{ID: "policy-helper", DisplayName: "Policy Helper", Version: "7"}
	ctx, err := agentrun.WithTaskAgentIdentity(context.Background(), placement)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := agentrun.NewPlan(agentTaskPlanSteps(AgentTaskPlanningOutput{}))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := agentrun.NewRuntime(agentrun.NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	task, err := runtime.CreateTask(ctx, agentrun.CreateRequest{ID: "task-persona", TenantID: "tenant-a", UserID: "user-a", Goal: "help", Plan: plan, Now: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil || task.Plan.AnsweringAgent == nil || *task.Plan.AnsweringAgent != placement {
		t.Fatalf("task identity = %+v, %v", task.Plan.AnsweringAgent, err)
	}
}

func TestTodo_AGENTUX_016(t *testing.T) {
	// A request that declares no data need gets the answer step alone.
	steps := agentTaskPlanSteps(AgentTaskPlanningOutput{})
	if len(steps) != 1 || steps[0].Type != agentrun.StepAnalyze || steps[0].SkillID != agentSummarizeSkillID || steps[0].Tier != agentrun.TierPrivateDraft {
		t.Fatalf("no-data plan = %+v", steps)
	}
	// A request that needs the user's own record reads it first, then answers.
	steps = agentTaskPlanSteps(AgentTaskPlanningOutput{NeedsOwnRecord: true})
	if len(steps) != 2 || steps[0].Type != agentrun.StepRead || steps[0].SkillID != agentReadSkillID || steps[1].Type != agentrun.StepAnalyze || steps[1].SkillID != agentSummarizeSkillID {
		t.Fatalf("own-record plan = %+v", steps)
	}
	for _, shape := range [][]agentrun.PlanStep{agentTaskPlanSteps(AgentTaskPlanningOutput{}), steps} {
		if _, err := agentrun.NewPlan(shape); err != nil {
			t.Fatalf("plan %+v is not a valid plan: %v", shape, err)
		}
	}
}

func TestTodo_AGENTUX_008_Integration(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	ctx, client := newServedAgentClient(t, f)
	started, err := client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "What is my job title?", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := f.runtime.Platform.ForTenant(context.Background(), values.TenantId(f.tenant))
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := runner.Runtime.ConfirmPlan(context.Background(), started.GetTaskId(), agentTestWorker, started.GetVersion(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	privateFailure := errors.New("provider stack trace request-id secret-123")
	failed, err := runner.Runtime.ExecuteNext(context.Background(), confirmed.ID, confirmed.Version, failingTaskStepExecutor{err: privateFailure}, nil, time.Now().UTC())
	if !errors.Is(err, privateFailure) || failed.State != agentrun.StateFailed {
		t.Fatalf("failed task = %+v, %v", failed, err)
	}
	got, err := client.GetAgentTask(ctx, &agentv1.GetAgentTaskRequest{TaskId: failed.ID})
	if err != nil {
		t.Fatal(err)
	}
	task := got.GetTask()
	if task.GetFailureSummary() != "A step could not be completed." || !task.GetRetryable() || strings.Contains(task.String(), "secret-123") || len(task.GetSteps()) != 2 {
		t.Fatalf("failed projection = %+v", task)
	}
	step := task.GetSteps()[0]
	if step.GetKind() != "Read information" || step.GetStatus() != "Failed" || step.GetFailureSummary() != "This step could not be completed." || step.GetStartedAt() == nil || step.GetFinishedAt() == nil {
		t.Fatalf("failed step = %+v", step)
	}
}

func TestTodo_AGENTUX_009_Integration(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	f.runtime.Starter.personas = agentTaskPersonaResolverFunc(func(_ context.Context, principal *trust.Principal, id string) (agentrun.TaskAgentIdentity, error) {
		if principal.Subject() != agentTestWorker || id != "policy-helper" {
			return agentrun.TaskAgentIdentity{}, agentrun.ErrTaskAgentDenied
		}
		return agentrun.TaskAgentIdentity{ID: id, DisplayName: "Policy Helper", Version: "7"}, nil
	})
	ctx, client := newServedAgentClient(t, f)
	selected, err := client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "Summarize the policy", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK, PersonaId: "policy-helper"})
	if err != nil {
		t.Fatal(err)
	}
	if task := selected.GetTask(); task.GetAnsweringAgentId() != "policy-helper" || task.GetAnsweringAgentDisplayName() != "Policy Helper" || task.GetAnsweringAgentVersion() != "7" {
		t.Fatalf("selected task = %+v", task)
	}
	general, err := client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "Give a general tip", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK})
	if err != nil {
		t.Fatal(err)
	}
	if task := general.GetTask(); task.GetAnsweringAgentId() != "general-agent" || task.GetAnsweringAgentDisplayName() != "General agent" || task.GetAnsweringAgentVersion() != agentVersion {
		t.Fatalf("general task = %+v", task)
	}
}

func TestTodo_AGENTUX_009_Security(t *testing.T) {
	placement := agentpersonastore.AvailableInstallation{ConversationID: "room-a", PersonaID: "admin-only-agent", PersonaVersion: 3, InstallationID: "install-a"}
	forged := agentinvoke.Admission{HumanMember: true, AudienceMember: false, PersonaInstalled: true,
		Persona:      agentinvoke.Persona{ID: placement.PersonaID, Version: "3", Current: true, PinnedSkills: agentinvoke.SkillScopes{"read": {"worker"}}},
		Installation: agentinvoke.Installation{ID: placement.InstallationID, Current: true, SkillCeiling: agentinvoke.SkillScopes{"read": {"worker"}}},
		Channel:      agentinvoke.ChannelPolicy{SkillCeiling: agentinvoke.SkillScopes{"read": {"worker"}}}, Discoverable: agentinvoke.SkillScopes{"read": {"worker"}}}
	if agentTaskPersonaAdmissionAllowed(forged, placement) {
		t.Fatal("an out-of-audience persona passed the invocation gate")
	}
	principal := agentTestPrincipal(t, agentTestTenant, agentTestWorker)
	trustedCtx := trust.WithPrincipal(context.Background(), principal)
	realResolver, err := NewAgentTaskPersonaResolver(
		agentTaskAuthorityFunc(func(_ context.Context, request agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
			if request.PersonaID != placement.PersonaID || request.InvokerID != principal.Subject() || request.ConversationID != placement.ConversationID {
				t.Fatal("persona authority received a forged admission tuple")
			}
			return forged, nil
		}),
		agentTaskAudienceFunc(func(context.Context, *trust.Principal) ([]agentpersonastore.AvailableInstallation, error) {
			return []agentpersonastore.AvailableInstallation{placement}, nil
		}),
		&agentpersonastore.Store{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := realResolver.ResolveAgentTaskPersona(trustedCtx, principal, placement.PersonaID); !errors.Is(err, agentrun.ErrTaskAgentDenied) {
		t.Fatalf("real gate denial = %v", err)
	}
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	var calls int
	f.runtime.Starter.personas = agentTaskPersonaResolverFunc(func(context.Context, *trust.Principal, string) (agentrun.TaskAgentIdentity, error) {
		calls++
		return agentrun.TaskAgentIdentity{}, agentrun.ErrTaskAgentDenied
	})
	ctx, client := newServedAgentClient(t, f)
	_, err = client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "Use the forged selection", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK, PersonaId: " admin-only-agent "})
	if status.Code(err) != codes.InvalidArgument || calls != 0 {
		t.Fatalf("malformed persona = %v calls=%d", err, calls)
	}
	_, err = client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "Use the forged selection", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK, PersonaId: "admin-only-agent"})
	if status.Code(err) != codes.PermissionDenied || calls != 1 || strings.Contains(status.Convert(err).Message(), "admin-only-agent") {
		t.Fatalf("forged persona = %v calls=%d", err, calls)
	}
	listed, listErr := client.ListAgentTasks(ctx, &agentv1.ListAgentTasksRequest{})
	if listErr != nil || len(listed.GetTasks()) != 0 {
		t.Fatalf("denied selection stored tasks = %+v, %v", listed, listErr)
	}
}

func TestTodo_AGENTUX_016_Golden(t *testing.T) {
	withoutData, err := json.Marshal(agentTaskPlanSteps(AgentTaskPlanningOutput{}))
	if err != nil {
		t.Fatal(err)
	}
	withOwnRecord, err := json.Marshal(agentTaskPlanSteps(AgentTaskPlanningOutput{NeedsOwnRecord: true}))
	if err != nil {
		t.Fatal(err)
	}
	const wantWithout = `[{"id":"summarize_request","type":"ANALYZE","skill_id":"agent.summarize_request","skill_version":1,"expected_output":"a short private answer to the request","tier":1,"state":"","attempt":0,"started_at":"0001-01-01T00:00:00Z","finished_at":"0001-01-01T00:00:00Z"}]`
	const wantWith = `[{"id":"read_worker_state","type":"READ","skill_id":"agent.read_own_worker_state","skill_version":1,"expected_output":"the signed-in user's own worker record","tier":0,"state":"","attempt":0,"started_at":"0001-01-01T00:00:00Z","finished_at":"0001-01-01T00:00:00Z"},{"id":"summarize_request","type":"ANALYZE","skill_id":"agent.summarize_request","skill_version":1,"expected_output":"a short private answer to the request","tier":1,"state":"","attempt":0,"started_at":"0001-01-01T00:00:00Z","finished_at":"0001-01-01T00:00:00Z"}]`
	if string(withoutData) != wantWithout || string(withOwnRecord) != wantWith {
		t.Fatalf("without=%s\nwith=%s", withoutData, withOwnRecord)
	}
}

func TestTodo_AGENTUX_016_Security(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	evidence := f.cell.Evidence.(*intentapp.MemoryEvidenceSink)
	principal := agentTestPrincipal(t, f.tenant, agentTestWorker)
	ctx := trust.WithPrincipal(context.Background(), principal)
	noData, err := f.runtime.Starter.StartTask(ctx, principal, "In one sentence, what is a good way to welcome a new teammate? Use no company data.")
	// The request needs no data, so the worker-state capability is never called.
	if err != nil || noData.State != string(agentrun.StateCompleted) || len(evidence.Records()) != 0 {
		t.Fatalf("no-data task = %+v, %v evidence=%+v", noData, err, evidence.Records())
	}
	withData, err := f.runtime.Starter.StartTask(ctx, principal, "What is my job title?")
	if err != nil || withData.State != string(agentrun.StateCompleted) {
		t.Fatalf("own-record task = %+v, %v", withData, err)
	}
	// The request that needs the record reads exactly that one record, as the
	// signed-in user, in the user's tenant.
	records := evidence.Records()
	if len(records) != 1 || records[0].CapabilityID != agentReadCapabilityID || records[0].SubjectRef != agentTestWorker || records[0].Tenant != f.tenant || records[0].Decision != "INVOKED" {
		t.Fatalf("own-record evidence = %+v", records)
	}
}

func TestTodo_AGENTUX_016_Integration(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	ctx, client := newServedAgentClient(t, f)
	for _, tc := range []struct {
		prompt string
		steps  int
		first  string
	}{
		{"In one sentence, what is a good way to welcome a new teammate? Use no company data.", 1, "Prepare answer"},
		{"What is my job title?", 2, "Read information"},
	} {
		started, err := client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: tc.prompt, Mode: agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER})
		if err != nil || started.GetState() != string(agentrun.StateCompleted) || len(started.GetTask().GetSteps()) != tc.steps || started.GetTask().GetSteps()[0].GetKind() != tc.first {
			t.Fatalf("served plan for %q = %+v, %v", tc.prompt, started, err)
		}
	}
}

func TestTodo_AGENTUX_016_PlannerDefaultsToNoData(t *testing.T) {
	planner := deterministicAgentTaskPlanner{}
	for _, tc := range []struct {
		prompt string
		need   bool
	}{
		{"Give one general onboarding tip", false},
		{"Use no company data", false},
		{"Where do I work?", true},
		{"Summarize my record", true},
	} {
		got, err := planner.PlanAgentTask(context.Background(), tc.prompt, agentclient.StartQuickAnswer)
		if err != nil || got.NeedsOwnRecord != tc.need {
			t.Fatalf("plan %q = %+v, %v", tc.prompt, got, err)
		}
	}
}
