package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

// chatlangOpenAIStack is the structured "openai" engine over the same fake
// provider as the governed text route: an httptest stand-in for the Responses
// API, so no network, no key and no cost.
type chatlangOpenAIStack struct {
	provider *chatlangProvider
	engine   ChatlangStructuredEngine
	binding  *ChatlangDeploymentBinding
	ledger   *agentbudget.Ledger
	audit    *chatlangAuditSpy
	dep      PersonaModelDeployment
}

// chatlangFixtureModel is the model name the shared deployment fixture
// qualifies.
const chatlangFixtureModel = "test-model"

func newChatlangOpenAIStack(t *testing.T, tenant, model string) chatlangOpenAIStack {
	t.Helper()
	provider := newChatlangProvider(t)
	base := modelDeploymentFixture(t)
	base.BaseURL = provider.server.URL
	base.Credential.Scopes = []OpenAIModelCredentialScope{{TenantID: tenant, Region: "us", Purpose: "persona.reply", Destination: base.Profiles[0].ID}}
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
	gateway, leases, err := chatlangOpenAIModel(dep, "test-only-provider-key", PersonaModelDeploymentDependencies{Budget: agentmodel.BudgetAdapter{Ledger: ledger}, Routes: evidence, Sources: evidence, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewChatlangStructuredBinding(dep, leases, ledger, time.Now, model)
	if err != nil {
		t.Fatal(err)
	}
	return chatlangOpenAIStack{provider: provider, engine: ChatlangStructuredEngine{Gateway: gateway, Binding: binding}, binding: binding, ledger: ledger, audit: spy, dep: dep}
}

// TestTodo_CHATLANG_003_OpenAI: the "openai" engine behind the same port. One
// call detects the language and translates; the call goes through the same
// gateway (lease, egress, pricing, budget, audit) as every governed model call;
// what leaves is the typed schema request, with no temperature and no prompt
// cache key; tokens and cost come back priced from the provider's own usage.
func TestTodo_CHATLANG_003_OpenAI(t *testing.T) {
	stack := newChatlangOpenAIStack(t, "tenant-a", chatlangFixtureModel)
	ctx := context.Background()
	request := chatlang.Request{Tenant: "tenant-a", Message: "m", Revision: 1, Source: "en", Target: "de", Text: "Please review ⟦HCM:abcd1234:0⟧ today.", Context: []string{"Earlier message"}}
	if !stack.engine.Ready() || !stack.engine.Detects() {
		t.Fatal("a bound structured engine is not ready or does not detect")
	}
	response, err := stack.engine.Translate(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "[de] Please review ⟦HCM:abcd1234:0⟧ today." || response.DetectedSource != "en" || !response.MeaningChecked || !response.MeaningPreserved {
		t.Fatalf("translate %+v", response)
	}
	if response.CostMicros != 120 || response.InputTokens != 20 || response.OutputTokens != 10 || response.Provider != "openai" || response.Model == "" || response.InstructionDigest != chatlang.StructuredInstructionDigest() {
		t.Fatalf("usage %+v", response)
	}
	instruction, data := chatlang.StructuredPrompt(request)
	sent := stack.provider.last()
	if stack.provider.calls() != 1 || sent.Authorization != "Bearer test-only-provider-key" || sent.Store || sent.System != instruction || sent.User != data || !sent.Structured {
		t.Fatalf("what left the deployment: calls %d %+v", stack.provider.calls(), sent)
	}
	for _, banned := range []string{"temperature", "prompt_cache_key", "top_p", "tools"} {
		if _, present := sent.Raw[banned]; present {
			t.Errorf("the request carries %q", banned)
		}
	}
	if string(sent.Raw["model"]) != `"`+chatlangFixtureModel+`"` {
		t.Errorf("model = %s", sent.Raw["model"])
	}
	var format struct {
		Format struct {
			Strict bool            `json:"strict"`
			Schema json.RawMessage `json:"schema"`
		} `json:"format"`
	}
	schema, _ := agentmodel.ChatlangTranslationSchema()
	if err := json.Unmarshal(sent.Raw["text"], &format); err != nil || !format.Format.Strict {
		t.Fatalf("output format %s %v", sent.Raw["text"], err)
	}
	var left, right any
	_ = json.Unmarshal(schema, &left)
	_ = json.Unmarshal(format.Format.Schema, &right)
	if l, r := mustJSON(left), mustJSON(right); l != r {
		t.Fatalf("the schema on the wire is not the SchemaFlux-derived one:\n%s\n%s", l, r)
	}
	// The same ledger and audit chain as every governed call.
	taskID := ChatlangBudgetTaskID("tenant-a", time.Now())
	var used agentbudget.Usage
	for _, task := range stack.ledger.Snapshot().Tasks {
		if task.ID == taskID {
			used = task.Used
		}
	}
	if used.Steps != 1 || used.Tokens != 30 || used.SpendMicros != 120 {
		t.Fatalf("budget %+v", used)
	}
	stack.audit.mu.Lock()
	entries := append([]agentaudit.Entry(nil), stack.audit.entries...)
	stack.audit.mu.Unlock()
	if len(entries) != 1 || entries[0].Action != "model.route" || entries[0].TenantID != "tenant-a" {
		t.Fatalf("audit %+v", entries)
	}
	for _, f := range entries[0].Fields {
		if strings.Contains(f.Value, "review") || strings.Contains(f.Value, "Earlier message") {
			t.Fatal("the route record carries message text")
		}
	}

	t.Run("a language that is not recorded is detected in the same call", func(t *testing.T) {
		unknown := request
		unknown.Source = ""
		stack.provider.mu.Lock()
		stack.provider.detected = "fr"
		stack.provider.mu.Unlock()
		defer func() {
			stack.provider.mu.Lock()
			stack.provider.detected = ""
			stack.provider.mu.Unlock()
		}()
		got, err := stack.engine.Translate(ctx, unknown)
		if err != nil || got.DetectedSource != "fr" || !strings.Contains(stack.provider.last().User, `"source":"und"`) {
			t.Fatalf("%+v %v user=%s", got, err, stack.provider.last().User)
		}
	})
	t.Run("a language the product does not offer is no language", func(t *testing.T) {
		stack.provider.mu.Lock()
		stack.provider.detected = "sw"
		stack.provider.mu.Unlock()
		defer func() {
			stack.provider.mu.Lock()
			stack.provider.detected = ""
			stack.provider.mu.Unlock()
		}()
		if got, err := stack.engine.Translate(ctx, request); err != nil || got.DetectedSource != "und" {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("a provider failure is an error and is not retried inside the call", func(t *testing.T) {
		stack.provider.mu.Lock()
		stack.provider.status = http.StatusServiceUnavailable
		stack.provider.mu.Unlock()
		defer func() {
			stack.provider.mu.Lock()
			stack.provider.status = 0
			stack.provider.mu.Unlock()
		}()
		before := stack.provider.calls()
		if _, err := stack.engine.Translate(ctx, request); err == nil || errors.Is(err, chatlang.ErrBudget) || stack.provider.calls() != before+1 {
			t.Fatalf("provider outage: %v after %d calls", err, stack.provider.calls()-before)
		}
	})
	t.Run("a workspace the credential does not cover reaches nothing", func(t *testing.T) {
		before := stack.provider.calls()
		other := request
		other.Tenant = "tenant-b"
		if _, err := stack.engine.Translate(ctx, other); err == nil || stack.provider.calls() != before {
			t.Fatalf("an out-of-scope workspace was served: %v", err)
		}
	})
	t.Run("an engine that is not bound refuses", func(t *testing.T) {
		if _, err := (ChatlangStructuredEngine{}).Translate(ctx, request); !errors.Is(err, chatlang.ErrUnavailable) {
			t.Fatal(err)
		}
		if (ChatlangStructuredEngine{}).Ready() {
			t.Fatal("ready with nothing bound")
		}
	})
}

func mustJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// TestTodo_CHATLANG_003_OpenAI_Deployment: the structured route has its own
// qualification. A profile qualified for the text route is not eligible for it
// and the reverse; the model named is the model called, never a substitute; and
// a deployment document for it loads like any other.
func TestTodo_CHATLANG_003_OpenAI_Deployment(t *testing.T) {
	provider := newChatlangProvider(t)
	base := modelDeploymentFixture(t)
	base.BaseURL = provider.server.URL
	structured, err := NewChatlangStructuredDeployment(base, chatlangEvaluation(), 20*time.Second, 20000)
	if err != nil {
		t.Fatal(err)
	}
	text, err := NewChatlangDeployment(base, chatlangEvaluation(), 20*time.Second, 20000)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agentmodel.ChatlangTranslationSchemaDigest()
	if err != nil {
		t.Fatal(err)
	}
	if structured.Profiles[0].OutputSchemaDigest != digest || structured.Profiles[0].ID == text.Profiles[0].ID {
		t.Fatalf("the structured profile %q %q is not its own", structured.Profiles[0].ID, structured.Profiles[0].OutputSchemaDigest)
	}
	if _, err := chatlangQualify(structured); !errors.Is(err, errChatlangBinding) {
		t.Fatalf("a structured profile qualified for the text route: %v", err)
	}
	if _, err := chatlangQualifyFor(text, digest); !errors.Is(err, errChatlangBinding) {
		t.Fatalf("a text profile qualified for the structured route: %v", err)
	}
	if _, err := chatlangQualifyFor(structured, digest); err != nil {
		t.Fatalf("the structured profile does not qualify: %v", err)
	}
	raw, _ := json.Marshal(structured)
	if _, err := ParsePersonaModelDeployment(raw); err != nil {
		t.Fatalf("the structured approval document does not load: %v", err)
	}
	// The default model is gpt-6-luna and the fixture's model is not it: no silent
	// substitution either way.
	if agentmodel.ChatlangDefaultModel != "gpt-6-luna" || ChatlangModelFromEnv(nil) != "gpt-6-luna" || ChatlangModelFromEnv(func(string) string { return "" }) != "gpt-6-luna" {
		t.Fatal("the default model is not gpt-6-luna")
	}
	if got := ChatlangModelFromEnv(func(k string) string {
		if k == EnvChatTranslationModel {
			return " gpt-6-sol "
		}
		return ""
	}); got != "gpt-6-sol" {
		t.Fatalf("configured model = %q", got)
	}
	ledger, _ := agentbudget.New(ChatlangBudgetPolicy())
	if _, err := NewChatlangStructuredBinding(structured, nil, ledger, time.Now, chatlangFixtureModel); !errors.Is(err, errChatlangBinding) {
		t.Fatalf("a binding without leases: %v", err)
	}
	stack := newChatlangOpenAIStack(t, "tenant-a", chatlangFixtureModel)
	if _, err := NewChatlangStructuredBinding(stack.dep, stack.binding.Leases, stack.ledger, time.Now, ""); !errors.Is(err, errChatlangBinding) || !strings.Contains(err.Error(), "gpt-6-luna") {
		t.Fatalf("the default model was served by a profile that names another: %v", err)
	}
	if _, err := NewChatlangStructuredBinding(stack.dep, stack.binding.Leases, stack.ledger, time.Now, "gpt-6-sol"); !errors.Is(err, errChatlangBinding) {
		t.Fatalf("a configured model was served by a profile that names another: %v", err)
	}
	if _, err := NewChatlangStructuredBinding(text, stack.binding.Leases, stack.ledger, time.Now, chatlangFixtureModel); !errors.Is(err, errChatlangBinding) {
		t.Fatalf("a text deployment bound as structured: %v", err)
	}
}

// TestTodo_CHATLANG_003_OpenAI_Serve: the engine is chosen by name, only with a
// qualified document and the same key the agents use, and never silently.
func TestTodo_CHATLANG_003_OpenAI_Serve(t *testing.T) {
	rig := newChatlangRig(t)
	rig.governance.BindEngine(ChatlangEngineInfo{})
	env := func(values map[string]string) func(string) string { return func(k string) string { return values[k] } }
	local := ServeConfig{Profile: ServeProfileLocalDev}
	// Asked for by name with nothing provisioned: absent, with the reason that names the document.
	runtime, reason, err := composeServedChatlang(context.Background(), chatlangServeInput{Config: local, Chat: rig.chat, Env: env(map[string]string{EnvChatTranslationEngine: ChatlangEngineOpenAI}), Tenants: []string{"host"}})
	if err != nil || runtime != nil || !strings.Contains(reason, EnvChatTranslationModelConfigFile) || rig.governance.Engine().Ready {
		t.Fatalf("openai without a document: %v %q %+v", err, reason, rig.governance.Engine())
	}
}
