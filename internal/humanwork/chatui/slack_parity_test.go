package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestMentionTokenAtFindsOnlyAWordStartingAt(t *testing.T) {
	cases := []struct {
		value     string
		caret     int
		query     string
		start     int
		ok        bool
		rationale string
	}{
		{"@Cam", 4, "Cam", 0, true, "at the start of the field"},
		{"hi @Ca", 6, "Ca", 3, true, "after a space"},
		{"(@mei", 5, "mei", 1, true, "after punctuation"},
		{"hi @", 4, "", 3, true, "a bare @ lists everyone"},
		{"me@example", 10, "", 0, false, "an email address is not a mention"},
		{"@Camila Morales ", 16, "", 0, false, "a finished mention followed by a space"},
		{"@Cam", 2, "C", 0, true, "the caret in the middle reads up to the caret"},
		{"😀 @Zu", 6, "Zu", 3, true, "offsets are UTF-16 units, so an emoji counts twice"},
		{"no mention", 10, "", 0, false, "no @ at all"},
	}
	for _, c := range cases {
		query, start, ok := mentionTokenAt(c.value, c.caret)
		if ok != c.ok || (ok && (query != c.query || start != c.start)) {
			t.Errorf("%s: mentionTokenAt(%q, %d) = %q, %d, %v; want %q, %d, %v", c.rationale, c.value, c.caret, query, start, ok, c.query, c.start, c.ok)
		}
	}
}

func TestMentionCandidatesRankMembersAndPrefixesFirst(t *testing.T) {
	m := Model{
		Members:         []Member{{ID: "w-2", Name: "Mateo Alvarez"}, {ID: "w-1", Name: "Camila Morales"}, {ID: "raw-id", Name: "raw-id"}},
		SearchDirectory: []SearchPerson{{ID: "w-3", Name: "Cameron Diaz"}, {ID: "w-1", Name: "Camila Morales"}, {ID: "w-4", Name: "Rosa Camacho"}},
	}
	got := mentionCandidates(m, "cam")
	names := []string{}
	for _, c := range got {
		names = append(names, c.Name)
	}
	want := []string{"Camila Morales", "Cameron Diaz", "Rosa Camacho"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("candidates = %v, want %v", names, want)
	}
	if !got[0].Member || got[1].Member {
		t.Fatalf("membership flags wrong: %+v", got)
	}
	for _, c := range mentionCandidates(m, "") {
		if c.ID == "raw-id" {
			t.Fatal("a member whose display name is still a raw ID must not be offered")
		}
	}
	fromAuthors := mentionCandidates(Model{Members: []Member{{ID: "hc-9", Name: "hc-9"}}, Messages: []Message{{AuthorID: "hc-9", Author: "Zuri Mensah"}, {AuthorID: "hc-7", Author: "Zane Park"}}}, "z")
	if len(fromAuthors) != 2 || fromAuthors[0].Name != "Zane Park" || fromAuthors[1].Name != "Zuri Mensah" {
		t.Fatalf("unresolved member names were not filled from message authors: %+v", fromAuthors)
	}
	many := Model{}
	for i := 0; i < mentionLimit+5; i++ {
		many.Members = append(many.Members, Member{ID: "id" + itoa(i+1), Name: "Person " + itoa(i+1)})
	}
	if n := len(mentionCandidates(many, "")); n != mentionLimit {
		t.Fatalf("candidate list not capped: %d", n)
	}
}

func TestApplyMentionAndNavigation(t *testing.T) {
	out, caret := applyMention("hi @Ca and more", 3, 6, "Camila Morales")
	if out != "hi @Camila Morales  and more" || caret != len("hi @Camila Morales ") {
		t.Fatalf("applyMention = %q, %d", out, caret)
	}
	if nextMention(0, -1, 3) != 2 || nextMention(2, 1, 3) != 0 || nextMention(1, 1, 3) != 2 || nextMention(4, 1, 0) != 0 {
		t.Fatal("nextMention does not wrap")
	}
}

