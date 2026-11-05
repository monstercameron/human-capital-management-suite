package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

// TestTodo_CHATUX_051: typing "#" suggests the channels the person can read
// and inserts a link; the shortcode list shows the full name and trims the
// shortcode; "Jump to newest" sits in reserved space above the composer;
// keyboard hints hide on touch devices; the header subtitle arrives whole.
func TestTodo_CHATUX_051(t *testing.T) {
	t.Run("ChannelTrigger", func(t *testing.T) {
		// "#" opens the channel list once a letter follows it. A digit, a
		// space, a heading's "##" or a caret in the middle of a word does
		// not: choosing there would cut the word in two.
		for _, tc := range []struct {
			value      string
			caret      int
			query      string
			start, end int
			ok         bool
		}{
			{"#d", 2, "d", 0, 2, true},
			{"see #gen", 8, "gen", 4, 8, true},
			{"#general ", 8, "general", 0, 8, true},
			{"#", 1, "", 0, 0, false},
			{"# ", 2, "", 0, 0, false},
			{"#1 thing", 2, "", 0, 0, false},
			{"## heading", 2, "", 0, 0, false},
			{"a#b", 3, "", 0, 0, false},
			{"#ge|x", 3, "", 0, 0, false},
			{"#général", 8, "général", 0, 8, true},
		} {
			value, caret := tc.value, tc.caret
			if i := strings.Index(value, "|"); i >= 0 {
				value, caret = value[:i]+value[i+1:], i
			}
			query, start, end, ok := channelSuggestToken(value, caret)
			if query != tc.query || start != tc.start || end != tc.end || ok != tc.ok {
				t.Errorf("%q@%d: got (%q,%d,%d,%v), want (%q,%d,%d,%v)", tc.value, tc.caret, query, start, end, ok, tc.query, tc.start, tc.end, tc.ok)
			}
		}
	})

	t.Run("ChannelCandidates", func(t *testing.T) {
		// Only joined channels are suggested, filtered by the query in any
		// casing, five at most.
		m := Model{Conversations: []Conversation{
			{ID: "general", Name: "general", Kind: PublicChannel, Joined: true},
			{ID: "design", Name: "design-reviews", Kind: PrivateChannel, Joined: true},
			{ID: "sales", Name: "sales", Kind: PublicChannel},
			{ID: "dm", Name: "Loretta Haynes", Kind: DirectMessage, Joined: true},
			{ID: "grp", Name: "huddle", Kind: GroupChat, Joined: true},
		}}
		if got := channelNames(channelSuggest(m, "")); got != "general, design-reviews" {
			t.Errorf("empty query suggests %q", got)
		}
		if got := channelNames(channelSuggest(m, "DES")); got != "design-reviews" {
			t.Errorf("query DES suggests %q", got)
		}
		if got := channelSuggest(m, "zzz"); len(got) != 0 {
			t.Errorf("query zzz suggests %d channels", len(got))
		}
		many := Model{}
		for i := 0; i < 10; i++ {
			many.Conversations = append(many.Conversations, Conversation{ID: "c" + string(rune('a'+i)), Name: "chan", Kind: PublicChannel, Joined: true})
		}
		if got := channelSuggest(many, "ch"); len(got) != 5 {
			t.Errorf("ten matches suggest %d channels, want 5", len(got))
		}
	})

	t.Run("ChannelMenu", func(t *testing.T) {
		// The list reads like the mention list: a labelled listbox above
		// the caret, one option per channel with its kind, the best match
		// highlighted. Closed, it keeps its slot like the other lists.
		m := Model{Locale: "en-US", Conversations: []Conversation{
			{ID: "general", Name: "general", Kind: PublicChannel, Joined: true},
			{ID: "design", Name: "design-reviews", Kind: PrivateChannel, Joined: true},
		}}
		open := renderNode(t, channelSuggestMenu(m, channelSuggestState{Target: "chat-composer", Query: "", Start: 0, End: 1, Open: true, Active: 0}, "chat-composer"))
		for _, want := range []string{`role="listbox"`, `aria-label="Channels"`, `role="option"`, "general", "design-reviews", `aria-selected="true"`, `data-action="channel-suggest-pick"`, "icon-hash", "icon-lock"} {
			if !strings.Contains(open, want) {
				t.Errorf("the open list misses %q: %s", want, open)
			}
		}
		filtered := renderNode(t, channelSuggestMenu(m, channelSuggestState{Target: "chat-composer", Query: "d", Start: 0, End: 2, Open: true, Active: 0}, "chat-composer"))
		if !strings.Contains(filtered, "design-reviews") || strings.Contains(filtered, ">general<") {
			t.Errorf("the list does not filter by its query: %s", filtered)
		}
		closed := renderNode(t, channelSuggestMenu(m, channelSuggestState{}, "chat-composer"))
		if !strings.Contains(closed, "channel-suggest-slot") || strings.Contains(closed, "listbox") {
			t.Errorf("the closed list does not keep its slot: %s", closed)
		}
	})

	t.Run("ChannelInsert", func(t *testing.T) {
		// Choosing a channel replaces "#query" with the "#label URL" pair
		// the body renderer reads as one link, keeping a space to type in.
		m := Model{Locale: "en-US", EmbedOrigin: "https://chat.example",
			Conversations: []Conversation{{ID: "design", Name: "design-reviews", Kind: PrivateChannel, Joined: true}}}
		updated, caret := channelSuggestInsert(m, "see #des", 8, 4, m.Conversations[0])
		if updated != "see #design-reviews /workspace/app/chat#channel=design " || caret != len(updated) {
			t.Errorf("picking inserts %q caret %d", updated, caret)
		}
		nodes := renderNode(t, html.Div(html.Props{}, channelReferenceBody(m, updated)...))
		if !strings.Contains(nodes, `class="chat-channel-reference"`) || !strings.Contains(nodes, ">#design-reviews<") {
			t.Errorf("the inserted pair is not one link: %s", nodes)
		}
		// A space already after the caret is not doubled.
		if updated, _ := channelSuggestInsert(m, "see #des now", 8, 4, m.Conversations[0]); strings.Contains(updated, "design  now") {
			t.Errorf("picking doubled the space: %q", updated)
		}
	})

	t.Run("ChannelKeyAction", func(t *testing.T) {
		// Enter or Tab with a channel highlighted picks it; otherwise the
		// draft sends or stays empty as before.
		if got := composerKeyAction(composerKeyState{Draft: "see #des", ChannelOpen: true, ChannelHighlighted: true}, "Enter"); got != composerPickChannel {
			t.Errorf("Enter on a highlighted channel is %q", got)
		}
		if got := composerKeyAction(composerKeyState{Draft: "see #des", ChannelOpen: true, ChannelHighlighted: true}, "Tab"); got != composerPickChannel {
			t.Errorf("Tab on a highlighted channel is %q", got)
		}
		if got := composerKeyAction(composerKeyState{Draft: "see #des", ChannelOpen: true}, "Enter"); got != composerSend {
			t.Errorf("Enter with no highlight is %q, want send", got)
		}
	})

	t.Run("ShortcodeTrimsCode", func(t *testing.T) {
		// The shortcode list shows the emoji's full name and trims the
		// shortcode instead; the mention list still trims the name.
		for _, want := range []string{
			`.emoji-completion-menu .mention-name{flex:none}`,
			`.emoji-completion-menu .mention-detail{min-width:0;overflow:hidden;text-overflow:ellipsis}`,
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("the stylesheet misses %q", want)
			}
		}
		if !strings.Contains(Stylesheet, `.mention-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap`) {
			t.Error("the mention list lost its own name trimming")
		}
	})

	t.Run("JumpReserved", func(t *testing.T) {
		// "Jump to newest" has its own gutter above the composer (pinned
		// by AGENTUX-062), never drawn on top of the text.
		for _, want := range []string{
			".chat-workspace .jump-newest{position:static",
			".timeline-frame{display:flex;flex-direction:column",
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("the stylesheet misses %q", want)
			}
		}
	})

	t.Run("TouchHintsHidden", func(t *testing.T) {
		// Every keyboard hint hides where there is no keyboard: the hint
		// rows and the search box's shortcut chip.
		for _, want := range []string{
			`@media(pointer:coarse){.kbd-hint{display:none}}`,
			`@media(hover:none) and (pointer:coarse){.chat-workspace .chat-search-shortcut{display:none}}`,
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("the stylesheet misses %q", want)
			}
		}
	})

	t.Run("SubtitleWhole", func(t *testing.T) {
		// The header subtitle is one line of separator-led parts: no part
		// is ever joined with a bare space, so no doubled space can appear,
		// and a late agent roster never drops the count once known.
		m := Model{Locale: "en-US", SelectedID: "general", CurrentUser: "walt",
			Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true, MemberCount: 18}},
			AgentCounts:   map[string]int{"general": 2}}
		topic := channelTopic(t, m)
		if strings.Contains(topic, "  ") {
			t.Errorf("the subtitle holds a doubled space: %s", topic)
		}
		if !strings.Contains(topic, "2 agents") {
			t.Errorf("the subtitle lost the known agent count: %s", topic)
		}
		// The name stays above the subtitle, so a late part never moves it.
		markup := render(t, m)
		h1, p := strings.Index(markup, "<h1"), strings.Index(markup, "conversation-topic")
		if h1 < 0 || p < 0 || h1 > p {
			t.Error("the header title is not drawn above its subtitle")
		}
	})
}

func channelNames(conversations []Conversation) string {
	names := make([]string, 0, len(conversations))
	for _, c := range conversations {
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}

func channelTopic(t *testing.T, m Model) string {
	t.Helper()
	markup := render(t, m)
	at := strings.Index(markup, "conversation-topic")
	if at < 0 {
		t.Fatal("the header has no subtitle")
	}
	open := strings.LastIndex(markup[:at], "<p")
	end := strings.Index(markup[at:], "</p>")
	if open < 0 || end < 0 {
		t.Fatal("the subtitle is not a paragraph")
	}
	return markup[open : at+end+len("</p>")]
}
