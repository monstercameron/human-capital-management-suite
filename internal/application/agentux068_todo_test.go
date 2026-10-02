package application

import "testing"

// TestTodo_AGENTUX_068 covers Reminder: the shared audience decision with
// Task Catcher, explicit requests that are never second-guessed, the stated lead
// times, a reminder whose text comes only from its source message, and delivery
// through the announcement scheduler.
func TestTodo_AGENTUX_068(t *testing.T) {
	s15Run(t,
		s15Proof{"starter manifest", TestAgentUXAmbient_StarterManifest_Golden},
		s15Proof{"consent and source", TestAgentUXAmbient_ReminderConsentAndSource_Integration},
		s15Proof{"screening", TestAgentUXAmbient_Screen_Performance},
	)
}

// TestTodo_AGENTUX_068_Security: a private reminder is never posted publicly, a
// reminder is never delivered to someone who left the conversation and a message
// cannot schedule a reminder into another conversation.
func TestTodo_AGENTUX_068_Security(t *testing.T) {
	s15Run(t,
		s15Proof{"audience", TestAgentUXAmbient_Audience_Security},
		s15Proof{"delivery membership", TestAgentUXAmbient_DeliveryMembership_Security_Integration},
		s15Proof{"model quarantine", TestAgentUXAmbient_ModelQuarantine_Security},
		s15Proof{"parent and source visibility", TestAgentUXAmbient_ParentAndSourceVisibility_Security_Integration},
	)
}

// TestTodo_AGENTUX_068_Integration: one scheduler and one delivery path shared
// with announcements, and the outbox.
func TestTodo_AGENTUX_068_Integration(t *testing.T) {
	s15Run(t,
		s15Proof{"shared scheduler and delivery", TestAgentUXAmbient_SharedSchedulerAndDelivery_Fault_Integration},
		s15Proof{"reminder consent", TestAgentUXAmbient_ReminderConsentAndSource_Integration},
		s15Proof{"outbox", TestAgentUXAmbient_Outbox_Integration},
	)
}

// TestTodo_AGENTUX_068_Golden is the labelled suite with expected audience, time
// and lead time.
func TestTodo_AGENTUX_068_Golden(t *testing.T) {
	TestAgentUXAmbient_LabelledSuites_Golden(t)
}

// TestTodo_AGENTUX_068_Property: time zone conversion and daylight-saving
// transitions never fire a reminder twice or skip it.
func TestTodo_AGENTUX_068_Property(t *testing.T) {
	s15Run(t,
		s15Proof{"time", TestAgentUXAmbient_Time_Property},
		s15Proof{"schedule math", TestAgentUXProactive_ScheduleMath},
	)
}
