package application

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

func agentegressDispatchResultFor079(proposal []byte) agentegress.DispatchResult {
	return agentegress.DispatchResult{Model: agentmodel.ModelResult{Structured: proposal, Finish: agentmodel.FinishComplete}}
}

func agentegressDispatchRefusedFor079() agentegress.DispatchResult {
	return agentegress.DispatchResult{Model: agentmodel.ModelResult{Refusal: &agentmodel.ModelRefusal{Detail: "test refusal"}, Finish: agentmodel.FinishComplete}}
}

func writeAgentUX079TestFile(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// agentUX079AmbientScreenTestEvaluation is a passing evaluation for the
// screen task's pinned instruction version.
func agentUX079AmbientScreenTestEvaluation() agentmodel.ModelEvaluation {
	return agentmodel.ModelEvaluation{AgentVersionDigest: AgentUX079AmbientScreenAgentVersionDigest(), SuiteDigest: "sha256:agentux-ambient-screen-suite", Passed: true}
}

func agentUX079AmbientScreenTestDeployment(t *testing.T) PersonaModelDeployment {
	t.Helper()
	dep, err := NewAgentUX079AmbientScreenDeployment(modelDeploymentFixture(t), agentUX079AmbientScreenTestEvaluation(), 10*time.Second, 20000)
	if err != nil {
		t.Fatal(err)
	}
	return dep
}

func agentUX079AmbientScreenTestLedger(t *testing.T) *agentbudget.Ledger {
	t.Helper()
	ledger, err := agentbudget.New(agentBudgetPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

// TestAgentUX079_AmbientScreenDeploymentQualifies: a screen-model deployment
// of the writing-style kind qualifies exactly one profile for the screen
// task, and anything that differs is refused without widening the approval.
func TestAgentUX079_AmbientScreenDeploymentQualifies(t *testing.T) {
	dep := agentUX079AmbientScreenTestDeployment(t)
	raw, err := json.Marshal(dep)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ParsePersonaModelDeployment(raw)
	if err != nil {
		t.Fatalf("the generated approval document does not load: %v", err)
	}
	ledger := agentUX079AmbientScreenTestLedger(t)
	binding, err := NewAgentUX079AmbientScreenBinding(loaded, &ModelLeaseSource{}, ledger, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Ready() {
		t.Fatal("a qualified binding is not ready")
	}
	task := AgentUX079AmbientScreenTaskProfile()
	profile := binding.Profile
	if len(loaded.Profiles) != 1 || len(loaded.Terms) != 1 || len(loaded.Destinations) != 1 || len(loaded.Clearances) != 1 ||
		profile.TaskProfileIDs[0] != task.ID || profile.SemanticsDigest != task.SemanticsDigest || profile.MaxLatency != 10*time.Second || profile.MaxCostMicros != 20000 {
		t.Fatalf("approval document shape: %+v", profile)
	}
	if loaded.Destinations[0].Purposes[0] != AgentUX079AmbientScreenPurpose {
		t.Fatal("destination does not carry only the screen purpose")
	}
	for _, scope := range loaded.Credential.Scopes {
		if scope.Purpose != AgentUX079AmbientScreenPurpose {
			t.Fatal("credential scope outside the screen purpose", scope)
		}
	}
	sources := map[string]bool{}
	for _, rule := range loaded.Terms[0].SourceRules {
		sources[rule.Class] = true
	}
	if len(sources) != 2 {
		t.Fatalf("terms approve more or less than the two screen fields: %v", sources)
	}
	if _, err := NewAgentUX079AmbientScreenBinding(modelDeploymentFixture(t), &ModelLeaseSource{}, ledger, time.Now); err == nil {
		t.Fatal("an unqualified deployment qualified")
	}
	for name, mutate := range map[string]func(*PersonaModelDeployment){
		"evaluation not passed": func(d *PersonaModelDeployment) { d.Profiles[0].Evaluation.Passed = false },
		"evaluated another instruction version": func(d *PersonaModelDeployment) {
			d.Profiles[0].Evaluation.AgentVersionDigest = "hcm-ambient-screen/other"
		},
		"other semantics": func(d *PersonaModelDeployment) { d.Profiles[0].SemanticsDigest = "other" },
		"too slow":        func(d *PersonaModelDeployment) { d.Profiles[0].MaxLatency = 2 * time.Minute },
		"too expensive": func(d *PersonaModelDeployment) {
			d.Profiles[0].MaxCostMicros = task.MaxCostMicros + 1
		},
		"missing data class": func(d *PersonaModelDeployment) { d.Profiles[0].DataClasses = nil },
		"terms not approved": func(d *PersonaModelDeployment) { d.Terms[0].Approved = false },
		"wrong purpose": func(d *PersonaModelDeployment) {
			d.Destinations[0].Purposes = []string{"other.purpose"}
		},
	} {
		mutated := agentUX079AmbientScreenTestDeployment(t)
		mutate(&mutated)
		if _, err := NewAgentUX079AmbientScreenBinding(mutated, &ModelLeaseSource{}, ledger, time.Now); err == nil {
			t.Fatalf("%s qualified", name)
		}
	}
	if _, err := NewAgentUX079AmbientScreenBinding(dep, nil, ledger, time.Now); err == nil {
		t.Fatal("a binding without leases qualified")
	}
	if _, err := NewAgentUX079AmbientScreenBinding(dep, &ModelLeaseSource{}, nil, time.Now); err == nil {
		t.Fatal("a binding without a ledger qualified")
	}
}

// agentUX079AmbientScreenGatewayFixture records the request the planner built
// and answers the canned model result.
type agentUX079AmbientScreenGatewayFixture struct {
	requests []AgentModelGatewayRequest
	result   AgentModelGatewayResult
	err      error
}

func (f *agentUX079AmbientScreenGatewayFixture) Dispatch(_ context.Context, req AgentModelGatewayRequest) (AgentModelGatewayResult, error) {
	f.requests = append(f.requests, req)
	return f.result, f.err
}

func agentUX079AmbientScreenTestInput() AgentUXAmbientModelInput {
	return AgentUXAmbientModelInput{
		SourceKind: "CHAT_MESSAGE", Untrusted: true,
		Message: AgentUXAmbientMessage{Tenant: "tenant-a", Conversation: "room", ID: "post-1", Author: "ir-008-curtis-bell", Body: "Curtis, can you draft the rota for next week and share it here?", Revision: 3, Zone: "UTC"},
	}
}

func agentUX079AmbientScreenTestStack(t *testing.T, gateway *agentUX079AmbientScreenGatewayFixture) AgentUX079AmbientScreenPlanner {
	t.Helper()
	dep := agentUX079AmbientScreenTestDeployment(t)
	ledger := agentUX079AmbientScreenTestLedger(t)
	material, err := LoadOrCreateLocalPersonaModelSigningMaterial(t.TempDir() + "/signing.json")
	if err != nil {
		t.Fatal(err)
	}
	evidence := &AgentUX079AmbientScreenEvidence{Audit: agentaudit.NewMemoryStore(), Now: time.Now}
	composed, _, err := ComposePersonaRuntimeTypedModel(dep, "test-only-provider-key", material.OutputSeed, material.WorkloadSeed,
		PersonaModelDeploymentDependencies{Budget: agentmodel.BudgetAdapter{Ledger: ledger}, Routes: evidence, Sources: evidence, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewAgentUX079AmbientScreenBinding(dep, composed.Leases, ledger, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return AgentUX079AmbientScreenPlanner{Gateway: gateway, Binding: binding}
}

func agentUX079AmbientScreenProposalJSON() []byte {
	raw, _ := json.Marshal(map[string]any{"kind": "TASK", "title": "draft the rota for next week", "owner": "Curtis", "date": "next week", "clock": "", "event": "", "explicit": false})
	return raw
}

// TestAgentUX079_AmbientScreenPlannerDispatches: the planner runs the
// adapter's quarantined envelope through the governed gateway and returns the
// model's proposal bytes; the adapter decodes them strictly. Refusals,
// failures and unknown agents never produce a proposal.
func TestAgentUX079_AmbientScreenPlannerDispatches(t *testing.T) {
	proposal := agentUX079AmbientScreenProposalJSON()
	gateway := &agentUX079AmbientScreenGatewayFixture{result: AgentModelGatewayResult{Dispatch: agentegressDispatchResultFor079(proposal)}}
	planner := agentUX079AmbientScreenTestStack(t, gateway)
	input := agentUX079AmbientScreenTestInput()
	messages, err := AgentUXAmbientModelMessages("task-catcher", input)
	if err != nil {
		t.Fatal(err)
	}
	source := AgentUXAmbientRunSource{Kind: "CHAT_MESSAGE", Tenant: "tenant-a", Conversation: "room", Post: "post-1", Author: "ir-008-curtis-bell", Revision: 3}
	raw, err := planner.PlanAmbientMessage(context.Background(), source, "task-catcher", "screen-key-1", messages)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(proposal) {
		t.Fatalf("planner returned %s, want the model's proposal bytes", raw)
	}
	if len(gateway.requests) != 1 {
		t.Fatalf("dispatches = %d, want one governed call", len(gateway.requests))
	}
	req := gateway.requests[0]
	if req.TenantID != "tenant-a" || req.Route.Task.ID != AgentUX079AmbientScreenTaskProfileID || req.Route.Pin.TaskProfileID != AgentUX079AmbientScreenTaskProfileID || req.Dispatch.Outbound.Purpose != AgentUX079AmbientScreenPurpose {
		t.Fatalf("request routes %+v with purpose %q, want the screen task and purpose", req.Route.Task, req.Dispatch.Outbound.Purpose)
	}
	if len(req.Dispatch.Model.Messages) != 2 || !reflect.DeepEqual(req.Dispatch.Model.Messages[0], messages[0]) || !reflect.DeepEqual(req.Dispatch.Model.Messages[1], messages[1]) || len(req.Dispatch.Model.Tools) != 0 {
		t.Fatal("the gateway call must forward the adapter's quarantined envelope verbatim with no tools")
	}
	if len(req.Dispatch.Outbound.DeclaredFields) != 2 || len(req.Dispatch.Outbound.Fields) != 2 {
		t.Fatal("the gateway call must declare exactly the two screened fields")
	}
	// The adapter decodes the planner's bytes strictly: a full proposal.
	decoded, err := AgentUXAmbientModelAdapter{Planner: planner}.ExtractAmbient(context.Background(), "task-catcher", input)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != "TASK" || decoded.Title != "draft the rota for next week" {
		t.Fatalf("decoded proposal = %+v", decoded)
	}

	refused := &agentUX079AmbientScreenGatewayFixture{result: AgentModelGatewayResult{Dispatch: agentegressDispatchRefusedFor079()}}
	if _, err := agentUX079AmbientScreenTestStack(t, refused).PlanAmbientMessage(context.Background(), source, "task-catcher", "screen-key-2", messages); err == nil {
		t.Fatal("a refused call produced a proposal")
	}
	failed := &agentUX079AmbientScreenGatewayFixture{result: AgentModelGatewayResult{}}
	if _, err := agentUX079AmbientScreenTestStack(t, failed).PlanAmbientMessage(context.Background(), source, "task-catcher", "screen-key-3", messages); err == nil {
		t.Fatal("a failed call produced a proposal")
	}
	if _, err := planner.PlanAmbientMessage(context.Background(), source, "unknown-agent", "screen-key-4", messages); err == nil {
		t.Fatal("an unknown agent produced a proposal")
	}
	if _, err := (AgentUX079AmbientScreenPlanner{Binding: planner.Binding}).PlanAmbientMessage(context.Background(), source, "task-catcher", "screen-key-5", messages); err == nil {
		t.Fatal("a planner without a gateway produced a proposal")
	}
	if _, err := (AgentUX079AmbientScreenPlanner{Gateway: gateway}).PlanAmbientMessage(context.Background(), source, "task-catcher", "screen-key-6", messages); err == nil {
		t.Fatal("a planner without a binding produced a proposal")
	}
}

// TestAgentUX079_AmbientScreenAbsent: without a planner nothing reads a
// message (the adapter denies), and the composition states the reason.
func TestAgentUX079_AmbientScreenAbsent(t *testing.T) {
	if _, err := (AgentUXAmbientModelAdapter{}).ExtractAmbient(context.Background(), "task-catcher", agentUX079AmbientScreenTestInput()); !errors.Is(err, ErrAgentUXAmbientDenied) {
		t.Fatalf("absent planner err = %v, want denied", err)
	}
	planner, reason, err := composeAgentUX079AmbientScreenPlanner(context.Background(), agentUX079AmbientScreenServeInput{Config: ServeConfig{Profile: ServeProfileStandard}, Env: func(string) string { return "" }, Now: time.Now})
	if err != nil || planner != nil || !strings.Contains(reason, EnvAgentUX079AmbientScreenModelConfigFile) {
		t.Fatalf("absent composition = (%v, %q, %v), want nil with the stated reason", planner, reason, err)
	}
}

// TestAgentUX079_AmbientScreenComposition: the served composition wires the
// real gateway behind the planner when a qualified deployment is configured,
// and states the reason for every way it is not.
func TestAgentUX079_AmbientScreenComposition(t *testing.T) {
	dep := agentUX079AmbientScreenTestDeployment(t)
	raw, err := json.Marshal(dep)
	if err != nil {
		t.Fatal(err)
	}
	deploymentPath := t.TempDir() + "/ambient-screen-deployment.json"
	writeAgentUX079TestFile(t, deploymentPath, raw)
	env := func(key string) string {
		switch key {
		case EnvAgentUX079AmbientScreenModelConfigFile:
			return deploymentPath
		case "MODEL_API_KEY":
			return "test-only-provider-key"
		}
		return ""
	}
	ledger := agentUX079AmbientScreenTestLedger(t)
	runtime := &agentRuntime{Budget: ledger, Audit: agentaudit.NewMemoryStore()}
	var outputSeed, workloadSeed [32]byte
	if _, err := rand.Read(outputSeed[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(workloadSeed[:]); err != nil {
		t.Fatal(err)
	}
	outputSeeds := base64.StdEncoding.EncodeToString(outputSeed[:])
	workloadSeeds := base64.StdEncoding.EncodeToString(workloadSeed[:])
	config := ServeConfig{Profile: ServeProfileStandard, CellID: dep.Worker.Cell, PersonaOutputSigningSeed: outputSeeds, PersonaWorkloadSigningSeed: workloadSeeds}
	planner, reason, err := composeAgentUX079AmbientScreenPlanner(context.Background(), agentUX079AmbientScreenServeInput{Config: config, Runtime: runtime, Env: env, Now: time.Now})
	if err != nil || reason != "" || planner == nil {
		t.Fatalf("composed = (%v, %q, %v), want a planner with no reason", planner, reason, err)
	}
	if _, ok := planner.(AgentUX079AmbientScreenPlanner); !ok {
		t.Fatalf("planner type = %T, want the screen planner", planner)
	}

	unqualified := modelDeploymentFixture(t)
	unqualifiedRaw, _ := json.Marshal(unqualified)
	unqualifiedPath := t.TempDir() + "/unqualified.json"
	writeAgentUX079TestFile(t, unqualifiedPath, unqualifiedRaw)
	unqualifiedEnv := func(key string) string {
		switch key {
		case EnvAgentUX079AmbientScreenModelConfigFile:
			return unqualifiedPath
		case "MODEL_API_KEY":
			return "test-only-provider-key"
		}
		return ""
	}
	unqualifiedConfig := ServeConfig{Profile: ServeProfileStandard, CellID: "test-cell", PersonaOutputSigningSeed: outputSeeds, PersonaWorkloadSigningSeed: workloadSeeds}
	planner, reason, err = composeAgentUX079AmbientScreenPlanner(context.Background(), agentUX079AmbientScreenServeInput{Config: unqualifiedConfig, Runtime: runtime, Env: unqualifiedEnv, Now: time.Now})
	if err != nil || planner != nil || reason == "" {
		t.Fatalf("unqualified composition = (%v, %q, %v), want nil with the stated reason", planner, reason, err)
	}
	if planner, reason, err = composeAgentUX079AmbientScreenPlanner(context.Background(), agentUX079AmbientScreenServeInput{Config: unqualifiedConfig, Env: env, Now: time.Now}); err != nil || planner != nil || reason == "" {
		t.Fatalf("runtime-less composition = (%v, %q, %v), want nil with the stated reason", planner, reason, err)
	}
}
