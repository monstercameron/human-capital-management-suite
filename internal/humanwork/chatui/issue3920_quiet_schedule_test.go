package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

func issue3920Model(locale string) Model {
	m := chatux007Model(locale)
	m.State = StateReady
	m.Preferences.QuietHours = true
	m.Preferences.QuietTimezone = "UTC"
	m.Preferences.QuietStartMinute, m.Preferences.QuietEndMinute = 22*60, 7*60
	m.QuietSchedule = QuietScheduleView{Loaded: true, Revision: 2}
	m.Callbacks.SaveQuietSchedule = func(QuietScheduleEdit) {}
	return m
}

func issue3920Render(t *testing.T, m Model) string {
	t.Helper()
	return renderNode(t, html.Div(html.Props{}, chatbug045PanelRows(m, handlers{})...))
}

func TestIssue3920_RowIsOfferedOnlyWhereItCanSave(t *testing.T) {
	m := issue3920Model("en-US")
	m.Callbacks.SaveQuietSchedule = nil
	if strings.Contains(issue3920Render(t, m), `data-prefs-section="quiet-schedule"`) {
		t.Fatal("a schedule row with nothing to save it was drawn")
	}
	m = issue3920Model("en-US")
	if !strings.Contains(issue3920Render(t, m), `data-prefs-section="quiet-schedule"`) {
		t.Fatal("the schedule row is missing from Chat preferences")
	}
}

func TestIssue3920_LoadingAndUnavailable(t *testing.T) {
	m := issue3920Model("en-US")
	m.QuietSchedule = QuietScheduleView{}
	got := issue3920Render(t, m)
	if !strings.Contains(got, "Loading your schedule") || strings.Contains(got, "data-quiet-schedule") {
		t.Fatalf("loading state: %s", got)
	}
	m.QuietSchedule = QuietScheduleView{Failed: true}
	got = issue3920Render(t, m)
	if !strings.Contains(got, "could not be loaded") || strings.Contains(got, "data-quiet-schedule") {
		t.Fatalf("unavailable state must not offer controls that do nothing: %s", got)
	}
}

func TestIssue3920_DailyScheduleStaysSimple(t *testing.T) {
	m := issue3920Model("en-US")
	got := issue3920Render(t, m)
	if !strings.Contains(got, "Same every day") || !strings.Contains(got, "Different on each day") {
		t.Fatalf("missing the schedule type choices: %s", got)
	}
	if strings.Contains(got, `quiet-day-`) {
		t.Fatalf("the weekly editor is shown to someone on the daily schedule: %s", got)
	}
	if !strings.Contains(got, `aria-pressed="true"`) {
		t.Fatal("the current schedule type is not marked")
	}
	if !strings.Contains(got, "Required HR and company notices are never held back") {
		t.Fatal("the exception for mandatory notices is not stated")
	}
	if !strings.Contains(got, "daylight-saving") {
		t.Fatal("the zone and daylight-saving note is missing")
	}
}

