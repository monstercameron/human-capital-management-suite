package chatui

import (
	"strings"
	"testing"
	"time"
)

func issue3964Model() Model {
	at := time.Date(2025, 11, 10, 10, 0, 0, 0, time.Local)
	return Model{
		Locale:      "en-US",
		SelectedID:  "ops",
		CurrentUser: "me",
		Members:     []Member{{ID: "maya", Name: "Maya Patel"}, {ID: "malik", Name: "Malik Johnson"}},
		Conversations: []Conversation{
			{ID: "ops", Name: "ops", Kind: PublicChannel},
		},
		Callbacks: Callbacks{LoadTimeline: func(string, string, bool) {}, OpenTimelineSource: func(string) {}, CheckTimelineEntries: func([]string, func(TimelineCheck)) {}},
		Timeline: TimelineView{ConversationID: "ops", Status: TimelineReady, From: "2025-11-10", To: "2025-11-11", Replies: true, Entries: []TimelineEntry{
			{ID: "p1", AuthorID: "maya", Text: "Pump 3 is leaking again", At: at, Href: "/workspace/app/chat#channel=ops&message=p1&at=4"},
			{ID: "p2", AuthorID: "malik", Text: "It started around 09:30 I think", At: at.Add(20 * time.Minute), Edited: true},
			{ID: "p3", AuthorID: "ghost", Text: "valves closed", At: at.Add(10 * time.Hour), InThread: true},
			{ID: "p4", AuthorID: "maya", Text: "leads only: cost estimate", At: at.Add(10*time.Hour + time.Minute), Restricted: true},
		}},
	}
}

func issue3964Open() handlers { return handlers{local: localUI{timelineOpen: true}} }

func TestIssue3964_EntryIsACollapsedKeyboardOperableDisclosure(t *testing.T) {
	m := issue3964Model()
	markup := renderNode(t, issue3964Section(m, handlers{}))
	for _, want := range []string{`id="chat-details-timeline"`, "Event timeline", `data-action="timeline-toggle"`, `aria-expanded="false"`, `aria-controls="chat-details-timeline-body"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("collapsed entry lacks %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "Pump 3") || strings.Contains(markup, "timeline-entry") {
		t.Fatalf("a collapsed entry must not list messages:\n%s", markup)
	}
	if !strings.Contains(renderNode(t, issue3964Section(m, issue3964Open())), `aria-expanded="true"`) {
		t.Fatal("open state is not announced")
	}
	m.Callbacks.LoadTimeline = nil
	if issue3964Section(m, handlers{}) != nil {
		t.Fatal("a client that cannot read a timeline must not offer a dead entry")
	}
}

func TestIssue3964_SetupShowsSourceRangeAndRepliesBeforeAnythingIsRead(t *testing.T) {
	m := issue3964Model()
	m.Timeline = TimelineView{}
	markup := renderNode(t, issue3964Section(m, issue3964Open()))
	for _, want := range []string{
		"Reads: #ops", `for="chat-timeline-from"`, `id="chat-timeline-from"`, `type="date"`, "First day", `for="chat-timeline-to"`, `id="chat-timeline-to"`, "Last day",
		`id="chat-timeline-replies"`, `type="checkbox"`, "Include replies in threads", `data-action="timeline-make"`, "Make timeline",
		"Nothing is posted, exported or changed unless you choose to.",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("setup lacks %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "timeline-entry") || strings.Contains(markup, "timeline-draft") {
		t.Fatalf("nothing is read, so nothing is listed or draftable:\n%s", markup)
	}
}

func TestIssue3964_EntriesAreReportsWithPostedTimeAuthorAndTheWayToTheMessage(t *testing.T) {
	m := issue3964Model()
	markup := renderNode(t, issue3964Section(m, issue3964Open()))
	for _, want := range []string{
		"Maya Patel reported", "Malik Johnson reported", "Posted ", "datetime=", "“Pump 3 is leaking again”",
		`data-action="timeline-open"`, `data-id="p1"`, "Open the message from Maya Patel",
		`data-action="timeline-remove"`, "Remove from the timeline: Maya Patel",
		// The standing statement that a report is not a verified event.
		"not proof of when the event happened",
		`id="chat-timeline-status"`, `aria-live="polite"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("list lacks %q:\n%s", want, markup)
		}
	}
	// Oldest first.
	if strings.Index(markup, "Pump 3") > strings.Index(markup, "valves closed") {
		t.Fatalf("entries are not in time order:\n%s", markup)
	}
	// An author the roster does not name is never shown as an id.
	if strings.Contains(markup, "ghost") || !strings.Contains(markup, "Someone reported") {
		t.Fatalf("an unknown author leaked its id:\n%s", markup)
	}
}

