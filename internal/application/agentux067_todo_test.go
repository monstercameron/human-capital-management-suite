package application

import "testing"

// TestTodo_AGENTUX_067 covers Task Catcher: the starter, the decision of where a
// noticed task belongs (the person's own list, the channel's list, or a person
// asked), the card that is offered and never added without a person pressing Add
// unless the administrator turned on automatic channel tasks, and duplicates.
func TestTodo_AGENTUX_067(t *testing.T) {
	s15Run(t,
		s15Proof{"starter manifest", TestAgentUXAmbient_StarterManifest_Golden},
		s15Proof{"tasks are offered and added on a press", TestAgentUXAmbient_Tasks_Integration},
		s15Proof{"named tasks and automatic channel tasks", TestAgentUXAmbient_AutomaticAndNamedTasks_Security_Integration},
		s15Proof{"source changes and duplicates", TestAgentUXAmbient_SourceChangesAndDuplicates_Integration},
	)
}

// TestTodo_AGENTUX_067_Security: a task is never created for or shown to someone
// outside the conversation, a private card never reaches anyone but its person,
// and message text cannot instruct the agent.
func TestTodo_AGENTUX_067_Security(t *testing.T) {
	s15Run(t,
		s15Proof{"audience", TestAgentUXAmbient_Audience_Security},
		s15Proof{"model quarantine", TestAgentUXAmbient_ModelQuarantine_Security},
		s15Proof{"parent and source visibility", TestAgentUXAmbient_ParentAndSourceVisibility_Security_Integration},
		s15Proof{"installation consent", TestAgentUXAmbient_InstallationConsent_Security_Integration},
		s15Proof{"grants and quarantine", TestAgentUXAmbient_GrantsAndQuarantine_Security_Integration},
		s15Proof{"revoked during the model call", TestAgentUXAmbient_RevokedDuringModel_Security_Integration},
		s15Proof{"surface", TestAgentUXAmbient_Surface_Security_Integration},
	)
}

// TestTodo_AGENTUX_067_Integration runs the cards, the outbox and the budget
// against the test database with a fake model.
func TestTodo_AGENTUX_067_Integration(t *testing.T) {
	s15Run(t,
		s15Proof{"tasks", TestAgentUXAmbient_Tasks_Integration},
		s15Proof{"outbox", TestAgentUXAmbient_Outbox_Integration},
		s15Proof{"budget and edits", TestAgentUXAmbient_BudgetAndEdits_Fault_Integration},
		s15Proof{"examples are prepared", TestAgentUXAmbient_PrepareExamples_Integration},
	)
}

// TestTodo_AGENTUX_067_Golden is the labelled suite of messages with their
// expected scope and owner; a private commitment is never offered publicly.
func TestTodo_AGENTUX_067_Golden(t *testing.T) {
	TestAgentUXAmbient_LabelledSuites_Golden(t)
}
