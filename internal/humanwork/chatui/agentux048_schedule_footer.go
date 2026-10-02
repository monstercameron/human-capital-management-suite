package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentux048ScheduleFooterText says who set the schedule an announcement was
// posted on. The name is substituted into the sentence, never joined to it, so
// right-to-left text keeps its order.
func agentux048ScheduleFooterText(locale, owner string) string {
	sentence := "Posted on a schedule set by {owner}"
	switch {
	case strings.HasPrefix(locale, "de"):
		sentence = "Nach einem Zeitplan von {owner} veröffentlicht"
	case strings.HasPrefix(locale, "ar"):
		sentence = "نُشر وفق جدول أعدّه {owner}"
	}
	return strings.ReplaceAll(sentence, "{owner}", strings.TrimSpace(owner))
}

// agentux048ScheduleFooter is the quiet line under a scheduled announcement. A
// post made at once by its owner, and one without a known owner, carry none.
func agentux048ScheduleFooter(model Model, message AgentAnnouncementMessage) []ui.Node {
	if !message.Scheduled || strings.TrimSpace(message.OwnerName) == "" {
		return nil
	}
	return []ui.Node{html.P(html.Props{Class: "muted agent-announcement-footer", Dir: "auto", Raw: map[string]any{"data-agent-announcement-footer": "scheduled"}}, ui.Text(agentux048ScheduleFooterText(model.Locale, message.OwnerName)))}
}
