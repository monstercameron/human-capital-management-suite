package application

import "testing"

// TestTodo_AGENTUX_053 covers Birthday Buddy: a starter built on the announcement
// mechanism with a people source instead of a document, posting one message on
// the morning of a member's birthday (day and month only), nothing when nobody
// has a birthday, and a #general whose old test questions are archived.
func TestTodo_AGENTUX_053(t *testing.T) {
	s15Run(t,
		s15Proof{"starters", TestAgentUXDemo_Starters},
		s15Proof{"birthday announcement", TestAgentUXDemo_BirthdayAnnouncement},
		s15Proof{"people source", TestAgentUXDemo_PeopleSource},
		s15Proof{"document source is unchanged", TestAgentUXDemo_DocumentSource},
		s15Proof{"general cleanup archives the test questions", TestAgentUXDemo_GeneralCleanup},
		s15Proof{"seed inputs", TestAgentUXDemo_SeedInputs},
	)
}

// TestTodo_AGENTUX_053_Security: the birth year is never read by the agent, an
// opted-out person is never named and a person outside the conversation is never
// named in it.
func TestTodo_AGENTUX_053_Security(t *testing.T) {
	s15Run(t,
		s15Proof{"birthday profile", TestAgentUXDemo_BirthdayProfile_Security},
		s15Proof{"birthday runtime", TestAgentUXDemo_BirthdayRuntime_Security},
		s15Proof{"birthday output", TestAgentUXDemo_BirthdayOutput_Security},
		s15Proof{"people source", TestAgentUXDemo_PeopleSource_Security},
		s15Proof{"people names", TestAgentUXDemo_PeopleName_Security},
		s15Proof{"general cleanup", TestAgentUXDemo_GeneralCleanup_Security},
		s15Proof{"starter manifest", TestAgentUXDemo_StarterManifest_Security},
	)
}

// TestTodo_AGENTUX_053_Integration reads the birthdays from the core directory
// and cleans the demo channel against the test database.
func TestTodo_AGENTUX_053_Integration(t *testing.T) {
	s15Run(t,
		s15Proof{"birthday core directory", TestAgentUXDemo_BirthdayCore_Integration},
		s15Proof{"general cleanup", TestAgentUXDemo_GeneralCleanup_Integration},
		s15Proof{"general cleanup faults", TestAgentUXDemo_GeneralCleanup_Fault},
		s15Proof{"seed inputs faults", TestAgentUXDemo_SeedInputs_Fault},
	)
}
