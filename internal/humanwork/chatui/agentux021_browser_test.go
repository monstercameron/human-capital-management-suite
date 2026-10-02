package chatui

import (
	"strings"
	"testing"
)

// TestTodo_AGENTUX_021_Browser renders the mention menu as a member of a
// conversation sees it and checks the two things the page has to get right:
// an agent placed in the open conversation is listed once, under Agents, by
// name, handle and purpose; and the same identity is never offered as a
// person, even when the conversation's member list carries it. A placement in
// another conversation is not offered here.
func TestTodo_AGENTUX_021_Browser(t *testing.T) {
	agent := func(conversation string) ResolvedPersonaMention {
		return ResolvedPersonaMention{
			Reference: ChatReference{Kind: "AGENT_MENTION", TenantID: "tenant", ID: "policy-helper", Display: "Policy Helper", ConversationID: conversation},
			Handle:    "policy-helper", Purpose: "Answers policy questions with citations.",
		}
	}
	headings := map[string][2]string{
		"en-US": {"Agents", "People in this conversation"},
		"de-DE": {"Agenten", "Personen in dieser Unterhaltung"},
		"ar":    {"الوكلاء", "الأشخاص في هذه المحادثة"},
	}
	for locale, heading := range headings {
		for _, conversation := range []string{"general", "direct-walt"} {
			t.Run(locale+"/"+conversation, func(t *testing.T) {
				model := agentUXMentionModel(PersonaLookupReady)
				model.Locale, model.SelectedID, model.PersonaLookupConversationID = locale, conversation, conversation
				model.ResolvedPersonaMentions = []ResolvedPersonaMention{agent(conversation)}
				// The agent's own chat identity is a member row of the
				// conversation, under its id and under another subject id
				// with the same handle. Neither may surface as a person.
				model.Members = []Member{
					{ID: "person", Name: "Camila Morales"},
					{ID: "policy-helper", Name: "Policy Helper", Agent: true},
					{ID: "POLICY-HELPER", Name: "Policy Helper"},
				}
				markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true}, "chat-composer"))
				agents := strings.Index(markup, `class="mention-heading agents"`)
				people := strings.Index(markup, `class="mention-heading people"`)
				if agents < 0 || people < 0 || agents > people {
					t.Fatalf("the menu does not show Agents and then people: %s", markup)
				}
				agentGroup, peopleGroup := markup[agents:people], markup[people:]
				for _, want := range []string{heading[0], "Policy Helper", "@policy-helper", "Answers policy questions with citations.", `class="mention-agent-row"`} {
					if !strings.Contains(agentGroup, want) {
						t.Errorf("the Agents group is missing %q: %s", want, agentGroup)
					}
				}
				if got := strings.Count(markup, `class="mention-agent-row"`); got != 1 {
					t.Errorf("the agent is listed %d times, want once", got)
				}
				if !strings.Contains(peopleGroup, heading[1]) || !strings.Contains(peopleGroup, "Camila Morales") {
					t.Errorf("the people group lost its heading or its person: %s", peopleGroup)
				}
				if strings.Contains(peopleGroup, "Policy Helper") || strings.Contains(peopleGroup, "policy-helper") {
					t.Errorf("the agent is offered as a person: %s", peopleGroup)
				}
				// Typing the agent's name narrows the menu to the agent
				// alone; no person row answers to it.
				options, _ := mentionOptionsForState(model, "pol", "chat-composer", false)
				if len(options) != 1 || options[0].persona == nil || options[0].person != nil {
					t.Errorf("typing the agent's name offers %+v, want the agent alone", options)
				}
				typed := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Query: "pol", Open: true}, "chat-composer"))
				if strings.Contains(typed, `class="mention-heading people"`) || !strings.Contains(typed, `class="mention-agent-row"`) {
					t.Errorf("typing the agent's name shows a people group or no agent: %s", typed)
				}
			})
		}
	}

	// A conversation where the agent is not placed: the lookup answered for
	// it with nothing, and a placement that belongs to another conversation
	// is not accepted in its place.
	t.Run("not placed here", func(t *testing.T) {
		model := agentUXMentionModel(PersonaLookupReady)
		model.SelectedID, model.PersonaLookupConversationID = "payroll", "payroll"
		model.ResolvedPersonaMentions = []ResolvedPersonaMention{agent("general")}
		markup := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true}, "chat-composer"))
		if strings.Contains(markup, "Policy Helper") || strings.Contains(markup, `class="mention-agent-row"`) {
			t.Errorf("an agent placed elsewhere is offered here: %s", markup)
		}
		if !strings.Contains(markup, "Camila Morales") {
			t.Errorf("the people of the conversation are missing: %s", markup)
		}
		model.ResolvedPersonaMentions = nil
		empty := renderNode(t, mentionMenu(model, mentionState{Target: "chat-composer", Open: true}, "chat-composer"))
		if strings.Contains(empty, `class="mention-heading agents"`) || strings.Contains(empty, "Policy Helper") {
			t.Errorf("a conversation with no agent shows an Agents section: %s", empty)
		}
	})
}
