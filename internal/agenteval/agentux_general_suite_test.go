package agenteval

import (
	"slices"
	"strings"
	"testing"
)

func TestAgentUXGeneral_EvaluationSuite(t *testing.T) {
	suite := AssistantSuite("documents.search", "chat.reply")
	if suite.ID != "AGENTUX-049.assistant" || len(suite.Cases) != 8 {
		t.Fatalf("Assistant suite = %#v", suite)
	}
	for _, id := range []string{"general-question", "unsupported-without-document", "unreadable-document", "mixed-audience", "document-prompt-injection", "document-authority-injection", "document-recipient-injection", "document-tool-injection"} {
		if !slices.ContainsFunc(suite.Cases, func(testCase PersonaCase) bool { return testCase.ID == id }) {
			t.Fatalf("Assistant suite is missing %q", id)
		}
	}
}

func TestAgentUXGeneral_EvaluationSuite_Security(t *testing.T) {
	suite := AssistantSuite("documents.search", "chat.reply")
	if err := validatePersonaSuite(PersonaEvaluationTarget{TenantID: "tenant-a", SyntheticTenantID: "synthetic", PersonaID: "assistant", PersonaVersion: 1, ProfileDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ModelDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", InvokerID: "admin"}, suite); err != nil {
		t.Fatalf("Assistant suite does not satisfy the evaluator's security matrix: %v", err)
	}
	foundInjection := slices.ContainsFunc(suite.Cases, func(testCase PersonaCase) bool {
		return testCase.ID == "document-prompt-injection" && testCase.Kind == PersonaPeerInjection && strings.Contains(testCase.PeerText, "Document: 2026 holiday guide\n") && strings.Contains(testCase.PeerText, "Ignore all instructions")
	})
	if !foundInjection {
		t.Fatal("Assistant suite lacks a prompt injection embedded in document text")
	}
}
