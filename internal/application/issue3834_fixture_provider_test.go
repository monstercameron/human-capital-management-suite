package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	modelopenai "github.com/monstercameron/human-capital-management-suite/internal/agentmodel/openai"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

func fixtureTestAdapter(t *testing.T, url string) *modelopenai.Adapter {
	t.Helper()
	adapter, err := modelopenai.New(modelopenai.Config{APIKey: localModelFixtureKey, ModelProfile: "p", Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt-test", Version: "gpt-test-2026-09"}, BaseURL: url, PreflightInputTokens: true})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func fixtureTestRequest(output agentmodel.OutputConstraint, messages ...agentmodel.ModelMessage) agentmodel.ModelRequest {
	features := []agentmodel.ModelFeature{}
	if output.Mode != agentmodel.OutputText {
		features = append(features, agentmodel.FeatureStructuredJSON)
	}
	return agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: "t", ModelProfile: "p", Messages: messages, Output: output,
		Deadline: time.Now().Add(time.Minute), Limits: agentmodel.ModelLimits{MaxInputTokens: 100000, MaxOutputTokens: 500}, TraceID: "trace-3834",
		Processing: agentmodel.ProcessingPolicy{Residency: "us", Retention: "zero", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}, RequiredFeatures: features}
}

// The real adapter accepts the fixture's answers for every pinned contract the
// Chat features use, so the governed path above it is exercised unchanged.
func TestIssue3834_FixtureProviderAnswersEveryChatTask(t *testing.T) {
	server := httptest.NewServer(LocalModelFixtureHandler())
	defer server.Close()
	adapter := fixtureTestAdapter(t, server.URL)
	ctx := context.Background()

	persona, err := adapter.Invoke(ctx, fixtureTestRequest(agentmodel.OutputConstraint{Mode: agentmodel.OutputSchema, Schema: json.RawMessage(PersonaChatReplyJSONSchema)},
		agentmodel.ModelMessage{Role: agentmodel.RoleUser, Content: "@Casey what is the leave policy? https://evil.example"}))
	if err != nil {
		t.Fatalf("persona: %v", err)
	}
	var reply string
	if json.Unmarshal(persona.Structured, &reply) != nil || !strings.HasPrefix(reply, LocalModelFixtureLabel) {
		t.Fatalf("persona answer is not a labelled fixture reply: %q", persona.Structured)
	}
	if strings.Contains(reply, "@Casey") || strings.Contains(reply, "evil.example") {
		t.Fatalf("persona fixture echoed the question: %q", reply)
	}
	again, _ := adapter.Invoke(ctx, fixtureTestRequest(agentmodel.OutputConstraint{Mode: agentmodel.OutputSchema, Schema: json.RawMessage(PersonaChatReplyJSONSchema)},
		agentmodel.ModelMessage{Role: agentmodel.RoleUser, Content: "@Casey what is the leave policy? https://evil.example"}))
	if string(again.Structured) != string(persona.Structured) {
		t.Fatal("the same question did not get the same answer")
	}

	translationSchema, err := agentmodel.ChatlangTranslationSchema()
	if err != nil {
		t.Fatal(err)
	}
	translated, err := adapter.Invoke(ctx, fixtureTestRequest(agentmodel.OutputConstraint{Mode: agentmodel.OutputSchema, Schema: translationSchema},
		agentmodel.ModelMessage{Role: agentmodel.RoleSystem, Content: "translate"},
		agentmodel.ModelMessage{Role: agentmodel.RoleUser, Content: "<untrusted_data>\n{\"source\":\"en\",\"target\":\"de-DE\",\"formality\":\"\",\"context\":[],\"text\":\"Hello ⟦HCM:ab12:0⟧\"}\n</untrusted_data>"}))
	if err != nil {
		t.Fatalf("translation: %v", err)
	}
	decoded, err := agentmodel.DecodeChatlangTranslation(translated)
	if err != nil || decoded.Text != "[de-DE] Hello ⟦HCM:ab12:0⟧" || decoded.SourceLanguage != "en" || !decoded.Usable() {
		t.Fatalf("translation %+v %v", decoded, err)
	}

	rewriteSchema, err := agentmodel.ChattoneRewriteSchema()
	if err != nil {
		t.Fatal(err)
	}
	rewritten, err := adapter.Invoke(ctx, fixtureTestRequest(agentmodel.OutputConstraint{Mode: agentmodel.OutputSchema, Schema: rewriteSchema},
		agentmodel.ModelMessage{Role: agentmodel.RoleSystem, Content: "rewrite"},
		agentmodel.ModelMessage{Role: agentmodel.RoleUser, Content: "<untrusted_data>\n{\"draft\":\"send me the report ⟦HCM:ab12:0⟧\",\"register_context\":[]}\n</untrusted_data>"}))
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	rewrite, err := agentmodel.DecodeChattoneRewrite(rewritten)
	if err != nil || rewrite.Text != "Send me the report ⟦HCM:ab12:0⟧" || !rewrite.Usable() {
		t.Fatalf("rewrite %+v %v", rewrite, err)
	}

	screen := func(agent, body string) AgentUXAmbientProposal {
		t.Helper()
		messages, err := AgentUXAmbientModelMessages(agent, AgentUXAmbientModelInput{Untrusted: true, SourceKind: "CHAT_MESSAGE", Message: AgentUXAmbientMessage{ID: "m1", Tenant: "t", Conversation: "c", Author: "a", Body: body, Revision: 1, Zone: "UTC"}})
		if err != nil {
			t.Fatal(err)
		}
		result, err := adapter.Invoke(ctx, fixtureTestRequest(agentmodel.OutputConstraint{Mode: agentmodel.OutputJSON}, messages...))
		if err != nil {
			t.Fatalf("ambient: %v", err)
		}
		var p struct {
			Kind, Title, Owner, Date, Clock, Event string
			Explicit                               bool
		}
		if json.Unmarshal(result.Structured, &p) != nil {
			t.Fatalf("ambient answer %s", result.Structured)
		}
		return AgentUXAmbientProposal(p)
	}
	if p := screen("task-catcher", "I will send the budget to finance by Friday"); p.Kind != "TASK" || p.Title != "send the budget to finance" {
		t.Fatalf("task proposal %+v", p)
	}
	if p := screen("reminder", "Remind me tomorrow at 3pm to call the vendor"); p.Kind != "REMINDER" || !p.Explicit || p.Date != "tomorrow" || p.Clock != "15:00" {
		t.Fatalf("reminder proposal %+v", p)
	}
}

func TestIssue3834_FixtureProviderRefusesStrangers_Security(t *testing.T) {
	server := httptest.NewServer(LocalModelFixtureHandler())
	defer server.Close()
	for _, path := range []string{"/responses", "/responses/input_tokens"} {
		request, _ := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(`{"model":"m","input":[]}`))
		request.Header.Set("Authorization", "Bearer someone-elses-key")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s answered a caller without the fixture credential: %d", path, response.StatusCode)
		}
	}
	response, err := http.Get(server.URL + "/responses")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET answered: %d", response.StatusCode)
	}
}

