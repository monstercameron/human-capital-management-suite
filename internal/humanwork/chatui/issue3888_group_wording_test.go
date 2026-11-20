package chatui

import (
	"strings"
	"testing"
)

func issue3888Group(locale string) Model {
	m := chatux030Model(3)
	m.Locale = locale
	m.Conversations[0].Kind = GroupChat
	m.Conversations[0].Name = "Q4 hiring huddle"
	m.GateJoins = map[string]GateJoin{}
	return m
}

// TestIssue3888_PrivateGroupJoiningRowDoesNotSayAnyone: a private group is
// invitation-only, so its joining row never reads "Anyone can join".
func TestIssue3888_PrivateGroupJoiningRowDoesNotSayAnyone(t *testing.T) {
	for locale, anyone := range map[string]string{"en-US": "Anyone can join", "de-DE": "Jeder kann beitreten", "ar": "يمكن للجميع الانضمام"} {
		m := issue3888Group(locale)
		if got := chatux041GateState(m, m.selected()); got == anyone || got == "" {
			t.Errorf("%s: private group joining state is %q", locale, got)
		}
		page := chatux021Details(t, m, handlers{})
		if strings.Contains(page, anyone) {
			t.Errorf("%s: private group details say %q", locale, anyone)
		}
	}
	m := issue3888Group("en-US")
	if got := chatux041GateState(m, m.selected()); got != "By invitation" {
		t.Errorf("private group joining state = %q, want By invitation", got)
	}
	// A public channel keeps its wording.
	m.Conversations[0].Kind = PublicChannel
	if got := chatux041GateState(m, m.selected()); got != "Anyone can join" {
		t.Errorf("public channel joining state = %q", got)
	}
}

// TestIssue3889_PrivateGroupSettingsAreNotCalledChannel: the settings
// disclosure of a private group names the group, a channel's keeps its name.
func TestIssue3889_PrivateGroupSettingsAreNotCalledChannel(t *testing.T) {
	for locale, tc := range map[string][2]string{
		"en-US": {"Manage group", "Manage channel"},
		"de-DE": {"Gruppe verwalten", "Kanal verwalten"},
		"ar":    {"إدارة المجموعة", "إدارة القناة"},
	} {
		m := issue3888Group(locale)
		page := chatux021Details(t, m, handlers{})
		if !strings.Contains(page, ">"+tc[0]+"<") || strings.Contains(page, tc[1]) {
			t.Errorf("%s: group details label settings wrongly, want %q and not %q", locale, tc[0], tc[1])
		}
		m.Conversations[0].Kind = PrivateChannel
		page = chatux021Details(t, m, handlers{})
		if !strings.Contains(page, ">"+tc[1]+"<") {
			t.Errorf("%s: a channel lost %q", locale, tc[1])
		}
	}
}
