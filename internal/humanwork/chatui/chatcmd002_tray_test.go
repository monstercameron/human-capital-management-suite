package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// chatcmd002TrayModel is a channel with the standing list and poll and three
// open cards posted as messages: five things the bar could name.
func chatcmd002TrayModel(t *testing.T) Model {
	t.Helper()
	m := chatcmd003UIFixture()
	m.ChannelTodo = ChannelTodoList{Items: []ChannelTodoItem{{ID: "i1", Text: "Order pizza", Completed: true}}}
	m.ChannelPoll = ChannelPoll{Question: "Lunch spot?", TotalVotes: 0, Options: []ChannelPollOption{{ID: "o1", Text: "Tacos"}}}
	for _, id := range []string{"first", "second", "third"} {
		d, _ := chat.Chatcmd003ParsePoll(`"Poll `+id+`?" 1="A" 2="B"`, time.Now(), nil)
		body, _ := d.Card.Body()
		m.Messages = append(m.Messages, Message{ID: id, Body: body})
	}
	return m
}

// TestTodo_CHATCMD_002_Tray: the bar under the header is one line. With five
// open things it names the two most recent (cards first, newest first) and says
// how many more, and "+N more" opens a list of the rest where each row is a way
// to its message or to the channel's own card.
func TestTodo_CHATCMD_002_Tray(t *testing.T) {
	m := chatcmd002TrayModel(t)
	bar := renderNode(t, channelTray(m, handlers{}, ""))
	if got := strings.Count(bar, "<button"); got != 3 {
		t.Fatalf("the bar must hold two chips and the more button, got %d: %s", got, bar)
	}
	if !strings.Contains(bar, `data-id="third"`) || !strings.Contains(bar, `data-id="second"`) || strings.Contains(bar, `data-id="first"`) {
		t.Fatalf("the two newest cards must lead: %s", bar)
	}
	if !strings.Contains(bar, `data-action="tray-more"`) || !strings.Contains(bar, "+3 more") || !strings.Contains(bar, `aria-label="3 more polls and lists"`) || !strings.Contains(bar, `aria-expanded="false"`) {
		t.Fatalf("the more button: %s", bar)
	}
	if strings.Contains(bar, `data-action="tray-todo"`) || strings.Contains(bar, `data-action="tray-poll"`) || strings.Contains(bar, "tray-more-list") {
		t.Fatalf("the channel's own chips and the list belong behind the button: %s", bar)
	}

	open := renderNode(t, channelTray(m, handlers{}, chatcmd002TrayMore))
	for _, want := range []string{`data-tray="more"`, "More polls and lists", `class="tray-more-list"`, `data-action="tray-more-jump"`, `data-id="first"`, `data-action="tray-todo"`, `data-action="tray-poll"`, "Lunch spot? · 0 votes", `aria-expanded="true"`} {
		if !strings.Contains(open, want) {
			t.Errorf("the open list lacks %s: %s", want, open)
		}
	}
	if strings.Count(open, `class="tray-more-row`) != 3 {
		t.Errorf("the list holds exactly what the bar left out: %s", open)
	}

	// Two things fit in the bar: no button, and an opened list with nothing in it is not drawn.
	m.Messages, m.ChannelPoll = m.Messages[:1], ChannelPoll{}
	few := renderNode(t, channelTray(m, handlers{}, chatcmd002TrayMore))
	if strings.Contains(few, "tray-more") || strings.Contains(few, "channel-tray-card") || strings.Count(few, "<button") != 2 {
		t.Fatalf("a bar that fits has no more button: %s", few)
	}
	// The channel's own list and poll lead nothing: they follow the cards.
	m = chatcmd002TrayModel(t)
	m.Messages = nil
	only := renderNode(t, channelTray(m, handlers{}, ""))
	if strings.Index(only, `data-action="tray-todo"`) < 0 || strings.Index(only, `data-action="tray-todo"`) > strings.Index(only, `data-action="tray-poll"`) || strings.Contains(only, "tray-more") {
		t.Fatalf("standing chips alone: %s", only)
	}
}

// TestTodo_CHATCMD_002_TrayOneLine: the bar never wraps. The rule is in the
// stylesheet that decides ties, chips may shrink with an ellipsis, and the
// more button keeps its size.
func TestTodo_CHATCMD_002_TrayOneLine(t *testing.T) {
	if !strings.Contains(Stylesheet, ChatLane2Styles) || !strings.Contains(ChatLane2Styles, chatcmd002TrayStyles) {
		t.Fatal("the tray rules must be part of the stylesheet")
	}
	if got := s31LaterOverrides(t, chatcmd002TrayStyles); len(got) != 0 {
		t.Fatalf("a later block redraws the tray rules, so they lose: %v", got)
	}
	for _, want := range []string{".channel-tray-bar{flex-wrap:nowrap;overflow:hidden", ".channel-tray-bar>.tray-chip{flex:0 1 auto;min-width:0", ".tray-chip.tray-more{flex:none}"} {
		if !strings.Contains(chatcmd002TrayStyles, want) {
			t.Errorf("tray styles lack %s", want)
		}
	}
	// The three languages carry the button's words.
	for _, locale := range []string{"de-DE", "ar"} {
		m := chatcmd002TrayModel(t)
		m.Locale = locale
		bar := renderNode(t, channelTray(m, handlers{}, ""))
		if strings.Contains(bar, "+3 more") || !strings.Contains(bar, "+"+chatCount(locale, 3)+" ") {
			t.Errorf("%s: the more button is not translated: %s", locale, bar)
		}
	}
	// Choosing a row of the list and the button itself are known actions.
	if chatLayerKind("tray-more") != chatcmd002TrayMore {
		t.Fatal("the more button opens its layer under its own kind")
	}
}
