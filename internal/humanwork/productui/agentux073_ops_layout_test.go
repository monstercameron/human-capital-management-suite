package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

func agentUX073Snapshot() AgentControlsSnapshot {
	star := agenticon.Generate(agenticon.Input{Name: "Assistant"})
	return AgentControlsSnapshot{
		Available: true, UpdatedAt: "2026-10-02T02:05:00Z", AllowedCommands: []string{"SUSPEND", "PUBLISH"},
		Agents: []PersonaAdminPersona{
			{ID: "assistant", Name: "Assistant", Version: "3", Lifecycle: PersonaPublished, Icon: star},
			{ID: "policy-helper", Name: "Policy Helper", Version: "6", Lifecycle: PersonaSuspended},
			{ID: "draft-agent", Name: "Draft Agent", Version: "1", Lifecycle: PersonaDraft},
		},
		Runs: []AgentControlRun{
			{ID: "run-failed", AgentID: "assistant", Name: "Assistant", Version: "3", State: "FAILED", Started: "2026-10-02T01:00:00Z", Duration: "1h0m0s", RequestedBy: "Walt Brennan", Location: "#general", Failure: "MODEL_UNAVAILABLE"},
			{ID: "run-done", AgentID: "policy-helper", Name: "Policy Helper", Version: "6", State: "COMPLETED", Started: "2026-10-02T01:30:00Z", Duration: "12s", RequestedBy: "Walt Brennan", Location: "#general"},
		},
	}
}

func TestTodo_AGENTUX_073(t *testing.T) {
	locale := ResolveProductLocale("en-US").WithTimeZone("America/New_York")
	snapshot := agentUX073Snapshot()
	activity := agentUXR7Render(t, RenderAgentControls(locale, snapshot, "done"))

	// Agents are rows of icon, name, state and the pause control.
	if strings.Count(activity, `class="agent-owner-pause-row"`) != 2 || strings.Contains(activity, "Draft Agent") {
		t.Fatalf("Activity must list the live and the paused agent as rows and leave drafts to Agent setup: %s", activity)
	}
	for _, id := range []string{"assistant", "policy-helper"} {
		start := strings.Index(activity, `data-persona-id="`+id+`"`)
		if start < 0 {
			t.Fatalf("no row for %s", id)
		}
		row := activity[start:]
		row = row[:strings.Index(row, "</li>")]
		for _, want := range []string{`class="agent-activity-icon"`, "<svg", `class="agent-activity-identity"`, `class="status agent-activity-state"`, `data-persona-command=`} {
			if !strings.Contains(row, want) {
				t.Fatalf("row for %s is missing %q: %s", id, want, row)
			}
		}
	}
	for _, want := range []string{">Answering<", ">Paused<", ">Pause agent<", ">Resume agent<", ">Version 3<", ">Version 6<"} {
		if !strings.Contains(activity, want) {
			t.Fatalf("Activity is missing %q", want)
		}
	}
	if stored := agentUXR7Render(t, agenticon.Node(snapshot.Agents[0].Icon)); !strings.Contains(activity, stored) {
		t.Fatal("the Assistant row does not draw the agent's stored icon")
	}

	// The three filters carry three different labels, on one line.
	history := activity[strings.Index(activity, `id="agent-run-history"`):]
	for _, label := range []string{`<label for="agent-history-agent">Agent</label>`, `<label for="agent-history-version">Version</label>`, `<label for="agent-history-outcome">Outcome</label>`} {
		if !strings.Contains(history, label) {
			t.Fatalf("run filters are missing the label %s", label)
		}
	}
	if strings.Contains(history[:strings.Index(history, "<table")], "Agent and version") {
		t.Fatal("the agent filter still carries the table column's label")
	}

	// A failed run says how long it ran before it failed, in grammatical words.
	if !strings.Contains(history, "<td>Failed after 1 hour</td>") || strings.Contains(history, "1 hours") {
		t.Fatalf("failed run duration is not stated as time before failure: %s", history)
	}
	if !strings.Contains(history, "<td>12 seconds</td>") {
		t.Fatalf("completed run duration is wrong: %s", history)
	}

	// An announcement is one card: heading, instruction, two labelled rows,
	// history behind a disclosure, and Delete apart from the other actions.
	row := AgentAnnouncementRow{
		ID: "holiday", AgentName: "Assistant", ConversationName: "general", OwnerName: "Walt Brennan", State: "ACTIVE",
		Instruction: "Tell employees which company holidays are coming up.", ResultCode: "POSTED", MessageHref: "/workspace/app/chat?conversation=general&message=m1",
		LastRun: "Oct 1, 2026, 9:00 AM", LastRunAt: "2026-10-01T13:00:00Z", NextRun: "Oct 8, 2026, 9:00 AM", NextRunAt: "2026-10-08T13:00:00Z",
		Editor:   AgentAnnouncementEditorValue{Cadence: "WEEKLY", Time: "09:00", Zone: "America/New_York", Weekdays: []int{4}},
		Attempts: []AgentAnnouncementAttempt{{At: "2026-10-01T13:00:00Z", TimeLabel: "Oct 1, 2026, 9:00 AM", ResultCode: "POSTED"}},
	}
	announcements := agentUXR7Render(t, RenderAgentAnnouncements(locale, AgentAnnouncementsSnapshot{Available: true, CanCreate: true, Rows: []AgentAnnouncementRow{row}}))
	if strings.Contains(announcements, "<style") {
		t.Fatal("Announcements emits a style element the content security policy refuses")
	}
	order := []string{`<h3 dir="auto">Assistant · general</h3>`, "Set by Walt Brennan", `class="agent-announcement-instruction"`, `<dl class="agent-announcement-facts">`, "<dt>Schedule</dt>", "Every Thursday at 09:00 (America/New_York)", "Next post: Oct 8, 2026, 9:00 AM", "<dt>Last result</dt>", "Posted. Open the message.", `<details class="agent-announcement-history"`, `class="agent-announcement-row-actions"`, `data-announcement-action="pause"`, `data-announcement-action="post"`, `data-announcement-action="edit"`, `agent-announcement-delete-action`}
	at := 0
	for _, want := range order {
		next := strings.Index(announcements[at:], want)
		if next < 0 {
			t.Fatalf("announcement card is missing %q after offset %d: %s", want, at, announcements)
		}
		at += next
	}
	if strings.Count(announcements, "<dt>") != 2 {
		t.Fatalf("an announcement shows schedule and last result as two labelled rows: %s", announcements)
	}
}

