package chatui

import (
	"strings"
	"testing"
	"time"
)

// The notification choice is one radio group: the choice in force is filled and
// carries a check mark, and a press changes it at once.
func TestTodo_CHATUX_019(t *testing.T) {
	m := chatux021Model()
	for _, tc := range []struct {
		mode    NotificationMode
		current string
	}{{NotifyAll, "All messages"}, {NotifyMention, "Mentions only"}, {NotifyMute, "Muted"}} {
		got := renderNode(t, notificationsControl(m, "general", tc.mode))
		if strings.Count(got, `type="radio"`) != 3 || strings.Count(got, " checked ") != 1 || strings.Count(got, "details-notify-check") != 1 || !strings.Contains(got, `role="radiogroup"`) {
			t.Errorf("%s: not one radio group with one choice filled and checked: %s", tc.current, got)
		}
		if !strings.Contains(got, `channel-widget-summary-value">`+tc.current+"<") {
			t.Errorf("%s: the row does not name the choice in force: %s", tc.current, got)
		}
		// The filled radio is the one of the mode in force.
		checked := got[strings.Index(got, " checked "):]
		if !strings.Contains(checked[:strings.Index(checked, ">")], `value="`+string(tc.mode)+`"`) {
			t.Errorf("%s: the wrong radio is filled: %s", tc.current, checked[:80])
		}
	}
	off := m
	off.Callbacks.SetConversationNotification = nil
	if got := renderNode(t, notificationsControl(off, "general", NotifyAll)); strings.Count(got, "disabled") != 3 {
		t.Errorf("without the callback the radios are live: %s", got)
	}
}

// A pinned item shows who pinned it and when, opens the message when pressed,
// shows its text formatted, and has Unpin.
func TestTodo_CHATUX_019_PinnedItem(t *testing.T) {
	now := time.Now()
	m := chatux021Model()
	m.Callbacks.JumpToPin = func(string, uint64) {}
	m.Callbacks.CopyPinReference = func(string) {}
	m.Callbacks.Unpin = func(string) {}
	m.ChannelPins = []ChannelPin{
		{PostID: "p1", Author: "Jake Sullivan", Body: "**bold**, _italic_, `code` and [a link](https://example.com)\nsecond line", Sequence: 4, PinnedBy: "Walt Brennan", PinnedAt: now},
		{PostID: "p2", Author: "Jake Sullivan", Body: "Older", Sequence: 2, PinnedBy: "Walt Brennan", PinnedAt: now.AddDate(0, 0, -3)},
		{PostID: "p3", Author: "Jake Sullivan", Body: "Unknown pinner", Sequence: 1},
	}
	got := renderNode(t, chatux019PinnedSection(m))
	for _, want := range []string{"<strong>bold</strong>", "<em>italic</em>", "<code>code</code>", "a link", `data-action="pin-jump" data-id="p1"`, `data-action="unpin" data-id="p1"`, ">Unpin<", ">Copy link<", "Pinned by Walt Brennan · " + chat5Clock("en-US", now)} {
		if !strings.Contains(got, want) {
			t.Errorf("the pinned item misses %q: %s", want, got)
		}
	}
	for _, absent := range []string{"**", "_italic_", "`code`", "<a ", "second line"} {
		if strings.Contains(got, absent) {
			t.Errorf("the preview shows %q: %s", absent, got)
		}
	}
	if !strings.Contains(got, "Pinned by Walt Brennan · "+formatShortDate("en-US", now.AddDate(0, 0, -3))) {
		t.Errorf("an older pin shows no date: %s", got)
	}
	// A pin whose pinner and time are unknown shows no meta line at all, not an
	// empty one.
	if row := got[strings.Index(got, "Unknown pinner"):]; strings.Contains(row[:strings.Index(row, "</li>")], "pinned-meta") {
		t.Errorf("an empty meta line: %s", row)
	}
	// Opening and unpinning are the page's own callbacks; without them the
	// buttons rest.
	rest := m
	rest.Callbacks.JumpToPin, rest.Callbacks.Unpin = nil, nil
	if got := renderNode(t, chatux019PinnedSection(rest)); strings.Count(got, "disabled") < 2 {
		t.Errorf("buttons without callbacks are live: %s", got)
	}
	// The preview never becomes a link or a block inside the button.
	if strings.Contains(got[strings.Index(got, `class="pinned-link"`):strings.Index(got, "</button>")], "<p>") {
		t.Errorf("a block element inside the button: %s", got)
	}
}

func TestTodo_CHATUX_019_PinPreviewNodes(t *testing.T) {
	for body, want := range map[string]string{
		"# Heading **bold**":     "<strong>bold</strong>",
		"- item one\n- item two": "item one",
		"> quoted _text_":        "<em>text</em>",
		"":                       "",
		"   \n\n  first real":    "first real",
		"<b>raw</b>":             "&lt;b&gt;raw&lt;/b&gt;",
	} {
		got := renderNode(t, spanOf(pinPreviewNodes(body)))
		if want == "" && got != "<span></span>" {
			t.Errorf("%q: %s", body, got)
		}
		if want != "" && !strings.Contains(got, want) {
			t.Errorf("%q does not show %q: %s", body, want, got)
		}
	}
	long := strings.Repeat("word ", 200)
	if got := renderNode(t, spanOf(pinPreviewNodes(long))); len([]rune(got)) > 400 {
		t.Errorf("a long preview is not bounded: %d runes", len([]rune(got)))
	}
}