func TestIssue3920_WeeklyEditorExplainsEveryDay(t *testing.T) {
	m := issue3920Model("en-US")
	m.QuietSchedule.WeekActive = true
	for i := range m.QuietSchedule.Days {
		m.QuietSchedule.Days[i] = QuietDayView{Mode: QuietDayNone}
	}
	m.QuietSchedule.Days[time.Saturday] = QuietDayView{Mode: QuietDayAll}
	m.QuietSchedule.Days[time.Monday] = QuietDayView{Mode: QuietDayInterval, StartMinute: 22 * 60, EndMinute: 7 * 60}
	m.QuietSchedule.Days[time.Tuesday] = QuietDayView{Mode: QuietDayInterval, StartMinute: 12 * 60, EndMinute: 13 * 60}
	got := issue3920Render(t, m)
	for _, want := range []string{
		"Saturday: quiet all day",
		"Monday: quiet from 10:00 PM until 7:00 AM on Tuesday",
		"Tuesday: quiet from 12:00 PM to 1:00 PM",
		"Wednesday: no quiet time",
		`id="quiet-day-1-mode"`, `id="quiet-day-0-start"`, `data-chat-value="22:00"`, "Save schedule",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	// A whole quiet day is said in words, not drawn as two equal clocks (#3701).
	if strings.Contains(got, "Saturday: quiet from") {
		t.Error("an all-day Saturday is explained with times")
	}
	if n := strings.Count(got, `class="quiet-day"`); n != 7 {
		t.Fatalf("%d day rows, want 7", n)
	}
	if strings.Index(got, "quiet-day-1-name") > strings.Index(got, "quiet-day-0-name") {
		t.Error("the week does not start on Monday")
	}
}

func TestIssue3920_StatusSaysWhenNotificationsNextPauseOrResume(t *testing.T) {
	m := issue3920Model("en-US")
	// 2026-03-06 is a Friday.
	m.QuietSchedule.HasNext, m.QuietSchedule.NextQuiet = true, true
	m.QuietSchedule.NextAt = time.Date(2026, 3, 6, 22, 0, 0, 0, time.UTC)
	got := issue3920Render(t, m)
	if !strings.Contains(got, "Notifications pause Friday 10:00 PM.") {
		t.Fatalf("next pause: %s", got)
	}
	m.QuietSchedule.NextQuiet, m.QuietSchedule.QuietNow = false, true
	m.QuietSchedule.NextAt = time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	got = issue3920Render(t, m)
	if !strings.Contains(got, "Quiet now. Notifications resume Sunday 12:00 AM.") {
		t.Fatalf("next resume: %s", got)
	}
	// Quiet hours off: no promise about pausing.
	m.Preferences.QuietHours = false
	got = issue3920Render(t, m)
	if strings.Contains(got, "Notifications resume") {
		t.Fatalf("a switched-off schedule announced a resume: %s", got)
	}
	if !strings.Contains(got, "Turn on Quiet hours to use a schedule") || !strings.Contains(got, "disabled") {
		t.Fatalf("the schedule type is not disabled with a reason: %s", got)
	}
}

func TestIssue3920_PauseAndOverrideAreSeparateWithAnExplicitEnd(t *testing.T) {
	m := issue3920Model("en-US")
	got := issue3920Render(t, m)
	for _, want := range []string{"Pause for 1 hour", "Pause for 4 hours", "Pause for 8 hours", `data-extra="pause:60"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(got, "Notify me anyway") {
		t.Error("the override is offered while nothing is quiet")
	}
	m.QuietSchedule.QuietNow = true
	if got = issue3920Render(t, m); !strings.Contains(got, "Notify me anyway for 1 hour") {
		t.Errorf("no override while quiet: %s", got)
	}
	m.QuietSchedule.PauseKind = QuietPausePause
	m.QuietSchedule.PauseUntil = time.Now().Add(2 * time.Hour)
	got = issue3920Render(t, m)
	if !strings.Contains(got, "Paused until") || !strings.Contains(got, "Back to my schedule") || strings.Contains(got, `data-extra="pause:60"`) {
		t.Fatalf("an active pause: %s", got)
	}
	m.QuietSchedule.PauseKind = QuietPauseNotify
	if got = issue3920Render(t, m); !strings.Contains(got, "Quiet hours are lifted until") {
		t.Fatalf("an active override: %s", got)
	}
}

func TestIssue3920_SaveNotesAreAnnounced(t *testing.T) {
	m := issue3920Model("en-US")
	m.QuietSchedule.WeekActive = true
	for name, tc := range map[string]struct{ note, want, role string }{
		"saved":    {"saved", "Schedule saved", `role="status"`},
		"conflict": {"conflict", "changed somewhere else", `role="alert"`},
		"overlap":  {"overlap", "Wednesday: quiet time starts before the overnight hours", `role="alert"`},
		"empty":    {"empty_interval", "Wednesday: the start and end are the same", `role="alert"`},
		"failed":   {"failed", "could not be saved", `role="alert"`},
	} {
		m.QuietSchedule.Note, m.QuietSchedule.NoteDay = tc.note, int(time.Wednesday)
		got := issue3920Render(t, m)
		i := strings.Index(got, tc.want)
		if i < 0 || !strings.Contains(got[max(0, i-250):i], tc.role) {
			t.Errorf("%s: %q not announced as %s in %s", name, tc.want, tc.role, got)
		}
	}
}

func TestIssue3920_ClicksSaveTheRightChange(t *testing.T) {
	var saved []QuietScheduleEdit
	m := issue3920Model("en-US")
	m.Callbacks.SaveQuietSchedule = func(e QuietScheduleEdit) { saved = append(saved, e) }

	// Switching to different days starts from today's daily interval on every
	// day, so nothing changes until the person edits a day.
	if !issue3920Apply(m, "quiet-week-mode", "weekly") || len(saved) != 1 || !saved[0].WeekActive {
		t.Fatalf("weekly: %+v", saved)
	}
	for i, d := range saved[0].Days {
		if d.Mode != QuietDayInterval || d.StartMinute != 22*60 || d.EndMinute != 7*60 {
			t.Fatalf("day %d starts as %+v, want the daily interval", i, d)
		}
	}
	// A daily interval with equal ends starts every day as a whole quiet day.
	allDay := issue3920Model("en-US")
	allDay.Preferences.QuietStartMinute, allDay.Preferences.QuietEndMinute = 0, 0
	allDay.Callbacks.SaveQuietSchedule = func(e QuietScheduleEdit) { saved = append(saved, e) }
	issue3920Apply(allDay, "quiet-week-mode", "weekly")
	if saved[1].Days[3].Mode != QuietDayAll {
		t.Fatalf("equal start and end started as %+v, want all day", saved[1].Days[3])
	}
	// Choosing the schedule type already in force saves nothing.
	issue3920Apply(m, "quiet-week-mode", "daily")
	if len(saved) != 2 {
		t.Fatalf("an unchanged choice saved: %d", len(saved))
	}
	// Back to the daily schedule keeps the days the person set.
	weekly := issue3920Model("en-US")
	weekly.QuietSchedule.WeekActive = true
	weekly.QuietSchedule.Days[time.Friday] = QuietDayView{Mode: QuietDayAll}
	weekly.Callbacks.SaveQuietSchedule = func(e QuietScheduleEdit) { saved = append(saved, e) }
	issue3920Apply(weekly, "quiet-week-mode", "daily")
	if got := saved[2]; got.WeekActive || got.Days[time.Friday].Mode != QuietDayAll {
		t.Fatalf("daily: %+v", got)
	}
	// A pause carries its length; clearing carries none.
	issue3920Apply(m, "quiet-pause", "pause:240")
	if got := saved[3]; got.PauseKind != QuietPausePause || got.PauseMinutes != 240 {
		t.Fatalf("pause: %+v", got)
	}
	issue3920Apply(m, "quiet-pause", "notify:60")
	if got := saved[4]; got.PauseKind != QuietPauseNotify || got.PauseMinutes != 60 {
		t.Fatalf("override: %+v", got)
	}
	issue3920Apply(m, "quiet-pause", "clear")
	if got := saved[5]; got.PauseKind != "" || got.PauseMinutes != 0 {
		t.Fatalf("clear: %+v", got)
	}
	// Malformed pauses and unknown actions do nothing.
	n := len(saved)
	for _, extra := range []string{"pause:0", "pause:-5", "pause:99999", "forever:60", "pause", "pause:x"} {
		issue3920Apply(m, "quiet-pause", extra)
	}
	if len(saved) != n {
		t.Fatalf("a malformed pause saved: %+v", saved[n:])
	}
	if issue3920Apply(m, "something-else", "weekly") {
		t.Fatal("another feature's click was claimed")
	}
}

func TestIssue3920_CopyInEveryLanguage(t *testing.T) {
	for key := range issue3920Copy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			got := issue3920Copy[locale][key]
			if got == "" || got == issue3920Copy["en-US"][key] {
				t.Errorf("%s has no %s text", key, locale)
			}
		}
	}
	m := issue3920Model("de-DE")
	m.QuietSchedule.WeekActive = true
	got := issue3920Render(t, m)
	if !strings.Contains(got, "Montag") || !strings.Contains(got, "Zeitplan speichern") {
		t.Fatalf("German row: %s", got)
	}
	m = issue3920Model("ar")
	m.QuietSchedule.WeekActive = true
	got = issue3920Render(t, m)
	if !strings.Contains(got, "الاثنين") || !strings.Contains(got, "حفظ الجدول") {
		t.Fatalf("Arabic row: %s", got)
	}
	if !strings.Contains(got, `dir="auto"`) {
		t.Error("day names and sentences do not isolate their direction")
	}
}

func TestIssue3920_StylesUseLogicalPropertiesAndTouchTargets(t *testing.T) {
	for _, bad := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "text-align:left", "text-align:right", " left:", " right:"} {
		if strings.Contains(Issue3920Styles, bad) {
			t.Errorf("styles use a physical property %q", bad)
		}
	}
	if !strings.Contains(Issue3920Styles, "min-block-size:44px") {
		t.Error("controls are not 44px tall")
	}
	if !strings.Contains(Stylesheet, Issue3920Styles) {
		t.Error("the schedule styles are not part of the page stylesheet")
	}
}

// While the weekly schedule is on, the daily interval no longer decides anything,
// so the Quiet hours row must not show it as the person's hours nor offer From
// and Until fields whose edits would silently change nothing.
func TestIssue3920_WeeklyScheduleReplacesTheDailyFields(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := issue3920Model(locale)
		m.QuietSchedule.WeekActive = true
		got := renderNode(t, railPreferences(m, handlers{}))
		for _, id := range []string{`id="quiet-start"`, `id="quiet-end"`} {
			i := strings.Index(got, id)
			if i < 0 {
				t.Fatalf("%s: %s missing", locale, id)
			}
			tag := got[strings.LastIndex(got[:i], "<"):]
			tag = tag[:strings.Index(tag, ">")]
			if !strings.Contains(tag, "disabled") {
				t.Fatalf("%s: the daily %s stays editable although the weekly schedule decides: %s", locale, id, tag)
			}
		}
		if !strings.Contains(got, issue3920Text(m, k3920DailyReplaced)) {
			t.Fatalf("%s: the row does not say the weekly schedule sets the hours: %s", locale, got)
		}
		if value := chatux002QuietValue(m); value != issue3920Text(m, k3920Weekly) {
			t.Fatalf("%s: the heading value shows %q, not the weekly schedule", locale, value)
		}
		if issue3920Text(m, k3920DailyReplaced) == issue3920Copy["en-US"][k3920DailyReplaced] && locale != "en-US" {
			t.Fatalf("%s: no translation", locale)
		}
	}
	m := issue3920Model("en-US")
	if value := chatux002QuietValue(m); value != "10:00 PM–7:00 AM" && !strings.Contains(value, "–") {
		t.Fatalf("the daily value changed for someone on the daily schedule: %q", value)
	}
}
