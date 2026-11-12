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

// TestIssue_3607_ListBarHidesBrand: while the conversation list is open, the
// shell's brand cluster must be hidden with an important declaration, because
// the shell forces it to display:grid!important at phone width. A plain
// display:none lost that cascade and the brand link painted over the list
// heading. The served-browser hit test is still required; this pins the rule.
func TestIssue_3607_ListBarHidesBrand(t *testing.T) {
	sheet := chatShellStylesheet()
	hide := `.app-shell:has(.chat-workspace[data-sidebar-open="true"]) .topbar>:not(.notifications,.account-menu){display:none!important}`
	if !strings.Contains(sheet, hide) {
		t.Errorf("the list's bar does not hide the brand cluster with an important declaration:\n%s", chatPhoneShellStylesheet)
	}
}
