package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// #3925: a poll's closing time names its time zone, in the preview and on the
// posted card, and says whose zone it is. #3924: a poll whose results wait for
// closing and that never closes by itself says so before it is posted.
// #3906: the preview's settings come back with the draft.

func issue3925Preview(t *testing.T, m Model, raw string) localStore {
	t.Helper()
	issue3906Reset(t)
	local := localStore{box: &localUI{}}
	if !chatcmd003Follow(m, local, "chat-composer", raw) {
		t.Fatal("no preview")
	}
	return local
}

func issue3925Set(t *testing.T, m Model, local localStore, id, value string) {
	t.Helper()
	if !chatcmd003Action(m, local, "chatcmd003-set", id, value) {
		t.Fatalf("%s=%s was not taken", id, value)
	}
}

func TestIssue3925_PreviewClosingTimeNamesItsZone(t *testing.T) {
	m := issue3925Fixture()
	m.Chatcmd002TimeZone = "UTC"
	local := issue3925Preview(t, m, "/poll Which time works? Morning or Afternoon")
	issue3925Set(t, m, local, "closes", "today")
	shown := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	at := local.get().chatcmd003.Draft.Card.Poll.ClosesAt
	if at == nil {
		t.Fatal("no closing time")
	}
	want := "Closes " + at.UTC().Format("2006-01-02 15:04") + " UTC (your time zone)"
	if !strings.Contains(shown, want) {
		t.Fatalf("the preview does not name the zone, want %q in %s", want, shown)
	}
	if !strings.Contains(shown, "own time zone") {
		t.Fatalf("the preview does not say recipients see their own zone: %s", shown)
	}
}

func TestIssue3925_OffsetFollowsTheZoneAndKeepsWallClockAcrossDST(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tz database")
	}
	m := issue3925Fixture()
	m.Chatcmd002TimeZone = "America/New_York"
	for _, preset := range []string{"today", "tomorrow", "week"} {
		at := chatcmd003Closes(m, preset, nil)
		if at == nil {
			t.Fatalf("%s: no time", preset)
		}
		if h, mi := at.In(zone).Hour(), at.In(zone).Minute(); h != 23 || mi != 59 {
			t.Fatalf("%s: the deadline moved to %02d:%02d in its own zone", preset, h, mi)
		}
	}
	// A winter date and a summer date in one zone carry different offsets.
	winter := chatcmd002Stamp(m, time.Date(2026, 1, 15, 23, 59, 0, 0, zone))
	summer := chatcmd002Stamp(m, time.Date(2026, 7, 15, 23, 59, 0, 0, zone))
	if !strings.HasSuffix(winter, "23:59 UTC-5") || !strings.HasSuffix(summer, "23:59 UTC-4") {
		t.Fatalf("offsets are wrong: %q %q", winter, summer)
	}
	half := chatcmd002Stamp(Model{Chatcmd002TimeZone: "Asia/Kolkata"}, time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	if !strings.HasSuffix(half, "17:30 UTC+5:30") {
		t.Fatalf("a half-hour offset is wrong: %q", half)
	}
}

func TestIssue3925_FallbackZoneIsSaidNotGuessed(t *testing.T) {
	m := issue3925Fixture()
	m.Chatcmd002TimeZone = "Not/AZone"
	local := issue3925Preview(t, m, "/poll Which time works? Morning or Afternoon")
	issue3925Set(t, m, local, "closes", "tomorrow")
	shown := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	if !strings.Contains(shown, "time zone)") || !strings.Contains(shown, "device") {
		t.Fatalf("the fallback is not explained: %s", shown)
	}
}

func TestIssue3925_PostedCardNamesTheZone(t *testing.T) {
	m := issue3925Fixture()
	m.Chatcmd002TimeZone = "UTC"
	at := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Minute)
	card := chat.Chatcmd002Card{Kind: "poll", Title: "Which?", Poll: &chat.Chatcmd002Poll{Options: []chat.ChannelPollOption{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}}, ClosesAt: &at}}
	shown := renderNode(t, Chatcmd002RenderCard(m, "p1", chat.Chatcmd002View{Card: card, ResultsVisible: true}, false))
	if !strings.Contains(shown, "Closes "+at.Format("2006-01-02 15:04")+" UTC (your time zone)") {
		t.Fatalf("the posted card does not name the zone: %s", shown)
	}
}

func TestIssue3925_ZoneNoteIsLocalized(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		m := chatcmd003UIFixture()
		m.Locale = locale
		m.Chatcmd002TimeZone = "UTC"
		at := time.Date(2026, 11, 12, 23, 59, 0, 0, time.UTC)
		card := chat.Chatcmd002Card{Kind: "poll", Title: "Q", Poll: &chat.Chatcmd002Poll{Options: []chat.ChannelPollOption{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}}, ClosesAt: &at}}
		shown := renderNode(t, Chatcmd002RenderCard(m, "p1", chat.Chatcmd002View{Card: card, ResultsVisible: true}, false))
		if strings.Contains(shown, "your time zone") || !strings.Contains(shown, "UTC") {
			t.Fatalf("%s: zone note not localized: %s", locale, shown)
		}
	}
}

