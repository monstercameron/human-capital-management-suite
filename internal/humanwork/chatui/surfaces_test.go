package chatui

import (
	"strings"
	"testing"
	"time"
)

func TestMentionsRenderAsPersonChipsThatOpenTheirDetails(t *testing.T) {
	opened := ""
	m := Model{CurrentUser: "me", CurrentUserName: "Rafael Torres",
		Members:   []Member{{ID: "w-1", Name: "Camila Morales"}, {ID: "w-2", Name: "Ana Maria Lopez"}, {ID: "w-3", Name: "Ana Maria"}},
		Messages:  []Message{{AuthorID: "w-4", Author: "Zuri Mensah"}},
		Callbacks: Callbacks{OpenPerson: func(id string) { opened = id }}}
	markup := renderNode(t, spanOf(mentionReferenceBody(m, "@Camila Morales and @ana maria lopez, cc @Zuri Mensah, not me@example.com or @Nobody Here, and @Rafael Torres")))
	for _, want := range []string{
		`class="mention-chip" data-action="open-person" data-id="w-1"`, `>@Camila Morales<`,
		`data-id="w-2"`, `>@Ana Maria Lopez<`,
		`data-id="w-4"`, `class="mention-chip self" data-action="open-person" data-id="me"`,
		`me@example.com or @Nobody Here`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("mention markup missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, `data-id="w-3"`) {
		t.Fatal("the shorter name won over the longer one it prefixes")
	}
	m.actWith("open-person", "w-1", "")
	if opened != "w-1" {
		t.Fatalf("mention chip opened %q", opened)
	}
	if plain := renderNode(t, spanOf(mentionReferenceBody(m, "no mentions here"))); strings.Contains(plain, "mention-chip") {
		t.Fatal("text without @ grew a chip")
	}
}

func TestBrowseListsJoinedChannelsAlongsideDiscoverableOnes(t *testing.T) {
	m := Model{State: StateReady,
		Conversations: []Conversation{{ID: "g", Name: "general", Kind: PublicChannel}, {ID: "dm", Name: "Evelyn", Kind: DirectMessage}, {ID: "p", Name: "payroll-close", Kind: PrivateChannel, Topic: "month end"}},
		Browse:        []Conversation{{ID: "d", Name: "design", Kind: PublicChannel, MemberCount: 8, LastActivity: time.Now()}},
		ShowBrowse:    true,
		Callbacks:     Callbacks{OpenBrowse: func() {}, CloseBrowse: func() {}, JoinConversation: func(string) {}, SelectConversation: func(string) {}, OpenCreate: func() {}}}
	entries := browseEntries(m)
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "design,general,payroll-close" {
		t.Fatalf("browse entries = %v", names)
	}
	markup := render(t, m)
	for _, want := range []string{`data-action="join" data-id="d"`, `data-action="browse-open" data-id="g"`, "Joined", "3 channels", "month end", `data-action="browse-to-create"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("browse dialog missing %q", want)
		}
	}
	if strings.Count(markup, "Create a channel") != 1 {
		t.Fatalf("create action appears %d times", strings.Count(markup, "Create a channel"))
	}
	m.BrowseQuery = "month"
	if got := browseEntries(m); len(got) != 2 || got[1].ID != "p" {
		t.Fatalf("filtered entries = %+v", got)
	}
	m.Browse, m.Conversations, m.BrowseQuery = nil, nil, "zzz"
	if !strings.Contains(render(t, m), "No channels match") {
		t.Fatal("empty filter lacks its no-match message")
	}
}

func TestMemberPickerSuggestsPeopleAndSubmitsIDs(t *testing.T) {
	m := Model{CurrentUser: "me", SearchDirectory: []SearchPerson{{ID: "me", Name: "Rafael Torres"}, {ID: "w-1", Name: "Camila Morales"}, {ID: "w-2", Name: "Carl Weber"}, {ID: "w-3", Name: "Rosa Camacho"}}}
	got := pickCandidates(m, "ca", []mentionCandidate{{ID: "w-2", Name: "Carl Weber"}})
	if len(got) != 2 || got[0].ID != "w-1" || got[1].ID != "w-3" {
		t.Fatalf("candidates = %+v", got)
	}
	if ids := pickedIDs([]mentionCandidate{{ID: "a"}, {ID: "b"}}); ids != "a,b" {
		t.Fatalf("pickedIDs = %q", ids)
	}
	h := handlers{local: localUI{createKind: GroupChat, pickQuery: "ca", picked: []mentionCandidate{{ID: "w-2", Name: "Carl Weber"}}}}
	markup := renderNode(t, createDialog(Model{SearchDirectory: m.SearchDirectory, CurrentUser: "me", Callbacks: Callbacks{CreateConversation: func(ConversationKind, string, []string) {}, CloseCreate: func() {}}}, h))
	for _, want := range []string{`role="radiogroup"`, `aria-checked="true" class="kind-card" data-action="create-kind" data-id="group"`, `class="person-chip"`, `data-action="create-unpick" data-id="w-2"`,
		`data-action="create-pick" data-id="w-1"`, `id="new-chat-members" name="members" type="hidden" value="w-2"`, `id="new-chat-kind" name="kind" type="hidden" value="group"`, "Start conversation"} {
		if !strings.Contains(markup, want) {
			t.Errorf("create dialog missing %q", want)
		}
	}
	dm := renderNode(t, createDialog(Model{Callbacks: Callbacks{CreateConversation: func(ConversationKind, string, []string) {}}}, handlers{local: localUI{createKind: DirectMessage}}))
	if strings.Contains(dm, `id="new-chat-name"`) || !strings.Contains(dm, `disabled type="submit"`) {
		t.Fatal("a direct message needs one person and no name field")
	}
	channel := renderNode(t, createDialog(Model{}, handlers{}))
	if !strings.Contains(channel, `class="name-prefix"`) || !strings.Contains(channel, "Lowercase, no spaces") || !strings.Contains(channel, "Create channel") {
		t.Fatal("channel creation lacks its # prefix, naming hint or label")
	}
}

func TestLocalStoreResetsRoomScopedState(t *testing.T) {
	box := &localUI{room: "a", tray: "todo", createKind: GroupChat, pickQuery: "x", picked: []mentionCandidate{{ID: "1"}}, pickActive: 2}
	s := localStore{box: box}
	s.forRoom("a")
	if box.tray != "todo" {
		t.Fatal("same room closed the tray")
	}
	s.forRoom("b")
	if box.tray != "" || box.room != "b" {
		t.Fatal("room change kept the tray open")
	}
	s.resetCreate()
	if box.createKind != "" || box.pickQuery != "" || box.picked != nil || box.pickActive != 0 {
		t.Fatalf("resetCreate left %+v", *box)
	}
	if s.get().room != "b" {
		t.Fatal("get does not read the box")
	}
}

func TestChannelTrayShowsSummaryChipsAndTheOpenWidget(t *testing.T) {
	m := Model{SelectedID: "room", Conversations: []Conversation{{ID: "room", Kind: PublicChannel}},
		ChannelTodo: ChannelTodoList{Revision: 1, Items: []ChannelTodoItem{{ID: "1", Text: "draft", Completed: true}, {ID: "2", Text: "send"}}},
		ChannelPoll: ChannelPoll{Question: "Lunch?", TotalVotes: 3}}
	closed := renderNode(t, channelTray(m, handlers{}, ""))
	for _, want := range []string{`data-action="tray-todo"`, "To-do list · 1 of 2 done", `data-action="tray-poll"`, "Lunch? · 3 votes"} {
		if !strings.Contains(closed, want) {
			t.Errorf("tray bar missing %q", want)
		}
	}
	if strings.Contains(closed, "channel-tray-card") {
		t.Fatal("a closed tray rendered a card")
	}
	open := renderNode(t, channelTray(m, handlers{}, "todo"))
	if !strings.Contains(open, `data-tray="todo"`) || !strings.Contains(open, `id="chat-todo-section"`) || !strings.Contains(open, `data-action="tray-close"`) {
		t.Fatal("open tray lacks the to-do card")
	}
	dm := renderNode(t, channelTray(Model{SelectedID: "d", Conversations: []Conversation{{ID: "d", Kind: DirectMessage}}}, handlers{}, "todo"))
	if dm != `<div class="channel-tray-slot"></div>` {
		t.Fatalf("a direct message showed channel tools: %s", dm)
	}
	if (Model{}).nz(0) != "0" || (Model{Number: func(int) string { return "٠" }}).nz(0) != "٠" {
		t.Fatal("nz does not render zero")
	}
}

func TestQuietHoursPopoverShowsSwitchScheduleAndZones(t *testing.T) {
	m := Model{State: StateReady, Preferences: Preferences{QuietHours: true, QuietTimezone: "Europe/Berlin", QuietStartMinute: 22 * 60, QuietEndMinute: 7 * 60}}
	markup := render(t, m)
	for _, want := range []string{`class="switch" id="quiet-hours" role="switch"`, `checked`, "Paused 10:00 PM–7:00 AM · Europe/Berlin", `<option selected value="Europe/Berlin">`, `value="UTC"`, `value="Asia/Tokyo"`, `id="quiet-start"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("quiet hours missing %q", want)
		}
	}
	if got := quietClock(Model{Locale: "de-DE"}, 22*60); got != "22:00" {
		t.Fatalf("German quiet clock = %q, want 24-hour", got)
	}
	m.Preferences.QuietHours = false
	off := render(t, m)
	if !strings.Contains(off, `disabled id="quiet-start"`) || !strings.Contains(off, "Messages still arrive") {
		t.Fatal("quiet hours off should disable the schedule and explain the setting")
	}
}