// A run that failed at once reads as a sentence, with no capital mid-sentence.
func TestTodo_AGENTUX_073_FailedAtOnce(t *testing.T) {
	for language, want := range map[string]string{
		"en-US": "Failed in under a second", "de-DE": "In unter einer Sekunde fehlgeschlagen", "ar": "فشل في أقل من ثانية",
	} {
		locale := ResolveProductLocale(language)
		for _, raw := range []string{"0s", "400ms"} {
			if got := agentUX073RunDuration(locale, AgentControlRun{State: "FAILED", Duration: raw}); got != want {
				t.Errorf("%s: a run that failed in %s reads %q, want %q", language, raw, got, want)
			}
		}
		// A run that worked for a while keeps "after", and one that completed
		// at once is still "Under a second" on its own.
		after := agentUX073RunDuration(locale, AgentControlRun{State: "FAILED", Duration: "2m0s"})
		if after == want || !strings.Contains(after, agentUX073Unit(locale, "minute", 2)) {
			t.Errorf("%s: a run that failed after two minutes reads %q", language, after)
		}
		if got := agentUX073RunDuration(locale, AgentControlRun{State: "COMPLETED", Duration: "0s"}); got != agentUXR7Text(locale, "under_second") {
			t.Errorf("%s: a run that completed at once reads %q", language, got)
		}
	}
	history := agentUXR7Render(t, RenderAgentRunHistory(ResolveProductLocale("en-US"), []AgentControlRun{{ID: "r", Name: "Policy Helper", Version: "6", State: "FAILED", Started: "2026-10-01T12:26:00Z", Duration: "0s"}}, AgentRunHistoryFilter{Page: 1}))
	if !strings.Contains(history, "<td>Failed in under a second</td>") || strings.Contains(history, "after Under") {
		t.Fatalf("the run table still reads a capital mid-sentence: %s", history)
	}
}

// With no agent rows drawn, Activity does not tell the reader to pause an
// agent "above"; when the agents could not be listed it says so.
func TestTodo_AGENTUX_073_HintMatchesWhatIsDrawn(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		withRows := agentUXR7Render(t, RenderAgentControls(locale, agentUX073Snapshot(), "done"))
		if !strings.Contains(withRows, agentControlsText(locale, "empty_help")) {
			t.Fatalf("%s: with agent rows the hint does not say where to pause", language)
		}
		none := agentUX073Snapshot()
		none.Agents = nil
		noRows := agentUXR7Render(t, RenderAgentControls(locale, none, "done"))
		if strings.Contains(noRows, agentControlsText(locale, "empty_help")) || !strings.Contains(noRows, agentUX073Text(locale, "empty_help_runs")) || strings.Contains(noRows, `class="agent-owner-pause-row"`) {
			t.Fatalf("%s: with no agent rows the hint still points above: %s", language, noRows)
		}
		none.AgentsUnavailable = true
		unavailable := agentUXR7Render(t, RenderAgentControls(locale, none, "done"))
		if !strings.Contains(unavailable, agentUX073Text(locale, "agents_unavailable")) || !strings.Contains(unavailable, `href="`+agentSetupHref(locale)+`"`) {
			t.Fatalf("%s: a failed read of the agents is not explained with a way to pause: %s", language, unavailable)
		}
	}
}