func TestIssue3924_NoteWhenResultsWaitForAClosingThatNeverComes(t *testing.T) {
	m := issue3925Fixture()
	m.Chatcmd002.Mutate = func(string, uint64, chat.Chatcmd002Mutation) {}
	local := issue3925Preview(t, m, "/poll Which time works? Morning or Afternoon")
	issue3925Set(t, m, local, "anonymous", "yes")
	issue3925Set(t, m, local, "results", "after-closing")
	state := local.get().chatcmd003
	shown := renderNode(t, chatcmd003PreviewView(m, state))
	if !strings.Contains(shown, `id="chatcmd003-closes-note"`) || !strings.Contains(shown, "you close the poll") {
		t.Fatalf("no explanation of when results appear: %s", shown)
	}
	if !strings.Contains(shown, `data-extra="today"`) || !strings.Contains(shown, "Close at end of day") {
		t.Fatalf("no way to choose a closing time from the note: %s", shown)
	}
	if strings.Contains(shown, `id="chatcmd003-post" class="button small" type="button" disabled`) {
		t.Fatalf("Post must stay available for an intentional manual close: %s", shown)
	}
	// Choosing a closing time keeps everything else and removes the note.
	issue3925Set(t, m, local, "closes", "today")
	state = local.get().chatcmd003
	p := state.Draft.Card.Poll
	if p.Results != "after-closing" || !p.Anonymous || len(p.Options) != 2 || p.ClosesAt == nil {
		t.Fatalf("settings were lost: %+v", p)
	}
	if shown = renderNode(t, chatcmd003PreviewView(m, state)); strings.Contains(shown, "chatcmd003-closes-note") {
		t.Fatalf("the note stays after a closing time is chosen: %s", shown)
	}
}

func TestIssue3924_NoNoteWhenResultsDoNotWait(t *testing.T) {
	m := issue3925Fixture()
	m.Chatcmd002.Mutate = func(string, uint64, chat.Chatcmd002Mutation) {}
	local := issue3925Preview(t, m, "/poll Which time works? Morning or Afternoon")
	issue3925Set(t, m, local, "results", "after-voting")
	if shown := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003)); strings.Contains(shown, "chatcmd003-closes-note") {
		t.Fatalf("note for after-voting: %s", shown)
	}
}

func TestIssue3924_SaysWhenManualClosingIsUnavailable(t *testing.T) {
	m := issue3925Fixture()
	m.Chatcmd002.Mutate = nil
	local := issue3925Preview(t, m, "/poll Which time works? Morning or Afternoon")
	issue3925Set(t, m, local, "results", "after-closing")
	shown := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	if !strings.Contains(shown, "cannot be closed by hand") {
		t.Fatalf("the limit is not explained: %s", shown)
	}
	if strings.Contains(shown, "or leave it to close by hand") {
		t.Fatalf("suggests an action that cannot be done: %s", shown)
	}
}

func TestIssue3924_NoteIsLocalized(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		m := issue3925Fixture()
		m.Chatcmd002.Mutate = func(string, uint64, chat.Chatcmd002Mutation) {}
		local := issue3925Preview(t, m, "/poll Which time works? Morning or Afternoon")
		issue3925Set(t, m, local, "results", "after-closing")
		m.Locale = locale
		shown := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
		if !strings.Contains(shown, "chatcmd003-closes-note") || strings.Contains(shown, "Results stay hidden") || strings.Contains(shown, "Close at end of day") {
			t.Fatalf("%s: note missing or not localized: %s", locale, shown)
		}
	}
}

func issue3906Reset(t *testing.T) {
	t.Helper()
	chatcmd003KeptReset()
	t.Cleanup(chatcmd003KeptReset)
}

func issue3906Room(m Model, room string) Model {
	m.SelectedID = room
	m.Conversations = append(append([]Conversation{}, m.Conversations...), Conversation{ID: room, Kind: PublicChannel, Name: room, Joined: true})
	return m
}

