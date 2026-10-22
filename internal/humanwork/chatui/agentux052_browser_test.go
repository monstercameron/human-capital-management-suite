package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func agentReactionSettingsRender(t *testing.T, props agentReactionSettingsProps, s agentReactionSettingsState) string {
	t.Helper()
	node := agentReactionSettingsView(props, s, ui.Handler{}, ui.Handler{})
	if node == nil {
		return ""
	}
	return renderNode(t, node)
}

func agentReactionRowModel(kind ConversationKind, admin bool) Model {
	m := Model{Locale: "en-US", SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "Room", Kind: kind, HostTenantID: "tenant-a"}}, CurrentTenantID: "tenant-a"}
	m.IsTenantAdmin = admin
	return m
}

// TestTodo_AGENTUX_052_Browser: the two switches have a row each, in the place
// the person looks for it — the workspace switch in the workspace Chat
// settings, the channel switch in Manage channel for people who administer the
// channel — each saying what it changes in one line and showing loading, saved
// and failed states, in the reader's language.
func TestTodo_AGENTUX_052_Browser(t *testing.T) {
	loaded := agentReactionSettingsState{Loaded: true, Data: AgentReactionSettingsData{Workspace: true, Channel: false, IsChannel: true, CanManageWorkspace: true, CanManageChannel: true}}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			ws := agentReactionSettingsRender(t, agentReactionSettingsProps{Locale: locale, Scope: agentReactionScopeWorkspace}, loaded)
			for _, want := range []string{AgentReactionSettingsCopy(locale, "ws_switch"), AgentReactionSettingsCopy(locale, "ws_hint"), AgentReactionSettingsCopy(locale, "ws_intro"), `role="switch"`, `checked`, `id="agentreactions-switch-workspace"`, `aria-describedby="agentreactions-switch-workspace-hint"`} {
				if !strings.Contains(ws, want) {
					t.Fatalf("workspace row is missing %q: %s", want, ws)
				}
			}
			if locale == "ar" && !strings.Contains(ws, `dir="rtl"`) {
				t.Fatal("the Arabic workspace row is not right to left")
			}
			ch := agentReactionSettingsRender(t, agentReactionSettingsProps{Locale: locale, Scope: agentReactionScopeChannel, Tenant: "tenant-a", Conversation: "room"}, loaded)
			if !strings.Contains(ch, AgentReactionSettingsCopy(locale, "ch_switch")) || !strings.Contains(ch, AgentReactionSettingsCopy(locale, "ch_hint")) || strings.Contains(ch, "checked") {
				t.Fatalf("the channel row is wrong or on by default: %s", ch)
			}
		})
	}
	for _, key := range []string{"title", "ws_intro", "ws_switch", "ws_hint", "ch_switch", "ch_hint", "loading", "loadfailed", "saving", "saved", "savefailed", "retry", "unavailable", "denied", "on", "off"} {
		en := AgentReactionSettingsCopy("en-US", key)
		if en == "" || en == key {
			t.Fatalf("english %q is %q", key, en)
		}
		for _, locale := range []string{"de-DE", "ar"} {
			if got := AgentReactionSettingsCopy(locale, key); got == "" || got == key || got == en {
				t.Fatalf("%s %q is %q", locale, key, got)
			}
		}
	}

	// States: loading shell, a failed read that offers Try again, a save in
	// flight, saved and failed saves, the person who may not administer.
	props := agentReactionSettingsProps{Locale: "en-US", Scope: agentReactionScopeWorkspace}
	if got := agentReactionSettingsRender(t, props, agentReactionSettingsState{}); !strings.Contains(got, "Loading reaction settings…") || !strings.Contains(got, `aria-busy="true"`) || strings.Contains(got, `role="switch"`) {
		t.Fatalf("loading: %s", got)
	}
	if got := agentReactionSettingsRender(t, props, agentReactionSettingsState{LoadFailed: true}); !strings.Contains(got, "Reaction settings could not load. Try again.") || !strings.Contains(got, ">Try again<") || strings.Contains(got, `role="switch"`) {
		t.Fatalf("load failed: %s", got)
	}
	saving, saved, failed := loaded, loaded, loaded
	saving.Saving, saved.Saved, failed.Fails = true, true, true
	if got := agentReactionSettingsRender(t, props, saving); !strings.Contains(got, "Saving…") {
		t.Fatalf("saving: %s", got)
	}
	if got := agentReactionSettingsRender(t, props, saved); !strings.Contains(got, "chatlangadmin-saved") || !strings.Contains(got, ">Saved<") {
		t.Fatalf("saved: %s", got)
	}
	if got := agentReactionSettingsRender(t, props, failed); !strings.Contains(got, `role="alert"`) || !strings.Contains(got, "Could not save. The setting is unchanged. Try again.") || !strings.Contains(got, `role="switch"`) {
		t.Fatalf("failed save keeps the switch and says so: %s", got)
	}
	member := loaded
	member.Data.CanManageWorkspace = false
	if got := agentReactionSettingsRender(t, props, member); !strings.Contains(got, "Only a workspace administrator can change agent reactions for the workspace.") || strings.Contains(got, `role="switch"`) {
		t.Fatalf("a member saw the workspace switch: %s", got)
	}
	notChannelAdmin := loaded
	notChannelAdmin.Data.CanManageChannel = false
	if got := agentReactionSettingsRender(t, agentReactionSettingsProps{Locale: "en-US", Scope: agentReactionScopeChannel}, notChannelAdmin); got != "" {
		t.Fatalf("the channel row is drawn for a person the server says may not change it: %s", got)
	}

	// Placement: the workspace panel in the workspace Chat settings, the
	// channel row in Manage channel (not in a direct conversation, not for a
	// member), and nothing drawn where reactions are not composed.
	if got := renderNode(t, AgentReactionWorkspaceSettingsPanel("en-US")); !strings.Contains(got, "Agent reactions") || !strings.Contains(got, "Loading reaction settings…") {
		t.Fatalf("workspace panel at rest: %s", got)
	}
	admin := agentReactionRowModel(PublicChannel, true)
	if agentReactionChannelRow(admin, handlers{}, admin.selected()) == nil {
		t.Fatal("an administrator did not get the channel switch")
	}
	memberModel := agentReactionRowModel(PublicChannel, false)
	if agentReactionChannelRow(memberModel, handlers{}, memberModel.selected()) != nil {
		t.Fatal("a member who administers nothing got the channel switch")
	}
	dm := agentReactionRowModel(DirectMessage, true)
	if agentReactionChannelRow(dm, handlers{}, dm.selected()) != nil {
		t.Fatal("a direct conversation has a channel switch")
	}
	if got := agentReactionSettingsRender(t, agentReactionSettingsProps{Locale: "en-US", Scope: agentReactionScopeWorkspace}, agentReactionSettingsState{Loaded: true, Unavailable: true}); !strings.Contains(got, "Agent reactions are not set up on this server.") || strings.Contains(got, `role="switch"`) {
		t.Fatalf("reactions not composed: %s", got)
	}
	if got := agentReactionSettingsRender(t, agentReactionSettingsProps{Locale: "en-US", Scope: agentReactionScopeChannel}, agentReactionSettingsState{Loaded: true, Unavailable: true}); got != "" {
		t.Fatalf("a channel row is drawn where reactions are not composed: %s", got)
	}
}
