package productui

import (
	"strings"
	"testing"
)

// TestTodo_CHATUX_038_ShellBar: on a phone the workspace bar is hidden for the
// conversation and for the conversation list alike, so Chat never stacks two
// bars; the list's own bar carries the workspace menu button.
func TestTodo_CHATUX_038_ShellBar(t *testing.T) {
	sheet := chatShellStylesheet()
	open := `.app-shell:has(.chat-workspace:is([data-has-selection="true"],[data-sidebar-open="true"])) .topbar{display:none}`
	if !strings.Contains(sheet, "@media(max-width:760px){") || !strings.Contains(sheet, open) {
		t.Errorf("the phone shell rule does not hide the workspace bar for the list and the conversation: %s", chatPhoneShellStylesheet)
	}
	if strings.Contains(sheet, `:not([data-sidebar-open="true"])) .topbar`) {
		t.Errorf("the workspace bar is still shown over the conversation list")
	}
}
