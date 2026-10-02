package chatui

import (
	"strings"
	"testing"
)

// agentux070ChannelModel is a channel with an agent in it whose manager the
// server told that the setting may be changed.
func agentux070ChannelModel(locale string) Model {
	model := chat4Fixture(locale, "answered", false)
	model.CurrentUser = "alice"
	model.Callbacks.SetChannelAgentPrivacy = func(bool) {}
	model.ChannelAgentPrivacy = ChannelAgentPrivacyState{ConversationID: "general", Known: true, CanChange: true}
	return model
}

// AGENTUX-070: the channel administrator's requirement is one row of
// Conversation details under Manage channel, shown only to a person the server
// said may change it, in a channel that has agents.
func TestTodo_AGENTUX_070_ChannelPrivacyRow(t *testing.T) {
	model := agentux070ChannelModel("en-US")
	row := agentux070ChannelRow(model, handlers{}, model.selected())
	if row == nil {
		t.Fatal("a manager of a channel with an agent was not offered the setting")
	}
	markup := renderNode(t, row)
	for _, want := range []string{"Agent answers", "Visible to the channel", "Agent answers here must be private", `role="switch"`, `aria-checked="false"`, `data-action="agent-channel-private"`, `data-id="1"`, "manage-agent-privacy"} {
		if !strings.Contains(markup, want) {
			t.Errorf("the row lacks %q: %s", want, markup)
		}
	}

	// Required: the value says so and the switch turns it off.
	model.ChannelAgentPrivacy.Private = true
	markup = renderNode(t, agentux070ChannelRow(model, handlers{}, model.selected()))
	if !strings.Contains(markup, "Must be private") || !strings.Contains(markup, `aria-checked="true"`) || !strings.Contains(markup, `data-id="0"`) {
		t.Errorf("a channel that requires private answers: %s", markup)
	}

	// While it saves the switch cannot be pressed again; a failure says so, in the row.
	model.ChannelAgentPrivacy.Saving = true
	if markup = renderNode(t, agentux070ChannelRow(model, handlers{}, model.selected())); !strings.Contains(markup, "Saving") || !strings.Contains(markup, `aria-busy="true"`) || !strings.Contains(markup, "disabled") {
		t.Errorf("a saving row: %s", markup)
	}
	model.ChannelAgentPrivacy.Saving, model.ChannelAgentPrivacy.Failed = false, true
	if markup = renderNode(t, agentux070ChannelRow(model, handlers{}, model.selected())); !strings.Contains(markup, "Could not save this setting. Try again.") || !strings.Contains(markup, `role="alert"`) {
		t.Errorf("a failed save: %s", markup)
	}

	// Nothing to show for anyone else, or where there is nothing to apply it to.
	for name, change := range map[string]func(*Model){
		"not a manager":         func(m *Model) { m.ChannelAgentPrivacy.CanChange = false },
		"the server said none":  func(m *Model) { m.ChannelAgentPrivacy = ChannelAgentPrivacyState{} },
		"another conversation":  func(m *Model) { m.ChannelAgentPrivacy.ConversationID = "people" },
		"no agent in the room":  func(m *Model) { m.ResolvedPersonaMentions = nil },
		"a direct conversation": func(m *Model) { m.Conversations[0].Kind = DirectMessage },
	} {
		m := agentux070ChannelModel("en-US")
		change(&m)
		if agentux070ChannelRow(m, handlers{}, m.selected()) != nil {
			t.Errorf("%s: the setting is offered", name)
		}
	}
}

// The words are in the three languages, never a key.
func TestTodo_AGENTUX_070_ChannelPrivacyRow_Browser(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "Antworten von Agenten", "ar": "إجابات الوكلاء"} {
		model := agentux070ChannelModel(locale)
		markup := renderNode(t, agentux070ChannelRow(model, handlers{}, model.selected()))
		if !strings.Contains(markup, want) || strings.Contains(markup, "agentux070") || strings.Contains(markup, "Agent answers here") {
			t.Errorf("%s: %s", locale, markup)
		}
	}
	// The row sits in Manage channel, in its own group, for a manager.
	model := agentux070ChannelModel("en-US")
	model.IsTenantAdmin = true
	if text := agentux070ChannelText(model, "group"); text != "Agents" {
		t.Errorf("group caption = %q", text)
	}
}

// The press reaches the application with the choice it carries, and a press that
// cannot do anything (saving, not allowed) changes nothing.
func TestTodo_AGENTUX_070_ChannelPrivacyChoice(t *testing.T) {
	var asked []bool
	model := agentux070ChannelModel("en-US")
	model.Callbacks.SetChannelAgentPrivacy = func(private bool) { asked = append(asked, private) }
	if !agentux070ChannelChoice(model, "1") || !agentux070ChannelChoice(model, "0") || len(asked) != 2 || !asked[0] || asked[1] {
		t.Fatalf("choices reached the application as %v", asked)
	}
	if agentux070ChannelChoice(model, "maybe") || len(asked) != 2 {
		t.Fatalf("an unknown choice was taken: %v", asked)
	}
	model.ChannelAgentPrivacy.Saving = true
	if agentux070ChannelChoice(model, "1") || len(asked) != 2 {
		t.Fatalf("a press while saving reached the application: %v", asked)
	}
	model.ChannelAgentPrivacy.Saving, model.ChannelAgentPrivacy.CanChange = false, false
	if agentux070ChannelChoice(model, "1") || len(asked) != 2 {
		t.Fatalf("a member who may not change it was taken: %v", asked)
	}
}
