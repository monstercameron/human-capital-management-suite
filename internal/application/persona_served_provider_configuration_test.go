package application

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
)

func servedProviderTestRuntime(t *testing.T) *agentRuntime {
	t.Helper()
	budget, err := agentbudget.New(agentBudgetPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return &agentRuntime{Budget: budget, Audit: agentaudit.NewMemoryStore()}
}

func writeServedProviderTestDeployment(t *testing.T, path string) {
	t.Helper()
	raw, err := json.Marshal(modelDeploymentFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_AGENT_021_ServedProviderQualifiedLocalConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := ServeConfig{Profile: ServeProfileLocalDev, CellID: "actual-serving-cell"}
	called := false
	env := func(key string) string {
		called = true
		if key != "MODEL_API_KEY" {
			t.Fatalf("unexpected environment read %q", key)
		}
		return "test-only-provider-secret"
	}
	factory, err := newPersonaServedProviderConfiguration(context.Background(), cfg, nil, env)
	if err != nil || factory != nil || called {
		t.Fatalf("unqualified local deployment composed provider: %v", err)
	}
	writeServedProviderTestDeployment(t, filepath.FromSlash(localPersonaModelDeploymentPath))
	factory, err = newPersonaServedProviderConfiguration(context.Background(), cfg, servedProviderTestRuntime(t), env)
	if err != nil || factory == nil {
		t.Fatalf("qualified local provider not composed: %v", err)
	}
	first, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.FromSlash(localPersonaModelSigningPath))
	if err != nil {
		t.Fatal(err)
	}
	factory, err = newPersonaServedProviderConfiguration(context.Background(), cfg, servedProviderTestRuntime(t), env)
	if err != nil || factory == nil {
		t.Fatalf("provider restart failed: %v", err)
	}
	second, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.FromSlash(localPersonaModelSigningPath))
	if err != nil || first != second {
		t.Fatalf("provider restart rotated private authority: %v", err)
	}
}

func TestTodo_AGENT_021_ServedProviderExplicitConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qualified.json")
	writeServedProviderTestDeployment(t, path)
	material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.Join(t.TempDir(), "signing.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := ServeConfig{AgentModelConfigFile: path, CellID: "test-cell", PersonaOutputSigningSeed: material.OutputSeed, PersonaWorkloadSigningSeed: material.WorkloadSeed}
	factory, err := newPersonaServedProviderConfiguration(context.Background(), cfg, servedProviderTestRuntime(t), func(string) string { return "test-only-provider-secret" })
	if err != nil || factory == nil {
		t.Fatalf("explicit deployment failed: %v", err)
	}
	cfg.CellID = "different-cell"
	if _, err := newPersonaServedProviderConfiguration(context.Background(), cfg, servedProviderTestRuntime(t), func(string) string { return "test-only-provider-secret" }); !errors.Is(err, ErrPersonaModelConfiguration) {
		t.Fatalf("wrong cell accepted: %v", err)
	}
	cfg.CellID = "test-cell"
	cfg.PersonaOutputSigningSeed = ""
	if _, err := newPersonaServedProviderConfiguration(context.Background(), cfg, servedProviderTestRuntime(t), func(string) string { return "test-only-provider-secret" }); !errors.Is(err, ErrPersonaModelConfiguration) {
		t.Fatalf("missing configured seed accepted: %v", err)
	}
}