func TestMentionMenuRendersListboxWithActiveOption(t *testing.T) {
	m := Model{State: StateReady, Members: []Member{{ID: "w-1", Name: "Camila Morales"}, {ID: "w-5", Name: "Carl Weber"}}}
	state := mentionState{Target: "chat-composer", Query: "ca", Open: true, Active: 1}
	markup := renderNode(t, mentionMenu(m, state, "chat-composer"))
	for _, want := range []string{`role="listbox"`, `id="chat-composer-mentions"`, `data-action="mention-pick"`, `data-extra="2"`, `aria-selected="true"`, "Carl Weber", "mention-option active"} {
		if !strings.Contains(markup, want) {
			t.Errorf("mention menu missing %q in %s", want, markup)
		}
	}
	// The closed list keeps an empty slot so the textarea after it never
	// shifts position; a shift makes the reconciler rebuild the field and
	// drop what the person is typing.
	if closed := renderNode(t, mentionMenu(m, state, "thread-composer")); closed != `<div class="mention-slot"></div>` {
		t.Fatalf("a list open on one composer rendered under the other: %s", closed)
	}
	empty := renderNode(t, mentionMenu(m, mentionState{Target: "chat-composer", Query: "zz", Open: true}, "chat-composer"))
	if !strings.Contains(empty, "No one matches") {
		t.Fatalf("no-match state missing: %s", empty)
	}
	aria := mentionFieldAria(state, "chat-composer", map[string]string{"describedby": "composer-help"})
	if aria["activedescendant"] != "chat-composer-mention-2" || aria["controls"] != "chat-composer-mentions" || aria["describedby"] != "composer-help" {
		t.Fatalf("field aria = %v", aria)
	}
	if closed := mentionFieldAria(mentionState{}, "chat-composer", nil); closed["activedescendant"] != "" {
		t.Fatalf("closed list still names a descendant: %v", closed)
	}
}

