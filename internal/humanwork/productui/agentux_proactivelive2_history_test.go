package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"strings"
	"testing"
)

func TestAgentUXProactiveLive_History_Browser(t *testing.T) {
	reason := "The service is busy or unavailable. Try again in a few minutes."
	attempts := []AgentAnnouncementAttempt{{At: "2026-10-01T20:00:00Z", ResultCode: "FAILED", Reason: reason}, {At: "2026-10-01T20:01:00Z", ResultCode: "REFUSED", Reason: "1 of the documents cannot be read by everyone in #general"}, {At: "2026-10-01T20:02:00Z", ResultCode: "POSTED", MessageHref: "/workspace/app/chat?conversation=general&message=posted"}}
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		markup, err := ui.RenderToString(RenderAgentAnnouncements(locale, AgentAnnouncementsSnapshot{Available: true, Rows: []AgentAnnouncementRow{{ID: "one-definition", AgentName: "Assistant", ConversationName: "general", OwnerName: "Walt Brennan", Instruction: "Announce holidays", State: "ACTIVE", Revision: 1, ResultCode: "POSTED", Attempts: attempts}}}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(markup, `class="agent-announcement-row"`) != 1 || strings.Count(markup, `<li>`) != 3 || !strings.Contains(markup, `<details class="agent-announcement-history"`) || !strings.Contains(markup, `href="/workspace/app/chat?conversation=general&amp;message=posted"`) {
			t.Fatalf("%s split attempts into definitions: %s", language, markup)
		}
		title := [3]string{"View posting history", "Veröffentlichungsverlauf anzeigen", "عرض سجل النشر"}[agentRPLocaleIndex(locale)]
		if !strings.Contains(markup, title) || strings.Contains(markup, "???") {
			t.Fatalf("history title corrupted: %s", markup)
		}
		if language != "en-US" && strings.Contains(markup, reason) {
			t.Fatalf("%s reason not localized: %s", language, markup)
		}
		for _, reason := range []string{"The channel is locked or your posting access changed. Ask a channel manager to allow posting, then try again.", "The agent is paused or its access changed. Resume the agent and check its channel and document access, then preview again.", "The preview expired or its documents or authority changed. Preview again before posting.", "The documents or posting authority changed. Preview again before posting.", "Posting is temporarily unavailable. Try again in a few minutes; contact a workspace administrator if it continues.", "The agent's corrected reply still broke this rule: use plain text without web addresses, square brackets or Source lines; the product adds sources. Preview again to try a new reply."} {
			localized := AgentAnnouncementResultText(locale, "FAILED", reason)
			if localized == "" || language != "en-US" && strings.Contains(localized, reason) {
				t.Fatalf("%s missing reason translation %s", language, localized)
			}
		}
	}
	markup, err := ui.RenderToString(RenderAgentAnnouncements(ResolveProductLocale("en-US"), AgentAnnouncementsSnapshot{Available: true, Rows: []AgentAnnouncementRow{{ID: "expired", Revision: 1, ResultCode: "REFUSED", Reason: "The preview expired or its documents or authority changed. Preview again before posting."}}}))
	if err != nil || !strings.Contains(markup, `data-announcement-action="preview-again"`) || !strings.Contains(markup, "Preview again") {
		t.Fatalf("expired preview has no next action: %s %v", markup, err)
	}
	row := AgentAnnouncementRow{ResultCode: "FAILED", LastOccurrence: "previous-attempt"}
	if announcementRetryOccurrence(row) != "previous-attempt" || announcementRetryOccurrence(AgentAnnouncementRow{ResultCode: "POSTED", LastOccurrence: "previous-attempt"}) != "" {
		t.Fatal("Try again lost original attempt binding")
	}
}
