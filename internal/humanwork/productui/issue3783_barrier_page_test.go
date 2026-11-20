package productui

import (
	"strings"
	"testing"
)

// The Chat settings page carries the information barriers, which load for an
// administrator and are absent from a page a person may not open (#3783).
func TestIssue3783_ChatSettingsPageCarriesBarriers(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageChatSettings), []string{RoleHCMAdmin})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"chatbarrier-title", "Information barriers", "Loading information barriers"} {
		if !strings.Contains(doc, want) {
			t.Errorf("chat settings page missing %q", want)
		}
	}
	other := ApplyRoleVisibility(testView(PageChatSettings), []string{"employee"})
	if doc, err = Render(other); err == nil && strings.Contains(doc, "chatbarrier-title") {
		t.Error("a person without access to Chat settings was offered the information barriers")
	}
}
