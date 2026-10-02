package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/schemaflux"
	"github.com/monstercameron/schemaflux/schemafluxtest"
)

func TestTodo_AGENT_017_ServedTaskOpenAIComposition(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	var schema json.RawMessage
	fake := schemafluxtest.New().ReplyFunc(func(_ int, req schemaflux.CompletionRequest) (string, error) {
		schema, _ = json.Marshal(req.JSONSchema)
		return `{"text":"schema capture","citations":[]}`, nil
	})
	if _, err := schemaflux.Generating[agentsystem.ModelOutput]("schema capture").Strict().RunResult(schemaflux.NewClient("").WithProviderInstance(fake).Context(context.Background())); err != nil {
		t.Fatal(err)
	}
	caps, _, err := newAgentCapabilities(ownWorkerReader{}, f.cell.Evidence, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := newAgentSkills(caps)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := skills.Pin(agentskillsKey(agentSummarizeSkillID))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/responses/input_tokens" {
			io.WriteString(w, `{"object":"response.input_tokens","input_tokens":20}`)
			return
		}
		calls.Add(1)
		var body struct {
			Store bool `json:"store"`
			Text  struct {
				Format struct {
					Strict bool            `json:"strict"`
					Schema json.RawMessage `json:"schema"`
				} `json:"format"`
			} `json:"text"`
		}
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer test-only-provider-key" || json.NewDecoder(r.Body).Decode(&body) != nil || body.Store || !body.Text.Format.Strict || !sameTaskModelTestSchema(schema, body.Text.Format.Schema) {
			t.Error("served task lost its provider credential or strict Go schema")
		}
		io.WriteString(w, `{"id":"response-task","model":"test-model","status":"completed","usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30},"output":[{"type":"message","content":[{"type":"output_text","text":"{\"text\":\"The private task reached OpenAI.\",\"citations\":[]}"}]}]}`)
	}))
	defer server.Close()
	deployment := modelDeploymentFixture(t)
	deployment.BaseURL = server.URL
	deployment.PublicTaskRetentionSeconds = 30 * 24 * 60 * 60
	profile := &deployment.Profiles[0]
	profile.TaskProfileIDs = []string{AgentTaskModelSkillProfileID(pin, schema)}
	profile.OutputSchemaDigest = taskModelDigest(schema)
	profile.Evaluation.AgentVersionDigest = agentVersion
	profile.MaxLatency = time.Minute
	profile.MaxCostMicros = 100_000
	profile.ProfileDigest = agentmodel.ModelProfileDigest(*profile)
	deployment.Terms[0].Retention = agentegress.RetentionPolicy{Mode: agentegress.RetentionBounded, MaxAge: 30 * 24 * time.Hour}
	deployment.Terms[0].SourceRules = []agentegress.ProviderSourceRule{{Class: AgentTaskApprovedInputSource, Classes: deployment.Terms[0].AllowedClasses}}
	deployment.Destinations[0].Purposes = []string{agentPurpose}
	deployment.Credential.Scopes[0].Purpose = agentPurpose
	deployment.Credential.Scopes[0].TenantID = f.tenant
	material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.Join(t.TempDir(), "signing.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "deployment.json")
	raw, _ := json.Marshal(deployment)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := ServeConfig{Profile: ServeProfileLocalDev, AgentModelConfigFile: path, CellID: deployment.Worker.Cell, PersonaOutputSigningSeed: material.OutputSeed, PersonaWorkloadSigningSeed: material.WorkloadSeed}
	runtime, err := composeAgentRuntime(context.Background(), agentRuntimeInput{Pool: f.pool, Cell: f.cell, Config: cfg, Env: func(string) string { return "" }, Now: time.Now, Tenants: []string{f.tenant}})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Model.Available() {
		t.Fatal("unbound task model was available")
	}
	if err := composeServedAgentTaskModel(context.Background(), cfg, runtime, nil, func(string) string { return "test-only-provider-key" }, time.Now); err != nil {
		t.Fatal(err)
	}
	if !runtime.Model.Available() || calls.Load() != 0 {
		t.Fatal("composition dispatched a provider call or failed to bind")
	}
	started, err := runtime.Starter.StartTask(context.Background(), agentTestPrincipal(t, f.tenant, agentTestWorker), "Say hello in one sentence.")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := runtime.Platform.ForTenant(context.Background(), values.TenantId(f.tenant))
	if err != nil {
		t.Fatal(err)
	}
	task, err := runner.Runtime.GetTask(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.State != string(agentrun.StateCompleted) || calls.Load() != 1 {
		t.Fatalf("served task state=%s failure=%s detail=%s calls=%d", task.State, task.FailureCode, task.FailureDetail, calls.Load())
	}
	if !strings.Contains(task.Ledger.AnswerText, "private task reached OpenAI") {
		t.Fatalf("answer not persisted: %+v", task)
	}
	if err := composeServedAgentTaskModel(context.Background(), cfg, runtime, nil, func(string) string { return "test-only-provider-key" }, time.Now); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatalf("rebound model: %v", err)
	}
}

func sameTaskModelTestSchema(left, right json.RawMessage) bool {
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestTodo_AGENT_017_ServedTaskMissingQualification(t *testing.T) {
	if err := composeServedAgentTaskModel(nil, ServeConfig{}, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	runtime := &agentRuntime{TypedModels: &AgentTypedModelDispatchBinding{}}
	if err := composeServedAgentTaskModel(context.Background(), ServeConfig{Profile: ServeProfileLocalDev}, runtime, nil, nil, nil); err != nil || runtime.TypedModels.Available() {
		t.Fatalf("missing qualification enabled task model: %v", err)
	}
	writeServedProviderTestDeployment(t, filepath.FromSlash(localAgentTaskModelDeploymentPath))
	if err := composeServedAgentTaskModel(context.Background(), ServeConfig{Profile: ServeProfileLocalDev}, runtime, nil, func(string) string { return "test-only-key" }, nil); err != nil || runtime.TypedModels.Available() {
		t.Fatalf("persona qualification enabled task model: %v", err)
	}
}
