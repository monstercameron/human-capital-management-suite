package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

func chatbug056Model(locale string) Model {
	m := composerToolsModel(locale, false)
	m.GiphyAPIKey = "key"
	m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
	return m
}

// TestTodo_CHATBUG_056: typing "/" listed one command under a hint, "Tab
// details", that does nothing for commands. The list holds every command the
// conversation supports with what each does, narrows as the person types, and
// its hint names only keys that act on it.
func TestTodo_CHATBUG_056(t *testing.T) {
	m := chatbug056Model("en-US")
	registry := defaultComposerCommands()
	if names := composerCommandNames(registry.menu(m, "")); names != "poll,todo,giphy,location" {
		t.Fatalf("the list offers %s", names)
	}
	for query, want := range map[string]string{"p": "poll,giphy", "po": "poll", "t": "todo,location", "o": "poll,todo,location", "gi": "giphy", "x": ""} {
		if names := composerCommandNames(registry.menu(m, query)); names != want {
			t.Errorf("typing /%s lists %q, want %q", query, names, want)
		}
	}
	markup := composerToolsMarkup(t, m, localUI{commandMenu: composerCommandMenu{Target: "chat-composer", Open: true}})
	for _, want := range []string{">/poll<", "Ask a question and let people vote", ">/todo<", "Post a to-do list people can tick off", ">/giphy<", "Search for a GIF and post it", ">/location<", "Share your location",
		"↑↓ to move", "Enter or Tab to choose", "Esc to close"} {
		if !strings.Contains(markup, want) {
			t.Errorf("the open list lacks %q", want)
		}
	}
	if strings.Contains(markup, "Tab details") {
		t.Fatal("the hint of the list names a key that does nothing for a command")
	}
	// Each key the hint names does act: arrows move, Escape closes, and Enter
	// or Tab choose (composerCommandKey picks on both).
	open := composerCommandMenu{Target: "chat-composer", Open: true}
	if next, handled := composerCommandMenuMove(open, "ArrowDown", 4); !handled || next.Active != 1 {
		t.Fatalf("ArrowDown: %+v %v", next, handled)
	}
	if next, handled := composerCommandMenuMove(open, "Escape", 4); !handled || next.Open {
		t.Fatalf("Escape: %+v %v", next, handled)
	}
	if got, caret := composerCommandApply("/po", 3, "poll"); got != "/poll " || caret != 6 {
		t.Fatalf("choosing /poll writes %q with the caret at %d", got, caret)
	}
	// Where a command cannot be used it is not offered: an agent conversation
	// has no /poll, and an archived channel has no commands at all.
	agent := chatbug056Model("en-US")
	for i := range agent.Conversations {
		agent.Conversations[i].Agent = true
	}
	if names := composerCommandNames(registry.menu(agent, "")); strings.Contains(names, "poll") || strings.Contains(names, "todo") {
		t.Fatalf("an agent conversation offers %s", names)
	}
	archived := chatbug056Model("en-US")
	archived.ChannelStatuses = map[string]ChannelStatusView{archived.SelectedID: {Status: chat.ChannelStatus{Status: chatpolicy.StatusArchived}}}
	if names := composerCommandNames(registry.menu(archived, "")); names != "" {
		t.Fatalf("an archived channel offers %s", names)
	}
	// No language prints a key in place of a description or the hint.
	for _, locale := range []string{"de-DE", "ar"} {
		translated := composerToolsMarkup(t, chatbug056Model(locale), localUI{commandMenu: composerCommandMenu{Target: "chat-composer", Open: true}})
		for _, raw := range []string{"cmd-poll", "cmd-keys", "⟦", "Ask a question and let people vote", "Enter or Tab to choose"} {
			if strings.Contains(translated, raw) {
				t.Errorf("%s: the list shows %q", locale, raw)
			}
		}
		if strings.Count(translated, `role="option"`) != 4 {
			t.Errorf("%s: %d commands listed", locale, strings.Count(translated, `role="option"`))
		}
	}
}

