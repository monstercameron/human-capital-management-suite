package chatui

import (
	"strings"
	"testing"
	"time"
)

func issue3788Model(locale string) Model {
	m := chatux007Model(locale)
	m.State = StateReady
	m.Callbacks.LoadScheduled = func() {}
	m.Callbacks.ScheduleSend = func(string, string, time.Time, func(SendLaterResult)) {}
	m.Callbacks.CancelScheduled = func(string) {}
	m.Chatcmd002TimeZone = "Europe/Berlin"
	m.SendLater = SendLaterState{Loaded: true, Rows: []ScheduledRow{
		{ID: "s1", ConversationID: "general", Channel: "general", InChannel: true, Body: "Reminder: compliance training is tomorrow at ten.", SendAt: time.Date(2026, 3, 4, 7, 30, 0, 0, time.UTC), State: "pending", Revision: 1, QuietPeople: 1},
		{ID: "s2", ConversationID: "unread", Channel: "Priya", Body: "Welcome aboard", SendAt: time.Date(2026, 3, 5, 7, 30, 0, 0, time.UTC), State: "failed", Refusal: "channel_closed", Revision: 3},
		{ID: "s3", ConversationID: "gone", Body: "Secret plans", SendAt: time.Date(2026, 3, 6, 7, 30, 0, 0, time.UTC), State: "failed", Refusal: "content_blocked", Revision: 2},
	}}
	return m
}

func TestIssue3788_TrayControlIsADisclosureWithTheCount(t *testing.T) {
	markup := renderNode(t, rail(issue3788Model("en-US"), handlers{}))
	for _, want := range []string{`data-action="scheduled-toggle"`, `aria-expanded="false"`, `aria-controls="issue3788-panel-tray"`, "Scheduled, 3 scheduled"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, `id="issue3788-panel-tray"`) {
		t.Fatal("tray drawn while closed")
	}
	// A page that cannot reach the server draws no control at all, never a dead one.
	m := issue3788Model("en-US")
	m.Callbacks.LoadScheduled = nil
	if markup = renderNode(t, rail(m, handlers{local: localUI{scheduledOpen: true}})); strings.Contains(markup, "issue3788") {
		t.Fatalf("a control with nothing behind it: %s", markup)
	}
}

