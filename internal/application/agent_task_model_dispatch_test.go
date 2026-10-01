package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

type taskModelTestDispatch func(context.Context, agentmodel.Request, agentmodel.TypedModelInput) (agentmodel.ModelResult, error)

func (f taskModelTestDispatch) DispatchTypedModel(ctx context.Context, req agentmodel.Request, input agentmodel.TypedModelInput) (agentmodel.ModelResult, error) {
	return f(ctx, req, input)
}

func TestTodo_AGENT_017_IntegrationTaskModelAuthority(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	runtime, err := composeAgentRuntime(context.Background(), agentRuntimeInput{Pool: f.pool, Cell: f.cell, Config: ServeConfig{Profile: ServeProfileLocalDev, AgentModelConfigFile: "test-approved-deployment"}, Env: func(string) string { return "" }, Now: time.Now, Tenants: []string{f.tenant}})
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	err = BindAgentRuntimeTypedModel(runtime, taskModelTestDispatch(func(ctx context.Context, req agentmodel.Request, input agentmodel.TypedModelInput) (agentmodel.ModelResult, error) {
		calls++
		profile := agentmodel.ModelProfile{ID: "task.openai", Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "test-model", Version: "v1"}, Regions: []string{"us-east"}, DataClasses: []string{"PUBLIC"}, TaskProfileIDs: []string{AgentTaskModelSkillProfileID(req.Skill, input.Schema)}, MaxLatency: time.Minute, MaxCostMicros: 100_000, ExpectedCostMicros: 100, SemanticsDigest: "test-semantics", OutputSchemaDigest: taskModelDigest(input.Schema), ToolSchemaDigest: "test-tools", Evaluation: agentmodel.ModelEvaluation{AgentVersionDigest: req.Actor.AgentVersion, SuiteDigest: "test-qualification", Passed: true}}
		profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
		term := agentegress.ProviderTerms{ModelProfile: profile.ID, ProviderID: "openai", ModelID: "test-model", ModelVersion: "v1", Approved: true, Encryption: true, ContractRef: "test-reviewed-contract", EgressGrantRef: "test-reviewed-egress", AllowedRegions: profile.Regions, AllowedClasses: []trustdlp.DataClass{trustdlp.ClassPublic}, Retention: agentegress.RetentionPolicy{Mode: agentegress.RetentionNone}, TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied, SourceRules: []agentegress.ProviderSourceRule{{Class: AgentTaskApprovedInputSource, Classes: []trustdlp.DataClass{trustdlp.ClassPublic}}}}
		deployment := PersonaModelDeployment{Profiles: []agentmodel.ModelProfile{profile}, Terms: []agentegress.ProviderTerms{term}, Credential: PersonaModelCredentialDeployment{Scopes: []OpenAIModelCredentialScope{{TenantID: req.TenantID, Region: "us-east", Purpose: req.Purpose, Destination: profile.ID}}}}
		source, err := NewAgentTaskModelRequestSource(AgentTaskModelRequestSourceConfig{Platform: runtime.Platform, Deployment: deployment, Workload: agentWorkload, LeaseTTL: time.Minute, Now: time.Now, Audit: runtime.Audit})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := source.BuildTypedAgentModelRequest(ctx, req, input); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
			t.Fatalf("unbound source=%v", err)
		}
		manager, err := lease.NewManager(modelLeaseCustody{now: time.Now()}, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		credentialOwner := &modelLeaseAuthority{handle: custody.Handle{ID: "opaque-task-model", Version: "v1", Kind: custody.Secret, Tenant: req.TenantID, Region: "us-east"}}
		credentials, err := NewModelLeaseSource(ModelLeaseSourceConfig{Authorities: map[string]ModelCredentialAuthority{"openai": credentialOwner}, Leases: manager, MaxTTL: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		if err := source.BindCredentials(credentials); err != nil {
			t.Fatal(err)
		}
		if err := source.BindCredentials(credentials); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
			t.Fatalf("credential rebind=%v", err)
		}
		changed := req
		changed.Prompt = "caller replacement"
		if _, err := source.BuildTypedAgentModelRequest(ctx, changed, input); !errors.Is(err, agentsystem.ErrDenied) {
			t.Fatalf("prompt substitution=%v", err)
		}
		changedInput := input
		changedInput.Schema = json.RawMessage(`{"type":"object"}`)
		if _, err := source.BuildTypedAgentModelRequest(ctx, req, changedInput); !errors.Is(err, ErrAgentModelGatewayPin) {
			t.Fatalf("unqualified schema=%v", err)
		}
		if credentialOwner.seen.TaskID != "" {
			t.Fatal("refused input requested a credential")
		}
		built, err := source.BuildTypedAgentModelRequest(ctx, req, input)
		if err != nil {
			t.Fatal(err)
		}
		if built.Dispatch.Outbound.TaskID != req.Actor.TaskID || built.Dispatch.Outbound.Principal != req.Actor.UserID || built.Route.Pin.AgentVersionDigest != req.Actor.AgentVersion || built.Route.Pin.OutputSchemaDigest != taskModelDigest(input.Schema) || built.Dispatch.Model.Output.Mode != agentmodel.OutputSchema || built.Dispatch.Model.Limits.MaxInputTokens+built.Dispatch.Model.Limits.MaxOutputTokens != req.Estimate.Tokens || credentialOwner.seen.TaskID != req.Actor.TaskID {
			t.Fatalf("trusted build=%+v", built)
		}
		field := built.Dispatch.Outbound.Fields[0]
		proof := agentegress.SourceClassificationRequest{Tenant: req.TenantID, Purpose: req.Purpose, FieldName: field.Name, SourceClass: AgentTaskApprovedInputSource, DataClass: field.Class, Provenance: field.Provenance, ValueDigest: taskModelDigest([]byte(input.Prompt))}
		if err := source.VerifySourceClassification(ctx, proof); !errors.Is(err, ErrAgentModelGatewayTenant) {
			t.Fatalf("source without call binding=%v", err)
		}
		bound := context.WithValue(ctx, agentTypedModelEvidenceContextKey{}, built)
		if err := source.VerifySourceClassification(bound, proof); err != nil {
			t.Fatal(err)
		}
		proof.ValueDigest = taskModelDigest([]byte("different input"))
		if err := source.VerifySourceClassification(bound, proof); !errors.Is(err, ErrAgentModelGatewayPin) {
			t.Fatalf("source hash replacement=%v", err)
		}
		router, err := agentmodel.NewRouter([]agentmodel.ModelProfile{profile}, source)
		if err != nil {
			t.Fatal(err)
		}
		route, err := router.Route(bound, built.Route)
		if err != nil || route.Selected != built.Route.Pin.Primary {
			t.Fatalf("durable route=%+v,%v", route, err)
		}
		return agentmodel.ModelResult{Finish: agentmodel.FinishComplete, Structured: json.RawMessage(`{"text":"trusted private answer","citations":[]}`)}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	started, err := runtime.Starter.StartTask(context.Background(), agentTestPrincipal(t, f.tenant, agentTestWorker), "where do I work?")
	if err != nil || started.State != string(agentrun.StateCompleted) || calls != 1 {
		t.Fatalf("task=%+v,%v,model calls=%d", started, err, calls)
	}
}

func TestTodo_AGENT_017_TaskModelSourceRefusesMissingOwners(t *testing.T) {
	if _, err := NewAgentTaskModelRequestSource(AgentTaskModelRequestSourceConfig{}); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatalf("missing owners=%v", err)
	}
	var source *AgentTaskModelRequestSource
	if err := source.BindCredentials(nil); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatal(err)
	}
	if err := source.VerifySourceClassification(nil, agentegress.SourceClassificationRequest{}); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatal(err)
	}
	if err := source.RecordRoute(nil, agentmodel.RouteRecord{}); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatal(err)
	}
}
