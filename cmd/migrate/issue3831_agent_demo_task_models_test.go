package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

func TestIssue3831_ReceiptNamesDeploymentsOrTheReason(t *testing.T) {
	got := formatAgentDemoChatTaskModels(application.LocalAgentDemoSummary{ChatTaskModels: []application.LocalChatTaskModelDocument{
		{Task: "chat translation", Path: "a.json", Written: true},
		{Task: "chat writing style", Path: "b.json"},
		{Task: "ambient screen", Path: "c.json"},
	}})
	for _, want := range []string{"chat translation a.json (written)", "chat writing style b.json (already current)", "ambient screen c.json (already current)", "nothing was measured"} {
		if !strings.Contains(got, want) {
			t.Fatalf("receipt %q lacks %q", got, want)
		}
	}
	none := formatAgentDemoChatTaskModels(application.LocalAgentDemoSummary{ChatTaskModelsNote: "not derived: add -suite-digest x"})
	if !strings.Contains(none, "-suite-digest") {
		t.Fatalf("receipt %q must name the flag", none)
	}
	if formatAgentDemoChatTaskModels(application.LocalAgentDemoSummary{}) != "" {
		t.Fatal("a run that never reached the step says nothing")
	}
}

func TestIssue3831_AgentDemoAcceptsSuiteDigestFlag(t *testing.T) {
	spec := agentDemoSpec(nil)
	found := false
	for _, f := range spec.ConfigFields {
		found = found || f.Name == fieldChatTaskSuiteDigest
	}
	if !found {
		t.Fatal("agent-demo must accept -suite-digest")
	}
}
