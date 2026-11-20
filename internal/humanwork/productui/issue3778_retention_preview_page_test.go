package productui

import (
	"strings"
	"testing"
)

// The Chat settings page carries the retention preview beside the policy editor
// for an administrator, and not for a person who may not open the page (#3778).
func TestIssue3778_ChatSettingsPageCarriesRetentionPreview(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageChatSettings), []string{RoleHCMAdmin})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"chatretpreview-title", "Preview what retention would remove", "nothing is deleted", `id="chatretpreview-run"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("chat settings page missing %q", want)
		}
	}
	if strings.Index(doc, "chat-retention-title") > strings.Index(doc, "chatretpreview-title") {
		t.Error("the preview must follow the policy editor")
	}
	other := ApplyRoleVisibility(testView(PageChatSettings), []string{"employee"})
	if doc, err = Render(other); err == nil && strings.Contains(doc, "chatretpreview-title") {
		t.Error("a person without access to Chat settings was offered the retention preview")
	}
}
