package agentsystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// personaFixtureOwner supplies only static synthetic records. Runner task
// transitions and observations remain owned by the real agentsystem runtime.
type personaFixtureOwner struct{ sourceDigest string }

func (o personaFixtureOwner) Prepare(_ context.Context, req PrepareRequest) (Prepared, error) {
	if !strings.HasPrefix(req.Task.TenantID, "synthetic-") {
		return Prepared{}, errors.New("fixture owner refuses non-synthetic tenant")
	}
	prepared := Prepared{Purpose: purposeKey}
	switch req.Step.ID {
	case "fixture-read":
		if req.Step.Type != agentrun.StepRead {
			return Prepared{}, errors.New("fixture read step has an unexpected type")
		}
	case "fixture-analyze":
		if req.Step.Type != agentrun.StepAnalyze {
			return Prepared{}, errors.New("fixture analysis step has an unexpected type")
		}
		prepared.Egress = egressCall(agentegress.TargetModel, "model.eu", publicField("goal", req.Task.Goal))
	case "fixture-verify-step":
		if req.Step.Type != agentrun.StepVerify {
			return Prepared{}, errors.New("fixture verification step has an unexpected type")
		}
	default:
		return Prepared{}, fmt.Errorf("fixture owner has no record for step %q", req.Step.ID)
	}
	return prepared, nil
}

func (o personaFixtureOwner) Invoke(_ context.Context, call Invocation) (Result, error) {
	switch call.Step.ID {
	case "fixture-read":
		return Result{Ref: "fixture:source", Digest: o.sourceDigest}, nil
	case "fixture-verify-step":
		for _, entry := range call.Task.Ledger.Entries {
			if entry.Kind == "STEP_RESULT" && entry.Ref == "fixture:source" && entry.Digest == o.sourceDigest {
				return Result{Ref: "fixture:verification", Digest: digestFixtureValue(entry.Ref, entry.Digest)}, nil
			}
		}
		return Result{}, errors.New("fixture source result is absent from the durable task ledger")
	default:
		return Result{}, fmt.Errorf("fixture owner has no record for step %q", call.Step.ID)
	}
}

func (personaFixtureOwner) Retain(context.Context, agentrun.AgentTask, agentrun.PlanStep, string, agentsecurity.QuarantineExtraction) error {
	return errors.New("fixture owner does not retain untrusted external content")
}

func (o personaFixtureOwner) Verify(_ context.Context, _ agentrun.AgentTask, step agentrun.PlanStep, result agentrun.StepResult) error {
	if step.ID != "fixture-verify-step" || result.Ref != "fixture:verification" || result.Digest != digestFixtureValue("fixture:source", o.sourceDigest) {
		return errors.New("fixture verification does not match the recorded source result")
	}
	return nil
}

