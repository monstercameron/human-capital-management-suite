package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3927State(t *testing.T, m Model) (localStore, chatcmd003Preview) {
	t.Helper()
	local := issue3925Preview(t, m, "/poll Which vendor? Acme, Birch or Cedar")
	issue3925Set(t, m, local, "closes", closesCustom)
	return local, local.get().chatcmd003
}

func TestIssue3927_CustomChoiceShowsDateTimeAndZone(t *testing.T) {
	m := issue3925Fixture()
	m.Chatcmd002TimeZone = "America/New_York"
	_, state := issue3927State(t, m)
	shown := renderNode(t, chatcmd003PreviewView(m, state))
	for _, want := range []string{"Pick date and time", `id="chatcmd003-custom-date"`, `type="date"`, `id="chatcmd003-custom-time"`, `type="time"`, `for="chatcmd003-custom-date"`, "Set closing time", `data-action="chatcmd003-custom"`, "America/New_York", "your time zone"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("missing %q in %s", want, shown)
		}
	}
	if state.Draft.Card.Poll.ClosesAt != nil {
		t.Fatal("a closing time appeared before one was chosen")
	}
	// The presets stay, and Custom is the chosen radio.
	if !strings.Contains(shown, "In a week") || !strings.Contains(shown, "End of day") {
		t.Fatalf("presets are gone: %s", shown)
	}
}

func TestIssue3927_ExactTimeIsResolvedShownAndPosted(t *testing.T) {
	m := issue3925Fixture()
	m.Chatcmd002TimeZone = "America/New_York"
	var posted chat.Chatcmd002Card
	m.Chatcmd002.Post = func(_, _ string, c chat.Chatcmd002Card, done func(error)) { posted = c; done(nil) }
	local, state := issue3927State(t, m)
	date := time.Now().AddDate(0, 0, 20).Format("2006-01-02")
	next := chatcmd003CustomSet(m, state, date, "12:00", time.Now())
	if next.Error != "" {
		t.Fatalf("a good time was refused: %s", next.Error)
	}
	local.update(func(u *localUI) { u.chatcmd003 = next })
	zone, _ := time.LoadLocation("America/New_York")
	at := next.Draft.Card.Poll.ClosesAt
	if at == nil || at.In(zone).Format("2006-01-02 15:04") != date+" 12:00" {
		t.Fatalf("the deadline is %v, want %s 12:00 New York", at, date)
	}
	shown := renderNode(t, chatcmd003PreviewView(m, next))
	if !strings.Contains(shown, "Closes "+date+" 12:00 UTC") || !strings.Contains(shown, `id="chatcmd003-custom-resolved"`) {
		t.Fatalf("the preview does not show the resolved deadline: %s", shown)
	}
	if !strings.Contains(shown, `data-chat-value="`+date+`"`) || !strings.Contains(shown, `data-chat-value="12:00"`) {
		t.Fatalf("the fields lost what was typed: %s", shown)
	}
	// Reading the text again (a keystroke) does not move it; Post sends it.
	again := chatcmd003Draw(m, next, "poll", next.Draft.Raw)
	if got := again.Draft.Card.Poll.ClosesAt; got == nil || !got.Equal(*at) {
		t.Fatalf("the deadline moved on redraw: %v", got)
	}
	chatcmd003Post(m, local)
	if posted.Poll == nil || posted.Poll.ClosesAt == nil || !posted.Poll.ClosesAt.Equal(*at) || len(posted.Poll.Options) != 3 {
		t.Fatalf("posted %+v", posted.Poll)
	}
}

