package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHATBUG_014_DirectoryReadsSkillsOnce: the agent directory of a
// conversation reads the caller's skills once, however many agents the
// conversation has, and lists the same agents as when it read them per agent.
func TestTodo_CHATBUG_014_DirectoryReadsSkillsOnce(t *testing.T) {
	surface, ctx, _, _, _ := personaSurfaceFixture(t)
	references := surface.References.(*personaSurfaceReferencesFixture)
	first := references.candidates[0]
	// Two more references to agents of the conversation, each current.
	for _, id := range []string{"agent:coach-emea", "agent:coach-apac"} {
		facts := references.byReference[first.ID]
		facts.ReferenceID = id
		reference := first.Reference
		reference.ID = id
		references.byReference[id] = facts
		references.candidates = append(references.candidates, chat.ReferenceCandidate{Reference: reference, Eligible: true})
	}
	discovery := surface.Skills.(*agentUserCatalogDiscovery)

	directory, err := surface.Directory(ctx, "channel-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(directory.Personas) != 3 {
		t.Fatalf("the directory lists %d agents, want the three of the conversation: %+v", len(directory.Personas), directory.Personas)
	}
	for i, id := range []string{"agent:coach", "agent:coach-emea", "agent:coach-apac"} {
		if directory.Personas[i].Reference.ID != id || len(directory.Personas[i].Skills) != 1 || directory.Personas[i].Skills[0].Name != "Read policy" {
			t.Fatalf("agent %d = %+v", i, directory.Personas[i])
		}
	}
	if len(discovery.called) != 1 || discovery.called[0] != personaChatReplyPurpose {
		t.Fatalf("three agents read the caller's skills %d times (%v), want once", len(discovery.called), discovery.called)
	}

	// A conversation with no agent does not read them at all.
	references.candidates = nil
	discovery.called = nil
	if empty, err := surface.Directory(ctx, "channel-a"); err != nil || len(empty.Personas) != 0 || len(discovery.called) != 0 {
		t.Fatalf("an empty directory = %+v after %d skill reads (%v)", empty.Personas, len(discovery.called), err)
	}
}
