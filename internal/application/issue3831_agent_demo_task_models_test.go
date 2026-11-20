package application

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssue3831_ValidationRefusesMalformedDigest_Security(t *testing.T) {
	if err := validateLocalAgentDemoChatTaskDigest(""); err != nil {
		t.Fatalf("an empty digest is allowed (the three documents are then not derived): %v", err)
	}
	if err := validateLocalAgentDemoChatTaskDigest(issue3829Suite); err != nil {
		t.Fatalf("a well-formed digest was refused: %v", err)
	}
	for _, bad := range []string{"passed", "sha256:abc", "SHA256:" + strings.Repeat("a", 64)} {
		if err := validateLocalAgentDemoChatTaskDigest(bad); err == nil || !strings.Contains(err.Error(), "-suite-digest") {
			t.Fatalf("%q: err = %v, want a refusal naming -suite-digest", bad, err)
		}
	}
}

func TestIssue3831_DerivesThreeDeploymentsBesideBaseInOneRun_Integration(t *testing.T) {
	dir := issue3829Dir(t)
	var summary LocalAgentDemoSummary
	if err := summary.prepareChatTaskModels(LocalAgentDemoConfig{Profile: ServeProfileLocalDev, ChatTaskSuiteDigest: issue3829Suite}); err != nil {
		t.Fatal(err)
	}
	if len(summary.ChatTaskModels) != 3 || summary.ChatTaskModelsNote != "" {
		t.Fatalf("summary = %+v, want three documents and no note", summary)
	}
	if !summary.Changed() {
		t.Fatal("writing the three documents must count as a change")
	}
	for _, name := range []string{"chat-translation-deployment.json", "chat-writing-style-deployment.json", "ambient-screen-deployment.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	// A second run rewrites nothing and reports no change from them.
	var again LocalAgentDemoSummary
	if err := again.prepareChatTaskModels(LocalAgentDemoConfig{Profile: ServeProfileLocalDev, ChatTaskSuiteDigest: issue3829Suite}); err != nil {
		t.Fatal(err)
	}
	if len(again.ChatTaskModels) != 3 || again.Changed() {
		t.Fatalf("rerun = %+v, want three documents already current and no change", again)
	}
}

func TestIssue3831_NoDigestSaysWhatToDoAndWritesNothing(t *testing.T) {
	dir := issue3829Dir(t)
	var summary LocalAgentDemoSummary
	if err := summary.prepareChatTaskModels(LocalAgentDemoConfig{Profile: ServeProfileLocalDev}); err != nil {
		t.Fatal(err)
	}
	if len(summary.ChatTaskModels) != 0 || !strings.Contains(summary.ChatTaskModelsNote, "-suite-digest") {
		t.Fatalf("summary = %+v, want no documents and a note naming -suite-digest", summary)
	}
	if _, err := os.Stat(filepath.Join(dir, "chat-translation-deployment.json")); err == nil {
		t.Fatal("no document may be written without an owner-stated digest")
	}
}

func TestIssue3831_MissingBaseDeploymentIsANoteNotAFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvLocalAgentDevDir, dir)
	var summary LocalAgentDemoSummary
	if err := summary.prepareChatTaskModels(LocalAgentDemoConfig{Profile: ServeProfileLocalDev, ChatTaskSuiteDigest: issue3829Suite}); err != nil {
		t.Fatalf("a finished preparation must not fail at the last step: %v", err)
	}
	if len(summary.ChatTaskModels) != 0 || !strings.Contains(summary.ChatTaskModelsNote, "does not exist") {
		t.Fatalf("summary = %+v, want no documents and a note about the missing base deployment", summary)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("nothing may be written without a base deployment: %v", entries)
	}
}

func TestIssue3831_ConfigValidationRefusesMalformedDigestBeforeAnyConnection_Security(t *testing.T) {
	err := ValidateLocalAgentDemoConfig(LocalAgentDemoConfig{Profile: ServeProfileLocalDev, Tenant: localAgentDemoTenant, ChatTaskSuiteDigest: "passed"})
	if err == nil || !strings.Contains(err.Error(), "-suite-digest") {
		t.Fatalf("err = %v, want a refusal naming -suite-digest", err)
	}
}