func TestIssue3964_UncertaintyIsFlaggedNotFlattened(t *testing.T) {
	m := issue3964Model()
	markup := renderNode(t, issue3964Section(m, issue3964Open()))
	for _, want := range []string{
		"Edited after it was posted. This is its current wording.",
		"Names a time of its own. The posted time may not be when it happened.",
		"Reply in a thread",
		"Addressed to part of this channel. Left out of the draft.",
		// A silence between the second and the third entry is a missing interval.
		"No messages for 9 hours before the next one.",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("a flag is missing %q:\n%s", want, markup)
		}
	}
	if strings.Count(markup, "Names a time of its own") != 1 {
		t.Fatalf("only the message that names a time says so:\n%s", markup)
	}
}

func TestIssue3964_MentionsOwnTime(t *testing.T) {
	for text, want := range map[string]bool{
		"around 10:00": true, "at 3pm": true, "it broke yesterday": true, "gestern Abend": true, "um 14 Uhr": true, "منذ ساعة": true, "أمس": true,
		"pump 3 failed": false, "room 12 and 300 units": false, "version 1.2.3": false, "": false,
	} {
		if got := TimelineMentionsOwnTime(text); got != want {
			t.Fatalf("TimelineMentionsOwnTime(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestIssue3964_RemovingAnEntryHidesItAndRestoreBringsItBack(t *testing.T) {
	m := issue3964Model()
	h := handlers{local: localUI{timelineOpen: true, timelineRemoved: map[string]bool{"p1": true}}}
	markup := renderNode(t, issue3964Section(m, h))
	if strings.Contains(markup, "Pump 3 is leaking again") {
		t.Fatalf("a removed entry is still listed:\n%s", markup)
	}
	for _, want := range []string{"Restore removed entries (1)", `data-action="timeline-restore"`, "Pump 3"} {
		if want == "Pump 3" {
			continue
		}
		if !strings.Contains(markup, want) {
			t.Fatalf("no way back after removing: %q:\n%s", want, markup)
		}
	}
	all := h
	all.local.timelineRemoved = map[string]bool{"p1": true, "p2": true, "p3": true, "p4": true}
	if !strings.Contains(renderNode(t, issue3964Section(m, all)), "Every entry has been removed") {
		t.Fatal("removing everything must be said")
	}
}

func TestIssue3964_EveryStateIsSaidInWords(t *testing.T) {
	cases := []struct {
		name         string
		change       func(*Model, *handlers)
		want, not    string
		alert, retry bool
	}{
		{"loading", func(m *Model, _ *handlers) {
			m.Timeline = TimelineView{ConversationID: "ops", Status: TimelineLoading, From: "2025-11-10", To: "2025-11-11"}
		}, "Reading messages", "Pump 3", false, false},
		{"empty", func(m *Model, _ *handlers) { m.Timeline.Entries = nil }, "No messages you can read were posted in this conversation from", "Pump 3", false, false},
		{"truncated", func(m *Model, _ *handlers) { m.Timeline.Truncated = true }, "Only the first 200 messages in this range were read. Later messages are not shown.", "", true, false},
		{"error keeps the range", func(m *Model, _ *handlers) {
			m.Timeline = TimelineView{ConversationID: "ops", Status: TimelineError, From: "2025-11-10", To: "2025-11-11", Replies: true}
		}, "Your dates are kept", "Pump 3", true, true},
		{"unavailable keeps the range", func(m *Model, _ *handlers) {
			m.Timeline = TimelineView{ConversationID: "ops", Status: TimelineUnavailable, From: "2025-11-10", To: "2025-11-11", Replies: true}
		}, "cannot be read right now", "Pump 3", true, true},
		{"range problem", func(_ *Model, h *handlers) { h.local.timelineNote = "range_order" }, "The first day cannot be after the last day.", "", true, false},
		{"changed since", func(_ *Model, h *handlers) { h.local.timelineNote, h.local.timelineChanged = "changed", 2 }, "2 entries were changed or can no longer be read", "", true, false},
		{"drafted", func(_ *Model, h *handlers) { h.local.timelineNote = "drafted" }, "It has not been sent.", "", false, false},
	}
	for _, c := range cases {
		m, h := issue3964Model(), issue3964Open()
		c.change(&m, &h)
		markup := renderNode(t, issue3964Section(m, h))
		if !strings.Contains(markup, c.want) || (c.not != "" && strings.Contains(markup, c.not)) {
			t.Fatalf("%s: want %q and not %q:\n%s", c.name, c.want, c.not, markup)
		}
		if c.alert && !strings.Contains(markup, `role="alert"`) {
			t.Fatalf("%s: must be announced as an alert:\n%s", c.name, markup)
		}
		if c.retry && (!strings.Contains(markup, "Try again") || !strings.Contains(markup, `value="2025-11-10"`) && !strings.Contains(markup, `data-chat-value="2025-11-10"`)) {
			t.Fatalf("%s: a failure must offer a retry and keep the chosen range:\n%s", c.name, markup)
		}
	}
}

func TestIssue3964_AnotherConversationsTimelineIsNeverShown(t *testing.T) {
	m := issue3964Model()
	m.Timeline.ConversationID = "finance"
	markup := renderNode(t, issue3964Section(m, issue3964Open()))
	if strings.Contains(markup, "Pump 3") || strings.Contains(markup, "timeline-entry") {
		t.Fatalf("another conversation's timeline leaked into this one:\n%s", markup)
	}
}

func TestIssue3964_RangeIsReadInTheReadersDaysAndBounded(t *testing.T) {
	start, end, problem := TimelineRange("2025-11-10", "2025-11-12")
	if problem != "" || !start.Equal(time.Date(2025, 11, 10, 0, 0, 0, 0, time.Local)) || !end.Equal(time.Date(2025, 11, 13, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("range = %v %v %q", start, end, problem)
	}
	for from, to := range map[string]string{"": "2025-11-12", "2025-11-10": "", "x": "y", "2025-13-40": "2025-11-12"} {
		if _, _, p := TimelineRange(from, to); p != "range_missing" {
			t.Fatalf("TimelineRange(%q,%q) problem = %q", from, to, p)
		}
	}
	if _, _, p := TimelineRange("2025-11-12", "2025-11-10"); p != "range_order" {
		t.Fatalf("backwards range problem = %q", p)
	}
	if _, _, p := TimelineRange("2025-01-01", "2025-11-10"); p != "range_long" {
		t.Fatalf("long range problem = %q", p)
	}
	if _, _, p := TimelineRange("2025-11-10", "2025-11-10"); p != "" {
		t.Fatalf("a single day is a range: %q", p)
	}
	// The search compares UTC days exclusively; the days it is asked for must
	// cover every local instant in the range in any time zone.
	after, before := TimelineSearchDays(start, end)
	if after >= start.UTC().Format("2006-01-02") || before <= end.UTC().Format("2006-01-02") {
		t.Fatalf("search days %s..%s do not enclose the range", after, before)
	}
	// And no wider than that: every extra day is read, paid for out of the page
	// budget, and then thrown away.
	if after != start.UTC().AddDate(0, 0, -1).Format("2006-01-02") || before != end.UTC().AddDate(0, 0, 1).Format("2006-01-02") {
		t.Fatalf("search days %s..%s are wider than one UTC day outside the range", after, before)
	}
}

func TestIssue3964_EntriesOutsideTheRangeAreDropped(t *testing.T) {
	start := time.Date(2025, 11, 10, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 1)
	got := timelineInRange([]TimelineEntry{
		{ID: "late", At: start.Add(25 * time.Hour)}, {ID: "in2", At: start.Add(23 * time.Hour)}, {ID: "in1", At: start}, {ID: "early", At: start.Add(-time.Second)}, {ID: "", At: start.Add(time.Hour)}, {ID: "zero"},
	}, start, end)
	if len(got) != 2 || got[0].ID != "in1" || got[1].ID != "in2" {
		t.Fatalf("in range = %+v, want in1 then in2", got)
	}
}

func TestIssue3964_DraftIsPlainWordsLabelsReportsAndLeavesOutWhatIsNarrower_Security(t *testing.T) {
	m := issue3964Model()
	m.Timeline.Entries[0].Text = "Pump 3 leaking\n@everyone please\u202e check"
	all := timelineInRange(m.Timeline.Entries, mustDay("2025-11-10"), mustEnd("2025-11-11"))
	use, left := timelineDraftable(all)
	if len(use) != 3 || left != 1 {
		t.Fatalf("draftable = %d left out = %d, want 3 and 1", len(use), left)
	}
	text := timelineDraftText(m, m.Timeline, all, use, left, "https://hcm.example/")
	for _, want := range []string{
		"Event timeline for #ops, ", "Draft for review. Each line is when someone posted a report in chat. It does not confirm when anything happened.",
		"1. ", "Maya Patel reported: “Pump 3 leaking @\u200beveryone please check”",
		"Malik Johnson reported: “It started around 09:30 I think” (edited after posting; names a time of its own)",
		"(No messages for 9 hours.)", "Someone reported: “valves closed” (reply in a thread)",
		"Source: https://hcm.example/workspace/app/chat#channel=ops&message=p1&at=4",
		"Left out: 1 addressed to part of the channel.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("draft lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "cost estimate") || strings.Contains(text, "leads only") {
		t.Fatalf("a message addressed to part of the channel reached the draft:\n%s", text)
	}
	if strings.Contains(text, "\u202e") || strings.Contains(text, "ghost") || strings.Contains(text, "@everyone") {
		t.Fatalf("untrusted text or an id reached the draft:\n%s", text)
	}
	m.Timeline.Truncated = true
	if !strings.Contains(timelineDraftText(m, m.Timeline, all, use, left, ""), "Later messages in this range were not read.") {
		t.Fatal("a cut-short range must say so in the draft too")
	}
}

func TestIssue3964_DraftControlsExplainThemselvesAndStayOffWithNothingToUse(t *testing.T) {
	m := issue3964Model()
	markup := renderNode(t, issue3964Section(m, issue3964Open()))
	for _, want := range []string{`data-action="timeline-draft"`, "Put in message box", `aria-describedby="chat-timeline-draft-hint"`, "Access is checked again first", "Nothing is sent."} {
		if !strings.Contains(markup, want) {
			t.Fatalf("draft control lacks %q:\n%s", want, markup)
		}
	}
	h := issue3964Open()
	h.local.timelineRemoved = map[string]bool{"p1": true, "p2": true, "p3": true}
	only := renderNode(t, issue3964Section(m, h))
	i := strings.Index(only, `data-action="timeline-draft"`)
	if i < 0 || !strings.Contains(only[strings.LastIndex(only[:i], "<button"):i+strings.Index(only[i:], ">")], "disabled") {
		t.Fatalf("with only a part-of-channel entry left the draft button must be off:\n%s", only)
	}
	h = issue3964Open()
	h.local.timelineChecking = true
	if !strings.Contains(renderNode(t, issue3964Section(m, h)), "Checking access…") {
		t.Fatal("the access check must say it is running")
	}
}

func TestIssue3964_CopyIsTranslatedAndLaidOutRightToLeft(t *testing.T) {
	for key, copy := range issue3964Copy {
		for i, text := range copy {
			if strings.TrimSpace(text) == "" {
				t.Fatalf("copy %q has no text for locale %d", key, i)
			}
		}
		if key != "d_edited" && copy[1] == copy[0] && len(copy[0]) > 12 {
			t.Fatalf("copy %q is not translated to German", key)
		}
		if copy[2] == copy[0] {
			t.Fatalf("copy %q is not translated to Arabic", key)
		}
	}
	for locale, want := range map[string]string{"de-DE": "Ereignis-Zeitleiste", "ar": "الجدول الزمني للأحداث"} {
		m := issue3964Model()
		m.Locale = locale
		markup := renderNode(t, issue3964Section(m, issue3964Open()))
		if !strings.Contains(markup, want) || strings.Contains(markup, "Event timeline") || strings.Contains(markup, "{") || strings.Contains(markup, "chat.issue3964") {
			t.Fatalf("%s: not fully translated:\n%s", locale, markup)
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("the Arabic timeline is not right-to-left")
		}
	}
}
