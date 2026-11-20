package application

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssue3832_LocalPathsAreAbsoluteAndOverridable(t *testing.T) {
	t.Setenv(EnvLocalAgentDevDir, "")
	t.Setenv(EnvLocalAgentModelSigningFile, "")
	if p := localAgentDevFile(localPersonaModelDeploymentPath); !filepath.IsAbs(p) || !strings.HasSuffix(p, "model-deployment.json") {
		t.Fatalf("default path %q is not absolute", p)
	}
	dir := t.TempDir()
	t.Setenv(EnvLocalAgentDevDir, dir)
	for _, c := range []string{localPersonaModelDeploymentPath, localChatlangModelDeploymentPath, localChattoneModelDeploymentPath, localAgentTaskModelDeploymentPath} {
		if got, want := localAgentDevFile(c), filepath.Join(dir, filepath.Base(c)); got != want {
			t.Fatalf("%s resolved to %q, want %q", c, got, want)
		}
	}
	if got, want := localPersonaSigningFile(), filepath.Join(dir, "model-signing.json"); got != want {
		t.Fatalf("signing %q want %q", got, want)
	}
	own := filepath.Join(t.TempDir(), "seeds.json")
	t.Setenv(EnvLocalAgentModelSigningFile, own)
	if got := localPersonaSigningFile(); got != own {
		t.Fatalf("signing override %q want %q", got, own)
	}
}

func TestIssue3832_MissingDeploymentLogNamesAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvLocalAgentDevDir, dir)
	reason := personaServedProviderUnavailableReason(ServeConfig{Profile: ServeProfileLocalDev}, func(string) string { return "" })
	if !strings.Contains(reason, filepath.Join(dir, "model-deployment.json")) {
		t.Fatalf("reason does not name the searched file: %q", reason)
	}
}

func TestIssue3832_DeploymentFlagKeepsLocalSigning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvLocalAgentDevDir, dir)
	deployment := filepath.Join(t.TempDir(), "deployment.json")
	writeServedProviderTestDeployment(t, deployment)
	cfg := ServeConfig{Profile: ServeProfileLocalDev, CellID: "actual-serving-cell", AgentModelConfigFile: deployment}
	env := func(string) string { return "test-only-provider-secret" }
	factory, err := newPersonaServedProviderConfiguration(context.Background(), cfg, servedProviderTestRuntime(t), env)
	if err != nil || factory == nil {
		t.Fatalf("explicit deployment with local signing not composed: %v", err)
	}
	// The factory must have created the seeds itself, at the overridden path.
	if _, err := os.Stat(filepath.Join(dir, "model-signing.json")); err != nil {
		t.Fatalf("local signing seeds were not loaded from the override directory: %v", err)
	}
}

func TestIssue3832_AmbientScreenDeploymentFollowsOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvLocalAgentDevDir, dir)
	if got, want := localAgentDevFile(localAgentUX079AmbientScreenDeploymentPath), filepath.Join(absLocalAgentPath(dir), "ambient-screen-deployment.json"); got != want {
		t.Fatalf("ambient screen deployment %q, want %q", got, want)
	}
	if got, want := localAgentDevRoot(), absLocalAgentPath(dir); got != want {
		t.Fatalf("support object root %q, want %q", got, want)
	}
}

func TestIssue3832_ProductionIgnoresLocalOverrides_Security(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvLocalAgentDevDir, dir)
	t.Setenv(EnvLocalAgentModelSigningFile, filepath.Join(dir, "seeds.json"))
	// A valid prepared deployment sits in the override directory; the standard
	// profile must still neither read it nor create signing seeds.
	writeServedProviderTestDeployment(t, filepath.Join(dir, "model-deployment.json"))
	cfg := ServeConfig{Profile: ServeProfileStandard}
	_, _, _, ok, err := loadServedAgentModelConfiguration(context.Background(), cfg, func(string) string { return "k" })
	if ok || err != nil {
		t.Fatalf("production composed from local override: ok=%v err=%v", ok, err)
	}
	for _, name := range []string{"model-signing.json", "seeds.json"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr == nil {
			t.Fatalf("production created local signing seeds %s", name)
		}
	}
}
