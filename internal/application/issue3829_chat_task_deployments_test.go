package application

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

const issue3829Suite = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func issue3829Dir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(EnvLocalAgentDevDir, dir)
	profile := localOpenAIProfileFixture(t)
	profile.ToolSchemaDigest = LocalPersonaOpenAIToolSchemaDigest()
	profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
	base, err := NewLocalPersonaOpenAIModelDeployment("harborcare-demo", []agentmodel.ModelProfile{profile}, localOpenAIMaterialFixture())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeLocalAgentDemoDeployment(filepath.Join(dir, "model-deployment.json"), base); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestIssue3829_WritesThreeLoadableDocumentsIdempotently_Integration(t *testing.T) {
	dir := issue3829Dir(t)
	cfg := LocalChatTaskModelsConfig{Profile: ServeProfileLocalDev, SuiteDigest: issue3829Suite}
	first, err := WriteLocalChatTaskModels(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 {
		t.Fatalf("documents = %d, want 3", len(first))
	}
	for _, doc := range first {
		if !doc.Written || filepath.Clean(filepath.Dir(doc.Path)) != filepath.Clean(dir) {
			t.Fatalf("first run %+v: want written beside the base in %s", doc, dir)
		}
		dep, err := LoadPersonaModelDeployment(doc.Path)
		if err != nil {
			t.Fatalf("%s does not load: %v", doc.Task, err)
		}
		if len(dep.Profiles) != 1 || dep.Profiles[0].Evaluation.SuiteDigest != issue3829Suite || !dep.Profiles[0].Evaluation.Passed {
			t.Fatalf("%s profile does not state the owner's evaluation: %+v", doc.Task, dep.Profiles)
		}
	}
	// The files are the ones a local-dev serve reads without any environment variable.
	for _, name := range []string{"chat-translation-deployment.json", "chat-writing-style-deployment.json", "ambient-screen-deployment.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	second, err := WriteLocalChatTaskModels(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range second {
		if doc.Written {
			t.Fatalf("rerun rewrote %s", doc.Task)
		}
	}
}

func TestIssue3829_RefusesOutsideLocalDevAndBadEvidence_Security(t *testing.T) {
	dir := issue3829Dir(t)
	for name, cfg := range map[string]LocalChatTaskModelsConfig{
		"production profile": {Profile: "production", SuiteDigest: issue3829Suite},
		"empty profile":      {SuiteDigest: issue3829Suite},
		"no digest":          {Profile: ServeProfileLocalDev},
		"invented digest":    {Profile: ServeProfileLocalDev, SuiteDigest: "passed"},
		"short digest":       {Profile: ServeProfileLocalDev, SuiteDigest: "sha256:abc"},
	} {
		if docs, err := WriteLocalChatTaskModels(cfg); err == nil || len(docs) != 0 {
			t.Fatalf("%s: want refusal, got %v %v", name, docs, err)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*-deployment.json"))
	for i := 0; i < len(matches); i++ {
		if filepath.Base(matches[i]) == "model-deployment.json" {
			matches = append(matches[:i], matches[i+1:]...)
			i--
		}
	}
	if len(matches) != 0 {
		t.Fatalf("a refused run wrote %v", matches)
	}
}

func TestIssue3829_MissingBaseNamesThePreparationCommand(t *testing.T) {
	t.Setenv(EnvLocalAgentDevDir, t.TempDir())
	_, err := WriteLocalChatTaskModels(LocalChatTaskModelsConfig{Profile: ServeProfileLocalDev, SuiteDigest: issue3829Suite})
	if err == nil || !strings.Contains(err.Error(), "migrate agent-demo") {
		t.Fatalf("err = %v, want a pointer to migrate agent-demo", err)
	}
}

func TestIssue3829_NoSecretsInDocuments_Security(t *testing.T) {
	dir := issue3829Dir(t)
	if _, err := WriteLocalChatTaskModels(LocalChatTaskModelsConfig{Profile: ServeProfileLocalDev, SuiteDigest: issue3829Suite}); err != nil {
		t.Fatal(err)
	}
	material := localOpenAIMaterialFixture()
	matches, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, seed := range []string{material.OutputSeed, material.WorkloadSeed, material.PolicySeed} {
		if seed == "" {
			continue
		}
		for _, m := range matches {
			raw, _ := os.ReadFile(m)
			if strings.Contains(string(raw), seed) {
				t.Fatalf("%s carries a signing seed", m)
			}
		}
	}
}

func TestIssue3829_HintNamesTheCommandOnlyLocally(t *testing.T) {
	if got := localChatTaskModelsHint(true); !strings.Contains(got, "migrate chat-task-models") {
		t.Fatalf("local hint = %q", got)
	}
	if got := localChatTaskModelsHint(false); got != "" {
		t.Fatalf("non-local hint = %q, want none", got)
	}
}

// A document that only loads is not enough: the serve path binds each one to
// its task and refuses a profile whose evidence or semantics differ. The three
// written files must be accepted by those same bindings, and must be refused
// when the owner's evaluation is not what the task expects.
func TestIssue3829_WrittenDocumentsQualifyTheirTasks_Integration(t *testing.T) {
	issue3829Dir(t)
	docs, err := WriteLocalChatTaskModels(LocalChatTaskModelsConfig{Profile: ServeProfileLocalDev, SuiteDigest: issue3829Suite})
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := agentbudget.New(agentBudgetPolicy())
	if err != nil {
		t.Fatal(err)
	}
	byTask := map[string]PersonaModelDeployment{}
	for _, doc := range docs {
		dep, err := LoadPersonaModelDeployment(doc.Path)
		if err != nil {
			t.Fatal(err)
		}
		byTask[doc.Task] = dep
	}
	leases := &ModelLeaseSource{}
	if _, err := NewChatlangDeploymentBinding(byTask["chat translation"], leases, ledger, time.Now); err != nil {
		t.Fatalf("translation document is not accepted by the serve binding: %v", err)
	}
	if _, err := NewChattoneDeploymentBinding(byTask["chat writing style"], leases, ledger, time.Now); err != nil {
		t.Fatalf("writing-style document is not accepted by the serve binding: %v", err)
	}
	if _, err := NewAgentUX079AmbientScreenBinding(byTask["ambient screen"], leases, ledger, time.Now); err != nil {
		t.Fatalf("ambient-screen document is not accepted by the serve binding: %v", err)
	}
	// Each document qualifies only its own task.
	if _, err := NewChattoneDeploymentBinding(byTask["chat translation"], leases, ledger, time.Now); err == nil {
		t.Fatal("the translation document must not qualify the writing-style task")
	}
}