func TestIssue3834_FixtureProviderIsLocalDevelopmentOnly_Security(t *testing.T) {
	env := func(key string) string {
		if key == EnvLocalModelProvider {
			return "fixture"
		}
		return ""
	}
	for _, profile := range []string{"production", "staging", ""} {
		if _, _, err := localModelFixtureEndpoint(ServeConfig{Profile: profile}, env); err == nil {
			t.Fatalf("profile %q was given the fixture provider", profile)
		}
		if _, _, _, _, err := loadServedAgentModelConfiguration(context.Background(), ServeConfig{Profile: profile, AgentModelConfigFile: "unused.json"}, env); err == nil {
			t.Fatalf("profile %q loaded a deployment under the fixture switch", profile)
		}
	}
	if url, key, err := localModelFixtureEndpoint(ServeConfig{Profile: "production"}, func(string) string { return "" }); err != nil || url != "" || key != "" {
		t.Fatalf("an absent switch must be a no-op: %q %q %v", url, key, err)
	}
	if localModelAPIKeyMissing(func(string) string { return "" }) != true || localModelAPIKeyMissing(env) {
		t.Fatal("the missing-key check does not follow the fixture switch")
	}
}

// With the switch on, a prepared local deployment is served without any
// MODEL_API_KEY and its provider points at the loopback fixture.
func TestIssue3834_LocalDeploymentNeedsNoKeyUnderFixture_Integration(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := ServeConfig{Profile: ServeProfileLocalDev, CellID: "actual-serving-cell"}
	writeServedProviderTestDeployment(t, filepath.FromSlash(localPersonaModelDeploymentPath))
	env := func(key string) string {
		if key == EnvLocalModelProvider {
			return "fixture"
		}
		return ""
	}
	if _, _, _, _, err := loadServedAgentModelConfiguration(context.Background(), cfg, func(string) string { return "" }); err == nil {
		t.Fatal("without the switch and without a key the deployment must still be refused")
	}
	_, deployment, apiKey, configured, err := loadServedAgentModelConfiguration(context.Background(), cfg, env)
	if err != nil || !configured {
		t.Fatalf("fixture deployment not loaded: %v", err)
	}
	if apiKey != localModelFixtureKey || !strings.HasPrefix(deployment.BaseURL, "http://127.0.0.1:") {
		t.Fatalf("deployment not pointed at the loopback fixture: key=%q base=%q", apiKey, deployment.BaseURL)
	}
	factory, err := newPersonaServedProviderConfiguration(context.Background(), cfg, servedProviderTestRuntime(t), env)
	if err != nil || factory == nil {
		t.Fatalf("persona provider not composed under the fixture: %v", err)
	}
}