func TestIssue3788_TrayListsEachMessageWithItsTimeAndActions(t *testing.T) {
	markup := renderNode(t, rail(issue3788Model("en-US"), handlers{local: localUI{scheduledOpen: true}}))
	for _, want := range []string{`id="issue3788-panel-tray"`, "Scheduled messages", "Only you can see these",
		"To #general", "Reminder: compliance training is tomorrow at ten.", "Sends 2026-03-04 08:30 UTC+1",
		"At least one person you are writing to has quiet hours then.",
		`data-action="scheduled-edit"`, `data-action="scheduled-cancel"`, `data-id="s1"`,
		"Cancel and delete: Reminder: compliance training is tomorrow at ten.",
		"Scheduled messages: 3"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	// Only a pending row can be cancelled with that word; a refused one is discarded.
	if got := strings.Count(markup, "Discard: "); got != 2 {
		t.Fatalf("discard buttons = %d", got)
	}
}

func TestIssue3788_RefusalIsPlainWordsAndLeaksNothing_Security(t *testing.T) {
	markup := renderNode(t, rail(issue3788Model("en-US"), handlers{local: localUI{scheduledOpen: true}}))
	for _, want := range []string{"Not sent", "This channel is not accepting messages right now. Nothing was sent.",
		"A workspace rule held this message back. Nothing was sent.", "To a conversation"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	for _, leak := range []string{"channel_closed", "content_blocked", "no_access", "gone</", "chat.issue3788"} {
		if strings.Contains(markup, leak) {
			t.Fatalf("%q leaked into %s", leak, markup)
		}
	}
	// A refused message offers no quiet-hours advice.
	m := issue3788Model("en-US")
	m.SendLater.Rows = m.SendLater.Rows[1:2]
	m.SendLater.Rows[0].QuietPeople = 2
	if got := renderNode(t, rail(m, handlers{local: localUI{scheduledOpen: true}})); strings.Contains(got, "quiet hours") {
		t.Fatalf("quiet-hours advice on a refused message: %s", got)
	}
}

func TestIssue3788_TrayStates(t *testing.T) {
	open := handlers{local: localUI{scheduledOpen: true}}
	m := issue3788Model("en-US")
	m.SendLater = SendLaterState{Loaded: true}
	if markup := renderNode(t, rail(m, open)); !strings.Contains(markup, "Nothing is scheduled.") || strings.Contains(markup, "issue3788-count") {
		t.Fatalf("empty: %s", markup)
	}
	m.SendLater = SendLaterState{Loading: true}
	if markup := renderNode(t, rail(m, open)); !strings.Contains(markup, "Loading your scheduled messages") || strings.Contains(markup, "Nothing is scheduled.") {
		t.Fatalf("loading must not read as empty: %s", markup)
	}
	m.SendLater = SendLaterState{Fail: true}
	markup := renderNode(t, rail(m, open))
	if !strings.Contains(markup, "could not be loaded") || !strings.Contains(markup, `data-action="scheduled-retry"`) || strings.Contains(markup, "Nothing is scheduled.") || strings.Contains(markup, " scheduled</") {
		t.Fatalf("unavailable: %s", markup)
	}
	m = issue3788Model("en-US")
	m.SendLater.Note = "failed"
	if markup = renderNode(t, rail(m, open)); !strings.Contains(markup, "That change could not be saved. Try again.") || !strings.Contains(markup, "Welcome aboard") {
		t.Fatalf("a refused change must keep the rows and say so: %s", markup)
	}
}

func TestIssue3788_EditorShowsTheTimeInThePersonsZone(t *testing.T) {
	m := issue3788Model("en-US")
	markup := renderNode(t, rail(m, handlers{local: localUI{scheduledOpen: true, scheduledEdit: "s1", scheduledBody: "Reminder: compliance training is tomorrow at ten.", scheduledDate: "2026-03-04", scheduledClock: "08:30"}}))
	for _, want := range []string{`id="issue3788-edit-body"`, `id="issue3788-edit-date"`, `id="issue3788-edit-time"`, `data-chat-value="2026-03-04"`, `data-chat-value="08:30"`,
		`data-action="scheduled-save"`, `data-action="scheduled-edit-close"`, "Times are in Europe/Berlin (UTC+1)."} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	// The fields belong to no form, so Enter in them cannot send what is in the message box.
	if got := strings.Count(markup, `form="issue3788-no-form"`); got != 3 {
		t.Fatalf("fields tied to no form = %d", got)
	}
}

func TestIssue3788_PresetsAreRealFutureTimesInThePersonsZone(t *testing.T) {
	m := issue3788Model("en-US")
	// Tuesday 2026-03-03, 23:00 in Berlin: no "this evening", tomorrow is Wednesday.
	now := time.Date(2026, 3, 3, 22, 0, 0, 0, time.UTC)
	got := issue3788Presets(m, now)
	want := []struct {
		key string
		at  time.Time
	}{
		{keyIssue3788InHour, now.Add(time.Hour)},
		{keyIssue3788Morning, time.Date(2026, 3, 4, 7, 30, 0, 0, time.UTC)},
		{keyIssue3788NextWeek, time.Date(2026, 3, 9, 7, 30, 0, 0, time.UTC)},
	}
	if len(got) != len(want) {
		t.Fatalf("presets = %+v", got)
	}
	for i := range want {
		if got[i].Key != want[i].key || !got[i].At.Equal(want[i].at) {
			t.Fatalf("preset %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	// Morning in Berlin at noon: the evening is offered, and Monday is not repeated when it is tomorrow.
	friday := time.Date(2026, 3, 6, 11, 0, 0, 0, time.UTC)
	got = issue3788Presets(m, friday)
	keys := []string{}
	for _, p := range got {
		keys = append(keys, p.Key)
		if !issue3788Window(p.At, friday) {
			t.Fatalf("%s is outside the window: %v", p.Key, p.At)
		}
	}
	if strings.Join(keys, ",") != strings.Join([]string{keyIssue3788InHour, keyIssue3788Evening, keyIssue3788Morning, keyIssue3788NextWeek}, ",") {
		t.Fatalf("friday presets = %v", keys)
	}
	saturday := time.Date(2026, 3, 7, 11, 0, 0, 0, time.UTC)
	monday := false
	for _, p := range issue3788Presets(m, saturday) {
		monday = monday || (p.Key == keyIssue3788NextWeek && p.At.Equal(time.Date(2026, 3, 9, 7, 30, 0, 0, time.UTC)))
	}
	if !monday {
		t.Fatal("Monday is two days away on a Saturday and must be offered")
	}
	// Sunday: tomorrow is Monday, so the Monday preset would repeat the morning one.
	sunday := time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC)
	for _, p := range issue3788Presets(m, sunday) {
		if p.Key == keyIssue3788NextWeek {
			t.Fatal("Monday repeated on a Sunday")
		}
	}
	// The hour a clock skips is not offered: Berlin springs forward 2026-03-29 at 02:00.
	for _, p := range issue3788Presets(m, time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)) {
		if p.At.In(chatcmd003Location(m)).Hour() == 2 {
			t.Fatalf("a time in the skipped hour: %v", p.At)
		}
	}
}

func TestIssue3788_PanelOffersPresetsAndAPickerWithoutAForm(t *testing.T) {
	m := issue3788Model("en-US")
	markup := renderNode(t, issue3788Panel(m, localUI{sendLaterOpen: true}))
	for _, want := range []string{`id="issue3788-panel"`, "Send later", "In one hour", "Tomorrow morning",
		`data-action="sendlater-pick"`, `data-action="sendlater-custom"`, `data-action="sendlater-close"`,
		`id="issue3788-date"`, `id="issue3788-time"`, "Times are in Europe/Berlin", "only you can see"} {
		if !strings.Contains(strings.ToLower(markup), strings.ToLower(want)) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	if strings.Count(markup, `form="issue3788-no-form"`) != 2 {
		t.Fatalf("the picker's fields must belong to no form: %s", markup)
	}
	if got := renderNode(t, issue3788Panel(m, localUI{})); strings.Contains(got, "issue3788-panel") {
		t.Fatal("panel drawn while closed")
	}
	// An agent's conversation, or a page with no server behind it, offers nothing.
	m.Callbacks.ScheduleSend = nil
	if got := renderNode(t, issue3788Panel(m, localUI{sendLaterOpen: true})); strings.Contains(got, "issue3788-panel") {
		t.Fatal("panel drawn with nothing behind it")
	}
}

func TestIssue3788_PlusMenuOffersSendLater(t *testing.T) {
	m := issue3788Model("en-US")
	found := false
	for _, item := range composerAddItems(m) {
		found = found || item.kind == "sendlater"
	}
	if !found {
		t.Fatal("the + menu has no Send later item")
	}
	markup := renderNode(t, composerAddControl(m, "chat-composer", false))
	for _, want := range []string{`data-extra="sendlater"`, "Send later", "Choose when this message goes out"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	m.Callbacks.ScheduleSend = nil
	for _, item := range composerAddItems(m) {
		if item.kind == "sendlater" {
			t.Fatal("Send later offered with nothing behind it")
		}
	}
	m = issue3788Model("en-US")
	m.Conversations = append(m.Conversations, Conversation{ID: "bot", Name: "Helper", Kind: DirectMessage, Joined: true, Agent: true})
	m.SelectedID = "bot"
	for _, item := range composerAddItems(m) {
		if item.kind == "sendlater" {
			t.Fatal("Send later offered in an agent's conversation")
		}
	}
}

func TestIssue3788_ScheduleKeepsTheDraftWhenItCannotBeScheduled(t *testing.T) {
	type call struct {
		conversation, body string
		at                 time.Time
	}
	var calls []call
	var finish func(SendLaterResult)
	m := issue3788Model("en-US")
	m.Draft = "Training reminder"
	m.Callbacks.ScheduleSend = func(conversation, body string, at time.Time, done func(SendLaterResult)) {
		calls, finish = append(calls, call{conversation, body, at}), done
	}
	cleared := []string{}
	clear := func(c string) { cleared = append(cleared, c) }
	at := time.Now().Add(3 * time.Hour)

	// Refusals that never reach the server, each with its own words and the draft left alone.
	for name, tc := range map[string]struct {
		model Model
		at    time.Time
		err   error
		note  string
	}{
		"empty":         {func() Model { x := m; x.Draft = "  "; return x }(), at, nil, "empty"},
		"slash command": {func() Model { x := m; x.Draft = "/poll lunch"; return x }(), at, nil, "command"},
		"too soon":      {m, time.Now().Add(10 * time.Second), nil, "time"},
		"too far":       {m, time.Now().Add(400 * 24 * time.Hour), nil, "time"},
		"unreadable":    {m, time.Time{}, errTest3788, "time"},
	} {
		local := localStore{box: &localUI{sendLaterOpen: true}}
		issue3788Schedule(tc.model, local, clear, tc.at, tc.err)
		if len(calls) != 0 || len(cleared) != 0 || local.get().sendLaterNote != tc.note || !local.get().sendLaterOpen {
			t.Fatalf("%s: calls=%d cleared=%v note=%q", name, len(calls), cleared, local.get().sendLaterNote)
		}
	}

	// A good choice reaches the server once, with the box emptied only after it says saved.
	local := localStore{box: &localUI{sendLaterOpen: true}}
	issue3788Schedule(m, local, clear, at, nil)
	if len(calls) != 1 || calls[0].conversation != "general" || calls[0].body != "Training reminder" || !calls[0].at.Equal(at) || !local.get().sendLaterBusy {
		t.Fatalf("calls=%+v busy=%v", calls, local.get().sendLaterBusy)
	}
	if len(cleared) != 0 {
		t.Fatal("the box was emptied before the server kept the message")
	}
	finish(SendLaterResult{Outcome: "failed"})
	if len(cleared) != 0 || local.get().sendLaterNote != "failed" || local.get().sendLaterBusy || !local.get().sendLaterOpen {
		t.Fatalf("a failure must keep the draft: cleared=%v local=%+v", cleared, local.get())
	}
	issue3788Schedule(m, local, clear, at, nil)
	finish(SendLaterResult{Outcome: "limit"})
	if local.get().sendLaterNote != "limit" || len(cleared) != 0 {
		t.Fatalf("limit: %+v", local.get())
	}
	issue3788Schedule(m, local, clear, at, nil)
	finish(SendLaterResult{Outcome: "saved", SendAt: at, QuietPeople: 1})
	if len(cleared) != 1 || cleared[0] != "general" || local.get().sendLaterOpen || local.get().sendLaterBusy || !local.get().sendLaterSaved.Equal(at) || local.get().sendLaterQuiet != 1 {
		t.Fatalf("saved: cleared=%v local=%+v", cleared, local.get())
	}
}

type testError3788 string

func (e testError3788) Error() string { return string(e) }

const errTest3788 = testError3788("unreadable")

func TestIssue3788_NotesAndConfirmationAreInPlainWords(t *testing.T) {
	m := issue3788Model("en-US")
	for note, want := range map[string]string{
		"empty":   "Write a message first",
		"command": "A slash command can&#39;t be scheduled.",
		"files":   "A message with files can&#39;t be scheduled yet.",
		"limit":   "You have the most scheduled messages allowed.",
		"time":    "Choose a time between two minutes and a year from now.",
		"failed":  "It is still in the box.",
	} {
		markup := renderNode(t, issue3788Panel(m, localUI{sendLaterOpen: true, sendLaterNote: note}))
		if !strings.Contains(markup, want) || !strings.Contains(markup, `role="alert"`) {
			t.Fatalf("%s: %s", note, markup)
		}
	}
	at := time.Date(2026, 3, 4, 7, 30, 0, 0, time.UTC)
	markup := renderNode(t, issue3788Confirmation(m, localUI{sendLaterSaved: at, sendLaterQuiet: 1}))
	for _, want := range []string{`role="status"`, "Scheduled for 2026-03-04 08:30 UTC+1.", "under Scheduled", "quiet hours"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	if markup = renderNode(t, issue3788Confirmation(m, localUI{sendLaterSaved: at})); strings.Contains(markup, "quiet hours") {
		t.Fatalf("quiet-hours warning without a quiet person: %s", markup)
	}
}

func TestIssue3788_CopyIsTranslatedAndAlwaysComplete(t *testing.T) {
	for key := range issue3788Copy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			if strings.TrimSpace(issue3788Copy[locale][key]) == "" {
				t.Fatalf("%s lacks %s", locale, key)
			}
		}
	}
	for locale, want := range map[string]string{"de-DE": "Geplante Nachrichten", "ar": "الرسائل المجدولة"} {
		markup := renderNode(t, rail(issue3788Model(locale), handlers{local: localUI{scheduledOpen: true}}))
		if !strings.Contains(markup, want) || strings.Contains(markup, "Scheduled messages") || strings.Contains(markup, "chat.issue3788") {
			t.Fatalf("%s copy: %s", locale, markup)
		}
	}
	for locale, want := range map[string]string{"de-DE": "Später senden", "ar": "إرسال لاحقاً"} {
		markup := renderNode(t, issue3788Panel(issue3788Model(locale), localUI{sendLaterOpen: true}))
		if !strings.Contains(markup, want) || strings.Contains(markup, "chat.issue3788") {
			t.Fatalf("%s panel: %s", locale, markup)
		}
	}
}

func TestIssue3788_StylesAreInTheServedStylesheetAndAreLogical(t *testing.T) {
	if !strings.Contains(Stylesheet, Issue3788Styles) {
		t.Fatal("Issue3788Styles is not part of Stylesheet")
	}
	if !strings.Contains(Issue3788Styles, "min-height:44px") || strings.Contains(Issue3788Styles, "margin-left") || strings.Contains(Issue3788Styles, "text-align:left") {
		t.Fatal("styles must hold 44px targets and use logical properties")
	}
}

// A save the server refused for a reason other than the time (the message was
// being sent, or changed elsewhere) must not tell the person to pick another time.
func TestIssue3788_EditorNamesTheRealReasonASaveFailed(t *testing.T) {
	m := issue3788Model("en-US")
	editing := localUI{scheduledOpen: true, scheduledEdit: "s1", scheduledBody: "x", scheduledDate: "2026-03-04", scheduledClock: "08:30"}
	editing.scheduledEditNote = "failed"
	markup := renderNode(t, rail(m, handlers{local: editing}))
	if !strings.Contains(markup, "That change could not be saved. Try again.") || strings.Contains(markup, "between two minutes and a year") {
		t.Fatalf("a failed save was worded as a bad time: %s", markup)
	}
	editing.scheduledEditNote = "time"
	markup = renderNode(t, rail(m, handlers{local: editing}))
	if !strings.Contains(markup, "between two minutes and a year") {
		t.Fatalf("a bad time was not worded as one: %s", markup)
	}
}