func TestFormatSelectionAppliesSupportedMarkdown(t *testing.T) {
	cases := []struct {
		kind, value  string
		start, end   int
		want         string
		wantS, wantE int
	}{
		{"bold", "make this loud", 5, 9, "make **this** loud", 7, 11},
		{"italic", "x", 0, 1, "_x_", 1, 2},
		{"bold", "", 0, 0, "****", 2, 2},
		{"code", "run go test now", 4, 11, "run `go test` now", 5, 12},
		{"code", "a\nb", 0, 3, "```\na\nb\n```", 4, 7},
		{"link", "see docs", 4, 8, "see [docs](url)", 11, 14},
		{"bullets", "one\ntwo", 0, 7, "- one\n- two", 11, 11},
		{"quote", "intro\nquoted", 6, 12, "intro\n> quoted", 14, 14},
		{"unknown", "same", 1, 2, "same", 1, 2},
	}
	for _, c := range cases {
		got, s, e := formatSelection(c.value, c.kind, c.start, c.end)
		if got != c.want || s != c.wantS || e != c.wantE {
			t.Errorf("%s(%q,%d,%d) = %q,%d,%d; want %q,%d,%d", c.kind, c.value, c.start, c.end, got, s, e, c.want, c.wantS, c.wantE)
		}
	}
	markup := renderNode(t, formatToolbar(Model{}, "chat-composer", false))
	for _, want := range []string{`role="group"`, `aria-label="Formatting"`, `data-extra="bold"`, `data-extra="quote"`, `aria-label="Bulleted list"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("toolbar missing %q", want)
		}
	}
}

func TestHighlightTextMarksEveryQueryWordCaseInsensitively(t *testing.T) {
	markup := renderNode(t, spanOf(highlightText("Launch the LAUNCH plan", "launch plan")))
	if strings.Count(markup, `<mark class="search-hit">`) != 3 || !strings.Contains(markup, `<mark class="search-hit">LAUNCH</mark>`) {
		t.Fatalf("highlight = %s", markup)
	}
	if plain := renderNode(t, spanOf(highlightText("nothing here", ""))); strings.Contains(plain, "<mark") {
		t.Fatalf("empty query highlighted: %s", plain)
	}
	when := searchWhen(Model{}, Message{SentAt: time.Now(), TimeLabel: "9:05 AM"})
	if when != "Today · 9:05 AM" {
		t.Fatalf("searchWhen = %q", when)
	}
	if searchWhen(Model{}, Message{TimeLabel: "noon"}) != "noon" {
		t.Fatal("a hit without a timestamp lost its label")
	}
}

func TestSelfDirectMessageIsLabelledAndIntroduced(t *testing.T) {
	self := Conversation{ID: "dm-me", Name: "Rafael Torres", Kind: DirectMessage}
	m := Model{State: StateReady, CurrentUser: "hc-050", SelectedID: "dm-me", Conversations: []Conversation{self}, PeerIDs: map[string]string{"dm-me": "hc-050"},
		Messages: []Message{{ID: "p1", Body: "note to self", SentAt: time.Now()}}}
	if got := displayName(m, self); got != "Rafael Torres (you)" {
		t.Fatalf("self DM name = %q", got)
	}
	markup := render(t, m)
	if !strings.Contains(markup, "Nobody else can see it") {
		t.Fatal("self DM intro missing")
	}
	other := Conversation{ID: "dm-2", Name: "Evelyn Morgan", Kind: DirectMessage}
	m.PeerIDs["dm-2"] = "hc-003"
	if got := displayName(m, other); got != "Evelyn Morgan" {
		t.Fatalf("a DM with someone else was marked as self: %q", got)
	}
}

func TestRailOffersAddChannelsAndSearchHidesTheSidePane(t *testing.T) {
	room := Conversation{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}
	m := Model{State: StateReady, Conversations: []Conversation{room}, SelectedID: "general", ShowThread: true, ThreadParentID: "p1",
		Callbacks: Callbacks{OpenBrowse: func() {}}}
	markup := render(t, m)
	if !strings.Contains(markup, `class="chat-row rail-add"`) || !strings.Contains(markup, "Add channels") {
		t.Fatal("channels section has no Add channels row")
	}
	if !strings.Contains(markup, `data-details-open="true"`) {
		t.Fatal("an open thread should open the side column")
	}
	m.Search = "budget"
	searching := render(t, m)
	if !strings.Contains(searching, `data-details-open="false"`) || strings.Contains(searching, "thread-pane") {
		t.Fatal("search results were drawn beside the room's thread pane")
	}
}

func TestStylesheetKeepsThreadBesideTimelineAtLaptopWidths(t *testing.T) {
	for _, want := range []string{
		`.chat-workspace{container:chat/inline-size}`,
		`@container chat (max-width:1350px){.chat-workspace[data-details-open="true"] .chat-layout{grid-template-columns:minmax(200px,var(--chat-rail)) minmax(0,1fr) minmax(280px,340px)}}`,
		`@container chat (max-width:1100px) and (min-width:761px){.chat-workspace[data-details-open="true"] .chat-layout{grid-template-columns:minmax(0,1fr) minmax(280px,360px)}.chat-workspace[data-details-open="true"] .chat-rail{display:none}`,
		`@container chatmain (max-width:560px){.channel-todo-trigger-label,.channel-poll-trigger-label{display:none}`, `.format-button[data-extra=code],.format-button[data-extra=bullets],.format-button[data-extra=quote]{display:none}`,
		`.message-list{flex:1;min-height:0;overflow-y:auto;overscroll-behavior:contain;display:flex;flex-direction:column;padding:8px 20px;scroll-padding-top:8px}`,
		`.mention-menu{`, `.search-hit{`,
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("stylesheet missing %q", want)
		}
	}
	if strings.Contains(Stylesheet, `@media(max-width:1350px){.chat-workspace[data-details-open="true"] .chat-layout{grid-template-columns:minmax(220px,var(--chat-rail)) minmax(0,1fr)}.chat-side{position:absolute`) {
		t.Fatal("the side column still overlays the timeline at 1350px and below")
	}
}

func renderNode(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func spanOf(children []ui.Node) ui.Node { return html.Span(html.Props{}, children...) }

func TestSearchSnippetCentresOnTheFirstMatch(t *testing.T) {
	long := "Opening line about something else entirely.\n\n" + strings.Repeat("filler words here ", 12) + "the budget moved to Friday and nobody objected."
	got := searchSnippet(long, "budget", 80)
	if !strings.HasPrefix(got, "…") || !strings.Contains(got, "budget") || strings.Contains(got, "\n") || len([]rune(got)) > 82 {
		t.Fatalf("snippet = %q", got)
	}
	if short := searchSnippet("one\n\ntwo", "two", 80); short != "one two" {
		t.Fatalf("short body not flattened: %q", short)
	}
	if head := searchSnippet(strings.Repeat("a ", 100), "zz", 20); strings.HasPrefix(head, "…") || !strings.HasSuffix(head, "…") {
		t.Fatalf("no-match snippet should start at the top: %q", head)
	}
}

func TestSearchSnippetCutsAtWordBoundaries(t *testing.T) {
	cut := searchSnippet("alpha bravo charlie delta echo foxtrot golf hotel india budget juliet kilo lima", "budget", 30)
	if !strings.HasPrefix(cut, "…") || !strings.Contains(cut, "budget") {
		t.Fatalf("snippet = %q", cut)
	}
	for _, word := range strings.Fields(strings.Trim(cut, "…")) {
		if !strings.Contains(" alpha bravo charlie delta echo foxtrot golf hotel india budget juliet kilo lima ", " "+word+" ") {
			t.Fatalf("snippet cut mid-word at %q: %q", word, cut)
		}
	}
}

func TestMeasuredAttachmentCarriesItsFrameAsDataNotStyle(t *testing.T) {
	msg := Message{ID: "p1", Attachments: []Attachment{{ID: "a1", Name: "party.gif", ContentType: "image/gif", Width: 480, Height: 270}}}
	markup := renderNode(t, attachments(Model{}, msg))
	for _, want := range []string{`data-frame-width="360"`, `data-frame-w="480"`, `data-frame-h="270"`, "attachment-image measured gif"} {
		if !strings.Contains(markup, want) {
			t.Errorf("attachment missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, "style=") {
		t.Fatalf("an inline style attribute violates the product CSP: %s", markup)
	}
}

func TestHoverBarLeadsWithQuickReactionsAndThreadShowsItsCount(t *testing.T) {
	msg := Message{ID: "p1", AuthorID: "a", Author: "Ann", Body: "hi", SentAt: time.Now()}
	m := Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel}}, Messages: []Message{msg},
		ShowThread: true, ThreadParentID: "p1", ThreadParent: &msg, ThreadMessages: []Message{{ID: "r1", Author: "Bo", Body: "one"}, {ID: "r2", Author: "Cy", Body: "two"}},
		Callbacks: Callbacks{ReactWith: func(string, string) {}, ReplyInThread: func(string, string) {}}}
	markup := render(t, m)
	for _, emoji := range quickReactions {
		if !strings.Contains(markup, `data-action="react-with" data-emoji="`+emoji+`" data-id="p1"`) && !strings.Contains(markup, `data-emoji="`+emoji+`"`) {
			t.Errorf("hover bar lacks quick reaction %s", emoji)
		}
	}
	if !strings.Contains(markup, `class="thread-count"`) || !strings.Contains(markup, "2 replies") {
		t.Fatal("thread pane has no reply-count divider")
	}
	if !strings.Contains(markup, `data-id="thread-composer" data-extra="bold"`) && !strings.Contains(markup, `data-extra="bold" data-id="thread-composer"`) {
		t.Fatal("thread composer lacks the formatting toolbar")
	}
	if got := searchSnippet("For context:\n- 3.4 percent\n- 71 percent", "percent", 200); got != "For context: 3.4 percent · 71 percent" {
		t.Fatalf("list flattening = %q", got)
	}
}

func TestSearchContextPlacesGlyphBeforeTheNameAndPillMarksUnread(t *testing.T) {
	ctx := renderNode(t, spanOf(searchContext(Model{}, html.Span(html.Props{Class: "glyph"}), "comp cycle")))
	if !strings.Contains(ctx, `in <span class="glyph"></span>comp cycle`) {
		t.Fatalf("context = %s", ctx)
	}
	m := Model{SelectedID: "a", Conversations: []Conversation{{ID: "a", Unread: 3}, {ID: "b", Unread: 0}}}
	if unreadDot(m) != nil {
		t.Fatal("the open room's own unread count lit the dot")
	}
	m.Conversations = append(m.Conversations, Conversation{ID: "c", Unread: 1})
	if unreadDot(m) == nil {
		t.Fatal("another room's unread did not light the dot")
	}
	m.Conversations[2].Muted = true
	if unreadDot(m) != nil {
		t.Fatal("a muted room lit the dot")
	}
}

// renderWithTray renders the workspace plus its inline channel tray opened on
// one widget: the to-do list and poll moved out of the details pane into the
// chat, and the tray's open state is local to the rendered component.
func renderWithTray(t *testing.T, m Model, which string) string {
	t.Helper()
	return render(t, m) + renderNode(t, channelTray(m, handlers{}, which))
}