// Once a command is chosen its usage is shown, arguments the grammar cannot
// read are marked, and a word that is no command is answered with the nearest
// one and a way to send the line as text (CHATCMD-001).
func TestTodo_CHATBUG_056_Chosen(t *testing.T) {
	m := chatbug056Model("en-US")
	for value, want := range map[string]composerCommandMenu{
		"/poll ":                   {Target: "chat-composer", Chosen: "poll"},
		"/poll where? a, b":        {Target: "chat-composer", Chosen: "poll"},
		`/poll "unclosed 1="a"`:    {Target: "chat-composer", Chosen: "poll", Invalid: true},
		"/todo book the room":      {Target: "chat-composer", Chosen: "todo"},
		`/giphy "cats`:             {Target: "chat-composer", Chosen: "giphy"},
		"/poll":                    {Target: "chat-composer", Query: "poll", Open: true},
		"/nonsense now":            {},
		"hello /poll ":             {},
		"//poll is how you ask it": {},
	} {
		if got := composerCommandMenuNext(m, composerCommandMenu{}, "chat-composer", value, len([]rune(value))); got != want {
			t.Errorf("%q: %+v, want %+v", value, got, want)
		}
	}
	usage := composerToolsMarkup(t, m, localUI{commandMenu: composerCommandMenu{Target: "chat-composer", Chosen: "giphy"}})
	for _, want := range []string{`id="chat-composer-command-usage"`, "Usage", "/giphy what to search for"} {
		if !strings.Contains(usage, want) {
			t.Errorf("the usage line lacks %q", want)
		}
	}
	if !strings.Contains(usage, `data-open="false:`) || strings.Contains(usage, "A quote is not closed") {
		t.Fatal("a chosen command still shows the list, or an error it does not have")
	}
	broken := composerToolsMarkup(t, m, localUI{commandMenu: composerCommandMenu{Target: "chat-composer", Chosen: "location", Invalid: true}})
	if !strings.Contains(broken, "A quote is not closed") || !strings.Contains(broken, `role="alert"`) {
		t.Fatal("arguments that cannot be read are not marked")
	}
	// /poll and /todo say how to write one in their preview, which is drawn
	// from the first character; no second line stands under it.
	if chosen := composerToolsMarkup(t, m, localUI{commandMenu: composerCommandMenu{Target: "chat-composer", Chosen: "poll"}}); strings.Contains(chosen, "command-usage") {
		t.Fatal("/poll shows a usage line as well as its preview")
	}

	// An unknown command is not sent.
	var log composerCommandLog
	rt := log.runtime(m)
	var unknown, suggestion string
	rt.Unknown = func(name, suggest string) { unknown, suggestion = name, suggest }
	if text, consumed := composerSendCommand(rt, "/pol where? a, b"); !consumed || text != "" || unknown != "pol" || suggestion != "poll" || log.notice != "" {
		t.Fatalf("/pol: consumed=%v text=%q unknown=%q suggestion=%q", consumed, text, unknown, suggestion)
	}
	if composerSendCommand(rt, "/xyzzy"); unknown != "xyzzy" || suggestion != "" {
		t.Fatalf("/xyzzy suggested %q", suggestion)
	}
	line := composerToolsMarkup(t, m, localUI{commandMenu: composerCommandMenu{Target: "chat-composer", Unknown: "pol", Suggest: "poll"}})
	for _, want := range []string{"/pol is not a command here. Did you mean /poll?", `data-action="command-use"`, `data-extra="poll"`, "Use /poll", `data-action="command-as-text"`, "Send as text", `role="status"`} {
		if !strings.Contains(line, want) {
			t.Errorf("the unknown-command line lacks %q", want)
		}
	}
	none := composerToolsMarkup(t, m, localUI{commandMenu: composerCommandMenu{Target: "chat-composer", Unknown: "xyzzy"}})
	if strings.Contains(none, "command-use") || !strings.Contains(none, "/xyzzy is not a command here.") || !strings.Contains(none, "Send as text") {
		t.Fatal("a word with no near command is offered one, or not offered Send as text")
	}
	if got, caret := composerCommandReplace("  /pol where? a, b", "poll"); got != "/poll where? a, b" || caret != 6 {
		t.Fatalf("taking the suggestion writes %q with the caret at %d", got, caret)
	}
	if got, caret := composerCommandReplace("/pol", "poll"); got != "/poll " || caret != 6 {
		t.Fatalf("taking the suggestion on a bare word writes %q, caret %d", got, caret)
	}
	local := localStore{box: &localUI{commandMenu: composerCommandMenu{Target: "chat-composer", Unknown: "pol", Suggest: "poll"}}}
	if !composerCommandAction(local, "command-use", "chat-composer", "poll") || local.get().commandMenu != (composerCommandMenu{}) {
		t.Fatal("taking the suggestion leaves the line up")
	}
	if composerCommandAction(local, "command-pick", "chat-composer", "1") {
		t.Fatal("a list row was taken for one of the line's buttons")
	}
	// The page only suggests a command it can run here.
	off := chatbug056Model("en-US")
	off.Chatcmd002.Post = nil
	if got := composerCommandClosest(off, "chat-composer", "pol"); got != "" {
		t.Fatalf("suggested /%s, which this page cannot run", got)
	}
}
