//go:build !(js && wasm)

package chatui

import (
	"strings"
	"testing"
)

func issue3994Render(t *testing.T, m Model, stage, tone string, waits bool) string {
	t.Helper()
	return chatDesignRender(t, journeyPreviewCard(m, JourneyPreview{IntentID: "intent-1", Readable: true, State: "ready", Workflow: "Promotion", Worker: "Sam Lee", Stage: stage, StageTone: tone, Approver: "Dana Reyes", WaitsOnViewer: waits}))
}

// A card never claims "Waiting on you" from a display name: a same-named
// person without the resolver's identity answer sees an informational card.
func TestIssue3994_SameNameWithoutIdentityAnswerIsInformational(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale, CurrentUser: "u-other", CurrentUserName: "Dana Reyes"}
		if got := issue3994Render(t, m, "JOURNEY_STAGE_MANAGER_APPROVAL", "active", false); strings.Contains(got, "data-waits") || strings.Contains(got, workCardWaitingText(locale)) {
			t.Errorf("%s journey: same display name alone claims the card waits on the reader: %s", locale, got)
		}
		ticket := chatDesignRender(t, projectPreviewCard(m, ProjectTaskPreview{ProjectID: "p", TaskID: "t", Title: "Fix", Readable: true, State: "ready", Status: "Doing", StatusTone: "doing", Assignee: "Dana Reyes"}))
		if strings.Contains(ticket, "data-waits") || strings.Contains(ticket, workCardWaitingText(locale)) {
			t.Errorf("%s ticket: same display name alone claims the card waits on the reader: %s", locale, ticket)
		}
	}
}

// A renamed reader still gets the claim: it rests on the resolver's answer.
func TestIssue3994_RenamedViewerStillWaiting(t *testing.T) {
	m := Model{Locale: "en-US", CurrentUser: "u-me", CurrentUserName: "Dana Smith"}
	if got := issue3994Render(t, m, "JOURNEY_STAGE_MANAGER_APPROVAL", "active", true); !strings.Contains(got, `data-waits="you"`) || !strings.Contains(got, "Waiting on you") {
		t.Errorf("a resolver-confirmed approval is not marked: %s", got)
	}
}

// The flag is not enough in stages that ask the approver for nothing, and
// never on a finished card.
func TestIssue3994_NonActionableStagesStayInformational(t *testing.T) {
	m := Model{Locale: "en-US", CurrentUser: "u-me"}
	for stage, tone := range map[string]string{
		"JOURNEY_STAGE_BLOCKED":                "blocked",
		"JOURNEY_STAGE_WAITING_EFFECTIVE_DATE": "active",
		"JOURNEY_STAGE_REVALIDATION":           "active",
		"JOURNEY_STAGE_COMPLETED":              "done",
		"JOURNEY_STAGE_REJECTED":               "done",
		"":                                     "active",
	} {
		if got := issue3994Render(t, m, stage, tone, true); strings.Contains(got, "data-waits") || strings.Contains(got, "Waiting on you") {
			t.Errorf("stage %q claims the card waits on the reader: %s", stage, got)
		}
	}
	for _, stage := range []string{"JOURNEY_STAGE_AWAITING_APPROVAL", "JOURNEY_STAGE_FINANCE_APPROVAL", "JOURNEY_STAGE_MANAGER_APPROVAL"} {
		if got := issue3994Render(t, m, stage, "active", true); !strings.Contains(got, `data-waits="you"`) {
			t.Errorf("stage %s with a confirmed approval is not marked", stage)
		}
	}
	done := chatDesignRender(t, projectPreviewCard(m, ProjectTaskPreview{ProjectID: "p", TaskID: "t", Title: "Fix", Readable: true, State: "ready", StatusTone: "done", Assignee: "Dana", WaitsOnViewer: true}))
	if strings.Contains(done, "data-waits") {
		t.Errorf("a finished ticket claims it waits on the reader: %s", done)
	}
}

// The resolver compares stable subjects, so equal names with different ids do
// not match, and an unavailable assignment never does.
func TestIssue3994_ResolverComparesStableIdentity(t *testing.T) {
	const awaiting = "JOURNEY_STAGE_MANAGER_APPROVAL"
	cases := []struct {
		name string
		got  bool
		want bool
	}{
		{"journey same subject", JourneyWaitsOnSubject(awaiting, "mgr-1", "mgr-1"), true},
		{"journey other subject", JourneyWaitsOnSubject(awaiting, "mgr-2", "mgr-1"), false},
		{"journey no approver", JourneyWaitsOnSubject(awaiting, "", ""), false},
		{"journey waiting effective date", JourneyWaitsOnSubject("JOURNEY_STAGE_WAITING_EFFECTIVE_DATE", "mgr-1", "mgr-1"), false},
		{"journey revalidation", JourneyWaitsOnSubject("JOURNEY_STAGE_REVALIDATION", "mgr-1", "mgr-1"), false},
		{"journey rejected", JourneyWaitsOnSubject("JOURNEY_STAGE_REJECTED", "mgr-1", "mgr-1"), false},
		{"task same id", TaskWaitsOnSubject("doing", "u-1", "u-1"), true},
		{"task other id", TaskWaitsOnSubject("doing", "u-2", "u-1"), false},
		{"task unassigned", TaskWaitsOnSubject("doing", "", ""), false},
		{"task done", TaskWaitsOnSubject("done", "u-1", "u-1"), false},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}
