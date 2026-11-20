package productui

import (
	"strings"
	"testing"
)

// The Chat settings page carries the legal holds, which load for an
// administrator and are absent from a page a person may not open (#3779).
func TestIssue3779_ChatSettingsPageCarriesLegalHolds(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageChatSettings), []string{RoleHCMAdmin})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"chathold-title", "Legal holds", "Loading legal holds"} {
		if !strings.Contains(doc, want) {
			t.Errorf("chat settings page missing %q", want)
		}
	}
	other := ApplyRoleVisibility(testView(PageChatSettings), []string{"employee"})
	if doc, err = Render(other); err == nil && strings.Contains(doc, "chathold-title") {
		t.Error("a person without access to Chat settings was offered the legal holds")
	}
}
