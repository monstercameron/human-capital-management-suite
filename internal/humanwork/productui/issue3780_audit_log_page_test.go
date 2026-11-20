package productui

import (
	"strings"
	"testing"
)

// The Chat settings page carries the audit log, which loads for an administrator
// and is absent from a page a person may not open (#3780).
func TestIssue3780_ChatSettingsPageCarriesAuditLog(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageChatSettings), []string{RoleHCMAdmin})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"chataudit-title", "Audit log", "Loading the audit log"} {
		if !strings.Contains(doc, want) {
			t.Errorf("chat settings page missing %q", want)
		}
	}
	// A person without the Chat settings page sees no audit log and no data.
	other := ApplyRoleVisibility(testView(PageChatSettings), []string{"employee"})
	if doc, err = Render(other); err == nil && strings.Contains(doc, "chataudit-title") {
		t.Error("a person without access to Chat settings was offered the audit log")
	}
}
