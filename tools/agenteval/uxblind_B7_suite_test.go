package agenteval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/schemaflux/schemafluxtest"
)

type modelObservation struct {
	Completed      bool  `json:"completed" schemaflux:"required"`
	VerifyPassed   int   `json:"verify_passed" schemaflux:"required"`
	VerifyTotal    int   `json:"verify_total" schemaflux:"required"`
	PlanRevisions  int   `json:"plan_revisions" schemaflux:"required"`
	ApprovalCount  int   `json:"approval_count" schemaflux:"required"`
	Steps          int   `json:"steps" schemaflux:"required"`
	WallClockNanos int64 `json:"wall_clock_nanos" schemaflux:"required"`
	CostMicros     int64 `json:"cost_micros" schemaflux:"required"`
}

type testReservation struct{}

func (testReservation) Settle(agentbudget.Usage) error { return nil }
func (testReservation) Fail() error                    { return nil }

type testBudget struct{}

func (testBudget) Reserve(context.Context, agentbudget.Request) (agentmodel.Reservation, error) {
	return testReservation{}, nil
}

type testAudit struct{}

func (testAudit) Append(context.Context, agentaudit.Entry) (agentaudit.Record, error) {
	return agentaudit.Record{}, nil
}

type testSkills struct{}

func (testSkills) ResolvePin(agentskills.SkillPin) (agentskills.SkillRecord, error) {
	return agentskills.SkillRecord{
		Definition: agentskills.SkillDefinition{ID: "evaluation-model", Version: 1, RequiredPurposes: []string{"evaluation"}, DataClassesRead: []string{"INTERNAL"}},
		Digest:     "evaluation-skill-digest", Status: agentskills.StatusActive,
	}, nil
}

type testRedactor struct{}

func (testRedactor) Redact(_ context.Context, text string, _ []string) (agentmodel.Redaction, error) {
	return agentmodel.Redaction{Text: text}, nil
}