func TestTodo_AGENTUX_073_DurationsAreGrammatical(t *testing.T) {
	cases := []struct {
		duration   time.Duration
		en, de, ar string
	}{
		{500 * time.Millisecond, "Under a second", "Unter einer Sekunde", "أقل من ثانية"},
		{time.Second, "1 second", "1 Sekunde", "ثانية واحدة"},
		{12 * time.Second, "12 seconds", "12 Sekunden", "١٢ ثانية"},
		{time.Minute, "1 minute", "1 Minute", "دقيقة واحدة"},
		{2 * time.Minute, "2 minutes", "2 Minuten", "دقيقتان"},
		{45 * time.Minute, "45 minutes", "45 Minuten", "٤٥ دقيقة"},
		{59*time.Minute + 40*time.Second, "1 hour", "1 Stunde", "ساعة واحدة"},
		{time.Hour, "1 hour", "1 Stunde", "ساعة واحدة"},
		{time.Hour + 5*time.Minute, "1 hour 5 minutes", "1 Stunde 5 Minuten", "ساعة واحدة و٥ دقائق"},
		{3 * time.Hour, "3 hours", "3 Stunden", "٣ ساعات"},
	}
	for _, tc := range cases {
		for language, want := range map[string]string{"en-US": tc.en, "de-DE": tc.de, "ar": tc.ar} {
			if got := agentUX073Duration(ResolveProductLocale(language), tc.duration); got != want {
				t.Errorf("%s %s = %q, want %q", language, tc.duration, got, want)
			}
		}
	}
}

func TestTodo_AGENTUX_073_Localized(t *testing.T) {
	for _, language := range []string{"de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		activity := agentUXR7Render(t, RenderAgentControls(locale, agentUX073Snapshot(), "done"))
		for _, key := range []string{"agents", "state_answering", "state_paused", "filter_agent"} {
			text := agentUX073Text(locale, key)
			if text == "" || !strings.Contains(activity, ">"+text+"<") {
				t.Fatalf("%s Activity is missing %s (%q)", language, key, text)
			}
		}
		for _, english := range []string{">Answering<", "Failed after", ">Agents<"} {
			if strings.Contains(activity, english) {
				t.Fatalf("%s Activity shows English %q", language, english)
			}
		}
		for _, cadence := range []AgentAnnouncementEditorValue{{Cadence: "DAILY", Time: "09:00"}, {Cadence: "WEEKLY", Time: "09:00", Weekdays: []int{1, 3}}, {Cadence: "MONTHLY", Time: "09:00", MonthDay: 15}, {Cadence: "NOW"}} {
			if text := agentUX073AnnouncementCadence(locale, cadence); text == "" || strings.Contains(text, "{") || strings.Contains(text, "Every") {
				t.Fatalf("%s cadence %s = %q", language, cadence.Cadence, text)
			}
		}
	}
}

// The layout holds only when the browser accepts the rules. The product page's
// content security policy admits one stylesheet by hash, so this asserts the
// rules are in that sheet and that neither page emits a style element of its own.
func TestTodo_AGENTUX_073_Browser(t *testing.T) {
	sheet := Stylesheet()
	for _, want := range []string{
		agentUXR7Stylesheet(), AgentAnnouncementsStyles, agentUX073LayoutStyles,
		`.agent-activity-agents .agent-owner-pause-row{display:grid;grid-template-columns:auto minmax(0,1fr) auto auto;align-items:center`,
		`.agent-history-filters{display:flex;flex-wrap:wrap;align-items:end`,
		`.agent-history-filters .persona-admin-editor-field{flex:0 1 14rem`,
		`.agent-announcement-row dl.agent-announcement-facts{display:grid;grid-template-columns:max-content minmax(0,1fr)`,
		`.agent-announcement-row-actions .agent-announcement-delete-action{margin-inline-start:auto}`,
		`.agent-announcement-row-actions{align-items:center;gap:var(--hcm-space-2,.5rem)`,
		`max-inline-size:72ch`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("the product stylesheet is missing %.80q", want)
		}
	}
	for _, tab := range []string{"running", "announcements"} {
		view := ApplyLocale(NewView(PageAgentOperations, "tenant", "owner", ""), ResolveProductLocale("en-US"))
		view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
		view.Query = "tab=" + tab
		if page := agentUXR7Render(t, BuildAgentOperationsPage(view)); strings.Contains(page, "<style") {
			t.Fatalf("Agent operations (%s) emits a style element the content security policy refuses", tab)
		}
	}
	client := agentUXR7Render(t, RenderAgentAnnouncements(ResolveProductLocale("en-US"), AgentAnnouncementsSnapshot{Loading: true}))
	if strings.Contains(client, "<style") {
		t.Fatal("the announcements client render emits a style element")
	}
}
