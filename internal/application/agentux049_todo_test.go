package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
)

// TestTodo_AGENTUX_049 covers the general-purpose starter: Assistant is offered
// as a reviewed template for new agents (a draft that is not published and does
// not install itself), has its own eight-case evaluation suite, and the demo
// workspace places a holiday guide of ten observed days with verified weekdays.
func TestTodo_AGENTUX_049(t *testing.T) {
	starter, ok := agenttemplate.PersonaStarterFor(localAgentDemoAssistantStarterID, 1)
	if !ok || starter.DisplayName != "Assistant" || starter.Published || starter.AutoInstall || !starter.OwnerRequired {
		t.Fatalf("Assistant is not offered as a governed draft template: %+v", starter)
	}
	offered := false
	for _, listed := range agenttemplate.PersonaStarters() {
		offered = offered || listed.ID == localAgentDemoAssistantStarterID
	}
	if !offered {
		t.Fatal("New agent does not offer the Assistant template")
	}
	suite := agenteval.AssistantSuite("documents.search", "chat.reply")
	if len(suite.Cases) != 8 || suite.ID != "AGENTUX-049.assistant" {
		t.Fatalf("Assistant evaluation suite = %s with %d cases, want eight", suite.ID, len(suite.Cases))
	}
	s15Run(t,
		s15Proof{"instructions", TestAgentUXGeneral_AssistantInstructions},
		s15Proof{"holiday guide of ten days", TestAgentUXGeneral_HolidayGuideCalendar},
		s15Proof{"catalog and suspension reasons", TestAgentUXGeneral_SuspensionReasons},
		s15Proof{"policy helper's published versions keep verifying", TestAgentUXGeneral_PolicyRevision_Security},
		s15Proof{"deployment adds Assistant without changing Policy Helper", TestAgentUXGeneral_DeploymentAddsAssistantWithoutChangingPolicyProfile},
	)
	if rows := strings.Count(localAgentDemoHolidayMarkdown, "\n| "); rows != 12 {
		t.Fatalf("the holiday guide table has %d lines, want a header, a divider and ten days", rows)
	}
}

// TestTodo_AGENTUX_049_Integration prepares the agent against the test database:
// the Assistant is reviewed, evaluated, published and runnable, the idempotent
// preparation adds it beside Policy Helper, and the runtime composes locally.
func TestTodo_AGENTUX_049_Integration(t *testing.T) {
	s15Run(t,
		s15Proof{"assistant draft", TestAgentUXGeneral_AssistantDraft_Integration},
		s15Proof{"publish runnable", TestAgentUXGeneral_PublishRunnable_Integration},
		s15Proof{"policy helper upgrade", TestAgentUXGeneral_PolicyUpgrade_Integration},
		s15Proof{"publish requires a runtime", TestAgentUXGeneral_PublishRequiresRuntime},
		s15Proof{"provisioning failure is fail closed", TestAgentUXGeneral_PublishProvisioningFailureIsFailClosed},
		s15Proof{"local runtime composition", TestAgentUXGeneral_LocalRuntimeComposition},
		s15Proof{"verifier keys", TestAgentUXGeneral_VerifierKeys},
	)
}

// TestTodo_AGENTUX_049_Security: a prompt injection inside a document is one of
// the Assistant's evaluation cases, the evaluator's security matrix accepts the
// suite, and the runtime refuses another tenant's identity.
func TestTodo_AGENTUX_049_Security(t *testing.T) {
	suite := agenteval.AssistantSuite("documents.search", "chat.reply")
	injected := 0
	for _, testCase := range suite.Cases {
		if strings.HasPrefix(testCase.ID, "document-") && strings.Contains(testCase.ID, "injection") {
			injected++
		}
	}
	if injected != 4 {
		t.Fatalf("the Assistant suite has %d document injection cases, want four", injected)
	}
	s15Run(t,
		s15Proof{"runtime provisioner rejects another tenant", TestAgentUXGeneral_RuntimeProvisionerRejectsCrossTenant},
		s15Proof{"evaluation authority accepts both suites", TestAgentUXGeneral_EvaluationAuthorityAcceptsBothSuites},
		s15Proof{"current identity", TestAgentUXGeneral_CurrentIdentity_Security},
	)
}
