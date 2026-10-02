package chatui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHATBUG_028_Commands is the "/" half of the entry: typing "/" in a
// channel opens the list of its commands, more letters narrow it, every row
// says what the command takes, and choosing a row writes the command and a
// space into the draft.
func TestTodo_CHATBUG_028_Commands(t *testing.T) {
	m := composerToolsModel("en-US", false)
	m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
	shown := func(draft string) []string {
		state := composerCommandMenuNext(m, composerCommandMenu{}, "chat-composer", draft, len(draft))
		return composerCommandShown(m, state, "chat-composer")
	}

	if got := shown("/"); !reflect.DeepEqual(got, []string{"poll", "todo", "giphy", "location", "ask"}) {
		t.Fatalf("\"/\" lists %v, want every command of the channel", got)
	}
	if got := shown("/po"); !reflect.DeepEqual(got, []string{"poll"}) {
		t.Fatalf("\"/po\" lists %v, want poll", got)
	}
	if got := shown("/gi"); !reflect.DeepEqual(got, []string{"giphy"}) {
		t.Fatalf("\"/gi\" lists %v, want giphy", got)
	}
	if got := shown("/zzz"); len(got) != 0 {
		t.Fatalf("\"/zzz\" lists %v, want a closed list", got)
	}
	if got := shown("hello /po"); len(got) != 0 {
		t.Fatalf("a slash inside a sentence opened the list: %v", got)
	}

	open := composerCommandMenuNext(m, composerCommandMenu{}, "chat-composer", "/", 1)
	markup := renderNode(t, composerCommandMenuView(m, open, "chat-composer", 1))
	for _, want := range []string{`role="listbox"`, `data-open="true:1"`, ">/poll<", ">/todo<", ">/giphy<", ">/location<",
		">question? option, option or option<", ">task, task @Name by Friday, task<", ">what to search for<"} {
		if !strings.Contains(markup, want) {
			t.Errorf("the open list lacks %q: %s", want, markup)
		}
	}
	// /location takes nothing, so its row (the other four take arguments, /ask among them) has no arguments line.
	if got := strings.Count(markup, `class="command-args"`); got != 4 {
		t.Errorf("%d rows carry arguments, want the four commands that take some", got)
	}

	updated, caret := composerCommandApply("/po", 3, "poll")
	if updated != "/poll " || caret != 6 {
		t.Fatalf("choosing /poll for \"/po\" gives %q, caret %d", updated, caret)
	}
	// A space already after the word is not doubled.
	if updated, caret = composerCommandApply("/po rest", 3, "poll"); updated != "/poll rest" || caret != 6 {
		t.Fatalf("choosing /poll in \"/po rest\" gives %q, caret %d", updated, caret)
	}

	// The list is also drawn by the composer itself, from the page's state.
	page := renderNode(t, composer(m, handlers{local: localUI{commandMenu: open}}))
	if !strings.Contains(page, `id="chat-composer-commands"`) || !strings.Contains(page, `data-open="true:`) {
		t.Errorf("the composer does not draw the open command list: %s", page)
	}
}
