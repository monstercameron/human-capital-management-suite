package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

// localAgentDemoStarterSpec is everything the shared preparation needs to know
// about one starter beyond what Policy Helper's profile already says: the
// starter, what its versions are built from, how its manifest is made, and where
// it is placed (AGENTUX-049).
type localAgentDemoStarterSpec struct {
	starter agenttemplate.PersonaStarter
	plan    localAgentDemoStarterPlan
	// manifest makes the manifest the starter's version pins; nil leaves the
	// agent's existing versions alone (Policy Helper).
	manifest func(context.Context, *agentstore.Store, uuid.UUID) (agentmanifest.Manifest, error)
	// grantSkills gives every skill the version pins a current administrator
	// grant over the agent's audience (CHATLIVE-001).
	grantSkills bool
	// direct creates the administrator's conversation with the agent and installs
	// the agent there as well as in #general.
	direct bool
	// rooms, when set, replaces the default placement: the agent is installed in
	// these rooms only, under each room's own ceiling.
	rooms []string
	// unplaced prepares and publishes the agent without installing it in any
	// conversation (Support Desk acts on its inbox, not on a conversation).
	unplaced bool
}

// agentUXDemoStarterSuites are the evaluation suites of the starters added after
// Policy Helper and Assistant. The evaluation key is registered for each.
func agentUXDemoStarterSuites() []agenteval.PersonaSuite {
	return []agenteval.PersonaSuite{
		agenteval.BirthdayBuddySuite(personaChatReplySkillID),
		agenteval.SupportDeskSuite(agentskills.SupportCreateTicket, agentskills.SupportAlertChannel),
		agentUXAmbientPersonaSuite("task-catcher"),
		agentUXAmbientPersonaSuite("reminder"),
	}
}

// agentUXDemoStarterSuite resolves one of those suites by its identifier.
func agentUXDemoStarterSuite(id string) (agenteval.PersonaSuite, bool) {
	for _, suite := range agentUXDemoStarterSuites() {
		if suite.ID == id {
			return suite, true
		}
	}
	return agenteval.PersonaSuite{}, false
}
