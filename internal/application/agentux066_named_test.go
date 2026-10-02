package application

import "testing"

// AGENTUX-066 was built as the ambient reading service (agentux_ambient_*) and
// proved under the names TestAgentUXAmbient_*. The tests below are the todo's
// own names; each runs the proofs of its matrix row, so a failure of any of
// them fails the row. The clause each proof covers:
//
//   - default off, per conversation, administrator only, never a direct
//     conversation of two people: GrantsAndQuarantine_Security_Integration
//   - one candidate per human message through the chat outbox, no agent loops:
//     Outbox_Integration, GrantsAndQuarantine_Security_Integration
//   - the in-process screen: Audience_Security, LabelledSuites_Golden,
//     Screen_Performance
//   - the model sees one message and its thread parent as quarantined data:
//     ModelQuarantine_Security, GrantsAndQuarantine_Security_Integration
//   - budgets and "paused": BudgetAndEdits_Fault_Integration
//   - an edit is re-read once and a deletion cancels what it caused:
//     BudgetAndEdits_Fault_Integration, ReminderConsentAndSource_Integration,
//     SourceChangesAndDuplicates_Integration
//   - every read in the run record: BudgetAndEdits_Fault_Integration
//   - a member who opted out, a second tenant, a conversation without the
//     installation: GrantsAndQuarantine_Security_Integration
func runAgentUX066(t *testing.T, proofs map[string]func(*testing.T)) {
	t.Helper()
	for name, proof := range proofs {
		t.Run(name, proof)
	}
}

func TestTodo_AGENTUX_066(t *testing.T) {
	runAgentUX066(t, map[string]func(*testing.T){
		"tasks_private_and_public":  TestAgentUXAmbient_Tasks_Integration,
		"reminder_consent_source":   TestAgentUXAmbient_ReminderConsentAndSource_Integration,
		"edits_deletes_duplicates":  TestAgentUXAmbient_SourceChangesAndDuplicates_Integration,
		"budget_paused_and_journal": TestAgentUXAmbient_BudgetAndEdits_Fault_Integration,
		"screen_golden":             TestAgentUXAmbient_LabelledSuites_Golden,
	})
}

func TestTodo_AGENTUX_066_Security(t *testing.T) {
	runAgentUX066(t, map[string]func(*testing.T){
		"grants_optout_tenant_injection_loop": TestAgentUXAmbient_GrantsAndQuarantine_Security_Integration,
		"parent_and_source_visibility":        TestAgentUXAmbient_ParentAndSourceVisibility_Security_Integration,
		"installation_consent":                TestAgentUXAmbient_InstallationConsent_Security_Integration,
		"revoked_during_model":                TestAgentUXAmbient_RevokedDuringModel_Security_Integration,
		"model_quarantine":                    TestAgentUXAmbient_ModelQuarantine_Security,
		"audience":                            TestAgentUXAmbient_Audience_Security,
		"automatic_and_named_tasks":           TestAgentUXAmbient_AutomaticAndNamedTasks_Security_Integration,
	})
}

func TestTodo_AGENTUX_066_Integration(t *testing.T) {
	runAgentUX066(t, map[string]func(*testing.T){
		"outbox_candidates":    TestAgentUXAmbient_Outbox_Integration,
		"tasks":                TestAgentUXAmbient_Tasks_Integration,
		"delivery_membership":  TestAgentUXAmbient_DeliveryMembership_Security_Integration,
		"surface_and_controls": TestAgentUXAmbient_Surface_Security_Integration,
	})
}

// The screen adds well under 5 ms to posting and keeps the model out of at
// least 80 percent of the seeded corpus; a model that is slow never holds the
// posting lock.
func TestTodo_AGENTUX_066_Performance(t *testing.T) {
	runAgentUX066(t, map[string]func(*testing.T){
		"screen_cost_and_model_share": TestAgentUXAmbient_Screen_Performance,
		"posting_while_model_runs":    TestAgentUXAmbient_PostingWhileModelRuns_Performance_Integration,
	})
}

// Replaying the outbox, and replaying a read after a fault, never produces a
// second action for one message.
func TestTodo_AGENTUX_066_Fault(t *testing.T) {
	runAgentUX066(t, map[string]func(*testing.T){
		"outbox_replay_once":     TestAgentUXAmbient_Outbox_Integration,
		"budget_and_edit_replay": TestAgentUXAmbient_BudgetAndEdits_Fault_Integration,
		"scheduler_and_delivery": TestAgentUXAmbient_SharedSchedulerAndDelivery_Fault_Integration,
	})
}