func TestIssue3906_PollSettingsComeBackAfterSwitchingChannels(t *testing.T) {
	issue3906Reset(t)
	people := issue3906Room(issue3925Fixture(), "people-ops")
	benefits := issue3906Room(people, "benefits")
	local := localStore{box: &localUI{}}
	local.forRoom("people-ops")
	raw := "/poll Which time works? Morning or Afternoon"
	chatcmd003Follow(people, local, "chat-composer", raw)
	issue3925Set(t, people, local, "multiple", "yes")
	issue3925Set(t, people, local, "anonymous", "yes")
	issue3925Set(t, people, local, "results", "after-voting")
	issue3925Set(t, people, local, "closes", "tomorrow")
	local.forRoom("benefits")
	local.forRoom("people-ops")
	people.Draft = raw
	shown := chatcmd003Effective(people, local.get().chatcmd003, "chat-composer")
	p := shown.Draft.Card.Poll
	if p == nil || !p.Multiple || !p.Anonymous || p.Results != "after-voting" || p.ClosesAt == nil {
		t.Fatalf("the settings did not come back: %+v", p)
	}
	// Typing on keeps them; another conversation does not inherit them.
	chatcmd003Follow(people, local, "chat-composer", raw+" later")
	if p = local.get().chatcmd003.Draft.Card.Poll; p == nil || !p.Anonymous || !p.Multiple {
		t.Fatalf("typing dropped the settings: %+v", p)
	}
	benefits.Draft = raw
	if other := chatcmd003Effective(benefits, chatcmd003Preview{}, "chat-composer").Draft.Card.Poll; other == nil || other.Anonymous || other.Multiple {
		t.Fatalf("another conversation took the settings: %+v", other)
	}
}

func TestIssue3906_TodoRuleComesBackAfterARemount(t *testing.T) {
	issue3906Reset(t)
	m := issue3906Room(issue3925Fixture(), "benefits")
	local := localStore{box: &localUI{}}
	raw := "/todo Prepare employee guide by tomorrow"
	chatcmd003Follow(m, local, "chat-composer", raw)
	issue3925Set(t, m, local, "tick", "author")
	// The Agents page replaces the chat component; its local state is new.
	fresh := localStore{box: &localUI{}}
	m.Draft = raw
	shown := chatcmd003Effective(m, fresh.get().chatcmd003, "chat-composer")
	if shown.Draft.Card.Todo == nil || shown.Draft.Card.Todo.Tick != "author" {
		t.Fatalf("the completion rule was not kept: %+v", shown.Draft.Card.Todo)
	}
}

func TestIssue3906_AnonymousPollSurvivesARemountAndIsPrivateToTheViewer(t *testing.T) {
	issue3906Reset(t)
	m := issue3906Room(issue3925Fixture(), "design")
	local := localStore{box: &localUI{}}
	raw := "/poll Pick a meeting time? Morning or Afternoon"
	chatcmd003Follow(m, local, "chat-composer", raw)
	issue3925Set(t, m, local, "anonymous", "yes")
	m.Draft = raw
	if p := chatcmd003Effective(m, chatcmd003Preview{}, "chat-composer").Draft.Card.Poll; p == nil || !p.Anonymous {
		t.Fatalf("anonymous was lost across a remount: %+v", p)
	}
	other := m
	other.CurrentUser = "dana"
	if p := chatcmd003Effective(other, chatcmd003Preview{}, "chat-composer").Draft.Card.Poll; p == nil || p.Anonymous {
		t.Fatalf("another viewer took the settings: %+v", p)
	}
	// A different kind of card in the same conversation does not take them.
	m.Draft = "/todo Prepare guide"
	if c := chatcmd003Effective(m, chatcmd003Preview{}, "chat-composer").Draft.Card; c.Todo == nil || c.Todo.Tick == "author" {
		t.Fatalf("a list took poll settings: %+v", c)
	}
}

func TestIssue3906_CancelAndPostForgetTheSettings(t *testing.T) {
	issue3906Reset(t)
	m := issue3906Room(issue3925Fixture(), "design")
	m.Chatcmd002.Post = func(_, _ string, _ chat.Chatcmd002Card, done func(error)) { done(nil) }
	local := localStore{box: &localUI{}}
	raw := "/poll Pick a meeting time? Morning or Afternoon"
	chatcmd003Follow(m, local, "chat-composer", raw)
	issue3925Set(t, m, local, "anonymous", "yes")
	chatcmd003Action(m, local, "chatcmd003-cancel", "", "")
	m.Draft = raw
	if p := chatcmd003Effective(m, chatcmd003Preview{}, "chat-composer").Draft.Card.Poll; p == nil || p.Anonymous {
		t.Fatalf("a cancelled draft left its settings behind: %+v", p)
	}
	chatcmd003Follow(m, local, "chat-composer", raw)
	issue3925Set(t, m, local, "anonymous", "yes")
	chatcmd003Post(m, local)
	if p := chatcmd003Effective(m, chatcmd003Preview{}, "chat-composer").Draft.Card.Poll; p == nil || p.Anonymous {
		t.Fatalf("a posted draft left its settings behind: %+v", p)
	}
}

func issue3925Fixture() Model {
	m := chatcmd003UIFixture()
	m.Chatcmd002.Post = func(_, _ string, _ chat.Chatcmd002Card, done func(error)) { done(nil) }
	return m
}