func TestGiphyCommandIsConsumedNotPosted(t *testing.T) {
	cases := []struct {
		body, query string
		ok          bool
	}{
		{"/giphy car", "car", true},
		{"  /GIPHY   happy dance ", "happy dance", true},
		{"/giphy", "", true},
		{"/giphycar", "", false},
		{"see /giphy car", "", false},
		{"hello", "", false},
	}
	for _, c := range cases {
		q, ok := giphyCommand(c.body)
		if ok != c.ok || q != c.query {
			t.Errorf("giphyCommand(%q) = %q, %v; want %q, %v", c.body, q, ok, c.query, c.ok)
		}
	}
	sent := ""
	m := Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Kind: PublicChannel}}, Draft: "/giphy car", ShowThread: true, ThreadParentID: "p",
		Callbacks: Callbacks{SendMessage: func(_, body string) { sent = body }, ReplyInThread: func(string, string) {}}}
	markup := render(t, m)
	if !strings.Contains(markup, `id="thread-composer-giphy-picker"`) {
		t.Fatal("the thread reply box has no GIF picker for /giphy to open")
	}
	if sent != "" {
		t.Fatalf("rendering posted %q", sent)
	}
}

func TestPersonPaneFallsBackToTheNameChatKnows(t *testing.T) {
	m := Model{State: StateReady, ShowPerson: true, PersonDetails: &PersonDetails{ID: "hc-043", Unavailable: true},
		Messages:  []Message{{ID: "p1", AuthorID: "hc-043", Author: "Camila Morales", Body: "hi"}},
		Callbacks: Callbacks{StartDirectMessage: func(string) {}, ClosePerson: func() {}}}
	markup := render(t, m)
	if !strings.Contains(markup, "<h3>Camila Morales</h3>") {
		t.Fatal("an unavailable directory entry hid the name chat already knows")
	}
	if strings.Contains(markup, "person-detail-list") {
		t.Fatal("unavailable details still listed empty fields")
	}
	if strings.Contains(markup, `data-action="start-direct-message" data-id="hc-043" disabled`) || strings.Contains(markup, `disabled data-action="start-direct-message"`) {
		t.Fatal("Message was disabled although chat can reach this person")
	}
	if chatKnownName(Model{CurrentUser: "me", CurrentUserName: "Rafael Torres"}, "me") != "Rafael Torres" || chatKnownName(Model{}, "") != "" {
		t.Fatal("chatKnownName fallbacks")
	}
}

func TestSearchFiltersResolveChannelAndAuthor(t *testing.T) {
	m := Model{Conversations: []Conversation{{ID: "g", Name: "general", Kind: PublicChannel}},
		Messages: []Message{{AuthorID: "hc-043", Author: "Camila Morales"}}}
	text, conv, author := SearchFilters(m, "budget in:#general from:@Camila Morales review")
	if text != "budget review" || conv != "g" || author != "hc-043" {
		t.Fatalf("filters = %q %q %q", text, conv, author)
	}
	if text, conv, _ := SearchFilters(m, "in:#nowhere budget"); text != "in:#nowhere budget" || conv != "" {
		t.Fatalf("unknown channel changed the query: %q %q", text, conv)
	}
	if text, conv, _ := SearchFilters(m, "in:#general"); text != "in:#general" || conv != "g" {
		t.Fatalf("filter-only search lost its text: %q %q", text, conv)
	}
}
