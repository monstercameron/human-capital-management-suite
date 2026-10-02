package application

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

// TestTodo_CHATBUG_069 covers an announcement made with an installation that
// was retired when its agent was installed again at a newer version. It is
// named from the agent as it is installed in that conversation now; before, it
// could not be named and the whole Announcements page answered "could not be
// loaded".
func TestTodo_CHATBUG_069(t *testing.T) {
	owner := &proactiveOwner{actor: AgentAnnouncementActor{TenantID: "tenant-a", TenantUUID: uuid.New(), SubjectID: "owner"}}
	source := AgentAnnouncementCatalogNames{
		Versions: catalogVersions{
			{Profile: agentpersona.PersonaVersion{Profile: agentpersona.PersonaProfile{PersonaID: "assistant", Version: 2, DisplayName: "Assistant"}}},
		},
		Installations: catalogInstalls{
			{ID: "new-install", PersonaID: "assistant", PersonaVersion: 2, ConversationID: "general", Conversation: "#general", ConversationKind: "PUBLIC_CHANNEL", Active: true},
			{ID: "elsewhere", PersonaID: "assistant", PersonaVersion: 2, ConversationID: "random", Conversation: "#random", ConversationKind: "PUBLIC_CHANNEL", Active: true},
		},
		Authority: owner,
		OwnerNames: func(context.Context, string, []string) (map[string]string, error) {
			return map[string]string{"owner": "Alex Example"}, nil
		},
	}
	record := agentstore.Announcement{TenantID: owner.actor.TenantUUID, OwnerID: "owner", PersonaID: "assistant", InstallationID: "retired-install", ConversationID: "general"}
	name, room, person, err := source.AgentAnnouncementNames(context.Background(), owner.actor, record)
	if err != nil || name != "Assistant" || room != "#general" || person != "Alex Example" {
		t.Fatalf("an announcement made with a retired installation is not named: %q %q %q %v", name, room, person, err)
	}
	// The agent is no longer in that conversation at all: there is nothing to
	// name it by, and the caller leaves the record out of the list.
	record.ConversationID = "removed-from-here"
	if _, _, _, err = source.AgentAnnouncementNames(context.Background(), owner.actor, record); err == nil {
		t.Fatal("an announcement for a conversation the agent has left was named")
	}
	// Another owner's record is still refused.
	record.ConversationID, record.OwnerID = "general", "someone-else"
	if _, _, _, err = source.AgentAnnouncementNames(context.Background(), owner.actor, record); err == nil {
		t.Fatal("another owner's announcement was named")
	}
}
