package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATREVIEW-001: run states arrive in mixed case (the streak detector and
// the state label both match case-insensitively), but the outcome cell
// compared exact uppercase, so a "failed" run kept the neutral tone and hid
// its reason, and a "completed" run lost its success tone.
func TestChatReview001_RunOutcomeMatchesStateCaseInsensitively(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	failed, err := ui.RenderToString(agentRunOutcome(locale, AgentControlRun{ID: "run-1", Name: "Policy Helper", State: "failed"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(failed, `data-tone="danger"`) {
		t.Fatalf("lowercase failed run lost its danger tone: %s", failed)
	}
	if !strings.Contains(failed, "agent-run-failure") {
		t.Fatalf("lowercase failed run hid its reason: %s", failed)
	}
	completed, err := ui.RenderToString(agentRunOutcome(locale, AgentControlRun{ID: "run-2", Name: "Policy Helper", State: "completed"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(completed, `data-tone="success"`) {
		t.Fatalf("lowercase completed run lost its success tone: %s", completed)
	}
	upper, err := ui.RenderToString(agentRunOutcome(locale, AgentControlRun{ID: "run-3", Name: "Policy Helper", State: "FAILED"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(upper, `data-tone="danger"`) {
		t.Fatalf("uppercase failed run changed: %s", upper)
	}
}

// CHATREVIEW-001: the run-history pagination landmark names the same count
// as its heading, which already uses the reader's digits; the landmark
// formatted it with the default digits instead.
func TestChatReview001_HistoryPaginationLabelUsesLocaleDigits(t *testing.T) {
	runs := make([]AgentControlRun, 0, 5)
	for i := 0; i < 5; i++ {
		runs = append(runs, AgentControlRun{ID: "run-" + string(rune('a'+i)), Name: "Policy Helper", State: "COMPLETED"})
	}
	locale := ResolveProductLocale("ar")
	markup, err := ui.RenderToString(RenderAgentRunHistory(locale, runs, AgentRunHistoryFilter{}))
	if err != nil {
		t.Fatal(err)
	}
	nav := strings.Index(markup, "agent-history-pagination")
	if nav < 0 {
		t.Fatalf("pagination landmark missing: %s", markup)
	}
	open := strings.LastIndex(markup[:nav], "<nav")
	end := strings.Index(markup[nav:], ">")
	if open < 0 || end < 0 {
		t.Fatalf("cannot isolate pagination landmark: %s", markup)
	}
	if tag := markup[open : nav+end+1]; !strings.Contains(tag, "٥") {
		t.Fatalf("pagination landmark count not localized: %s", tag)
	}
}

// CHATREVIEW-001: a collapsed disclosure chevron points forward: right in
// left-to-right locales, left in right-to-left ones. The posting history
// already flips it per locale; the run history, the run card and the
// technical details hardcoded the left-to-right glyph.
func TestChatReview001_DisclosureChevronFollowsDirection(t *testing.T) {
	run := AgentControlRun{ID: "run-1", Name: "Policy Helper", Version: "2", State: "COMPLETED", Started: "2026-10-01T10:00:00Z"}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		want := "›"
		if localeID == "ar" {
			want = "‹"
		}
		card, err := ui.RenderToString(agentUXR7RecentRunCard(locale, run))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(card, `data-chevron="`+want+`"`) {
			t.Fatalf("%s run card chevron wrong: %s", localeID, card)
		}
		details, err := ui.RenderToString(agentControlTechnicalDetails(locale, "run-1", []string{"state"}, []string{"COMPLETED"}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(details, `data-chevron="`+want+`"`) {
			t.Fatalf("%s technical details chevron wrong: %s", localeID, details)
		}
		history, err := ui.RenderToString(RenderAgentRunHistory(locale, []AgentControlRun{run}, AgentRunHistoryFilter{}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(history, `data-chevron="`+want+`"`) {
			t.Fatalf("%s run history chevron wrong: %s", localeID, history)
		}
	}
}

// CHATREVIEW-001: a posting attempt whose timestamp is neither a supplied
// label nor RFC-3339 still shows its stored value; the timeline rendered an
// empty time element instead.
func TestChatReview001_AttemptTimeFallsBackToStoredValue(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup, err := ui.RenderToString(renderAnnouncementAttempts(locale, []AgentAnnouncementAttempt{{At: "2026-10-01", ResultCode: "POSTED"}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, ">2026-10-01<") {
		t.Fatalf("attempt without a parseable time shows nothing: %s", markup)
	}
}
