package chatui

import (
	stdhtml "html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The line is hidden once the author has typed past the refused text, and a later
// refusal (a new stamp) shows again.
func TestTodo_CHATMOD_002_AccessibilityLineLifecycle(t *testing.T) {
	key := ModAuthorKeyComposer("general")
	m := Model{Locale: "en-US", SelectedID: "general", AuthorBlocked: map[string]AuthorBlocked{key: {Surface: ModAuthorSurfaceMessage, Words: []string{"damn"}, Text: "you damn fool", Stamp: 7}}}
	if _, ok := modAuthorVisible(m, localUI{}, key); !ok {
		t.Fatal("a fresh refusal is not shown")
	}
	if _, ok := modAuthorVisible(m, localUI{modAuthorHidden: map[string]int64{key: 7}}, key); ok {
		t.Fatal("a refusal the author typed past is still shown")
	}
	if _, ok := modAuthorVisible(m, localUI{modAuthorHidden: map[string]int64{key: 6}}, key); !ok {
		t.Fatal("an older dismissal hid a newer refusal")
	}
	if _, ok := modAuthorVisible(m, localUI{}, ModAuthorKeyComposer("other")); ok {
		t.Fatal("a refusal in one conversation is shown in another")
	}
	aria := modAuthorDescribe(m, localUI{}, key, "line", map[string]string{"describedby": "composer-help"})
	if aria["describedby"] != "composer-help line" {
		t.Fatalf("describedby = %q", aria["describedby"])
	}
	if aria := modAuthorDescribe(m, localUI{modAuthorHidden: map[string]int64{key: 7}}, key, "line", nil); aria != nil {
		t.Fatalf("a hidden line is still described: %v", aria)
	}
}

// A reply that failed keeps its text in the thread box: the box is drawn with the
// text the model holds, with the line naming the words beside it, and a box with
// nothing to keep is drawn empty.
func TestTodo_CHATMOD_002_ThreadReplyKeepsText(t *testing.T) {
	root := Message{ID: "root", AuthorID: "walt", Author: "Walt", Body: "root message", Sequence: 1}
	build := func(drafts map[string]string, blocked map[string]AuthorBlocked) string {
		m := Model{State: StateReady, Locale: "en-US", CurrentUser: "jake", CurrentTenantID: "t", SelectedID: "room",
			Conversations: []Conversation{{ID: "room", Name: "room", Kind: PublicChannel, Joined: true}}, Messages: []Message{root},
			ShowThread: true, ThreadParentID: "root", ThreadParent: &root, ThreadDrafts: drafts, AuthorBlocked: blocked,
			Callbacks: Callbacks{ReplyInThread: func(string, string) {}}}
		page, err := ui.RenderToString(Build(m))
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	page := build(map[string]string{"root": "you damn fool"}, map[string]AuthorBlocked{ModAuthorKeyReply("root"): {Surface: ModAuthorSurfaceMessage, Words: []string{"damn"}, Text: "you damn fool", Stamp: 1}})
	if !strings.Contains(page, `data-chat-value="you damn fool"`) || !strings.Contains(page, `id="thread-composer"`) {
		t.Fatalf("the thread box does not carry the failed reply: %s", page)
	}
	if !strings.Contains(page, `id="chatmod002-blocked-reply"`) || !strings.Contains(stdhtml.UnescapeString(page), `allow: "damn".`) {
		t.Fatal("the thread line does not name the word")
	}
	if other := build(map[string]string{"another": "text"}, nil); strings.Contains(other, `data-chat-value="text"`) || strings.Contains(other, "chatmod002-blocked-reply") {
		t.Fatal("another thread's text or line leaked into this one")
	}
}

func TestTodo_CHATMOD_002_RemovedWordChip(t *testing.T) {
	m := Model{Locale: "de-DE"}
	markup, err := ui.RenderToString(html.P(html.Props{}, modAuthorInline(m, "du [removed word] und [removed word]")...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, `<span class="chatfilter-removed">entferntes Wort</span>`) != 2 || strings.Contains(markup, "[removed word]") {
		t.Fatalf("chips: %s", markup)
	}
	// Inside markdown too, beside emphasis, and in the note's own conditions.
	markup = renderMarkdownBody(t, Model{Locale: "en-US"}, "so **very [removed word]** rude")
	if !strings.Contains(markup, `<strong>very <span class="chatfilter-removed">removed word</span></strong>`) {
		t.Fatalf("markdown: %s", markup)
	}
	if modAuthorMaskedNote(m, Message{Body: "x [removed word]"}, false) != nil || modAuthorMaskedNote(m, Message{Body: "plain"}, true) != nil || modAuthorMaskedNote(m, Message{Body: "x [removed word]"}, true) == nil {
		t.Fatal("the masked note is shown to the wrong people")
	}
}