func digestFixtureValue(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = fmt.Fprintf(hash, "%s\x00", value)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func TestPersonaFixtureOwnerRejectsNonFixtureOperations(t *testing.T) {
	owner := personaFixtureOwner{sourceDigest: digestFixtureValue("fixture", "v1")}
	if _, err := owner.Prepare(context.Background(), PrepareRequest{Task: agentrun.AgentTask{TenantID: "tenant-live"}}); err == nil {
		t.Fatal("fixture owner prepared a non-synthetic tenant")
	}
	if _, err := owner.Prepare(context.Background(), PrepareRequest{Task: agentrun.AgentTask{TenantID: "synthetic-fixture"}, Step: agentrun.PlanStep{ID: "unknown"}}); err == nil {
		t.Fatal("fixture owner prepared an unregistered step")
	}
	if _, err := owner.Invoke(context.Background(), Invocation{Step: agentrun.PlanStep{ID: "unknown"}}); err == nil {
		t.Fatal("fixture owner served an unregistered step")
	}
	if err := owner.Retain(context.Background(), agentrun.AgentTask{}, agentrun.PlanStep{}, "external", agentsecurity.QuarantineExtraction{}); err == nil {
		t.Fatal("fixture owner retained untrusted external content")
	}
	if err := owner.Verify(context.Background(), agentrun.AgentTask{}, agentrun.PlanStep{ID: "fixture-verify-step"}, agentrun.StepResult{Ref: "fixture:verification", Digest: "sha256:" + strings.Repeat("0", 64)}); err == nil {
		t.Fatal("fixture owner accepted a verification unrelated to its record")
	}
}

func TestTodo_AGENTP_021_SyntheticRunnerUsesFixtureOwnerAndDurableEvents(t *testing.T) {
	// newFixture installs SchemaFlux's deterministic local provider and no network adapter.
	base := newFixture(t, nil)
	const tenant values.TenantId = "synthetic-persona-runner"
	scopes := &memoryScopers{grants: map[values.TenantId]*agentdelegation.MemoryGrantStore{}, tasks: map[values.TenantId]*agentrun.MemoryStore{}}
	ledger, err := agentbudget.NewWithPersistence(testPolicy(), func() time.Time { return fixedNow }, nil)
	if err != nil {
		t.Fatal(err)
	}
	sourceDigest := digestFixtureValue("policy-record", "fixture-v1")
	cfg := base.platform.cfg
	cfg.Budget = ledger
	cfg.Audit = agentaudit.NewMemoryStore()
	cfg.Grants = grantScoper{scopes}
	cfg.Tasks = taskScoper{scopes}
	cfg.Owner = personaFixtureOwner{sourceDigest: sourceDigest}
	cfg.Connections = nil
	cfg.Authority = agentdelegation.ResolverFunc(func(userID string, resolvedTenant values.TenantId, _ string, _ time.Time) (agentdelegation.UserAuthority, error) {
		current := userAuthority(true)
		current.UserID = userID
		current.Authority.Tenant = resolvedTenant
		return current, nil
	})
	platform, err := NewPlatform(cfg)
	if err != nil {
		t.Fatalf("NewPlatform(): %v", err)
	}
	userScope := userAuthority(true).Authority
	userScope.Tenant = tenant
	runner, task, err := platform.StartSyntheticTask(context.Background(), tenant, syntheticTenantAuthorityFunc(func(_ context.Context, got values.TenantId) error {
		if got != tenant || !strings.HasPrefix(string(got), "synthetic-") {
			return ErrSyntheticTenantDenied
		}
		return nil
	}), StartRequest{
		TaskID: "eval-persona-runner-001", UserID: "user-42", AgentVersion: "persona-eval/v1", InstallationID: "synthetic-install",
		Purpose: purposeKey, OrganizationScopeID: "org-west", Goal: "answer from the reviewed policy fixture",
		Steps: []agentrun.PlanStep{
			planStep("fixture-read", agentrun.StepRead, "skill.lookup", agentrun.TierRead),
			planStep("fixture-analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft),
			{ID: "fixture-verify-step", Type: agentrun.StepVerify, SkillID: "skill.lookup", SkillVersion: 1, ExpectedOutput: "verify fixture source", Tier: agentrun.TierRead, Inputs: []agentrun.InputRef{{Name: "source", Ref: "fixture:source"}}},
		},
		UserAuthority: userScope, Lifetime: time.Hour,
	})
	if err != nil {
		t.Fatalf("StartSyntheticTask(): %v", err)
	}
	task, err = runner.Runtime.ConfirmPlan(context.Background(), task.ID, task.UserID, task.Version, fixedNow)
	if err != nil {
		t.Fatalf("ConfirmPlan(): %v", err)
	}
	for task.State != agentrun.StateCompleted {
		task, err = runner.Step(context.Background(), task.ID, ModeOnBehalfOf)
		if err != nil {
			t.Fatalf("Runner.Step(): %v", err)
		}
	}
	stored, err := runner.Runtime.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := runner.Runtime.TaskEvents(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var executions, verifications int
	for _, event := range events {
		if err := event.Validate(); err != nil {
			t.Fatalf("durable event failed validation: %v", err)
		}
		if event.Type == agentrun.TaskEventStepExecution {
			executions++
		}
		if event.Type == agentrun.TaskEventStepVerification && event.Outcome == "VERIFIED" {
			verifications++
		}
	}
	if stored.TenantID != string(tenant) || stored.State != agentrun.StateCompleted || stored.CurrentStep != len(stored.Plan.Steps) || executions != 3 || verifications != 1 {
		t.Fatalf("durable Runner evidence = task(%s,%s,%d) executions=%d verified=%d", stored.TenantID, stored.State, stored.CurrentStep, executions, verifications)
	}
	if len(stored.Ledger.Entries) < 4 || stored.Ledger.Entries[len(stored.Ledger.Entries)-1].Kind != "STEP_RESULT" {
		t.Fatalf("completed task lacks durable step results: %+v", stored.Ledger.Entries)
	}
}
