package application

import "testing"

// TestTodo_AGENTUX_054 covers Support Desk: a starter with two declared skills,
// a seeded inbox of customer emails with an owner's "Simulate a customer email"
// control, one ticket per email and one alert in the incident channel.
func TestTodo_AGENTUX_054(t *testing.T) {
	s15Run(t,
		s15Proof{"starters", TestAgentUXDemo_Starters},
		s15Proof{"support executor creates one ticket and one alert", TestAgentUXDemo_SupportExecutor},
		s15Proof{"inbox surface", TestAgentUXDemo_InboxSurface},
		s15Proof{"seed inputs", TestAgentUXDemo_SeedInputs},
	)
}

// TestTodo_AGENTUX_054_Security: prompt injection in the email body, a spoofed
// sender, replay, an email naming another tenant and a severity beyond the
// agent's limits.
func TestTodo_AGENTUX_054_Security(t *testing.T) {
	s15Run(t,
		s15Proof{"executor", TestAgentUXDemo_SupportExecutor_Security},
		s15Proof{"plan", TestAgentUXDemo_SupportPlan_Security},
		s15Proof{"email containment", TestAgentUXDemo_EmailContainment_Security},
		s15Proof{"inbox", TestAgentUXDemo_InboxSurface_Security},
		s15Proof{"project authority", TestAgentUXDemo_ProjectAuthority_Security},
		s15Proof{"starter manifest", TestAgentUXDemo_StarterManifest_Security},
	)
}

// TestTodo_AGENTUX_054_Integration: the executor's two effects with their
// recorded plan, the inbox queue and the seeded inputs.
func TestTodo_AGENTUX_054_Integration(t *testing.T) {
	s15Run(t,
		s15Proof{"executor", TestAgentUXDemo_SupportExecutor},
		s15Proof{"inbox", TestAgentUXDemo_InboxSurface},
		s15Proof{"seed inputs", TestAgentUXDemo_SeedInputs},
	)
}

// TestTodo_AGENTUX_054_Fault: the project service or chat unavailable between the
// two effects leaves no duplicate ticket and sends the alert once on recovery.
func TestTodo_AGENTUX_054_Fault(t *testing.T) {
	s15Run(t,
		s15Proof{"executor", TestAgentUXDemo_SupportExecutor_Fault},
		s15Proof{"inbox queue", TestAgentUXDemo_InboxQueue_Fault},
		s15Proof{"seed inputs", TestAgentUXDemo_SeedInputs_Fault},
	)
}
