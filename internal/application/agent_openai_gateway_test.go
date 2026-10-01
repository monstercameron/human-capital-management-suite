package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

type openAITestRouteRecorder struct{ records []agentmodel.RouteRecord }

func (r *openAITestRouteRecorder) RecordRoute(_ context.Context, record agentmodel.RouteRecord) error {
	r.records = append(r.records, record)
	return nil
}

type openAITestBudget struct {
	settled  agentbudget.Usage
	estimate agentbudget.Usage
	failed   bool
}

func (b *openAITestBudget) Reserve(_ context.Context, request agentbudget.Request) (agentmodel.Reservation, error) {
	b.estimate = request.Estimate
	return b, nil
}
func (b *openAITestBudget) Settle(usage agentbudget.Usage) error { b.settled = usage; return nil }
func (b *openAITestBudget) Fail() error                          { b.failed = true; return nil }

type openAITestSource struct{}

func (openAITestSource) VerifySourceClassification(_ context.Context, request agentegress.SourceClassificationRequest) error {
	if request.Tenant != "tenant-a" || request.Purpose != "persona.reply" || request.SourceClass != "persona-invoking-post" || request.DataClass != trustdlp.ClassPublic {
		return ErrOpenAICredentialScope
	}
	return nil
}

func TestTodo_AGENT_021_OpenAIGateway_HTTPBoundary(t *testing.T) {
	calls := 0
	refuse := false
	providerFailure := false
	typedBusiness := false
	countCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/responses/input_tokens" {
			countCalls++
			io.WriteString(w, `{"object":"response.input_tokens","input_tokens":20}`)
			return
		}
		calls++
		if providerFailure {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":{"message":"unavailable"}}`)
			return
		}
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer test-provider-key" {
			t.Error("provider request binding failed")
		}
		var body struct {
			Model string `json:"model"`
			Store bool   `json:"store"`
			Text  struct {
				Format struct {
					Type   string          `json:"type"`
					Strict bool            `json:"strict"`
					Schema json.RawMessage `json:"schema"`
				} `json:"format"`
			} `json:"text"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "gpt-5" || body.Store || body.Text.Format.Type != "json_schema" || !body.Text.Format.Strict || !json.Valid(body.Text.Format.Schema) {
			t.Error("provider did not receive SchemaFlux's strict schema")
		}
		if refuse {
			io.WriteString(w, `{"id":"response-2","model":"gpt-5","status":"completed","usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30},"output":[{"type":"message","content":[{"type":"refusal","refusal":"blocked"}]}]}`)
		} else if typedBusiness {
			body := `{"definition":{"intent_type_id":"leave.request","version":1},"subjects":[{"subject_kind":"person","subject_id":"person-a","authority_domain":"employment"}],"argument_fields":[{"name":"days","value_json":"1"}],"uncertainty":"low"}`
			encoded, _ := json.Marshal(body)
			io.WriteString(w, `{"id":"response-business","model":"gpt-5","status":"completed","usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30},"output":[{"type":"message","content":[{"type":"output_text","text":`+string(encoded)+`}]}]}`)
		} else {
			io.WriteString(w, `{"id":"response-1","model":"gpt-5","status":"completed","usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30},"output":[{"type":"message","content":[{"type":"output_text","text":"{\"text\":\"A real provider answer\",\"tool_proposals\":[]}"}]}]}`)
		}
	}))
	defer server.Close()
	now := time.Now().UTC()
	identity := agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt-5", Version: "2025-08-07"}
	profile := agentmodel.ModelProfile{ID: "openai.profile", Identity: identity, Regions: []string{"us"}, DataClasses: []string{string(trustdlp.ClassPublic)}, TaskProfileIDs: []string{"reply"}, MaxLatency: time.Second, MaxCostMicros: 1000, ExpectedCostMicros: 100, SemanticsDigest: "semantics", OutputSchemaDigest: PersonaChatReplySchemaDigest, ToolSchemaDigest: "tools", Evaluation: agentmodel.ModelEvaluation{AgentVersionDigest: "agent-digest", SuiteDigest: "test-suite", Passed: true}}
	profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
	selection := agentmodel.ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: identity}
	scope := OpenAIModelCredentialScope{TenantID: "tenant-a", Region: "us", Purpose: "persona.reply", Destination: profile.ID}
	authority, err := NewOpenAIModelCredentialAuthority(OpenAIModelCredentialConfig{CredentialID: "provider-key-ref", Version: "v1", Workload: "persona.worker", Scopes: []OpenAIModelCredentialScope{scope}, MaxTTL: time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := lease.NewManager(authority, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	leases, err := NewModelLeaseSource(ModelLeaseSourceConfig{Authorities: map[string]ModelCredentialAuthority{"openai": authority}, Leases: manager})
	if err != nil {
		t.Fatal(err)
	}
	trust, err := outbound.NewPolicy(outbound.Destination{Name: profile.ID, TrustBundleRef: "test-reviewed-bundle", Purposes: []string{scope.Purpose}, DataClasses: profile.DataClasses})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := trustdlp.NewPolicy(trust, trustdlp.Clearance{Destination: profile.ID, Classes: []trustdlp.DataClass{trustdlp.ClassPublic}, Decision: trustdlp.Allow})
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := trustdlp.NewInspector()
	if err != nil {
		t.Fatal(err)
	}
	egress, err := agentegress.NewEvaluator(policy, inspector, trustdlp.NewReceiptLog())
	if err != nil {
		t.Fatal(err)
	}
	pricing, err := agentmodel.NewPricingSchedule(agentmodel.PricingSchedule{Version: "test-v1", Authority: "test-admin", Signature: "test-signature", Entries: []agentmodel.PricingEntry{{Identity: identity, InputMicrosPerToken: 2, OutputMicrosPerToken: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	term := agentegress.ProviderTerms{ModelProfile: profile.ID, ProviderID: identity.ProviderID, ModelID: identity.ModelID, ModelVersion: identity.Version, ContractRef: "test-contract", EgressGrantRef: "test-grant", Approved: true, Encryption: true, AllowedRegions: []string{scope.Region}, AllowedClasses: []trustdlp.DataClass{trustdlp.ClassPublic}, Retention: agentegress.RetentionPolicy{Mode: agentegress.RetentionNone}, TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied, SourceRules: []agentegress.ProviderSourceRule{{Class: "persona-invoking-post", Classes: []trustdlp.DataClass{trustdlp.ClassPublic}}}}
	budget := &openAITestBudget{}
	routes := &openAITestRouteRecorder{}
	gatewayConfig := OpenAIAgentModelGatewayConfig{APIKey: "test-provider-key", BaseURL: server.URL, Profiles: []agentmodel.ModelProfile{profile}, Terms: []agentegress.ProviderTerms{term}, Pricing: pricing, Budget: budget, Routes: routes, Egress: egress, Leases: manager, LeaseBindings: leases, Sources: openAITestSource{}}
	gateway, err := NewOpenAIAgentModelGateway(gatewayConfig)
	if err != nil {
		t.Fatal(err)
	}
	task, err := NewTrustedModelTask(scope.TenantID, "run-1", "agent-digest", "persona.worker")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := leases.Issue(context.Background(), ModelLeaseRequest{Task: task, ProviderID: "openai", Destination: scope.Destination, Purpose: scope.Purpose, Region: scope.Region, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := leases.ValidateTaskBoundModelLease(context.Background(), scope.TenantID, "other-run", task.AgentID, "openai", issued); err == nil || calls != 0 {
		t.Fatalf("cross-task credential=%v calls=%d", err, calls)
	}
	if err := leases.ValidateTaskBoundModelLease(context.Background(), scope.TenantID, task.TaskID, task.AgentID, "openai", issued); err != nil {
		t.Fatal(err)
	}
	request := AgentModelGatewayRequest{TenantID: scope.TenantID, Route: agentmodel.RouteRequest{TraceID: "run-1", Pin: agentmodel.ModelPin{AgentVersionDigest: "agent-digest", TaskProfileID: "reply", Primary: selection, SemanticsDigest: "semantics", OutputSchemaDigest: PersonaChatReplySchemaDigest, ToolSchemaDigest: "tools"}, Task: agentmodel.TaskProfile{ID: "reply", AgentVersionDigest: "agent-digest", Region: scope.Region, DataClasses: profile.DataClasses, MaxLatency: time.Second, MaxCostMicros: 1000, SemanticsDigest: "semantics", OutputSchemaDigest: PersonaChatReplySchemaDigest, ToolSchemaDigest: "tools"}, BudgetRemainingMicros: 1000}, Dispatch: agentegress.ProviderDispatchRequest{Model: agentmodel.ModelRequest{ContractVersion: 1, TaskProfile: "reply", ModelProfile: profile.ID, TraceID: "run-1", Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "Hello"}}, Output: agentmodel.OutputConstraint{Mode: agentmodel.OutputText}, Deadline: now.Add(time.Minute), Limits: agentmodel.ModelLimits{MaxInputTokens: 100, MaxOutputTokens: 100, MaxCostMicros: 1000}, Processing: agentmodel.ProcessingPolicy{Residency: scope.Region, Retention: "NONE:0", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}}, Outbound: agentegress.OutboundRequest{TaskID: task.TaskID, Tenant: scope.TenantID, Principal: "person-a", Purpose: scope.Purpose, Profile: agentegress.Profile{ID: profile.ID, Kind: agentegress.TargetModel, AllowedRegions: term.AllowedRegions, AllowedClasses: term.AllowedClasses, Retention: term.Retention}, Region: scope.Region, DeclaredFields: []string{"model.message.0"}, Fields: []agentegress.Field{{Name: "model.message.0", Value: "Hello", Class: trustdlp.ClassPublic, Provenance: []string{"persona-run:run-1"}}}, Task: agentegress.TaskPolicy{AllowedRegions: term.AllowedRegions, AllowedResultClasses: term.AllowedClasses, ResultRetention: time.Hour}, Now: now}, FieldSources: map[string]string{"model.message.0": "persona-invoking-post"}, Lease: issued}}
	request.Dispatch.Outbound.Fields[0].Taint = []string{"USER_INPUT"}
	forged := request
	forged.Dispatch.Outbound.TaskID = "other-run"
	if _, err := gateway.Dispatch(context.Background(), forged); !errors.Is(err, ErrAgentModelGatewayLease) || calls != 0 || countCalls != 0 || len(routes.records) != 0 {
		t.Fatalf("cross-task dispatch err=%v calls=%d count=%d routes=%d", err, calls, countCalls, len(routes.records))
	}
	result, err := gateway.Dispatch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Dispatch.Model.Text != "A real provider answer" || result.Dispatch.Model.Usage.CostMicros != 120 || calls != 1 || countCalls != 1 || budget.settled.SpendMicros != 120 || budget.settled.WallClock <= 0 || budget.estimate.WallClock != time.Second || budget.estimate.Tokens != 200 || len(routes.records) != 1 || result.Dispatch.LeaseID != issued.ID {
		t.Fatalf("dispatch=%+v calls=%d budget=%+v", result, calls, budget.settled)
	}
	if _, err := gateway.Dispatch(context.Background(), request); err == nil || calls != 1 {
		t.Fatalf("replay err=%v calls=%d", err, calls)
	}
	refuse = true
	budget.failed = false
	budget.settled = agentbudget.Usage{}
	issued, err = leases.Issue(context.Background(), ModelLeaseRequest{Task: task, ProviderID: "openai", Destination: scope.Destination, Purpose: scope.Purpose, Region: scope.Region, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	request.Dispatch.Lease = issued
	if denied, err := gateway.Dispatch(context.Background(), request); err != nil || denied.Dispatch.Model.Refusal == nil || calls != 2 || budget.failed || budget.settled.SpendMicros != 120 || budget.settled.Tokens != 30 {
		t.Fatalf("billed refusal err=%v calls=%d budget=%+v failed=%v", err, calls, budget.settled, budget.failed)
	}
	providerFailure = true
	budget.settled = agentbudget.Usage{}
	issued, err = leases.Issue(context.Background(), ModelLeaseRequest{Task: task, ProviderID: "openai", Destination: scope.Destination, Purpose: scope.Purpose, Region: scope.Region, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	request.Dispatch.Lease = issued
	if _, err := gateway.Dispatch(context.Background(), request); err == nil || calls != 3 || budget.failed || budget.settled.Steps != 1 || budget.settled.WallClock <= 0 || budget.settled.SpendMicros != 0 {
		t.Fatalf("failed attempt err=%v calls=%d settled=%+v failed=%v", err, calls, budget.settled, budget.failed)
	}
	t.Run("native typed business contract", func(t *testing.T) {
		providerFailure, refuse, typedBusiness = false, false, true
		profile.OutputSchemaDigest = taskModelDigest([]byte(AgentActionModelJSONSchema))
		profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
		gatewayConfig.Profiles = []agentmodel.ModelProfile{profile}
		gateway, err = NewOpenAISchemaFluxTypedAgentModelGateway[AgentActionModelCandidate](gatewayConfig)
		if err != nil {
			t.Fatal(err)
		}
		request.Route.Pin.Primary.ProfileDigest = profile.ProfileDigest
		request.Route.Pin.OutputSchemaDigest = profile.OutputSchemaDigest
		request.Route.Task.OutputSchemaDigest = profile.OutputSchemaDigest
		request.Dispatch.Model.Output = agentmodel.OutputConstraint{Mode: agentmodel.OutputSchema, Schema: json.RawMessage(AgentActionModelJSONSchema)}
		request.Dispatch.Model.RequiredFeatures = []agentmodel.ModelFeature{agentmodel.FeatureStructuredJSON}
		drift := request
		drift.Dispatch.Model.Output.Schema = append([]byte(" "), drift.Dispatch.Model.Output.Schema...)
		beforeRoutes := len(routes.records)
		if _, err := gateway.Dispatch(context.Background(), drift); !errors.Is(err, ErrAgentModelGatewayPin) || calls != 3 || countCalls != 3 || len(routes.records) != beforeRoutes {
			t.Fatalf("unapproved output contract err=%v calls=%d counts=%d routes=%d", err, calls, countCalls, len(routes.records))
		}
		issued, err = leases.Issue(context.Background(), ModelLeaseRequest{Task: task, ProviderID: "openai", Destination: scope.Destination, Purpose: scope.Purpose, Region: scope.Region, TTL: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		request.Dispatch.Lease = issued
		result, err := gateway.Dispatch(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		var candidate AgentActionModelCandidate
		if json.Unmarshal(result.Dispatch.Model.Structured, &candidate) != nil || candidate.Definition.IntentTypeID != "leave.request" || result.Dispatch.Model.Text != "" || calls != 4 || countCalls != 4 || budget.settled.SpendMicros != 120 {
			t.Fatalf("typed candidate=%+v calls=%d counts=%d budget=%+v", candidate, calls, countCalls, budget.settled)
		}
	})
}
