package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// An agent that is a member of a channel appears once in the member list, with
// the Agent badge and its own name, and not among the people; it does so
// whichever of the member read and the agent directory arrives first.
func TestTodo_AGENTUX_033(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "alice"}
	icon := agenticon.Value{Glyph: "calendar", Shape: "circle", Foreground: "--hcm-color-info", Background: "--hcm-color-info-surface"}
	if !icon.Valid() {
		t.Fatal("the fixture icon is not one the product draws")
	}
	directory := personaChatDirectory{Personas: []personaChatProfile{
		{Reference: personaChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "assistant", Display: "Assistant", ConversationID: "general"}, Purpose: "Answers everyday questions."},
		{Icon: icon, IconRevision: 3, Reference: personaChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "673214ec-4402-5f09-bf93-0d42e691712f", Display: "Policy Helper", ConversationID: "general"}, Purpose: "Answers policy questions."},
	}}
	// The member read knows each agent only by its identifier. Two agents are in
	// the channel: marking used to stop at the first one the directory listed.
	members := func() []chatui.Member {
		return []chatui.Member{{ID: "alice", HomeTenantID: "tenant", Name: "Alice Smith"}, {ID: "673214ec-4402-5f09-bf93-0d42e691712f", HomeTenantID: "tenant", Name: "673214ec-4402-5f09-bf93-0d42e691712f"}, {ID: "bob", HomeTenantID: "tenant", Name: "Bob Jones"}, {ID: "assistant", HomeTenantID: "tenant", Name: "assistant"}}
	}
	base := func() chatui.Model {
		return chatui.Model{State: chatui.StateReady, Locale: "en-US", ShowDetails: true, SelectedID: "general", CurrentTenantID: "tenant", CurrentUser: "alice",
			Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true, MemberCount: 3}}}
	}
	check := func(order string, model chatui.Model) {
		t.Helper()
		if len(model.Members) != 4 || !model.Members[1].Agent || model.Members[1].Name != "Policy Helper" || model.Members[1].Icon != icon || model.Members[0].Agent || model.Members[2].Agent || !model.Members[3].Agent || model.Members[3].Name != "Assistant" {
			t.Fatalf("%s: members = %+v", order, model.Members)
		}
		page, err := ui.RenderToString(chatui.Build(model))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(page, "673214ec") && strings.Contains(strings.SplitN(page, "member-list", 2)[1], ">673214ec-4402-5f09-bf93-0d42e691712f<") {
			t.Fatalf("%s: the member list shows the agent's identifier", order)
		}
		list := page[strings.Index(page, `id="chat-agents-here"`):]
		people := list[strings.Index(list, `id="chat-details-people"`):]
		agents := list[:strings.Index(list, `id="chat-details-people"`)]
		if strings.Count(agents, "<li") != 2 || !strings.Contains(agents, "Policy Helper") || !strings.Contains(agents, "Assistant") || strings.Count(agents, "agent-badge") < 2 {
			t.Fatalf("%s: the agents of the member list: %s", order, agents)
		}
		peopleList := people[:strings.Index(people, "</ul>")]
		if strings.Contains(peopleList, "Policy Helper") || strings.Contains(peopleList, "Assistant") || strings.Count(peopleList, "<li") != 2 || !strings.Contains(peopleList, "Alice Smith") || !strings.Contains(peopleList, "Bob Jones") {
			t.Fatalf("%s: an agent is listed among the people, or a person is missing: %s", order, peopleList)
		}
	}

	// The member read first, then the directory.
	model := base()
	model.Members = members()
	applyPersonaDirectoryResult(&model, directory, cfg, "general", nil, nil)
	check("members, then agents", model)

	// The directory first, then the member read (what loadChatMembers does).
	model = base()
	applyPersonaDirectoryResult(&model, directory, cfg, "general", nil, nil)
	model.Members, _ = agentUX033MarkAgentMembers(members(), model.ResolvedPersonaMentions)
	check("agents, then members", model)

	// Nothing to mark changes nothing, and people are never taken for agents.
	people := members()[:1]
	if same, changed := agentUX033MarkAgentMembers(people, model.ResolvedPersonaMentions); changed || len(same) != 1 || same[0].Agent {
		t.Fatalf("a person was marked: %+v", same)
	}
	if same, changed := agentUX033MarkAgentMembers(members(), nil); changed || same[1].Agent {
		t.Fatal("a member was marked with no agent directory")
	}
	already, _ := agentUX033MarkAgentMembers(members(), model.ResolvedPersonaMentions)
	if _, changed := agentUX033MarkAgentMembers(already, model.ResolvedPersonaMentions); changed {
		t.Fatal("marking twice reported a change")
	}
	// An agent of another conversation, or one with no name, marks nobody.
	stray := []chatui.ResolvedPersonaMention{{Reference: chatui.ChatReference{Kind: "AGENT_MENTION", ID: "bob", Display: " "}}}
	if _, changed := agentUX033MarkAgentMembers(members(), stray); changed {
		t.Fatal("an agent with no name marked a member")
	}
}
