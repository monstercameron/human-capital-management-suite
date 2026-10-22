package productui

import (
	"strings"
	"testing"
)

// The workspace Chat settings page carries the agent reaction switch beside
// the voice and location switches (AGENTUX-052).
func TestAgentUX052_ChatSettingsPageCarriesReactionSwitch(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageChatSettings), []string{RoleHCMAdmin})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Agent reactions", "Loading reaction settings…", "agentreactions-title"} {
		if !strings.Contains(doc, want) {
			t.Errorf("chat settings page missing %q", want)
		}
	}
}