func modelGateway(t *testing.T) *agentmodel.Gateway {
	t.Helper()
	gateway, err := agentmodel.New(agentmodel.Config{
		Skills: testSkills{}, Budget: testBudget{}, Audit: testAudit{}, Redactor: testRedactor{},
		Clock: func() time.Time { return time.Unix(100, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("agentmodel.New() error = %v", err)
	}
	return gateway
}

type schemaFluxExecutor struct {
	gateway *agentmodel.Gateway
}

func (e schemaFluxExecutor) Execute(ctx context.Context, task TaskCase) (TaskOutcome, error) {
	answer, err := agentmodel.Generate[modelObservation](ctx, e.gateway, agentmodel.Request{
		TenantID: task.TenantID,
		Actor:    agentmodel.ActorChain{UserID: "eval-user", AgentVersion: "eval-agent/v1", InstallationID: "eval-install", TaskID: "eval-" + task.ID, PlanRevision: "1", StepID: "step-1", DelegationGrantID: "eval-grant"},
		Purpose:  "evaluation", Prompt: "Evaluate synthetic task " + task.ID + ": " + task.Goal,
		Skill:       agentskills.SkillPin{ID: "evaluation-model", Version: 1, Digest: "evaluation-skill-digest"},
		DataClasses: []string{"INTERNAL"}, Estimate: agentbudget.Usage{Steps: 1, Tokens: 100, WallClock: time.Second, SpendMicros: 10_000},
	})
	if err != nil {
		return TaskOutcome{}, err
	}
	outcome := TaskOutcome{Completed: answer.Value.Completed, VerifyPassed: answer.Value.VerifyPassed, VerifyTotal: answer.Value.VerifyTotal, PlanRevisions: answer.Value.PlanRevisions, Steps: answer.Value.Steps, WallClock: time.Duration(answer.Value.WallClockNanos), CostMicros: answer.Value.CostMicros}
	if answer.Value.ApprovalCount > 0 && task.ApprovalEffect != "" {
		outcome.ApprovalsRequested = map[string]int{task.ApprovalEffect: answer.Value.ApprovalCount}
	}
	return outcome, nil
}

func passingFixtures() []agentsecurity.EvalFixture {
	return []agentsecurity.EvalFixture{{Name: "long-horizon-safety", SafetyPass: true, GroundingPass: true, ToolSelectionPass: true}}
}

func release() agentsecurity.Release {
	return agentsecurity.Release{Agent: "evaluation-agent", AgentBuild: "build-1", AgentVersion: "agent-v1", PersonaID: "persona-7", PersonaVersion: "persona-v2", InstallationID: "install-a", Model: "schemaflux-test", ModelDigest: "sha256:model-1", Tool: "evaluation-skill", ToolVersion: 1, PromptHash: "sha256:prompt-1"}
}

func staticExecutor(outcome TaskOutcome) TaskExecutor {
	return TaskExecutorFunc(func(context.Context, TaskCase) (TaskOutcome, error) { return cloneOutcome(outcome), nil })
}

func TestTodo_AGENT2_025(t *testing.T) {
	provider := schemafluxtest.New().Reply(
		`{"completed":true,"verify_passed":2,"verify_total":2,"plan_revisions":1,"approval_count":1,"steps":4,"wall_clock_nanos":1000000,"cost_micros":1500}`,
		`{"completed":true,"verify_passed":1,"verify_total":1,"plan_revisions":0,"approval_count":0,"steps":3,"wall_clock_nanos":2000000,"cost_micros":2500}`,
		`{"completed":true,"verify_passed":2,"verify_total":2,"plan_revisions":1,"approval_count":0,"steps":5,"wall_clock_nanos":3000000,"cost_micros":3500}`,
		`{"completed":true,"verify_passed":1,"verify_total":1,"plan_revisions":0,"approval_count":0,"steps":2,"wall_clock_nanos":4000000,"cost_micros":4500}`,
	)
	t.Cleanup(schemafluxtest.Install(t, provider))

	evaluator := NewEvaluator()
	run, err := evaluator.Evaluate(context.Background(), Request{Release: release(), Suite: DefaultSuite(), Fixtures: passingFixtures(), Executor: schemaFluxExecutor{gateway: modelGateway(t)}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !run.Passed || run.Metrics.TotalTasks != 4 || run.Metrics.CompletionRate != 1 || run.Metrics.VerifyPassRate != 1 || run.Metrics.PlanRevisions != 2 || run.Metrics.ApprovalsRequested["T3"] != 1 || run.Metrics.Steps != 14 || run.Metrics.WallClock != 10*time.Millisecond || run.Metrics.CostMicros != 12_000 {
		t.Fatalf("run metrics = %+v, threshold = %+v, tasks=%+v, passed=%v", run.Metrics, run.Threshold, run.Tasks, run.Passed)
	}
	if provider.CallCount() != 4 {
		t.Fatalf("provider calls = %d, want one typed model call for each synthetic task", provider.CallCount())
	}
	if run.Tasks[0].Outcome.Steps != 4 || run.Tasks[1].Outcome.Steps != 3 || run.Tasks[2].Outcome.Steps != 5 || run.Tasks[3].Outcome.Steps != 2 {
		t.Fatalf("per-task outcomes = %+v, want distinct four-task observations", run.Tasks)
	}
	if err := run.Verify(); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	publisher := NewPublisher()
	if err := publisher.Publish(run); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	routed, err := publisher.Route(DefaultSuite().Version, release())
	if err != nil || routed.Digest != run.Digest {
		t.Fatalf("Route() = digest %q, error %v; want %q", routed.Digest, err, run.Digest)
	}
}

func TestTodo_AGENT2_025_Conformance(t *testing.T) {
	suite := DefaultSuite()
	if len(suite.Tasks) != 4 || suite.Tasks[0].Kind != TaskPromotionPreparation || suite.Tasks[1].Kind != TaskOnboardingChecklist || suite.Tasks[2].Kind != TaskPolicyQuestion || suite.Tasks[3].Kind != TaskCrossSystemLookup {
		t.Fatalf("default task kinds = %+v, want the four conformance scenarios", suite.Tasks)
	}
	base := TaskOutcome{Completed: true, VerifyPassed: 1, VerifyTotal: 1, Steps: 2, WallClock: time.Millisecond, CostMicros: 10}
	evaluator := NewEvaluator()
	failed, err := evaluator.Evaluate(context.Background(), Request{Release: release(), Suite: suite, Fixtures: passingFixtures(), Executor: TaskExecutorFunc(func(_ context.Context, task TaskCase) (TaskOutcome, error) {
		outcome := base
		if task.Kind == TaskPolicyQuestion {
			outcome.VerifyPassed = 0
		}
		return outcome, nil
	})})
	if err != nil {
		t.Fatalf("failed conformance evaluation error = %v", err)
	}
	if failed.Passed || failed.Threshold.VerifyPassRate || failed.Metrics.VerifyPassRate != 0.75 {
		t.Fatalf("failed verify gate = metrics %+v threshold %+v passed=%v", failed.Metrics, failed.Threshold, failed.Passed)
	}
	if err := NewPublisher().Publish(failed); !errors.Is(err, ErrThreshold) {
		t.Fatalf("failed publication error = %v, want ErrThreshold", err)
	}

	passing, err := NewEvaluator().Evaluate(context.Background(), Request{Release: release(), Suite: suite, Fixtures: passingFixtures(), Executor: staticExecutor(baseWithApproval())})
	if err != nil {
		t.Fatalf("passing conformance evaluation error = %v", err)
	}
	publisher := NewPublisher()
	if err := publisher.Publish(passing); err != nil {
		t.Fatalf("passing publication error = %v", err)
	}
	changed := release()
	changed.ModelDigest = "sha256:model-2"
	if _, err := publisher.Route(suite.Version, changed); !errors.Is(err, ErrNotPublished) {
		t.Fatalf("changed model route error = %v, want ErrNotPublished", err)
	}
	for name, mutate := range map[string]func(*agentsecurity.Release){
		"agent version":   func(v *agentsecurity.Release) { v.AgentVersion = "agent-v2" },
		"persona id":      func(v *agentsecurity.Release) { v.PersonaID = "persona-8" },
		"persona version": func(v *agentsecurity.Release) { v.PersonaVersion = "persona-v3" },
		"installation":    func(v *agentsecurity.Release) { v.InstallationID = "install-b" },
	} {
		t.Run(name, func(t *testing.T) {
			versionChanged := release()
			mutate(&versionChanged)
			if _, err := publisher.Route(suite.Version, versionChanged); !errors.Is(err, ErrNotPublished) {
				t.Fatalf("changed release route error = %v, want ErrNotPublished", err)
			}
		})
	}
}

func TestTodo_AGENT2_025_Thresholds(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Suite, *TaskOutcome)
		failed    func(ThresholdResult) bool
	}{
		{"completion", func(s *Suite, o *TaskOutcome) { o.Completed = false; s.Thresholds.MinCompletionRate = 1 }, func(r ThresholdResult) bool { return !r.CompletionRate }},
		{"verify", func(s *Suite, o *TaskOutcome) { o.VerifyPassed = 0; s.Thresholds.MinVerifyPassRate = 1 }, func(r ThresholdResult) bool { return !r.VerifyPassRate }},
		{"plan revisions", func(s *Suite, o *TaskOutcome) { o.PlanRevisions = 1; s.Thresholds.MaxPlanRevisions = 0 }, func(r ThresholdResult) bool { return !r.PlanRevisions }},
		{"approvals", func(s *Suite, o *TaskOutcome) {
			o.ApprovalsRequested = map[string]int{"T3": 1}
			s.Thresholds.MaxApprovalsPerEffect = map[string]int{"T3": 0}
		}, func(r ThresholdResult) bool { return !r.Approvals["T3"] }},
		{"steps", func(s *Suite, _ *TaskOutcome) { s.Thresholds.MaxSteps = 7 }, func(r ThresholdResult) bool { return !r.Steps }},
		{"wall clock", func(s *Suite, _ *TaskOutcome) { s.Thresholds.MaxWallClock = 3 * time.Millisecond }, func(r ThresholdResult) bool { return !r.WallClock }},
		{"cost", func(s *Suite, _ *TaskOutcome) { s.Thresholds.MaxCostMicros = 39 }, func(r ThresholdResult) bool { return !r.Cost }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			suite := DefaultSuite()
			outcome := TaskOutcome{Completed: true, VerifyPassed: 1, VerifyTotal: 1, Steps: 2, WallClock: time.Millisecond, CostMicros: 10}
			tc.configure(&suite, &outcome)
			run, err := NewEvaluator().Evaluate(context.Background(), Request{
				Release: release(), Suite: suite, Fixtures: passingFixtures(),
				Executor: staticExecutor(outcome),
			})
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if run.Passed || !tc.failed(run.Threshold) {
				t.Fatalf("threshold result = %+v, passed=%v; want %q threshold to fail", run.Threshold, run.Passed, tc.name)
			}
			if err := NewPublisher().Publish(run); !errors.Is(err, ErrThreshold) {
				t.Fatalf("Publish() error = %v, want ErrThreshold", err)
			}
		})
	}
}

func baseWithApproval() TaskOutcome {
	return TaskOutcome{Completed: true, VerifyPassed: 1, VerifyTotal: 1, PlanRevisions: 1, ApprovalsRequested: map[string]int{"T3": 1}, Steps: 2, WallClock: time.Millisecond, CostMicros: 10}
}

func TestTodo_AGENT2_025_Golden(t *testing.T) {
	suite := DefaultSuite()
	outcome := baseWithApproval()
	evaluate := func() Run {
		run, err := NewEvaluator().Evaluate(context.Background(), Request{Release: release(), Suite: suite, Fixtures: passingFixtures(), Executor: staticExecutor(outcome)})
		if err != nil {
			t.Fatalf("Evaluate() error = %v", err)
		}
		return run
	}
	first, second := evaluate(), evaluate()
	firstEvidence, err := first.MarshalEvidence()
	if err != nil {
		t.Fatalf("MarshalEvidence() error = %v", err)
	}
	secondEvidence, err := second.MarshalEvidence()
	if err != nil {
		t.Fatalf("second MarshalEvidence() error = %v", err)
	}
	if string(firstEvidence) != string(secondEvidence) || first.Digest == "" {
		t.Fatalf("golden evidence is not deterministic: first=%s second=%s", firstEvidence, secondEvidence)
	}
	const wantGoldenDigest = "sha256:bf6dbe287bb2687f5f342b98abd037099de28ba6c61ffe89a7f0d75f9f02330a"
	if first.Digest != wantGoldenDigest {
		t.Fatalf("golden digest = %q, want %q", first.Digest, wantGoldenDigest)
	}
	if err := first.Verify(); err != nil {
		t.Fatalf("golden Verify() error = %v", err)
	}
	first.Tasks[0].Outcome.Steps++
	if err := first.Verify(); !errors.Is(err, ErrUnsealed) {
		t.Fatalf("tampered golden Verify() error = %v, want ErrUnsealed", err)
	}
}