func TestIssue3927_RefusalsKeepTheDraftAndWhatWasTyped(t *testing.T) {
	m := issue3925Fixture()
	m.Chatcmd002TimeZone = "America/New_York"
	_, state := issue3927State(t, m)
	state = func() chatcmd003Preview {
		s := state
		s.Settings = map[string]string{"closes": closesCustom, "multiple": "yes"}
		return chatcmd003Draw(m, s, "poll", s.Draft.Raw)
	}()
	now := time.Date(2027, 3, 1, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct{ date, clock, want, text string }{
		{"2027-02-01", "12:00", "custom-past", "already passed"},
		{"2027-03-14", "02:30", "custom-gap", "does not exist"},
		{"2027-11-07", "01:30", "custom-ambiguous", "happens twice"},
		{"", "12:00", "custom-invalid", "both a date and a time"},
	} {
		next := chatcmd003CustomSet(m, state, c.date, c.clock, now)
		if next.Error != c.want {
			t.Fatalf("%s %s: error %q, want %q", c.date, c.clock, next.Error, c.want)
		}
		if next.Draft.Card.Poll.ClosesAt != nil || len(next.Draft.Card.Poll.Options) != 3 || !next.Draft.Card.Poll.Multiple || next.Draft.Raw != state.Draft.Raw {
			t.Fatalf("%s: the draft was damaged: %+v", c.want, next.Draft.Card.Poll)
		}
		shown := renderNode(t, chatcmd003PreviewView(m, next))
		if !strings.Contains(shown, c.text) || !strings.Contains(shown, `role="alert"`) || !strings.Contains(shown, `aria-invalid="true"`) {
			t.Fatalf("%s: refusal not said in words: %s", c.want, shown)
		}
		if c.date != "" && !strings.Contains(shown, `data-chat-value="`+c.date+`"`) {
			t.Fatalf("%s: the typed date was lost: %s", c.want, shown)
		}
	}
}

func TestIssue3927_PostRevalidatesAndKeepsTheDraft(t *testing.T) {
	m := issue3925Fixture()
	posts := 0
	m.Chatcmd002.Post = func(_, _ string, _ chat.Chatcmd002Card, done func(error)) { posts++; done(nil) }
	local, state := issue3927State(t, m)
	// Custom chosen but never set: nothing is posted, and the preview says why.
	chatcmd003Post(m, local)
	if posts != 0 || local.get().chatcmd003.Error != "custom-needed" {
		t.Fatalf("posted without a time: posts=%d error=%q", posts, local.get().chatcmd003.Error)
	}
	// A time that was fine when set but has passed by the time Post is pressed.
	settings := map[string]string{"closes": closesCustom, settingClosesAt: time.Now().Add(-time.Minute).Format(time.RFC3339Nano)}
	state.Settings = settings
	local.update(func(u *localUI) { u.chatcmd003 = state })
	chatcmd003Post(m, local)
	got := local.get().chatcmd003
	if posts != 0 || got.Error != "custom-past" || !got.Open || len(got.Draft.Card.Poll.Options) != 3 {
		t.Fatalf("a passed time posted or the draft was lost: posts=%d error=%q", posts, got.Error)
	}
	if chatcmd003CustomProblem(map[string]string{"closes": "week"}, time.Now()) != "" {
		t.Fatal("a preset was held to the custom rule")
	}
}

func TestIssue3927_CopyInThreeLanguages(t *testing.T) {
	for _, key := range []string{"closes-custom", "custom-date", "custom-time", "custom-apply", "custom-zone-yours", "custom-zone-device", "custom-resolved", "custom-needed", "custom-invalid", "custom-past", "custom-gap", "custom-ambiguous"} {
		entry, ok := chatcmd003Copy[key]
		if !ok {
			t.Fatalf("no copy for %s", key)
		}
		for i, text := range entry {
			if strings.TrimSpace(text) == "" {
				t.Fatalf("%s has no text in language %d", key, i)
			}
		}
		if entry[0] == entry[1] && key != "custom-resolved" {
			t.Fatalf("%s is not translated to German", key)
		}
	}
	m := issue3925Fixture()
	m.Locale = "ar"
	m.Chatcmd002TimeZone = "Asia/Dubai"
	_, state := issue3927State(t, m)
	shown := renderNode(t, chatcmd003PreviewView(m, state))
	if strings.Contains(shown, "Pick date") || !strings.Contains(shown, "اختيار التاريخ والوقت") || !strings.Contains(shown, `dir="rtl"`) {
		t.Fatalf("Arabic preview: %s", shown)
	}
}

func TestIssue3927_ZoneSentenceIsSaidOnceWhateverTheState(t *testing.T) {
	m := issue3925Fixture()
	m.Chatcmd002TimeZone = "America/New_York"
	_, state := issue3927State(t, m)
	sentence := chatcmd003Text(m, "zone-everyone")
	if n := strings.Count(renderNode(t, chatcmd003PreviewView(m, state)), sentence); n != 1 {
		t.Fatalf("before a time is set the sentence appears %d times", n)
	}
	next := chatcmd003CustomSet(m, state, time.Now().AddDate(0, 0, 20).Format("2006-01-02"), "12:00", time.Now())
	if n := strings.Count(renderNode(t, chatcmd003PreviewView(m, next)), sentence); n != 1 {
		t.Fatalf("after a time is set the sentence appears %d times", n)
	}
}
