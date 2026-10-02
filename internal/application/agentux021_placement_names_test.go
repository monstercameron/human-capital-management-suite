package application

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Agent operations names a placement the way Chat names the conversation: a
// channel by "#" and its name, a direct conversation by the person it is with,
// and never by a position ("Conversation 1"). A conversation the viewer cannot
// see is described only as a person's private conversation with the agent.
func TestTodo_AGENTUX_021_PlacementNames(t *testing.T) {
	principal := personaCatalogSourcePrincipal(t)
	public := chat.Conversation{ID: "general", TenantID: "tenant-a", Name: "General", Kind: chat.PublicChannel}
	direct := chat.Conversation{ID: "policy-dm", TenantID: "tenant-a", Name: "Policy Helper", Kind: chat.Direct}
	members := []chat.Membership{
		{ConversationID: public.ID, TenantID: public.TenantID, HomeTenantID: public.TenantID, SubjectID: "admin"},
		{ConversationID: direct.ID, TenantID: direct.TenantID, HomeTenantID: direct.TenantID, SubjectID: "admin"},
		{ConversationID: direct.ID, TenantID: direct.TenantID, HomeTenantID: direct.TenantID, SubjectID: "walt"},
	}
	source := AgentPersonaRunSource{Conversations: &ChatDirectoryPersonaCatalogTargets{
		Chat:      personaCatalogChatFake{rooms: []chat.Conversation{public, direct}, members: members},
		Directory: agentUXSetup3Directory{},
	}}
	label := func(id string) string {
		return source.conversationLabel(context.Background(), principal, id, "Policy Helper")
	}
	if got := label("general"); got != "#General" {
		t.Fatalf("a channel placement is named %q", got)
	}
	if got := label("policy-dm"); got != "Your conversation with Walt Brennan" {
		t.Fatalf("a direct placement is named %q", got)
	}
	for _, id := range []string{"general", "policy-dm", "unseen"} {
		if got := label(id); strings.HasPrefix(got, "Conversation ") {
			t.Fatalf("%s is named by position: %q", id, got)
		}
	}
	if got := label("unseen"); got != "A person's private conversation with Policy Helper" {
		t.Fatalf("a conversation the viewer cannot see is named %q", got)
	}
}
