package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func TestTodo_CHATSEARCH_002_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		v := ChatSearchView{Query: "budget has:file", Recent: []string{"budget"}, Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Message, Text: "Budget <script>alert(1)</script>", Private: true, Target: chatsearch.Target{ConversationID: "room", MessageID: "message", Sequence: 42}}}}}, NextCursor: "next"}}
		markup, err := ui.RenderToString(RenderChatSearch(locale, v))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`data-chatsearch-action="open"`, `data-target=`, `<mark`, chatsearchText(locale, "only"), `data-chatsearch-action="remove"`, `data-chatsearch-action="more"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s missing %s: %s", locale, want, markup)
			}
		}
		if strings.Contains(markup, "<script>") || strings.Contains(markup, `data-chatsearch-action="clear-recent"`) {
			t.Fatal("unsafe text rendering, or recent searches under a query")
		}
		for _, state := range []ChatSearchView{{Query: "budget in:room"}, {Query: "budget", Loading: true}, {Query: "budget", Error: "meaning"}, {Query: "budget", Error: "invalid"}, {Query: "budget", Error: "unexpected"}} {
			markup, err := ui.RenderToString(RenderChatSearch(locale, state))
			if err != nil || !strings.Contains(markup, `role="`) || strings.Contains(markup, "unexpected") {
				t.Fatalf("state %+v %s %v", state, markup, err)
			}
		}
	}
	m := Model{Conversations: []Conversation{{ID: "room", Name: "General", Kind: PublicChannel}}}
	query, err := ResolveChatSearchQuery(m, `budget in:#General`)
	if err != nil || query != "budget in:room" {
		t.Fatalf("resolve %q %v", query, err)
	}
	query, err = ResolveChatSearchQuery(m, `budget in:restricted`)
	if err != nil || query != "budget in:restricted" {
		t.Fatalf("unknown filter widened: %q %v", query, err)
	}
	markup, err := ui.RenderToString(ChatSearchMount(m))
	if err != nil || !strings.Contains(markup, `id="chatsearch-results"`) {
		t.Fatalf("mount %s %v", markup, err)
	}
	markup, err = ui.RenderToString(RenderChatSearchReturn("de-DE"))
	if err != nil || !strings.Contains(markup, `data-chatsearch-action="back"`) || !strings.Contains(markup, chatsearchText("de-DE", "back")) {
		t.Fatalf("return %s %v", markup, err)
	}
}

func TestTodo_CHATSEARCH_002_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(RenderChatSearch(locale, ChatSearchView{Query: "budget", Error: "invalid"}))
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"channel", "person", "kind", "before", "after", "on", "file", "link", "reactions", "threads", "mentions", "agent", "mine", "voice"} {
			if !strings.Contains(markup, `for="chatsearch-`+field+`"`) || !strings.Contains(markup, `id="chatsearch-`+field+`"`) || chatsearchText(locale, field) == "" {
				t.Fatalf("%s unlabeled %s", locale, field)
			}
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("Arabic lacks RTL")
		}
		for _, d := range chatsearch.Declarations() {
			if chatsearchKind(locale, d.Kind) == "" {
				t.Fatalf("missing %s label in %s", d.Kind, locale)
			}
		}
	}
	for _, rule := range []string{"min-height:44px", ":focus-visible", "prefers-reduced-motion", "overflow-wrap:anywhere", "--hcm-"} {
		if !strings.Contains(ChatSearchStyles, rule) {
			t.Fatalf("missing style %s", rule)
		}
	}
}