// A reaction is named in words: who reacted and with what. A screen reader
// hears "You and Sam reacted with eyes", not a count and a glyph.
func TestTodo_CHATUX_019_ReactionNames(t *testing.T) {
	m := Model{Locale: "en-US", CurrentUser: "me", Members: []Member{{ID: "sam", Name: "Sam Rivera"}, {ID: "kim", Name: "Kim Park"}, {ID: "lee", Name: "Lee Chen"}}}
	for _, tc := range []struct {
		chip ReactionChip
		want string
	}{
		{ReactionChip{Emoji: "👀", Count: 2, Mine: true, PeopleIDs: []string{"sam"}}, "You and Sam Rivera reacted with eyes"},
		{ReactionChip{Emoji: "👀", Count: 1, Mine: true}, "You reacted with eyes"},
		{ReactionChip{Emoji: "👍", Count: 1, PeopleIDs: []string{"kim"}}, "Kim Park reacted with thumbs up"},
		{ReactionChip{Emoji: "❤️", Count: 3, Mine: true, PeopleIDs: []string{"sam", "kim"}}, "You, Sam Rivera and Kim Park reacted with red heart"},
		{ReactionChip{Emoji: "🎉", Count: 4, Mine: true, PeopleIDs: []string{"sam", "kim", "lee"}}, "4 people reacted with party popper"},
		{ReactionChip{Emoji: "🔥", Count: 2, PeopleIDs: []string{"nobody"}}, "2 people reacted with fire"},
		{ReactionChip{Emoji: "🦄", Count: 1, PeopleIDs: []string{"sam"}}, "Sam Rivera reacted with an emoji"},
	} {
		if got := chatux019ReactionLabel(m, tc.chip); got != tc.want {
			t.Errorf("%+v = %q, want %q", tc.chip, got, tc.want)
		}
	}
	m.Locale = "de-DE"
	if got := chatux019ReactionLabel(m, ReactionChip{Emoji: "👀", Count: 2, Mine: true, PeopleIDs: []string{"sam"}}); !strings.Contains(got, "Sie und Sam Rivera haben mit Augen reagiert") {
		t.Errorf("German: %q", got)
	}
	if got := chatux019ReactionLabel(m, ReactionChip{Emoji: "👀", Count: 1, PeopleIDs: []string{"sam"}}); got != "Sam Rivera hat mit Augen reagiert" {
		t.Errorf("German singular: %q", got)
	}
	m.Locale = "ar"
	if got := chatux019ReactionLabel(m, ReactionChip{Emoji: "👀", Count: 1, PeopleIDs: []string{"sam"}}); !strings.Contains(got, "Sam Rivera") || !strings.Contains(got, "عينان") {
		t.Errorf("Arabic: %q", got)
	}
	// The chip carries it as its accessible name and its tooltip.
	m.Locale = "en-US"
	msg := Message{ID: "x", Chips: []ReactionChip{{Emoji: "👀", Count: 2, Mine: true, PeopleIDs: []string{"sam"}}}}
	row := renderNode(t, reactionRow(m, msg))
	if !strings.Contains(row, `aria-label="You and Sam Rivera reacted with eyes"`) || !strings.Contains(row, `title="You and Sam Rivera reacted with eyes"`) || strings.Contains(row, "reacted with 👀") {
		t.Errorf("the chip's name: %s", row)
	}
}

func TestTodo_CHATUX_019_Accessibility(t *testing.T) {
	m := chatux021Model()
	// Each radio is inside its own label, so its name is the words beside it, and
	// the group is named too.
	notify := renderNode(t, notificationsControl(m, "general", NotifyMention))
	if strings.Count(notify, "<label") != 3 || !strings.Contains(notify, `aria-label="Notifications for me"`) {
		t.Errorf("radios without labels: %s", notify)
	}
	// The check mark is decoration: the radio's state is the information.
	if !strings.Contains(notify, `class="details-notify-check" aria-hidden="true"`) && !strings.Contains(notify, `aria-hidden="true" class="details-notify-check"`) {
		t.Errorf("the check mark is read out: %s", notify)
	}
	// Every pinned control has a name, and a reaction's name carries no glyph.
	m.ChannelPins = []ChannelPin{{PostID: "p1", Author: "Jake Sullivan", Body: "Policy", Sequence: 1, PinnedBy: "Walt Brennan", PinnedAt: time.Now()}}
	m.Callbacks.JumpToPin, m.Callbacks.CopyPinReference, m.Callbacks.Unpin = func(string, uint64) {}, func(string) {}, func(string) {}
	pins := renderNode(t, chatux019PinnedSection(m))
	for _, want := range []string{`title="Jump to message"`, `aria-label="Copy link"`, `aria-label="Unpin message"`} {
		if !strings.Contains(pins, want) {
			t.Errorf("the pinned item misses %q: %s", want, pins)
		}
	}
	for _, tc := range []string{"👍", "❤️", "🙏", "🦄"} {
		if label := chatux019ReactionLabel(Model{Locale: "en-US"}, ReactionChip{Emoji: tc, Count: 1, PeopleIDs: []string{"x"}}); strings.Contains(label, tc) {
			t.Errorf("the name of %s holds the glyph: %q", tc, label)
		}
	}
}
