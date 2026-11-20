package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// Issue 3882: every Cancel in the Manage channel editors names, through its own
// row id and state, a control that exists on the page, and that is the control
// that opened the editor.
func TestIssue3882_CancelReturnsFocusToItsOpener(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chatux021Model()
		m.Locale = locale
		m.ChannelTeam.Members = append(m.ChannelTeam.Members, ChannelTeamMember{HomeTenantID: "t", SubjectID: "loretta"})
		page := chatux027Manage(t, m, "status", "project")
		var local localUI
		local.setDetailGroup(chatux005GroupManage, true)
		local.toggleSection(chatux027ManageScope(m), "project", false)
		local.toggleSection(chatux027ManageScope(m)+"/add", "milestone", false)
		built := channelWidgetPartsFor(m, handlers{local: local})
		withAdder := renderNode(t, chatux005Manage(m, handlers{local: local}, m.selected(), &built))
		cancel := regexp.MustCompile(`<button[^>]*data-action="manage-section"[^>]*>` + regexp.QuoteMeta(m.t(KeyCancel)) + `</button>`)
		seen := map[string]bool{}
		for _, markup := range []string{page, withAdder} {
			for _, button := range cancel.FindAllString(markup, -1) {
				id := regexp.MustCompile(`data-id="([^"]+)"`).FindStringSubmatch(button)
				state := regexp.MustCompile(`data-extra="[^"]*:(\d)"`).FindStringSubmatch(button)
				if id == nil || state == nil {
					t.Fatalf("%s: a Cancel does not name its row: %s", locale, button)
				}
				target := issue3882ReturnFocusID(id[1], state[1])
				if target == "" {
					t.Errorf("%s: Cancel in %s returns focus nowhere", locale, id[1])
					continue
				}
				seen[id[1]] = true
				if !strings.Contains(markup, `id="`+target+`"`) {
					t.Errorf("%s: Cancel in %s returns focus to %q, which is not on the page", locale, id[1], target)
				}
			}
		}
		for _, row := range []string{"status", "project", "milestone"} {
			if !seen[row] {
				t.Errorf("%s: no Cancel found for %s", locale, row)
			}
		}
	}
}

func TestIssue3882_OpeningARowMovesNoFocus(t *testing.T) {
	for _, id := range []string{"status", "project", "milestone"} {
		if got := issue3882ReturnFocusID(id, "0"); got != "" {
			t.Errorf("opening %s asks to move focus to %q", id, got)
		}
	}
	if got := issue3882ReturnFocusID("project", "1"); got != "chat-manage-project-row" {
		t.Errorf("project returns to %q", got)
	}
	if got := issue3882ReturnFocusID("milestone", "1"); got != "chat-manage-milestone-toggle" {
		t.Errorf("milestone returns to %q", got)
	}
}