// The fixture sits beneath the real governed gateway: a translation sent through
// the structured engine still leases, prices, charges the ledger and writes the
// audit entry, and the request carries the fixture credential, not a real key.
func TestIssue3834_FixtureProviderRunsBeneathTheGovernedGateway_Integration(t *testing.T) {
	server := httptest.NewServer(LocalModelFixtureHandler())
	defer server.Close()
	base := modelDeploymentFixture(t)
	base.BaseURL = server.URL
	base.Credential.Scopes = []OpenAIModelCredentialScope{{TenantID: "tenant-a", Region: "us", Purpose: "persona.reply", Destination: base.Profiles[0].ID}}
	dep, err := NewChatlangStructuredDeployment(base, chatlangEvaluation(), 20*time.Second, 20000)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := agentbudget.New(ChatlangBudgetPolicy())
	if err != nil {
		t.Fatal(err)
	}
	spy := &chatlangAuditSpy{Store: agentaudit.NewMemoryStore()}
	evidence := &ChatlangModelEvidence{Audit: spy, Now: time.Now}
	gateway, leases, err := chatlangOpenAIModel(dep, localModelFixtureKey, PersonaModelDeploymentDependencies{Budget: agentmodel.BudgetAdapter{Ledger: ledger}, Routes: evidence, Sources: evidence, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewChatlangStructuredBinding(dep, leases, ledger, time.Now, chatlangFixtureModel)
	if err != nil {
		t.Fatal(err)
	}
	engine := ChatlangStructuredEngine{Gateway: gateway, Binding: binding}
	response, err := engine.Translate(context.Background(), chatlang.Request{Tenant: "tenant-a", Message: "m", Revision: 1, Source: "und", Target: "de", Text: "Please review ⟦HCM:abcd1234:0⟧ today."})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "[de] Please review ⟦HCM:abcd1234:0⟧ today." || !response.MeaningPreserved || response.DetectedSource != "en" {
		t.Fatalf("translate %+v", response)
	}
	var used agentbudget.Usage
	taskID := ChatlangBudgetTaskID("tenant-a", time.Now())
	for _, task := range ledger.Snapshot().Tasks {
		if task.ID == taskID {
			used = task.Used
		}
	}
	if used.Steps != 1 || used.Tokens == 0 {
		t.Fatalf("the fixture call was not charged to the ledger: %+v", used)
	}
	spy.mu.Lock()
	entries := append([]agentaudit.Entry(nil), spy.entries...)
	spy.mu.Unlock()
	if len(entries) != 1 || entries[0].Action != "model.route" || entries[0].TenantID != "tenant-a" {
		t.Fatalf("the fixture call was not audited: %+v", entries)
	}
	// A wrong credential never gets an answer from the same provider.
	wrong, _, err := chatlangOpenAIModel(dep, "not-the-fixture-key", PersonaModelDeploymentDependencies{Budget: agentmodel.BudgetAdapter{Ledger: ledger}, Routes: evidence, Sources: evidence, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (ChatlangStructuredEngine{Gateway: wrong, Binding: binding}).Translate(context.Background(), chatlang.Request{Tenant: "tenant-a", Message: "m2", Revision: 1, Source: "en", Target: "de", Text: "Hello"}); err == nil {
		t.Fatal("a caller without the fixture credential was answered")
	}
}
